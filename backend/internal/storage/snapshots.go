package storage

import (
	"encoding/json"

	"codearts2api/internal/accounts"
)

// cloneAccount 返回账号的浅拷贝。
//
// 对外只暴露拷贝：Account 的字段（Enabled / Status / Failures / LastErr / Session…）
// 会被请求路径（MarkAccountResult）与控制台并发读写，直接把内部指针交出去
// 就是数据竞争。Session 指针可以共享 —— 会话构造完成后不再原地修改，
// 续期时是整体替换指针。
func cloneAccount(a *accounts.Account) *accounts.Account {
	if a == nil {
		return nil
	}
	cp := *a
	cp.Credentials = append(json.RawMessage(nil), a.Credentials...)
	return &cp
}

// cloneAccounts 批量浅拷贝。
func cloneAccounts(in []*accounts.Account) []*accounts.Account {
	out := make([]*accounts.Account, len(in))
	for i, a := range in {
		out[i] = cloneAccount(a)
	}
	return out
}

// Accounts 返回账号快照（元素是浅拷贝，可直接并发读取）。
func (s *Store) Accounts() []*accounts.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneAccounts(s.st.Accounts)
}

// Account 按 ID 查找账号，返回快照。
func (s *Store) Account(id string) *accounts.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.st.Accounts {
		if a.ID == id {
			return cloneAccount(a)
		}
	}
	return nil
}

// Keys 返回密钥快照。
func (s *Store) Keys() []*APIKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*APIKey, len(s.st.Keys))
	for i, k := range s.st.Keys {
		snapshot := *k
		out[i] = &snapshot
	}
	return out
}

// Settings 返回设置快照。
func (s *Store) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := s.st.Settings
	cp.CodeArts = cp.CodeArts.Clone()
	cp.Trae = cp.Trae.Clone()
	cp.Qoder = cp.Qoder.Clone()
	cp.Kiro = cp.Kiro.Clone()
	cp.Claude = cp.Claude.Clone()
	cp.ChatGPT = cp.ChatGPT.Clone()
	cp.WorkBuddy = cp.WorkBuddy.Clone()
	cp.ModelAliases = map[string]string{}
	for k, v := range s.st.Settings.ModelAliases {
		cp.ModelAliases[k] = v
	}
	return cp
}

// Stats 返回统计快照。
func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.st.Stats
}

// Logs 返回最近的日志。
func (s *Store) Logs(limit int) []LogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := len(s.st.Logs)
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]LogEntry, limit)
	copy(out, s.st.Logs[n-limit:])
	return out
}
