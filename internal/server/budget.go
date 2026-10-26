package server

import (
	"codearts2api/internal/store"
	"context"
	"encoding/json"
	"net/http"
)

type budgetKeyContext struct{}

func (s *Server) withKeyBudget(style errStyle, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			next(w, r)
			return
		}
		key := extractKey(r)
		release, err := s.Store.BeginKeyBudget(key)
		if err != nil {
			if style == errAnthropic {
				writeAnthropicErr(w, 402, "permission_error", err.Error())
			} else {
				writeErr(w, 402, err.Error(), "budget_exceeded")
			}
			return
		}
		defer release()
		next(w, r.WithContext(context.WithValue(r.Context(), budgetKeyContext{}, key)))
	}
}
func (s *Server) checkRequestBudget(ctx context.Context, model string) error {
	key, _ := ctx.Value(budgetKeyContext{}).(string)
	return s.Store.CheckKeyBudgetProgress(key, model, requestAttemptDetails(ctx))
}
func (s *Server) handleKeyBudget(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		Ref    string          `json:"ref"`
		Budget store.KeyBudget `json:"budget"`
		Reset  bool            `json:"reset"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body) != nil {
		writeErr(w, 400, "预算格式无效", "invalid_request_error")
		return
	}
	if err := s.Store.SetKeyBudgetAndSave(s.resolveKeyRef(body.Ref), body.Budget, body.Reset); err != nil {
		writeErr(w, 400, err.Error(), "invalid_request_error")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
