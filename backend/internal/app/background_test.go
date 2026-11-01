package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestBackgroundCadenceAndFailureContinuation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		saves, claims := make(chan time.Duration, 8), make(chan time.Duration, 8)
		logs := make(chan string, 8)
		var refreshes atomic.Int32
		stop := startBackground(func() error {
			saves <- time.Since(start)
			return errors.New("fixture disk error")
		}, func(ctx context.Context) error {
			refreshes.Add(1)
			deadline, ok := ctx.Deadline()
			if !ok || deadline.Sub(start) != time.Minute {
				t.Errorf("refresh deadline: %v, %v", deadline, ok)
			}
			return nil
		}, func(context.Context) { claims <- time.Since(start) }, func(format string, args ...any) {
			logs <- fmt.Sprintf(format, args...)
		})
		defer stop()
		synctest.Wait()
		if refreshes.Load() != 1 || len(saves) != 0 || len(claims) != 0 {
			t.Fatal("unexpected startup work")
		}
		time.Sleep(8 * time.Second)
		synctest.Wait()
		if len(claims) != 1 {
			t.Fatal("first claim missing")
		}
		if first := <-claims; first != 8*time.Second {
			t.Fatal("claim delay changed", first)
		}
		time.Sleep(52 * time.Second)
		synctest.Wait()
		if len(saves) != 2 || len(logs) != 3 {
			t.Fatal("save count or error continuation changed")
		}
		if first, second := <-saves, <-saves; first != 30*time.Second || second != time.Minute {
			t.Fatal("save cadence changed", first, second)
		}
		stop()
		synctest.Wait()
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		if len(saves) != 0 || len(claims) != 0 || refreshes.Load() != 1 {
			t.Fatal("background polling continued after cancellation")
		}
	})
}

func TestBackgroundRefreshDeadlineIndependentOfPollingStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		finished := make(chan time.Duration, 1)
		logMessage := make(chan string, 1)
		stop := startBackground(func() error { t.Error("unexpected save"); return nil }, func(ctx context.Context) error {
			<-ctx.Done()
			finished <- time.Since(start)
			return ctx.Err()
		}, func(context.Context) { t.Error("unexpected claim") }, func(format string, args ...any) {
			logMessage <- fmt.Sprintf(format, args...)
		})
		synctest.Wait()
		stop()
		synctest.Wait()
		time.Sleep(59 * time.Second)
		synctest.Wait()
		if len(finished) != 0 {
			t.Fatal("polling cancellation shortened startup refresh")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if len(finished) != 1 || len(logMessage) != 1 {
			t.Fatal("refresh did not finish at its deadline")
		}
		if duration, message := <-finished, <-logMessage; duration != time.Minute || !strings.Contains(message, "刷新上游模型清单失败（将使用内置兜底清单）: context deadline exceeded") {
			t.Fatal("refresh timeout or log changed", duration, message)
		}
	})
}

func TestBackgroundSaveRemainsSerialAndStopDoesNotJoin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		var saves atomic.Int32
		stop := startBackground(func() error {
			saves.Add(1)
			close(entered)
			<-release
			return nil
		}, func(context.Context) error { return nil }, func(context.Context) {}, func(string, ...any) {})
		synctest.Wait()
		time.Sleep(30 * time.Second)
		<-entered
		time.Sleep(time.Second)
		stop()
		if saves.Load() != 1 {
			t.Fatal("unexpected overlapping save")
		}
		// Cancellation must return while the existing save is still blocked.
		close(release)
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()
		if saves.Load() != 1 {
			t.Fatal("save repeated after polling stop")
		}
	})
}
