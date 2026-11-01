// Search wire contract follows workbuddy2api-panel (MIT; LICENSE.workbuddy).
package workbuddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"codearts2api/internal/provider"
)

func (c *Client) Search(ctx context.Context, credentials json.RawMessage, input provider.SearchRequest) (*provider.SearchOutcome, error) {
	a, err := decode(credentials)
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(input.Query)
	if n := utf8.RuneCountInString(query); n < 2 || n > 4096 {
		return nil, &provider.Error{Status: 400, Message: "搜索词须为 2–4096 个字符"}
	}
	limit := input.MaxResults
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	payload, _ := json.Marshal(struct {
		Query      string `json:"query"`
		Type       string `json:"type"`
		MaxResults int    `json:"max_results"`
	}{query, "text2text", limit})
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base(a.Realm)+"/agenttool/v1/search", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	c.headers(req, a)
	c.conversationHeaders(req, input.SessionID, input.RequestID)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("X-Agent-Intent", "craft")
	req.Header.Set("X-Agent-Runtime", "other")
	for header, value := range map[string]string{"X-Conversation-ID": input.SessionID, "X-Conversation-Request-ID": input.ConversationRequestID, "X-Trace-ID": input.TraceID} {
		if value != "" {
			req.Header.Set(header, value)
		}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("WorkBuddy 搜索连接失败：%T", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("WorkBuddy 搜索响应读取失败")
	}
	if len(raw) > 1<<20 {
		return nil, fmt.Errorf("WorkBuddy 搜索响应超过 1 MiB")
	}
	if err = upstreamError(resp.StatusCode, raw); err != nil {
		return nil, err
	}
	var parsed struct {
		Query        string                   `json:"query"`
		Results      *[]provider.SearchResult `json:"results"`
		TotalResults int                      `json:"total_results"`
	}
	if json.Unmarshal(raw, &parsed) != nil || parsed.Results == nil {
		return nil, fmt.Errorf("WorkBuddy 搜索响应格式无效")
	}
	results := []provider.SearchResult{}
	for _, result := range *parsed.Results {
		u, e := url.Parse(result.URL)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			continue
		}
		results = append(results, result)
		if len(results) >= limit {
			break
		}
	}
	return &provider.SearchOutcome{Query: query, Results: results, TotalResults: len(results)}, nil
}
