package gateway

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
)

type retryFixture struct {
	acquire func(context.Context, map[string]bool) (RetryAttempt, error)
	execute func(RetryAttempt, bool) (error, bool)
	success func(RetryAttempt)
	cancel  func(RetryAttempt)
	fail    func(RetryAttempt, error) RetryDecision
}

func (f retryFixture) Acquire(ctx context.Context, tried map[string]bool) (RetryAttempt, error) {
	return f.acquire(ctx, tried)
}
func (f retryFixture) Execute(a RetryAttempt, mayRepeat bool) (error, bool) {
	return f.execute(a, mayRepeat)
}
func (f retryFixture) Succeeded(a RetryAttempt) {
	if f.success != nil {
		f.success(a)
	}
}
func (f retryFixture) Canceled(a RetryAttempt) {
	if f.cancel != nil {
		f.cancel(a)
	}
}
func (f retryFixture) Failed(a RetryAttempt, err error) RetryDecision { return f.fail(a, err) }

func TestAccountRetrySharesDeadlineAndAttemptSlots(t *testing.T) {
	first, last := &accounts.Account{ID: "first"}, &accounts.Account{ID: "last"}
	upstream := errors.New("upstream")
	var events []string
	var deadline time.Time
	active, calls := 0, 0
	fixture := retryFixture{
		acquire: func(ctx context.Context, tried map[string]bool) (RetryAttempt, error) {
			got, ok := ctx.Deadline()
			if !ok || !deadline.IsZero() && got != deadline || active != 0 {
				t.Fatal("deadline reset or previous lease still held")
			}
			deadline = got
			acct := first
			if calls > 0 {
				if !tried[first.ID] {
					t.Fatal("failed account not excluded")
				}
				acct = last
			}
			events = append(events, "acquire:"+acct.ID)
			active++
			return RetryAttempt{Account: acct, Model: "actual", Context: ctx, Release: func() { active--; events = append(events, "release") }}, nil
		},
		execute: func(a RetryAttempt, mayRepeat bool) (error, bool) {
			calls++
			got, _ := a.Context.Deadline()
			if active != 1 || got != deadline || mayRepeat != (calls == 1) {
				t.Fatal("lease, deadline or shared attempt budget changed")
			}
			events = append(events, "execute")
			if calls == 1 {
				return upstream, true // Two attempts consumed on this lease.
			}
			return nil, false
		},
		fail: func(a RetryAttempt, err error) RetryDecision {
			if active != 0 || err != upstream {
				t.Fatal("health effects ran before release or lost original error")
			}
			events = append(events, "failure")
			return RetryDecision{Exclude: true}
		},
		success: func(RetryAttempt) {
			if active != 0 {
				t.Fatal("success retained lease")
			}
			events = append(events, "success")
		},
	}
	used, err := RunAccountRetry(t.Context(), RetryOptions{Attempts: 3, Timeout: time.Minute}, fixture)
	if err != nil || used != last || calls != 2 || !reflect.DeepEqual(events, []string{"acquire:first", "execute", "release", "failure", "acquire:last", "execute", "release", "success"}) {
		t.Fatalf("shared budget/order changed: %v calls=%d events=%v", err, calls, events)
	}
}

func TestAccountRetryLimitAndInterruptedBackoff(t *testing.T) {
	for _, configured := range []int{-1, 1, 3, 99} {
		calls, released := 0, 0
		var waits []time.Duration
		upstream := errors.New("original upstream error")
		fixture := retryFixture{
			acquire: func(ctx context.Context, tried map[string]bool) (RetryAttempt, error) {
				if len(tried) != 0 {
					t.Fatal("transient error excluded single account")
				}
				return RetryAttempt{Account: &accounts.Account{ID: "one"}, Context: ctx, Release: func() { released++ }}, nil
			},
			execute: func(RetryAttempt, bool) (error, bool) { calls++; return upstream, false },
			fail:    func(RetryAttempt, error) RetryDecision { return RetryDecision{Backoff: true} },
		}
		_, err := RunAccountRetry(t.Context(), RetryOptions{Attempts: configured, Timeout: time.Minute, Wait: func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }}, fixture)
		want := configured
		if want <= 0 {
			want = 3
		}
		want = min(want, 8)
		if err != upstream || calls != want || released != want || len(waits) != want-1 {
			t.Fatalf("limit changed: configured=%d calls=%d released=%d waits=%v err=%v", configured, calls, released, waits, err)
		}
		for i, delay := range waits {
			if delay != min(time.Duration(i+1)*time.Second, 3*time.Second) {
				t.Fatal("backoff sequence changed")
			}
		}
		calls = 0
		_, err = RunAccountRetry(t.Context(), RetryOptions{Attempts: 3, Timeout: time.Minute, Wait: func(context.Context, time.Duration) error { return context.Canceled }}, fixture)
		if err != upstream || calls != 1 {
			t.Fatal("interrupted backoff changed upstream error or replayed")
		}
	}
}

