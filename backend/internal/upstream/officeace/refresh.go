package officeace

import (
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

type refreshEnvelope struct {
	Credentials *struct {
		AccessKeyID     string `json:"access_key_id"`
		SecretAccessKey string `json:"secret_access_key"`
		SecurityToken   string `json:"security_token"`
		Expiration      string `json:"expiration"`
		ProjectID       string `json:"project_id"`
	} `json:"credentials"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	Result       *struct {
		Credentials *struct {
			AccessKeyID     string `json:"access_key_id"`
			SecretAccessKey string `json:"secret_access_key"`
			SecurityToken   string `json:"security_token"`
			Expiration      string `json:"expiration"`
			ProjectID       string `json:"project_id"`
		} `json:"credentials"`
		RefreshToken string `json:"refresh_token"`
	} `json:"result"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// Refresh rotates the STS credentials using the refresh token and DPoP.
// Supports both pdp5_for_agentarts and codearts-agent client_ids.
func (c *Client) Refresh(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	norm, before, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	if !before.HasRefresh {
		return nil, &provider.Error{Status: 401, AccountFault: true, Message: "OfficeAce 凭据缺少 refresh_token 或 DPoP 私钥，无法自动续期"}
	}
	var cred Credentials
	if err := json.Unmarshal(norm, &cred); err != nil {
		return nil, err
	}

	proof, err := dpopProof(cred.DPoPPrivateKeyJWK, cred.DPoPPublicKeyJWK, http.MethodPost, c.TokenURL)
	if err != nil {
		return nil, fmt.Errorf("生成 DPoP 证明失败：%w", err)
	}

	clientID := cred.OAuthClient
	if clientID == "" {
		clientID = c.ClientID
	}

	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", cred.RefreshToken)
	if cred.CodeVerifier != "" {
		form.Set("code_verifier", cred.CodeVerifier)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("DPoP", proof)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, provider.NewTransportError("OfficeAce", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var env refreshEnvelope
	if err := json.Unmarshal(respBody, &env); err != nil {
		return nil, providerhttp.Failure("OfficeAce", resp.StatusCode)
	}

	creds := env.Credentials
	newRefresh := env.RefreshToken
	if creds == nil && env.Result != nil {
		creds = env.Result.Credentials
		if newRefresh == "" {
			newRefresh = env.Result.RefreshToken
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 || creds == nil || creds.AccessKeyID == "" {
		msg := env.ErrorDescription
		if msg == "" {
			msg = env.Error
		}
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		// Check for expired/spent token
		isSpent := strings.Contains(msg, "STS5.1806") || strings.Contains(msg, "STS5.1807") || resp.StatusCode == 400 || resp.StatusCode == 401
		return nil, &provider.Error{
			Status:       resp.StatusCode,
			AccountFault: isSpent,
			Message:      fmt.Sprintf("OfficeAce 凭据续期失败（%s）", msg),
		}
	}

	// Update credentials with new STS credentials and rotated refresh token
	cred.AccessKeyID = creds.AccessKeyID
	cred.SecretAccessKey = creds.SecretAccessKey
	cred.SecurityToken = creds.SecurityToken
	cred.ExpiresAt = creds.Expiration
	if creds.ProjectID != "" {
		cred.ProjectID = creds.ProjectID
	}
	if newRefresh != "" {
		cred.RefreshToken = newRefresh
	}

	var expUnix int64
	if creds.Expiration != "" {
		expUnix = parseTimeISO(creds.Expiration)
	}
	if expUnix <= 0 {
		expUnix = time.Now().Add(24 * time.Hour).Unix()
	}
	cred.ExpiresAtUnix = expUnix

	// Best-effort re-fetch MaaS model info if missing or expired
	if cred.ModelAppKey == "" || cred.MaaSBaseURL == "" {
		if info, err := c.fetchModelInfo(ctx, cred); err == nil {
			if info.AppKey != "" {
				cred.ModelAppKey = info.AppKey
			}
			if info.AppSecret != "" {
				cred.ModelAppSecret = info.AppSecret
			}
			if info.BaseURL != "" {
				cred.MaaSBaseURL = info.BaseURL
			}
			if cred.DomainID == "" && info.AccountID != "" {
				cred.DomainID = info.AccountID
				cred.AccountID = info.AccountID
			}
		}
	}

	updatedRaw, err := json.Marshal(cred)
	if err != nil {
		return nil, err
	}

	normalized, _, err := c.Normalize(updatedRaw)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}
