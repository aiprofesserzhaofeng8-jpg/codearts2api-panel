// Package accounts owns platform-neutral account metadata and credential lifecycle errors.
package accounts

import (
	"encoding/json"
	"errors"
	"time"
)

// CredentialSnapshot is the immutable view used by the legacy Session slot.
// Only the storage codec and owning platform interpret its concrete value.
// New platforms use Credentials; this interface is not their credential contract.
// It never causes parsing or serialization on request admission.
type CredentialSnapshot interface {
	Valid(time.Duration) bool
	ExpiresIn() time.Duration
	HasRefresh() bool
	IsPermanent() bool
}

// ErrCredentialChanged rejects stale credential commits, including deleted accounts.
var ErrCredentialChanged = errors.New("账号凭据已更新或账号已删除，请重新发起请求")

// Account owns shared account metadata; platform credentials remain opaque.
type Account struct {
	Provider    string          `json:"provider,omitempty"`
	Scope       string          `json:"scope,omitempty"`
	ExternalID  string          `json:"external_id,omitempty"`
	Credentials json.RawMessage `json:"provider_credentials,omitempty"`

	ID             string `json:"id"`
	Name           string `json:"name"`
	Source         string `json:"source"`    // desktop | manual
	UserData       string `json:"user_data"` // %APPDATA%\codearts-agent
	Enabled        bool   `json:"enabled"`
	Paused         bool   `json:"paused"`
	MaxInFlight    int    `json:"max_in_flight,omitempty"` // 0 inherits the pool default.
	CooldownUntil  int64  `json:"cooldown_until,omitempty"`
	CooldownReason string `json:"cooldown_reason,omitempty"`
	Status         string `json:"status"`
	Failures       int    `json:"failures"`
	LastErr        string `json:"last_err,omitempty"`
	LastUsed       int64  `json:"last_used"`
	Note           string `json:"note,omitempty"`
	// 每日运营活动福利的领取记录（按本地日期去重）
	LastClaimState string `json:"last_claim_state,omitempty"`
	NextClaimAt    int64  `json:"next_claim_at,omitempty"`
	LastClaimDate  string `json:"last_claim_date,omitempty"`
	LastClaimAt    int64  `json:"last_claim_at,omitempty"`
	LastClaimErr   string `json:"last_claim_err,omitempty"`
	LastClaimMsg   string `json:"last_claim_msg,omitempty"`
	// Session preserves the legacy on-disk session field during M3 migration.
	// Keep the immutable platform value; never copy platform fields into this type.
	Session CredentialSnapshot `json:"session,omitempty"`
}

// DisplayName 返回账号展示名。
func (a *Account) DisplayName() string {
	if a.Name != "" {
		return a.Name
	}
	if len(a.ID) > 8 {
		return a.ID[:8]
	}
	return a.ID
}

// ProviderID preserves old state files: omitted provider means CodeArts.
func (a *Account) ProviderID() string {
	if a.Provider == "" {
		return "codearts"
	}
	return a.Provider
}
