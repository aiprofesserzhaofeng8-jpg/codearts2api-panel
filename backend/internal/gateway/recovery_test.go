package gateway

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type recoveryFixture struct {
	run    func(context.Context) error
	finish func(context.Context, error)
	can    func(context.Context, error) bool
	admit  func(context.Context) (bool, error)
}

func (f recoveryFixture) Run(ctx context.Context) error                   { return f.run(ctx) }
func (f recoveryFixture) Finish(ctx context.Context, err error)           { f.finish(ctx, err) }
func (f recoveryFixture) CanRecover(ctx context.Context, err error) bool  { return f.can(ctx, err) }
func (f recoveryFixture) AdmitRecovery(ctx context.Context) (bool, error) { return f.admit(ctx) }

func TestRecoveryOrderAndAdmission(t *testing.T) {
	for _, mode := range []string{"success", "repeat_failure", "disabled", "ineligible", "wait_failure", "cancel", "window_expired", "budget_denied"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			deadline, _ := ctx.Deadline()
			upstream, denied := errors.New("upstream"), errors.New("budget/permission")
			calls := 0
			var order []string
			attempt := recoveryFixture{
				run: func(got context.Context) error {
					calls++
					limit, _ := got.Deadline()
					if got != ctx || limit != deadline {
						t.Fatal("recovery changed context or shortened generation deadline")
					}
					order = append(order, "run")
					if calls == 1 || mode == "repeat_failure" {
						return upstream
					}
					return nil
				},
				finish: func(_ context.Context, err error) {
					if calls == 1 && err != upstream {
						t.Fatal("first attempt accounting lost error")
					}
					order = append(order, "finish")
				},
				can: func(_ context.Context, err error) bool {
					if err != upstream {
						t.Fatal("eligibility lost original error")
					}
					order = append(order, "eligible")
					return mode != "ineligible"
				},
				admit: func(context.Context) (bool, error) {
					order = append(order, "admit")
					if mode == "budget_denied" {
						return false, denied
					}
					return mode != "window_expired", nil
				},
			}
			options := RecoveryOptions{MayRepeat: mode != "disabled", Delay: 300 * time.Millisecond, Wait: func(got context.Context, delay time.Duration) error {
				if got != ctx || delay != 300*time.Millisecond {
					t.Fatal("recovery delay or context changed")
				}
				order = append(order, "wait")
				if mode == "wait_failure" {
					return denied
				}
				if mode == "cancel" {
					cancel()
				}
				return nil
			}}
			err, repeated := RunRecovery(ctx, options, attempt)
			wantErr, wantRepeated := error(upstream), false
			want := []string{"run", "finish"}
			if mode != "disabled" {
				want = append(want, "eligible")
			}
			if mode != "disabled" && mode != "ineligible" {
				want = append(want, "wait")
				if mode == "wait_failure" {
					wantErr = denied
				} else if mode == "cancel" {
					wantErr = context.Canceled
				} else {
					want = append(want, "admit")
					if mode == "budget_denied" {
						wantErr = denied
					} else if mode != "window_expired" {
						want = append(want, "run", "finish")
						wantRepeated = true
						if mode == "success" {
							wantErr = nil
						}
					}
				}
			}
			if err != wantErr || repeated != wantRepeated || !reflect.DeepEqual(order, want) {
				t.Fatalf("recovery changed: err=%v repeated=%v order=%v want=%v", err, repeated, order, want)
			}
		})
	}
}
