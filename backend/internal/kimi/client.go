// Package kimi connects the official domestic pay-as-you-go API.
package kimi

import (
	"codearts2api/internal/cnapi"
)

// Endpoints follow the pinned Sub2API profiles; MiniMax uses its current official domain.
// Provenance and differences: ../cnapi/UPSTREAM.md.
func New() *cnapi.Client {
	c := cnapi.New("kimi", "Kimi", "https://api.moonshot.cn/v1", "https://api.moonshot.cn/v1/users/me/balance", nil)
	c.Coding = cnapi.New("kimi", "Kimi Coding", "https://api.kimi.com/coding/v1", "", nil)
	c.Coding.Scope = "coding-cn"
	c.Coding.Mode = "coding"
	return c
}
