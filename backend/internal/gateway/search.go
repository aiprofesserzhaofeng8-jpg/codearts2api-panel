package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
)

type SearchOptions struct {
	Model, Backend, URL string
	RequestTimeout      int
}
type SearchCall = func(context.Context, *accounts.Account) error
type SearchOperations struct {
	Start           func(context.Context, *accounts.Account, string)
	Credentials     func(context.Context, *accounts.Account) (provider.Adapter, json.RawMessage, error)
	External        func(context.Context, string, provider.SearchRequest) (*provider.SearchOutcome, error)
	ExternalAccount func(context.Context, time.Duration, string, SearchCall) (*accounts.Account, error)
	AccountRetry    func(context.Context, time.Duration, SearchCall) (*accounts.Account, error)
}

// RunSearch preserves the two distinct execution paths. External search never
// resolves model credentials or enters model retry/health classification.
func RunSearch(ctx context.Context, options SearchOptions, input provider.SearchRequest, ops SearchOperations) (*accounts.Account, *provider.SearchOutcome, error) {
	timeout := 20 * time.Second
	if options.RequestTimeout > 0 {
		timeout = min(timeout, time.Duration(options.RequestTimeout)*time.Second)
	}
	var outcome *provider.SearchOutcome
	call := func(ctx context.Context, a *accounts.Account) error {
		ops.Start(ctx, a, options.Model)
		if options.Backend == "searxng" {
			var err error
			outcome, err = ops.External(ctx, options.URL, input)
			return err
		}
		adapter, raw, err := ops.Credentials(ctx, a)
		if err != nil {
			return err
		}
		searcher, ok := adapter.(provider.Searcher)
		if !ok {
			return &provider.Error{Status: 400, Message: "该上游未接入联网搜索"}
		}
		outcome, err = searcher.Search(ctx, raw, input)
		if err == nil && outcome == nil {
			return fmt.Errorf("上游搜索未返回结果")
		}
		return err
	}
	var acct *accounts.Account
	var err error
	if options.Backend == "searxng" {
		acct, err = ops.ExternalAccount(ctx, timeout, options.Model, call)
	} else {
		acct, err = ops.AccountRetry(ctx, timeout, call)
	}
	return acct, outcome, err
}

// ExternalSearchAdmission adapts the existing atomic Store entry. Implementors
// must not split group authorization and concurrency occupation into two writes.
type ExternalSearchAdmission interface {
	Check(context.Context) error
	Acquire(context.Context) (*accounts.Account, func(), error)
}

// The type parameter keeps the narrow admission contract without boxing a
// per-request adapter onto the heap. No state or extra lease is introduced.
func RunExternalSearchAccount[T ExternalSearchAdmission](ctx context.Context, timeout time.Duration, admission T, call SearchCall) (*accounts.Account, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := admission.Check(ctx); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	acct, release, err := admission.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	if acct == nil {
		return nil, accounts.ErrNoAccount
	}
	defer release()
	return acct, call(ctx, acct)
}
