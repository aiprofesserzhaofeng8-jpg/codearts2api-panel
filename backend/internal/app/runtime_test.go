package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"testing/synctest"
	"time"
)

type fakeHTTP struct {
	listen   func() error
	shutdown func(context.Context) error
}

func (h fakeHTTP) ListenAndServe() error              { return h.listen() }
func (h fakeHTTP) Shutdown(ctx context.Context) error { return h.shutdown(ctx) }

func TestRuntimeCloseOrderAndSaveAfterShutdownFailure(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var events []string
				polling, cancel := context.WithCancel(context.Background())
				defer cancel()
				r := &Runtime{
					http: fakeHTTP{shutdown: func(ctx context.Context) error {
						events = append(events, "shutdown")
						deadline, ok := ctx.Deadline()
						if !ok || time.Until(deadline) != 10*time.Second || polling.Err() != nil {
							t.Fatal("shutdown deadline or cancellation order changed")
						}
						if failed {
							<-ctx.Done()
							return ctx.Err()
						}
						return nil
					}},
					save: func() error {
						events = append(events, "save")
						if polling.Err() != nil {
							t.Fatal("polling canceled before final save")
						}
						if failed {
							return errors.New("fixture save error")
						}
						return nil
					},
					stopBackground: func() { events = append(events, "cancel"); cancel() },
					logf:           func(format string, args ...any) { events = append(events, fmt.Sprintf(format, args...)) },
				}
				r.Close()
				want := []string{"正在关闭…", "shutdown", "save", "已退出", "cancel"}
				if failed {
					want = []string{"正在关闭…", "shutdown", "关闭异常: context deadline exceeded", "save", "保存状态失败: fixture save error", "已退出", "cancel"}
				}
				if !reflect.DeepEqual(events, want) || polling.Err() != context.Canceled {
					t.Fatal("close sequence changed", events)
				}
			})
		})
	}
}

func TestRuntimeForwardsOnlyFatalListenErrors(t *testing.T) {
	for _, err := range []error{nil, http.ErrServerClosed, fmt.Errorf("wrapped: %w", http.ErrServerClosed), errors.New("fixture bind error")} {
		r := &Runtime{http: fakeHTTP{listen: func() error { return err }}, errors: make(chan error, 1)}
		started := false
		r.serve(func() { started = true })
		if !started {
			t.Fatal("startup log was omitted")
		}
		select {
		case got := <-r.Errors():
			if got != err || errors.Is(err, http.ErrServerClosed) {
				t.Fatal("wrong error forwarded", got)
			}
		default:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Fatal("listener failure was lost", err)
			}
		}
	}
}
