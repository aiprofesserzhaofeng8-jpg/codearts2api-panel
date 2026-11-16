package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codearts2api/internal/accounts"
	"codearts2api/internal/protocol"
	"codearts2api/internal/provider"
)

var fixtureKey = json.RawMessage(`{"api_key":"fixture-key"}`)

func TestNormalizeAndEndpoints(t *testing.T) {
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := New()
	if c.Base != "https://generativelanguage.googleapis.com/v1beta/openai" || c.ModelsURL != "https://generativelanguage.googleapis.com/v1beta/models" {
		t.Fatal("wrong endpoint")
	}
	raw, id, err := c.Normalize(fixtureKey)
	if err != nil || id.Scope != "api" || !id.NonExpiring || id.HasRefresh || id.ExpiresAt != 0 || strings.Contains(id.ID, "fixture-key") {
		t.Fatal(id, err)
	}
	_, again, _ := c.Normalize(raw)
	if again.ID != id.ID {
		t.Fatal("unstable identity")
	}
	for _, raw := range []string{`{"api_key":"fixture-key","scope":"cli"}`, `{"api_key":"fixture-key","scope":"cn"}`, `{"access_token":"fixture-key"}`, `{"api_key":"fixture-key","base_url":"http://localhost"}`, `{"api_key":"bad\r\nkey"}`} {
		if _, _, err := c.Normalize(json.RawMessage(raw)); err == nil {
			t.Fatal("accepted invalid credential")
		}
	}
	credit, err := c.Credit(context.Background(), fixtureKey)
	if err != nil || !strings.Contains(string(credit), `"kind":"unsupported"`) {
		t.Fatal(string(credit), err)
	}
}

func TestNativeCatalogPagination(t *testing.T) {
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/models" || r.URL.Query().Get("key") != "" || r.Header.Get("x-goog-api-key") != "fixture-key" {
			t.Error("key or endpoint mismatch")
		}
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			io.WriteString(w, `{"models":[{"name":"models/gemini-2.5-flash","inputTokenLimit":1048576,"outputTokenLimit":65536,"supportedGenerationMethods":["generateContent"]},{"name":"models/embedding-001","supportedGenerationMethods":["embedContent"]},{"name":"models/gemini-image","supportedGenerationMethods":["generateContent"]}],"nextPageToken":"page+2"}`)
		} else {
			if r.URL.Query().Get("pageToken") != "page+2" {
				t.Error("pagination token lost")
			}
			io.WriteString(w, `{"models":[{"name":"models/gemini-2.5-flash","supportedGenerationMethods":["generateContent"]},{"name":"models/gemini-new-future","supportedGenerationMethods":["generateContent"]}]}`)
		}
	}))
	defer up.Close()
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := New()
	c.ModelsURL = up.URL + "/models"
	rows, err := c.Models(context.Background(), fixtureKey)
	if err != nil || len(rows) != 2 || calls != 2 {
		t.Fatal(rows, err, calls)
	}
	if rows[0].Context != 1048576 || rows[0].MaxOutput != 65536 || rows[0].Scope != "api" || rows[0].UpstreamID != "gemini-2.5-flash" || rows[0].ImageInput != "supported" {
		t.Fatal(rows[0])
	}
	if rows[1].ImageInput != "" || rows[1].Context != 0 {
		t.Fatal("invented future capabilities")
	}
}

func TestCatalogErrorsNeverFallback(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{401, `{}`}, {403, `{}`}, {429, `{}`}, {500, `{}`}, {302, `{}`}, {200, `{}`}, {200, `{"models":[]}`},
		{200, `{"models":[{"name":"models/gemini-2.5-flash","inputTokenLimit":-1,"supportedGenerationMethods":["generateContent"]}]}`},
		{200, `{"models":[],"nextPageToken":"repeat"}`},
	} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); io.WriteString(w, tc.body) }))
		t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
		t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
		c := New()
		c.ModelsURL = up.URL
		if _, err := c.Verify(context.Background(), fixtureKey); err == nil {
			t.Error("invalid/empty/unauthorized catalog accepted", tc)
		}
		up.Close()
	}
}

