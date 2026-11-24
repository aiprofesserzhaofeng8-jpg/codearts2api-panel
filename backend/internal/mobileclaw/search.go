package mobileclaw

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/url"
	"strconv"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

const SearchPath = "/desktop/search/v2/mwebsearch"

func (c *Client) Search(ctx context.Context, raw json.RawMessage, input provider.SearchRequest) (*provider.SearchOutcome, error) {
	query, limit, err := providerhttp.SearchInput(input)
	if err != nil {
		return nil, err
	}
	sid := input.SessionID
	if sid == "" {
		sid = input.RequestID
	}
	// The native service accepts a request identifier as its search session.
	if sid == "" {
		sid = rand.Text()
	}
	params := url.Values{"q": {query}, "ref_prom": {"aiso-sr"}, "sid": {sid}, "count": {strconv.Itoa(limit)}}
	resp, err := c.request(ctx, raw, SearchPath+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	body, err := providerhttp.SearchJSON(resp, "MobileClaw")
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Errno *int `json:"errno"`
		Items *[]struct {
			Title     string `json:"title"`
			URL       string `json:"url"`
			SummaryAI string `json:"summary_ai"`
			Summary   string `json:"summary"`
		} `json:"items"`
	}
	if json.Unmarshal(body, &parsed) != nil || parsed.Errno == nil || *parsed.Errno != 0 || parsed.Items == nil {
		return nil, providerhttp.Broken("MobileClaw")
	}
	results := make([]provider.SearchResult, 0, len(*parsed.Items))
	for _, item := range *parsed.Items {
		snippet := item.SummaryAI
		if snippet == "" {
			snippet = item.Summary
		}
		results = append(results, provider.SearchResult{Title: item.Title, URL: item.URL, Snippet: snippet})
	}
	return providerhttp.SearchOutcome(query, results, limit), nil
}

var _ provider.Searcher = (*Client)(nil)
