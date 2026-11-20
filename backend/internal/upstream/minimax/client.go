// Package minimax connects the official domestic pay-as-you-go API.
package minimax

import (
	"codearts2api/internal/cnapi"
	"codearts2api/internal/provider"
)

// Endpoints follow the pinned Sub2API profiles; MiniMax uses its current official domain.
// Provenance and differences: ../cnapi/UPSTREAM.md.
func New() *cnapi.Client {
	c := cnapi.New("minimax", "MiniMax", "https://api.minimax.cn/v1", "", []provider.Model{{ID: "MiniMax-M3.1-Flash", Name: "MiniMax-M3.1-Flash", ImageInput: "supported", ToolCalling: "supported", Source: "https://platform.minimax.cn/docs/api-reference/text-openai-api"}, {ID: "MiniMax-M3", Name: "MiniMax-M3", ImageInput: "supported", ToolCalling: "supported", Source: "https://platform.minimax.cn/docs/api-reference/text-openai-api"}})
	c.Coding = cnapi.New("minimax", "MiniMax M Plan", "https://api.minimax.cn/v1", "https://www.minimax.cn/v1/token_plan/remains", c.Fallback[:1])
	c.Coding.Scope = "coding-cn"
	c.Coding.Mode = "coding"
	return c
}
