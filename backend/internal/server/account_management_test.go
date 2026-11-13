package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codearts2api/internal/accounts"
)

func TestAccountManagementHTTPStatusAndOpaqueExport(t *testing.T) {
	s := newAdminTestServer(t, "a")
	for _, tc := range []struct {
		body, message, kind string
		status              int
	}{
		{`{"action":"remove"}`, "缺少账号 ID", "invalid_request_error", 400},
		{`{"action":"remove","id":"missing"}`, "账号已不存在，请刷新列表", "invalid_request_error", 404},
		{`{"action":"toggle","id":"missing"}`, "账号不存在，请刷新列表", "invalid_request_error", 404},
		{`{"action":"pause","id":"missing"}`, "账号不存在，请刷新列表", "invalid_request_error", 404},
		{`{"action":"export","id":"missing"}`, "账号不存在", "invalid_request_error", 400},
		{`{"action":"reimport","id":"missing"}`, "账号不存在", "invalid_request_error", 400},
		{`{"action":"session-json","session_json":"{}"}`, "session JSON 里缺少 accessKey / secretKey", "invalid_request_error", 400},
		{`{"action":"clear-errors","id":"missing"}`, "账号不存在，请刷新列表", "invalid_request_error", 404},
	} {
		w := adminProbeRequest(t, s, http.MethodPost, "/admin/api/accounts", tc.body)
		if w.Code != tc.status {
			t.Fatalf("%s: status %d: %s", tc.body, w.Code, w.Body.String())
		}
		if tc.message != "" {
			var output struct {
				Error struct{ Message, Type string }
			}
			if err := json.Unmarshal(w.Body.Bytes(), &output); err != nil || output.Error.Message != tc.message || output.Error.Type != tc.kind {
				t.Fatalf("wire error changed: %s", w.Body.String())
			}
		}
	}
	for _, platform := range []string{"workbuddy", "trae"} {
		raw := json.RawMessage("{ \"large_id\": 9007199254740993, \"token\": \"fixture\" }")
		s.Store.UpsertAccount(&accounts.Account{ID: platform, Provider: platform, Credentials: raw})
		w := adminProbeRequest(t, s, http.MethodPost, "/admin/api/accounts", `{"action":"export","id":"`+platform+`"}`)
		var output map[string]any
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &output) != nil || output["session_json"] != string(raw) {
			t.Fatalf("opaque credential bytes changed: %s", w.Body.String())
		}
		if _, exists := output["expires_at"]; exists {
			t.Fatal("non-native export acquired native-only fields")
		}
	}
}

func TestAccountManagementHTTPFailedDeletePreservesCreditView(t *testing.T) {
	s := newAdminTestServer(t, "a")
	s.Credits.Store("a", nil, nil)
	if err := s.Store.Save(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(s.Store.Path())
	if err := os.Rename(dir, dir+"-saved"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(dir); _ = os.Rename(dir+"-saved", dir) })
	if err := os.WriteFile(dir, []byte("block writes"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"toggle", "pause", "resume", "clear-errors"} {
		before := s.Store.Account("a")
		failed := adminProbeRequest(t, s, http.MethodPost, "/admin/api/accounts", `{"action":"`+action+`","id":"a"}`)
		after := s.Store.Account("a")
		if failed.Code != 500 || before.Enabled != after.Enabled || before.Paused != after.Paused {
			t.Fatalf("failed %s mutated account or reported success: %s", action, failed.Body.String())
		}
	}
	w := adminProbeRequest(t, s, http.MethodPost, "/admin/api/accounts", `{"action":"remove","id":"a"}`)
	if _, _, cached := s.Credits.Cached("a"); w.Code != 500 || !cached || s.Store.Account("a") == nil || !strings.Contains(w.Body.String(), "删除未完成") {
		t.Fatalf("failed delete lost account or credit view: %s", w.Body.String())
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir+"-saved", dir); err != nil {
		t.Fatal(err)
	}
	w = adminProbeRequest(t, s, http.MethodPost, "/admin/api/accounts", `{"action":"remove","id":"a"}`)
	if _, _, cached := s.Credits.Cached("a"); w.Code != 200 || cached || s.Store.Account("a") != nil {
		t.Fatalf("committed delete left stale view: %s", w.Body.String())
	}
}

func TestCreditQueryExplicitScope(t *testing.T) {
	s := newAdminTestServer(t, "a", "b", "c")
	for _, id := range []string{"a", "b", "c"} {
		s.Credits.Store(id, &accounts.CreditInfo{}, nil)
	}
	w := adminProbeRequest(t, s, "GET", "/admin/api/credits?id=b&id=a&id=b", "")
	var result struct{ Accounts []struct{ ID string } }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Accounts) != 2 || result.Accounts[0].ID != "b" || result.Accounts[1].ID != "a" {
		t.Fatalf("query scope lost: %s", w.Body.String())
	}
	for _, query := range []string{"id=", "id=missing", strings.Repeat("id=a&", 101)} {
		w = adminProbeRequest(t, s, "GET", "/admin/api/credits?"+query, "")
		if w.Code < 400 {
			t.Fatalf("invalid scope expanded to all accounts: %s", query)
		}
	}
}
