package gateway

import (
	"context"

	"codearts2api/internal/accounts"
	"codearts2api/internal/usage"
)

// AdmissionStore is owned by its consumer. Implementations must recheck current
// key/group/model permissions and reserve account capacity in one transaction.
// Never implement this contract by assembling independently locked snapshots.
type AdmissionStore interface {
	AcquireAccountForKey(session, model string, exclude map[string]bool, key string) (*accounts.Account, func(), error)
	AcquireMappedAccount(session, public string, models []string, exclude map[string]bool, key string) (*accounts.Account, string, func(), error)
	BeginKeyBudget(string) (func(), error)
	CheckKeyBudgetProgress(string, string, []usage.RequestAttempt) error
	CheckSearchBudget(string) error
}

// Admission owns request coordination, not mutable counters or a second lock.
// The same injected store owns budget reservations, leases and settlement state.
type Admission struct{ store AdmissionStore }

func NewAdmission(store AdmissionStore) *Admission { return &Admission{store: store} }

type AdmissionRequest struct {
	Session, Model, Key string
	Mapped              bool
	Public              string
	Sources             []string
	Exclude             map[string]bool
}

type AdmissionResult struct {
	Account *accounts.Account
	Model   string
	Release func()
}

// AdmissionDenied distinguishes a transaction's permission rejection from a
// budget or cancellation error. HTTP adapters retain their original projection.
type AdmissionDenied struct{ Cause error }

func (e *AdmissionDenied) Error() string { return e.Cause.Error() }
func (e *AdmissionDenied) Unwrap() error { return e.Cause }

// Acquire preserves the original order: raw-model progress check, cancellation,
// then one atomic selection/lease. A mapped model is checked after selection by
// the executor, once its real target has been projected into request context.
func (a *Admission) Acquire(ctx context.Context, request AdmissionRequest, check func(context.Context, string) error) (AdmissionResult, error) {
	if !request.Mapped && check != nil {
		if err := check(ctx, request.Model); err != nil {
			return AdmissionResult{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return AdmissionResult{}, err
	}
	return a.Lease(request)
}

// Lease is the atomic primitive for callers that already performed their own
// deadline and budget checks (external search and paid growth operations).
// It adds no cancellation point, retry, reservation or context projection.
func (a *Admission) Lease(request AdmissionRequest) (AdmissionResult, error) {
	result := AdmissionResult{Model: request.Model}
	var err error
	if request.Mapped {
		result.Account, result.Model, result.Release, err = a.store.AcquireMappedAccount(request.Session, request.Public, request.Sources, request.Exclude, request.Key)
	} else {
		result.Account, result.Release, err = a.store.AcquireAccountForKey(request.Session, request.Model, request.Exclude, request.Key)
	}
	if err != nil {
		return result, &AdmissionDenied{Cause: err}
	}
	return result, nil
}

func (a *Admission) Reserve(key string) (func(), error) { return a.store.BeginKeyBudget(key) }
func (a *Admission) CheckProgress(key, model string, attempts []usage.RequestAttempt) error {
	return a.store.CheckKeyBudgetProgress(key, model, attempts)
}
func (a *Admission) CheckSearch(key string) error { return a.store.CheckSearchBudget(key) }
