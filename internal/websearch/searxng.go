// Package websearch contains external search transports. It never receives
// model-provider credentials and never selects another provider as a fallback.
package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"codearts2api/internal/provider"
)

func failed(status int, message string) error {
	return &provider.Error{Status: status, Message: message}
}

type Client struct{ HTTP *http.Client }

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// endpoint is administrator configuration, not a client-supplied URL. Loopback
// and private addresses are intentional for self-hosted SearXNG installations.
func (c *Client) Search(ctx context.Context, endpoint string, input provider.SearchRequest) (*provider.SearchOutcome, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, failed(503, "SearXNG 搜索服务尚未正确配置")
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/search"
	}
	form := url.Values{"q": {input.Query}, "format": {"json"}, "categories": {"general"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, failed(503, "SearXNG 搜索地址无效")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, failed(502, "SearXNG 搜索连接失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		status := 502
		if resp.StatusCode == 429 {
			status = 429
		}
		return nil, failed(status, fmt.Sprintf("SearXNG 搜索失败（HTTP %d）；请检查服务及 JSON 输出配置", resp.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, failed(502, "SearXNG 搜索响应读取失败或超过 1 MiB")
	}
	var obj struct {
		Results *[]struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &obj) != nil || obj.Results == nil {
		return nil, failed(502, "SearXNG 未返回有效 JSON 搜索结果")
	}
	limit := input.MaxResults
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	results := []provider.SearchResult{}
	seen := map[string]bool{}
	for _, item := range *obj.Results {
		link, err := url.Parse(item.URL)
		if err != nil || (link.Scheme != "http" && link.Scheme != "https") || link.Hostname() == "" || link.User != nil || seen[item.URL] {
			continue
		}
		seen[item.URL] = true
		results = append(results, provider.SearchResult{Title: item.Title, URL: item.URL, Snippet: item.Content})
		if len(results) == limit {
			break
		}
	}
	return &provider.SearchOutcome{Query: input.Query, Results: results, TotalResults: len(results)}, nil
}
