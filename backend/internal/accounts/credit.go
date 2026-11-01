package accounts

// CreditCategory 是积分的一个来源分类（基础 / 按需 / 赠送）。
type CreditCategory struct {
	Total    float64 `json:"total"`
	Used     float64 `json:"used"`
	Expiring float64 `json:"expiring"`
}

// CreditInfo 是一个账号的积分额度快照。
//
// Available=false 仅表示响应缺少可用的积分指标，不足以证明没有套餐或没有余额。
// 查询失败另以 error 返回；前端应展示原因，不把未知额度当作零余额。
type CreditInfo struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`

	// Total / Used / Remaining 是积分数量；Percent 是上游给的百分比（0–100）。
	Total     float64 `json:"total"`
	Used      float64 `json:"used"`
	Remaining float64 `json:"remaining"`
	Percent   float64 `json:"percent"`
	Expiring  float64 `json:"expiring"`

	ResetDate   string `json:"reset_date,omitempty"`
	Status      string `json:"status,omitempty"` // active / expired / frozen / unknown
	PackageName string `json:"package_name,omitempty"`
	SpecCode    string `json:"spec_code,omitempty"`
	PackageURL  string `json:"package_url,omitempty"`

	IsCreditPackage bool `json:"is_credit_package"`
	IsTokenPackage  bool `json:"is_token_package"`

	Basic    *CreditCategory `json:"basic,omitempty"`
	OnDemand *CreditCategory `json:"on_demand,omitempty"`
	Bonus    *CreditCategory `json:"bonus,omitempty"`

	// TokenUsed / TokenLimit 是 token 套餐的用量（积分套餐通常为 0）。
	TokenUsed  int64 `json:"token_used,omitempty"`
	TokenLimit int64 `json:"token_limit,omitempty"`
}