func TestAccountRetryTerminalAndCommittedOrder(t *testing.T) {
	for _, kind := range []string{"terminal", "credentials", "prompt", "committed", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			base := errors.New("upstream")
			var failure error = base
			switch kind {
			case "terminal":
				failure = &SummaryFailure{"request failure"}
			case "credentials":
				failure = accounts.ErrCredentialChanged
			case "prompt":
				failure = &provider.Error{Status: 400, ContentBlocked: true}
			case "committed":
				failure = StopAfterOutput(base, true)
			}
			released, classified, canceled := false, 0, 0
			fixture := retryFixture{
				acquire: func(ctx context.Context, _ map[string]bool) (RetryAttempt, error) {
					if released {
						t.Fatal("terminal attempt replayed")
					}
					return RetryAttempt{Account: &accounts.Account{ID: "one"}, Context: ctx, Release: func() { released = true }}, nil
				},
				execute: func(RetryAttempt, bool) (error, bool) {
					if kind == "cancel" {
						cancel()
					}
					return failure, false
				},
				fail: func(_ RetryAttempt, err error) RetryDecision {
					classified++
					if !released || !errors.Is(err, base) {
						t.Fatal("committed health effects lost order or error chain")
					}
					return RetryDecision{Exclude: true, Backoff: true}
				},
				cancel: func(RetryAttempt) { canceled++ },
			}
			_, err := RunAccountRetry(ctx, RetryOptions{Attempts: 8, Timeout: time.Minute}, fixture)
			if !released || kind == "cancel" && (err != context.Canceled || canceled != 1) || kind != "cancel" && err != failure || kind == "committed" && classified != 1 || kind != "committed" && classified != 0 {
				t.Fatalf("terminal behavior changed: %v released=%v classified=%d canceled=%d", err, released, classified, canceled)
			}
		})
	}
}

func TestAccountRetryAcquireFailureAndPanicRelease(t *testing.T) {
	denied, unavailable := errors.New("permission denied"), errors.New("unavailable")
	fixture := retryFixture{acquire: func(context.Context, map[string]bool) (RetryAttempt, error) { return RetryAttempt{}, denied }}
	used, err := RunAccountRetry(t.Context(), RetryOptions{Timeout: time.Minute}, fixture)
	if used != nil || err != denied {
		t.Fatal("acquisition error identity changed")
	}
	fixture.acquire = func(context.Context, map[string]bool) (RetryAttempt, error) { return RetryAttempt{}, nil }
	used, err = RunAccountRetry(t.Context(), RetryOptions{Timeout: time.Minute, Unavailable: unavailable}, fixture)
	if used != nil || err != unavailable {
		t.Fatal("empty pool lost supplied error")
	}
	released := false
	fixture.acquire = func(ctx context.Context, _ map[string]bool) (RetryAttempt, error) {
		return RetryAttempt{Account: &accounts.Account{}, Context: ctx, Release: func() { released = true }}, nil
	}
	fixture.execute = func(RetryAttempt, bool) (error, bool) { panic("fixture") }
	func() {
		defer func() {
			if recover() != "fixture" || !released {
				t.Fatal("panic swallowed or lease leaked")
			}
		}()
		RunAccountRetry(t.Context(), RetryOptions{Timeout: time.Minute}, fixture)
	}()
}
