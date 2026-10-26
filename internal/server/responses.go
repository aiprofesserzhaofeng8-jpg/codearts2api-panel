package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"codearts2api/internal/apierr"
	"codearts2api/internal/codearts"
	"codearts2api/internal/protocol"
	apicompat "codearts2api/internal/responsescompat"
)

// handleResponses uses Sub2API's Chat fallback converter while keeping this
// gateway's authentication, leases, refresh, timeout, retry and accounting.
func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeErr(w, 405, "仅支持 POST（Responses WebSocket 暂不支持）", "invalid_request_error")
		return
	}
	r = beginInference(w, r, "responses")
	start := time.Now()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<20))
	if err != nil {
		status := 400
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			status = 413
		}
		writeErr(w, status, "读取请求体失败: "+err.Error(), "invalid_request_error")
		return
	}
	var req apicompat.ResponsesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, 400, "请求体解析失败: "+err.Error(), "invalid_request_error")
		return
	}
	if err := validateResponsesRequest(body, &req); err != nil {
		writeErr(w, 400, err.Error(), "invalid_request_error")
		return
	}
	key := extractKey(r)
	scope := responsesCacheScope(key)
	s.responsesReasoning.recacheInput(scope, req.Input)
	chat, err := apicompat.ResponsesToChatCompletionsRequestWithOptions(&req, &apicompat.ResponsesToChatOptions{
		ReasoningContentByID: func(id string) string { return s.responsesReasoning.get(scope, id, time.Now()) },
	})
	if err != nil {
		writeErr(w, 400, err.Error(), "invalid_request_error")
		return
	}
	if len(chat.Messages) == 0 {
		writeErr(w, 400, "input 不能为空", "invalid_request_error")
		return
	}
	settings := s.Store.Settings()
	model, err := s.resolveModel(settings, req.Model)
	if err != nil {
		writeErr(w, apierr.From(err).Status, err.Error(), "invalid_request_error")
		return
	}
	if !settings.AllowCreditModels {
		writeErr(w, 403, "当前已关闭允许计费模型", "invalid_request_error")
		return
	}
	tools, err := apicompat.EffectiveResponsesTools(&req)
	if err != nil {
		writeErr(w, 400, err.Error(), "invalid_request_error")
		return
	}
	custom := apicompat.CustomToolNames(tools)
	functions := apicompat.FunctionToolNames(tools)
	namespaces := apicompat.NamespaceToolNames(tools)
	toolSearch := apicompat.HasToolSearchTool(tools)
	// CodeArts accepts max_tokens. The Sub2API converter uses max_completion_tokens.
	chat.Model = model.ID
	chat.MaxTokens, chat.MaxCompletionTokens = chat.MaxCompletionTokens, nil
	// Always consume the upstream stream, including for buffered clients. This
	// avoids the empty non-streaming rejection returned by some CodeArts models.
	chat.Stream = true
	chat.StreamOptions = &apicompat.ChatStreamOptions{IncludeUsage: true}
	upBody, err := json.Marshal(chat)
	if err != nil {
		writeErr(w, 400, err.Error(), "invalid_request_error")
		return
	}
	r = r.WithContext(withRoutingModel(withRoutingSession(r, upBody), model.ID))
	timeout := time.Duration(settings.RequestTimeout) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	promptLen := protocol.EstimatePromptLen(upBody)
	var flusher http.Flusher
	if req.Stream {
		var ok bool
		flusher, ok = w.(http.Flusher)
		if !ok {
			writeErr(w, 500, "服务端不支持流式响应", "server_error")
			return
		}
	}
	var started bool
	var state *apicompat.ChatCompletionsToResponsesStreamState
	var final *apicompat.ResponsesResponse
	outLen := 0
	writeEvents := func(events []apicompat.ResponsesStreamEvent) error {
		for _, event := range events {
			if event.Type == "response.output_item.done" && event.Item != nil {
				s.responsesReasoning.rememberOutput(scope, []apicompat.ResponsesOutput{*event.Item})
			}
			if event.Response != nil && event.Response.Status != "in_progress" {
				final = event.Response
				s.responsesReasoning.rememberOutput(scope, final.Output)
			}
			if !req.Stream {
				continue
			}
			wire, err := apicompat.ResponsesEventToSSE(event)
			if err != nil {
				return err
			}
			if !started {
				w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("X-Accel-Buffering", "no")
				w.Header().Set("X-Content-Type-Options", "nosniff")
				w.WriteHeader(200)
				started = true
			}
			if _, err := io.WriteString(w, wire); err != nil {
				return err
			}
		}
		if req.Stream && started {
			flusher.Flush()
		}
		return nil
	}
	acct, err := s.withAccountRetry(r.Context(), timeout, func(ctx context.Context, acct *codearts.Account) error {
		state = apicompat.NewChatCompletionsToResponsesStreamState(model.ID)
		state.CustomTools, state.FunctionTools = custom, functions
		state.NamespaceTools, state.ToolSearchDeclared = namespaces, toolSearch
		final = nil
		outLen = 0
		totalBytes := 0
		err := s.streamOnce(ctx, acct, model, upBody, func(raw []byte) error {
			totalBytes += len(raw)
			if totalBytes > 32<<20 {
				return fmt.Errorf("upstream response exceeds 32 MiB")
			}
			var chunk apicompat.ChatCompletionsChunk
			if err := json.Unmarshal(raw, &chunk); err != nil {
				return fmt.Errorf("invalid upstream Chat chunk: %w", err)
			}
			if len(chunk.Choices) == 0 && chunk.Usage == nil {
				return fmt.Errorf("upstream returned neither choices nor usage")
			}
			outLen += deltaLen(raw)
			return writeEvents(apicompat.ChatCompletionsChunkToResponsesEvents(&chunk, state))
		})
		if err == nil && state.FinishReason == "" {
			err = fmt.Errorf("upstream stream ended without finish_reason")
		}
		if err == nil {
			err = state.ValidateToolCallArguments()
		}
		if err == nil {
			err = writeEvents(apicompat.FinalizeChatCompletionsResponsesStream(state))
		}
		return stopAfterOutput(err, started)
	})
	if err != nil {
		e := apierr.From(err)
		s.record(r.Context(), key, acct, model.ID, req.Stream, e.Status, start, promptLen, outLen, err)
		if !started {
			writeErr(w, e.Status, err.Error(), e.OpenAI)
			return
		}
		// Do not manufacture a successful terminal event or replay partial tool calls.
		failed := apicompat.ResponsesStreamEvent{Type: "response.failed", SequenceNumber: state.SequenceNumber,
			Response: &apicompat.ResponsesResponse{ID: state.ResponseID, Object: "response", CreatedAt: state.Created,
				Model: model.ID, Status: "failed", Output: []apicompat.ResponsesOutput{},
				Error: &apicompat.ResponsesError{Code: e.OpenAI, Message: err.Error()}},
		}
		_ = writeEvents([]apicompat.ResponsesStreamEvent{failed})
		return
	}
	if final == nil {
		writeErr(w, 502, "upstream returned no response", "upstream_error")
		return
	}
	s.responsesReasoning.rememberOutput(scope, final.Output)
	s.record(r.Context(), key, acct, model.ID, req.Stream, 200, start, promptLen, outLen, nil)
	if req.Stream {
		// Matches Sub2API's Chat fallback terminal sentinel.
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
	} else {
		writeJSON(w, 200, final)
	}
}

