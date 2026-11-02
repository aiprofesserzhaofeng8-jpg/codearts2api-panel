package access

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestUnifiedRolesAndCredentialRevocation(t *testing.T) {
	password := "admin-test-password"
	f := &identityFixture{u: User{ID: "consumer", Username: "alice", Enabled: true, Revision: 1}}
	s := NewUsers(f, func() string { return password })
	u, expiry, err := s.Login(context.Background(), "ADMIN", password, "admin-token")
	if err != nil || u.Role != RoleAdmin || u.ID != BuiltinAdminID || time.Until(expiry) < 7*time.Hour {
		t.Fatal(u, err)
	}
	if _, _, err = s.OpenSession(BuiltinAdminID, 0, "forged"); err == nil {
		t.Fatal("consumer OAuth path opened admin session")
	}
	consumer, _, err := s.OpenSession(f.u.ID, 1, "consumer-token")
	if err != nil || consumer.Role != RoleUser {
		t.Fatal(consumer, err)
	}
	if _, ok := s.Authenticate("unknown"); ok {
		t.Fatal("unknown accepted")
	}
	password = "changed"
	if _, ok := s.Authenticate("admin-token"); ok {
		t.Fatal("credential change not revoked")
	}
	if u, ok := s.Authenticate("consumer-token"); !ok || u.Role != RoleUser {
		t.Fatal("admin credential change affected consumer")
	}
	password = "admin-test-password"
	s.Logout("admin-token")
	if _, ok := s.Authenticate("admin-token"); ok {
		t.Fatal("logout not revoked")
	}
	s.openSession(administratorView(), "expired", sessionDigest(password))
	entry := s.sessions[sessionDigest("expired")]
	entry.expires = time.Now().Add(-time.Second)
	s.sessions[sessionDigest("expired")] = entry
	if _, ok := s.Authenticate("expired"); ok {
		t.Fatal("expired accepted")
	}
	if _, ok := s.sessions["consumer-token"]; ok {
		t.Fatal("raw token retained")
	}
}
func TestUnifiedSessionsConcurrentAndBounded(t *testing.T) {
	s := NewUsers(&identityFixture{}, func() string { return "stored" })
	for i := 0; i < 17; i++ {
		s.openSession(administratorView(), fmt.Sprint(i), sessionDigest("stored"))
	}
	if len(s.sessions) != 16 {
		t.Fatal("principal bound", len(s.sessions))
	}
	for i := 0; i < 2050; i++ {
		s.openSession(UserView{ID: fmt.Sprint(i), Role: RoleUser}, "user"+fmt.Sprint(i), "")
	}
	if len(s.sessions) != 2048 {
		t.Fatal("global bound", len(s.sessions))
	}
	s = NewUsers(&identityFixture{}, func() string { return "stored" })
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				token := fmt.Sprintf("%d-%d", worker, i)
				s.openSession(administratorView(), token, sessionDigest("stored"))
				s.Authenticate(token)
				s.Logout(token)
			}
		}(worker)
	}
	wg.Wait()
	if len(s.sessions) != 0 {
		t.Fatal("revoked sessions retained")
	}
}
