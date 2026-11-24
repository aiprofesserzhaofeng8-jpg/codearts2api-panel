package server

import (
	"codearts2api/internal/storage"
	"codearts2api/internal/websearch"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExternalSearchRouterIsolation(t *testing.T) {
	s := newTestServer(t, 0, 1, "external-fixture")
	k := s.Store.Keys()[0]
	s.Search = websearch.New()
	status := 200
	calls := 0
	s.Search.HTTP.Transport = probeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fixture-private-key" {
			t.Error("incorrect upstream credentials")
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"results":[{"title":"Source","url":"https://example.org/paper","summary":"evidence"}]}`)), Header: make(http.Header)}, nil
	})
	name, engine, credential, on := "Research", "deepseek", "fixture-private-key", true
	id, err := s.Store.SaveSearchService("", storage.SearchServicePatch{Name: &name, Provider: &engine, APIKey: &credential}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.SaveSearchService(id, storage.SearchServicePatch{Enabled: &on}, 2); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.SetDefaultSearchService(id, 3); err != nil {
		t.Fatal(err)
	}
	ask := func(key string) (int, string) {
		r := httptest.NewRequest(http.MethodPost, "/v1/search", strings.NewReader(`{"query":"paper"}`))
		r.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	if code, _ := ask("invalid"); code != 401 {
		t.Fatal(code)
	}
	if code, _ := ask(k.Key); code != 403 || calls != 0 {
		t.Fatal(code, calls)
	}
	for _, g := range s.Store.CanonicalGroups() {
		if g.ID == k.GroupID {
			g.SearchServiceIDs = []string{id}
			if _, err = s.Store.SaveUnifiedGroup(g, g.Revision); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, want := range []struct {
		upstream, local int
		marker          string
	}{{402, 502, "search_upstream_balance"}, {429, 429, "search_upstream_rate_limit"}, {200, 200, "evidence"}} {
		status = want.upstream
		code, body := ask(k.Key)
		if code != want.local || !strings.Contains(body, want.marker) || strings.Contains(body, credential) {
			t.Fatal(code, body)
		}
	}
	if calls != 3 {
		t.Fatal("unexpected retries", calls)
	}
	// Hold one real search reservation until the client cancels its HTTP request.
	started := make(chan struct{})
	s.Search.HTTP.Transport = probeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/search", strings.NewReader(`{"query":"cancel"}`))
	req.Header.Set("Authorization", "Bearer "+k.Key)
	finished := make(chan struct{})
	go func() { defer close(finished); s.Handler().ServeHTTP(httptest.NewRecorder(), req) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("search never reached upstream")
	}
	if err = s.Store.SetKeyBudgetAndSave(k.Key, storage.KeyBudget{}, false); err == nil {
		t.Fatal("active search did not hold budget reservation")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("search did not stop after cancellation")
	}
	if err = s.Store.SetKeyBudgetAndSave(k.Key, storage.KeyBudget{}, false); err != nil {
		t.Fatal("search leaked reservation", err)
	}
	release, _, err := s.Store.BeginRequestBudget(k.Key)
	if err != nil {
		t.Fatal("model budget locked after search", err)
	}
	release()
}