func validateResponsesRequest(body []byte, req *apicompat.ResponsesRequest) error {
	if strings.TrimSpace(req.Model) == "" {
		return fmt.Errorf("model is required")
	}
	if req.PreviousResponseID != "" {
		return fmt.Errorf("previous_response_id is not supported by the Chat upstream; send the full conversation in input")
	}
	if req.Store != nil && *req.Store {
		return fmt.Errorf("store=true is not supported; use store=false and send history in input")
	}
	if req.MaxOutputTokens != nil && *req.MaxOutputTokens <= 0 {
		return fmt.Errorf("max_output_tokens must be positive")
	}
	var extra map[string]json.RawMessage
	if err := json.Unmarshal(body, &extra); err != nil {
		return err
	}
	for _, field := range []string{"background", "conversation", "context_management"} {
		v := strings.TrimSpace(string(extra[field]))
		if v != "" && v != "null" && v != "false" && v != "[]" {
			return fmt.Errorf("%s is not supported by the Chat upstream", field)
		}
	}
	tools, err := apicompat.EffectiveResponsesTools(req)
	if err != nil {
		return err
	}
	for _, tool := range tools {
		switch tool.Type {
		case "function", "custom", "namespace", "tool_search":
		default:
			return fmt.Errorf("tool type %q is not supported by CodeArts; use client-executed function/custom tools", tool.Type)
		}
	}
	return nil
}
