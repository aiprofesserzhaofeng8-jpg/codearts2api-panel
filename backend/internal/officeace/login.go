package officeace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

type loginSession struct {
	ID           string
	State        string
	CodeVerifier string
	PrivJWK      json.RawMessage
	PubJWK       json.RawMessage
	AuthURL      string
	Created      time.Time
	Expires      time.Time
}

type ModelInfo struct {
	AccountID   string
	PrincipalID string
	AppKey      string
	AppSecret   string
	BaseURL     string
	// Subscribed records whether the permission answer carried a subscription
	// block at all. An account answered with `"subscription":null` has never
	// redeemed an invitation, which is an account state, not a fetch failure.
	Subscribed bool
}

// BeginLogin starts a browser OAuth flow with cloud state, PKCE, DPoP, and authorization URL.
func (c *Client) BeginLogin(ctx context.Context, scope string) (provider.Login, error) {
	if err := ctx.Err(); err != nil {
		return provider.Login{}, err
	}
	if scope != "cn" && scope != "" {
		return provider.Login{}, invalid("OfficeAce 地区须为 cn")
	}

	state, err := c.fetchState(ctx)
	if err != nil {
		return provider.Login{}, fmt.Errorf("获取云端 state 失败：%w", err)
	}

	verifier, challenge, err := newPKCEPair()
	if err != nil {
		return provider.Login{}, fmt.Errorf("生成 PKCE 密钥对失败：%w", err)
	}

	privJWK, pubJWK, err := newDPoPKeyPair()
	if err != nil {
		return provider.Login{}, fmt.Errorf("生成 DPoP 密钥对失败：%w", err)
	}

	authURL := c.buildAuthorizeURL(state, challenge)

	c.loginMu.Lock()
	defer c.loginMu.Unlock()

	// Prune expired sessions
	now := time.Now()
	for id, s := range c.logins {
		if now.After(s.Expires) {
			delete(c.logins, id)
		}
	}
	if len(c.logins) >= 128 {
		return provider.Login{}, invalid("未完成的授权过多，请稍后再试")
	}

	s := &loginSession{
		ID:           state,
		State:        state,
		CodeVerifier: verifier,
		PrivJWK:      privJWK,
		PubJWK:       pubJWK,
		AuthURL:      authURL,
		Created:      now,
		Expires:      now.Add(LoginTTL),
	}
	c.logins[state] = s

	return provider.Login{
		State:     state,
		URL:       authURL,
		ExpiresAt: s.Expires.Unix(),
	}, nil
}

func (c *Client) ReleaseLogin(state string) {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	delete(c.logins, state)
}

