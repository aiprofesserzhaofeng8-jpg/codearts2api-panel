package typesafe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func key() json.RawMessage { return json.RawMessage(`{"api_key":"fixture"}`) }
func input() []byte {
	return []byte(`{"model":"jev-preview","state":{"text":"fixture","integer":9007199254740993},"questions":{"check":{"type":"noul"}}}`)
}
func TestNativeValidation(t *testing.T) {
	for _, b := range []string{string(input()), `{"model":"jev-1.14.0","state":[],"questions":{"q":{"type":"choice","criteria":{"a":null,"b":{"description":"b"}}}}}`, `{"model":"jev-latest","state":"x","questions":{"q":{"type":"score","criteria":["low","high"]}}}`} {
		if _, err := ValidateSystemOneRequest([]byte(b)); err != nil {
			t.Fatal(err)
		}
	}
	for _, b := range []string{
		`{"model":"jev-latest","model":"other","state":"x","questions":{"q":{"type":"noul"}}}`,
		`{"Model":"jev-latest","state":"x","questions":{"q":{"type":"noul"}}}`,
		`{"model":"jev-latest","state":"x","questions":{"q":{"type":"noul"}},"stream":true}`,
		`{"model":"jev-latest","state":"x","questions":{"q":{"type":"box"}}}`,
		`{"model":"jev-latest","state":"x","questions":{"q":{"type":"noul"}},"assets":{"x":{}}}`,
		`{"model":"jev-latest","state":5,"questions":{"q":{"type":"noul"}}}`,
		`{"model":"jev-latest","state":"x","questions":{"q":{"type":"noul"},"q":{"type":"noul"}}}`,
	} {
		if _, err := ValidateSystemOneRequest([]byte(b)); err == nil {
			t.Fatal("accepted", b)
		}
	}
	if _, err := ValidateSystemOneRequest(make([]byte, MaxRequestBytes+1)); err == nil {
		t.Fatal("oversize")
	}
}
func TestFixedNativeRequestAndRawAnswer(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/v1/systemone" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer fixture" || !strings.Contains(string(b), "9007199254740993") {
			t.Error("invalid transport")
		}
		io.WriteString(w, `{"model":"jev-1.14.0","answers":{"check":{"type":"noul","noul":0.75,"metadata":9007199254740993}},"usage":{"input_tokens":3,"output_tokens":0}}`)
	}))
	defer s.Close()
	c := New()
	c.Origin = s.URL
	out, err := c.Decide(context.Background(), key(), input())
	if err != nil || calls != 1 || !out.Usage.Known || out.Usage.InputTokens != 3 || out.Usage.OutputTokens != 0 || !strings.Contains(string(out.Body), "9007199254740993") {
		t.Fatal(out, err, calls)
	}
	if _, err = c.Decide(context.Background(), key(), []byte(`{"messages":[]}`)); err == nil || calls != 1 {
		t.Fatal("invalid request sent")
	}
}
func TestModelsVerifyAndIdentity(t *testing.T) {
	c := New()
	_, id, err := c.Normalize(key())
	if err != nil || !id.NonExpiring || id.HasRefresh {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"api_key":"key","base_url":"http://evil"}`, `{"api_key":"key","scope":"cn"}`, `{"api_key":"key","access_token":"mixed"}`} {
		if _, _, err = c.Normalize([]byte(raw)); err == nil {
			t.Fatal(raw)
		}
	}
	for _, status := range []int{200, 401, 429} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" || r.URL.Path != "/v1/models" {
				t.Error("unexpected inference")
			}
			w.WriteHeader(status)
			io.WriteString(w, `{"models":[{"name":"jev-latest","description":"decision","release_date":"2026-09-15"},{"name":"jev-preview"}]}`)
		}))
		c.Origin = s.URL
		_, err = c.Verify(context.Background(), key())
		if (err == nil) != (status == 200) {
			t.Fatal(status, err)
		}
		s.Close()
	}
}
func TestResponseUnknownUsageAndFailures(t *testing.T) {
	for _, usage := range []string{`null`, `{}`, `{"input_tokens":-1,"output_tokens":4}`, `{"input_tokens":"4","output_tokens":4}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"answers":{"check":{"type":"noul","noul":0.8}},"usage":`+usage+`}`)
		}))
		c := New()
		c.Origin = s.URL
		out, err := c.Decide(context.Background(), key(), input())
		if err != nil || out.Usage.Known {
			t.Fatal(out, err)
		}
		s.Close()
	}
	for _, body := range []string{`{}`, `{"answers":{}}`, `{"answers":{"check":null}}`, `{"answers":{"check":{"type":"choice"}}}`, `{"error":{"message":"secret"}}`, strings.Repeat("x", MaxResponseBytes+1)} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
		c := New()
		c.Origin = s.URL
		if _, err := c.Decide(context.Background(), key(), input()); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("bad response accepted or leaked", err)
		}
		s.Close()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Decide(ctx, key(), input()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestRedirectDoesNotForwardKey(t *testing.T) {
	calls := 0
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer dest.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, dest.URL, 307) }))
	defer source.Close()
	c := New()
	c.Origin = source.URL
	if _, err := c.Decide(context.Background(), key(), input()); err == nil || calls != 0 {
		t.Fatal("followed redirect", err, calls)
	}
}
