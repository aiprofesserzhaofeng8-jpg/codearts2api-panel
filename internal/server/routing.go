package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

type routingSessionKey struct{}
type routingModelKey struct{}

func withRoutingModel(ctx context.Context, model string) context.Context {
	return context.WithValue(ctx, routingModelKey{}, model)
}

// withRoutingSession scopes stickiness to the API key and model. Only hashes enter
// the pool. Explicit session IDs take priority; ordinary clients use the first user
// message and system instructions as a best-effort conversation identifier.
func withRoutingSession(r *http.Request, body []byte) context.Context {
	var req struct {
		Model          string          `json:"model"`
		ConversationID string          `json:"conversation_id"`
		System         json.RawMessage `json:"system"`
		Messages       []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &req) != nil {
		return r.Context()
	}
	id := r.Header.Get("X-Session-Id")
	if id == "" {
		id = req.ConversationID
	}
	var parts []json.RawMessage
	if id == "" {
		if len(req.System) > 0 {
			parts = append(parts, req.System)
		}
		foundUser := false
		for _, m := range req.Messages {
			if m.Role == "system" || m.Role == "developer" {
				parts = append(parts, m.Content)
			}
			if m.Role == "user" {
				parts = append(parts, m.Content)
				foundUser = true
				break
			}
		}
		if !foundUser {
			return r.Context()
		}
	}
	raw, _ := json.Marshal(struct {
		Key, Model, ID string
		Parts          []json.RawMessage
	}{extractKey(r), req.Model, id, parts})
	sum := sha256.Sum256(raw)
	return context.WithValue(r.Context(), routingSessionKey{}, hex.EncodeToString(sum[:]))
}

// Once output is visible to a client, another attempt would concatenate two
// answers/tool-call sequences. Preserve the original error for classification.
type committedStreamError struct{ err error }

func (e *committedStreamError) Error() string { return e.err.Error() }
func (e *committedStreamError) Unwrap() error { return e.err }
func stopAfterOutput(err error, started bool) error {
	if err != nil && started {
		return &committedStreamError{err: err}
	}
	return err
}
