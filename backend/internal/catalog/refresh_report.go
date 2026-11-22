package catalog

import (
	"maps"
	"time"
)

type PartitionStatus struct {
	Status      string     `json:"status"`
	Error       string     `json:"error,omitempty"`
	Count       int        `json:"count"`
	LastSuccess *time.Time `json:"last_success,omitempty"`
}
type RefreshReport struct {
	Status         string                     `json:"status"`
	Partitions     map[string]PartitionStatus `json:"partitions"`
	ReconcileError string                     `json:"reconcile_error,omitempty"`
}

func (s *Runtime) RefreshReport() RefreshReport {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r := s.report
	r.Partitions = maps.Clone(r.Partitions)
	return r
}
