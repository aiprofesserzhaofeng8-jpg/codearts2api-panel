package server

import (
	"context"
	"errors"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/apierr"
	"codearts2api/internal/codearts"
	"codearts2api/internal/gateway"
	"codearts2api/internal/provider"
)

// Tests replace the wait locally; production uses one interruptible timer.
var waitRetry = gateway.WaitRetry

// withAccountRetry keeps the existing handler entry point. The gateway owns
// control flow; this driver retains atomic Store admission and platform policy.
func (s *Server) withAccountRetry(ctx context.Context, timeout time.Duration, fn func(context.Context, *accounts.Account) error) (*accounts.Account, error) {
	if len(s.Store.Accounts()) == 0 {
		return nil, apierr.NoAccount
	}
	attempts := s.Store.Settings().MaxRetries
	session, _ := ctx.Value(routingSessionKey{}).(string)
	model, _ := ctx.Value(routingModelKey{}).(string)
	return gateway.RunAccountRetry(ctx, gateway.RetryOptions{Attempts: attempts, Timeout: timeout, Unavailable: apierr.NoAccount, Wait: waitRetry}, &accountRetryDriver{s: s, fn: fn, session: session, model: model, plan: mappedPlan(ctx)})
}

type accountRetryDriver struct {
	s              *Server
	fn             func(context.Context, *accounts.Account) error
	session, model string
	plan           *groupModelPlan
}

func (d *accountRetryDriver) Acquire(ctx context.Context, tried map[string]bool) (gateway.RetryAttempt, error) {
	if d.plan == nil {
		if err := d.s.checkRequestBudget(ctx, d.model); err != nil {
			return gateway.RetryAttempt{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return gateway.RetryAttempt{}, err
	}
	var acct *accounts.Account
	var release func()
	var accessErr error
	if d.plan == nil {
		acct, release, accessErr = d.s.Store.AcquireAccountForKey(d.session, d.model, tried, groupKey(ctx))
	} else {
		acct, d.model, release, accessErr = d.s.Store.AcquireMappedAccount(d.session, d.plan.public, d.plan.ids, tried, groupKey(ctx))
	}
	if accessErr != nil {
		return gateway.RetryAttempt{}, groupDenied()
	}
	if acct != nil && d.plan != nil {
		ctx = withRoutingModel(ctx, d.model)
		if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
			c.selectedModel = d.model
		}
	}
	return gateway.RetryAttempt{Account: acct, Model: d.model, Context: ctx, Release: release}, nil
}

func (d *accountRetryDriver) Execute(a gateway.RetryAttempt, mayRepeat bool) (error, bool) {
	if d.plan != nil {
		if err := d.s.checkRequestBudget(a.Context, a.Model); err != nil {
			return err, false
		}
	}
	return d.s.runAccountAttempt(a.Context, a.Account, a.Model, d.fn, mayRepeat)
}

func (d *accountRetryDriver) Succeeded(a gateway.RetryAttempt) {
	d.s.Store.MarkModelResult(a.Account.ID, a.Model, true, "", 0)
	d.s.Store.MarkAccountResult(a.Account.ID, true, "")
}

func (d *accountRetryDriver) Canceled(a gateway.RetryAttempt) {
	d.s.Store.ForgetSession(d.session, a.Account.ID)
}

func (d *accountRetryDriver) Failed(a gateway.RetryAttempt, err error) gateway.RetryDecision {
	var external *provider.Error
	if errors.As(err, &external) {
		d.s.providerAccountError(a.Context, a.Account, a.Model, err)
		if external.ContentBlocked || !external.AccountFault && (external.Status == 400 || external.Status == 422 || external.Status == 403) {
			return gateway.RetryDecision{Stop: true}
		}
		return gateway.RetryDecision{Exclude: true}
	}
	d.s.Store.ForgetSession(d.session, a.Account.ID)
	if codearts.IsTransientUpstream(err) {
		cooldown := d.s.Store.Settings().Pool.RateCooldownSec
		if apierr.From(err).Status >= 500 {
			cooldown = min(cooldown, 5)
		}
		if a.Model == "" {
			d.s.Store.CooldownAccount(a.Account.ID, "上游限流或暂时不可用", cooldown)
		} else {
			d.s.Store.MarkModelResult(a.Account.ID, a.Model, false, "上游限流或暂时不可用", cooldown)
		}
		d.s.persist()
		return gateway.RetryDecision{Backoff: true}
	}
	if a.Model != "" && modelUnavailable(err) {
		d.s.Store.MarkModelResult(a.Account.ID, a.Model, false, "上游模型暂不可用", d.s.Store.Settings().Pool.BreakerCooldownSec)
		d.s.persist()
		return gateway.RetryDecision{Exclude: true}
	}
	if codearts.IsClientRequestError(err) {
		return gateway.RetryDecision{Stop: true}
	}
	d.s.Store.MarkAccountResult(a.Account.ID, false, err.Error())
	if codearts.IsCredentialsError(err) {
		d.s.Store.CooldownAccount(a.Account.ID, "凭据失效，请重新登录", d.s.Store.Settings().Pool.BreakerCooldownSec)
	}
	d.s.persist()
	return gateway.RetryDecision{Exclude: true}
}
