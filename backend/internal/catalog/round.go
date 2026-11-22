package catalog

import (
	"context"
	"time"
)

type refreshRound struct {
	done   chan struct{}
	report RefreshReport
	err    error
}

// BindRefreshContext binds background discovery to the serving process.
func (s *Runtime) BindRefreshContext(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.process = ctx
}

func (s *Runtime) beginRound(ctx context.Context, maxAge time.Duration, ops RefreshOperations) *refreshRound {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.round != nil {
		return s.round
	}
	if maxAge > 0 && (s.process == nil || time.Since(s.loadedAt) <= maxAge) {
		return nil
	}
	if s.process != nil {
		ctx = s.process
	}
	if ctx.Err() != nil {
		return nil
	}
	round := &refreshRound{done: make(chan struct{})}
	s.round = round
	go func() {
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		round.err = s.Refresh(ctx, ops)
		s.mu.Lock()
		round.report = s.report
		s.round = nil
		close(round.done)
		s.mu.Unlock()
	}()
	return round
}

func (s *Runtime) SignalIfStale(maxAge time.Duration, ops RefreshOperations) {
	s.beginRound(context.Background(), maxAge, ops)
}

func (s *Runtime) JoinRefresh(ctx context.Context, ops RefreshOperations) error {
	_, err := s.WaitRefresh(ctx, ops)
	return err
}

func (s *Runtime) WaitRefresh(ctx context.Context, ops RefreshOperations) (RefreshReport, error) {
	round := s.beginRound(ctx, 0, ops)
	if round == nil {
		return RefreshReport{}, context.Canceled
	}
	select {
	case <-ctx.Done():
		return RefreshReport{}, ctx.Err()
	case <-round.done:
		return round.report, round.err
	}
}
