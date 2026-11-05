package app_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/app"
	"codearts2api/internal/catalog"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"codearts2api/internal/trae"
	"codearts2api/internal/workbuddy"
)

// Opt-in only. Reads the source bytes without Open/Save or credential refresh.
// All account names/IDs in the report are local ordinal aliases.
type liveSource struct {
	account *accounts.Account
	adapter provider.Adapter
	models  []provider.Model
	alias   string
}

type liveObservedAdapter struct {
	provider.Adapter
	total     *atomic.Int32
	model     string
	calls     atomic.Int32
	refreshes atomic.Int32
}

func (a *liveObservedAdapter) Refresh(context.Context, json.RawMessage) (json.RawMessage, error) {
	a.refreshes.Add(1)
	return nil, errors.New("live acceptance forbids refresh")
}
func (a *liveObservedAdapter) Stream(ctx context.Context, raw json.RawMessage, req provider.Request, emit func([]byte) error) error {
	if req.Model != a.model {
		return errors.New("live original model mismatch")
	}
	if a.total.Add(1) > 24 {
		return errors.New("live inference attempt cap reached")
	}
	a.calls.Add(1)
	return a.Adapter.Stream(ctx, raw, req, emit)
}

func liveJSONRequest(client *http.Client, base, key, path string, body any) (int, []byte, error) {
	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		input = bytes.NewReader(raw)
	}
	method := "GET"
	if body != nil {
		method = "POST"
	}
	req, err := http.NewRequest(method, base+path, input)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	return response.StatusCode, raw, err
}

func liveAnswer(path string, stream bool, raw []byte) (string, bool, bool) {
	objects := []map[string]any{}
	if stream {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "data: ") && line != "data: [DONE]" {
				var obj map[string]any
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &obj) == nil {
					objects = append(objects, obj)
				}
			}
		}
	} else {
		var obj map[string]any
		if json.Unmarshal(raw, &obj) == nil {
			objects = append(objects, obj)
		}
	}
	text := ""
	terminal, usage := false, false
	var scan func(any)
	scan = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if u, ok := x["usage"].(map[string]any); ok {
				for _, v := range u {
					if n, ok := v.(float64); ok && n > 0 {
						usage = true
					}
				}
			}
			if x["finish_reason"] == "stop" || x["stop_reason"] == "end_turn" || x["status"] == "completed" || x["type"] == "message_stop" {
				terminal = true
			}
			for _, v := range x {
				scan(v)
			}
		case []any:
			for _, v := range x {
				scan(v)
			}
		}
	}
	for _, obj := range objects {
		scan(obj)
		if path == "/v1/chat/completions" {
			if cs, ok := obj["choices"].([]any); ok {
				for _, c := range cs {
					if c, ok := c.(map[string]any); ok {
						field := "message"
						if stream {
							field = "delta"
						}
						if msg, ok := c[field].(map[string]any); ok {
							if s, ok := msg["content"].(string); ok {
								text += s
							}
						}
					}
				}
			}
		}
		if path == "/v1/responses" {
			if stream {
				if obj["type"] == "response.output_text.delta" {
					if s, ok := obj["delta"].(string); ok {
						text += s
					}
				}
			} else {
				if outputs, ok := obj["output"].([]any); ok {
					for _, o := range outputs {
						if o, ok := o.(map[string]any); ok {
							if parts, ok := o["content"].([]any); ok {
								for _, p := range parts {
									if p, ok := p.(map[string]any); ok && p["type"] == "output_text" {
										text += fmt.Sprint(p["text"])
									}
								}
							}
						}
					}
				}
			}
		}
		if path == "/v1/messages" {
			if stream {
				if d, ok := obj["delta"].(map[string]any); ok && d["type"] == "text_delta" {
					text += fmt.Sprint(d["text"])
				}
			} else {
				if parts, ok := obj["content"].([]any); ok {
					for _, p := range parts {
						if p, ok := p.(map[string]any); ok && p["type"] == "text" {
							text += fmt.Sprint(p["text"])
						}
					}
				}
			}
		}
	}
	return text, terminal, usage
}

