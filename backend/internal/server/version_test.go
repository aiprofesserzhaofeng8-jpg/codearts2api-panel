package server

import (
	"codearts2api/internal/releaseinfo"
	"net/http"
	"strings"
	"testing"
)

func TestVersionAdminOnlyAndActualBuild(t *testing.T) {
	s, _, group := mappedFixture(t, nil)
	admin := fixtureAdminCookie(s)
	createTestUser(t, s, "ordinary", group.ID)
	user := loginTestUser(t, s, "ordinary")
	s.ReleaseInfo = releaseinfo.New("v0.3.1")
	for _, tc := range []struct {
		cookie *http.Cookie
		want   int
	}{{nil, 401}, {user, 403}, {admin, 200}} {
		w := userHTTP(s, tc.cookie, "GET", "/admin/api/version", "")
		if w.Code != tc.want {
			t.Fatalf("got %d want %d", w.Code, tc.want)
		}
		if tc.want == 200 && (!strings.Contains(w.Body.String(), `"current":"v0.3.1"`) || !strings.Contains(w.Body.String(), `"state":"unchecked"`)) {
			t.Fatal(w.Body.String())
		}
	}
	if w := userHTTP(s, admin, "POST", "/admin/api/version", `{}`); w.Code != 405 {
		t.Fatal(w.Code)
	}
}
