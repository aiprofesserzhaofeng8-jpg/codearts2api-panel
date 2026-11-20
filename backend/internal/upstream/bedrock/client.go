package bedrock

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

type Client struct{ HTTP *http.Client }

func New() *Client { return &Client{HTTP: providerhttp.New()} }
func (c *Client) Describe() provider.Descriptor {
	return provider.Descriptor{ID: provider.Bedrock, Name: "AWS Bedrock / Claude", Available: true, Scopes: []string{"us-east-1", "us-west-2", "eu-west-1", "ap-northeast-1"}}
}

type credentials struct {
	Key     string   `json:"aws_access_key_id"`
	Secret  string   `json:"aws_secret_access_key"`
	Token   string   `json:"aws_session_token,omitempty"`
	Region  string   `json:"aws_region"`
	Scope   string   `json:"scope"`
	Expires int64    `json:"expires_at,omitempty"`
	Name    string   `json:"name,omitempty"`
	Models  []string `json:"model_ids,omitempty"`
}

var modelPattern = regexp.MustCompile(`^(?:(?:us|eu|apac|jp|au|global)\.)?anthropic\.claude-[A-Za-z0-9._:-]+$`)

func supportedModel(id string) bool {
	if !modelPattern.MatchString(id) {
		return false
	}
	return !strings.Contains(id, "anthropic.claude-v") && !strings.Contains(id, "anthropic.claude-instant")
}

