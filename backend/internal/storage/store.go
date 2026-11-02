// Package storage owns durable state and atomic runtime transactions.
package storage

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"codearts2api/internal/access"
	"codearts2api/internal/accounts"
	"codearts2api/internal/codearts"
	"codearts2api/internal/config"
	"codearts2api/internal/scheduler"
	"codearts2api/internal/usage"
)

// APIKey 是网关对外签发的调用密钥。
type APIKey = access.APIKey

// Settings is retained as a compatibility alias for schema 6.
type Settings = config.Settings

// Compatibility aliases retain schema-6 field and Go API identity.
type TokenUsage = usage.TokenUsage
type RequestAttempt = usage.RequestAttempt
type LogEntry = usage.LogEntry
type Stats = usage.Stats

// StateVersion 是状态文件当前的 schema 版本。
//
// 0 表示「加版本号之前的历史文件」。对 State 结构做不兼容改动时 +1，
// 并在 migrate 里补一段从旧版本升级的逻辑。Open 会拒绝读取版本高于
// 本程序的文件 —— 否则新版本写的字段会被旧程序静默丢弃。
const StateVersion = 8

// State 是全部持久化状态。
type State struct {
	Users []access.User `json:"users,omitempty"`
	// UsageSequence is the journal checkpoint included in this snapshot.
	UsageSequence           uint64                            `json:"usage_sequence,omitempty"`
	Groups                  []AccountGroup                    `json:"groups,omitempty"`
	ModelHealth             map[string]map[string]ModelHealth `json:"model_health,omitempty"`
	Audit                   []AuditEvent                      `json:"audit,omitempty"`
	ModelPolicies           map[string]ModelPolicy            `json:"model_policies,omitempty"`
	UsageRecords            []LogEntry                        `json:"usage_records,omitempty"`
	UsageHistoryInitialized bool                              `json:"usage_history_initialized,omitempty"`
	Prices                  map[string]ModelPrice             `json:"model_prices,omitempty"`
	// Version 是 schema 版本（见 StateVersion）。
	Version  int                 `json:"version"`
	Accounts []*accounts.Account `json:"accounts"`
	Keys     []*APIKey           `json:"keys"`
	Settings Settings            `json:"settings"`
	Stats    Stats               `json:"stats"`
	Logs     []LogEntry          `json:"logs"`
}

// Store 是带锁的状态容器。
type Store struct {
	// mu 保护 st 里的字段（含账号指针指向的对象）。
	mu sync.RWMutex
	// saveMu 串行化落盘。
	//
	// 并发 Save 曾共用同一个 `<path>.tmp`：Windows 上直接报「文件被占用」
	// 而失败（调用方多半忽略错误 → 状态静默不落盘），Linux 上则可能把写了一半
	// 的临时文件 rename 成正式状态文件（含 refresh_token 的账号全部丢失）。
	// 因此落盘必须串行，并且临时文件名要唯一。
	saveMu sync.Mutex
	// journalMu serializes settlement and compaction, without holding it during
	// full-snapshot disk I/O. Lock order: saveMu -> journalMu -> mu.
	journalMu      sync.Mutex
	journalSize    int64 // protected by journalMu
	journalErr     error // protected by mu; blocks inference until a successful save
	usageBaseReady bool  // protected by mu
	path           string
	st             *State
	rotate         int
	pool           poolRuntime
	budgetActive   map[string]int
}

// --- 账号 ---

// UpsertAccount 按 UserData / AK 去重后写入账号。
//
// 入库时拷贝一份：store 拥有自己的副本，调用方之后对入参的修改不会绕过锁
// 偷偷改到 store 内部状态。
func (s *Store) UpsertAccount(a *accounts.Account) bool {
	if a == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upsertAccountLocked(a)
}

func (s *Store) upsertAccountLocked(a *accounts.Account) bool {
	for i, ex := range s.st.Accounts {
		if ex.ProviderID() != a.ProviderID() || ex.Scope != a.Scope {
			continue
		}
		sameExternal := a.ExternalID != "" && a.ExternalID == ex.ExternalID
		sameDir := ex.UserData != "" && ex.UserData == a.UserData
		oldSession, newSession := codearts.StoredSession(ex), codearts.StoredSession(a)
		sameAK := oldSession != nil && newSession != nil &&
			oldSession.AccessKey != "" && oldSession.AccessKey == newSession.AccessKey
		sameDomain := oldSession != nil && newSession != nil &&
			oldSession.DomainID != "" && oldSession.DomainID == newSession.DomainID
		if sameExternal || sameDir || sameAK || sameDomain {
			a.ID = ex.ID
			a.Enabled, a.Paused = ex.Enabled, ex.Paused
			a.MaxInFlight = ex.MaxInFlight
			s.st.Accounts[i] = cloneAccount(a)
			return false
		}
	}
	s.st.Accounts = append(s.st.Accounts, cloneAccount(a))
	return true
}

