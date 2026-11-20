// Package gemini connects Google AI Studio keys through Google's official
// OpenAI-compatible endpoint and the separate Code Assist OAuth transport.
package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"

	"codearts2api/internal/cnapi"
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

type Client struct {
	clientSecret string
	*cnapi.Client
	ModelsURL                      string
	CLIBase, TokenURL, UserInfoURL string
	Login                          *providerhttp.PKCE
}

func New() *Client {
	c := cnapi.New("gemini", "Gemini", "https://generativelanguage.googleapis.com/v1beta/openai", "", nil)
	c.Scope = "api"
	return &Client{clientSecret: os.Getenv(GeminiCLIOAuthClientSecretEnv), Client: c, ModelsURL: "https://generativelanguage.googleapis.com/v1beta/models", CLIBase: GeminiCliBaseURL, TokenURL: TokenURL, UserInfoURL: "https://www.googleapis.com/oauth2/v2/userinfo", Login: &providerhttp.PKCE{Config: providerhttp.OAuthConfig{Name: "Gemini", Authorize: AuthorizeURL, Redirect: GeminiCLIRedirectURI, ClientID: os.Getenv(GeminiCLIOAuthClientIDEnv), Scopes: DefaultCodeAssistScopes, AccountScope: "cli", Extra: url.Values{"access_type": {"offline"}, "prompt": {"consent"}, "include_granted_scopes": {"true"}}}}}
}

// Models uses the native metadata, not hardcoded limits or the CLI fallback
// catalog. Model listing proves key access only, not a successful generation.
func (c *Client) Models(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	if isCLI(raw) {
		return c.cliModels(ctx, raw)
	}
	normalized, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	var credentials struct {
		Key string `json:"api_key"`
	}
	_ = json.Unmarshal(normalized, &credentials)
	out := []provider.Model{}
	seen, pages := map[string]bool{}, map[string]bool{}
	token := ""
	for page := 0; page < 20; page++ {
		u, err := url.Parse(c.ModelsURL)
		if err != nil {
			return nil, providerhttp.Broken("Gemini")
		}
		q := u.Query()
		q.Set("pageSize", "1000")
		if token != "" {
			q.Set("pageToken", token)
		}
		u.RawQuery = q.Encode()
		resp, err := providerhttp.Do(ctx, c.HTTP, "Gemini", http.MethodGet, u.String(), http.Header{"x-goog-api-key": {credentials.Key}}, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return nil, providerhttp.FailureResponse("Gemini", resp)
		}
		obj, err := providerhttp.JSON(resp, "Gemini")
		if err != nil {
			return nil, err
		}
		var rows []struct {
			Name        string   `json:"name"`
			Display     string   `json:"displayName"`
			Description string   `json:"description"`
			Input       int      `json:"inputTokenLimit"`
			Output      int      `json:"outputTokenLimit"`
			Methods     []string `json:"supportedGenerationMethods"`
		}
		if json.Unmarshal(obj["models"], &rows) != nil || rows == nil {
			return nil, providerhttp.Broken("Gemini")
		}
		for _, r := range rows {
			if !slices.Contains(r.Methods, "generateContent") {
				continue
			}
			id, ok := strings.CutPrefix(r.Name, "models/")
			if !ok || !providerhttp.ModelID(id) || r.Input < 0 || r.Output < 0 {
				return nil, providerhttp.Broken("Gemini")
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			// The compatibility adapter currently returns text, not generated media.
			// Do not advertise specialized image, audio or live generation endpoints.
			if strings.Contains(id, "image") || strings.Contains(id, "tts") || strings.Contains(id, "audio") || strings.Contains(id, "live") {
				continue
			}
			name := r.Display
			if name == "" {
				name = id
			}
			m := provider.Model{ID: id, UpstreamID: id, Name: name, Desc: r.Description, Provider: "gemini", Scope: "api", Context: r.Input, MaxOutput: r.Output, StreamOnly: true, Source: "https://ai.google.dev/api/models"}
			// Explicit verified documentation entries; unknown future IDs retain unknown
			// capabilities. Catalog discovery itself is not a capability probe.
			switch id {
			case "gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite":
				m.ImageInput = "supported"
				m.ToolCalling = "supported"
			case "gemini-3-pro-preview", "gemini-3-flash-preview":
				m.ImageInput = "supported"
				// Gateway bridges retain signed tool metadata; client round-trip
				// support is still unverified (local Pi drops extra_content).
			}
			out = append(out, m)
		}
		token = ""
		if b, ok := obj["nextPageToken"]; ok && json.Unmarshal(b, &token) != nil {
			return nil, providerhttp.Broken("Gemini")
		}
		if token == "" {
			return out, nil
		}
		if len(token) > 8192 || pages[token] {
			return nil, providerhttp.Broken("Gemini")
		}
		pages[token] = true
	}
	return nil, providerhttp.Broken("Gemini")
}

func (c *Client) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	if isCLI(raw) {
		return c.verifyCLI(ctx, raw)
	}
	_, id, err := c.Normalize(raw)
	if err != nil {
		return id, err
	}
	models, err := c.Models(ctx, raw)
	if err != nil {
		return id, err
	}
	if len(models) == 0 {
		return id, providerhttp.Invalid("Gemini", "没有返回可用的对话模型")
	}
	return id, nil
}
