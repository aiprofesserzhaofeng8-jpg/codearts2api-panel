// SPDX-License-Identifier: LGPL-3.0-only
// Adapted Sub2API resolveDefaultTierID and Client.OnboardUser; see UPSTREAM.md.
package antigravity

import (
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Called only during explicit browser authorization, never during import,
// directory refresh, token rotation or generation. Follow only an offered
// default tier; absence is not permission to guess free/legacy/paid enrollment.
func (c *Client) onboard(ctx context.Context, a *credentials, load map[string]json.RawMessage) error {
	var tiers []struct {
		ID      string `json:"id"`
		Default bool   `json:"isDefault"`
	}
	if json.Unmarshal(load["allowedTiers"], &tiers) != nil {
		return providerhttp.Invalid("Antigravity", "未提供默认初始化套餐，请在官方客户端完成初始化")
	}
	tier := ""
	for _, t := range tiers {
		if t.Default && strings.TrimSpace(t.ID) != "" {
			tier = strings.TrimSpace(t.ID)
			break
		}
	}
	if tier == "" || !providerhttp.Safe(tier) {
		return providerhttp.Invalid("Antigravity", "未提供默认初始化套餐，未代选其他套餐")
	}
	body := map[string]any{"tierId": tier, "metadata": map[string]string{"ideType": "ANTIGRAVITY", "platform": "PLATFORM_UNSPECIFIED", "pluginType": "GEMINI"}}
	// Match Sub2API's onboarding protocol: resubmit the same tier only when the
	// previous response explicitly says done=false. Transport/business errors
	// stop immediately; they are not evidence that an unknown write can be retried.
	for attempt := 0; attempt < 5; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		obj, err := c.cliCall(ctx, *a, "POST", "/v1internal:onboardUser", body)
		if err != nil {
			return err
		}
		var done bool
		if json.Unmarshal(obj["done"], &done) != nil || string(obj["done"]) == "null" {
			return providerhttp.Broken("Antigravity")
		}
		if done {
			var response map[string]json.RawMessage
			if json.Unmarshal(obj["response"], &response) != nil {
				return providerhttp.Broken("Antigravity")
			}
			project := projectID(response["cloudaicompanionProject"])
			if project == "" || !providerhttp.Safe(project) {
				return providerhttp.Broken("Antigravity")
			}
			if a.Project != "" && a.Project != project {
				return providerhttp.Invalid("Antigravity", "初始化项目与原账号不匹配")
			}
			a.Project = project
			return nil
		}
		if attempt == 4 {
			break
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return &provider.Error{Status: 503, Message: "Antigravity 项目仍在初始化，请稍后重新检查授权"}
}