// RemoveAccount 删除账号。
func (s *Store) RemoveAccount(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, a := range s.st.Accounts {
		if a.ID == id {
			s.st.Accounts = append(s.st.Accounts[:i], s.st.Accounts[i+1:]...)
			return true
		}
	}
	return false
}

// NextAccount 以轮询方式挑一个可用账号，返回快照。
//
// 连续失败过多的账号会被跳过；全部不可用时回退到失败最少的那个，
// 保证不会硬阻塞请求。
func (s *Store) NextAccount() *accounts.Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	chosen, cursor := scheduler.Legacy(len(s.st.Accounts), s.rotate, func(idx int) (bool, int) {
		a := s.st.Accounts[idx]
		return a.ProviderID() == "codearts" && a.Enabled, a.Failures
	})
	s.rotate = cursor
	if chosen < 0 {
		return nil
	}
	return cloneAccount(s.st.Accounts[chosen])
}

// SetAccountEnabled 启用 / 停用账号（在锁内修改，避免与控制台读取竞争）。
func (s *Store) SetAccountEnabled(id string, enabled bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.st.Accounts {
		if a.ID == id {
			a.Enabled = enabled
			return true
		}
	}
	return false
}

// ClearAccountErrors 清零失败计数与错误状态。
func (s *Store) ClearAccountErrors(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.st.Accounts {
		if a.ID == id {
			a.Failures = 0
			a.Status = "ok"
			a.LastErr = ""
			a.CooldownUntil = 0
			a.CooldownReason = ""
			return true
		}
	}
	return false
}

// SetAccountSession 用新的会话替换账号上缓存的凭据（续期 / 跟随 IDE 时调用）。
//
// 凭据解析走的是快照，所以 Manager 拿到新会话后必须通过这里写回，
// 否则续期结果只存在于那次请求里。
func (s *Store) SetAccountSession(id string, sess *codearts.Session) bool {
	if sess == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.st.Accounts {
		if a.ID == id {
			a.NativeCredential = sess
			return true
		}
	}
	return false
}

// MarkAccountResult 记录一次账号调用结果。
func (s *Store) MarkAccountResult(id string, ok bool, note string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.st.Accounts {
		if a.ID != id {
			continue
		}
		a.LastUsed = time.Now().Unix()
		if ok {
			a.Failures = 0
			a.Status = "ok"
			a.LastErr = ""
			if note != "" {
				a.Note = note
			}
		} else {
			a.Failures++
			a.Status = "error"
			a.LastErr = note
			if a.Failures >= s.st.Settings.Pool.BreakerThreshold {
				until := s.poolNow().Add(time.Duration(s.st.Settings.Pool.BreakerCooldownSec) * time.Second).Unix()
				if until > a.CooldownUntil {
					a.CooldownUntil = until
					a.CooldownReason = "连续失败熔断"
				}
			}
		}
		return
	}
}

// MarkClaimResult 记录一次每日签到结果。
//
// ok 为真时记下日期（同一天不会重复签），失败时只记错误、不写日期，
// 这样下一轮（5 分钟后）还会重试。
func (s *Store) MarkClaimResult(id, date string, ok bool, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.st.Accounts {
		if a.ID != id {
			continue
		}
		a.LastClaimAt = time.Now().Unix()
		if ok {
			a.LastClaimDate = date
			a.LastClaimErr = ""
			a.LastClaimMsg = msg
		} else {
			a.LastClaimErr = msg
		}
		return
	}
}

// --- 密钥 ---

// AddKey 新增密钥。
func (s *Store) AddKey(name string) *APIKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := &APIKey{
		Key:       "sk-ca-" + codearts.RandomHex(24),
		Name:      name,
		Enabled:   true,
		CreatedAt: time.Now().Unix(),
	}
	s.st.Keys = append(s.st.Keys, k)
	snapshot := *k
	return &snapshot
}

// DeleteKey 删除密钥。
func (s *Store) DeleteKey(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, k := range s.st.Keys {
		if k.Key == key {
			s.st.Keys = append(s.st.Keys[:i], s.st.Keys[i+1:]...)
			return true
		}
	}
	return false
}

// ToggleKey 启用 / 停用密钥。
func (s *Store) ToggleKey(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range s.st.Keys {
		if k.Key == key {
			k.Enabled = !k.Enabled
			return true
		}
	}
	return false
}

// ValidKey 校验密钥并计数。
func (s *Store) ValidKey(key string) bool {
	_, ok := s.AuthenticateKey(key)
	return ok
}

// AuthenticateKey returns immutable ownership with the original key counter
// update. Administrator-owned keys do not traverse a user registry.
func (s *Store) AuthenticateKey(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range s.st.Keys {
		if k.Key == key && k.Enabled {
			if s.keyOwnerLocked(k) != nil {
				return "", false
			}
			k.LastUsed = time.Now().Unix()
			k.Requests++
			return k.UserID, true
		}
	}
	return "", false
}

// HasKeys 报告是否已签发任何密钥。
func (s *Store) HasKeys() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.st.Keys) > 0
}

