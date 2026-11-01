package server

import (
	"net"
	"sync"
	"time"
)

type adminAttempt struct {
	count   int
	expires time.Time
}
type adminAttempts struct {
	mu    sync.Mutex
	peers map[string]adminAttempt
}

func (a *adminAttempts) key(remote string, now time.Time) string {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	if a.peers == nil {
		a.peers = map[string]adminAttempt{}
	}
	for k, v := range a.peers {
		if !v.expires.After(now) {
			delete(a.peers, k)
		}
	}
	if _, ok := a.peers[host]; !ok && len(a.peers) >= 1024 {
		return "overflow"
	}
	return host
}
func (a *adminAttempts) blocked(remote string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.peers[a.key(remote, time.Now())].count >= 20
}
func (a *adminAttempts) failure(remote string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	key := a.key(remote, now)
	v := a.peers[key]
	if v.expires.IsZero() {
		v.expires = now.Add(time.Minute)
	}
	v.count++
	a.peers[key] = v
}
func (a *adminAttempts) success(remote string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.peers, a.key(remote, time.Now()))
}
