package server

import (
	"codearts2api/internal/access"
	"codearts2api/internal/apierr"
	"context"
	"encoding/json"
	"net/http"
)

type budgetKeyContext struct{}
type consumerRequestKey struct{}
type consumerRequest struct {
	id         string
	inputBytes int
}

func (s *Server) withKeyBudget(style errStyle, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			next(w, r)
			return
		}
		key := extractKey(r)
		release, metered, err := s.Admission.ReserveRequest(key)
		if err != nil {
			mapped := apierr.From(err)
			if style == errAnthropic {
				writeAnthropicErr(w, mapped.Status, mapped.Anthropic, err.Error(), apierr.Reason(err))
			} else {
				writeErr(w, mapped.Status, err.Error(), mapped.OpenAI, apierr.Reason(err))
			}
			return
		}
		defer release()
		if !metered {
			next(w, r.WithContext(context.WithValue(r.Context(), budgetKeyContext{}, key)))
			return
		}
		quote := &consumerRequest{}
		defer func() { s.Admission.ReleaseQuote(quote.id) }()
		ctx := context.WithValue(r.Context(), budgetKeyContext{}, key)
		ctx = context.WithValue(ctx, consumerRequestKey{}, quote)
		next(w, r.WithContext(ctx))
	}
}
func (s *Server) checkRequestBudget(ctx context.Context, model string) error {
	if err := s.checkGroupModel(ctx, model); err != nil {
		return err
	}
	key, _ := ctx.Value(budgetKeyContext{}).(string)
	return s.admitAttemptPrice(ctx, key, model)
}
func (s *Server) admitAttemptPrice(ctx context.Context, key, model string) error {
	consumer, metered := ctx.Value(consumerRequestKey{}).(*consumerRequest)
	var id string
	var size int
	if metered {
		id, size = consumer.id, consumer.inputBytes
	}
	price, err := s.Admission.AdmitAttempt(key, id, model, size, metered, requestAttemptDetails(ctx))
	if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
		c.pendingPrice = price
	}
	return err
}
func (s *Server) handleKeyBudget(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		Ref    string           `json:"ref"`
		Budget access.KeyBudget `json:"budget"`
		Reset  bool             `json:"reset"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body) != nil {
		writeErr(w, 400, "预算格式无效", "invalid_request_error")
		return
	}
	if err := s.AccessManagement.SetKeyBudget(body.Ref, body.Budget, body.Reset); err != nil {
		writeErr(w, 400, err.Error(), "invalid_request_error")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
