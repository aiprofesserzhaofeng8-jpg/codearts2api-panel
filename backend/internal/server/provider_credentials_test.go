package server

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestProviderCredentialsKeepRewardAndGrowthLocksIndependent(t *testing.T) {
	entered, finish := make(chan struct{}), make(chan struct{})
	s, _, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-finish
		io.WriteString(w, `{"code":0,"data":{}}`)
	})
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	done := make(chan error, 1)
	go func() { _, err := s.ClaimForAccount(t.Context(), s.Store.Account("wb-cn"), ""); done <- err }()
	defer func() {
		close(finish)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("claim did not start")
	}
	releaseGrowth, err := s.growthRuntime.Acquire(t.Context(), "wb-cn")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseGrowth()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, _, err := s.providerCredentials(ctx, s.Store.Account("wb-cn")); err != nil {
		t.Fatal("reward/growth operation blocked credential resolution", err)
	}
}