func TestUnifiedLiveProtocols(t *testing.T) {
	if os.Getenv("CODEARTS_UNIFIED_LIVE_INFERENCE") != "1" {
		t.Skip("explicit opt-in inference required")
	}
	sources, report := liveSources(t)
	report["phase"] = "real_unified_protocols"
	report["output_token_cap_per_request"] = 128
	report["attempt_cap"] = 24
	checks := []map[string]any{}
	report["checks"] = checks
	defer func() { report["checks"] = checks; saveLiveReport(t, report) }()
	const rawID = "deepseek-v4.1-flash"
	const public = "deepseek-4.1-flash-live-20261105"
	selected := map[string]liveSource{}
	for _, source := range sources {
		if _, ok := selected[source.account.ProviderID()]; ok {
			continue
		}
		for _, m := range source.models {
			if m.ID == rawID {
				selected[source.account.ProviderID()] = source
				break
			}
		}
	}
	if len(selected) != 2 {
		report["blocked"] = "two current catalog sources unavailable"
		t.Fatal("two real catalog sources required")
	}
	st, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = st.UpdateSettings(func(s *config.Settings) {
		s.AutoRefresh = false
		s.AutoClaim = false
		s.WriteBackToIDE = false
		s.RateLimitEnabled = false
		s.MaxRetries = 1
		s.RequestTimeout = 35
		s.Pool.Strategy = "sequential_exhaust"
		s.Pool.StickyTTLSec = 0
		s.WorkBuddy.PromptMode = "passthrough"
		s.WorkBuddy.PromptFallback = false
		s.Trae.PromptMode = "passthrough"
		s.Trae.PromptFallback = false
	}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{provider.WorkBuddy, provider.Trae} {
		st.UpsertAccount(selected[p].account)
	}
	srv, err := app.Build(st)
	if err != nil {
		t.Fatal(err)
	}
	srv.Providers = provider.NewRegistry()
	observed := map[string]*liveObservedAdapter{}
	var total atomic.Int32
	for _, p := range []string{provider.WorkBuddy, provider.Trae} {
		a := &liveObservedAdapter{Adapter: selected[p].adapter, total: &total, model: rawID}
		observed[p] = a
		if err = srv.Providers.Register(a); err != nil {
			t.Fatal(err)
		}
	}
	if err = srv.Catalog.Refresh(context.Background(), catalog.RefreshOperations{Provider: func(_ context.Context, a *accounts.Account) ([]provider.Model, error) {
		return selected[a.ProviderID()].models, nil
	}}); err != nil {
		t.Fatal("seed real discovered catalog")
	}
	m, err := st.SaveUnifiedModel(config.UnifiedModel{Name: public, Enabled: true, Sources: []config.ModelSource{{ID: "workbuddy/cn/" + rawID, Enabled: true}, {ID: "trae/cn/" + rawID, Enabled: true}}}, nil, 0, st.UnifiedSnapshot().Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, err := st.AddKeyInGroupAndSave("bounded-real-acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = st.SetKeyBudgetAndSave(key.Key, storage.KeyBudget{MaxRequests: 24}, false); err != nil {
		t.Fatal(err)
	}
	gateway := httptest.NewServer(srv.Handler())
	defer gateway.Close()
	client := gateway.Client()
	client.Timeout = 40 * time.Second
	for _, path := range []string{"/v1/models", "/v1/client-profile?model=" + public} {
		status, raw, err := liveJSONRequest(client, gateway.URL, key.Key, path, nil)
		passed := err == nil && status == 200 && strings.Contains(string(raw), public)
		checks = append(checks, map[string]any{"name": path, "passed": passed, "http_status": status})
		if !passed {
			t.Error("real model list/profile check failed", path, status)
		}
	}
	for _, platform := range []string{provider.WorkBuddy, provider.Trae} {
		for p, source := range selected {
			st.SetAccountPaused(source.account.ID, p != platform)
		}
		for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
			for _, stream := range []bool{false, true} {
				marker := "LIVE_OK_" + fmt.Sprint(len(checks))
				prompt := "Reply with exactly " + marker + ". No explanation."
				body := map[string]any{"model": public, "stream": stream, "max_tokens": 128, "messages": []map[string]string{{"role": "user", "content": prompt}}}
				if path == "/v1/responses" {
					delete(body, "messages")
					delete(body, "max_tokens")
					body["input"] = prompt
					body["max_output_tokens"] = 128
				}
				if platform == provider.WorkBuddy {
					body["thinking"] = map[string]string{"type": "disabled"}
				}
				before := total.Load()
				started := time.Now()
				status, raw, err := liveJSONRequest(client, gateway.URL, key.Key, path, body)
				answer, terminal, usage := liveAnswer(path, stream, raw)
				identity := strings.Contains(string(raw), `"model":"`+public+`"`)
				logs := st.Logs(1)
				actual := ""
				attempts := 0
				if len(logs) > 0 {
					actual = logs[0].Model
					attempts = logs[0].Attempts
				}
				passed := err == nil && status == 200 && strings.Contains(answer, marker) && terminal && usage && identity && actual == platform+"/cn/"+rawID && total.Load()-before == 1 && attempts == 1
				checks = append(checks, map[string]any{"name": "text", "platform": platform, "protocol": path, "stream": stream, "http_status": status, "marker_returned": strings.Contains(answer, marker), "terminal": terminal, "usage_present": usage, "public_identity": identity, "actual_model": actual, "attempts": attempts, "elapsed_ms": time.Since(started).Milliseconds(), "passed": passed})
				t.Log("live text", platform, path, stream, "status", status, "passed", passed)
				if !passed {
					t.Error("real protocol check failed; sanitized details in report")
				}
			}
		}
	}
	report["public_name"] = m.Name
	report["native_attempts"] = total.Load()
	report["refresh_attempts"] = observed[provider.WorkBuddy].refreshes.Load() + observed[provider.Trae].refreshes.Load()
	report["passed"] = !t.Failed()
	for _, lease := range st.PoolSnapshot() {
		if lease.InFlight != 0 {
			t.Error("real test leaked lease")
		}
	}
}

func liveError(err error) map[string]any {
	out := map[string]any{"kind": "upstream_or_transport_failure"}
	var p *provider.Error
	if errors.As(err, &p) {
		out["status"] = p.Status
		out["account_fault"] = p.AccountFault
	}
	if errors.Is(err, context.DeadlineExceeded) {
		out["kind"] = "deadline_exceeded"
	}
	// Error strings may contain raw upstream account details; never export them.
	return out
}

func liveSources(t *testing.T) ([]liveSource, map[string]any) {
	t.Helper()
	path := os.Getenv("CODEARTS_UNIFIED_LIVE_STATE")
	if path == "" {
		t.Skip("opt-in real upstream acceptance: CODEARTS_UNIFIED_LIVE_STATE required")
	}
	reportPath := os.Getenv("CODEARTS_UNIFIED_LIVE_REPORT")
	if reportPath == "" {
		t.Fatal("a new private CODEARTS_UNIFIED_LIVE_REPORT path is required before network access")
	}
	if _, err := os.Lstat(reportPath); err == nil || !os.IsNotExist(err) {
		t.Fatal("live report already exists or is not accessible; use a new output path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("live source file not readable")
	}
	before := sha256.Sum256(raw)
	t.Cleanup(func() {
		after, err := os.ReadFile(path)
		if err != nil || sha256.Sum256(after) != before {
			t.Error("read-only source snapshot changed")
		}
	})
	var state storage.State
	if json.Unmarshal(raw, &state) != nil {
		t.Fatal("live source state invalid")
	}
	report := map[string]any{"checked_at": time.Now().UTC().Format(time.RFC3339), "real_upstream": true, "credential_refresh_enabled": false, "reward_claims": 0, "source_schema": state.Version, "source_modified": false}
	rows := []map[string]any{}
	sources := []liveSource{}
	counts := map[string]int{}
	for _, original := range state.Accounts {
		if original == nil || !original.Enabled || original.Paused {
			continue
		}
		var adapter provider.Adapter
		switch original.ProviderID() {
		case provider.WorkBuddy:
			adapter = workbuddy.New()
		case provider.Trae:
			adapter = trae.New()
		default:
			continue
		}
		counts[original.ProviderID()]++
		alias := fmt.Sprintf("%s-%d", original.ProviderID(), counts[original.ProviderID()])
		row := map[string]any{"account": alias, "provider": original.ProviderID(), "scope": original.Scope}
		rows = append(rows, row)
		normalized, identity, err := adapter.Normalize(original.Credentials)
		if err != nil {
			row["normalize_error"] = true
			continue
		}
		row["expires_at"] = identity.ExpiresAt
		if !identity.NonExpiring && identity.ExpiresAt < time.Now().Add(15*time.Minute).Unix() {
			row["skipped"] = "credential_expired_or_near_expiry_no_refresh"
			continue
		}
		if identity.ID != original.ExternalID || identity.Scope != original.Scope {
			row["skipped"] = "credential_identity_mismatch"
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		models, err := adapter.Models(ctx, normalized)
		cancel()
		if err != nil {
			row["models_error"] = liveError(err)
			continue
		}
		row["models"] = models
		ctx, cancel = context.WithTimeout(context.Background(), 20*time.Second)
		credit, err := adapter.Credit(ctx, normalized)
		cancel()
		if err != nil {
			row["credit_error"] = liveError(err)
		} else {
			var c accounts.CreditInfo
			if json.Unmarshal(credit, &c) == nil {
				row["credit"] = map[string]any{"available": c.Available, "kind": c.Kind, "total": c.Total, "used": c.Used, "remaining": c.Remaining}
			}
		}
		account := &accounts.Account{ID: "live-" + alias, Name: alias, Provider: original.ProviderID(), Scope: identity.Scope, ExternalID: identity.ID, Credentials: normalized, Enabled: true, MaxInFlight: 1}
		sources = append(sources, liveSource{account, adapter, models, alias})
	}
	report["accounts"] = rows
	return sources, report
}

func saveLiveReport(t *testing.T, report map[string]any) {
	t.Helper()
	path := os.Getenv("CODEARTS_UNIFIED_LIVE_REPORT")
	if path == "" {
		t.Fatal("CODEARTS_UNIFIED_LIVE_REPORT is required")
	}
	if err := writeLiveReport(path, report); err != nil {
		t.Fatal("live report write failed without replacing existing files")
	}
}

func writeLiveReport(path string, report map[string]any) error {
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(raw, '\n'))
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func TestLiveReportNeverReplacesSourceOrPriorEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	original := []byte(`{"private":"synthetic source"}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeLiveReport(path, map[string]any{"passed": true}); err == nil {
		t.Fatal("existing source overwritten")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatal("source changed")
	}
	newPath := filepath.Join(t.TempDir(), "report.json")
	if err = writeLiveReport(newPath, map[string]any{"passed": true}); err != nil {
		t.Fatal(err)
	}
	if err = writeLiveReport(newPath, map[string]any{"passed": false}); err == nil {
		t.Fatal("prior evidence overwritten")
	}
}

func TestUnifiedLiveDiscovery(t *testing.T) {
	_, report := liveSources(t)
	report["phase"] = "catalog_and_credit_discovery"
	report["inference_requests"] = 0
	saveLiveReport(t, report)
	t.Log("Real catalog/credit discovery complete; sanitized report saved; no refresh or inference")
}

type liveCatalogTransport struct {
	next http.RoundTripper
	rows []map[string]any
}

func (c *liveCatalogTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := c.next.RoundTrip(r)
	if err != nil {
		return response, err
	}
	if r.URL.Path != "/v3/config" && r.URL.Path != trae.ModelsPath {
		return response, nil
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	response.Body.Close()
	if err != nil {
		return nil, err
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	var obj any
	if json.Unmarshal(raw, &obj) != nil {
		return response, nil
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if x["id"] == "deepseek-v4.1-flash" || x["config_name"] == "deepseek-v4.1-flash" {
				fields := map[string]any{}
				var summarize func(string, any, int)
				summarize = func(prefix string, value any, depth int) {
					if depth > 3 {
						return
					}
					switch val := value.(type) {
					case bool:
						fields[prefix] = val
					case string:
						if prefix == "display_config.model_capability" && len(val) < 128 {
							fields[prefix] = val
						} else {
							fields[prefix] = "string"
						}
					case map[string]any:
						for k, v := range val {
							summarize(prefix+"."+k, v, depth+1)
						}
					default:
						fields[prefix] = fmt.Sprintf("%T", value)
					}
				}
				for k, v := range x {
					summarize(k, v, 0)
				}
				c.rows = append(c.rows, fields)
			}
			for _, v := range x {
				walk(v)
			}
		case []any:
			for _, v := range x {
				walk(v)
			}
		}
	}
	walk(obj)
	return response, nil
}

func TestUnifiedLiveCatalogMetadata(t *testing.T) {
	sources, report := liveSources(t)
	rows := []map[string]any{}
	for _, source := range sources {
		var client *http.Client
		switch a := source.adapter.(type) {
		case *workbuddy.Client:
			client = a.HTTP
		case *trae.Client:
			client = a.HTTP
		}
		transport := &liveCatalogTransport{next: client.Transport}
		client.Transport = transport
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, err := source.adapter.Models(ctx, source.account.Credentials)
		cancel()
		row := map[string]any{"account": source.alias, "model_id": "deepseek-v4.1-flash", "field_shapes": transport.rows}
		if err != nil {
			row["error"] = liveError(err)
		}
		rows = append(rows, row)
	}
	report["phase"] = "catalog_metadata_field_shapes"
	report["metadata"] = rows
	report["inference_requests"] = 0
	saveLiveReport(t, report)
}

func TestUnifiedLiveTools(t *testing.T) {
	if os.Getenv("CODEARTS_UNIFIED_LIVE_INFERENCE") != "1" {
		t.Skip("explicit opt-in inference required")
	}
	sources, report := liveSources(t)
	checks := []map[string]any{}
	defer func() {
		report["phase"] = "raw_and_unified_tools"
		report["checks"] = checks
		report["passed"] = !t.Failed()
		saveLiveReport(t, report)
	}()
	st, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = st.UpdateSettings(func(s *config.Settings) {
		s.AutoRefresh = false
		s.AutoClaim = false
		s.WriteBackToIDE = false
		s.RateLimitEnabled = false
		s.MaxRetries = 1
		s.RequestTimeout = 35
		s.WorkBuddy.PromptMode = "passthrough"
		s.WorkBuddy.PromptFallback = false
		s.Trae.PromptFallback = false
	}); err != nil {
		t.Fatal(err)
	}
	selected := map[string]liveSource{}
	for _, source := range sources {
		if _, ok := selected[source.account.ProviderID()]; !ok {
			selected[source.account.ProviderID()] = source
			st.UpsertAccount(source.account)
		}
	}
	if len(selected) != 2 {
		t.Fatal("two real sources required")
	}
	srv, err := app.Build(st)
	if err != nil {
		t.Fatal(err)
	}
	srv.Providers = provider.NewRegistry()
	var attempts atomic.Int32
	observed := map[string]*liveObservedAdapter{}
	for _, p := range []string{provider.WorkBuddy, provider.Trae} {
		a := &liveObservedAdapter{Adapter: selected[p].adapter, total: &attempts, model: "deepseek-v4.1-flash"}
		observed[p] = a
		if err = srv.Providers.Register(a); err != nil {
			t.Fatal(err)
		}
	}
	if err = srv.Catalog.Refresh(context.Background(), catalog.RefreshOperations{Provider: func(_ context.Context, a *accounts.Account) ([]provider.Model, error) {
		return selected[a.ProviderID()].models, nil
	}}); err != nil {
		t.Fatal(err)
	}
	const public = "deepseek-4.1-flash-live-tools-20261105"
	if _, err = st.SaveUnifiedModel(config.UnifiedModel{Name: public, Enabled: true, Sources: []config.ModelSource{{ID: "workbuddy/cn/deepseek-v4.1-flash", Enabled: true}, {ID: "trae/cn/deepseek-v4.1-flash", Enabled: true}}}, nil, 0, st.UnifiedSnapshot().Revision, nil); err != nil {
		t.Fatal(err)
	}
	key, err := st.AddKeyInGroupAndSave("bounded-real-tools", "")
	if err != nil {
		t.Fatal(err)
	}
	gateway := httptest.NewServer(srv.Handler())
	defer gateway.Close()
	client := gateway.Client()
	client.Timeout = 40 * time.Second
	marker := "REAL_TOOL_READ_OK_20261105"
	fixture := filepath.Join(t.TempDir(), "probe.txt")
	if err = os.WriteFile(fixture, []byte(marker), 0600); err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{provider.WorkBuddy, provider.Trae} {
		for p, source := range selected {
			st.SetAccountPaused(source.account.ID, p != platform)
		}
		for _, mode := range []string{"raw", "unified"} {
			model := platform + "/cn/deepseek-v4.1-flash"
			if mode == "unified" {
				model = public
			}
			messages := []any{map[string]any{"role": "user", "content": "Call acceptance_read with path probe.txt. After the tool returns reply with only its exact text. Do not guess file contents."}}
			body := map[string]any{"model": model, "max_tokens": 128, "messages": messages, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "acceptance_read", "description": "Read only probe.txt in the isolated test fixture.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]string{"type": "string"}}, "required": []string{"path"}, "additionalProperties": false}}}}}
			if platform == provider.WorkBuddy {
				body["thinking"] = map[string]string{"type": "disabled"}
			}
			before := attempts.Load()
			status, raw, err := liveJSONRequest(client, gateway.URL, key.Key, "/v1/chat/completions", body)
			row := map[string]any{"platform": platform, "mode": mode, "http_status_first": status, "passed": false}
			var out struct {
				Choices []struct {
					Message      json.RawMessage
					FinishReason string `json:"finish_reason"`
				}
			}
			if err != nil || status != 200 || json.Unmarshal(raw, &out) != nil || len(out.Choices) != 1 {
				row["native_attempts"] = attempts.Load() - before
				checks = append(checks, row)
				t.Error("live tool request rejected", platform, mode, status)
				continue
			}
			var message struct {
				ToolCalls []struct {
					ID       string
					Function struct{ Name, Arguments string }
				} `json:"tool_calls"`
			}
			if json.Unmarshal(out.Choices[0].Message, &message) != nil || len(message.ToolCalls) != 1 || message.ToolCalls[0].Function.Name != "acceptance_read" || out.Choices[0].FinishReason != "tool_calls" {
				checks = append(checks, row)
				t.Error("live tool call missing", platform, mode)
				continue
			}
			call := message.ToolCalls[0]
			var args map[string]string
			if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || args["path"] != "probe.txt" {
				checks = append(checks, row)
				t.Error("live tool arguments invalid", platform, mode)
				continue
			}
			result, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			body["messages"] = append(messages, out.Choices[0].Message, map[string]any{"role": "tool", "tool_call_id": call.ID, "content": string(result)})
			status, raw, err = liveJSONRequest(client, gateway.URL, key.Key, "/v1/chat/completions", body)
			answer, terminal, usage := liveAnswer("/v1/chat/completions", false, raw)
			passed := err == nil && status == 200 && strings.Contains(answer, marker) && terminal && usage && attempts.Load()-before == 2
			row["http_status_second"], row["tool_executed"], row["marker_returned"], row["native_attempts"], row["passed"] = status, true, strings.Contains(answer, marker), attempts.Load()-before, passed
			checks = append(checks, row)
			if !passed {
				t.Error("live tool roundtrip failed", platform, mode)
			}
		}
	}
	report["native_attempts"] = attempts.Load()
	report["refresh_attempts"] = observed[provider.WorkBuddy].refreshes.Load() + observed[provider.Trae].refreshes.Load()
}
