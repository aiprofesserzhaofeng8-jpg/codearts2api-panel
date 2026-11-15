package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdminResetsAreDurableAndRollbackOnFailure(t *testing.T) {
	for _, kind := range []string{"logs", "stats"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			s, err := Open(filepath.Join(dir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			s.Record(LogEntry{Time: 100, Status: 200}, 0)
			if err = s.Save(); err != nil {
				t.Fatal(err)
			}
			stats := s.Stats()
			reset := s.ClearLogsAndSave
			if kind == "stats" {
				reset = s.ClearStatsAndSave
			}
			if err = os.Rename(dir, dir+"-backup"); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(dir, []byte("block"), 0600); err != nil {
				t.Fatal(err)
			}
			if err = reset(); err == nil || len(s.Logs(0)) != 1 || s.Stats() != stats {
				t.Fatal("failed reset changed published state", err)
			}
			if err = os.Remove(dir); err != nil {
				t.Fatal(err)
			}
			if err = os.Rename(dir+"-backup", dir); err != nil {
				t.Fatal(err)
			}
			if err = reset(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(s.Path())
			if err != nil {
				t.Fatal(err)
			}
			if kind == "logs" && (len(reopened.Logs(0)) != 0 || reopened.Stats() != stats) {
				t.Fatal("log reset not durable")
			}
			if kind == "stats" && (reopened.Stats().TotalRequests != 0 || len(reopened.Logs(0)) != 1) {
				t.Fatal("stats reset not durable")
			}
			if len(reopened.UsageSnapshot()) != 1 {
				t.Fatal("reset erased usage history")
			}
		})
	}
}
