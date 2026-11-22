package server

import (
	"codearts2api/internal/usage"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type priceInput struct {
	Currency string   `json:"currency"`
	Input    *float64 `json:"input"`
	Output   *float64 `json:"output"`
	Cached   *float64 `json:"cached"`
	Source   string   `json:"source"`
}

func (p priceInput) value() (usage.ModelPrice, error) {
	if p.Input == nil || p.Output == nil || p.Cached == nil {
		return usage.ModelPrice{}, errors.New("模型和三项单价均为必填，0 表示免费")
	}
	return usage.ModelPrice{Currency: p.Currency, Input: *p.Input, Output: *p.Output, Cached: *p.Cached, Source: p.Source}, nil
}
func (s *Server) pricingResponse(saved bool) map[string]any {
	prices := s.Pricing.Prices()
	for _, m := range s.Reg.List(nil) {
		if _, ok := prices[m.ID]; ok {
			continue
		}
		if p, ok := usage.ReferencePrice(m.ID); ok {
			prices[m.ID] = p
		}
	}
	result := map[string]any{"prices": prices}
	if p := s.Pricing.DefaultTokenPrice(); p != nil {
		result["default_price"] = p
	}
	if saved {
		result["ok"] = true
	}
	return result
}
func (s *Server) handleAdminPricing(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, s.pricingResponse(false))
	case http.MethodPost:
		var body struct {
			Model string     `json:"model"`
			Price priceInput `json:"price"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); err != nil {
			writeErr(w, 400, "价格格式错误", "invalid_request_error")
			return
		}
		model := strings.TrimSpace(body.Model)
		p, err := body.Price.value()
		if model == "" || err != nil {
			writeErr(w, 400, "模型和三项单价均为必填，0 表示免费", "invalid_request_error")
			return
		}
		s.writePricingResult(w, s.Pricing.SetPrice(model, p))
	default:
		writeErr(w, 405, "仅支持 GET/POST", "invalid_request_error")
	}
}
func (s *Server) handleAdminDefaultPricing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		writeErr(w, 405, "仅支持 PUT/DELETE", "invalid_request_error")
		return
	}
	var price *usage.ModelPrice
	if r.Method == http.MethodPut {
		var input priceInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input); err != nil {
			writeErr(w, 400, "价格格式错误", "invalid_request_error")
			return
		}
		p, err := input.value()
		if err != nil {
			writeErr(w, 400, err.Error(), "invalid_request_error")
			return
		}
		price = &p
	}
	s.writePricingResult(w, s.Pricing.SetDefaultPrice(price))
}
func (s *Server) writePricingResult(w http.ResponseWriter, err error) {
	if errors.Is(err, usage.ErrPriceSave) {
		writeErr(w, 500, "保存价格失败", "server_error")
		return
	}
	if err != nil {
		writeErr(w, 400, err.Error(), "invalid_request_error")
		return
	}
	writeJSON(w, 200, s.pricingResponse(true))
}
