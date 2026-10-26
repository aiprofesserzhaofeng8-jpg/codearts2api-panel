package codearts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Protocol verified against the locally installed Huawei authentication extension:
// getAuthenticationByTicketId / doCreateOldLoginAuthentication. Ticket sessions
// carry temporary STS credentials, not a refresh token or a DPoP binding.
const LoginTicketURL = SnapHost + "/snap-manager/v1/login/ticket"

func (c *Client) ExchangeLoginTicket(ctx context.Context, ticket, secret, version string, interval time.Duration) (*Session, error) {
	if ticket == "" || secret == "" {
		return nil, fmt.Errorf("登录票据不完整，请重新发起登录")
	}
	if version == "" {
		version = LoginPluginVersion
	}
	query := url.Values{"ticket_id": {ticket}, "secret": {secret}}
	for attempt := 0; attempt < 10; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, LoginTicketURL+"?"+query.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("无法构造登录票据请求")
		}
		req.Header.Set("Content-Type", "application/json;charset=UTF-8")
		req.Header.Set("plugin-name", LoginPluginName)
		req.Header.Set("plugin-version", version)
		// Do not follow redirects carrying ticket secrets to another host.
		client := *c.HTTP
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// net/url.Error includes the full request URL with the ticket secret.
			return nil, fmt.Errorf("连接华为登录票据服务失败，请检查网关网络连接")
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("读取华为登录票据响应失败")
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("华为登录票据服务返回 HTTP %d，请重新登录", resp.StatusCode)
		}
		var result struct {
			UserName   string `json:"user_name"`
			DomainID   string `json:"domain_id"`
			Credential *struct {
				Access  string `json:"access"`
				Secret  string `json:"secret"`
				Token   string `json:"securitytoken"`
				Expires string `json:"expires_at"`
			} `json:"credential"`
		}
		if json.Unmarshal(raw, &result) != nil {
			return nil, fmt.Errorf("华为登录票据服务返回非预期数据")
		}
		if result.Credential == nil {
			continue
		} // authorization not yet confirmed
		creds := result.Credential
		if creds.Access == "" || creds.Secret == "" || creds.Token == "" {
			return nil, fmt.Errorf("华为返回的临时凭据不完整")
		}
		expires, err := parseTime(creds.Expires)
		if err != nil || !expires.After(time.Now()) {
			return nil, fmt.Errorf("华为返回的凭据已过期或缺少有效期，请重新登录")
		}
		return &Session{AccountLabel: result.UserName, DomainID: result.DomainID, AccessKey: creds.Access,
			SecretKey: creds.Secret, SecurityToken: creds.Token, ExpiresAt: expires, Source: "browser-ticket"}, nil
	}
	return nil, fmt.Errorf("华为尚未确认登录票据，请完成授权后重新登录")
}
