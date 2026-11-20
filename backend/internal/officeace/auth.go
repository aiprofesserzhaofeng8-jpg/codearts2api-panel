package officeace

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

func invalid(msg string) error {
	return providerhttp.Invalid("OfficeAce", msg)
}

func firstString(m map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		if raw, ok := m[k]; ok && len(raw) > 0 && string(raw) != "null" {
			var s string
			if json.Unmarshal(raw, &s) == nil && s != "" {
				return s
			}
		}
	}
	return ""
}

func firstRaw(m map[string]json.RawMessage, keys ...string) json.RawMessage {
	for _, k := range keys {
		if raw, ok := m[k]; ok && len(raw) > 0 && string(raw) != "null" {
			return raw
		}
	}
	return nil
}

func parseTimeISO(s string) int64 {
	if s == "" {
		return 0
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Unix()
		}
	}
	return 0
}

func parseIDTokenSubject(idToken string) (sub, preferredUsername string) {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return "", ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return "", ""
		}
	}
	var claims struct {
		Sub               string `json:"sub"`
		PreferredUsername string `json:"preferred_username"`
		Name              string `json:"name"`
	}
	if json.Unmarshal(raw, &claims) == nil {
		name := claims.PreferredUsername
		if name == "" {
			name = claims.Name
		}
		return claims.Sub, name
	}
	return "", ""
}

// Normalize normalizes incoming JSON credentials (either flat or auth/account-nested,
// in either OfficeAce historical camelCase or CodeArts snake_case shape).
func (c *Client) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return nil, provider.Identity{}, invalid("凭据须为 JSON 对象")
	}

	// Unwrap nested auth / account / access if present
	if authRaw, ok := m["auth"]; ok && len(authRaw) > 0 && string(authRaw) != "null" {
		var nested map[string]json.RawMessage
		if json.Unmarshal(authRaw, &nested) == nil && nested != nil {
			m = nested
		}
	} else if acctRaw, ok := m["account"]; ok && len(acctRaw) > 0 && string(acctRaw) != "null" {
		var nested map[string]json.RawMessage
		if json.Unmarshal(acctRaw, &nested) == nil && nested != nil {
			m = nested
		}
	} else if accessRaw, ok := m["access"]; ok && len(accessRaw) > 0 && string(accessRaw) != "null" {
		// Could be JSON string or JSON object
		var s string
		if json.Unmarshal(accessRaw, &s) == nil {
			var nested map[string]json.RawMessage
			if json.Unmarshal([]byte(s), &nested) == nil && nested != nil {
				m = nested
			}
		} else {
			var nested map[string]json.RawMessage
			if json.Unmarshal(accessRaw, &nested) == nil && nested != nil {
				m = nested
			}
		}
	}

	ak := firstString(m, "access_key_id", "accessKeyId", "access")
	sk := firstString(m, "secret_access_key", "secretAccessKey", "secret")
	token := firstString(m, "security_token", "securityToken")

	if ak == "" || sk == "" || token == "" {
		return nil, provider.Identity{}, invalid("缺少 STS 临时凭据（AK/SK/SecurityToken）")
	}

	refresh := firstString(m, "refresh_token", "refreshToken", "refresh")
	codeVerifier := firstString(m, "code_verifier", "codeVerifier")
	privJWK := firstRaw(m, "dpop_private_key_jwk", "dpopPrivateJwk", "dpop_private_jwk")
	pubJWK := firstRaw(m, "dpop_public_key_jwk", "dpopPublicJwk", "dpop_public_jwk")
	sessionID := firstString(m, "session_id", "sessionId")
	domainID := firstString(m, "domain_id", "domainId", "account_id", "accountId")
	userPhone := firstString(m, "user_phone", "userPhone")
	modelAppKey := firstString(m, "model_app_key", "modelAppKey")
	modelAppSecret := firstString(m, "model_app_secret", "modelAppSecret")
	maasBaseURL := firstString(m, "maas_base_url", "maasBaseUrl", "baseUrl")
	if _, err := c.maasBase(Credentials{MaaSBaseURL: maasBaseURL}); err != nil {
		return nil, provider.Identity{}, err
	}
	projectID := firstString(m, "project_id", "projectId")
	oauthClient := firstString(m, "oauth_client", "oauthClient")

	// ExpiresAt resolution
	expiresStr := firstString(m, "expires_at", "expiresAt")
	var expUnix int64
	if expiresRaw, ok := m["expires_at"]; ok {
		expUnix = providerhttp.Timestamp(expiresRaw)
	}
	if expUnix == 0 {
		if expiresRaw, ok := m["expiresAt"]; ok {
			expUnix = providerhttp.Timestamp(expiresRaw)
		}
	}
	if expUnix == 0 && expiresStr != "" {
		expUnix = parseTimeISO(expiresStr)
	}
	if expUnix == 0 {
		if expRaw, ok := m["expires"]; ok {
			expUnix = providerhttp.Timestamp(expRaw)
		}
	}
	if expUnix == 0 {
		expUnix = time.Now().Add(24 * time.Hour).Unix()
	}

	if oauthClient == "" {
		if firstString(m, "access_key_id") != "" {
			oauthClient = CodeArtsClientID
		} else {
			oauthClient = DefaultClientID
		}
	}

	cred := Credentials{
		AccessKeyID:       ak,
		SecretAccessKey:   sk,
		SecurityToken:     token,
		ExpiresAt:         expiresStr,
		ExpiresAtUnix:     expUnix,
		RefreshToken:      refresh,
		CodeVerifier:      codeVerifier,
		DPoPPrivateKeyJWK: privJWK,
		DPoPPublicKeyJWK:  pubJWK,
		SessionID:         sessionID,
		DomainID:          domainID,
		AccountID:         domainID,
		UserPhone:         userPhone,
		ModelAppKey:       modelAppKey,
		ModelAppSecret:    modelAppSecret,
		MaaSBaseURL:       maasBaseURL,
		OAuthClient:       oauthClient,
		ProjectID:         projectID,
	}

	// ExternalID stability:
	// Priority: domainID / accountID -> sessionID -> first 12 chars of AccessKeyID
	id := domainID
	if id == "" {
		id = sessionID
	}
	if id == "" {
		if len(ak) >= 12 {
			id = ak[:12]
		} else {
			id = ak
		}
	}

	name := "OfficeAce"
	if userPhone != "" {
		name = userPhone
	} else if domainID != "" {
		name = "OfficeAce " + domainID
	}

	hasRefresh := refresh != "" && len(privJWK) > 0

	normalizedBytes, err := json.Marshal(cred)
	if err != nil {
		return nil, provider.Identity{}, err
	}

	return normalizedBytes, provider.Identity{
		ID:         id,
		Name:       name,
		Scope:      "cn",
		ExpiresAt:  expUnix,
		HasRefresh: hasRefresh,
	}, nil
}

// Verify verifies the imported credentials by reading user profile/permissions from CLAW.
// Required by accounts.ProviderLogins.Import.
func (c *Client) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	norm, id, err := c.Normalize(raw)
	if err != nil {
		return id, err
	}
	var cred Credentials
	if err := json.Unmarshal(norm, &cred); err != nil {
		return id, err
	}

	// Call client-permission-validate to verify STS credentials and obtain model auth info.
	_, err = c.fetchModelInfo(ctx, cred)
	if err != nil {
		return id, fmt.Errorf("OfficeAce 凭据身份核验失败：%w", err)
	}

	if cred.UserPhone != "" {
		id.Name = cred.UserPhone
	}

	return id, nil
}
