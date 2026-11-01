package codearts

import (
	"context"
	"fmt"
	"time"

	"codearts2api/internal/accounts"
)

// ResolveOptions 控制凭据解析策略。
type ResolveOptions struct {
	// LiveTTL 是「从 state.vscdb 重读」的缓存时长。IDE 运行时它会持续续期，
	// 因此取一个较短的值就能自动跟随，同时避免每次请求都解析 SQLite。
	LiveTTL time.Duration
	// Margin 是提前量：剩余有效期小于它即视为需要续期。
	Margin time.Duration
	// AllowRefresh 允许在凭据过期且 IDE 未续期时，自行用 refresh_token 续期。
	AllowRefresh bool
	// WriteBack 允许把续期结果回写 state.vscdb（让 IDE 也拿到新凭据）。
	WriteBack bool
}

// DefaultResolveOptions 返回推荐策略。
func DefaultResolveOptions() ResolveOptions {
	return ResolveOptions{
		LiveTTL:      10 * time.Second,
		Margin:       5 * time.Minute,
		AllowRefresh: true,
		WriteBack:    true,
	}
}

// Manager 负责为账号解析出可用的上游凭据。
//
// 优先级：
//  1. state.vscdb 里的实时会话（IDE 运行时它自己会续期，永远最新）
//  2. 网关本地缓存的会话（IDE 关闭后仍可继续用）
//  3. 用 refresh_token 自行续期（DPoP + PKCE verifier）
type Manager struct {
	Client *Client
	// One business runtime owns the existing mutexes, cache and publication hooks.
	// Native credentials, IDE access, refresh and comparison remain in this package.
	accounts.CredentialRuntime[*Session]
}

// ErrSessionChanged preserves the existing caller error identity.
var ErrSessionChanged = accounts.ErrCredentialChanged

// NewManager 构造凭据管理器。
func NewManager(c *Client) *Manager {
	return &Manager{Client: c, CredentialRuntime: accounts.NewCredentialRuntime[*Session]()}
}

func (m *Manager) logf(format string, args ...any) {
	if m.Logf != nil {
		m.Logf(format, args...)
	}
}

// Session 解析出该账号当前可用的凭据。
//
// 锁的粒度是「账号」，不是整个 Manager：
//
//   - 必须串行：refresh_token 是「单次使用 + 轮转」的，同一账号两个 goroutine
//     同时续期会互相作废（上游报 STS5.1806，见 docs/UPSTREAM.md §8）；
//   - 不能全局串行：续期是网络调用（最长可达几十秒），全局锁会让一个账号的续期
//     挡住所有其他账号 —— 测活这类 4 路并发会在凭据解析阶段退化成串行。
//
// 拿到锁之后才解析：排在后面的 goroutine 会看到前一个刚写入缓存的会话
// （resolve 的第 1 步），直接复用，不会拿已轮转的 refresh_token 再续一次。
func (m *Manager) Session(ctx context.Context, acct *Account, opt ResolveOptions) (*Session, error) {
	if acct == nil || acct.ProviderID() != "codearts" {
		return nil, fmt.Errorf("该操作仅支持码道账号")
	}
	if opt.LiveTTL <= 0 {
		opt.LiveTTL = 10 * time.Second
	}
	if opt.Margin <= 0 {
		opt.Margin = 5 * time.Minute
	}

	return m.CredentialRuntime.Resolve(ctx, acct, func(ctx context.Context, latest *Account) (*Session, error) {
		return m.resolve(ctx, latest, opt)
	})
}

// resolve 是 Session 的实际解析逻辑。调用方必须已持有该账号的锁。
func (m *Manager) resolve(ctx context.Context, acct *Account, opt ResolveOptions) (*Session, error) {
	stored := StoredSession(acct)
	// 1) 实时会话（带短缓存）
	live := m.Cached(acct.ID, opt.LiveTTL)
	if m.Latest != nil && live != nil && live != stored {
		live = nil
	}
	if live == nil && acct.UserData != "" {
		if s, err := ReadSession(acct.UserData); err == nil {
			m.Remember(acct.ID, s)
			live = s
		} else {
			m.logf("账号 %s 读取 IDE 会话失败: %v", acct.DisplayName(), err)
		}
	}
	if live != nil && live.Valid(opt.Margin) {
		if err := m.adopt(acct, live); err != nil {
			return nil, err
		}
		return live, nil
	}

	// 2) 本地缓存的会话
	if stored != nil && stored.Valid(opt.Margin) {
		return stored, nil
	}

	// 3) 自行续期。优先用实时会话里的 refresh_token（更新），否则用缓存的。
	fresh := live
	if fresh == nil || !fresh.HasRefresh() {
		fresh = stored
	}
	if !opt.AllowRefresh || fresh == nil || !fresh.HasRefresh() {
		if live != nil {
			return nil, &CredentialsError{
				Account: acct.DisplayName(),
				Reason: fmt.Sprintf("凭据已于 %s 过期，且没有可用的续期材料（缺 refresh_token / DPoP 私钥 / PKCE verifier）",
					live.ExpiresAt.Local().Format("2006-01-02 15:04:05")),
			}
		}
		return nil, &CredentialsError{Account: acct.DisplayName(), Reason: "没有可用凭据"}
	}

	m.logf("账号 %s 凭据已过期，尝试用 refresh_token 续期", acct.DisplayName())
	res, err := m.Client.Refresh(ctx, fresh)
	if err != nil {
		return nil, &CredentialsError{
			Account: acct.DisplayName(),
			Reason:  "自动续期失败：" + DescribeRefreshError(err),
			Err:     err,
		}
	}
	updated := *fresh
	updated.AccessKey = res.AccessKey
	updated.SecretKey = res.SecretKey
	updated.SecurityToken = res.SecurityToken
	updated.ExpiresAt = res.ExpiresAt
	updated.RefreshToken = res.RefreshToken
	updated.Source = "refresh"
	m.logf("账号 %s 续期成功，新凭据有效期至 %s", acct.DisplayName(), res.ExpiresAt.Local().Format("2006-01-02 15:04:05"))

	if err := m.adopt(acct, &updated); err != nil {
		return nil, err
	}
	m.Remember(acct.ID, &updated)

	if opt.WriteBack && acct.UserData != "" {
		if err := writeBackSession(acct.UserData, &updated); err != nil {
			// 回写失败不影响网关自身使用，仅提示
			m.logf("账号 %s 回写 state.vscdb 失败（网关内仍可正常使用）: %v", acct.DisplayName(), err)
		} else {
			m.logf("账号 %s 新凭据已回写 state.vscdb，IDE 下次启动可直接复用", acct.DisplayName())
		}
	}
	return &updated, nil
}

