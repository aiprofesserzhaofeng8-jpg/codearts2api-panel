package identitytransport

import (
	"codearts2api/internal/access"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (*Client) Exchange(ctx context.Context, cfg access.IdentitySettings, provider, code, verifier, origin string) (string, error) {
	p, ok := access.OAuthProviderFor(provider)
	if !ok {
		return "", fmt.Errorf("第三方登录类型无效")
	}
	c := cfg.GitHub
	if provider == "linuxdo" {
		c = cfg.LinuxDO
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	body := url.Values{"client_id": {c.ClientID}, "client_secret": {c.ClientSecret}, "code": {code}, "grant_type": {"authorization_code"}, "redirect_uri": {origin + "/auth/oauth/callback"}, "code_verifier": {verifier}}
	req, err := http.NewRequestWithContext(ctx, "POST", p.Token, strings.NewReader(body.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("第三方令牌交换失败")
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&token)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 || token.AccessToken == "" {
		return "", fmt.Errorf("第三方令牌交换失败")
	}
	req, err = http.NewRequestWithContext(ctx, "GET", p.User, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "CodeArts2API-Panel")
	resp, err = client.Do(req)
	if err != nil {
		return "", fmt.Errorf("第三方身份查询失败")
	}
	var info struct {
		ID json.Number `json:"id"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 65536))
	decoder.UseNumber()
	err = decoder.Decode(&info)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 || info.ID.String() == "" || len(info.ID.String()) > 100 {
		return "", fmt.Errorf("第三方身份查询失败")
	}

	subject := info.ID.String()
	if subject[0] < '1' || subject[0] > '9' {
		return "", fmt.Errorf("第三方身份标识无效")
	}
	for _, c := range subject {
		if c < '0' || c > '9' {
			return "", fmt.Errorf("第三方身份标识无效")
		}
	}
	return subject, nil
}
