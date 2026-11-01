package server

import (
	"codearts2api/internal/usage"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

func (s *Server) handleAdminPricing(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, map[string]any{"prices": s.Pricing.Prices()})
	case http.MethodPost:
		var body struct {
			Model string `json:"model"`
			Price struct {
				Currency string   `json:"currency"`
				Input    *float64 `json:"input"`
				Output   *float64 `json:"output"`
				Cached   *float64 `json:"cached"`
				Source   string   `json:"source"`
			} `json:"price"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); err != nil {
			writeErr(w, 400, "价格格式错误", "invalid_request_error")
			return
		}
		body.Model = strings.TrimSpace(body.Model)
		p := body.Price
		if body.Model == "" || p.Input == nil || p.Output == nil || p.Cached == nil {
			writeErr(w, 400, "模型和三项单价均为必填，0 表示免费", "invalid_request_error")
			return
		}
		price := usage.ModelPrice{Currency: p.Currency, Input: *p.Input, Output: *p.Output, Cached: *p.Cached, Source: p.Source}
		if err := s.Pricing.SetPrice(body.Model, price); err != nil {
			if errors.Is(err, usage.ErrPriceSave) {
				writeErr(w, 500, "保存价格失败", "server_error")
			} else {
				writeErr(w, 400, err.Error(), "invalid_request_error")
			}
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "prices": s.Pricing.Prices()})
	default:
		writeErr(w, 405, "仅支持 GET/POST", "invalid_request_error")
	}
}
