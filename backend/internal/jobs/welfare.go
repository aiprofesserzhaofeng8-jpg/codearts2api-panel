package jobs

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
)

// WelfareStore borrows the existing scheduler; welfare owns no selection state.
type WelfareStore interface {
	Account(string) *accounts.Account
	NextAccount() *accounts.Account
	Accounts() []*accounts.Account
}

type Welfare struct{ store WelfareStore }

func NewWelfare(store WelfareStore) *Welfare { return &Welfare{store: store} }

func (w *Welfare) account(id string) *accounts.Account {
	var a *accounts.Account
	if id != "" {
		a = w.store.Account(id)
	}
	if a == nil {
		a = w.store.NextAccount()
	}
	return a
}

// ForList retains the daily-rewards fallback when the scheduler has no account.
// Explicit IDs remain untrimmed and may select a disabled account.
func (w *Welfare) ForList(id string, registry *provider.Registry) *accounts.Account {
	a := w.account(id)
	if a == nil {
		for _, candidate := range w.store.Accounts() {
			if candidate.Enabled && Supported(candidate, registry) {
				a = candidate
				break
			}
		}
	}
	return a
}

// ForInvite deliberately has no daily-rewards fallback or provider substitution.
func (w *Welfare) ForInvite(id string) *accounts.Account { return w.account(id) }

// AccountOptions includes disabled supported accounts, in the original order.
func (w *Welfare) AccountOptions(registry *provider.Registry) []map[string]any {
	accts := w.store.Accounts()
	out := make([]map[string]any, 0, len(accts))
	for _, a := range accts {
		if !Supported(a, registry) {
			continue
		}
		out = append(out, map[string]any{
			"id": a.ID, "name": a.DisplayName(), "provider": a.ProviderID(),
			"enabled": a.Enabled, "status": a.Status,
			"permanent": a.Session != nil && a.Session.IsPermanent(),
		})
	}
	return out
}

// WelfareSnapshot is the console projection, without native response metadata.
type WelfareSnapshot struct {
	Campaigns      []WelfareCampaign
	ClaimableCount int
}

// Keep zero/empty fields and the previous map's JSON key order on the wire.
type WelfareCampaign struct {
	AutoClaimable bool    `json:"auto_claimable"`
	BenefitAmount float64 `json:"benefit_amount"`
	BenefitUnit   string  `json:"benefit_unit"`
	CampaignID    int     `json:"campaign_id"`
	Claimable     bool    `json:"claimable"`
	Description   string  `json:"description"`
	EndTime       string  `json:"end_time"`
	StartTime     string  `json:"start_time"`
	Status        string  `json:"status"`
	Title         string  `json:"title"`
	TriggerEvent  string  `json:"trigger_event"`
	TriggerMode   string  `json:"trigger_mode"`
	Type          string  `json:"type"`
}

type WelfareInvite struct {
	CampaignID      int
	InviteCode      string
	InviteURL       string
	Valid           bool
	Status          string
	ActivationCount int
	MaxActivations  int
}
