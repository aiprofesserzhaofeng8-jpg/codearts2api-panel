package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"codearts2api/internal/requestpolicy"
	"codearts2api/internal/store"
	"codearts2api/internal/trae"
	"codearts2api/internal/workbuddy"
)

const mappedPublic = "deepseek-4.1-flash"

var mappedSources = []string{"test-model", "workbuddy/cn/shared", "trae/cn/shared"}

func mappedFixture(t *testing.T, respond func(string, http.ResponseWriter, *http.Request)) (*Server, string, store.AccountGroup) {
	t.Helper()
	handler := func(platform string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			text := string(raw)
			if !strings.Contains(text, requestpolicy.SummaryGuidance) && !strings.Contains(text, requestpolicy.SummaryRetryGuidance) && (!strings.Contains(text, platform+"-PROMPT") || strings.Contains(text, "CLIENT-PROMPT") || strings.Contains(text, mappedPublic)) {
				t.Errorf("wrong platform prompt or public ID leaked to %s: %s", platform, text)
			}
			for _, other := range []string{provider.CodeArts, provider.WorkBuddy, provider.Trae} {
				if other != platform && strings.Contains(text, other+"-PROMPT") {
					t.Errorf("other provider prompt leaked to %s", platform)
				}
			}
			r.Body = io.NopCloser(strings.NewReader(text))
			if respond != nil {
				respond(platform, w, r)
				return
			}
			var req struct{ Stream bool }
			_ = json.Unmarshal(raw, &req)
			policySuccess(w, platform, req.Stream || platform != provider.CodeArts)
		}
	}
	s, _ := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		handler(provider.CodeArts)(w, r)
		return w.Result(), nil
	})
	s.Store.UpdateSettings(func(st *store.Settings) {
		st.MaxRetries = 2
		st.Pool.Strategy = "sequential_exhaust"
		st.CodeArts.PromptMode = "custom"
		st.CodeArts.PromptText = provider.CodeArts + "-PROMPT"
		st.WorkBuddy.PromptMode = "custom"
		st.WorkBuddy.PromptText = provider.WorkBuddy + "-PROMPT"
		st.Trae.PromptMode = "custom"
		st.Trae.PromptText = provider.Trae + "-PROMPT"
	})
	s.Providers = provider.NewRegistry()
	wbServer := httptest.NewServer(handler(provider.WorkBuddy))
	t.Cleanup(wbServer.Close)
	wb := workbuddy.New()
	wb.CNBase = wbServer.URL
	_ = s.Providers.Register(wb)
	traeServer := httptest.NewServer(handler(provider.Trae))
	t.Cleanup(traeServer.Close)
	tr := trae.New()
	tr.AgentBase = traeServer.URL
	_ = s.Providers.Register(tr)
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	addTrae(t, s)
	// Catalog bounds yield safe client recommendations for both additional providers.
	for _, p := range []string{provider.WorkBuddy, provider.Trae} {
		s.Reg.ReplaceProvider(p, "cn", []provider.Model{{ID: "shared", StreamOnly: true, Context: 131072, MaxOutput: 8192}})
	}
	g, err := s.Store.SaveGroup(store.AccountGroup{Name: "mapped", AccountIDs: []string{"a", "wb-cn", "trae-acct"}, AllowlistEnabled: true, ModelIDs: slices.Clone(mappedSources), ModelMappings: []store.ModelMapping{{Name: mappedPublic, Sources: slices.Clone(mappedSources)}}})
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.Store.AddKeyInGroupAndSave("mapped", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Handler()
	s.cache = newModelCache()
	s.cache.loadedAt = time.Now()
	return s, k.Key, g
}
func mappedBody(path string, stream bool, model string) string {
	req := map[string]any{"model": model, "stream": stream, "max_tokens": 64, "messages": []map[string]string{{"role": "user", "content": "hi"}}, "system": "CLIENT-PROMPT"}
	if path == "/v1/chat/completions" {
		delete(req, "system")
		req["messages"] = []map[string]string{{"role": "system", "content": "CLIENT-PROMPT"}, {"role": "user", "content": "hi"}}
	}
	if path == "/v1/responses" {
		delete(req, "messages")
		delete(req, "system")
		req["input"] = "hi"
		req["instructions"] = "CLIENT-PROMPT"
	}
	return string(mustJSON(req))
}
func onlyMappedPlatform(s *Server, p string) {
	for _, a := range s.Store.Accounts() {
		s.Store.SetAccountPaused(a.ID, a.ProviderID() != p)
	}
}
func TestMappedModelAllProtocolsChoosePhysicalSource(t *testing.T) {
	for _, p := range []string{provider.CodeArts, provider.WorkBuddy, provider.Trae} {
		for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%v", p, path, stream), func(t *testing.T) {
					s, key, _ := mappedFixture(t, nil)
					onlyMappedPlatform(s, p)
					w := responseRequest(s, key, path, mappedBody(path, stream, mappedPublic))
					if w.Code != 200 || !(strings.Contains(w.Body.String(), "Hello") || strings.Contains(w.Body.String(), "OK") || strings.Contains(w.Body.String(), "TRAE_COMPLETE")) {
						t.Fatalf("completion %d %s", w.Code, w.Body.String())
					}
					if !strings.Contains(w.Body.String(), `"model":"`+mappedPublic+`"`) {
						t.Fatal("public response identity missing", w.Body.String())
					}
					l := s.Store.Logs(1)[0]
					if l.RequestedModel != mappedPublic || l.Provider != p || !slices.Contains(mappedSources, l.Model) || l.Model == mappedPublic || len(l.AttemptDetails) != 1 || l.AttemptDetails[0].Model != l.Model || l.Usage.InputTokens != 12 {
						t.Fatalf("wrong physical accounting: %+v", l)
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
func TestMappedModelSameNameSwitchesProviderAndSticks(t *testing.T) {
	s, key, _ := mappedFixture(t, nil)
	s.Store.SetAccountPaused("a", true)
	call := func() store.LogEntry {
		w := responseRequest(s, key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, mappedPublic))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		return s.Store.Logs(1)[0]
	}
	first := call()
	second := call()
	if first.Provider != provider.WorkBuddy || second.Provider != first.Provider {
		t.Fatal("session did not stick")
	}
	s.Store.SetAccountPaused("wb-cn", true)
	third := call()
	if third.Provider != provider.Trae {
		t.Fatalf("did not switch platform: %+v", third)
	}
}
func TestMappedModelRetryDoesNotReusePreviousProviderBody(t *testing.T) {
	var wb, tr atomic.Int32
	s, key, _ := mappedFixture(t, func(p string, w http.ResponseWriter, r *http.Request) {
		if p == provider.WorkBuddy {
			wb.Add(1)
			w.WriteHeader(429)
			io.WriteString(w, `{"error":{"message":"rate limit"}}`)
			return
		}
		if p == provider.Trae {
			tr.Add(1)
		}
		policySuccess(w, p, true)
	})
	s.Store.SetAccountPaused("a", true)
	w := responseRequest(s, key, "/v1/messages", mappedBody("/v1/messages", true, mappedPublic))
	if w.Code != 200 || wb.Load() != 1 || tr.Load() != 1 {
		t.Fatal("cross platform retry", w.Code, w.Body.String(), wb.Load(), tr.Load())
	}
	l := s.Store.Logs(1)[0]
	if l.Provider != provider.Trae || len(l.AttemptDetails) != 2 || l.AttemptDetails[0].Model != "workbuddy/cn/shared" || l.AttemptDetails[1].Model != "trae/cn/shared" {
		t.Fatalf("incorrect attempts %+v", l)
	}
}
func TestMappedModelCatalogExplicitCallsAndPermissions(t *testing.T) {
	s, key, g := mappedFixture(t, nil)
	ids := modelIDsFromResponse(t, scopedModelRequest(s, key))
	if !slices.Equal(ids, []string{mappedPublic}) {
		t.Fatal("not deduplicated", ids)
	}
	// A raw CodeArts ID sharing the public spelling must not steal the public route.
	s.Reg.ReplaceProvider(provider.CodeArts, "", []provider.Model{{ID: "test-model"}, {ID: mappedPublic}})
	onlyMappedPlatform(s, provider.WorkBuddy)
	if w := responseRequest(s, key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, mappedPublic)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, id := range []string{"workbuddy/cn/shared", "trae/cn/shared", "codearts/test-model"} {
		p, _, _ := provider.ParseModel(id)
		onlyMappedPlatform(s, p)
		w := responseRequest(s, key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, id))
		if w.Code != 200 {
			t.Fatal("direct hidden model failed", id, w.Code, w.Body.String())
		}
	}
	for _, name := range []string{"test-model", "shared", "deepseek"} {
		w := responseRequest(s, key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, name))
		if w.Code != 404 {
			t.Fatal("unmapped short name bypass", name, w.Code)
		}
	}
	g.ModelMappings = []store.ModelMapping{{Name: mappedPublic, Sources: []string{"workbuddy/cn/shared"}}}
	g.AccountIDs = []string{"wb-cn"}
	g.ModelIDs = []string{"workbuddy/cn/shared"}
	if _, err := s.Store.SaveGroup(g); err != nil {
		t.Fatal(err)
	}
	if w := responseRequest(s, key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, "trae/cn/shared")); w.Code != 403 {
		t.Fatal("direct ID bypass", w.Code, w.Body.String())
	}
	// The same public name has an independent source set in another group.
	other, err := s.Store.SaveGroup(store.AccountGroup{Name: "other", AccountIDs: []string{"trae-acct"}, ModelMappings: []store.ModelMapping{{Name: mappedPublic, Sources: []string{"trae/cn/shared"}}}})
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.Store.AddKeyInGroupAndSave("other", other.ID)
	if err != nil {
		t.Fatal(err)
	}
	onlyMappedPlatform(s, provider.Trae)
	if w := responseRequest(s, k.Key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, mappedPublic)); w.Code != 200 {
		t.Fatal("other group", w.Code, w.Body.String())
	}
	if w := responseRequest(s, key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, mappedPublic)); w.Code != 503 {
		t.Fatal("unavailable group leaked", w.Code, w.Body.String())
	}

}
func TestMappedModelThinkingExcludesTrae(t *testing.T) {
	s, key, _ := mappedFixture(t, nil)
	s.Store.SetAccountPaused("a", true)
	req := map[string]any{"model": mappedPublic, "messages": []map[string]string{{"role": "user", "content": "hi"}}, "max_tokens": 2048, "thinking": map[string]any{"type": "disabled"}}
	w := responseRequest(s, key, "/v1/messages", string(mustJSON(req)))
	if w.Code != 200 || s.Store.Logs(1)[0].Provider != provider.WorkBuddy {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestMappedModelPlanRejectsDeletedMapping(t *testing.T) {
	s, key, g := mappedFixture(t, nil)
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	r = r.WithContext(context.WithValue(r.Context(), groupKeyContext{}, key))
	r = beginInference(httptest.NewRecorder(), r, "chat")
	m, err := s.resolveModelForRequest(r, s.Store.Settings(), mappedPublic)
	if err != nil {
		t.Fatal(err)
	}
	g.ModelMappings = nil
	if _, err = s.Store.SaveGroup(g); err != nil {
		t.Fatal(err)
	}
	called := false
	_, err = s.withAccountRetry(withRoutingModel(r.Context(), m.ID), time.Second, func(context.Context, *codearts.Account) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("deleted mapping permitted")
	}
}

func TestMappedModelProfileDoesNotResolveNameCollision(t *testing.T) {
	s, key, _ := mappedFixture(t, nil)
	s.Reg.ReplaceProvider(provider.CodeArts, "", []provider.Model{{ID: "test-model"}, {ID: mappedPublic}})
	p := store.DefaultModelPolicies()["deepseek-v4.1-flash"]
	if err := s.Store.SetModelPolicyAndSave(mappedPublic, p); err != nil {
		t.Fatal(err)
	}
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/v1/client-profile?model="+mappedPublic, nil)
		r.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := request(); w.Code != 422 {
		t.Fatal("borrowed profile from unrelated model", w.Code, w.Body.String())
	}
	if err := s.Store.SetModelPolicyAndSave("test-model", p); err != nil {
		t.Fatal(err)
	}
	if w := request(); w.Code != 200 || !strings.Contains(w.Body.String(), mappedPublic) || strings.Contains(w.Body.String(), `"enabled":true`) {
		t.Fatal("unsafe combined recommendations", w.Code, w.Body.String())
	}
}

func TestMappedModelNoFailoverAfterStreamOutput(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
		t.Run(path, func(t *testing.T) {
			var calls atomic.Int32
			s, key, _ := mappedFixture(t, func(p string, w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"VISIBLE\"}}]}\n\ndata: {\"error\":{\"message\":\"failure\",\"code\":500}}\n\n")
			})
			s.Store.SetAccountPaused("a", true)
			w := responseRequest(s, key, path, mappedBody(path, true, mappedPublic))
			if calls.Load() != 1 || !strings.Contains(w.Body.String(), "VISIBLE") {
				t.Fatal("retried after visible output", calls.Load(), w.Code, w.Body.String())
			}
		})
	}
}
func TestMappedModelSummaryFallbackKeepsRawPermissions(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		t.Run(fmt.Sprint(allowed), func(t *testing.T) {
			calls := 0
			s, key, g := mappedFixture(t, func(p string, w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "text/event-stream")
				finish, text := "length", "PRIVATE"
				if calls > 1 {
					finish, text = "stop", "COMPLETE"
				}
				io.WriteString(w, summaryFixture(text, finish, 10, 4))
			})
			onlyMappedPlatform(s, provider.CodeArts)
			s.Reg.ReplaceProvider(provider.CodeArts, "", []provider.Model{{ID: "test-model"}, {ID: "backup-model"}})
			p := store.DefaultModelPolicies()["deepseek-v4.1-flash"]
			p.SummaryFallbackModel = "backup-model"
			if err := s.Store.SetModelPolicyAndSave("test-model", p); err != nil {
				t.Fatal(err)
			}
			if allowed {
				g.ModelIDs = append(g.ModelIDs, "backup-model")
				if _, err := s.Store.SaveGroup(g); err != nil {
					t.Fatal(err)
				}
			}
			r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(mappedBody("/v1/chat/completions", false, mappedPublic)))
			r.Header.Set("Authorization", "Bearer "+key)
			r.Header.Set("X-CodeArts-Request-Kind", "summary")
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if strings.Contains(w.Body.String(), "PRIVATE") {
				t.Fatal("incomplete summary leaked")
			}
			if allowed {
				if w.Code != 200 || calls != 2 || s.Store.Logs(1)[0].Model != "backup-model" {
					t.Fatal("fallback lost", w.Code, w.Body.String(), calls)
				}
			} else if calls != 1 || w.Code != 403 {
				t.Fatal("fallback allowlist bypass", calls, w.Code, w.Body.String())
			}
		})
	}
}
func TestMappedResponsePreservesOpaqueData(t *testing.T) {
	if _, err := mappedResponseModel([]byte("null"), mappedPublic); err == nil {
		t.Fatal("null response accepted")
	}
	raw := []byte(`{"model":"physical","id":9007199254740993,"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"{\"id\":9007199254740993}"}}]}}],"usage":{"prompt_tokens":9007199254740993}}`)
	out, err := mappedResponseModel(raw, mappedPublic)
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]json.RawMessage
	_ = json.Unmarshal(raw, &before)
	_ = json.Unmarshal(out, &after)
	delete(before, "model")
	delete(after, "model")
	if string(mustJSON(before)) != string(mustJSON(after)) {
		t.Fatal("model rewrite modified opaque fields")
	}
}

