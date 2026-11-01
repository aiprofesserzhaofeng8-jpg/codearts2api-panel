package server

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestProviderCredentialsKeepRewardAndGrowthLocksIndependent(t *testing.T) {
	s, _, _ := nativeWBServer(t, func(http.ResponseWriter, *http.Request) {
		t.Error("valid credentials unexpectedly called upstream")
	})
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	releaseReward, err := s.rewardRuntime.Acquire(t.Context(), "wb-cn")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseReward()
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
