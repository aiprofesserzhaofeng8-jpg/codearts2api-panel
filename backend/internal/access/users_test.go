package access

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type identityFixture struct{ u User }

func (f *identityFixture) User(id string) (User, bool)         { return f.u, id == f.u.ID }
func (f *identityFixture) UserByName(name string) (User, bool) { return f.u, name == f.u.Username }
func (f *identityFixture) SaveUser(update UserUpdate, hash string) (UserView, error) {
	f.u.PasswordHash = hash
	return f.u.View(), nil
}
func (f *identityFixture) ChangeUserPassword(id string, revision uint64, hash string) error {
	if id != f.u.ID || revision != f.u.Revision {
		return ErrUserAuthentication
	}
	f.u.PasswordHash = hash
	f.u.Revision++
	return nil
}
func TestUsersPasswordBoundedSessionsExpiryAndRevision(t *testing.T) {
	password := "strong-user-test-password"
	hash, err := HashAdminPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	f := &identityFixture{u: User{ID: "u", Username: "alice", Enabled: true, Revision: 1, PasswordHash: hash}}
	s := NewUsers(f)
	ctx := context.Background()
	u, expires, err := s.Login(ctx, "ALICE", password, "token")
	if err != nil || u.ID != "u" || time.Until(expires) < 7*time.Hour {
		t.Fatal(u, expires, err)
	}
	if _, ok := s.Authenticate("token"); !ok {
		t.Fatal("session invalid")
	}
	if _, _, err = s.Login(ctx, "unknown", password, "bad"); err != ErrUserAuthentication {
		t.Fatal("unknown login", err)
	}
	if _, _, err = s.Login(ctx, "alice", "wrong", "bad"); err != ErrUserAuthentication {
		t.Fatal("wrong password", err)
	}
	now := time.Now()
	s.sessions = map[string]userSession{}
	for i := 0; i < 16; i++ {
		s.sessions[sessionDigest(fmt.Sprint(i))] = userSession{"u", 1, now.Add(time.Duration(i+1) * time.Minute)}
	}
	if _, _, err = s.Login(ctx, "alice", password, "latest"); err != nil {
		t.Fatal(err)
	}
	if len(s.sessions) != 16 {
		t.Fatal("unbounded per-user sessions", len(s.sessions))
	}
	if _, ok := s.Authenticate("0"); ok {
		t.Fatal("oldest not evicted")
	}
	x := s.sessions[sessionDigest("latest")]
	x.expires = time.Now().Add(-time.Second)
	s.sessions[sessionDigest("latest")] = x
	if _, ok := s.Authenticate("latest"); ok {
		t.Fatal("expired session")
	}
	if _, _, err = s.Login(ctx, "alice", password, "new"); err != nil {
		t.Fatal(err)
	}
	if err = s.ChangePassword(ctx, "u", password, "replacement-long-password"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Authenticate("new"); ok {
		t.Fatal("old revision authenticated")
	}
	if _, _, err = s.Login(ctx, "alice", "replacement-long-password", "reset"); err != nil {
		t.Fatal(err)
	}
	s.Logout("reset")
	if _, ok := s.Authenticate("reset"); ok {
		t.Fatal("logout")
	}
	s.passwordSlots <- struct{}{}
	s.passwordSlots <- struct{}{}
	if _, _, err = s.Login(ctx, "alice", password, "busy"); err != ErrUserLoginBusy {
		t.Fatal("unbounded password work", err)
	}
}
