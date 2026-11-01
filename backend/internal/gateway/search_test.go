package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
)

type searchAdapter struct {
	provider.Adapter
	search func(context.Context, json.RawMessage, provider.SearchRequest) (*provider.SearchOutcome, error)
}

func (s searchAdapter) Search(ctx context.Context, raw json.RawMessage, in provider.SearchRequest) (*provider.SearchOutcome, error) {
	return s.search(ctx, raw, in)
}

func TestSearchExecutionPreservesBackendCredentialAndDeadlineBoundary(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(map[bool]string{true: "external", false: "native"}[external], func(t *testing.T) {
			account := &accounts.Account{ID: "fixture"}
			outcome := &provider.SearchOutcome{}
			input := provider.SearchRequest{Query: "query", SessionID: "session", RequestID: "request", TraceID: "trace", ConversationRequestID: "conversation", MaxResults: 3}
			var calls []string
			invoke := func(ctx context.Context, timeout time.Duration, call SearchCall) (*accounts.Account, error) {
				if timeout != 7*time.Second {
					t.Fatal("timeout changed", timeout)
				}
				ctx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				return account, call(ctx, account)
			}
			validate := func(ctx context.Context, in provider.SearchRequest) {
				if in != input {
					t.Fatal("search metadata changed", in)
				}
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 7*time.Second {
					t.Fatal("deadline not shared")
				}
			}
			ops := SearchOperations{
				Start: func(_ context.Context, a *accounts.Account, id string) {
					if a != account || id != "model" {
						t.Fatal(a, id)
					}
					calls = append(calls, "start")
				},
				Credentials: func(context.Context, *accounts.Account) (provider.Adapter, json.RawMessage, error) {
					calls = append(calls, "credentials")
					return searchAdapter{search: func(ctx context.Context, raw json.RawMessage, in provider.SearchRequest) (*provider.SearchOutcome, error) {
						validate(ctx, in)
						if string(raw) != "credential" {
							t.Fatal("credential changed")
						}
						calls = append(calls, "search")
						return outcome, nil
					}}, json.RawMessage("credential"), nil
				},
				External: func(ctx context.Context, url string, in provider.SearchRequest) (*provider.SearchOutcome, error) {
					validate(ctx, in)
					if url != "external-url" {
						t.Fatal(url)
					}
					calls = append(calls, "external")
					return outcome, nil
				},
				AccountRetry: func(ctx context.Context, d time.Duration, call SearchCall) (*accounts.Account, error) {
					calls = append(calls, "retry")
					return invoke(ctx, d, call)
				},
				ExternalAccount: func(ctx context.Context, d time.Duration, id string, call SearchCall) (*accounts.Account, error) {
					calls = append(calls, "lease")
					if id != "model" {
						t.Fatal(id)
					}
					return invoke(ctx, d, call)
				},
			}
			options := SearchOptions{Model: "model", RequestTimeout: 7, URL: "external-url"}
			want := []string{"retry", "start", "credentials", "search"}
			if external {
				options.Backend = "searxng"
				want = []string{"lease", "start", "external"}
			}
			a, out, err := RunSearch(context.Background(), options, input, ops)
			if a != account || out != outcome || err != nil || !reflect.DeepEqual(calls, want) {
				t.Fatal(a, out, err, calls)
			}
		})
	}
}

type searchAdmission struct {
	check   func(context.Context) error
	acquire func(context.Context) (*accounts.Account, func(), error)
}

func (a searchAdmission) Check(ctx context.Context) error { return a.check(ctx) }
func (a searchAdmission) Acquire(ctx context.Context) (*accounts.Account, func(), error) {
	return a.acquire(ctx)
}
func TestExternalSearchAdmissionOrderingAndLeaseRelease(t *testing.T) {
	denied := errors.New("budget denied")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	admission := searchAdmission{check: func(context.Context) error { return denied }, acquire: func(context.Context) (*accounts.Account, func(), error) {
		t.Fatal("acquired after denial")
		return nil, nil, nil
	}}
	if _, err := RunExternalSearchAccount(ctx, time.Second, admission, nil); err != denied {
		t.Fatal("budget/cancellation ordering changed", err)
	}
	admission.check = func(context.Context) error { return nil }
	if _, err := RunExternalSearchAccount(ctx, time.Second, admission, nil); err != context.Canceled {
		t.Fatal(err)
	}
	account := &accounts.Account{ID: "fixture"}
	released := 0
	admission.acquire = func(context.Context) (*accounts.Account, func(), error) { return account, func() { released++ }, nil }
	a, err := RunExternalSearchAccount(context.Background(), time.Second, admission, func(context.Context, *accounts.Account) error { return denied })
	if a != account || err != denied || released != 1 {
		t.Fatal(a, err, released)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panic swallowed")
			}
		}()
		_, _ = RunExternalSearchAccount(context.Background(), time.Second, admission, func(context.Context, *accounts.Account) error { panic("callback") })
	}()
	if released != 2 {
		t.Fatal("panic leaked lease")
	}
	admission.acquire = func(context.Context) (*accounts.Account, func(), error) { return nil, nil, nil }
	if _, err := RunExternalSearchAccount(context.Background(), time.Second, admission, nil); !errors.Is(err, accounts.ErrNoAccount) {
		t.Fatal(err)
	}
}

func TestNativeSearchNilOutcomeAndCredentialErrors(t *testing.T) {
	account := &accounts.Account{ID: "fixture"}
	denied := errors.New("credentials")
	ops := SearchOperations{Start: func(context.Context, *accounts.Account, string) {}, AccountRetry: func(ctx context.Context, d time.Duration, call SearchCall) (*accounts.Account, error) {
		if d != 20*time.Second {
			t.Fatal("default timeout changed")
		}
		return account, call(ctx, account)
	}, Credentials: func(context.Context, *accounts.Account) (provider.Adapter, json.RawMessage, error) {
		return nil, nil, denied
	}}
	if _, _, err := RunSearch(context.Background(), SearchOptions{RequestTimeout: 60}, provider.SearchRequest{}, ops); err != denied {
		t.Fatal(err)
	}
	ops.Credentials = func(context.Context, *accounts.Account) (provider.Adapter, json.RawMessage, error) {
		return searchAdapter{search: func(context.Context, json.RawMessage, provider.SearchRequest) (*provider.SearchOutcome, error) {
			return nil, nil
		}}, nil, nil
	}
	if _, _, err := RunSearch(context.Background(), SearchOptions{}, provider.SearchRequest{}, ops); err == nil || err.Error() != "上游搜索未返回结果" {
		t.Fatal(err)
	}
	ops.Credentials = func(context.Context, *accounts.Account) (provider.Adapter, json.RawMessage, error) {
		return nil, nil, nil
	}
	_, _, err := RunSearch(context.Background(), SearchOptions{}, provider.SearchRequest{}, ops)
	var upstream *provider.Error
	if !errors.As(err, &upstream) || upstream.Status != 400 {
		t.Fatal(err)
	}
}
