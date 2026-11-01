// Search Messages protocol adapted from workbuddy2api-panel (MIT; LICENSE.workbuddy).
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"codearts2api/internal/apierr"
	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
)

const searchQueryPrefix = "Perform a web search for the query:"

type searchProviderKey struct{}

func (s *Server) searchForProvider(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.handleSearchMessages(w, r.WithContext(context.WithValue(r.Context(), searchProviderKey{}, name)))
	}
}

type searchMessagesRequest struct {
	Model          string `json:"model"`
	Stream         bool   `json:"stream"`
	MaxResults     int    `json:"max_results"`
	ConversationID string `json:"conversation_id"`
	Tools          []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"tools"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
}

func searchQuery(req searchMessagesRequest) string {
	query := ""
	for _, m := range req.Messages {
		if m.Role != "user" {
			continue
		}
		var plain string
		if json.Unmarshal(m.Content, &plain) != nil {
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(m.Content, &blocks) != nil {
				continue
			}
			parts := []string{}
			for _, b := range blocks {
				if b.Type == "text" {
					parts = append(parts, b.Text)
				}
			}
			plain = strings.Join(parts, "\n")
		}
		if strings.TrimSpace(plain) != "" {
			query = strings.TrimSpace(plain)
		}
	}
	if len(query) >= len(searchQueryPrefix) && strings.EqualFold(query[:len(searchQueryPrefix)], searchQueryPrefix) {
		query = strings.TrimSpace(query[len(searchQueryPrefix):])
	}
	return query
}

// Virtual capabilities are namespaced separately from the conversation catalog.
// CodeArts has no region segment in its legacy model IDs.
func providerSearchModel(name, scope string) registry.Model {
	id := provider.ModelID(name, scope, "web-search")
	if name == provider.CodeArts {
		id = "codearts/web-search"
		scope = ""
	}
	return registry.Model{ID: id, Provider: name, Scope: scope, Name: name + " 联网搜索"}
}

func resolveSearchTarget(settings store.Settings, requested string) (registry.Model, store.ProviderSettings, error) {
	name, scope := provider.WorkBuddy, settings.WorkBuddy.SearchScope
	if strings.Contains(requested, "/") {
		var id string
		name, scope, id = provider.ParseModel(requested)
		switch name {
		case provider.CodeArts:
			if id != "web-search" {
				return registry.Model{}, store.ProviderSettings{}, fmt.Errorf("CodeArts 搜索标识须为 codearts/web-search")
			}
		case provider.Trae:
			if scope != "cn" || id != "web-search" {
				return registry.Model{}, store.ProviderSettings{}, fmt.Errorf("Trae 搜索标识须为 trae/cn/web-search")
			}
		case provider.WorkBuddy:
			if (scope != "cn" && scope != "global") || id == "" {
				return registry.Model{}, store.ProviderSettings{}, fmt.Errorf("WorkBuddy 搜索地区无效")
			}
		default:
			return registry.Model{}, store.ProviderSettings{}, fmt.Errorf("未知搜索平台，不会回落到其他平台")
		}
	}
	return providerSearchModel(name, scope), settings.ProviderPolicy(name), nil
}

