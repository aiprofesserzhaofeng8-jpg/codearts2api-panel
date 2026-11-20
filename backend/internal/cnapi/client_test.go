// SPDX-License-Identifier: LGPL-3.0-only
// Adapted Sub2API protocol mappings/test cases; see UPSTREAM.md.
package cnapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/cnapi"
	"codearts2api/internal/provider"
	"codearts2api/internal/upstream/deepseek"
	"codearts2api/internal/upstream/kimi"
	"codearts2api/internal/upstream/minimax"
	"codearts2api/internal/upstream/zhipu"
)

var key = json.RawMessage(`{"api_key":"fixture-key"}`)
var factories = []func() *cnapi.Client{deepseek.New, zhipu.New, kimi.New, minimax.New}

const goodStream = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":4,\"prompt_cache_hit_tokens\":3}}\n\ndata: [DONE]\n\n"

func TestFourOfficialAdapters(t *testing.T) {
	for _, factory := range factories {
		c := factory()
		t.Run(c.ID, func(t *testing.T) {
			if c.Describe().Login {
				t.Fatal("API keys must not advertise OAuth")
			}
			b, id, e := c.Normalize(key)
			if e != nil || !id.NonExpiring || id.ExpiresAt != 0 || id.HasRefresh || strings.Contains(id.ID, "fixture") {
				t.Fatal(id, e)
			}
			again, second, e := c.Normalize(b)
			if e != nil || string(again) != string(b) || second != id {
				t.Fatal("unstable normalize")
			}
			var count int
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count++
				if r.Header.Get("Authorization") != "Bearer fixture-key" {
					t.Error("auth")
				}
				if r.URL.Path == "/models" {
					io.WriteString(w, `{"data":[{"id":"glm-5.3","context_window":12345}]}`)
					return
				}
				var o map[string]json.RawMessage
				json.NewDecoder(r.Body).Decode(&o)
				if r.URL.Path != "/chat/completions" || string(o["model"]) != `"glm-5.3"` || !strings.Contains(string(o["messages"]), "9007199254740993") || !strings.Contains(string(o["messages"]), "real thinking") || !strings.Contains(string(o["messages"]), "image_url") {
					t.Error("lossy request", string(o["messages"]))
				}
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, goodStream)
			}))
			defer up.Close()
			c.Base = up.URL
			if _, e = c.Verify(context.Background(), key); e != nil {
				t.Fatal(e)
			}
			models, e := c.Models(context.Background(), key)
			if e != nil || len(models) != 1 || models[0].Provider != c.ID || models[0].Context != 12345 {
				t.Fatal(models, e)
			}
			var chunks []string
			body := []byte(`{"messages":[{"role":"assistant","content":"","reasoning_content":"real thinking","tool_calls":[{"id":"x","type":"function","function":{"name":"read","arguments":"{\"n\":9007199254740993}"}}]},{"role":"tool","tool_call_id":"x","content":"ok"},{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AQID"}}]}],"tools":[{"type":"function","function":{"name":"read","parameters":{"type":"object"}}}]}`)
			e = c.Stream(context.Background(), key, provider.Request{Model: "glm-5.3", Body: body}, func(b []byte) error { chunks = append(chunks, string(b)); return nil })
			if e != nil || len(chunks) != 2 || count != 3 || !strings.Contains(chunks[1], "prompt_cache_hit_tokens") {
				t.Fatal(e, chunks, count)
			}
		})
	}
}
func TestInvalidCredentialsAndNoSecretInErrors(t *testing.T) {
	for _, factory := range factories {
		c := factory()
		for _, raw := range []string{`{}`, `{"api_key":"x\ny"}`, `{"api_key":"x","scope":"global"}`, `{"api_key":"x","account_mode":"coding"}`, `{"api_key":"x","access_token":"secret"}`, `{"api_key":"x","base_url":"http://localhost"}`} {
			if c.Coding != nil && raw == `{"api_key":"x","account_mode":"coding"}` {
				continue
			}
			if _, _, e := c.Normalize([]byte(raw)); e == nil {
				t.Fatal(c.ID, raw)
			}
		}
	}
}
func TestIncompleteAndErrorStreams(t *testing.T) {
	for _, body := range []string{"data: [DONE]\n\n", "data: {}\n\n", `data: {"error":{"message":"secret"}}` + "\n\n", strings.Split(goodStream, "data: [DONE]")[0], "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"} {
		c := deepseek.New()
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, body)
		}))
		c.Base = up.URL
		e := c.Stream(context.Background(), key, provider.Request{Model: "m", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil })
		up.Close()
		if e == nil || strings.Contains(e.Error(), "secret") {
			t.Fatal(e, body)
		}
	}
}
func TestFallbackDoesNotMaskAuthOrQuota(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := minimax.New()
			calls := 0
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path == "/models" {
					w.WriteHeader(status)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, goodStream)
			}))
			defer up.Close()
			c.Base = up.URL
			_, e := c.Verify(context.Background(), key)
			if status == 404 {
				if e != nil || calls != 2 {
					t.Fatal(e, calls)
				}
			} else if e == nil || calls != 1 {
				t.Fatal(e, calls)
			}
		})
	}
}
func TestRedirectAndCancellation(t *testing.T) {
	hit := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer target.Close()
	c := deepseek.New()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer up.Close()
	c.Base = up.URL
	if _, e := c.Models(context.Background(), key); e == nil || hit {
		t.Fatal("followed credential redirect", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := c.Models(ctx, key); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	c.Base = slow.URL
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, e := c.Models(ctx, key); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
}
func TestBalanceUnknownZeroAndCurrencies(t *testing.T) {
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{`{}`, false}, {`{"balance_infos":[]}`, false}, {`{"balance_infos":[{"currency":"CNY","total_balance":"NaN"}]}`, false}, {`{"balance_infos":[{"currency":"CNY","total_balance":"0"},{"currency":"USD","total_balance":"1.25"}]}`, true},
	} {
		c := deepseek.New()
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, tc.body) }))
		c.BalanceURL = up.URL
		b, e := c.Credit(context.Background(), key)
		up.Close()
		if (e == nil) != tc.ok {
			t.Fatal(string(b), e)
		}
		if tc.ok && (!strings.Contains(string(b), "USD") || !strings.Contains(string(b), `"remaining":0`)) {
			t.Fatal(string(b))
		}
	}
	for _, tc := range []struct {
		body string
		ok   bool
	}{{`{"code":0,"data":{}}`, false}, {`{"code":1,"data":{"available_balance":0}}`, false}, {`{"code":0,"data":{"available_balance":0}}`, true}} {
		c := kimi.New()
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, tc.body) }))
		c.BalanceURL = up.URL
		_, e := c.Credit(context.Background(), key)
		up.Close()
		if (e == nil) != tc.ok {
			t.Fatal(e, tc.body)
		}
	}
	for _, c := range []*cnapi.Client{zhipu.New(), minimax.New()} {
		b, e := c.Credit(context.Background(), key)
		if e != nil || !strings.Contains(string(b), `"kind":"unsupported"`) {
			t.Fatal(string(b), e)
		}
	}
}

