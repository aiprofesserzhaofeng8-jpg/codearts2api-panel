package jobs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/provider"
)

func TestGrowthCheckpointRestartAndAuthoritativeProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json.growth.json")
	g := NewGrowth(path, nil)
	task := provider.GrowthTask{Code: "paid-task", Current: 2, Target: 5, AutoSupported: true}
	if err := g.Checkpoint("first", task, false); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("checkpoint must remain private", info, err)
	}
	for _, instance := range []*Growth{g, NewGrowth(path, nil)} {
		for _, tc := range []struct {
			id      string
			current int64
			claimed bool
			ready   bool
			pending bool
		}{
			{"first", 2, false, false, true}, {"first", 1, false, false, true},
			{"first", 3, false, false, false}, {"first", 2, true, false, false},
			{"first", 2, false, true, false}, {"second", 2, false, false, false},
		} {
			next := task
			next.Current, next.Claimed, next.Claimable = tc.current, tc.claimed, tc.ready
			pending, err := instance.Pending(tc.id, next)
			if err != nil || pending != tc.pending {
				t.Fatalf("progress guard changed: %+v %v %v", tc, pending, err)
			}
		}
	}
	if err := g.Checkpoint("first", task, true); err != nil {
		t.Fatal(err)
	}
	if pending, err := NewGrowth(path, nil).Pending("first", task); pending || err != nil {
		t.Fatal("explicitly rejected call should be cleared", pending, err)
	}
}

func TestGrowthCheckpointCorruptionFailsClosedAndStaysLatched(t *testing.T) {
	for _, content := range []string{
		`null`, `{"version":2,"marks":{}}`, `{"version":1,"marks":null}`,
		`{"version":1,"marks":{"x":{"current":-1,"target":1,"at":1}}}`,
		`{"version":1,"marks":{"x":{"current":0,"target":0,"at":1}}}`,
		`{"version":1,"marks":{"x":{"current":0,"target":1,"at":0}}}`,
		strings.Repeat("x", (2<<20)+1),
	} {
		path := filepath.Join(t.TempDir(), "state.growth.json")
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		g := NewGrowth(path, nil)
		task := provider.GrowthTask{Code: "paid", Target: 1, AutoSupported: true}
		if pending, err := g.Pending("account", task); !pending || err == nil {
			t.Fatal("invalid execution record allowed a paid replay")
		}
		if err := os.WriteFile(path, []byte(`{"version":1,"marks":{}}`), 0600); err != nil {
			t.Fatal(err)
		}
		if err := g.Checkpoint("account", task, false); err == nil {
			t.Fatal("record error must stay latched until restart")
		}
		tasks := []provider.GrowthTask{task, {Code: "activity", AutoSupported: true, ActivityOnly: true}}
		g.Decorate("account", tasks)
		if !tasks[0].ExecutionPending || tasks[0].ActionDescription == "" || tasks[1].ExecutionPending {
			t.Fatal("paid failure contaminated independent activity tasks", tasks)
		}
	}
}

func TestGrowthCheckpointFailedRenameDoesNotPublish(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.growth.json")
	g := NewGrowth(path, nil)
	task := provider.GrowthTask{Code: "paid", Target: 1}
	if _, err := g.Pending("account", task); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := g.Checkpoint("account", task, false); err == nil {
		t.Fatal("checkpoint rename failure ignored")
	}
	if pending, err := g.Pending("account", task); pending || err != nil {
		t.Fatal("failed state was published in memory", pending, err)
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".growth-execution-*"))
	if len(matches) != 0 {
		t.Fatal("temporary checkpoint leaked", matches)
	}
}

func TestGrowthTaskLocksAndExecutionSlotsRemainIndependent(t *testing.T) {
	g := NewGrowth(filepath.Join(t.TempDir(), "state.growth.json"), nil)
	release, err := g.Acquire(t.Context(), "first")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if unlock, err := g.Acquire(ctx, "first"); err == nil {
		unlock()
		t.Fatal("same-account waiter bypassed the task lock")
	}
	other, err := g.Acquire(t.Context(), "second")
	if err != nil {
		t.Fatal(err)
	}
	other()
	a, ok := g.execution.slot()
	if !ok {
		t.Fatal("first slot unavailable")
	}
	b, ok := g.execution.slot()
	if !ok {
		t.Fatal("second slot unavailable")
	}
	if _, ok := g.execution.slot(); ok {
		t.Fatal("third execution started")
	}
	a()
	c, ok := g.execution.slot()
	if !ok {
		t.Fatal("released slot not reusable")
	}
	b()
	c()
}

func TestGrowthCheckpointEntryLimitPreservesExistingRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.growth.json")
	marks := make(map[string]growthExecutionMark)
	for i := 0; i < 4096; i++ {
		marks[string(rune(i+1))] = growthExecutionMark{Target: 1, At: 1}
	}
	content, err := json.Marshal(struct {
		Version int                            `json:"version"`
		Marks   map[string]growthExecutionMark `json:"marks"`
	}{1, marks})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	g := NewGrowth(path, nil)
	if err := g.Checkpoint("new", provider.GrowthTask{Code: "paid", Target: 1}, false); err == nil {
		t.Fatal("checkpoint limit bypassed")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(content) {
		t.Fatal("entry limit damaged existing execution records", err)
	}
}
