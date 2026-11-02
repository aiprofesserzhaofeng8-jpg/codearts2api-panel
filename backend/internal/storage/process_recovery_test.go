package storage

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Kill the actual writer after a synced settlement and before any snapshot.
// This covers process death, not hardware power loss or disk firmware behavior.
func TestUsageJournalRecoversAfterProcessDeath(t *testing.T) {
	const fixtureEnv = "CODEARTS_JOURNAL_CRASH_FIXTURE"
	if path := os.Getenv(fixtureEnv); path != "" {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		k := s.AddKey("crash fixture")
		if err := s.SetKeyBudgetAndSave(k.Key, KeyBudget{MaxRequests: 1}, false); err != nil {
			t.Fatal(err)
		}
		if err := s.Record(journalEntry("committed-before-kill"), 2, k.Key); err != nil {
			t.Fatal(err)
		}
		fmt.Println("settled")
		for {
			time.Sleep(time.Second)
		}
	}
	path := filepath.Join(t.TempDir(), "state.json")
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestUsageJournalRecoversAfterProcessDeath$")
	cmd.Env = append(os.Environ(), fixtureEnv+"="+path)
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || line != "settled\n" {
		t.Fatalf("writer did not reach durable settlement: %q %v", line, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("writer exited normally instead of being killed")
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Stats().TotalRequests != 1 || s.Keys()[0].BudgetUsage.Requests != 1 || len(s.Logs(0)) != 1 {
		t.Fatal("durable request or budget lost after process death")
	}
	if _, err := s.BeginKeyBudget(s.Keys()[0].Key); err == nil {
		t.Fatal("restart reopened exhausted budget")
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	assertSettlementEqual(t, s, again)
}