// Imports use the actual existing coordinator, including its strict save result.
type importStore struct {
	saved *accounts.Account
	fail  bool
}

func (s *importStore) Account(string) *accounts.Account                             { return s.saved }
func (s *importStore) FindProviderAccount(string, string, string) *accounts.Account { return s.saved }
func (s *importStore) UpsertAccountAndSave(a *accounts.Account, _ bool) (bool, error) {
	if s.fail {
		return false, errors.New("fixture disk failure")
	}
	s.saved = a
	return true, nil
}
func TestVerifiedImportPropagatesSaveFailure(t *testing.T) {
	for _, factory := range factories {
		c := factory()
		t.Run(c.ID, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"data":[{"id":"glm-5.3"}]}`) }))
			defer up.Close()
			c.Base = up.URL
			store := &importStore{fail: true}
			reg := provider.NewRegistry()
			if e := reg.Register(c); e != nil {
				t.Fatal(e)
			}
			login := accounts.NewProviderLogins(store, func(int) string { return "fixture" })
			req := accounts.ProviderImportRequest{Provider: c.ID, Credentials: key}
			if _, e := login.Import(context.Background(), req, reg); e == nil || store.saved != nil {
				t.Fatal("false successful save", e)
			}
			store.fail = false
			if _, e := login.Import(context.Background(), req, reg); e != nil {
				t.Fatal(e)
			}
			if store.saved.Provider != c.ID || store.saved.Scope != "cn" || !store.saved.Enabled {
				t.Fatal("wrong imported account")
			}
		})
	}
}
func TestOfficialEndpoints(t *testing.T) {
	expected := map[string]string{"deepseek": "https://api.deepseek.com/v1", "zhipu": "https://open.bigmodel.cn/api/paas/v4", "kimi": "https://api.moonshot.cn/v1", "minimax": "https://api.minimax.cn/v1"}
	for _, newClient := range factories {
		c := newClient()
		if c.Base != expected[c.ID] {
			t.Fatal(c.ID, c.Base)
		}
	}
}
func TestCallbackFailureStopsStream(t *testing.T) {
	c := deepseek.New()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, goodStream)
	}))
	defer up.Close()
	c.Base = up.URL
	want := errors.New("downstream stopped")
	n := 0
	e := c.Stream(context.Background(), key, provider.Request{Model: "m", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { n++; return want })
	if !errors.Is(e, want) || n != 1 {
		t.Fatal(e, n)
	}
}

func TestToolAndReasoningStreamPreserved(t *testing.T) {
	for _, factory := range factories {
		c := factory()
		t.Run(c.ID, func(t *testing.T) {
			chunks := []string{`{"choices":[{"index":0,"delta":{"reasoning_content":"reasoning","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"n\":9007199254740993}"}}]},"finish_reason":null}]}`, `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`}
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				for _, b := range chunks {
					fmt.Fprintf(w, "data: %s\n\n", b)
				}
				io.WriteString(w, "data: [DONE]\n\n")
			}))
			defer up.Close()
			c.Base = up.URL
			var got []string
			e := c.Stream(context.Background(), key, provider.Request{Model: "m", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func(b []byte) error { got = append(got, string(b)); return nil })
			if e != nil || len(got) != 2 || got[0] != chunks[0] || got[1] != chunks[1] {
				t.Fatal(e, got)
			}
		})
	}
}