func (s *Server) handleSearchMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAnthropicErr(w, 405, "invalid_request_error", "仅支持 POST")
		return
	}
	if err := s.Store.CheckSearchBudget(extractKey(r)); err != nil {
		writeAnthropicErr(w, 402, "permission_error", err.Error())
		return
	}
	settings := s.Store.Settings()
	if !settings.AllowCreditModels {
		writeAnthropicErr(w, 403, "permission_error", "上游计费调用已关闭")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeAnthropicErr(w, 413, "invalid_request_error", "搜索请求体超过限制或读取失败")
		return
	}
	var req searchMessagesRequest
	if json.Unmarshal(body, &req) != nil {
		writeAnthropicErr(w, 400, "invalid_request_error", "搜索请求格式无效")
		return
	}
	declared := false
	for _, t := range req.Tools {
		if t.Name == "web_search" || strings.HasPrefix(t.Type, "web_search") {
			declared = true
		}
	}
	query := searchQuery(req)
	if !declared || utf8.RuneCountInString(query) < 2 || utf8.RuneCountInString(query) > 4096 || req.MaxResults < 0 {
		writeAnthropicErr(w, 400, "invalid_request_error", "请声明 web_search 工具，并提供 2–4096 字符的 user 搜索词；max_results 不能为负数")
		return
	}
	if req.Stream {
		writeAnthropicErr(w, 400, "invalid_request_error", "搜索接口返回完整 Messages JSON，请设置 stream:false")
		return
	}
	target := req.Model
	if bound, _ := r.Context().Value(searchProviderKey{}).(string); bound != "" {
		if strings.Contains(target, "/") {
			name, _, _ := provider.ParseModel(target)
			if name != bound {
				writeAnthropicErr(w, 400, "invalid_request_error", "搜索入口与 model 所属平台不一致")
				return
			}
		} else {
			target = providerSearchModel(bound, settings.ProviderPolicy(bound).SearchScope).ID
		}
	}
	model, policy, err := resolveSearchTarget(settings, target)
	if err != nil {
		writeAnthropicErr(w, 400, "invalid_request_error", err.Error())
		return
	}
	if !policy.SearchEnabled {
		writeAnthropicErr(w, 403, "permission_error", model.Provider+" 联网搜索已关闭，请在对应平台配置中设置")
		return
	}
	if err := policy.ValidateFor(model.Provider); err != nil {
		writeAnthropicErr(w, 503, "api_error", err.Error())
		return
	}
	r = beginInference(w, r, "search")
	if capture, ok := r.Context().Value(usageContextKey{}).(*usageCapture); ok {
		capture.kind = "search"
	}
	conv := strings.TrimSpace(r.Header.Get("X-Conversation-ID"))
	if conv == "" {
		conv = req.ConversationID
	}
	input := provider.SearchRequest{Query: query, MaxResults: req.MaxResults, SessionID: conv, RequestID: requestID(r.Context()), ConversationRequestID: strings.TrimSpace(r.Header.Get("X-Conversation-Request-ID")), TraceID: strings.TrimSpace(r.Header.Get("X-Trace-ID"))}
	for _, value := range []string{input.SessionID, input.ConversationRequestID, input.TraceID} {
		if len(value) > 256 || strings.ContainsAny(value, "\r\n") {
			writeAnthropicErr(w, 400, "invalid_request_error", "搜索会话标识无效")
			return
		}
	}
	routing, _ := json.Marshal(map[string]any{"model": model.ID, "conversation_id": conv, "messages": req.Messages})
	ctx := withRoutingModel(withRoutingSession(r, routing), model.ID)
	key, start := extractKey(r), time.Now()
	timeout := 20 * time.Second
	if settings.RequestTimeout > 0 {
		timeout = min(timeout, time.Duration(settings.RequestTimeout)*time.Second)
	}
	var outcome *provider.SearchOutcome
	call := func(ctx context.Context, a *codearts.Account) error {
		startAttempt(ctx, a, model.ID)
		if policy.SearchBackend == "searxng" {
			// Independent backend: never refresh or forward model credentials.
			outcome, err = s.Search.Search(ctx, policy.SearchURL, input)
			return err
		}
		adapter, raw, err := s.providerCredentials(ctx, a)
		if err != nil {
			return err
		}
		searcher, ok := adapter.(provider.Searcher)
		if !ok {
			return &provider.Error{Status: 400, Message: "该上游未接入联网搜索"}
		}
		outcome, err = searcher.Search(ctx, raw, input)
		if err == nil && outcome == nil {
			return fmt.Errorf("上游搜索未返回结果")
		}
		return err
	}
	var acct *codearts.Account
	if policy.SearchBackend == "searxng" {
		acct, err = s.withExternalSearchAccount(ctx, timeout, model.ID, call)
	} else {
		acct, err = s.withAccountRetry(ctx, timeout, call)
	}
	if err != nil {
		e := apierr.From(err)
		s.record(ctx, key, acct, model.ID, false, e.Status, start, len([]rune(query)), 0, err)
		writeAnthropicErr(w, e.Status, e.Anthropic, err.Error())
		return
	}
	// Search returns no measured token/cost data. Keep stored usage unknown;
	// protocol usage zeros below are compatibility fields, not a free-cost claim.
	s.record(ctx, key, acct, model.ID, false, 200, start, len([]rune(query)), 0, nil)
	writeJSON(w, 200, searchMessagesResponse(req.Model, outcome))
}

// An external search shares group authorization and concurrency leases, but its
// success/failure says nothing about model credentials or model account health.
func (s *Server) withExternalSearchAccount(ctx context.Context, timeout time.Duration, model string, call func(context.Context, *codearts.Account) error) (*codearts.Account, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := s.checkRequestBudget(ctx, model); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	session, _ := ctx.Value(routingSessionKey{}).(string)
	acct, release, err := s.Store.AcquireAccountForKey(session, model, nil, groupKey(ctx))
	if err != nil {
		return nil, groupDenied()
	}
	if acct == nil {
		return nil, apierr.NoAccount
	}
	defer release()
	return acct, call(ctx, acct)
}
func searchMessagesResponse(model string, out *provider.SearchOutcome) map[string]any {
	if model == "" {
		model = "web-search"
	}
	items, citations := []any{}, []any{}
	for _, r := range out.Results {
		title := r.Title
		if title == "" {
			title = r.URL
		}
		items = append(items, map[string]any{"type": "web_search_result", "url": r.URL, "title": title})
		if r.Snippet != "" {
			citations = append(citations, map[string]any{"type": "web_search_result_location", "url": r.URL, "cited_text": r.Snippet})
		}
	}
	id := codearts.RandomHex(16)
	content := []any{map[string]any{"type": "web_search_tool_result", "tool_use_id": "srvtoolu_" + id, "content": items}}
	if len(citations) > 0 {
		content = append(content, map[string]any{"type": "text", "text": "", "citations": citations})
	}
	return map[string]any{"id": "msg_search_" + id, "type": "message", "role": "assistant", "model": model, "content": content, "stop_reason": "end_turn", "stop_sequence": nil, "usage": map[string]int{"input_tokens": 0, "output_tokens": 0}}
}