// CheckLogin polls the cloud auth endpoint /v1/claw/auth/code?state=<state>.
// Returns ready=false when pending (202), exchanges code at STS once 200 is returned.
func (c *Client) CheckLogin(ctx context.Context, scope, state string) (json.RawMessage, bool, error) {
	c.loginMu.Lock()
	s, ok := c.logins[state]
	c.loginMu.Unlock()

	if !ok || time.Now().After(s.Expires) {
		return nil, false, invalid("OfficeAce 授权已失效或过期，请重新生成链接")
	}

	codeURL := fmt.Sprintf("%s%s?state=%s", c.CloudBase, AuthCodePath, url.QueryEscape(s.State))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, codeURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return nil, false, provider.NewTransportError("OfficeAce", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 202 {
		// Not ready yet
		return nil, false, nil
	}
	if resp.StatusCode == 404 {
		return nil, false, invalid("授权码已过期或未知 state，请重新发起登录")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, providerhttp.Failure("OfficeAce", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, false, err
	}

	var codeResp struct {
		Code              string `json:"code"`
		AuthorizationCode string `json:"authorization_code"`
		Error             string `json:"error"`
		ErrorMsg          string `json:"error_msg"`
	}
	if err := json.Unmarshal(bodyBytes, &codeResp); err != nil {
		return nil, false, providerhttp.Broken("OfficeAce")
	}
	if codeResp.Error != "" || codeResp.ErrorMsg != "" {
		msg := codeResp.ErrorMsg
		if msg == "" {
			msg = codeResp.Error
		}
		return nil, false, invalid("授权失败：" + msg)
	}

	code := codeResp.Code
	if code == "" {
		code = codeResp.AuthorizationCode
	}
	if code == "" {
		return nil, false, invalid("云端未返回授权码")
	}

	// Exchange code for STS credentials
	cred, err := c.exchangeCode(ctx, s, code)
	if err != nil {
		return nil, false, fmt.Errorf("换取凭据失败：%w", err)
	}

	// Fetch model info (model_app_key / secret / maas_base_url)
	if info, err := c.fetchModelInfo(ctx, cred); err == nil {
		cred.ModelAppKey = info.AppKey
		cred.ModelAppSecret = info.AppSecret
		cred.MaaSBaseURL = info.BaseURL
		if info.AccountID != "" {
			cred.DomainID = info.AccountID
			cred.AccountID = info.AccountID
		}
	}

	raw, err := json.Marshal(cred)
	if err != nil {
		return nil, false, err
	}

	normalized, _, err := c.Normalize(raw)
	if err != nil {
		return nil, false, err
	}

	return normalized, true, nil
}

func (c *Client) fetchState(ctx context.Context) (string, error) {
	stateURL := c.CloudBase + AuthStatePath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, stateURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", provider.NewTransportError("OfficeAce", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", providerhttp.Failure("OfficeAce", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", err
	}

	var stateResp struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(bodyBytes, &stateResp); err != nil || stateResp.State == "" {
		return "", providerhttp.Broken("OfficeAce")
	}
	return stateResp.State, nil
}

func (c *Client) buildAuthorizeURL(state, challenge string) string {
	q := url.Values{}
	q.Set("client_id", c.ClientID)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "SHA-256")
	q.Set("state", state)
	q.Set("scope", "openid")
	q.Set("redirect_uri", c.RedirectURI)
	q.Set("response_type", "code")

	inner := fmt.Sprintf("%s/authui/v1/oauth2/authorize?%s", c.AuthBase, q.Encode())

	const bgImg = "https://res.hc-cdn.com/AgentArts-Console/26.4.7/hws/assets/login/bg2.png"
	const adImg = "https://res.hc-cdn.com/AgentArts-Console/26.4.7/hws/assets/login/ad2.png"

	outer := fmt.Sprintf("%s/authui/login.html?service=%s&background_img_url=%s&portal_img_url=%s",
		c.AuthBase,
		url.QueryEscape(inner),
		url.QueryEscape(bgImg),
		url.QueryEscape(adImg),
	)
	return outer
}

func (c *Client) exchangeCode(ctx context.Context, s *loginSession, code string) (Credentials, error) {
	proof, err := dpopProof(s.PrivJWK, s.PubJWK, http.MethodPost, c.TokenURL)
	if err != nil {
		return Credentials{}, err
	}

	form := url.Values{}
	form.Set("client_id", c.ClientID)
	form.Set("code", code)
	form.Set("code_verifier", s.CodeVerifier)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", c.RedirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("DPoP", proof)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Credentials{}, ctx.Err()
		}
		return Credentials{}, provider.NewTransportError("OfficeAce", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Credentials{}, err
	}

	var env refreshEnvelope
	if err := json.Unmarshal(bodyBytes, &env); err != nil {
		return Credentials{}, providerhttp.Failure("OfficeAce", resp.StatusCode)
	}

	creds := env.Credentials
	if creds == nil && env.Result != nil {
		creds = env.Result.Credentials
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || creds == nil || creds.AccessKeyID == "" {
		msg := env.ErrorDescription
		if msg == "" {
			msg = env.Error
		}
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return Credentials{}, fmt.Errorf("换取凭据失败：%s", msg)
	}

	refreshToken := env.RefreshToken
	if refreshToken == "" && env.Result != nil {
		refreshToken = env.Result.RefreshToken
	}

	_, userName := parseIDTokenSubject(env.IDToken)

	var expUnix int64
	if creds.Expiration != "" {
		expUnix = parseTimeISO(creds.Expiration)
	}
	if expUnix <= 0 {
		expUnix = time.Now().Add(24 * time.Hour).Unix()
	}

	cred := Credentials{
		AccessKeyID:       creds.AccessKeyID,
		SecretAccessKey:   creds.SecretAccessKey,
		SecurityToken:     creds.SecurityToken,
		ExpiresAt:         creds.Expiration,
		ExpiresAtUnix:     expUnix,
		RefreshToken:      refreshToken,
		CodeVerifier:      s.CodeVerifier,
		DPoPPrivateKeyJWK: s.PrivJWK,
		DPoPPublicKeyJWK:  s.PubJWK,
		SessionID:         s.State,
		UserPhone:         userName,
		OAuthClient:       c.ClientID,
		ProjectID:         creds.ProjectID,
	}
	return cred, nil
}

func (c *Client) fetchModelInfo(ctx context.Context, cred Credentials) (ModelInfo, error) {
	permURL := c.CloudBase + PermissionPath
	headers, err := signV4(cred.AccessKeyID, cred.SecretAccessKey, cred.SecurityToken, http.MethodGet, permURL, nil, map[string]string{
		"content-type": "application/json;charset=utf8",
	}, "")
	if err != nil {
		return ModelInfo{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, permURL, nil)
	if err != nil {
		return ModelInfo{}, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ModelInfo{}, ctx.Err()
		}
		return ModelInfo{}, provider.NewTransportError("OfficeAce", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return ModelInfo{}, providerhttp.Failure("OfficeAce", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ModelInfo{}, err
	}

	var permResp struct {
		AccountID   string `json:"account_id"`
		PrincipalID string `json:"principal_id"`
		ModelInfo   *struct {
			ModelAuthInfo *struct {
				ModelAppKey    string `json:"model_app_key"`
				ModelAppSecret string `json:"model_app_secret"`
			} `json:"model_auth_info"`
			ModelAPIURLBase string `json:"model_api_url_base"`
		} `json:"model_info"`
		Subscription *struct {
			ModelInfo *struct {
				ModelAuthInfo *struct {
					ModelAppKey    string `json:"model_app_key"`
					ModelAppSecret string `json:"model_app_secret"`
				} `json:"model_auth_info"`
				ModelAPIURLBase string `json:"model_api_url_base"`
			} `json:"model_info"`
		} `json:"subscription"`
	}

	if err := json.Unmarshal(bodyBytes, &permResp); err != nil {
		return ModelInfo{}, providerhttp.Broken("OfficeAce")
	}

	mi := permResp.ModelInfo
	if mi == nil && permResp.Subscription != nil {
		mi = permResp.Subscription.ModelInfo
	}

	info := ModelInfo{
		AccountID:   permResp.AccountID,
		PrincipalID: permResp.PrincipalID,
		Subscribed:  permResp.Subscription != nil || permResp.ModelInfo != nil,
	}

	if mi != nil {
		if mi.ModelAuthInfo != nil {
			info.AppKey = mi.ModelAuthInfo.ModelAppKey
			info.AppSecret = mi.ModelAuthInfo.ModelAppSecret
		}
		base := strings.TrimSpace(mi.ModelAPIURLBase)
		base = strings.TrimPrefix(base, "http://")
		base = strings.TrimPrefix(base, "https://")
		base = strings.TrimSuffix(base, "/")
		if base != "" {
			info.BaseURL = "https://" + base + "/v2"
		}
	}

	return info, nil
}
