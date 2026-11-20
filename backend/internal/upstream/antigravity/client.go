// SPDX-License-Identifier: LGPL-3.0-only
// OAuth constants and endpoint protocol adapted from Sub2API; see UPSTREAM.md.
package antigravity

import (
	"codearts2api/internal/providerhttp"
	"net/http"
	"net/url"
	"os"
)

const (
	RedirectURI      = "http://localhost:8085/callback"
	UserAgentVersion = "2.23.0"
	UserAgent        = "antigravity/hub/" + UserAgentVersion + " windows/amd64"
	scopes           = "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile https://www.googleapis.com/auth/cclog https://www.googleapis.com/auth/experimentsandconfigs"
)

// Client owns only platform transport. Existing account services will own login
// commits and credential rotation.
type Client struct {
	clientSecret                   string
	HTTP                           *http.Client
	BaseURL, TokenURL, UserInfoURL string
	Login                          *providerhttp.PKCE
}

func New() *Client {
	return &Client{clientSecret: os.Getenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET"), HTTP: providerhttp.New(), BaseURL: "https://daily-cloudcode-pa.googleapis.com", TokenURL: "https://oauth2.googleapis.com/token", UserInfoURL: "https://www.googleapis.com/oauth2/v2/userinfo", Login: &providerhttp.PKCE{Config: providerhttp.OAuthConfig{Name: "Antigravity", Authorize: "https://accounts.google.com/o/oauth2/v2/auth", Redirect: RedirectURI, ClientID: os.Getenv("ANTIGRAVITY_OAUTH_CLIENT_ID"), Scopes: scopes, Extra: url.Values{"access_type": {"offline"}, "prompt": {"consent"}}}}}
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}