func TestMappedModelBudgetUsesPhysicalPrice(t *testing.T) {
	s, key, _ := mappedFixture(t, nil)
	onlyMappedPlatform(s, provider.WorkBuddy)
	if err := s.Store.SetKeyBudgetAndSave(key, store.KeyBudget{MaxUSD: 1}, false); err != nil {
		t.Fatal(err)
	}
	// A configured price on the public spelling must not make an unpriced upstream usable.
	if err := s.Store.SetPriceAndSave(mappedPublic, store.ModelPrice{Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	w := responseRequest(s, key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, mappedPublic))
	if w.Code < 400 {
		t.Fatal("budget accepted missing physical price")
	}
	if err := s.Store.SetPriceAndSave("workbuddy/cn/shared", store.ModelPrice{Currency: "USD", Input: 2, Output: 5, Cached: 1}); err != nil {
		t.Fatal(err)
	}
	w = responseRequest(s, key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, mappedPublic))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	l := s.Store.Logs(1)[0]
	if l.EstimatedCost == nil || l.EstimatedCost.Price.Input != 2 || l.EstimatedCost.Amount <= 0 {
		t.Fatalf("incorrect physical price %+v", l)
	}
}

func BenchmarkMappedResponseModel(b *testing.B) {
	raw := []byte(`{"id":"chunk-fixture","model":"workbuddy/cn/deepseek-v4.1-flash","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"one token"}}]}`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := mappedResponseModel(raw, mappedPublic); err != nil {
			b.Fatal(err)
		}
	}
}
