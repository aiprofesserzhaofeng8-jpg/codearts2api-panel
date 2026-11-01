package access

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"sync"
	"time"
)

type adminSession struct {
	Expires         time.Time
	PasswordVersion string
}

// AdminSessions owns one process's original administrator session registry.
// Its zero value is ready; tokens are digested and never persisted here.
type AdminSessions struct {
	mu     sync.Mutex
	tokens map[string]adminSession
}

func sessionDigest(v string) string { d := sha256.Sum256([]byte(v)); return hex.EncodeToString(d[:]) }

// The password callback is read under the original session lock, only for a
// known unexpired token. It must read the current stored password representation.
func (s *AdminSessions) Valid(token string, password func() string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.tokens[sessionDigest(token)]
	return ok && time.Now().Before(session.Expires) && sessionEqual(session.PasswordVersion, sessionDigest(password()))
}

// Issue retains eight-hour expiry, expired-token cleanup and earliest-expiry
// eviction at 128 entries. Token generation stays at the existing HTTP boundary.
func (s *AdminSessions) Issue(token, password string, now time.Time) time.Time {
	expires := now.Add(8 * time.Hour)
	s.mu.Lock()
	if s.tokens == nil {
		s.tokens = map[string]adminSession{}
	}
	for k, v := range s.tokens {
		if !v.Expires.After(now) {
			delete(s.tokens, k)
		}
	}
	if len(s.tokens) >= 128 {
		var oldest string
		var at time.Time
		for k, v := range s.tokens {
			if oldest == "" || v.Expires.Before(at) {
				oldest = k
				at = v.Expires
			}
		}
		delete(s.tokens, oldest)
	}
	s.tokens[sessionDigest(token)] = adminSession{expires, sessionDigest(password)}
	s.mu.Unlock()
	return expires
}
func (s *AdminSessions) Revoke(token string) {
	s.mu.Lock()
	delete(s.tokens, sessionDigest(token))
	s.mu.Unlock()
}
func sessionEqual(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}
