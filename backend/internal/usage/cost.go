package usage

type RequestCost struct {
	Partial  bool       `json:"partial,omitempty"`
	Currency string     `json:"currency"`
	Amount   float64    `json:"amount"`
	Price    ModelPrice `json:"price"`
}

func DefaultPrices() map[string]ModelPrice {
	return map[string]ModelPrice{"deepseek-v4.1-flash": {Currency: "USD", Input: 0.30, Output: 1.20, Cached: 0.006,
		Source: "DeepSeek 官方高峰参考价（固定，不自动切换时段） https://api-docs.deepseek.com/quick_start/pricing/", UpdatedAt: "2026-10-26"}}
}
func EstimateCost(u TokenUsage, p ModelPrice) *RequestCost {
	if !u.Known || p.Validate() != nil {
		return nil
	}
	cached := min(max(0, u.CachedTokens), max(0, u.InputTokens))
	amount := (float64(max(0, u.InputTokens)-cached)*p.Input + float64(cached)*p.Cached + float64(max(0, u.OutputTokens))*p.Output) / 1e6
	return &RequestCost{Currency: p.Currency, Amount: amount, Price: p}
}
