package vertex

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const nativeReply = "data: {\"candidates\":[{\"index\":0,\"content\":{\"parts\":[{\"text\":\"VERTEX_OK\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":12,\"candidatesTokenCount\":3}}\n\n"

func clientFixture(t *testing.T) (*Client, json.RawMessage) {
	a, _ := fixture(t)
	a.Models = []string{"gemini-fixture"}
	a.Access = "fixture-token"
	a.Expires = time.Now().Add(time.Hour).Unix()
	b, _ := json.Marshal(a)
	return New(), b
}

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
func TestImportExchangesThenVerifiesBeforeSaving(t *testing.T) {
	c, raw := clientFixture(t)
	var a credentials
	_ = json.Unmarshal(raw, &a)
	a.Access = ""
	a.Expires = 0
	raw, _ = json.Marshal(a)
	reg := provider.NewRegistry()
	reg.Register(c)
	store := &importStore{fail: true}
	login := accounts.NewProviderLogins(store, func(int) string { return "fixture" })
	inferOK := false
	tokens, generations := 0, 0
	c.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == tokenURL {
			tokens++
			return response(200, `{"access_token":"verified-token","token_type":"Bearer","expires_in":3600}`), nil
		}
		generations++
		if r.Header.Get("Authorization") != "Bearer verified-token" {
			t.Error("unverified token used")
		}
		if !inferOK {
			return response(403, `{"error":"private-secret"}`), nil
		}
		resp := response(200, nativeReply)
		resp.Header.Set("Content-Type", "text/event-stream")
		return resp, nil
	})
	req := accounts.ProviderImportRequest{Provider: provider.Vertex, Credentials: raw}
	if _, err := login.Import(t.Context(), req, reg); err == nil || store.saved != nil {
		t.Fatal("failed inference saved")
	}
	inferOK = true
	if _, err := login.Import(t.Context(), req, reg); err == nil || store.saved != nil {
		t.Fatal("save failure hidden")
	}
	store.fail = false
	if _, err := login.Import(t.Context(), req, reg); err != nil || store.saved == nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(store.saved.Credentials), "verified-token") || tokens != 3 || generations != 3 {
		t.Fatal("prepared token not persisted", tokens, generations)
	}
}
func TestNativeStreamToolsImagesAndErrors(t *testing.T) {
	c, raw := clientFixture(t)
	calls := 0
	c.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "aiplatform.googleapis.com" || r.URL.Path != "/v1/projects/test-project/locations/global/publishers/google/models/gemini-fixture:streamGenerateContent" || r.URL.Query().Get("alt") != "sse" {
			t.Fatal(r.URL)
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "inlineData") || !strings.Contains(string(b), "functionDeclarations") {
			t.Fatal(string(b))
		}
		resp := response(200, "data: {\"candidates\":[{\"index\":0,\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"lookup\",\"args\":{\"id\":9007199254740993}},\"thoughtSignature\":\"signed-fixture\"}]},\"finishReason\":\"STOP\"}]}\n\n")
		resp.Header.Set("Content-Type", "text/event-stream")
		return resp, nil
	})
	body := json.RawMessage(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}}]}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`)
	var out strings.Builder
	if err := c.Stream(t.Context(), raw, provider.Request{Model: "gemini-fixture", Body: body}, func(b []byte) error { out.Write(b); return nil }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "9007199254740993") || !strings.Contains(out.String(), "signed-fixture") {
		t.Fatal(out.String())
	}
	if err := c.Stream(t.Context(), raw, provider.Request{Model: "gemini-unconfigured", Body: body}, func([]byte) error { return nil }); err == nil || calls != 1 {
		t.Fatal("allowlist bypass")
	}
	models, err := c.Models(t.Context(), raw)
	if err != nil || len(models) != 1 || calls != 1 || models[0].ImageInput != "" {
		t.Fatal("catalog invented capability or request", err)
	}
	for _, status := range []int{401, 403, 429, 503} {
		c.HTTP.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return response(status, "private-secret"), nil })
		err := c.Stream(t.Context(), raw, provider.Request{Model: "gemini-fixture", Body: body}, func([]byte) error { return nil })
		var e *provider.Error
		if !errors.As(err, &e) || e.Status != status || strings.Contains(err.Error(), "private-secret") {
			t.Fatal(err)
		}
	}
	c.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.Stream(ctx, raw, provider.Request{Model: "gemini-fixture", Body: body}, func([]byte) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
