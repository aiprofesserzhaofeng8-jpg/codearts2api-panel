package server

import (
	"codearts2api/internal/access"
	"net/http"
	"strings"
	"testing"
)

func TestKeyGuideAuthorization(t *testing.T) {
	s, key, group := mappedFixture(t, nil)
	alice := createTestUser(t, s, "alice", group.ID)
	createTestUser(t, s, "bob", group.ID)
	k, err := s.Store.AddUserKeyAndSave(alice.ID, "personal")
	if err != nil {
		t.Fatal(err)
	}
	path := "/user/api/keys/models?ref=" + keyReference(k.Key)
	a := loginTestUser(t, s, "alice")
	b := loginTestUser(t, s, "bob")
	for _, tc := range []struct {
		cookie *http.Cookie
		url    string
		status int
	}{
		{a, path, 200}, {b, path, 404}, {nil, path, 401}, {a, "/admin/api/keys/models?ref=" + keyReference(key), 403},
	} {
		w := userHTTP(s, tc.cookie, "GET", tc.url, "")
		if w.Code != tc.status || strings.Contains(w.Body.String(), k.Key) {
			t.Fatalf("status %d, expected %d", w.Code, tc.status)
		}
		if tc.status == 200 && !strings.Contains(w.Body.String(), mappedPublic) {
			t.Fatal("missing callable model")
		}
	}
	if len(group.UnifiedModels) == 0 {
		t.Fatal("missing unified fixture grant")
	}
	group.UnifiedModels[0].Callable = false
	group.ModelMappings = nil
	if _, err := s.Store.SaveUnifiedGroup(group, group.Revision); err != nil {
		t.Fatal(err)
	}
	w := adminProbeRequest(t, s, "GET", "/admin/api/keys/models?ref="+access.KeyReference(key), "")
	if w.Code != 200 || strings.Contains(w.Body.String(), mappedPublic) {
		t.Fatalf("visible-only model returned: %d %s", w.Code, w.Body.String())
	}
	if s.Store.UserKeys(alice.ID)[0].Requests != 0 {
		t.Fatal("preview counted as inference")
	}
	if err := s.Store.DeleteGroup(group.ID); err != nil {
		t.Fatal(err)
	}
	checkModelError(t, adminProbeRequest(t, s, "GET", "/admin/api/keys/models?ref="+access.KeyReference(key), ""), 403, "group_unavailable")
	checkModelError(t, userHTTP(s, a, "GET", path, ""), 403, "group_unavailable")
	checkModelError(t, userHTTP(s, a, "POST", "/user/api/keys", "{}"), 403, "group_unavailable")
}
