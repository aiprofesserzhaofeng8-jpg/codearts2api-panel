package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/apierr"
	"codearts2api/internal/catalog"
	"codearts2api/internal/codearts"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	codeartsup "codearts2api/internal/upstream/codearts"
)

// handleAdminState 返回控制台首屏所需的全部状态。
func (s *Server) handleAdminState(w http.ResponseWriter, r *http.Request) {
	today := time.Now().Format("2006-01-02")
	accts := s.Store.Accounts()
	poolState := s.Store.PoolSnapshot()
	modelHealth := s.Store.ModelHealthSnapshot()
	safe := make([]map[string]any, 0, len(accts))
	for _, a := range accts {
		row := map[string]any{
			"model_health": modelHealth[a.ID],
			"provider":     a.ProviderID(), "scope": a.Scope,
			"paused": a.Paused, "pool": poolState[a.ID],
			"max_in_flight":  a.MaxInFlight,
			"cooldown_until": a.CooldownUntil, "cooldown_reason": a.CooldownReason,
			"id": a.ID, "name": a.DisplayName(), "source": a.Source,
			"user_data": a.UserData, "enabled": a.Enabled,
			"status": a.Status, "note": a.Note, "last_err": a.LastErr,
			"failures": a.Failures, "last_used": a.LastUsed,
			"last_claim_date": a.LastClaimDate, "last_claim_at": a.LastClaimAt,
			"last_claim_err":        a.LastClaimErr,
			"last_claim_msg":        a.LastClaimMsg,
			"claimed_today":         a.LastClaimDate == claimDateFor(a, time.Now()),
			"daily_claim_supported": s.dailyRewardsSupported(a), "last_claim_state": a.LastClaimState, "next_claim_at": a.NextClaimAt,
		}
		if session := codearts.StoredSession(a); session != nil {
			row["access_key"] = codearts.Mask(session.AccessKey)
			row["expires_at"] = session.ExpiresAt.Unix()
			row["expires_in"] = int(session.ExpiresIn().Seconds())
			row["has_refresh"] = session.HasRefresh()
			row["domain_id"] = codearts.Mask(session.DomainID)
			// 永久 AK/SK 没有过期时间，控制台据此显示「永不过期」而不是 1970。
			row["permanent"] = session.IsPermanent()
		}
		if a.ProviderID() != provider.CodeArts && s.Providers != nil {
			if adapter, ok := s.Providers.Get(a.ProviderID()); ok {
				if raw, identity, err := adapter.Normalize(a.Credentials); err == nil {
					if growth, ok := adapter.(provider.GrowthTasks); ok {
						row["growth_tasks_supported"] = growth.SupportsGrowthTasks(raw)
					}
					row["expires_at"] = identity.ExpiresAt
					row["expires_in"] = identity.ExpiresAt - time.Now().Unix()
					row["has_refresh"] = identity.HasRefresh
					row["access_key"] = "已保存"
				}
			}
		}
		safe = append(safe, row)
	}

	keys := s.Store.Keys()
	safeKeys := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		safeKeys = append(safeKeys, map[string]any{
			"ref": keyReference(k.Key), "key": codearts.Mask(k.Key), "name": k.Name, "enabled": k.Enabled,
			"group_id": k.GroupID, "user_id": k.UserID,
			"created_at": k.CreatedAt, "last_used": k.LastUsed, "requests": k.Requests, "budget": k.Budget, "budget_usage": k.BudgetUsage,
		})
	}

	cacheErr := s.Catalog.Error()

	writeJSON(w, http.StatusOK, map[string]any{
		"today":           today,
		"accounts":        safe,
		"groups":          s.Store.Groups(),
		"providers":       s.providerDescriptors(),
		"provider_errors": s.providerModelErrors(),
		"keys":            safeKeys,
		"models":          s.Reg.List(s.Store.Settings().ModelAliases),
		"settings":        sanitizedSettings(s.Store.Settings()),
		"stats":           s.Store.Stats(),
		"upstream_error":  cacheErr,
	})
}

