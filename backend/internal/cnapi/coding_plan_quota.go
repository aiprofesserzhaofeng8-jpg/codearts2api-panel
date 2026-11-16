// SPDX-License-Identifier: LGPL-3.0-only
// Adapted field mappings from Sub2API cn_provider_quota_service.go; see UPSTREAM.md.
package cnapi

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (c *Client) codingCredit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	normalized, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	var a credentials
	json.Unmarshal(normalized, &a)
	auth := "Bearer " + a.Key
	if c.ID == "zhipu" {
		auth = a.Key
	}
	resp, err := providerhttp.Do(ctx, c.HTTP, c.Name, "GET", c.BalanceURL, http.Header{"Authorization": {auth}, "Accept": {"application/json"}}, nil)
	if err != nil {
		return nil, err
	}
	obj, err := providerhttp.JSON(resp, c.Name)
	if err != nil {
		return nil, err
	}
	out := accounts.CreditInfo{Kind: "windows", UpdatedAt: time.Now().Unix(), Reason: "上游未返回可识别的套餐窗口"}
	switch c.ID {
	case "zhipu":
		out.Windows, err = zhipuCodingWindows(obj)
	case "minimax":
		out.Windows, err = minimaxCodingWindows(obj)
	}
	if err != nil {
		return nil, providerhttp.Broken(c.Name)
	}
	out.Available = len(out.Windows) > 0
	if out.Available {
		out.Reason = ""
	}
	return json.Marshal(out)
}

// Explicit window units outrank reset order. Unclassified windows stay labeled
// as such; timestamps alone do not prove a five-hour or weekly duration.
func zhipuCodingWindows(obj map[string]json.RawMessage) ([]accounts.QuotaWindow, error) {
	if b := obj["success"]; len(b) > 0 {
		var success bool
		if json.Unmarshal(b, &success) != nil || !success {
			return nil, fmt.Errorf("quota rejected")
		}
	}
	var data struct {
		Limits []map[string]json.RawMessage `json:"limits"`
	}
	if b := obj["data"]; len(b) > 0 && json.Unmarshal(b, &data) != nil {
		return nil, fmt.Errorf("invalid quota")
	}
	tokens := false
	for _, row := range data.Limits {
		if providerhttp.Field(row, "type") == "TOKENS_LIMIT" {
			tokens = true
		}
	}
	out := []accounts.QuotaWindow{}
	for i, row := range data.Limits {
		typ := providerhttp.Field(row, "type")
		if typ != "TOKENS_LIMIT" && (typ != "CREDIT_LIMIT" || tokens) {
			continue
		}
		used, err := accounts.QuotaNumber(row, "percentage")
		if err != nil {
			return nil, err
		}
		if used == nil {
			continue
		}
		unit := int64(0)
		_ = json.Unmarshal(row["unit"], &unit)
		w := accounts.QuotaWindow{ID: fmt.Sprintf("quota-%d", i), Name: "套餐窗口", Utilization: used, ResetsAt: quotaReset(row["nextResetTime"])}
		switch unit {
		case 3:
			w.Name = "5小时窗口"
			w.WindowSeconds = 18000
		case 6:
			w.Name = "7天窗口"
			w.WindowSeconds = 604800
		}
		if !w.Complete() {
			return nil, fmt.Errorf("invalid percentage")
		}
		out = append(out, w)
	}
	return out, nil
}

func minimaxCodingWindows(obj map[string]json.RawMessage) ([]accounts.QuotaWindow, error) {
	if b := obj["base_resp"]; len(b) > 0 {
		var base struct {
			Code json.RawMessage `json:"status_code"`
		}
		if json.Unmarshal(b, &base) != nil {
			return nil, fmt.Errorf("invalid status")
		}
		if len(base.Code) > 0 && string(base.Code) != "0" {
			return nil, fmt.Errorf("quota rejected")
		}
	}
	var rows []map[string]json.RawMessage
	if b := obj["model_remains"]; len(b) > 0 && json.Unmarshal(b, &rows) != nil {
		return nil, fmt.Errorf("invalid remains")
	}
	out := []accounts.QuotaWindow{}
	for _, row := range rows {
		if !strings.EqualFold(providerhttp.Field(row, "model_name"), "general") {
			continue
		}
		for _, spec := range []struct {
			id, name, remaining, reset string
			seconds                    int64
		}{
			{"5h", "5小时窗口", "current_interval_remaining_percent", "end_time", 18000},
			{"weekly", "7天窗口", "current_weekly_remaining_percent", "weekly_end_time", 604800},
		} {
			if spec.id == "weekly" && string(row["current_weekly_status"]) != "1" {
				continue
			}
			remaining, err := accounts.QuotaNumber(row, spec.remaining)
			if err != nil {
				return nil, err
			}
			if remaining == nil {
				continue
			}
			if *remaining < 0 || *remaining > 100 {
				return nil, fmt.Errorf("invalid percentage")
			}
			used := 100 - *remaining
			w := accounts.QuotaWindow{ID: spec.id, Name: spec.name, Utilization: &used, ResetsAt: quotaReset(row[spec.reset]), WindowSeconds: spec.seconds}
			if !w.Complete() {
				return nil, fmt.Errorf("invalid window")
			}
			out = append(out, w)
		}
		break
	}
	return out, nil
}

func quotaReset(b json.RawMessage) int64 {
	if len(b) == 0 {
		return 0
	}
	s := string(b)
	if b[0] == '"' {
		if json.Unmarshal(b, &s) != nil {
			return 0
		}
	}
	if at, err := time.Parse(time.RFC3339, s); err == nil && at.Unix() > 0 {
		return at.Unix()
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	if n >= 1000000000000 {
		n /= 1000
	}
	// Malformed far-future values are unknown, not invented resets.
	if n > 253402300799 {
		return 0
	}
	return n
}
