package accounts

import (
	"context"
	"time"

	"codearts2api/internal/provider"
)

type ProbeStore interface {
	Accounts() []*Account
	Account(string) *Account
	MarkAccountResult(string, bool, string)
	Save() error
}

type ProbeDirectory interface {
	ReplaceProvider(string, string, []provider.Model)
}

// ProbeCredentialInfo contains only the native session's existing admin hints.
// A non-nil value must be retained even when the probe itself fails.
type ProbeCredentialInfo struct {
	ExpiresAt int64
	AccessKey string
}

type ProbeOperations struct {
	Invalidate func(string)
	Native     func(context.Context, *Account) (*ProbeCredentialInfo, error)
	Provider   func(context.Context, *Account) ([]provider.Model, error)
}

type ProbeResult struct {
	ID, Name, Error string
	OK              bool
	Credentials     *ProbeCredentialInfo
}

// Probes owns the legacy administrative connectivity check, not model inference
// admission. It borrows the existing store, directory and credential operations.
type Probes struct {
	store     ProbeStore
	directory ProbeDirectory
	logf      func(string, ...any)
}

func NewProbes(store ProbeStore, directory ProbeDirectory, logf func(string, ...any)) *Probes {
	return &Probes{store: store, directory: directory, logf: logf}
}

// Run preserves sequential checks (including disabled/paused accounts), native
// invalidation before the timeout, result publication after cancel, and one
// final logged save even when the explicit account ID is missing.
func (p *Probes) Run(ctx context.Context, id string, ops ProbeOperations) []ProbeResult {
	targets := p.store.Accounts()
	if id != "" {
		targets = nil
		if a := p.store.Account(id); a != nil {
			targets = append(targets, a)
		}
	}
	results := make([]ProbeResult, 0, len(targets))
	for _, a := range targets {
		var info *ProbeCredentialInfo
		var err error
		if a.ProviderID() != provider.CodeArts {
			callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			var models []provider.Model
			models, err = ops.Provider(callCtx, a)
			if err == nil {
				p.directory.ReplaceProvider(a.ProviderID(), a.Scope, models)
			}
			cancel()
		} else {
			ops.Invalidate(a.ID)
			callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			info, err = ops.Native(callCtx, a)
			cancel()
		}
		note := ""
		if err != nil {
			note = err.Error()
		}
		p.store.MarkAccountResult(a.ID, err == nil, note)
		results = append(results, ProbeResult{ID: a.ID, Name: a.DisplayName(), OK: err == nil, Error: note, Credentials: info})
	}
	if err := p.store.Save(); err != nil && p.logf != nil {
		p.logf("保存状态文件失败: %v", err)
	}
	return results
}
