package jobs

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestDailyLoopDelayAndTickerStartAfterFirstRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		start := time.Now()
		times := make(chan time.Duration, 8)
		done := make(chan struct{})
		go func() {
			defer close(done)
			calls := 0
			RunDailyLoop(ctx, func(got context.Context) {
				if got != ctx {
					t.Error("callback did not receive the original context")
				}
				times <- time.Since(start)
				calls++
				if calls == 1 {
					time.Sleep(20 * time.Second)
				}
			})
		}()
		synctest.Wait()
		time.Sleep(8*time.Second - time.Nanosecond)
		synctest.Wait()
		if len(times) != 0 {
			t.Fatal("ran before startup delay")
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if len(times) != 1 {
			t.Fatal("first run missing")
		}
		if first := <-times; first != 8*time.Second {
			t.Fatal("first run changed", first)
		}
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		if len(times) != 0 {
			t.Fatal("ticker started before the first callback returned")
		}
		time.Sleep(20 * time.Second)
		synctest.Wait()
		if len(times) != 1 {
			t.Fatal("second run missing")
		}
		if second := <-times; second != 328*time.Second {
			t.Fatal("poll cadence changed", second)
		}
		cancel()
		<-done
		time.Sleep(10 * time.Minute)
		if len(times) != 0 {
			t.Fatal("ran after cancellation")
		}
	})
}

func TestDailyLoopCancellationBeforeStartup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			RunDailyLoop(ctx, func(context.Context) { t.Error("claim ran after early cancellation") })
		}()
		synctest.Wait()
		cancel()
		<-done
		time.Sleep(10 * time.Minute)
	})
}

func TestDailyLoopSerialAndInFlightCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var calls atomic.Int32
		done := make(chan struct{})
		go func() {
			defer close(done)
			RunDailyLoop(ctx, func(ctx context.Context) {
				calls.Add(1)
				<-ctx.Done()
			})
		}()
		synctest.Wait()
		time.Sleep(20 * time.Minute)
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatalf("overlapping checks: %d", calls.Load())
		}
		cancel()
		<-done
	})
}
