package server

import (
	"codearts2api/internal/access"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestUpstreamFailuresDoNotFreezeUser(t *testing.T) {
	calls := 0
	s, _ := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		calls++
		code, body := 200, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":13,"completion_tokens":264}}`
		if calls == 2 {
			code, body = 429, `{"error":{"message":"rate limited"}}`
		}
		if calls == 4 {
			code, body = 200, "{}"
		}
		return &http.Response{StatusCode: code, Request: r, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	s.Reg.Replace([]provider.Model{{ID: "healthy-v1"}, {ID: "limited-v1"}, {ID: "broken-v1"}})
	g, err := s.Store.SaveGroup(storage.AccountGroup{Name: "fixture", AccountIDs: []string{"a"}, ModelMappings: []storage.ModelMapping{{Name: "healthy-v1", Sources: []string{"healthy-v1"}}, {Name: "limited-v1", Sources: []string{"limited-v1"}}, {Name: "broken-v1", Sources: []string{"broken-v1"}}}})
	if err != nil {
		t.Fatal(err)
	}
	u := createTestUser(t, s, "metered", g.ID)
	budget := access.KeyBudget{MaxUSD: 1}
	if _, err = s.Store.SaveUser(access.UserUpdate{ID: u.ID, Username: u.Username, GroupID: g.ID, Budget: &budget}, ""); err != nil {
		t.Fatal(err)
	}
	k, err := s.Store.AddUserKeyAndSave(u.ID, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.SetKeyBudgetAndSave(k.Key, budget, false); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.SetDefaultPriceAndSave(&storage.ModelPrice{Currency: "USD", Input: 1, Output: 1}); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.AdjustWallet(u.ID, 0, 1000000000, true, false, "fixture"); err != nil {
		t.Fatal(err)
	}
	s.Store.UpdateSettings(func(st *storage.Settings) { st.MaxRetries = 1 })
	for i, m := range []string{"healthy-v1", "limited-v1", "healthy-v1", "broken-v1", "healthy-v1"} {
		w := responseRequest(s, k.Key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, m))
		if want := []int{200, 429, 200, 502, 200}[i]; w.Code != want {
			t.Fatalf("step %d: %d %s", i, w.Code, w.Body.String())
		}

	}
	got, _ := s.Store.User(u.ID)
	charge, _ := access.CostCredits(.000277)
	if calls != 5 || got.BudgetUsage.Tokens != 831 || got.Wallet.Balance != 1000000000-3*charge || s.Store.UserReservedCredits(u.ID) != 0 {
		t.Fatalf("calls=%d budget=%+v wallet=%+v", calls, got.BudgetUsage, got.Wallet)
	}
}

func TestRateLimitAfterOutputStillRequiresUsage(t *testing.T) {
	ctx := withUsageCapture(context.Background(), "chat")
	resetUsage(ctx)
	captureUsage(ctx, []byte(`{"choices":[{"delta":{"content":"partial"}}]}`))
	finishAttempt(ctx, &provider.Error{Status: 429})
	if !requestUsageIncomplete(ctx) {
		t.Fatal("discarded partial generation")
	}
}
