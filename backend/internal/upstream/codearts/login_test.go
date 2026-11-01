package codearts

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	legacy "codearts2api/internal/codearts"
)

func TestBrowserLoginAdapterVerifiesBeforeStaging(t *testing.T) {
	for _, tc := range []struct {
		name, expiry, secret, token, domain, response, want string
		status, calls                                       int
	}{
		{name: "missing expiry", secret: "sk", token: "sts", want: "expires_at"},
		{name: "expired", expiry: time.Now().Add(-time.Hour).Format(time.RFC3339), secret: "sk", token: "sts", want: "expires_at"},
		{name: "incomplete STS", expiry: "future", secret: "sk", want: "STS session"},
		{name: "identity failure", expiry: "future", secret: "sk", token: "sts", response: `{"error":"fixture"}`, status: 403, calls: 1, want: "身份验证"},
		{name: "missing identity", expiry: "future", secret: "sk", token: "sts", response: `{}`, status: 200, calls: 1, want: "身份验证"},
		{name: "identity mismatch", expiry: "future", secret: "sk", token: "sts", domain: "different", response: `{"account_id":"verified"}`, status: 200, calls: 1, want: "身份不一致"},
		{name: "valid STS", expiry: "future", secret: "sk", token: "sts", response: `{"account_id":"verified","principal_urn":"iam::verified:user/verified-user"}`, status: 200, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := legacy.NewClient()
			calls := 0
			client.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path != legacy.CallerIdentityPath || !strings.Contains(r.Header.Get("Authorization"), "fixture-ak") || r.Header.Get("x-security-token") != "sts" {
					t.Fatal("native identity signature changed")
				}
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > 30*time.Second {
					t.Fatal("identity timeout missing")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.response)), Request: r}, nil
			})
			manager := legacy.NewLoginManager(client)
			adapter := NewBrowserLogin(manager, client)
			start, err := adapter.Start(accounts.BrowserLoginOptions{Locale: "zh-cn"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { adapter.Cancel(start.ID) })
			parsed, _ := url.Parse(start.AuthURL)
			if parsed.Query().Get("plugin-version") != legacy.LoginPluginVersion || start.Port < 10000 || !strings.HasSuffix(start.Callback, legacy.AuthRedirectPath) || adapter.Get("missing") != nil {
				t.Fatal("native authorization options changed")
			}
			expiry := tc.expiry
			if expiry == "future" {
				expiry = time.Now().Add(time.Hour).Format(time.RFC3339Nano)
			}
			raw, _ := json.Marshal(map[string]string{"accessKey": "fixture-ak", "secretKey": tc.secret, "securitytoken": tc.token, "expires_at": expiry, "domainId": tc.domain})
			flow := adapter.Get(start.ID)
			err = flow.Submit(t.Context(), string(raw))
			if calls != tc.calls {
				t.Fatalf("identity calls=%d want %d", calls, tc.calls)
			}
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) || flow.Snapshot().Credential.Value != nil {
					t.Fatalf("invalid credential staged: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			snapshot := flow.Snapshot()
			_, _, _, native := manager.Get(start.ID).Snapshot()
			if snapshot.Status != "confirmed" || snapshot.Credential.Value != native || native.DomainID != "verified" || native.AccountLabel != "verified-user" || native.Source != "browser-import" {
				t.Fatal("native generation or verified identity changed")
			}
			if snapshot.Credential.KeyHint == native.AccessKey || snapshot.Credential.IdentityHint == native.DomainID || len(snapshot.Credential.Fingerprint) != 64 {
				t.Fatal("preview not masked or fingerprint missing")
			}
			if err := flow.Submit(t.Context(), start.Callback+"?code=fixture-other-code"); err == nil {
				t.Fatal("different callback replaced staged credentials")
			}
			if !adapter.Cancel(start.ID) || flow.Snapshot().Credential.Value != nil {
				t.Fatal("cancel retained unsaved credentials")
			}
		})
	}
}
