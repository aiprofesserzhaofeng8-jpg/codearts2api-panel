package access

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAdminSessionsExpiryRevocationAndPasswordBinding(t *testing.T) {
	var s AdminSessions
	password := "stored-password-representation"
	reads := 0
	current := func() string { reads++; return password }
	if s.Valid("unknown", current) || reads != 0 {
		t.Fatal("unknown token read password")
	}
	now := time.Now()
	expires := s.Issue("raw-private-token", password, now)
	if expires != now.Add(8*time.Hour) || !s.Valid("raw-private-token", current) || reads != 1 {
		t.Fatal("expiry or validation changed")
	}
	for key, entry := range s.tokens {
		if strings.Contains(key, "raw-private-token") || entry.PasswordVersion == password {
			t.Fatal("session material not digested")
		}
	}
	password = "changed-password"
	if s.Valid("raw-private-token", current) {
		t.Fatal("password change did not revoke")
	}
	password = "stored-password-representation"
	s.Revoke("raw-private-token")
	if s.Valid("raw-private-token", current) {
		t.Fatal("logout did not revoke")
	}
	s.Issue("expired", password, now.Add(-9*time.Hour))
	reads = 0
	if s.Valid("expired", current) || reads != 0 {
		t.Fatal("expired token read password")
	}
}
func TestAdminSessionsBoundAndExpiredCleanup(t *testing.T) {
	var s AdminSessions
	now := time.Now()
	for i := 0; i < 129; i++ {
		s.Issue(fmt.Sprint(i), "password", now.Add(time.Duration(i)*time.Second))
	}
	if len(s.tokens) != 128 || s.Valid("0", func() string { return "password" }) || !s.Valid("128", func() string { return "password" }) {
		t.Fatal("earliest expiry was not evicted")
	}
	s.Issue("latest", "password", now.Add(10*time.Hour))
	if len(s.tokens) != 1 {
		t.Fatal("expired sessions not cleaned", len(s.tokens))
	}
}
func TestAdminSessionsConcurrentIssueValidRevoke(t *testing.T) {
	var s AdminSessions
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				token := fmt.Sprintf("%d-%d", worker, i)
				s.Issue(token, "password", time.Now())
				s.Valid(token, func() string { return "password" })
				s.Revoke(token)
			}
		}(worker)
	}
	wg.Wait()
	if len(s.tokens) != 0 {
		t.Fatal("revoked sessions remained")
	}
}