// handleAdminAccounts 处理账号池操作。
func (s *Server) handleAdminAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		Action        string `json:"action"`
		ID            string `json:"id"`
		Dir           string `json:"dir"`
		Name          string `json:"name"`
		AccessKey     string `json:"access_key"`
		SecretKey     string `json:"secret_key"`
		SecurityToken string `json:"security_token"`
		SessionJSON   string `json:"session_json"`
		ExpiresAt     int64  `json:"expires_at"`
		MaxInFlight   *int   `json:"max_in_flight"`
		// CredentialKind 显式声明凭据形态：留空或 "sts" 走临时凭据，
		// "aksk" 表示 IAM 永久 AK/SK（不过期、不续期、不带 security_token）。
		CredentialKind string `json:"credential_kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败", "invalid_request_error")
		return
	}

	switch body.Action {
	case "concurrency":
		if err := s.AccountManagement.SetConcurrency(body.ID, body.MaxInFlight); err != nil {
			writeAccountManagementError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "id": body.ID, "max_in_flight": *body.MaxInFlight, "pool": s.Store.PoolSnapshot()[body.ID]})
	case "import":
		dir := body.Dir
		if dir == "" {
			dir = s.Store.Settings().UserDataDir
		}
		results, err := codeartsup.ImportDesktop(dir)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "导入失败: "+err.Error(), "invalid_request_error")
			return
		}
		list := make([]*accounts.Account, 0, len(results))
		for _, result := range results {
			list = append(list, result.Account)
		}
		created := s.AccountManagement.Import(list)
		imported := make([]map[string]any, 0, len(results))
		for i, result := range results {
			imported = append(imported, map[string]any{
				"id": result.Account.ID, "name": result.Account.Name, "dir": result.Directory,
				"access_key": result.KeyHint, "expires_at": result.ExpiresAt, "new": created[i],
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(imported), "imported": imported})
	case "manual":
		acct, permanent, err := codeartsup.ManualAccount(codeartsup.ManualAccountOptions{
			Name: body.Name, AccessKey: body.AccessKey, SecretKey: body.SecretKey,
			SecurityToken: body.SecurityToken, CredentialKind: body.CredentialKind, ExpiresAt: unixTime(body.ExpiresAt),
		})
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
			return
		}
		isNew := s.AccountManagement.Add(acct)
		response := map[string]any{"ok": true, "new": isNew, "id": acct.ID}
		if permanent {
			response["credential_kind"] = "aksk"
		}
		writeJSON(w, http.StatusOK, response)
	case "session-json":
		acct, err := codeartsup.AccountFromSessionJSON(body.Name, body.SessionJSON)
		if err != nil {
			writeAccountManagementError(w, err)
			return
		}
		isNew := s.AccountManagement.Add(acct)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "new": isNew, "id": acct.ID})
	case "remove":
		if err := s.AccountManagement.Remove(body.ID); err != nil {
			writeAccountManagementError(w, err)
			return
		}
		s.Credits.Invalidate(body.ID)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "removed_id": body.ID})
	case "toggle":
		if err := s.AccountManagement.Toggle(body.ID); err != nil {
			writeAccountManagementError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "clear-errors":
		s.AccountManagement.ClearErrors(body.ID)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "pause", "resume":
		if err := s.AccountManagement.SetPaused(body.ID, body.Action == "pause"); err != nil {
			writeAccountManagementError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "export":
		result, err := s.AccountManagement.Export(r.Context(), body.ID)
		if err != nil {
			writeAccountManagementError(w, err)
			return
		}
		if !result.Native {
			writeJSON(w, 200, map[string]any{"ok": true, "name": result.Name, "session_json": result.JSON, "warning": "此内容含完整上游凭据，请勿公开"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "name": result.Name, "session_json": result.JSON,
			"expires_at": result.ExpiresAt, "has_refresh": result.HasRefresh,
			"warning": "该内容等同于账号登录态（含 refresh_token 与 DPoP 私钥），请勿外传或提交到版本库",
		})
	case "reimport":
		if err := s.AccountManagement.Reimport(r.Context(), body.ID); err != nil {
			writeAccountManagementError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})

	default:
		writeErr(w, http.StatusBadRequest, "未知操作: "+body.Action, "invalid_request_error")
	}
}

// writeAccountManagementError maps business semantics to the existing wire errors.
func writeAccountManagementError(w http.ResponseWriter, err error) {
	failure, ok := err.(*accounts.ManagementError)
	if !ok {
		writeErr(w, 500, err.Error(), "server_error")
		return
	}
	status, kind := http.StatusBadRequest, "invalid_request_error"
	switch failure.Kind {
	case accounts.ManagementMissing:
		status = http.StatusNotFound
	case accounts.ManagementInternal:
		status, kind = http.StatusInternalServerError, "server_error"
	case accounts.ManagementUpstream:
		status, kind = http.StatusBadGateway, "upstream_error"
	}
	writeErr(w, status, failure.Message, kind)
}

// handleAdminKeys 处理密钥的增删改查。
func (s *Server) handleAdminKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		keys := s.AccessManagement.Keys()
		safe := make([]map[string]any, 0, len(keys))
		for _, k := range keys {
			safe = append(safe, map[string]any{
				"ref": keyReference(k.Key), "key": codearts.Mask(k.Key), "name": k.Name, "enabled": k.Enabled,
				"group_id": k.GroupID, "user_id": k.UserID,
				"created_at": k.CreatedAt, "last_used": k.LastUsed, "requests": k.Requests, "budget": k.Budget, "budget_usage": k.BudgetUsage,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"keys": safe})
	case http.MethodPost:
		var body struct {
			Name    string `json:"name"`
			GroupID string `json:"group_id"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			writeErr(w, 400, "密钥参数无效", "invalid_request_error")
			return
		}
		k, err := s.AccessManagement.AddKey(body.Name, body.GroupID)
		if err != nil {
			writeErr(w, 400, err.Error(), "invalid_request_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "key": k.Key})
	case http.MethodDelete:
		q := r.URL.Query()
		s.AccessManagement.ChangeKeys(q.Get("key"), q.Get("toggle"))
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "不支持的方法", "invalid_request_error")
	}
}