func (c *Client) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	var a credentials
	var fields map[string]json.RawMessage
	bad := func() (json.RawMessage, provider.Identity, error) {
		return nil, provider.Identity{}, providerhttp.Invalid("Bedrock", "需要 AWS 凭据、商业区域；临时令牌须提供 expires_at")
	}
	if json.Unmarshal(raw, &a) != nil || json.Unmarshal(raw, &fields) != nil || a.Key == "" || a.Secret == "" || !providerhttp.Safe(a.Key, a.Secret, a.Token, a.Name) || strings.TrimSpace(a.Key) != a.Key || strings.TrimSpace(a.Secret) != a.Secret {
		return bad()
	}
	for _, k := range []string{"base_url", "api_key", "refresh_token", "role_arn", "auth_mode"} {
		if b := fields[k]; len(b) > 0 && string(b) != "null" && string(b) != `""` {
			return bad()
		}
	}
	if a.Region == "" {
		a.Region = a.Scope
	}
	if a.Region == "" {
		a.Region = "us-east-1"
	}
	if !regionPattern.MatchString(a.Region) || a.Scope != "" && a.Scope != a.Region || (a.Token != "" && a.Expires <= 0) || (a.Token == "" && a.Expires != 0) {
		return bad()
	}
	if len(a.Models) > 128 {
		return bad()
	}
	seen := map[string]bool{}
	models := []string{}
	for _, m := range a.Models {
		if !supportedModel(m) || len(m) > 256 {
			return bad()
		}
		if !seen[m] {
			seen[m] = true
			models = append(models, m)
		}
	}
	a.Models = models
	a.Scope = a.Region
	if a.Name == "" {
		a.Name = "Bedrock / " + a.Region
	}
	sum := sha256.Sum256([]byte("bedrock:" + a.Region + ":" + a.Key))
	b, _ := json.Marshal(a)
	return b, provider.Identity{ID: hex.EncodeToString(sum[:16]), Name: a.Name, Scope: a.Region, ExpiresAt: a.Expires, NonExpiring: a.Token == ""}, nil
}
func (c *Client) Refresh(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	b, id, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	if !id.NonExpiring {
		return nil, providerhttp.Invalid("Bedrock", "临时凭据不能自动续期，请导入新的 AWS 临时凭据")
	}
	return b, nil
}
func (c *Client) credential(raw json.RawMessage) (credentials, error) {
	var a credentials
	b, _, err := c.Normalize(raw)
	if err != nil {
		return a, err
	}
	json.Unmarshal(b, &a)
	if a.Expires > 0 && a.Expires <= time.Now().Unix() {
		return a, &provider.Error{Status: 401, AccountFault: true, Message: "Bedrock 临时凭据已过期"}
	}
	return a, nil
}
func (c *Client) request(ctx context.Context, a credentials, method, target string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, providerhttp.Invalid("Bedrock", "请求地址无效")
	}
	req.Header.Set("Accept", "application/json")
	if method == "POST" {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/vnd.amazon.eventstream")
	}
	if err = NewBedrockSigner(a.Key, a.Secret, a.Token, a.Region).SignRequest(ctx, req, body); err != nil {
		return nil, providerhttp.Invalid("Bedrock", "AWS 签名失败")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, provider.NewTransportError("Bedrock", err)
	}
	if resp.StatusCode != 200 {
		defer resp.Body.Close()
		return nil, bedrockHTTPError(resp)
	}
	return resp, nil
}
func bedrockHTTPError(resp *http.Response) error {
	e := providerhttp.FailureResponse("Bedrock", resp).(*provider.Error)
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	var obj map[string]json.RawMessage
	json.Unmarshal(b, &obj)
	code := providerhttp.Field(obj, "__type", "code")
	if code == "" {
		code = resp.Header.Get("x-amzn-errortype")
	}
	if i := strings.LastIndex(code, "#"); i >= 0 {
		code = code[i+1:]
	}
	code = strings.Split(code, ":")[0]
	switch code {
	case "ExpiredTokenException", "UnrecognizedClientException", "InvalidSignatureException":
		e.Status = 401
		e.AccountFault = true
		e.Code = code
		e.Message = "Bedrock AWS 凭据失效或签名被拒绝"
	case "ThrottlingException":
		e.Status = 429
		e.Code = code
	case "AccessDeniedException", "ValidationException", "ResourceNotFoundException":
		e.Code = code
		e.AccountFault = false
	}
	return e
}
func (c *Client) Credit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	if _, err := c.credential(raw); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"kind":"unsupported","available":false,"reason":"AWS 账单余额未接入"}`), nil
}
func (c *Client) Stream(ctx context.Context, raw json.RawMessage, r provider.Request, emit func([]byte) error) error {
	a, err := c.credential(raw)
	if err != nil {
		return err
	}
	if !supportedModel(r.Model) || len(a.Models) > 0 && !slices.Contains(a.Models, r.Model) {
		return providerhttp.Invalid("Bedrock", "未配置或不支持的 Claude 型号")
	}
	target, err := InvokeURL(a.Region, r.Model)
	if err != nil {
		return providerhttp.Invalid("Bedrock", "区域或模型无效")
	}
	body, err := Encode(r)
	if err != nil {
		return err
	}
	resp, err := c.request(ctx, a, "POST", target, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	typ, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if typ != "application/vnd.amazon.eventstream" {
		return providerhttp.Broken("Bedrock")
	}
	err = Decode(ctx, resp.Body, r.Model, emit)
	var pe *provider.Error
	if err != nil && !errors.As(err, &pe) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return providerhttp.Broken("Bedrock")
	}
	return err
}
func (c *Client) Models(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	a, err := c.credential(raw)
	if err != nil {
		return nil, err
	}
	out := []provider.Model{}
	seen := map[string]bool{}
	add := func(id, name string, images bool) {
		if seen[id] || !supportedModel(id) {
			return
		}
		seen[id] = true
		m := provider.Model{ID: id, UpstreamID: id, Name: name, Provider: provider.Bedrock, Scope: a.Region, StreamOnly: true, Source: "AWS Bedrock"}
		if images {
			m.ImageInput = "supported"
		}
		out = append(out, m)
	}
	if len(a.Models) > 0 {
		for _, id := range a.Models {
			add(id, id, false)
		}
		for i := range out {
			out[i].CatalogKind = "configured"
		}
		return out, nil
	}
	base := "https://bedrock." + a.Region + ".amazonaws.com"
	resp, err := c.request(ctx, a, "GET", base+"/foundation-models?byProvider=Anthropic&byOutputModality=TEXT&byInferenceType=ON_DEMAND", nil)
	if err != nil {
		return nil, err
	}
	obj, err := providerhttp.JSON(resp, "Bedrock")
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID     string   `json:"modelId"`
		Name   string   `json:"modelName"`
		Stream bool     `json:"responseStreamingSupported"`
		Input  []string `json:"inputModalities"`
	}
	if json.Unmarshal(obj["modelSummaries"], &rows) != nil || rows == nil || len(rows) > 4096 {
		return nil, providerhttp.Broken("Bedrock")
	}
	for _, row := range rows {
		if row.Stream {
			add(row.ID, row.Name, slices.Contains(row.Input, "IMAGE"))
		}
	}
	token := ""
	tokens := map[string]bool{}
	for page := 0; page < 32; page++ {
		q := url.Values{"maxResults": {"1000"}, "type": {"SYSTEM_DEFINED"}}
		if token != "" {
			q.Set("nextToken", token)
		}
		resp, err = c.request(ctx, a, "GET", base+"/inference-profiles?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		obj, err = providerhttp.JSON(resp, "Bedrock")
		if err != nil {
			return nil, err
		}
		var profiles []struct {
			ID     string `json:"inferenceProfileId"`
			Name   string `json:"inferenceProfileName"`
			Status string `json:"status"`
		}
		if json.Unmarshal(obj["inferenceProfileSummaries"], &profiles) != nil || profiles == nil || len(profiles) > 1000 {
			return nil, providerhttp.Broken("Bedrock")
		}
		for _, p := range profiles {
			if p.Status == "ACTIVE" {
				add(p.ID, p.Name, false)
			}
		}
		token = providerhttp.Field(obj, "nextToken")
		if token == "" {
			return out, nil
		}
		if len(token) > 2048 || tokens[token] {
			return nil, providerhttp.Broken("Bedrock")
		}
		tokens[token] = true
	}
	return nil, providerhttp.Broken("Bedrock")
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
	if len(models) == 0 {
		return id, providerhttp.Invalid("Bedrock", "没有可用 Claude 型号，可填写 model_ids 指定已授权型号")
	}
	// Prefer a returned system profile; do not invent a region prefix or retry a paid request.
	model := models[0].UpstreamID
	for _, m := range models {
		if !strings.HasPrefix(m.UpstreamID, "anthropic.") {
			model = m.UpstreamID
			break
		}
	}
	err = c.Stream(ctx, raw, provider.Request{Model: model, Body: []byte(`{"messages":[{"role":"user","content":"Reply OK"}],"max_tokens":64}`)}, func([]byte) error { return nil })
	return id, err
}

var _ provider.Adapter = (*Client)(nil)
var _ provider.Streaming = (*Client)(nil)
