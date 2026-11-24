package teleagent

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

const SearchPath = "/superCowork/sapi/api/v1/coworker/web_search"

// Search uses the desktop search service, not the signed model gateway.
func (c *Client) Search(ctx context.Context, raw json.RawMessage, input provider.SearchRequest) (*provider.SearchOutcome, error) {
	query, limit, err := providerhttp.SearchInput(input)
	if err != nil {
		return nil, err
	}
	a, err := decode(raw)
	if err != nil {
		return nil, invalid("凭据格式无效")
	}
	n, err := nonce()
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	headers.Set("X-Token", a.Token)
	headers.Set("X-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	headers.Set("X-Nonce", n)
	headers.Set("X-App-Version", a.AppVersion)
	headers.Set("X-OS-Type", "linux")
	payload := map[string]any{"messages": []map[string]string{{"role": "user", "content": query}}}
	resp, err := providerhttp.Do(ctx, c.HTTP, "TeleAgent", http.MethodPost, strings.TrimRight(c.Origin, "/")+SearchPath, headers, payload)
	if err != nil {
		return nil, err
	}
	body, err := providerhttp.SearchJSON(resp, "TeleAgent")
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Code *int `json:"code"`
		Data struct {
			References *[]provider.SearchResult `json:"references"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &parsed) != nil || parsed.Code == nil || *parsed.Code != 0 || parsed.Data.References == nil {
		return nil, providerhttp.Broken("TeleAgent")
	}
	return providerhttp.SearchOutcome(query, *parsed.Data.References, limit), nil
}

var _ provider.Searcher = (*Client)(nil)
