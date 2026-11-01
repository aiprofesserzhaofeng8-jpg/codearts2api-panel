package gateway

import (
	"context"
	"time"
)

// RecoveryAttempt supplies platform eligibility and current authorization.
// AdmitRecovery rechecks the recovery window and permissions/budget after the
// wait; false keeps the original error without cutting short generation.
type RecoveryAttempt interface {
	Run(context.Context) error
	Finish(context.Context, error)
	CanRecover(context.Context, error) bool
	AdmitRecovery(context.Context) (bool, error)
}

type RecoveryOptions struct {
	MayRepeat bool
	Delay     time.Duration
	Wait      func(context.Context, time.Duration) error
}

// RunRecovery permits one recovery inside an already-admitted lease. It never
// selects another account, changes the context deadline or resets the budget.
func RunRecovery(ctx context.Context, options RecoveryOptions, attempt RecoveryAttempt) (error, bool) {
	err := attempt.Run(ctx)
	attempt.Finish(ctx, err)
	if !options.MayRepeat || !attempt.CanRecover(ctx, err) {
		return err, false
	}
	if waitErr := options.Wait(ctx, options.Delay); waitErr != nil {
		return waitErr, false
	}
	if ctx.Err() != nil {
		return ctx.Err(), false
	}
	allowed, admitErr := attempt.AdmitRecovery(ctx)
	if admitErr != nil {
		return admitErr, false
	}
	if !allowed {
		return err, false
	}
	err = attempt.Run(ctx)
	attempt.Finish(ctx, err)
	return err, true
}