// handleAdminLogs 返回或清空请求日志。
func (s *Server) handleAdminLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		s.Store.ClearLogs()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": s.Store.Logs(intQuery(r, "limit"))})
}

// handleAdminStats 返回或重置统计。
func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		s.Store.ClearStats()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeJSON(w, http.StatusOK, s.Store.Stats())
}

// handleAdminSettings 读取或更新设置。
func (s *Server) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusOK, sanitizedSettings(s.Store.Settings()))
		return
	}
	var body config.SettingsUpdate
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败", "invalid_request_error")
		return
	}
	if err := s.Configuration.UpdateSettings(body, s.AllowLocalAdmin); err != nil {
		writeErr(w, 400, err.Error(), "invalid_request_error")
		return
	}

	// 身份头变更后重建上游客户端
	s.ApplyIdentity()
	// 限流参数变更后立即生效（不用重启）
	s.ApplyRateLimit()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "settings": sanitizedSettings(s.Store.Settings())})
}

// ApplyIdentity 把设置里的身份头同步到上游客户端。
func (s *Server) ApplyIdentity() {
	st := s.Store.Settings()
	id := codearts.DefaultIdentity()
	if st.PluginName != "" {
		id.PluginName = st.PluginName
	}
	if st.PluginVersion != "" {
		id.PluginVersion = st.PluginVersion
	}
	if st.IDEName != "" {
		id.IDEName = st.IDEName
	}
	if st.IDEVersion != "" {
		id.IDEVersion = st.IDEVersion
	}
	if st.Language != "" {
		id.Language = st.Language
	}
	s.Up.SetIdentity(id)
}

