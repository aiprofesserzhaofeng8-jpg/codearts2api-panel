package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/catalog"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
)

// RefreshModels borrows the existing credential and platform operations.
func (s *Server) RefreshModels(ctx context.Context) error {
	return s.Catalog.Refresh(ctx, s.catalogRefreshOperations())
}
func (s *Server) catalogRefreshOperations() catalog.RefreshOperations {
	return catalog.RefreshOperations{
		Native: func(ctx context.Context, a *accounts.Account) ([]provider.Model, error) {
			return s.CodeArts.Models(ctx, s.Cred, a, s.resolveOptions())
		},
		Provider: func(ctx context.Context, a *accounts.Account) ([]provider.Model, error) {
			adapter, raw, err := s.providerCredentials(ctx, a)
			if err != nil {
				return nil, err
			}
			return adapter.Models(ctx, raw)
		},
	}
}
func (s *Server) refreshModelsIfStale(maxAge time.Duration) {
	s.Catalog.RefreshIfStale(maxAge, s.catalogRefreshOperations())
}

// handleModels 实现 GET /v1/models。
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	s.refreshModelsIfStale(10 * time.Minute)

	settings := s.Store.Settings()
	models, err := s.visibleModels(r.Context())
	if err != nil {
		writeErr(w, 403, err.Error(), "permission_error")
		return
	}
	now := time.Now().Unix()

	data := make([]map[string]any, 0, len(models))
	for _, m := range models {
		var recommendation map[string]any
		resolved := m
		if m.Source != "mapping" {
			resolved, _ = s.Reg.Resolve(m.ID, settings.ModelAliases)
		}
		if p, ok := s.requestModelPolicy(r, resolved); ok {
			recommendation = profileFromPolicy(resolved.ID, p)
		}
		data = append(data, map[string]any{
			"client_profile": recommendation,
			"id":             m.ID,
			"object":         "model",
			"created":        now,
			"owned_by":       modelProvider(m),
			"provider":       modelProvider(m),
			"scope":          m.Scope,
			"codearts": map[string]any{
				"name":       m.Name,
				"context":    m.Context,
				"max_output": m.MaxOutput,
				"source":     m.Source,
				// 免费权益通道已下线：所有模型都走积分通道、真实扣费。
				"channel":     "credit",
				"billable":    true,
				"credit":      m.Credit,
				"description": m.Desc,
			},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

// handleAdminRefreshModels 强制刷新模型清单（控制台按钮）。
func (s *Server) handleAdminRefreshModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST", "invalid_request_error")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := s.RefreshModels(ctx); err != nil {
		writeErr(w, http.StatusBadGateway, "刷新失败: "+err.Error(), "upstream_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"models":          s.Reg.List(s.Store.Settings().ModelAliases),
		"provider_errors": s.providerModelErrors(),
	})
}

// upstreamErrorPayload 尝试从上游错误响应里提取可读信息。
func upstreamErrorPayload(raw []byte) string {
	var probe struct {
		ErrorCode string `json:"error_code"`
		ErrorMsg  string `json:"error_msg"`
		Error     struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &probe); err == nil {
		if probe.ErrorCode != "" {
			return probe.ErrorCode + ": " + probe.ErrorMsg
		}
		if probe.Error.Message != "" {
			return probe.Error.Message
		}
	}
	return truncateStr(string(raw), 300)
}

// readAll 读取响应体（带大小上限）。
func readAll(rc io.ReadCloser, limit int64) ([]byte, error) {
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, limit))
}

// truncateStr 截断长字符串。
func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func modelProvider(m registry.Model) string {
	if m.Provider == "" {
		return provider.CodeArts
	}
	return m.Provider
}

func (s *Server) providerModelErrors() map[string]string {
	return s.Catalog.ProviderErrors()
}
