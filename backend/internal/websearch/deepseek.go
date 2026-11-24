package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"codearts2api/internal/provider"
)

const deepSeekSearchURL = "https://api.deepseek.com/v1/tools/search"

// DeepSeek calls the standalone search API with the credential of the selected search service.
func (c *Client) DeepSeek(ctx context.Context, apiKey string, input provider.SearchRequest) (*provider.SearchOutcome, error) {
	if strings.TrimSpace(input.Query) == "" || !utf8.ValidString(input.Query) || utf8.RuneCountInString(input.Query) > 128 {
		return nil, failed(400, "DeepSeek 搜索词须为 1–128 个字符")
	}
	count := input.MaxResults
	if count == 0 {
		count = 5
	}
	if count < 1 || count > 10 {
		return nil, failed(400, "搜索结果数量须为 1–10")
	}
	if apiKey == "" {
		return nil, failed(503, "DeepSeek 搜索密钥未配置")
	}
	body, err := json.Marshal(map[string]any{"query": input.Query, "count": count, "need_content": false})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deepSeekSearchURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &provider.Error{Status: 502, Code: "search_connection_failed", Message: "DeepSeek 搜索连接失败"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		status, code := 502, "search_upstream_error"
		switch resp.StatusCode {
		case 401, 403:
			code = "search_upstream_auth"
		case 402:
			code = "search_upstream_balance"
		case 429:
			status, code = 429, "search_upstream_rate_limit"
		}
		return nil, &provider.Error{Status: status, Code: code, Message: "DeepSeek 搜索服务未能完成请求"}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	var payload struct {
		Results *[]struct{ Title, URL, Snippet, Summary string }
	}
	if err != nil || len(raw) > 1<<20 || json.Unmarshal(raw, &payload) != nil || payload.Results == nil {
		return nil, &provider.Error{Status: 502, Code: "search_invalid_response", Message: "DeepSeek 搜索结果格式无效或超过 1 MiB"}
	}
	results := make([]provider.SearchResult, 0, count)
	seen := make(map[string]bool)
	for _, item := range *payload.Results {
		u, err := url.Parse(item.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || seen[item.URL] {
			continue
		}
		seen[item.URL] = true
		snippet := item.Summary
		if strings.TrimSpace(snippet) == "" {
			snippet = item.Snippet
		}
		results = append(results, provider.SearchResult{Title: item.Title, URL: item.URL, Snippet: snippet})
		if len(results) == count {
			break
		}
	}
	return &provider.SearchOutcome{Query: input.Query, Results: results, TotalResults: len(results)}, nil
}
