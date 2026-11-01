package provider

import "strings"

// ModelNotFound 表示请求的模型名既不在上游清单里，也不是已知别名。
//
// 免费权益通道下线后每个模型都真实扣积分，因此**不能**像以前那样
// 静默回落到某个默认模型 —— 猜错模型等于替用户乱花钱。
type ModelNotFound struct {
	// Name 是客户端请求的模型名（可能为空：请求体里没写 model）。
	Name string
	// Available 是当前可用的模型 ID 列表，用于拼出可操作的提示。
	Available []string
}

// Error 实现 error。
func (e *ModelNotFound) Error() string {
	if e.Name == "" {
		return "请求未指定 model，且没有配置默认模型"
	}
	msg := "模型 " + e.Name + " 不存在"
	if len(e.Available) > 0 {
		msg += "；可用模型：" + strings.Join(e.Available, ", ")
	}
	return msg
}
