package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/providerhttp"
)

// The read-only quota endpoint and bucket fields follow Google's Gemini CLI
// code_assist/server.ts and types.ts; see UPSTREAM.md. No model entitlement or
// dollar balance is inferred from a quota bucket.
func (c *Client) Credit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	if !isCLI(raw) {
		return c.Client.Credit(ctx, raw)
	}
	b, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	var a cliCredentials
	_ = json.Unmarshal(b, &a)
	obj, err := c.cliCall(ctx, a, "POST", "/v1internal:retrieveUserQuota", map[string]string{"project": a.Project, "userAgent": GeminiCLIUserAgent})
	if err != nil {
		return nil, err
	}
	out := accounts.CreditInfo{Kind: "windows", UpdatedAt: time.Now().Unix(), Reason: "上游未返回可用配额"}
	var buckets []map[string]json.RawMessage
	if v := obj["buckets"]; len(v) == 0 || string(v) == "null" {
		return json.Marshal(out)
	} else if json.Unmarshal(v, &buckets) != nil {
		return nil, providerhttp.Broken("Gemini")
	}
	if len(buckets) > 1024 {
		return nil, providerhttp.Broken("Gemini")
	}
	for i, bucket := range buckets {
		amount, err := accounts.QuotaNumber(bucket, "remainingAmount")
		if err != nil || amount != nil && *amount < 0 {
			return nil, providerhttp.Broken("Gemini")
		}
		fraction, err := accounts.QuotaNumber(bucket, "remainingFraction")
		if err != nil || fraction != nil && (*fraction < 0 || *fraction > 1) {
			return nil, providerhttp.Broken("Gemini")
		}
		if amount == nil && fraction == nil {
			continue
		}
		name := providerhttp.Field(bucket, "modelId")
		if name == "" {
			name = "Code Assist"
		}
		unit := providerhttp.Field(bucket, "tokenType")
		if unit == "" {
			unit = "quota"
		}
		window := accounts.QuotaWindow{ID: fmt.Sprintf("cli-%d", i), Name: name, Unit: unit, Remaining: amount}
		if fraction != nil {
			used := (1 - *fraction) * 100
			window.Utilization = &used
		}
		if reset := providerhttp.Field(bucket, "resetTime"); reset != "" {
			at, err := time.Parse(time.RFC3339, reset)
			if err != nil {
				return nil, providerhttp.Broken("Gemini")
			}
			window.ResetsAt = at.Unix()
		}
		if !window.Complete() {
			return nil, providerhttp.Broken("Gemini")
		}
		out.Windows = append(out.Windows, window)
	}
	out.Available = len(out.Windows) > 0
	if out.Available {
		out.Reason = ""
	}
	return json.Marshal(out)
}