// adopt 把新会话记到账号快照上；凭据真的变了就通知持久化。
//
// 凭据轮换（IDE 自行续期、网关自行续期）大约每 2 小时一次，changed 为假时
// 不会触发落盘，因此这里不会造成写盘风暴。
func (m *Manager) adopt(acct *Account, s *Session) error {
	stored := StoredSession(acct)
	changed := stored == nil ||
		stored.AccessKey != s.AccessKey ||
		stored.SecurityToken != s.SecurityToken ||
		stored.RefreshToken != s.RefreshToken
	return m.CredentialRuntime.Adopt(acct, stored, s, changed)
}

// ImportFromDesktop 扫描本机 CodeArts Agent 的 User Data 目录并导入会话。
func ImportFromDesktop(explicit string) ([]*ImportResult, error) {
	var out []*ImportResult
	var lastErr error
	for _, dir := range DefaultUserDataDirs(explicit) {
		sess, err := ReadSession(dir)
		if err != nil {
			lastErr = err
			continue
		}
		name := sess.AccountLabel
		if name == "" {
			name = "CodeArts " + shortID(sess.DomainID)
		}
		out = append(out, &ImportResult{
			Dir: dir,
			Account: &Account{
				ID:       RandomHex(8),
				Name:     name,
				Source:   "desktop",
				UserData: dir,
				Enabled:  true,
				Status:   "ok",
				Session:  sess,
			},
			AK:      sess.AccessKey,
			Expires: sess.ExpiresAt,
		})
	}
	if len(out) == 0 {
		if lastErr == nil {
			lastErr = fmt.Errorf("未找到 CodeArts Agent 的登录会话")
		}
		return nil, lastErr
	}
	return out, nil
}

// ManualAccount 用手工录入的 AK/SK/STS 构造账号（供 Docker / 跨平台场景使用）。
//
// 适用于 STS 临时凭据：必须有 security_token，且需要给出（或按 TTL 推定）过期时间，
// 之后靠 refresh_token 续期。若拿到的是 IAM 永久 AK/SK（无 STS、无 refresh_token），
// 请改用 ManualAKSKAccount，否则 110 分钟后会被判为过期且无法续期。
func ManualAccount(name, ak, sk, sts string, expires time.Time) (*Account, error) {
	if ak == "" || sk == "" {
		return nil, fmt.Errorf("access key / secret key 不能为空")
	}
	if name == "" {
		name = "手工录入 " + shortID(ak)
	}
	if expires.IsZero() {
		expires = time.Now().Add(DefaultCredentialTTL)
	}
	return &Account{
		ID:      RandomHex(8),
		Name:    name,
		Source:  "manual",
		Enabled: true,
		Status:  "ok",
		Session: &Session{
			AccountLabel:  name,
			AccessKey:     ak,
			SecretKey:     sk,
			SecurityToken: sts,
			ExpiresAt:     expires,
			Source:        "manual",
		},
	}, nil
}

// ManualAKSKAccount 用手工录入的 IAM 永久 AK/SK 构造账号。
//
// 与 ManualAccount 的区别是生命周期语义：永久 AK/SK 不过期、不续期、不带
// security_token。Session 标记为 kindAKSK 后，Valid() 恒为真，Manager 也不会
// 再尝试走 refresh 分支（HasRefresh 为 false 是「无此概念」而非「缺材料」）。
func ManualAKSKAccount(name, ak, sk string) (*Account, error) {
	if ak == "" || sk == "" {
		return nil, fmt.Errorf("access key / secret key 不能为空")
	}
	if name == "" {
		name = "永久AK/SK " + shortID(ak)
	}
	return &Account{
		ID:      RandomHex(8),
		Name:    name,
		Source:  "manual-aksk",
		Enabled: true,
		Status:  "ok",
		Session: &Session{
			AccountLabel:   name,
			AccessKey:      ak,
			SecretKey:      sk,
			CredentialKind: kindAKSK,
			Source:         "manual-aksk",
		},
	}, nil
}
