package providerhttp

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"codearts2api/internal/provider"
)

func SearchInput(input provider.SearchRequest) (string, int, error) {
	query := strings.TrimSpace(input.Query)
	if n := utf8.RuneCountInString(query); n < 2 || n > 4096 || input.MaxResults < 0 {
		return "", 0, Invalid("搜索", "搜索词须为 2–4096 个字符，max_results 不能为负数")
	}
	limit := input.MaxResults
	if limit == 0 {
		limit = 5
	}
	return query, min(limit, 20), nil
}

func SearchJSON(resp *http.Response, name string) ([]byte, error) {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, FailureResponse(name, resp)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, provider.NewTransportError(name, err)
	}
	if len(body) > 1<<20 {
		return nil, Broken(name)
	}
	return body, nil
}

func SearchOutcome(query string, results []provider.SearchResult, limit int) *provider.SearchOutcome {
	out := &provider.SearchOutcome{Query: query, Results: []provider.SearchResult{}}
	for _, result := range results {
		u, err := url.Parse(result.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			continue
		}
		out.Results = append(out.Results, result)
		if len(out.Results) >= limit {
			break
		}
	}
	out.TotalResults = len(out.Results)
	return out
}
