package accounts

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// QuotaWindow is a display snapshot, never a second admission or billing limit.
// Pointers preserve unknown vs known zero. Utilization is percent (0..100+).
type QuotaWindow struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Unit          string   `json:"unit,omitempty"`
	Used          *float64 `json:"used,omitempty"`
	Total         *float64 `json:"total,omitempty"`
	Remaining     *float64 `json:"remaining,omitempty"`
	Utilization   *float64 `json:"utilization,omitempty"`
	ResetsAt      int64    `json:"resets_at,omitempty"`
	ExpiresAt     int64    `json:"expires_at,omitempty"`
	WindowSeconds int64    `json:"window_seconds,omitempty"`
	Unlimited     bool     `json:"unlimited,omitempty"`
}

// QuotaBalance preserves upstream decimal strings without rounding or treating
// an unknown subscription balance as zero dollars.
type QuotaBalance struct {
	Amount    *string `json:"amount,omitempty"`
	Unit      string  `json:"unit"`
	Unlimited bool    `json:"unlimited,omitempty"`
}

// Complete derives only arithmetic implied by returned numbers. Missing
// amounts remain nil; a zero limit does not imply unlimited usage.
func (w *QuotaWindow) Complete() bool {
	for _, p := range []*float64{w.Used, w.Total, w.Remaining, w.Utilization} {
		if p != nil && (math.IsNaN(*p) || math.IsInf(*p, 0) || *p < 0) {
			return false
		}
	}
	if w.ResetsAt < 0 || w.ExpiresAt < 0 || w.WindowSeconds < 0 {
		return false
	}
	if !w.Unlimited {
		if w.Remaining == nil && w.Used != nil && w.Total != nil {
			v := math.Max(0, *w.Total-*w.Used)
			w.Remaining = &v
		}
		if w.Used == nil && w.Remaining != nil && w.Total != nil && *w.Remaining <= *w.Total {
			v := *w.Total - *w.Remaining
			w.Used = &v
		}
		if w.Utilization == nil && w.Used != nil && w.Total != nil && *w.Total > 0 {
			v := *w.Used / *w.Total * 100
			if math.IsInf(v, 0) || math.IsNaN(v) {
				return false
			}
			w.Utilization = &v
		}
	}
	return w.Unlimited || w.Used != nil || w.Total != nil || w.Remaining != nil || w.Utilization != nil
}

// QuotaNumber accepts numeric and decimal-string representations. Missing or
// null stays unknown. Provider-specific sentinels are checked by the adapter.
func QuotaNumber(o map[string]json.RawMessage, keys ...string) (*float64, error) {
	for _, key := range keys {
		b := o[key]
		if len(b) == 0 || string(b) == "null" {
			continue
		}
		s := string(b)
		if len(s) > 128 {
			return nil, fmt.Errorf("quota number too large")
		}
		if len(s) > 0 && s[0] == '"' {
			if json.Unmarshal(b, &s) != nil {
				return nil, fmt.Errorf("invalid quota number")
			}
		}
		if strings.TrimSpace(s) != s || s == "" {
			return nil, fmt.Errorf("invalid quota number")
		}
		n, e := strconv.ParseFloat(s, 64)
		if e != nil || math.IsInf(n, 0) || math.IsNaN(n) {
			return nil, fmt.Errorf("invalid quota number")
		}
		return &n, nil
	}
	return nil, nil
}

// SummarizeCredits keeps the original finite-credit projection. Percentage
// windows and unlimited pools never become credits or dollars.
func (c *CreditInfo) SummarizeCredits() {
	for _, w := range c.Windows {
		if w.Unit != "credits" || w.Unlimited || w.Total == nil || w.Used == nil || w.Remaining == nil {
			continue
		}
		c.Total += *w.Total
		c.Used += *w.Used
		c.Remaining += *w.Remaining
	}
	if c.Total > 0 {
		c.Percent = c.Used / c.Total * 100
	}
}
