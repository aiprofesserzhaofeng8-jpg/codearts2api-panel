package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/apierr"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
)

func TestUnifiedUngroupedAllProtocols(t *testing.T) {
	for _, platform := range []string{provider.WorkBuddy, provider.Trae} {
		for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%v", platform, path, stream), func(t *testing.T) {
					s, _, _ := mappedFixture(t, nil)
					key, err := s.Store.AddKeyInGroupAndSave("ungrouped", "")
					if err != nil {
						t.Fatal(err)
					}
					onlyMappedPlatform(s, platform)
					w := responseRequest(s, key.Key, path, mappedBody(path, stream, mappedPublic))
					if w.Code != 200 || !strings.Contains(w.Body.String(), `"model":"`+mappedPublic+`"`) {
						t.Fatal(w.Code, w.Body.String())
					}
					entry := s.Store.Logs(1)[0]
					if entry.Provider != platform || entry.Model == mappedPublic || entry.RequestedModel != mappedPublic {
						t.Fatal("wrong source accounting", entry)
					}
					for _, lease := range s.Store.PoolSnapshot() {
						if lease.InFlight != 0 {
							t.Fatal("leaked lease")
						}
					}
				})
			}
		}
	}
}
func TestUnifiedHTTPMissingAccountsAndUnknownTools(t *testing.T) {
	s, _, _ := mappedFixture(t, nil)
	key, _ := s.Store.AddKeyInGroupAndSave("ungrouped", "")
	for _, a := range s.Store.Accounts() {
		s.Store.SetAccountPaused(a.ID, true)
	}
	w := responseRequest(s, key.Key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, mappedPublic))
	if w.Code != 503 || strings.Contains(w.Body.String(), "导入本机桌面端") {
		t.Fatal("wrong availability diagnostic", w.Code, w.Body.String())
	}
	onlyMappedPlatform(s, provider.WorkBuddy)
	body := `{"model":"` + mappedPublic + `","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"read","parameters":{"type":"object"}}}]}`
	w = responseRequest(s, key.Key, "/v1/chat/completions", body)
	if w.Code != 400 {
		t.Fatal("unknown capability accepted", w.Code, w.Body.String())
	}
	for _, lease := range s.Store.PoolSnapshot() {
		if lease.InFlight != 0 {
			t.Fatal("rejection leased account")
		}
	}
}
func unifiedContext(t *testing.T, s *Server, key string) context.Context {
	t.Helper()
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	r = r.WithContext(context.WithValue(r.Context(), groupKeyContext{}, key))
	r = beginInference(httptest.NewRecorder(), r, "chat")
	m, err := s.resolveModelForRequest(r, s.Store.Settings(), mappedPublic)
	if err != nil {
		t.Fatal(err)
	}
	return withRoutingModel(r.Context(), m.ID)
}
func TestUnifiedRevisionStopsRetryBeforeSecondUpstream(t *testing.T) {
	s, _, _ := mappedFixture(t, nil)
	key, _ := s.Store.AddKeyInGroupAndSave("ungrouped", "")
	ctx := unifiedContext(t, s, key.Key)
	calls := 0
	_, err := s.withAccountRetry(ctx, time.Second, func(ctx context.Context, a *accounts.Account) error {
		calls++
		snapshot := s.Store.UnifiedSnapshot()
		model := snapshot.Models[0]
		model.Sources[0].Enabled = false
		if _, e := s.Store.SaveUnifiedModel(model, nil, model.Revision, snapshot.Revision, nil); e != nil {
			t.Fatal(e)
		}
		return &provider.Error{Status: 502, Message: "fixture upstream failed"}
	})
	if calls != 1 || apierr.From(err).Status != 409 {
		t.Fatal("stale retry executed", calls, err)
	}
	for _, lease := range s.Store.PoolSnapshot() {
		if lease.InFlight != 0 {
			t.Fatal("stale retry leaked lease")
		}
	}
}
func TestUnifiedCancellationReleasesAndNoRetry(t *testing.T) {
	s, _, _ := mappedFixture(t, nil)
	key, _ := s.Store.AddKeyInGroupAndSave("ungrouped", "")
	ctx, cancel := context.WithCancel(unifiedContext(t, s, key.Key))
	defer cancel()
	calls := 0
	_, err := s.withAccountRetry(ctx, time.Second, func(ctx context.Context, a *accounts.Account) error { calls++; cancel(); return ctx.Err() })
	if calls != 1 || !errors.Is(err, context.Canceled) {
		t.Fatal(calls, err)
	}
	for _, lease := range s.Store.PoolSnapshot() {
		if lease.InFlight != 0 {
			t.Fatal("cancel leaked lease")
		}
	}
}
func TestUnifiedTransportFailureSwitchesWithoutPoisoningAccount(t *testing.T) {
	s, _, _ := mappedFixture(t, nil)
	key, _ := s.Store.AddKeyInGroupAndSave("ungrouped", "")
	ctx := unifiedContext(t, s, key.Key)
	var seen []string
	_, err := s.withAccountRetry(ctx, 3*time.Second, func(ctx context.Context, a *accounts.Account) error {
		seen = append(seen, a.ID)
		if len(seen) == 1 {
			return provider.NewTransportError("fixture", errors.New("fixture network"))
		}
		return nil
	})
	if err != nil || len(seen) != 2 || seen[0] == seen[1] {
		t.Fatal(seen, err)
	}
	a := s.Store.Account(seen[0])
	if a.Failures != 0 || a.CooldownUntil != 0 {
		t.Fatal("network fault poisoned account")
	}
}
func TestUnifiedAPIErrorPreservesRevisionStatus(t *testing.T) {
	for _, status := range []int{400, 403, 409, 503} {
		err := config.ModelFailure(status, "fixture", "fixture")
		if apierr.From(err).Status != status {
			t.Fatal(status)
		}
	}
}

