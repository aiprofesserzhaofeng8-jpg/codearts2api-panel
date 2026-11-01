package server

import (
	"errors"
	"net/http"

	"codearts2api/internal/jobs"
)

func (s *Server) growthOperations() jobs.GrowthOperations {
	return jobs.GrowthOperations{
		Credit:            s.creditFor,
		PaidChat:          s.runGrowthChat,
		AllowCreditModels: func() bool { return s.Store.Settings().AllowCreditModels },
		ResolveModel:      func(id string) bool { _, ok := s.Reg.Resolve(id, nil); return ok },
	}
}

func (s *Server) writeGrowthError(w http.ResponseWriter, err error) {
	var rejection *jobs.GrowthRejection
	if errors.As(err, &rejection) {
		writeErr(w, rejection.Status, rejection.Message, "invalid_request_error")
		return
	}
	writeErr(w, 502, err.Error(), "upstream_error")
}
