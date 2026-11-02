package gateway

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"codearts2api/internal/accounts"
	"codearts2api/internal/usage"
)

type admissionFixture struct {
	events  []string
	request AdmissionRequest
	err     error
}

func (f *admissionFixture) AcquireAccountForKey(session, model string, exclude map[string]bool, key string) (*accounts.Account, func(), error) {
	f.events = append(f.events, "raw")
	f.request = AdmissionRequest{Session: session, Model: model, Key: key, Exclude: exclude}
	return &accounts.Account{ID: "raw"}, func() { f.events = append(f.events, "release") }, f.err
}
func (f *admissionFixture) AcquireMappedAccount(session, public string, sources []string, exclude map[string]bool, key string) (*accounts.Account, string, func(), error) {
	f.events = append(f.events, "mapped")
	f.request = AdmissionRequest{Session: session, Public: public, Sources: sources, Key: key, Exclude: exclude, Mapped: true}
	return &accounts.Account{ID: "mapped"}, "workbuddy/cn/real-v1", func() { f.events = append(f.events, "release") }, f.err
}
func (f *admissionFixture) BeginKeyBudget(string) (func(), error) {
	f.events = append(f.events, "reserve")
	return func() { f.events = append(f.events, "settled") }, f.err
}
func (f *admissionFixture) CheckKeyBudgetProgress(string, string, []usage.RequestAttempt) error {
	return f.err
}
func (f *admissionFixture) CheckSearchBudget(string) error { return f.err }

func TestAdmissionOrderingAndRealTarget(t *testing.T) {
	f := &admissionFixture{}
	a := NewAdmission(f)
	request := AdmissionRequest{Session: "session", Model: "real-v1", Key: "key", Exclude: map[string]bool{"old": true}}
	check := func(context.Context, string) error { f.events = append(f.events, "check"); return nil }
	selected, err := a.Acquire(t.Context(), request, check)
	if err != nil || selected.Model != request.Model || !reflect.DeepEqual(f.request, request) || !reflect.DeepEqual(f.events, []string{"check", "raw"}) {
		t.Fatal(selected, err, f)
	}
	selected.Release()
	f.events = nil
	request.Mapped = true
	request.Public = "public-v1"
	request.Sources = []string{"workbuddy/cn/real-v1"}
	selected, err = a.Acquire(t.Context(), request, check)
	if err != nil || selected.Model != "workbuddy/cn/real-v1" || !reflect.DeepEqual(f.events, []string{"mapped"}) || !reflect.DeepEqual(f.request.Sources, request.Sources) {
		t.Fatal(selected, err, f)
	}
	if len(request.Sources) != 1 {
		t.Fatal("request plan mutated")
	}
}

func TestAdmissionPreservesFailureAndCancellationBoundary(t *testing.T) {
	f := &admissionFixture{}
	a := NewAdmission(f)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	budget := errors.New("budget")
	_, err := a.Acquire(ctx, AdmissionRequest{}, func(context.Context, string) error { return budget })
	if !errors.Is(err, budget) || len(f.events) != 0 {
		t.Fatal(err, f.events)
	}
	_, err = a.Acquire(ctx, AdmissionRequest{}, nil)
	if !errors.Is(err, context.Canceled) || len(f.events) != 0 {
		t.Fatal(err, f.events)
	}
	f.err = errors.New("mapping withdrawn")
	_, err = a.Acquire(t.Context(), AdmissionRequest{Mapped: true}, nil)
	var denied *AdmissionDenied
	if !errors.As(err, &denied) || !errors.Is(err, f.err) {
		t.Fatal(err)
	}
	f.err = nil
	f.events = nil
	selected, err := a.Lease(AdmissionRequest{})
	if err != nil || selected.Account == nil {
		t.Fatal(err)
	}
	selected.Release()
	if !reflect.DeepEqual(f.events, []string{"raw", "release"}) {
		t.Fatal(f.events)
	}
	release, err := a.Reserve("key")
	if err != nil {
		t.Fatal(err)
	}
	release()
	if f.events[len(f.events)-1] != "settled" {
		t.Fatal(f.events)
	}
}
