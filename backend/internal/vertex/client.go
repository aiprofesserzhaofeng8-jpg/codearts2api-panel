package vertex

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"slices"
	"time"

	"codearts2api/internal/googlewire"
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

type Client struct{ *Auth }

func New() *Client { return &Client{Auth: NewAuth()} }
func (c *Client) Describe() provider.Descriptor {
	return provider.Descriptor{ID: provider.Vertex, Name: "Vertex AI / Gemini", Available: true, Scopes: []string{"global", "us-central1", "europe-west4"}}
}
func (c *Client) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	b, id, err := c.Auth.Normalize(raw)
	if err != nil {
		return nil, id, err
	}
	var a credentials
	_ = json.Unmarshal(b, &a)
	if len(a.Models) == 0 || len(a.Models) > 128 {
		return nil, provider.Identity{}, providerhttp.Invalid("Vertex AI", "请在 model_ids 填写已授权的 Gemini 型号")
	}
	seen := map[string]bool{}
	models := []string{}
	for _, m := range a.Models {
		if _, err := GeminiURL(a.Project, a.Location, m); err != nil {
			return nil, provider.Identity{}, err
		}
		if !seen[m] {
			models = append(models, m)
			seen[m] = true
		}
	}
	a.Models = models
	b, err = json.Marshal(a)
	return b, id, err
}
func (c *Client) PrepareImport(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	b, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	// Never trust an imported bearer token to establish the service-account
	// identity: verify a new token obtained using the supplied private key.
	return c.Refresh(ctx, b)
}
func (c *Client) credential(raw json.RawMessage) (credentials, error) {
	var a credentials
	b, id, err := c.Normalize(raw)
	if err != nil {
		return a, err
	}
	_ = json.Unmarshal(b, &a)
	if a.Access == "" || id.ExpiresAt <= time.Now().Unix() {
		return a, &provider.Error{Status: 401, AccountFault: true, Message: "Vertex AI 访问令牌已过期，需要续期"}
	}
	return a, nil
}
func (c *Client) Models(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	a, err := c.credential(raw)
	if err != nil {
		return nil, err
	}
	out := []provider.Model{}
	for _, m := range a.Models {
		out = append(out, provider.Model{ID: m, Name: m, Provider: provider.Vertex, Scope: a.Location, UpstreamID: m, StreamOnly: true, Desc: "管理员配置的 Vertex AI 型号；未验证全部型号授权"})
	}
	return out, nil
}
func (c *Client) Credit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	if _, err := c.credential(raw); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"kind":"unsupported","available":false,"reason":"Google Cloud 账单余额未接入"}`), nil
}
func (c *Client) Stream(ctx context.Context, raw json.RawMessage, r provider.Request, emit func([]byte) error) error {
	a, err := c.credential(raw)
	if err != nil {
		return err
	}
	if !slices.Contains(a.Models, r.Model) {
		return providerhttp.Invalid("Vertex AI", "未配置的 Gemini 型号")
	}
	target, err := GeminiURL(a.Project, a.Location, r.Model)
	if err != nil {
		return err
	}
	body, err := googlewire.Encode(r.Body, r.Model)
	if err != nil {
		return err
	}
	resp, err := providerhttp.Do(ctx, c.HTTP, "Vertex AI", http.MethodPost, target, http.Header{"Authorization": {"Bearer " + a.Access}, "Accept": {"text/event-stream"}}, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return providerhttp.FailureResponse("Vertex AI", resp)
	}
	typ, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if typ != "text/event-stream" {
		return providerhttp.Broken("Vertex AI")
	}
	return googlewire.Decode(ctx, resp.Body, "Vertex AI", r.Model, emit)
}
func (c *Client) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	_, id, err := c.Normalize(raw)
	if err != nil {
		return id, err
	}
	models, err := c.Models(ctx, raw)
	if err != nil {
		return id, err
	}
	err = c.Stream(ctx, raw, provider.Request{Model: models[0].ID, Body: json.RawMessage(`{"messages":[{"role":"user","content":"Reply OK"}],"max_tokens":64}`)}, func([]byte) error { return nil })
	return id, err
}

var _ provider.Adapter = (*Client)(nil)
var _ provider.Streaming = (*Client)(nil)
