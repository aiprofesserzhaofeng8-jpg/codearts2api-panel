package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"codearts2api/internal/catalog/modelmeta"
)

// FetchPublicModelReferences downloads the same public registry as Sub2API.
// Fixed HTTPS endpoint, no query/model IDs, cookies, gateway keys or credentials.
// No redirects; the optional result is only used during directory refresh.
func FetchPublicModelReferences(ctx context.Context) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return fetchPublicModelReferences(ctx, client)
}

func fetchPublicModelReferences(ctx context.Context, client *http.Client) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelmeta.RegistryURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "codearts2api-panel")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("public model registry returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, modelmeta.BodyLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > modelmeta.BodyLimit {
		return nil, fmt.Errorf("public model registry exceeds size limit")
	}
	return body, nil
}
