package server

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/gateway"
	"codearts2api/internal/protocol"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"codearts2api/internal/storage"
	"codearts2api/internal/upstream"
	"context"
	"encoding/json"
	"fmt"
)

// tryNative runs inside the existing account retry/lease. It never reselects an
// account and always starts from the original inbound request for each attempt.
func (s *Server) tryNative(ctx context.Context, acct *accounts.Account, model registry.Model, body []byte, p provider.Protocol, emit func([]byte) error) (bool, error) {
	if s.Providers == nil {
		return false, nil
	}
	adapter, ok := s.Providers.Get(acct.ProviderID())
	if !ok {
		return false, nil
	}
	native, ok := adapter.(provider.NativeStreaming)
	if !ok {
		return false, nil
	}
	actual, body, err := s.prepareMappedAttempt(ctx, acct, model, body)
	if err != nil {
		return true, err
	}
	target, err := native.ResolveProtocol(acct.Credentials, actual, p)
	if err != nil {
		return true, err
	}
	if target != p {
		return false, nil
	}
	startAttempt(ctx, acct, actual.ID)
	_, credentials, err := s.providerCredentials(ctx, acct)
	if err != nil {
		return true, err
	}
	target, err = native.ResolveProtocol(credentials, actual, p)
	if err != nil {
		return true, err
	}
	if target != p {
		return true, &provider.Error{Status: 409, Message: "账号协议已变化，请重新提交请求"}
	}
	_, _, upstreamModel := provider.ParseModel(actual.ID)
	session, _ := ctx.Value(routingSessionKey{}).(string)
	observer := nativeUsage{protocol: p}
	err = s.runProtocolPolicy(ctx, acct, actual, body, p, func(prepared []byte, observe func()) error {
		session := upstream.BindNative(native, credentials, provider.Request{Model: upstreamModel, Body: prepared, Protocol: p, SessionID: session, RequestID: requestID(ctx), SupportedProtocols: actual.SupportedProtocols})
		return s.Completions.Stream(ctx, session, actual.ID, prepared, gateway.Observer{Observe: observe, Usage: observer.capture}, func(raw []byte) error {
			projected, err := nativeResponseModel(raw, p, model.ID)
			if err != nil {
				return err
			}
			return emit(projected)
		})
	})
	return true, err
}

func nativeResponseModel(raw []byte, p provider.Protocol, name string) ([]byte, error) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return nil, fmt.Errorf("上游事件不是 JSON 对象")
	}
	field := "message"
	if p == provider.ResponsesProtocol {
		field = "response"
	}
	if len(obj[field]) == 0 {
		return raw, nil
	}
	b, err := protocol.MappedResponseModel(obj[field], name)
	if err != nil {
		return nil, err
	}
	obj[field] = b
	return json.Marshal(obj)
}

// Native usage snapshots are projected directly into the existing ledger. A
// partial Messages input count is not a complete billing receipt.
type nativeUsage struct {
	protocol                provider.Protocol
	input, cached, creation int64
	hasInput                bool
}

func (n *nativeUsage) capture(ctx context.Context, raw []byte) {
	c, ok := ctx.Value(usageContextKey{}).(*usageCapture)
	if !ok {
		return
	}
	c.attemptObserved = true
	var event struct {
		Type    string `json:"type"`
		Message struct {
			Usage json.RawMessage `json:"usage"`
		} `json:"message"`
		Response struct {
			Usage  json.RawMessage `json:"usage"`
			Status string          `json:"status"`
		} `json:"response"`
		Usage json.RawMessage `json:"usage"`
	}
	// Read only usage and completion metadata; business content remains opaque.
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &event) != nil || json.Unmarshal(raw, &envelope) != nil {
		return
	}
	if event.Type == "content_block_delta" || event.Type == "content_block_start" || event.Type == "response.output_text.delta" || event.Type == "response.function_call_arguments.delta" || event.Type == "response.reasoning_summary_text.delta" {
		captureFirstToken(c)
	}
	var u struct {
		Input        *int64 `json:"input_tokens"`
		Output       *int64 `json:"output_tokens"`
		Cached       int64  `json:"cache_read_input_tokens"`
		Creation     int64  `json:"cache_creation_input_tokens"`
		InputDetails struct {
			Cached int64 `json:"cached_tokens"`
		} `json:"input_tokens_details"`
		OutputDetails struct {
			Reasoning int64 `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	}
	if n.protocol == provider.ResponsesProtocol {
		if json.Unmarshal(event.Response.Usage, &u) != nil || u.Input == nil || u.Output == nil {
			return
		}
		c.usage = storage.TokenUsage{Known: true, InputTokens: max(0, *u.Input), OutputTokens: max(0, *u.Output), CachedTokens: min(max(0, u.InputDetails.Cached), max(0, *u.Input)), ReasoningTokens: min(max(0, u.OutputDetails.Reasoning), max(0, *u.Output))}
		if event.Response.Status == "completed" {
			c.finish = "stop"
		}
		if event.Response.Status == "incomplete" {
			c.finish = "length"
		}
		return
	}
	if event.Type == "message_start" {
		if json.Unmarshal(event.Message.Usage, &u) == nil && u.Input != nil {
			n.input, n.cached, n.creation, n.hasInput = max(0, *u.Input), max(0, u.Cached), max(0, u.Creation), true
		}
		return
	}
	if event.Type == "message_delta" {
		var delta struct {
			Stop string `json:"stop_reason"`
		}
		_ = json.Unmarshal(envelope["delta"], &delta)
		switch delta.Stop {
		case "max_tokens", "model_context_window_exceeded":
			c.finish = "length"
		case "tool_use":
			c.finish = "tool_calls"
		case "refusal":
			c.finish = "content_filter"
		case "end_turn", "stop_sequence":
			c.finish = "stop"
		}
		if json.Unmarshal(event.Usage, &u) == nil && u.Output != nil && n.hasInput {
			if u.Input != nil {
				n.input = max(0, *u.Input)
			}
			var deltaUsage map[string]json.RawMessage
			_ = json.Unmarshal(event.Usage, &deltaUsage)
			if _, ok := deltaUsage["cache_read_input_tokens"]; ok {
				n.cached = max(0, u.Cached)
			}
			if _, ok := deltaUsage["cache_creation_input_tokens"]; ok {
				n.creation = max(0, u.Creation)
			}
			c.usage = storage.TokenUsage{Known: true, InputTokens: n.input + n.cached + n.creation, OutputTokens: max(0, *u.Output), CachedTokens: n.cached}
		}
	}
}

// nativeDeltaLen counts only incremental output, not repeated terminal content.
func nativeDeltaLen(raw []byte) int {
	var obj struct {
		Type  string
		Delta json.RawMessage
	}
	if json.Unmarshal(raw, &obj) != nil {
		return 0
	}
	if obj.Type == "content_block_delta" {
		var delta struct{ Text, Thinking string }
		_ = json.Unmarshal(obj.Delta, &delta)
		return len([]rune(delta.Text)) + len([]rune(delta.Thinking))
	}
	if obj.Type == "response.output_text.delta" || obj.Type == "response.reasoning_summary_text.delta" {
		var delta string
		_ = json.Unmarshal(obj.Delta, &delta)
		return len([]rune(delta))
	}
	return 0
}
