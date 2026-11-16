// SPDX-License-Identifier: LGPL-3.0-only
// Envelope, microcredit conversion and plan fields adapted from cline_usage.go.
package cline

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var decimalMicrocredits = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

func (c *Client) account(ctx context.Context, raw json.RawMessage, path string) (map[string]json.RawMessage, error) {
	obj, err := c.get(ctx, raw, path)
	if err != nil {
		return nil, err
	}
	if b, ok := obj["success"]; ok {
		var success bool
		if json.Unmarshal(b, &success) != nil || !success {
			return nil, providerhttp.Broken("Cline")
		}
		var data map[string]json.RawMessage
		if json.Unmarshal(obj["data"], &data) != nil || data == nil {
			return nil, providerhttp.Broken("Cline")
		}
		return data, nil
	}
	return obj, nil
}
func (c *Client) Credit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	me, err := c.account(ctx, raw, "/api/v1/users/me")
	if err != nil {
		return nil, err
	}
	id := providerhttp.Field(me, "id")
	if id == "" || id == "." || id == ".." || !providerhttp.Safe(id) {
		return nil, providerhttp.Broken("Cline")
	}
	balance, err := c.account(ctx, raw, "/api/v1/users/"+url.PathEscape(id)+"/balance")
	if err != nil {
		return nil, err
	}
	if owner := providerhttp.Field(balance, "userId"); owner != "" && owner != id {
		return nil, providerhttp.Broken("Cline")
	}
	amount := string(balance["balance"])
	if strings.HasPrefix(amount, `"`) {
		if json.Unmarshal(balance["balance"], &amount) != nil {
			return nil, providerhttp.Broken("Cline")
		}
	}
	// Exact decimal arithmetic: preserve microcredits without float rounding.
	if len(amount) == 0 || len(amount) > 100 || !decimalMicrocredits.MatchString(amount) {
		return nil, providerhttp.Broken("Cline")
	}
	micro, ok := new(big.Rat).SetString(amount)
	if !ok || micro.Sign() < 0 {
		return nil, providerhttp.Broken("Cline")
	}
	dollars := new(big.Rat).Quo(micro, big.NewRat(1000000, 1)).FloatString(6)
	out := accounts.CreditInfo{Kind: "windows", Available: true, UpdatedAt: time.Now().Unix(), Balance: &accounts.QuotaBalance{Amount: &dollars, Unit: "USD"}}
	plan, err := c.account(ctx, raw, "/api/v1/users/me/plan/usage-limits")
	if err != nil {
		var e *provider.Error
		if errors.As(err, &e) && e.Status == 404 {
			return json.Marshal(out)
		}
		return nil, err
	}
	var limits []map[string]json.RawMessage
	if json.Unmarshal(plan["limits"], &limits) != nil || limits == nil {
		return nil, providerhttp.Broken("Cline")
	}
	seen := map[string]bool{}
	for _, item := range limits {
		kind := providerhttp.Field(item, "type")
		seconds := map[string]int64{"five_hour": 18000, "weekly": 604800, "monthly": 2592000}[kind]
		if seconds == 0 {
			continue
		}
		if seen[kind] {
			return nil, providerhttp.Broken("Cline")
		}
		seen[kind] = true
		percent, err := accounts.QuotaNumber(item, "percentUsed")
		if err != nil {
			return nil, providerhttp.Broken("Cline")
		}
		if percent == nil {
			continue
		}
		w := accounts.QuotaWindow{ID: kind, Name: "ClinePass " + kind, Unit: "percent", Utilization: percent, WindowSeconds: seconds}
		if reset := providerhttp.Field(item, "resetsAt"); reset != "" {
			at, err := time.Parse(time.RFC3339Nano, reset)
			if err != nil {
				return nil, providerhttp.Broken("Cline")
			}
			w.ResetsAt = at.Unix()
		}
		if !w.Complete() {
			return nil, providerhttp.Broken("Cline")
		}
		out.Windows = append(out.Windows, w)
	}
	return json.Marshal(out)
}
