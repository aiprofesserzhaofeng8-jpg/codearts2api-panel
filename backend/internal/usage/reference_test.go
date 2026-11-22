package usage

import "testing"

func TestReferencePricesUsePhysicalScopeAndUSDPerMillion(t *testing.T) {
	for _, tc := range []struct {
		id                    string
		input, output, cached float64
	}{
		{"opencode/go/kimi-k3", 3, 15, .3}, {"opencode/go/qwen3.8-flash", .15, .47, .016}, {"deepseek/cn/deepseek-chat", .28, .42, .028}, {"deepseek-v4.1-flash", .30, 1.2, .006},
	} {
		p, ok := ReferencePrice(tc.id)
		if !ok || p.Currency != "USD" || p.Input != tc.input || p.Output != tc.output || p.Cached != tc.cached {
			t.Errorf("%s: %+v", tc.id, p)
		}
	}
	for _, id := range []string{"chatgpt/global/gpt-6.1-sol", "gpt-6.1-sol", "opencode/zen/qwen3.8-flash", "trae/cn/kimi-k3", "opencode/go/KIMI-K3"} {
		if _, ok := ReferencePrice(id); ok {
			t.Errorf("unexpected reference for %s", id)
		}
	}
}
