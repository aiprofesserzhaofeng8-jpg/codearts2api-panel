package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"codearts2api/internal/gateway"
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
		Input          json.RawMessage `json:"input"`
		Instructions   json.RawMessage `json:"instructions"`
		Messages       []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &req) != nil {
		return r.Context()
	}
	if len(req.Input) > 0 {
		if len(req.Instructions) > 0 {
			req.System = req.Instructions
		}
		if json.Unmarshal(req.Input, &req.Messages) != nil {
			var text string
			if json.Unmarshal(req.Input, &text) == nil {
				var wrapper struct{ Messages json.RawMessage }
				wrapper.Messages, _ = json.Marshal([]map[string]string{{"role": "user", "content": text}})
				_ = json.Unmarshal(wrapper.Messages, &req.Messages)
			}
		}
	}
	id := r.Header.Get("X-Session-Id")
	if id == "" {
		id = r.Header.Get("Session_id")
	}
	if id == "" {
		id = r.Header.Get("X-Conversation-Id")
	}
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

// HTTP handlers report their commit boundary without losing error identity.
func stopAfterOutput(err error, started bool) error {
	return gateway.StopAfterOutput(err, started)
}
