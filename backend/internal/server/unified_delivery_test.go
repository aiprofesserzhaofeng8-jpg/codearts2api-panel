package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
)

// Exercise both real native adapters, protocol bridges, retry and settlement.
func TestUnifiedDeliveryFailoverAccounting(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", path, stream), func(t *testing.T) {
				var calls atomic.Int32
				s, key, _ := mappedFixture(t, func(platform string, w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					var body map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || string(body["model"]) != `"shared"` {
						t.Error("wrong original source model", platform, err)
					}
					if platform == provider.Trae && string(body["config_name"]) != `"shared"` {
						t.Error("wrong Trae source config")
					}
					if platform == provider.WorkBuddy {
						w.WriteHeader(429)
						io.WriteString(w, `{"error":{"message":"synthetic source exhausted"}}`)
						return
					}
					policySuccess(w, platform, true)
				})
				s.Store.SetAccountPaused("a", true)
				w := responseRequest(s, key, path, mappedBody(path, stream, mappedPublic))
				if w.Code != 200 || calls.Load() != 2 || !strings.Contains(w.Body.String(), mappedPublic) {
					t.Fatal(w.Code, calls.Load(), w.Body.String())
				}
				logs := s.Store.Logs(10)
				if len(logs) != 1 || len(logs[0].AttemptDetails) != 2 || logs[0].RequestedModel != mappedPublic || logs[0].Model != "trae/cn/shared" {
					t.Fatalf("logical/physical accounting: %+v", logs)
				}
				for _, k := range s.Store.Keys() {
					if k.Key == key && (k.BudgetUsage.Requests != 1 || k.BudgetUsage.Tokens != 16) {
						t.Fatalf("duplicate or lost settlement: %+v", k.BudgetUsage)
					}
				}
				for _, lease := range s.Store.PoolSnapshot() {
					if lease.InFlight != 0 {
						t.Fatal("lease leaked")
					}
				}
			})
		}
	}
}

func TestUnifiedDeliveryConcurrentLimitsAndCancellation(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	s, key, _ := mappedFixture(t, func(platform string, w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		case <-r.Context().Done():
			return
		}
		select {
		case <-release:
			policySuccess(w, platform, true)
		case <-r.Context().Done():
		}
	})
	s.Store.SetAccountPaused("a", true)
	for _, id := range []string{"wb-cn", "trae-acct"} {
		if ok, err := s.Store.SetAccountConcurrencyAndSave(id, 1); err != nil || !ok {
			t.Fatal("configure fixture concurrency", err)
		}
	}
	gateway := httptest.NewServer(s.Handler())
	defer gateway.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequestWithContext(ctx, "POST", gateway.URL+"/v1/chat/completions", strings.NewReader(mappedBody("/v1/chat/completions", true, mappedPublic)))
			req.Header.Set("Authorization", "Bearer "+key)
			resp, err := gateway.Client().Do(req)
			if err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			cancel()
			close(release)
			wg.Wait()
			t.Fatal("two sources not occupied")
		}
	}
	w := responseRequest(s, key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, mappedPublic))
	if w.Code != 503 {
		cancel()
		close(release)
		wg.Wait()
		t.Fatal("concurrency bypass", w.Code, w.Body.String())
	}
	cancel()
	close(release)
	wg.Wait()
	deadline := time.Now().Add(3 * time.Second)
	for {
		busy := false
		for _, lease := range s.Store.PoolSnapshot() {
			busy = busy || lease.InFlight != 0
		}
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancelled HTTP leaked lease")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestUnifiedDeliveryRetryRechecksGroupAndBudget(t *testing.T) {
	for _, change := range []string{"account-removal", "grant-revoked", "budget-exhausted"} {
		t.Run(change, func(t *testing.T) {
			var srv *Server
			var apiKey string
			calls := 0
			srv, apiKey, _ = mappedFixture(t, func(platform string, w http.ResponseWriter, r *http.Request) {
				calls++
				if platform != provider.WorkBuddy {
					t.Error("retry bypassed new permission")
				}
				g := srv.Store.CanonicalGroups()[0]
				if change == "account-removal" {
					g.AccountIDs = []string{"wb-cn"}
				} else if change == "grant-revoked" {
					g.UnifiedModels[0].Callable = false
				}
				if change != "budget-exhausted" {
					if _, err := srv.Store.SaveUnifiedGroup(g, g.Revision); err != nil {
						t.Error(err)
					}
				}
				w.WriteHeader(429)
				io.WriteString(w, `{"error":{"message":"synthetic failure"}}`)
			})
			srv.Store.SetAccountPaused("a", true)
			if change == "budget-exhausted" {
				if err := srv.Store.SetKeyBudgetAndSave(apiKey, storage.KeyBudget{MaxTokens: 1}, false); err != nil {
					t.Fatal(err)
				}
			}
			w := responseRequest(srv, apiKey, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, mappedPublic))
			if w.Code == 200 || calls != 1 {
				t.Fatal("outdated retry ran", w.Code, calls, w.Body.String())
			}
		})
	}
}

