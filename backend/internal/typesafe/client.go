// SPDX-License-Identifier: LGPL-3.0-only
// Native endpoint and bounded raw response adapted from Sub2API typesafe/client.go.
package typesafe

import (
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const (
	DefaultBaseURL   = "https://api.typesafe.ai"
	SystemOnePath    = "/v1/systemone"
	MaxRequestBytes  = 4 << 20
	MaxResponseBytes = 4 << 20
)

type Client struct {
	HTTP   *http.Client
	Origin string
}

func New() *Client { return &Client{HTTP: providerhttp.New(), Origin: DefaultBaseURL} }

type credentials struct {
	Key   string `json:"api_key"`
	Scope string `json:"scope"`
	Auth  string `json:"auth_mode"`
	Name  string `json:"name,omitempty"`
}

func (c *Client) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	var a credentials
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &a) != nil || json.Unmarshal(raw, &fields) != nil || fields == nil || a.Key == "" || strings.TrimSpace(a.Key) != a.Key || !providerhttp.Safe(a.Key, a.Name) || (a.Scope != "" && a.Scope != "global") || (a.Auth != "" && a.Auth != "api_key") {
		return nil, provider.Identity{}, providerhttp.Invalid("TypeSafe", "需要 API Key，scope 为 global")
	}
	for _, k := range []string{"base_url", "api_base_urls", "access_token", "refresh_token", "cookie", "password"} {
		if b := fields[k]; len(b) > 0 && string(b) != "null" && string(b) != `""` {
			return nil, provider.Identity{}, providerhttp.Invalid("TypeSafe", "不支持混合凭据或自定义地址")
		}
	}
	a.Scope = "global"
	a.Auth = "api_key"
	if a.Name == "" {
		a.Name = "TypeSafe"
	}
	sum := sha256.Sum256([]byte("typesafe:" + a.Key))
	b, _ := json.Marshal(a)
	return b, provider.Identity{ID: hex.EncodeToString(sum[:16]), Scope: a.Scope, Name: a.Name, NonExpiring: true}, nil
}
func (c *Client) request(ctx context.Context, raw json.RawMessage, method, path string, body any) (*http.Response, error) {
	b, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	var a credentials
	json.Unmarshal(b, &a)
	return providerhttp.Do(ctx, c.HTTP, "TypeSafe", method, c.Origin+path, http.Header{"Authorization": {"Bearer " + a.Key}, "Accept": {"application/json"}}, body)
}

// DecisionModel is deliberately not provider.Model: it is not a chat catalog.
type DecisionModel struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ReleaseDate string `json:"release_date"`
}

func (c *Client) DecisionModels(ctx context.Context, raw json.RawMessage) ([]DecisionModel, error) {
	resp, err := c.request(ctx, raw, "GET", "/v1/models", nil)
	if err != nil {
		return nil, err
	}
	obj, err := providerhttp.JSON(resp, "TypeSafe")
	if err != nil {
		return nil, err
	}
	var models []DecisionModel
	if json.Unmarshal(obj["models"], &models) != nil || models == nil || len(models) > 4096 {
		return nil, providerhttp.Broken("TypeSafe")
	}
	seen := map[string]bool{}
	out := []DecisionModel{}
	for _, m := range models {
		if !providerhttp.ModelID(m.Name) {
			return nil, providerhttp.Broken("TypeSafe")
		}
		if !seen[m.Name] {
			out = append(out, m)
			seen[m.Name] = true
		}
	}
	return out, nil
}
func (c *Client) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	_, id, err := c.Normalize(raw)
	if err != nil {
		return id, err
	}
	models, err := c.DecisionModels(ctx, raw)
	if err != nil {
		return id, err
	}
	if len(models) == 0 {
		return id, providerhttp.Invalid("TypeSafe", "未返回可用决策模型")
	}
	return id, nil
}

type Usage = provider.DecisionUsage
type Result = provider.DecisionResult

// Decide performs one native non-streaming attempt. The caller owns admission,
// account selection, deadlines and settlement; this client owns none of those.
func (c *Client) Decide(ctx context.Context, raw json.RawMessage, body []byte) (*Result, error) {
	if _, err := ValidateSystemOneRequest(body); err != nil {
		return nil, providerhttp.Invalid("TypeSafe", err.Error())
	}
	resp, err := c.request(ctx, raw, "POST", SystemOnePath, json.RawMessage(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, providerhttp.FailureResponse("TypeSafe", resp)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, providerhttp.Broken("TypeSafe")
	}
	if len(b) > MaxResponseBytes {
		return nil, providerhttp.Broken("TypeSafe")
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(b, &envelope) != nil || envelope == nil {
		return nil, providerhttp.Broken("TypeSafe")
	}
	if e := envelope["error"]; len(e) > 0 && string(e) != "null" {
		return nil, providerhttp.Broken("TypeSafe")
	}
	var answers map[string]json.RawMessage
	if json.Unmarshal(envelope["answers"], &answers) != nil || len(answers) == 0 {
		return nil, providerhttp.Broken("TypeSafe")
	}
	var request struct {
		Questions map[string]json.RawMessage `json:"questions"`
	}
	json.Unmarshal(body, &request)
	for id, question := range request.Questions {
		var q, a struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(question, &q) != nil || json.Unmarshal(answers[id], &a) != nil || a.Type != q.Type {
			return nil, providerhttp.Broken("TypeSafe")
		}
	}
	result := &Result{Body: b}
	json.Unmarshal(envelope["model"], &result.Model)
	var usage struct {
		Input  *int64 `json:"input_tokens"`
		Output *int64 `json:"output_tokens"`
	}
	if json.Unmarshal(envelope["usage"], &usage) == nil && usage.Input != nil && usage.Output != nil && *usage.Input >= 0 && *usage.Output >= 0 && *usage.Input <= 1<<40 && *usage.Output <= 1<<40 {
		result.Usage = Usage{Known: true, InputTokens: *usage.Input, OutputTokens: *usage.Output}
	}
	return result, nil
}

func (c *Client) Describe() provider.Descriptor {
	return provider.Descriptor{ID: provider.TypeSafe, Name: "TypeSafe / System One", Scopes: []string{"global"}, Available: true}
}
func (c *Client) Refresh(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	b, _, err := c.Normalize(raw)
	return b, err
}

// No decision model is advertised as a Chat model.
func (c *Client) Models(context.Context, json.RawMessage) ([]provider.Model, error) {
	return []provider.Model{}, nil
}
func (c *Client) Credit(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"kind":"unsupported","available":false,"reason":"TypeSafe 余额查询尚未接入"}`), nil
}
func (c *Client) ValidateDecision(b []byte) (string, error) { return ValidateSystemOneRequest(b) }

var _ provider.Adapter = (*Client)(nil)
var _ provider.Decision = (*Client)(nil)
