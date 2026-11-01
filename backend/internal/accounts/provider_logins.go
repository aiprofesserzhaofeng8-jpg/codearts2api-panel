package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"time"

	"codearts2api/internal/provider"
)

// ProviderLoginStore retains the existing atomic duplicate/save operation.
// Login checks only read it; credentials are persisted after confirmation.
type ProviderLoginStore interface {
	Account(string) *Account
	FindProviderAccount(string, string, string) *Account
	UpsertAccountAndSave(*Account, bool) (bool, error)
}

// LoginAdapters is supplied per operation so registry replacement cannot leave
// authorization using an older set of platform clients.
type LoginAdapters interface {
	Get(string) (provider.Adapter, bool)
}

type ProviderLoginRequest struct {
	Action, Provider, Scope, SessionID, Input, Preview, Name string
	MachineID, DeviceID                                      string
	Replace                                                  bool
}

type ProviderImportRequest struct {
	Provider, Name string
	Credentials    json.RawMessage
	Replace        bool
}

type LoginState uint8

const (
	LoginStarted LoginState = iota
	LoginPending
	LoginPreview
	LoginSaved
	LoginCanceled
)

// ProviderLoginResult contains display data only, never credentials or callback
// input. The HTTP boundary owns wire names and identity masking.
type ProviderLoginResult struct {
	State                           LoginState
	SessionID, URL, Provider, Scope string
	PreviewID, AccountID            string
	ExpiresAt                       int64
	Identity                        provider.Identity
	Existing                        bool
}

type LoginErrorKind uint8

const (
	LoginInvalid LoginErrorKind = iota
	LoginMissing
	LoginExpired
	LoginConflict
	LoginCapacity
	LoginUpstream
)

type LoginError struct {
	Kind    LoginErrorKind
	Message string
}

func (e *LoginError) Error() string { return e.Message }
func loginFailure(kind LoginErrorKind, message string) (ProviderLoginResult, error) {
	return ProviderLoginResult{}, &LoginError{Kind: kind, Message: message}
}

type providerLoginFlow struct {
	mu                          sync.Mutex
	provider, scope, state, url string
	created                     time.Time
	credentials                 json.RawMessage
	identity                    provider.Identity
	saved, callbackDigest       string
	canceled                    bool
}

// ProviderLogins owns staged authorization, preview confirmation and verified
// import. Native adapters still own authorization protocols and credential
// interpretation; the store remains the only persistence owner.
type ProviderLogins struct {
	store    ProviderLoginStore
	randomID func(int) string
	mu       sync.Mutex
	flows    map[string]*providerLoginFlow
}

func NewProviderLogins(store ProviderLoginStore, randomID func(int) string) *ProviderLogins {
	return &ProviderLogins{store: store, randomID: randomID}
}
func fingerprintCredentials(raw json.RawMessage) string {
	d := sha256.Sum256(raw)
	return hex.EncodeToString(d[:])
}
func releaseProviderLogin(adapters LoginAdapters, f *providerLoginFlow) {
	if adapter, ok := adapters.Get(f.provider); ok {
		if cleanup, ok := adapter.(provider.LoginReleaser); ok {
			cleanup.ReleaseLogin(f.state)
		}
	}
}