// handleAdminCheckin 对全部（或指定）账号做一次连通性探测。
func (s *Server) handleAdminCheckin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	checked := s.AccountProbes.Run(r.Context(), body.ID, accounts.ProbeOperations{
		Invalidate: s.Cred.Invalidate,
		Provider: func(ctx context.Context, a *accounts.Account) ([]provider.Model, error) {
			adapter, raw, err := s.providerCredentials(ctx, a)
			if err != nil {
				return nil, err
			}
			return adapter.Models(ctx, raw)
		},
		Native: func(ctx context.Context, a *accounts.Account) (*accounts.ProbeCredentialInfo, error) {
			return s.CodeArts.ProbeAccount(ctx, s.Cred, a, s.resolveOptions(), func() (string, error) {
				probe, ok := s.defaultProbeModel()
				if !ok {
					return "", &apierr.ModelNotFound{Name: s.Store.Settings().DefaultModel}
				}
				return probe.ID, nil
			})
		},
	})
	results := make([]map[string]any, 0, len(checked))
	for _, result := range checked {
		row := map[string]any{"id": result.ID, "name": result.Name, "ok": result.OK, "err": result.Error}
		if result.Credentials != nil {
			row["expires_at"] = result.Credentials.ExpiresAt
			row["access_key"] = result.Credentials.AccessKey
		}
		results = append(results, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "results": results})
}

// defaultProbeModel 返回探活用的模型。
//
// 探活会消耗极少量积分（max_tokens=8），控制台上已标注。
func (s *Server) defaultProbeModel() (registry.Model, bool) {
	st := s.Store.Settings()
	return s.Reg.Resolve(st.DefaultModel, st.ModelAliases)
}

// handleAdminModelProbe retains the task gate before permissive HTTP decoding.
func (s *Server) handleAdminModelProbe(w http.ResponseWriter, r *http.Request) {
	run, err := s.Catalog.BeginProbe()
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error(), "invalid_request_error")
		return
	}
	defer run.Close()
	var body struct {
		ID     string   `json:"id"`
		Models []string `json:"models"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	result, err := run.Run(r.Context(), body.ID, body.Models, catalog.ProbeOperations{
		Native: func(ctx context.Context, a *accounts.Account, model string) error {
			return s.CodeArts.Probe(ctx, s.Cred, a, s.resolveOptions(), model)
		},
		Provider: s.chatOnce,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "total": result.Total, "available": result.Available,
		"unavailable": result.Total - result.Available, "results": result.Results,
	})
}

func (s *Server) handleAdminCredits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 GET", "invalid_request_error")
		return
	}
	fresh := r.URL.Query().Get("fresh") != ""
	targets := s.Store.Accounts()
	if id := strings.TrimSpace(r.URL.Query().Get("id")); id != "" {
		targets = nil
		if a := s.Store.Account(id); a != nil {
			targets = append(targets, a)
		}
	}
	if len(targets) == 0 {
		writeErr(w, http.StatusBadRequest, apierr.NoAccount.Error(), "invalid_request_error")
		return
	}

	results := s.Credits.Rows(r.Context(), targets, fresh, s.creditOperations())

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "accounts": results})
}

func (s *Server) creditOperations() accounts.CreditOperations {
	return accounts.CreditOperations{
		Native: func(ctx context.Context, a *accounts.Account) (*accounts.CreditInfo, error) {
			return s.CodeArts.Credit(ctx, s.Cred, a, s.resolveOptions())
		},
		Provider: s.providerCredentials,
	}
}
func (s *Server) creditFor(ctx context.Context, a *accounts.Account, fresh bool) (*accounts.CreditInfo, error) {
	return s.Credits.For(ctx, a, fresh, s.creditOperations())
}
func (s *Server) cachedCredit(id string) (*accounts.CreditInfo, error, bool) {
	return s.Credits.Cached(id)
}

// unixTime 把 Unix 秒转换为时间（0 表示使用默认有效期）。
func unixTime(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
