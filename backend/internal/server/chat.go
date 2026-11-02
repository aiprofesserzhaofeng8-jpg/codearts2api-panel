package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/apierr"
	"codearts2api/internal/codearts"
	"codearts2api/internal/gateway"
	"codearts2api/internal/protocol"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"codearts2api/internal/storage"
)

// chatRequest 是 OpenAI Chat Completions 的请求体（仅取网关需要处理的字段）。
//
// 其余字段（tools / response_format / temperature / …）不解析，
// 而是原样转发给上游 —— 上游本身就是 OpenAI 兼容协议，无需转换。
type chatRequest struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
}

// handleChatCompletions 是 /v1/chat/completions 的主入口。
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST", "invalid_request_error")
		return
	}
	r = beginInference(w, r, "chat")
	start := time.Now()
	var req chatRequest
	reject := func(status int, message, kind string) {
		s.recordRejectedInference(r, req.Model, req.Stream, start, status, message)
		writeErr(w, status, message, kind)
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<20))
	captureConsumerInput(r.Context(), len(body))
	if err != nil {
		reject(http.StatusBadRequest, "读取请求体失败: "+err.Error(), "invalid_request_error")
		return
	}

	if err := json.Unmarshal(body, &req); err != nil {
		reject(http.StatusBadRequest, "请求体解析失败: "+err.Error(), "invalid_request_error")
		return
	}
	configureInference(r.Context(), start, req.Stream)
	if len(req.Messages) == 0 {
		reject(http.StatusBadRequest, "messages 不能为空", "invalid_request_error")
		return
	}

	settings := s.Store.Settings()
	model, err := s.resolveModelForRequest(r, settings, req.Model)
	if err != nil {
		reject(apierr.From(err).Status, err.Error(), "invalid_request_error")
		return
	}
	req.Model = model.ID
	if !settings.AllowCreditModels {
		reject(http.StatusForbidden,
			"模型 "+model.ID+" 会真实消耗积分，当前已在控制台关闭。如需使用请开启「允许计费模型」。",
			"invalid_request_error")
		return
	}

	upBody, err := rewriteModel(body, model.ID, req.Stream)
	if err != nil {
		reject(http.StatusBadRequest, "请求体改写失败: "+err.Error(), "invalid_request_error")
		return
	}

	key := extractKey(r)
	r = r.WithContext(withRoutingModel(withRoutingSession(r, upBody), model.ID))
	timeout := time.Duration(settings.RequestTimeout) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	promptLen := protocol.EstimatePromptLen(body)

	if req.Stream {
		s.streamChat(w, r, upBody, model, key, timeout, start, promptLen)
		return
	}
	s.blockingChat(w, r, upBody, model, key, timeout, start, promptLen)
}

