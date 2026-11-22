package officeace

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

const (
	ProviderID         = "officeace"
	ProviderName       = "云果办"
	Region             = "cn-southwest-2"
	Service            = "apic"
	DefaultCloudBase   = "https://agentarts.cn-southwest-2.myhuaweicloud.com"
	DefaultAuthBase    = "https://auth.huaweicloud.com"
	DefaultRedirectURI = "https://agentarts.cn-southwest-2.myhuaweicloud.com/v1/claw/auth/callback"
	DefaultTokenURL    = "https://sts.cn-north-4.myhuaweicloud.com/v1/oauth2/tokens"
	DefaultSubBase     = "https://officeace.cn-southwest-2.huaweicloud-agentarts.com"
	DefaultMaaSBase    = "https://api.modelarts-maas.com"
	DefaultClientID    = "pdp5_for_agentarts"
	CodeArtsClientID   = "codearts-agent"
	LoginTTL           = 10 * time.Minute
	RatesTTL           = 20 * time.Minute
	PermissionPath     = "/v1/claw/client-permission-validate"
	AuthStatePath      = "/v1/claw/auth/state"
	AuthCodePath       = "/v1/claw/auth/code"
	SubscriptionPath   = "/v1/subscription"
	TokenRatesPath     = "/v1/model-token-rates"
	BonusClaimPath     = "/v1/subscription/bonus/claim"
	ModelsPath         = "/v2/models"
	ChatPath           = "/v2/chat/completions"
)

// Internal unified credential representation stored in Account.Credentials.
// Supports both canonical officeace names and historical/exported keys.
type Credentials struct {
	AccessKeyID       string          `json:"access_key_id"`
	SecretAccessKey   string          `json:"secret_access_key"`
	SecurityToken     string          `json:"security_token"`
	ExpiresAt         string          `json:"expires_at"`
	RefreshToken      string          `json:"refresh_token,omitempty"`
	CodeVerifier      string          `json:"code_verifier,omitempty"`
	DPoPPrivateKeyJWK json.RawMessage `json:"dpop_private_key_jwk,omitempty"`
	DPoPPublicKeyJWK  json.RawMessage `json:"dpop_public_key_jwk,omitempty"`
	SessionID         string          `json:"session_id,omitempty"`
	DomainID          string          `json:"domain_id,omitempty"`
	AccountID         string          `json:"account_id,omitempty"`
	UserPhone         string          `json:"user_phone,omitempty"`
	ModelAppKey       string          `json:"model_app_key,omitempty"`
	ModelAppSecret    string          `json:"model_app_secret,omitempty"`
	MaaSBaseURL       string          `json:"maas_base_url,omitempty"`
	OAuthClient       string          `json:"oauth_client,omitempty"`
	ProjectID         string          `json:"project_id,omitempty"`
	ExpiresAtUnix     int64           `json:"expires_at_unix,omitempty"`
}

type Client struct {
	HTTP        *http.Client
	CloudBase   string
	AuthBase    string
	TokenURL    string
	SubBase     string
	MaaSBase    string
	ClientID    string
	RedirectURI string

	loginMu sync.Mutex
	logins  map[string]*loginSession

	ratesMu sync.RWMutex
	rates   map[string]ratesCacheEntry
}

type ratesCacheEntry struct {
	at   time.Time
	data map[string]float64
}

func New() *Client {
	return &Client{
		HTTP:        providerhttp.New(),
		CloudBase:   DefaultCloudBase,
		AuthBase:    DefaultAuthBase,
		TokenURL:    DefaultTokenURL,
		SubBase:     DefaultSubBase,
		MaaSBase:    DefaultMaaSBase,
		ClientID:    DefaultClientID,
		RedirectURI: DefaultRedirectURI,
		logins:      make(map[string]*loginSession),
		rates:       make(map[string]ratesCacheEntry),
	}
}

func (c *Client) Describe() provider.Descriptor {
	return provider.Descriptor{
		ID:        ProviderID,
		Name:      ProviderName,
		Available: true,
		Login:     true,
		Scopes:    []string{"cn"},
	}
}

var (
	_ provider.Adapter       = (*Client)(nil)
	_ provider.Streaming     = (*Client)(nil)
	_ provider.BrowserLogin  = (*Client)(nil)
	_ provider.LoginReleaser = (*Client)(nil)
	_ provider.DailyRewards  = (*Client)(nil)
)
