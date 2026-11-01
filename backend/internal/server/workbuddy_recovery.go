package server

import (
	"context"
	"errors"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/gateway"
	"codearts2api/internal/provider"
)

const (
	workBuddyRetryWait   = 300 * time.Millisecond
	workBuddyRetryWindow = 3 * time.Second
)

func workBuddyTransientStatus(status int) bool {
	return status == 500 || status == 502 || status == 503 || status == 504
}

// WorkBuddy eligibility remains platform policy. The gateway owns the bounded
// recovery sequence; this adapter keeps usage and authorization in their owner.
func (s *Server) runAccountAttempt(ctx context.Context, acct *accounts.Account, model string, fn func(context.Context, *accounts.Account) error, mayRepeat bool) (error, bool) {
	return gateway.RunRecovery(ctx, gateway.RecoveryOptions{MayRepeat: mayRepeat, Delay: workBuddyRetryWait, Wait: waitRetry}, &accountRecovery{s: s, acct: acct, model: model, fn: fn})
}

type accountRecovery struct {
	s     *Server
	acct  *accounts.Account
	model string
	fn    func(context.Context, *accounts.Account) error
}

func (a *accountRecovery) Run(ctx context.Context) error       { return a.fn(ctx, a.acct) }
func (*accountRecovery) Finish(ctx context.Context, err error) { finishAttempt(ctx, err) }
func (a *accountRecovery) CanRecover(ctx context.Context, err error) bool {
	return canRecoverWorkBuddy(ctx, a.acct, err)
}
func (a *accountRecovery) AdmitRecovery(ctx context.Context) (bool, error) {
	c := ctx.Value(usageContextKey{}).(*usageCapture)
	if time.Since(c.started) > workBuddyRetryWindow {
		return false, nil
	}
	if err := a.s.checkRequestBudget(ctx, a.model); err != nil {
		return false, err
	}
	return true, nil
}

func canRecoverWorkBuddy(ctx context.Context, acct *accounts.Account, err error) bool {
	if err == nil || ctx.Err() != nil || acct.ProviderID() != provider.WorkBuddy {
		return false
	}
	c, ok := ctx.Value(usageContextKey{}).(*usageCapture)
	if !ok || c.started.IsZero() || c.attempts != 1 || c.attemptObserved {
		return false
	}
	var committed *gateway.CommittedStreamError
	var terminal interface{ NonRetryableRequest() bool }
	if errors.As(err, &committed) || errors.As(err, &terminal) && terminal.NonRetryableRequest() {
		return false
	}
	var upstream *provider.Error
	return errors.As(err, &upstream) && !upstream.AccountFault && !upstream.ContentBlocked &&
		workBuddyTransientStatus(upstream.Status) && time.Since(c.started)+workBuddyRetryWait <= workBuddyRetryWindow
}
