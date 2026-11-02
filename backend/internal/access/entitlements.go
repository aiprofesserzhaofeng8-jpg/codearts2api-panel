package access

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Credits are internal USD reference units, never a payment-provider balance.
// Integer arithmetic preserves eight decimal places across grants and replay.
const CreditScale int64 = 100000000
const MaxCredits int64 = 100000000000000

type Wallet struct {
	Enabled        bool   `json:"enabled"`
	Balance        int64  `json:"balance"`
	Revision       uint64 `json:"revision"`
	ReviewRequired bool   `json:"review_required"`
}
type LedgerEntry struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Time      int64  `json:"time"`
	Delta     int64  `json:"delta"`
	Balance   int64  `json:"balance"`
	Kind      string `json:"kind"`
	Reference string `json:"reference"`
	Note      string `json:"note"`
}
type Plan struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	GroupID        string    `json:"group_id"`
	Enabled        bool      `json:"enabled"`
	Days           int       `json:"days"`
	MaxConcurrency int       `json:"max_concurrency"`
	Budget         KeyBudget `json:"budget"`
	Period         string    `json:"period"` // lifetime / day / week / month, UTC
}
type Subscription struct {
	ID          string      `json:"id"`
	Plan        Plan        `json:"plan"` // immutable assignment snapshot
	ExpiresAt   int64       `json:"expires_at"`
	Disabled    bool        `json:"disabled"`
	PeriodStart int64       `json:"period_start"`
	Usage       BudgetUsage `json:"usage"`
}
type Redemption struct {
	ID          string `json:"id"`
	Digest      string `json:"digest"`
	Hint        string `json:"hint"`
	Kind        string `json:"kind"`
	Amount      int64  `json:"amount"`
	Concurrency int    `json:"concurrency"`
	PlanID      string `json:"plan_id"`
	ExpiresAt   int64  `json:"expires_at"`
	Disabled    bool   `json:"disabled"`
	UsedBy      string `json:"used_by"`
	UsedAt      int64  `json:"used_at"`
}
type ExternalIdentity struct {
	Provider string `json:"provider"`
	Subject  string `json:"subject"`
	UserID   string `json:"user_id"`
}
type ConsumerState struct {
	Ledger      []LedgerEntry      `json:"ledger,omitempty"`
	Plans       []Plan             `json:"plans,omitempty"`
	Redemptions []Redemption       `json:"redemptions,omitempty"`
	Identities  []ExternalIdentity `json:"identities,omitempty"`
	Identity    IdentitySettings   `json:"identity"`
}

func ParseCredits(s string) (int64, error) {
	s = strings.TrimSpace(s)
	negative := strings.HasPrefix(s, "-")
	if negative {
		s = s[1:]
	}
	parts := strings.Split(s, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, fmt.Errorf("额度格式无效")
	}
	for _, p := range parts {
		for _, c := range p {
			if c < '0' || c > '9' {
				return 0, fmt.Errorf("额度必须为十进制数字")
			}
		}
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > MaxCredits/CreditScale {
		return 0, fmt.Errorf("额度超出范围")
	}
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
	}
	if len(frac) > 8 {
		return 0, fmt.Errorf("额度最多支持 8 位小数")
	}
	f, _ := strconv.ParseInt(frac+strings.Repeat("0", 8-len(frac)), 10, 64)
	n := whole*CreditScale + f
	if n > MaxCredits {
		return 0, fmt.Errorf("额度超出范围")
	}
	if negative {
		n = -n
	}
	return n, nil
}
func CostCredits(cost float64) (int64, error) {
	if math.IsNaN(cost) || math.IsInf(cost, 0) || cost < 0 || cost > float64(MaxCredits)/float64(CreditScale) {
		return 0, fmt.Errorf("费用超出范围")
	}
	return int64(math.Ceil(cost*float64(CreditScale) - 1e-9)), nil
}
func CodeDigest(code string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(h[:])
}
func (p Plan) Validate() error {
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 120 || p.Days < 1 || p.Days > 3650 {
		return fmt.Errorf("套餐名称和有效天数（1–3650）无效")
	}
	if err := ValidateUserConcurrency(p.MaxConcurrency); err != nil {
		return err
	}
	if err := ValidateBudget(p.Budget); err != nil {
		return err
	}
	switch p.Period {
	case "lifetime", "day", "week", "month":
		return nil
	}
	return fmt.Errorf("额度周期须为 lifetime、day、week 或 month")
}