// Once consumers exist, deleting their last key must not reopen local or
// explicitly anonymous access and thereby bypass user authorization/budgets.
func (s *Store) RequiresAPIKey() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.st.Keys) > 0 || len(s.st.Users) > 0
}

// --- 设置 / 统计 / 日志 ---

// UpdateSettings 局部更新设置。
func (s *Store) UpdateSettings(fn func(*Settings)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.st.Settings)
}

// recordPreparedLocked applies one priced settlement. Recovery uses the stored
// price, never today's model configuration. Caller holds mu.
func (s *Store) recordPreparedLocked(e LogEntry, outChars int, key string) {
	s.initUsageHistoryLocked()
	if e.Usage.Known {
		if e.EstimatedCost != nil {
			s.st.Stats.PricedRequests++
			s.st.Stats.EstimatedUSD += e.EstimatedCost.Amount
			s.st.Stats.PricedUSDRequests++
		}
	}
	if key != "" && e.Attempts > 0 {
		for _, k := range s.st.Keys {
			if k.Key == key {
				k.BudgetUsage.Requests++
				k.BudgetUsage.Tokens += e.Usage.InputTokens + e.Usage.OutputTokens
				if e.EstimatedCost != nil {
					k.BudgetUsage.USD += e.EstimatedCost.Amount
				}
				if e.Usage.Known && (e.EstimatedCost == nil || e.EstimatedCost.Partial) {
					k.BudgetUsage.Unpriced = true
				}
				if e.UsageIncomplete || !e.Usage.Known {
					k.BudgetUsage.Incomplete = true
				}
				break
			}
		}
	}
	// Ownership is captured at authentication, persisted in the journal, and
	// survives key deletion during an in-flight request and subsequent replay.
	if e.UserID != "" && e.Attempts > 0 {
		if u := s.userLocked(e.UserID); u != nil {
			addBudgetUsage(&u.BudgetUsage, e)
		}
	}
	if e.RequestKind == "summary" {
		s.st.Stats.SummaryRequests++
		s.st.Stats.SummaryRetries += int64(max(0, e.SummaryAttempts-1))
		if e.Status >= 400 {
			s.st.Stats.SummaryFailures++
		}
	}
	if e.FinishReason == "length" {
		s.st.Stats.TruncatedRequests++
	}
	if e.UsageIncomplete {
		s.st.Stats.PartialUsageRequests++
	}
	s.st.Stats.TotalRequests++
	s.st.Stats.TotalLatencyMS += max(0, e.LatencyMS)
	s.st.Stats.LatencySamples++
	if s.st.Stats.UsageStartedAt == 0 {
		s.st.Stats.UsageStartedAt = e.Time
	}
	if e.Usage.Known {
		s.st.Stats.UsageRequests++
		s.st.Stats.InputTokens += e.Usage.InputTokens
		s.st.Stats.OutputTokens += e.Usage.OutputTokens
		s.st.Stats.CachedTokens += e.Usage.CachedTokens
		s.st.Stats.ReasoningTokens += e.Usage.ReasoningTokens
	}
	if e.Status >= 400 || e.Err != "" {
		s.st.Stats.FailedRequests++
	}
	s.st.Stats.TotalCharsOut += int64(outChars)
	s.st.UsageRecords = append(s.st.UsageRecords, e)
	if len(s.st.UsageRecords) > MaxUsageRecords {
		s.st.UsageRecords = s.st.UsageRecords[len(s.st.UsageRecords)-MaxUsageRecords:]
	}
	s.st.Logs = append(s.st.Logs, e)
	if keep := s.st.Settings.LogKeep; keep > 0 && len(s.st.Logs) > keep {
		s.st.Logs = s.st.Logs[len(s.st.Logs)-keep:]
	}
}

// ClearLogs 清空日志。
func (s *Store) ClearLogs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initUsageHistoryLocked()
	s.st.Logs = nil
}

// ClearStats 重置统计。
func (s *Store) ClearStats() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Stats = Stats{StartedAt: time.Now().Unix()}
}

// Record external daily rewards atomically. Failed/unavailable results never set
// the completed-day marker; a retry deadline survives service restarts.
func (s *Store) SaveDailyReward(id, date, state, message string, confirmed bool, next int64) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.st.Accounts {
		if a.ID != id {
			continue
		}
		before := *a
		a.LastClaimAt = time.Now().Unix()
		a.LastClaimState = state
		a.NextClaimAt = next
		a.LastClaimMsg = message
		if confirmed {
			a.LastClaimDate = date
			a.LastClaimErr = ""
		} else if state == "failed" {
			a.LastClaimErr = message
		} else {
			a.LastClaimErr = ""
		}
		if err := s.hashPasswordLocked(); err != nil {
			*a = before
			return err
		}
		raw, err := json.MarshalIndent(s.st, "", "  ")
		if err == nil {
			err = s.writeState(raw)
		}
		if err != nil {
			*a = before
		}
		return err
	}
	return fmt.Errorf("账号已删除")
}
