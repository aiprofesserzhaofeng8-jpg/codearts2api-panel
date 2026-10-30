package server

import (
	"context"
	"errors"
	"time"

	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
)

const (
	workBuddyRetryWait   = 300 * time.Millisecond
	workBuddyRetryWindow = 3 * time.Second
)

func workBuddyTransientStatus(status int) bool {
	return status == 500 || status == 502 || status == 503 || status == 504
}

// One fast recovery, before any upstream output, shares the original lease and
// total-attempt limit. The window admits retries; it never cuts off generation.
func (s *Server) runAccountAttempt(ctx context.Context, acct *codearts.Account, model string, fn func(context.Context, *codearts.Account) error, mayRepeat bool) (error, bool) {
	err := fn(ctx, acct)
	finishAttempt(ctx, err)
	if !mayRepeat || !canRecoverWorkBuddy(ctx, acct, err) {
		return err, false
	}
	if waitErr := waitRetry(ctx, workBuddyRetryWait); waitErr != nil {
		return waitErr, false
	}
	c := ctx.Value(usageContextKey{}).(*usageCapture)
	if ctx.Err() != nil {
		return ctx.Err(), false
	}
	if time.Since(c.started) > workBuddyRetryWindow {
		return err, false
	}
	if budgetErr := s.checkRequestBudget(ctx, model); budgetErr != nil {
		return budgetErr, false
	}
	err = fn(ctx, acct)
	finishAttempt(ctx, err)
	return err, true
}

func canRecoverWorkBuddy(ctx context.Context, acct *codearts.Account, err error) bool {
	if err == nil || ctx.Err() != nil || acct.ProviderID() != provider.WorkBuddy {
		return false
	}
	c, ok := ctx.Value(usageContextKey{}).(*usageCapture)
	if !ok || c.started.IsZero() || c.attempts != 1 || c.attemptObserved {
		return false
	}
	var committed *committedStreamError
	var terminal interface{ NonRetryableRequest() bool }
	if errors.As(err, &committed) || errors.As(err, &terminal) && terminal.NonRetryableRequest() {
		return false
	}
	var upstream *provider.Error
	return errors.As(err, &upstream) && !upstream.AccountFault && !upstream.ContentBlocked &&
		workBuddyTransientStatus(upstream.Status) && time.Since(c.started)+workBuddyRetryWait <= workBuddyRetryWindow
}