func (s *ProviderLogins) Apply(ctx context.Context, body ProviderLoginRequest, adapters LoginAdapters) (ProviderLoginResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if body.Action == "start" {
		return s.start(ctx, body, adapters)
	}
	s.mu.Lock()
	f := s.flows[body.SessionID]
	s.mu.Unlock()
	if f == nil {
		return loginFailure(LoginMissing, "授权会话已失效，请重新获取链接")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if body.Action == "cancel" {
		f.canceled = true
		releaseProviderLogin(adapters, f)
		f.credentials = nil
		return ProviderLoginResult{State: LoginCanceled}, nil
	}
	if f.canceled || time.Since(f.created) > 10*time.Minute {
		return loginFailure(LoginExpired, "授权已取消或过期，请重新获取链接")
	}
	switch body.Action {
	case "check":
		return s.check(ctx, body, adapters, f)
	case "complete":
		return s.complete(body, adapters, f)
	default:
		return loginFailure(LoginInvalid, "未知授权操作")
	}
}

func (s *ProviderLogins) start(ctx context.Context, body ProviderLoginRequest, adapters LoginAdapters) (ProviderLoginResult, error) {
	adapter, ok := adapters.Get(body.Provider)
	if !ok {
		return loginFailure(LoginInvalid, "该上游尚未接入")
	}
	login, ok := adapter.(provider.BrowserLogin)
	if !ok {
		return loginFailure(LoginInvalid, "该上游不支持浏览器登录")
	}
	if body.Scope == "" {
		body.Scope = "cn"
	}
	f := &providerLoginFlow{provider: body.Provider, scope: body.Scope, created: time.Now()}
	id := s.randomID(16)
	s.mu.Lock()
	if s.flows == nil {
		s.flows = map[string]*providerLoginFlow{}
	}
	for k, v := range s.flows {
		if time.Since(v.created) > 10*time.Minute {
			releaseProviderLogin(adapters, v)
			delete(s.flows, k)
		}
	}
	if len(s.flows) >= 128 {
		s.mu.Unlock()
		return loginFailure(LoginCapacity, "未完成的授权过多，请稍后再试")
	}
	s.flows[id] = f
	f.mu.Lock()
	s.mu.Unlock()
	defer f.mu.Unlock()
	var upstream provider.Login
	var err error
	kind := LoginUpstream
	if deviceLogin, ok := adapter.(provider.DeviceBrowserLogin); ok {
		upstream, err = deviceLogin.BeginLoginWithDevice(ctx, body.Scope, body.MachineID, body.DeviceID)
		kind = LoginInvalid
	} else {
		upstream, err = login.BeginLogin(ctx, body.Scope)
	}
	if err != nil {
		f.canceled = true
		return loginFailure(kind, err.Error())
	}
	f.state = upstream.State
	f.url = upstream.URL
	return ProviderLoginResult{State: LoginStarted, SessionID: id, URL: f.url, Provider: f.provider, Scope: f.scope, ExpiresAt: f.created.Add(10 * time.Minute).Unix()}, nil
}

// The caller holds f.mu throughout check/complete, including native exchange
// and persistence. Repeated checks reuse the preview; repeated saves reuse ID.
func (s *ProviderLogins) check(ctx context.Context, body ProviderLoginRequest, adapters LoginAdapters, f *providerLoginFlow) (ProviderLoginResult, error) {
	if f.saved != "" {
		return loginFailure(LoginConflict, "本次授权已保存，请刷新账号列表")
	}
	adapter, _ := adapters.Get(f.provider)
	callback, usesCallback := adapter.(provider.CallbackLogin)
	if usesCallback && f.credentials != nil && strings.TrimSpace(body.Input) != "" && fingerprintCredentials([]byte(strings.TrimSpace(body.Input))) != f.callbackDigest {
		return loginFailure(LoginConflict, "本次授权已核对其他回调；切换账号请重新生成登录链接")
	}
	if body.Input != "" && !usesCallback {
		u, e := url.Parse(strings.TrimSpace(body.Input))
		if e != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Query().Get("state") != f.state {
			return loginFailure(LoginInvalid, "回调地址不属于本次授权；WorkBuddy 也可完成登录后直接点击检查")
		}
	}
	if f.credentials == nil {
		var raw json.RawMessage
		var ready bool
		var err error
		if usesCallback {
			raw, ready, err = callback.CheckCallback(ctx, f.scope, f.state, body.Input)
		} else {
			login := adapter.(provider.BrowserLogin)
			raw, ready, err = login.CheckLogin(ctx, f.scope, f.state)
		}
		if err != nil {
			return loginFailure(LoginUpstream, err.Error())
		}
		if !ready {
			return ProviderLoginResult{State: LoginPending}, nil
		}
		normalized, identity, err := adapter.Normalize(raw)
		if err != nil || identity.Scope != f.scope {
			return loginFailure(LoginUpstream, "授权身份与地区不匹配")
		}
		f.credentials = normalized
		if usesCallback {
			f.callbackDigest = fingerprintCredentials([]byte(strings.TrimSpace(body.Input)))
		}
		f.identity = identity
	}
	return ProviderLoginResult{State: LoginPreview, Provider: f.provider, Scope: f.scope, Identity: f.identity, PreviewID: fingerprintCredentials(f.credentials), Existing: s.store.FindProviderAccount(f.provider, f.scope, f.identity.ID) != nil}, nil
}

func (s *ProviderLogins) complete(body ProviderLoginRequest, adapters LoginAdapters, f *providerLoginFlow) (ProviderLoginResult, error) {
	if f.saved != "" {
		if s.store.Account(f.saved) == nil {
			return loginFailure(LoginExpired, "保存的账号已被删除，请重新授权")
		}
		return ProviderLoginResult{State: LoginSaved, AccountID: f.saved}, nil
	}
	if len(f.credentials) == 0 || body.Preview != fingerprintCredentials(f.credentials) {
		return loginFailure(LoginConflict, "请检查授权并核对账号预览")
	}
	if f.identity.ExpiresAt <= time.Now().Unix() {
		return loginFailure(LoginExpired, "授权凭据已过期")
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = f.identity.Name
	}
	a := &Account{ID: s.randomID(8), Provider: f.provider, Scope: f.scope, ExternalID: f.identity.ID, Credentials: f.credentials, Name: name, Enabled: true, Source: "browser", Status: "ok"}
	if _, err := s.store.UpsertAccountAndSave(a, body.Replace); err != nil {
		return loginFailure(LoginConflict, "保存失败："+err.Error())
	}
	f.saved = a.ID
	releaseProviderLogin(adapters, f)
	f.credentials = nil
	return ProviderLoginResult{State: LoginSaved, AccountID: a.ID}, nil
}

func (s *ProviderLogins) Import(ctx context.Context, body ProviderImportRequest, adapters LoginAdapters) (string, error) {
	adapter, ok := adapters.Get(body.Provider)
	if !ok {
		return "", &LoginError{Kind: LoginInvalid, Message: "该上游尚未接入"}
	}
	raw, identity, err := adapter.Normalize(body.Credentials)
	if err != nil {
		return "", &LoginError{Kind: LoginInvalid, Message: err.Error()}
	}
	if identity.ExpiresAt <= time.Now().Unix() {
		return "", &LoginError{Kind: LoginInvalid, Message: "导入凭据已过期，请先在原客户端刷新或重新登录"}
	}
	verifier, ok := adapter.(interface {
		Verify(context.Context, json.RawMessage) (provider.Identity, error)
	})
	if !ok {
		return "", &LoginError{Kind: LoginInvalid, Message: "该上游尚不支持凭据身份校验"}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	verified, err := verifier.Verify(ctx, raw)
	if err != nil || verified.ID != identity.ID || verified.Scope != identity.Scope {
		return "", &LoginError{Kind: LoginInvalid, Message: "上游账号身份校验失败，请检查完整凭据和所属地区"}
	}
	name := body.Name
	if name == "" {
		name = verified.Name
	}
	a := &Account{ID: s.randomID(8), Provider: body.Provider, Scope: identity.Scope, ExternalID: identity.ID, Credentials: raw, Name: name, Enabled: true, Source: "session", Status: "ok"}
	if _, err = s.store.UpsertAccountAndSave(a, body.Replace); err != nil {
		return "", &LoginError{Kind: LoginConflict, Message: "保存失败：" + err.Error()}
	}
	return a.ID, nil
}
