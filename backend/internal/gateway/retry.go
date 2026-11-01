package gateway

import (
	"context"
	"errors"
	"time"

	"codearts2api/internal/accounts"
)

// RetryAttempt is one admitted account lease. Context may carry the selected
// physical model; it must retain the request's single overall deadline.
type RetryAttempt struct {
	Account *accounts.Account
	Model   string
	Context context.Context
	Release func()
}

// RetryDecision separates provider-specific health effects from loop control.
// Exclude prevents selecting this account again; Backoff permits a bounded wait.
type RetryDecision struct {
	Stop, Exclude, Backoff bool
}

// RetryDriver retains atomic admission and provider policy at their existing
// owner. Acquire checks current permissions/budget and cancellation in the same
// order as admission. Execute may recover once within the same lease, reporting
// whether that recovery consumed another slot in the total attempt limit.
type RetryDriver interface {
	Acquire(context.Context, map[string]bool) (RetryAttempt, error)
	Execute(RetryAttempt, bool) (err error, repeated bool)
	Succeeded(RetryAttempt)
	Canceled(RetryAttempt)
	Failed(RetryAttempt, error) RetryDecision
}

type RetryOptions struct {
	Attempts    int
	Timeout     time.Duration
	Unavailable error
	Wait        func(context.Context, time.Duration) error
}

// TransientBackoff preserves the existing 1s, 2s, 3s capped sequence.
func TransientBackoff(n int) time.Duration {
	if n <= 0 {
		return 0
	}
	return min(time.Duration(n)*time.Second, 3*time.Second)
}

// WaitRetry remains interruptible by the request's cancellation/deadline.
func WaitRetry(ctx context.Context, delay time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

// RunAccountRetry owns the single deadline, bounded attempts and lease lifetime.
// Health effects run after release, before the committed-output retry gate.
func RunAccountRetry(ctx context.Context, options RetryOptions, driver RetryDriver) (*accounts.Account, error) {
	attempts := options.Attempts
	if attempts <= 0 {
		attempts = 3
	}
	attempts = min(attempts, 8)
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	tried := make(map[string]bool)
	var used *accounts.Account
	var lastErr error
	waits := 0
	for i := 0; i < attempts; i++ {
		attempt, err := driver.Acquire(ctx, tried)
		if err != nil {
			return used, err
		}
		if attempt.Account == nil {
			break
		}
		used = attempt.Account
		var repeated bool
		err = func() error {
			defer attempt.Release()
			var err error
			err, repeated = driver.Execute(attempt, i+1 < attempts)
			return err
		}()
		if repeated {
			i++
		}
		if err == nil {
			driver.Succeeded(attempt)
			return used, nil
		}
		if ctx.Err() != nil {
			driver.Canceled(attempt)
			return used, ctx.Err()
		}
		var terminal interface{ NonRetryableRequest() bool }
		if errors.As(err, &terminal) && terminal.NonRetryableRequest() || errors.Is(err, accounts.ErrCredentialChanged) || PromptBlocked(err) {
			return used, err
		}
		lastErr = err
		decision := driver.Failed(attempt, err)
		if decision.Exclude {
			tried[used.ID] = true
		}
		var committed *CommittedStreamError
		if decision.Stop || errors.As(err, &committed) {
			return used, err
		}
		if decision.Backoff {
			if i == attempts-1 {
				break
			}
			waits++
			if err := options.Wait(ctx, TransientBackoff(waits)); err != nil {
				// Preserve the original upstream error when backoff is interrupted.
				return used, lastErr
			}
		}
	}
	if lastErr == nil {
		lastErr = options.Unavailable
	}
	return used, lastErr
}

// CommittedStreamError retains the original error for health classification,
// while preventing another attempt after output becomes visible to a client.
type CommittedStreamError struct{ err error }

func (e *CommittedStreamError) Error() string { return e.err.Error() }
func (e *CommittedStreamError) Unwrap() error { return e.err }

func StopAfterOutput(err error, started bool) error {
	if err != nil && started {
		return &CommittedStreamError{err: err}
	}
	return err
}
