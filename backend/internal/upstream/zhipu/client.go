// Package zhipu connects the official domestic pay-as-you-go API.
package zhipu

import (
	"codearts2api/internal/cnapi"
	"codearts2api/internal/provider"
)

// Endpoints follow the pinned Sub2API profiles; MiniMax uses its current official domain.
// Provenance and differences: ../cnapi/UPSTREAM.md.
func New() *cnapi.Client {
	c := cnapi.New("zhipu", "智谱", "https://open.bigmodel.cn/api/paas/v4", "", []provider.Model{{ID: "glm-5.3", Name: "GLM-5.3", ImageInput: "unsupported", ToolCalling: "supported", Source: "https://docs.bigmodel.cn/cn/guide/start/model-overview"}, {ID: "glm-5.3-flash", Name: "GLM-5.3-Flash", ImageInput: "supported", ToolCalling: "supported", Source: "https://docs.bigmodel.cn/cn/guide/start/model-overview"}})
	c.Coding = cnapi.New("zhipu", "智谱 Coding", "https://open.bigmodel.cn/api/coding/paas/v4", "https://open.bigmodel.cn/api/monitor/usage/quota/limit", c.Fallback)
	c.Coding.Scope = "coding-cn"
	c.Coding.Mode = "coding"
	return c
}
