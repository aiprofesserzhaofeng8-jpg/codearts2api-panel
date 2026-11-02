package server

import (
	"codearts2api/internal/access"
	"net/http"
)

func (s *Server) registerConsumerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/api/consumer/wallet", s.withAdminAuth(s.handleAdminWallet))
	mux.HandleFunc("/admin/api/consumer/plans", s.withAdminAuth(s.handleAdminPlans))
	mux.HandleFunc("/admin/api/consumer/subscription", s.withAdminAuth(s.handleAdminSubscription))
	mux.HandleFunc("/admin/api/consumer/codes", s.withAdminAuth(s.handleAdminCodes))
	mux.HandleFunc("/admin/api/consumer/identity", s.withAdminAuth(s.handleAdminIdentitySettings))
	mux.HandleFunc("/user/api/entitlements", s.withUserAuth(s.handleUserEntitlements))
	mux.HandleFunc("/user/api/redeem", s.withUserAuth(s.handleUserRedeem))
	mux.HandleFunc("/user/api/identity", s.withUserAuth(s.handleUserIdentity))
	mux.HandleFunc("/auth/options", s.handleIdentityOptions)
	mux.HandleFunc("/auth/recovery", s.handlePasswordRecovery)
	mux.HandleFunc("/auth/oauth/start", s.handleOAuthStart)
	mux.HandleFunc("/auth/oauth/callback", s.handleOAuthCallback)
}
func consumerMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		writeErr(w, 405, "不支持的请求方法", "invalid_request_error")
		return false
	}
	return true
}
func consumerResult(w http.ResponseWriter, err error) {
	if err != nil {
		writeErr(w, 400, err.Error(), "invalid_request_error")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) handleAdminWallet(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		id := r.URL.Query().Get("user_id")
		writeJSON(w, 200, map[string]any{"entries": s.Store.UserLedger(id)})
		return
	}
	if !consumerMethod(w, r, "POST") {
		return
	}
	var b struct {
		UserID           string `json:"user_id"`
		ExpectedRevision uint64 `json:"expected_revision"`
		Amount           string `json:"amount"`
		Enabled          bool   `json:"enabled"`
		ClearReview      bool   `json:"clear_review"`
		Note             string `json:"note"`
	}
	if !decodeUserBody(w, r, &b) {
		return
	}
	delta, err := access.ParseCredits(b.Amount)
	if err != nil {
		consumerResult(w, err)
		return
	}
	consumerResult(w, s.Store.AdjustWallet(b.UserID, b.ExpectedRevision, delta, b.Enabled, b.ClearReview, b.Note))
}
func (s *Server) handleAdminPlans(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		writeJSON(w, 200, map[string]any{"plans": s.Store.Plans()})
		return
	}
	if !consumerMethod(w, r, "POST") {
		return
	}
	var p access.Plan
	if !decodeUserBody(w, r, &p) {
		return
	}
	result, err := s.Store.SavePlan(p)
	if err != nil {
		consumerResult(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "plan": result})
}
func (s *Server) handleAdminSubscription(w http.ResponseWriter, r *http.Request) {
	if !consumerMethod(w, r, "POST") {
		return
	}
	var b struct {
		UserID string `json:"user_id"`
		PlanID string `json:"plan_id"`
		Action string `json:"action"`
		Days   int    `json:"days"`
	}
	if !decodeUserBody(w, r, &b) {
		return
	}
	consumerResult(w, s.Store.ManageSubscription(b.UserID, b.PlanID, b.Action, b.Days))
}
func (s *Server) handleAdminCodes(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		writeJSON(w, 200, map[string]any{"codes": s.Store.Redemptions()})
		return
	}
	if !consumerMethod(w, r, "POST") {
		return
	}
	var b struct {
		Action   string            `json:"action"`
		ID       string            `json:"id"`
		Count    int               `json:"count"`
		Template access.Redemption `json:"template"`
	}
	if !decodeUserBody(w, r, &b) {
		return
	}
	if b.Action == "disable" {
		consumerResult(w, s.Store.DisableRedemption(b.ID))
		return
	}
	if b.Action != "generate" {
		writeErr(w, 400, "不支持的兑换码操作", "invalid_request_error")
		return
	}
	codes, err := s.Store.GenerateRedemptions(b.Template, b.Count)
	if err != nil {
		consumerResult(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "codes": codes})
}
func (s *Server) handleUserEntitlements(w http.ResponseWriter, r *http.Request) {
	if !consumerMethod(w, r, "GET") {
		return
	}
	u := currentUser(r)
	view, _, err := s.Store.UserProfile(u.ID)
	if err != nil {
		consumerResult(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"wallet": view.Wallet, "subscription": s.Store.UserSubscription(u.ID), "reserved": s.Store.UserReservedCredits(u.ID), "entries": s.Store.UserLedger(u.ID), "redemptions": s.Store.UserRedemptions(u.ID), "retention_limit": 10000})
}
func (s *Server) handleUserRedeem(w http.ResponseWriter, r *http.Request) {
	if !consumerMethod(w, r, "POST") {
		return
	}
	u := currentUser(r)
	if s.consumerFailures.blocked(u.ID) {
		writeErr(w, 429, "兑换尝试过多，请稍后再试", "rate_limit_error")
		return
	}
	var b struct {
		Code string `json:"code"`
	}
	if !decodeUserBody(w, r, &b) {
		return
	}
	err := s.Store.Redeem(u.ID, b.Code)
	if err != nil {
		s.consumerFailures.failure(u.ID)
	} else {
		s.consumerFailures.success(u.ID)
	}
	consumerResult(w, err)
}
