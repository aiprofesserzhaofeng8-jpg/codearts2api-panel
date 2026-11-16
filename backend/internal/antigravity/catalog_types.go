// SPDX-License-Identifier: LGPL-3.0-only
// Copied from Sub2API; see UPSTREAM.md.
package antigravity

type ModelQuotaInfo struct {
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime,omitempty"`
}

// ModelInfo 模型信息
type ModelInfo struct {
	QuotaInfo          *ModelQuotaInfo `json:"quotaInfo,omitempty"`
	DisplayName        string          `json:"displayName,omitempty"`
	SupportsImages     *bool           `json:"supportsImages,omitempty"`
	SupportsThinking   *bool           `json:"supportsThinking,omitempty"`
	ThinkingBudget     *int            `json:"thinkingBudget,omitempty"`
	Recommended        *bool           `json:"recommended,omitempty"`
	MaxTokens          *int            `json:"maxTokens,omitempty"`
	MaxOutputTokens    *int            `json:"maxOutputTokens,omitempty"`
	SupportedMimeTypes map[string]bool `json:"supportedMimeTypes,omitempty"`
}

// DeprecatedModelInfo 废弃模型转发信息
type DeprecatedModelInfo struct {
	NewModelID string `json:"newModelId"`
}

// FetchAvailableModelsRequest fetchAvailableModels 请求
type FetchAvailableModelsRequest struct {
	Project string `json:"project"`
}

// FetchAvailableModelsResponse fetchAvailableModels 响应
type FetchAvailableModelsResponse struct {
	Models             map[string]ModelInfo           `json:"models"`
	DeprecatedModelIDs map[string]DeprecatedModelInfo `json:"deprecatedModelIds,omitempty"`
}