func TestUnifiedCapabilityRoutingPreservesImagesAndToolSchema(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			calls := 0
			s, _, _ := mappedFixture(t, func(platform string, w http.ResponseWriter, r *http.Request) {
				calls++
				raw, _ := io.ReadAll(r.Body)
				if platform != provider.WorkBuddy || !strings.Contains(string(raw), "data:image/png;base64,fixture") || !strings.Contains(string(raw), "9007199254740993") {
					t.Error("capability or opaque input lost", platform, string(raw))
				}
				policySuccess(w, platform, true)
			})
			s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "shared", ImageInput: "supported", ToolCalling: "supported", Context: 65536, MaxOutput: 8192}})
			key, _ := s.Store.AddKeyInGroupAndSave("ungrouped", "")
			var body string
			switch path {
			case "/v1/chat/completions":
				body = `{"model":"` + mappedPublic + `","messages":[{"role":"system","content":"CLIENT-PROMPT"},{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,fixture"}}]}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"id":{"const":9007199254740993}}}}}]}`
			case "/v1/responses":
				body = `{"model":"` + mappedPublic + `","instructions":"CLIENT-PROMPT","input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,fixture"}]}],"tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{"id":{"const":9007199254740993}}}}]}`
			default:
				body = `{"model":"` + mappedPublic + `","max_tokens":128,"system":"CLIENT-PROMPT","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"fixture"}}]}],"tools":[{"name":"lookup","input_schema":{"type":"object","properties":{"id":{"const":9007199254740993}}}}]}`
			}
			w := responseRequest(s, key.Key, path, body)
			if w.Code != 200 || calls != 1 {
				t.Fatal(w.Code, calls, w.Body.String())
			}
		})
	}
}

func TestUnifiedRetriesBoundedAndAuthFailureStops(t *testing.T) {
	for _, status := range []int{502, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s, _, _ := mappedFixture(t, nil)
			key, _ := s.Store.AddKeyInGroupAndSave("ungrouped", "")
			s.Store.UpdateSettings(func(c *storage.Settings) { c.MaxRetries = 2 })
			ctx := unifiedContext(t, s, key.Key)
			calls := 0
			_, err := s.withAccountRetry(ctx, time.Second, func(context.Context, *accounts.Account) error {
				calls++
				return &provider.Error{Status: status, Message: "fixture"}
			})
			want := 2
			if status == 403 {
				want = 1
			}
			if calls != want || err == nil {
				t.Fatal(calls, err)
			}
			for _, lease := range s.Store.PoolSnapshot() {
				if lease.InFlight != 0 {
					t.Fatal("lease leaked")
				}
			}
		})
	}
}
