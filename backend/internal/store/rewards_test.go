package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRewardPersistenceFailureRollsBackClaimMarker(t *testing.T) {
	s := testPool(t, 1)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocker, "state.json")
	if err := s.SaveDailyReward("0", "2026-10-29", "claimed", "confirmed", true, 0); err == nil {
		t.Fatal("save should fail")
	}
	a := s.Account("0")
	if a.LastClaimDate != "" || a.LastClaimState != "" {
		t.Fatal("failed save left a success marker")
	}
}
