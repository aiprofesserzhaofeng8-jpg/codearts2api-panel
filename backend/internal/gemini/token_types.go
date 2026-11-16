// SPDX-License-Identifier: LGPL-3.0-only
// Derived from pinned Sub2API; see UPSTREAM.md.
package gemini

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope,omitempty"`
}
