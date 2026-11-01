package server

import (
	"codearts2api/internal/store"
	"codearts2api/internal/trae"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestExternalDailyClaimConcurrentDedupAndPersist(t *testing.T) {
	var calls atomic.Int32
	s, _, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); io.WriteString(w, `{"code":0,"data":{}}`) })
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ClaimForAccount(context.Background(), s.Store.Account("wb-cn"), "")
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("concurrent claims repeated", calls.Load())
	}
	a := s.Store.Account("wb-cn")
	if a.LastClaimDate != claimDateFor(a, time.Now()) || a.LastClaimState != "claimed" {
		t.Fatal("claim not recorded")
	}
	loaded, err := store.Open(s.Store.Path())
	if err != nil || loaded.Account("wb-cn").LastClaimDate != a.LastClaimDate {
		t.Fatal("claim not persisted")
	}
}
func TestExternalDailyFailureCooldownAndReadOnlyWelfare(t *testing.T) {
	var claims atomic.Int32
	s, _, _ := nativeTraeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case trae.CheckinStatusPath:
			io.WriteString(w, `{"enable":true,"checked_in":false,"credits":100}`)
		case trae.CheckinClaimPath:
			claims.Add(1)
			io.WriteString(w, `{"code":9074,"message":"rate limited"}`)
		}
	})
	addTrae(t, s)
	r := httptest.NewRequest("GET", "/admin/api/welfare?id=trae-acct", nil)
	w := httptest.NewRecorder()
	s.handleAdminWelfare(w, r)
	if w.Code != 200 || claims.Load() != 0 {
		t.Fatal("status query mutated state")
	}
	for i := 0; i < 2; i++ {
		if _, err := s.ClaimForAccount(context.Background(), s.Store.Account("trae-acct"), ""); err == nil {
			t.Fatal("failed claim treated as success")
		}
	}
	a := s.Store.Account("trae-acct")
	if claims.Load() != 1 || a.LastClaimDate != "" || a.NextClaimAt <= time.Now().Unix() || a.LastClaimState != "failed" {
		t.Fatal("failure retry/dedup incorrect")
	}
	if a.Failures != 0 {
		t.Fatal("reward failure poisoned inference account")
	}
}
func TestDailySchedulerRespectsToggleAndDisabledAccount(t *testing.T) {
	var calls atomic.Int32
	s, _, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); io.WriteString(w, `{"code":0,"data":{}}`) })
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	s.Store.UpdateSettings(func(c *store.Settings) { c.AutoClaim = false })
	s.RunDailyClaim(context.Background())
	if calls.Load() != 0 {
		t.Fatal("disabled scheduler called upstream")
	}
	s.Store.UpdateSettings(func(c *store.Settings) { c.AutoClaim = true; c.ClaimAfterHour = 0; c.ClaimAfterMinute = 0 })
	s.Store.SetAccountEnabled("wb-cn", false)
	s.RunDailyClaim(context.Background())
	if calls.Load() != 0 {
		t.Fatal("disabled account claimed")
	}
	s.Store.SetAccountEnabled("wb-cn", true)
	s.RunDailyClaim(context.Background())
	s.RunDailyClaim(context.Background())
	if calls.Load() != 1 {
		t.Fatal("scheduler failed or repeated")
	}
}
func TestDailyClaimBadBodyDoesNotClaimAll(t *testing.T) {
	s, _, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected network call") })
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	for _, body := range []string{`{"id":`, `{"id":"missing"}`} {
		w := httptest.NewRecorder()
		s.handleAdminClaim(w, httptest.NewRequest("POST", "/admin/api/claim", strings.NewReader(body)))
		if w.Code < 400 {
			t.Fatal("invalid target accepted")
		}
	}
}
