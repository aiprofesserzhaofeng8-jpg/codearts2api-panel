package mobileclaw

import (
	"context"
	"encoding/json"
	"strings"
)

// freeProvider is only used while completing login, never on the chat path.
func (c *Client) freeProvider(ctx context.Context, raw json.RawMessage) (string, error) {
	var subscriptions []struct {
		Provider string `json:"provider"`
		Packages []struct {
			Type      string `json:"providerType"`
			Available bool   `json:"available"`
		} `json:"packageList"`
	}
	if err := c.read(ctx, raw, "/desktop/manager/subscriptions/getAiProviders", &subscriptions); err != nil {
		return "", err
	}
	selected := ""
	for _, s := range subscriptions {
		for _, p := range s.Packages {
			if !p.Available || p.Type != "FREE" {
				continue
			}
			if s.Provider == "" || len(s.Provider) > 128 || strings.ContainsAny(s.Provider, "\r\n") {
				return "", invalid("免费套餐供应商信息无效")
			}
			if selected != "" && selected != s.Provider {
				return "", invalid("存在多个免费套餐供应商，请在官方客户端选择后导入")
			}
			selected = s.Provider
		}
	}
	if selected == "" {
		return "", invalid("账号没有可用的个人免费套餐")
	}
	return selected, nil
}
