package server

import (
	"codearts2api/internal/provider"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Independent from credentials, daily check-in and inference health. Persist
// before a billable call; uncertain calls stay blocked across restarts until
// upstream progress advances. Only explicit management actions touch this file.
type growthExecutionMark struct {
	Current int64 `json:"current"`
	Target  int64 `json:"target"`
	At      int64 `json:"at"`
}
type growthExecutionState struct {
	mu     sync.Mutex
	loaded bool
	err    error
	marks  map[string]growthExecutionMark
	slots  chan struct{}
}

func (g *growthExecutionState) slot() (func(), bool) {
	g.mu.Lock()
	if g.slots == nil {
		g.slots = make(chan struct{}, 2)
	}
	slots := g.slots
	g.mu.Unlock()
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, true
	default:
		return nil, false
	}
}
func (g *growthExecutionState) loadLocked(path string) error {
	if g.loaded {
		return g.err
	}
	g.loaded = true
	g.marks = map[string]growthExecutionMark{}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err == nil {
		defer f.Close()
		var b []byte
		b, err = io.ReadAll(io.LimitReader(f, (2<<20)+1))
		if err == nil {
			var data struct {
				Version int                            `json:"version"`
				Marks   map[string]growthExecutionMark `json:"marks"`
			}
			if len(b) > 2<<20 || json.Unmarshal(b, &data) != nil || data.Version != 1 || data.Marks == nil || len(data.Marks) > 4096 {
				err = fmt.Errorf("invalid execution state")
			} else {
				for key, mark := range data.Marks {
					if len(key) == 0 || len(key) > 512 || mark.Current < 0 || mark.Target <= 0 || mark.At <= 0 {
						err = fmt.Errorf("invalid checkpoint")
						break
					}
				}
				if err == nil {
					g.marks = data.Marks
				}
			}
		}
	}
	if err != nil {
		g.err = fmt.Errorf("成长执行记录无法读取，已停止付费任务；请检查本机执行记录文件")
	}
	return g.err
}
func (g *growthExecutionState) pending(path, id string, task provider.GrowthTask) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.loadLocked(path); err != nil {
		return true, err
	}
	mark, ok := g.marks[id+"\x00"+task.Code]
	return ok && !task.Claimed && !task.Claimable && task.Current <= mark.Current, nil
}
func (g *growthExecutionState) checkpoint(path, id string, task provider.GrowthTask, remove bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.loadLocked(path); err != nil {
		return err
	}
	marks := make(map[string]growthExecutionMark, len(g.marks)+1)
	for k, v := range g.marks {
		marks[k] = v
	}
	key := id + "\x00" + task.Code
	if remove {
		delete(marks, key)
	} else {
		marks[key] = growthExecutionMark{Current: task.Current, Target: task.Target, At: time.Now().Unix()}
	}
	if len(marks) > 4096 {
		return fmt.Errorf("成长执行记录达到上限，已停止新任务")
	}
	b, _ := json.Marshal(struct {
		Version int                            `json:"version"`
		Marks   map[string]growthExecutionMark `json:"marks"`
	}{1, marks})
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("成长执行记录无法保存，未开始新的付费对话")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".growth-execution-*.tmp")
	if err != nil {
		return fmt.Errorf("成长执行记录无法保存，未开始新的付费对话")
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		return fmt.Errorf("成长执行记录无法保存，已停止新的付费对话")
	}
	g.marks = marks
	return nil
}
func (s *Server) growthExecutionPath() string { return s.Store.Path() + ".growth.json" }
func (s *Server) decorateGrowthTasks(id string, tasks []provider.GrowthTask) {
	for i := range tasks {
		if tasks[i].AutoSupported && !tasks[i].ActivityOnly {
			pending, err := s.growthExecution.pending(s.growthExecutionPath(), id, tasks[i])
			tasks[i].ExecutionPending = pending
			if err != nil {
				tasks[i].ActionDescription = err.Error()
			}
		}
	}
}
