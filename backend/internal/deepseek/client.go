// Package deepseek connects the official domestic pay-as-you-go API.
package deepseek

import (
	"codearts2api/internal/cnapi"
)

// Endpoints follow the pinned Sub2API profiles; MiniMax uses its current official domain.
// Provenance and differences: ../cnapi/UPSTREAM.md.
func New() *cnapi.Client {
	return cnapi.New("deepseek", "DeepSeek", "https://api.deepseek.com/v1", "https://api.deepseek.com/user/balance", nil)
}