func TestStreamImageToolSignatureRoundTrip(t *testing.T) {
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("wrong inference route")
		}
		b, _ := io.ReadAll(r.Body)
		if calls == 1 {
			if !strings.Contains(string(b), "data:image/png;base64,AAAA") || !strings.Contains(string(b), `"tools"`) {
				t.Error("image/tools lost")
			}
		} else if !strings.Contains(string(b), "OPAQUE_SIGNATURE") || !strings.Contains(string(b), "9007199254740993") || !strings.Contains(string(b), "TOOL_RESULT") {
			t.Error("history lost")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			io.WriteString(w, "data: "+`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call1","type":"function","function":{"name":"lookup","arguments":"{\"n\":9007199254740993}"},"extra_content":{"google":{"thought_signature":"OPAQUE_SIGNATURE"}}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			io.WriteString(w, "data: "+`{"choices":[{"index":0,"delta":{"content":"ROUNDTRIP_OK"},"finish_reason":"stop"}]}`+"\n\n")
		}
		io.WriteString(w, "data: "+`{"choices":[],"usage":{"prompt_tokens":20,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":5},"completion_tokens_details":{"reasoning_tokens":3}}}`+"\n\ndata: [DONE]\n\n")
	}))
	defer up.Close()
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := New()
	c.Base = up.URL
	var chunks [][]byte
	emit := func(b []byte) error { chunks = append(chunks, append([]byte(nil), b...)); return nil }
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`)
	if err := c.Stream(context.Background(), fixtureKey, provider.Request{Model: "gemini-2.5-flash", Body: body}, emit); err != nil {
		t.Fatal(err)
	}
	reply, err := protocol.AggregateSSE(chunks)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Choices []struct {
			Message json.RawMessage `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(reply, &got) != nil || len(got.Choices) != 1 {
		t.Fatal(string(reply))
	}
	followup := []byte(`{"messages":[{"role":"user","content":"lookup"},` + string(got.Choices[0].Message) + `,{"role":"tool","tool_call_id":"call1","content":"TOOL_RESULT"}]}`)
	chunks = nil
	if err := c.Stream(context.Background(), fixtureKey, provider.Request{Model: "gemini-2.5-flash", Body: followup}, emit); err != nil {
		t.Fatal(err)
	}
	reply, err = protocol.AggregateSSE(chunks)
	if err != nil || !strings.Contains(string(reply), "ROUNDTRIP_OK") || !strings.Contains(string(reply), `"reasoning_tokens":3`) || calls != 2 {
		t.Fatal(string(reply), err, calls)
	}
}

func TestStreamCancellationAndErrors(t *testing.T) {
	for _, body := range []string{"data: [DONE]\n\n", "data: {\"error\":{\"message\":\"secret\"}}\n\n", "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n"} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, body)
		}))
		t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
		t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
		c := New()
		c.Base = up.URL
		req := provider.Request{Model: "gemini-2.5-flash", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}
		err := c.Stream(context.Background(), fixtureKey, req, func([]byte) error { return nil })
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid successful stream", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err = c.Stream(ctx, fixtureKey, req, func([]byte) error { return nil })
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		up.Close()
	}
}

// Reuse the existing login coordinator; test actual native discovery before save.
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
func TestVerifiedNativeImportSaveFailure(t *testing.T) {
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("x-goog-api-key") != "fixture-key" || r.Method != "GET" {
			t.Error("wrong verification endpoint")
		}
		io.WriteString(w, `{"models":[{"name":"models/gemini-2.5-flash","supportedGenerationMethods":["generateContent"]}]}`)
	}))
	defer up.Close()
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := New()
	c.ModelsURL = up.URL
	reg := provider.NewRegistry()
	if err := reg.Register(c); err != nil {
		t.Fatal(err)
	}
	store := &importStore{fail: true}
	login := accounts.NewProviderLogins(store, func(int) string { return "fixture" })
	request := accounts.ProviderImportRequest{Provider: "gemini", Credentials: fixtureKey}
	if _, err := login.Import(context.Background(), request, reg); err == nil || store.saved != nil {
		t.Fatal("false success", err)
	}
	store.fail = false
	if _, err := login.Import(context.Background(), request, reg); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || store.saved == nil || store.saved.Provider != "gemini" || store.saved.Scope != "api" || !store.saved.Enabled {
		t.Fatal("incorrect import")
	}
}