// blockingChat 收集完整回复后一次性返回。
func (s *Server) blockingChat(w http.ResponseWriter, r *http.Request, body []byte, model registry.Model, key string, timeout time.Duration, start time.Time, promptLen int) {
	var (
		raw    []byte
		outLen int
	)

	acct, err := s.withAccountRetry(r.Context(), timeout, func(ctx context.Context, acct *accounts.Account) error {
		res, err := s.chatOnce(ctx, acct, model, body)
		if err != nil {
			return err
		}
		if model.Source == "mapping" {
			res, err = mappedResponseModel(res, model.ID)
			if err != nil {
				return err
			}
		}
		raw = res
		return nil
	})
	if err != nil {
		e := apierr.From(err)
		s.record(r.Context(), key, acct, model.ID, false, e.Status, start, promptLen, 0, err)
		writeErr(w, e.Status, err.Error(), e.OpenAI)
		return
	}

	outLen = completionLen(raw)
	s.record(r.Context(), key, acct, model.ID, false, http.StatusOK, start, promptLen, outLen, nil)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// streamChat 以 OpenAI SSE 格式流式返回（上游本身就是 SSE，做透明转发 + 错误识别）。
func (s *Server) streamChat(w http.ResponseWriter, r *http.Request, body []byte, model registry.Model, key string, timeout time.Duration, start time.Time, promptLen int) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "服务端不支持流式响应", "server_error")
		return
	}

	// 先尝试建立上游连接；失败时还能以普通 HTTP 错误返回。
	var (
		started   bool
		outLen    int
		streamErr error
	)

	acct, err := s.withAccountRetry(r.Context(), timeout, func(ctx context.Context, acct *accounts.Account) error {
		err := s.streamOnce(ctx, acct, model, body, func(chunk []byte) error {
			if model.Source == "mapping" {
				var err error
				chunk, err = mappedResponseModel(chunk, model.ID)
				if err != nil {
					return err
				}
			}
			if !started {
				h := w.Header()
				h.Set("Content-Type", "text/event-stream; charset=utf-8")
				h.Set("Cache-Control", "no-cache")
				h.Set("Connection", "keep-alive")
				h.Set("X-Accel-Buffering", "no")
				h.Set("X-Content-Type-Options", "nosniff")
				w.WriteHeader(http.StatusOK)
				started = true
			}
			outLen += deltaLen(chunk)
			if _, err := fmt.Fprintf(w, "data: %s\n\n", chunk); err != nil {
				return err
			}
			flusher.Flush()
			return nil
		})
		return stopAfterOutput(err, started)
	})
	streamErr = err

	if !started {
		// 上游连一次都没成功：按普通错误返回，客户端能拿到明确的状态码
		e := apierr.From(err)
		msg := "全部账号均不可用"
		if err != nil {
			msg = err.Error()
		}
		s.record(r.Context(), key, acct, model.ID, true, e.Status, start, promptLen, outLen, err)
		writeErr(w, e.Status, msg, e.OpenAI)
		return
	}

	if streamErr != nil {
		// 已经在流里了，只能以 SSE 错误块收尾（OpenAI 客户端只看 error 对象）
		e := apierr.From(streamErr)
		errChunk, _ := json.Marshal(map[string]any{
			"error": map[string]any{
				"message": streamErr.Error(),
				"type":    e.OpenAI,
				"code":    e.Status,
			},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", errChunk)
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	flusher.Flush()

	s.record(r.Context(), key, acct, model.ID, true, apierr.From(streamErr).Status, start, promptLen, outLen, streamErr)
}

// chatOnce 用指定账号发一次非流式请求。
//
// 上游对个别模型（实测 glm-5.3）的非流式接口只回一个空 body 的 400，
// 这里检测到该特征后自动改用「流式 + 本地聚合」，对客户端保持非流式语义。
func (s *Server) chatOnce(ctx context.Context, acct *accounts.Account, model registry.Model, body []byte) ([]byte, error) {
	if mappedPlan(ctx) != nil {
		var err error
		model, body, err = s.prepareMappedAttempt(ctx, acct, model, body)
		if err != nil {
			return nil, err
		}
	}
	if acct.ProviderID() != provider.CodeArts {
		return s.chatProvider(ctx, acct, model, body)
	}

	startAttempt(ctx, acct, model.ID)
	sess, err := s.Cred.Session(ctx, acct, s.resolveOptions())
	if err != nil {
		return nil, err
	}

	var result []byte
	err = s.runRequestPolicy(ctx, acct, model, body, func(prepared []byte, observe func()) error {
		var callErr error
		result, callErr = s.codeartsChatOnce(ctx, sess, model, prepared, observe)
		return callErr
	})
	return result, err
}

func (s *Server) codeartsChatOnce(ctx context.Context, sess *codearts.Session, model registry.Model, body []byte, observe func()) ([]byte, error) {
	if policy, ok := s.summaryPolicy(ctx, body, model.ID); ok {
		chunks, err := s.summaryChunks(ctx, sess, model, body, policy)
		if err != nil {
			return nil, err
		}
		return protocol.AggregateSSE(chunks)
	}
	return s.Completions.Complete(ctx, s.CodeArts.Bind(sess), model, body, gateway.Observer{Observe: observe, Usage: captureUsage})
}

// streamOnce 用指定账号发一次流式请求，逐块回调。
func (s *Server) streamOnce(ctx context.Context, acct *accounts.Account, model registry.Model, body []byte, onChunk func([]byte) error) error {
	if mappedPlan(ctx) != nil {
		var err error
		model, body, err = s.prepareMappedAttempt(ctx, acct, model, body)
		if err != nil {
			return err
		}
	}
	if acct.ProviderID() != provider.CodeArts {
		return s.streamProvider(ctx, acct, model, body, onChunk)
	}
	body, err := protocol.RequestStreamUsage(body)
	if err != nil {
		return err
	}
	startAttempt(ctx, acct, model.ID)
	sess, err := s.Cred.Session(ctx, acct, s.resolveOptions())
	if err != nil {
		return err
	}
	return s.runRequestPolicy(ctx, acct, model, body, func(prepared []byte, observe func()) error {
		return s.codeartsStreamOnce(ctx, sess, model, prepared, observe, onChunk)
	})
}

func (s *Server) codeartsStreamOnce(ctx context.Context, sess *codearts.Session, model registry.Model, body []byte, observe func(), onChunk func([]byte) error) error {
	if policy, ok := s.summaryPolicy(ctx, body, model.ID); ok {
		chunks, err := s.summaryChunks(ctx, sess, model, body, policy)
		if err != nil {
			return err
		}
		for _, chunk := range chunks {
			if err := onChunk(chunk); err != nil {
				return err
			}
		}
		return nil
	}
	return s.Completions.Stream(ctx, s.CodeArts.Bind(sess), model.ID, body, gateway.Observer{Observe: observe, Usage: captureUsage}, onChunk)
}

// rewriteModel 把请求体里的 model 换成解析后的上游模型 ID。
//
// 其余字段原样保留，避免丢失 tools / response_format 等上游已支持的能力。
func rewriteModel(body []byte, model string, stream bool) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	mb, err := json.Marshal(model)
	if err != nil {
		return nil, err
	}
	m["model"] = mb
	sb, err := json.Marshal(stream)
	if err != nil {
		return nil, err
	}
	m["stream"] = sb
	return json.Marshal(m)
}

// record 记录一次请求结果。
func (s *Server) record(ctx context.Context, key string, acct *accounts.Account, model string, stream bool, status int, start time.Time, inLen, outLen int, err error) error {
	e := storage.LogEntry{
		UserID:          inferenceUser(ctx),
		AttemptDetails:  requestAttemptDetails(ctx),
		UsageIncomplete: requestUsageIncomplete(ctx),
		RequestID:       requestID(ctx),
		FinishReason:    requestFinish(ctx),
		RequestKind:     requestKind(ctx),
		Attempts:        requestAttempts(ctx),
		SummaryAttempts: summaryAttempts(ctx),
		Usage:           requestUsage(ctx),
		Protocol:        requestProtocol(ctx),
		Time:            time.Now().Unix(),
		Key:             codearts.Mask(key),
		Model:           model,
		Stream:          stream,
		Status:          status,
		LatencyMS:       time.Since(start).Milliseconds(),
		PromptLen:       inLen,
		OutputLen:       outLen,
	}
	if acct != nil {
		e.Account = acct.DisplayName()
		e.Provider = acct.ProviderID()
		e.Scope = acct.Scope
	} else if s.Reg != nil {
		if resolved, ok := s.Reg.Resolve(model, s.Store.Settings().ModelAliases); ok {
			e.Provider, e.Scope = modelProvider(resolved), resolved.Scope
		}
	}
	if err != nil {
		e.Err = err.Error()
		e.ErrorKind = failureKind(ctx, err)
	}
	if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
		e.FirstTokenMS = c.firstTokenMS
		e.RequestedModel = c.requestedModel
		if e.UserID != "" {
			e.PublicModel = model
		}
		if c.selectedModel != "" {
			e.Model = c.selectedModel
		}
	}
	if err := s.Store.Record(e, outLen, key); err != nil {
		log.Printf("保存请求 %s 的用量账本失败: %v", e.RequestID, err)
		return err
	}
	return nil
}

// completionLen 统计非流式响应里的输出字符数。
func completionLen(raw []byte) int {
	var resp struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0
	}
	n := 0
	for _, c := range resp.Choices {
		n += len([]rune(c.Message.Content)) + len([]rune(c.Message.ReasoningContent))
	}
	return n
}

// deltaLen 统计一个流式分片里的输出字符数。
func deltaLen(chunk []byte) int {
	var c struct {
		Choices []struct {
			Delta struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(chunk, &c); err != nil {
		return 0
	}
	n := 0
	for _, ch := range c.Choices {
		n += len([]rune(ch.Delta.Content)) + len([]rune(ch.Delta.ReasoningContent))
	}
	return n
}