// Actual Pi executable + unchanged launcher; only gateways/providers are fixtures.
func TestUnifiedPiClient(t *testing.T) {
	if os.Getenv("CODEARTS_U6_PI") != "1" {
		t.Skip("opt-in isolated Pi acceptance")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Fatal("Pi is required for U6 acceptance")
	}
	var texts, tools, results atomic.Int32
	s, _, _ := mappedFixture(t, func(platform string, w http.ResponseWriter, r *http.Request) {
		if platform != provider.WorkBuddy {
			t.Error("unexpected source", platform)
		}
		var body struct {
			Model    string            `json:"model"`
			Tools    []json.RawMessage `json:"tools"`
			Messages []struct {
				Role    string
				Content json.RawMessage
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if body.Model != "shared" {
			t.Error("wrong original model", body.Model)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, m := range body.Messages {
			if m.Role == "tool" && strings.Contains(string(m.Content), "U6_READ_MARKER_9007199254740993") {
				results.Add(1)
				io.WriteString(w, summaryFixture("U6_READ_MARKER_9007199254740993", "stop", 12, 4))
				return
			}
		}
		if len(body.Tools) > 0 {
			tools.Add(1)
			io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_u6\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"probe.txt\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":4}}\n\ndata: [DONE]\n\n")
			return
		}
		texts.Add(1)
		io.WriteString(w, summaryFixture("U6_TEXT_OK", "stop", 12, 4))
	})
	onlyMappedPlatform(s, provider.WorkBuddy)
	for _, platform := range []string{provider.WorkBuddy, provider.Trae} {
		s.Reg.ReplaceProvider(platform, "cn", []provider.Model{{ID: "shared", Context: 65536, MaxOutput: 8192, ToolCalling: "supported", ImageInput: "unsupported"}})
	}
	key, err := s.Store.AddKeyInGroupAndSave("u6-pi", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.UpdateSettings(func(st *storage.Settings) { st.DefaultModel = mappedPublic }); err != nil {
		t.Fatal(err)
	}
	markModelsFresh(s)
	// Disabled accounts do not erase configured-source profile requirements.
	if err := s.Store.SetModelPolicyAndSave("test-model", config.ModelPolicy{ContextWindow: 65536, MaxOutputTokens: 8192, ReserveTokens: 16384, TriggerTokens: 49152, KeepRecentTokens: 12000, SummaryMaxTokens: 4096, Source: "synthetic U6 policy"}); err != nil {
		t.Fatal(err)
	}
	gateway := httptest.NewServer(s.Handler())
	defer gateway.Close()
	root, _ := filepath.Abs("../../..")
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "probe.txt"), []byte("U6_READ_MARKER_9007199254740993\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []bool{false, true} {
		args := []string{filepath.Join(root, "scripts/connect-client.py"), "pi", "--url", gateway.URL, "--config-dir", filepath.Join(work, "config"), "--", "--offline", "--mode", "json", "--print", "--no-session", "--no-extensions", "--no-mcp", "--no-skills", "--no-context-files", "--no-prompt-templates", "--thinking", "off"}
		if tool {
			args = append(args, "--tools", "read", "Read only probe.txt and reply with its exact contents.")
		} else {
			args = append(args, "--no-tools", "Reply U6_TEXT_OK")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		cmd := exec.CommandContext(ctx, "python3", args...)
		cmd.Dir = work
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "CODEARTS_API_KEY=" + key.Key, "PI_OFFLINE=1", "PI_TELEMETRY=0"}
		output, runErr := cmd.CombinedOutput()
		cancel()
		if runErr != nil {
			t.Fatalf("Pi failed: %v: %s", runErr, strings.ReplaceAll(string(output), key.Key, "[redacted]"))
		}
		if !tool && !strings.Contains(string(output), "U6_TEXT_OK") {
			t.Fatal("text missing")
		}
		if tool && (!strings.Contains(string(output), `"type":"tool_execution_end"`) || !strings.Contains(string(output), "U6_READ_MARKER_9007199254740993")) {
			t.Fatal("actual tool loop missing")
		}
	}
	if texts.Load() != 1 || tools.Load() != 1 || results.Load() != 1 {
		t.Fatal("unexpected Pi request counts", texts.Load(), tools.Load(), results.Load())
	}
	raw, err := os.ReadFile(filepath.Join(work, "config/pi/models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), key.Key) || !strings.Contains(string(raw), mappedPublic) {
		t.Fatal("client configuration identity/secret mismatch")
	}
	t.Log("Pi: profile default public name, text and actual read tool loop passed; 3 inference requests; temporary client directory only")
}
