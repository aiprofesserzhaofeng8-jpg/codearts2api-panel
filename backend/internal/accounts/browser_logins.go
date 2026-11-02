package accounts

import (
	"context"
	"errors"
)

// BrowserLoginBackend keeps native authorization and its atomic commit gate in
// the platform adapter. The business service never decodes platform credentials.
type BrowserLoginBackend interface {
	Start(BrowserLoginOptions) (BrowserLoginStart, error)
	Get(string) BrowserLoginFlow
	Cancel(string) bool
}

type BrowserLoginOptions struct {
	PortalHost, Locale string
	Port               int
}
type BrowserLoginStart struct {
	ID, AuthURL, Callback, Hint string
	Port                        int
}

// BrowserCredential borrows an immutable generation only inside the backend /
// business boundary. Display hints are already masked by the owning platform.
type BrowserCredential struct {
	Value                                    any
	Name, Fingerprint, KeyHint, IdentityHint string
	HasRefresh                               bool
	ExpiresAt                                int64
}
type BrowserLoginSnapshot struct {
	Status, Message, AccountName, SavedAccountID string
	Credential                                   BrowserCredential
}
type BrowserLoginFlow interface {
	Snapshot() BrowserLoginSnapshot
	Submit(context.Context, string) error
	Commit(func(BrowserCredential) (string, error)) (string, error)
}
type BrowserLoginStore interface {
	Account(string) *Account
	FindAccountSession(any) *Account
	UpsertAccountAndSave(*Account, bool) (bool, error)
}

// BrowserLoginPreview is display data only. Credentials never leave Poll.
type BrowserLoginPreview struct {
	Status, Message, AccountName, AccountID string
	KeyHint, IdentityHint, PreviewID        string
	Ready, Saved, Existing, HasRefresh      bool
	ExpiresAt                               int64
}
type BrowserLoginConfirmation struct {
	SessionID, PreviewID, Name string
	Replace                    bool
}

// BrowserLogins coordinates CodeArts' existing staged browser authorization.
// Native flow locks still serialize confirmation/cancellation and persistence;
// no second flow registry or credential representation is introduced.
type BrowserLogins struct {
	store      BrowserLoginStore
	backend    BrowserLoginBackend
	randomID   func(int) string
	loginPort  func() int
	invalidate func(string)
}

func NewBrowserLogins(store BrowserLoginStore, backend BrowserLoginBackend, randomID func(int) string, loginPort func() int, invalidate func(string)) *BrowserLogins {
	return &BrowserLogins{store: store, backend: backend, randomID: randomID, loginPort: loginPort, invalidate: invalidate}
}
func (s *BrowserLogins) Start(portal, locale string) (BrowserLoginStart, error) {
	flow, err := s.backend.Start(BrowserLoginOptions{PortalHost: portal, Locale: locale, Port: s.loginPort()})
	if err != nil {
		return BrowserLoginStart{}, &LoginError{Kind: LoginInternal, Message: "启动登录失败: " + err.Error()}
	}
	return flow, nil
}
func (s *BrowserLogins) Poll(id string) (BrowserLoginPreview, error) {
	flow := s.backend.Get(id)
	if flow == nil {
		return BrowserLoginPreview{}, &LoginError{Kind: LoginMissing, Message: "登录会话不存在或已过期"}
	}
	snapshot := flow.Snapshot()
	result := BrowserLoginPreview{Status: snapshot.Status, Message: snapshot.Message}
	credential := snapshot.Credential
	if snapshot.Status == "confirmed" && credential.Value != nil {
		result.Ready = true
		result.Saved = snapshot.SavedAccountID != ""
		result.AccountID = snapshot.SavedAccountID
		result.AccountName = snapshot.AccountName
		result.KeyHint = credential.KeyHint
		result.IdentityHint = credential.IdentityHint
		result.ExpiresAt = credential.ExpiresAt
		result.HasRefresh = credential.HasRefresh
		result.PreviewID = credential.Fingerprint
		result.Existing = s.store.FindAccountSession(credential.Value) != nil
		result.Message = "授权已验证，请核对账号后点击确认完成登录"
	}
	return result, nil
}
func (s *BrowserLogins) Submit(ctx context.Context, id, input string) (BrowserLoginPreview, error) {
	flow := s.backend.Get(id)
	if flow == nil {
		return BrowserLoginPreview{}, &LoginError{Kind: LoginMissing, Message: "登录会话不存在或已过期，请重新发起登录"}
	}
	if err := flow.Submit(ctx, input); err != nil {
		return BrowserLoginPreview{}, &LoginError{Kind: LoginInvalid, Message: err.Error()}
	}
	snapshot := flow.Snapshot()
	message := snapshot.Message
	if snapshot.Status == "confirmed" {
		message = "授权已验证，请检查账号并确认保存"
	}
	return BrowserLoginPreview{Status: snapshot.Status, Message: message}, nil
}
func (s *BrowserLogins) Confirm(body BrowserLoginConfirmation) (string, error) {
	flow := s.backend.Get(body.SessionID)
	if flow == nil {
		return "", &LoginError{Kind: LoginMissing, Message: "登录会话已失效，请重新获取链接"}
	}
	id, err := flow.Commit(func(credential BrowserCredential) (string, error) {
		if body.PreviewID != credential.Fingerprint {
			return "", errors.New("账号预览已变化，请重新检查")
		}
		name := body.Name
		if name == "" {
			name = credential.Name
		}
		account := &Account{ID: s.randomID(8), Name: name, Source: "browser", Enabled: true, Status: "ok", NativeCredential: credential.Value}
		if _, err := s.store.UpsertAccountAndSave(account, body.Replace); err != nil {
			if errors.Is(err, ErrAccountExists) {
				return "", err
			}
			return "", errors.New("凭据保存失败，请检查目录权限后重试")
		}
		s.invalidate(account.ID)
		return account.ID, nil
	})
	if err != nil {
		return "", &LoginError{Kind: LoginConflict, Message: err.Error()}
	}
	if s.store.Account(id) == nil {
		return "", &LoginError{Kind: LoginExpired, Message: "该次登录保存的账号已被删除，请重新发起登录"}
	}
	return id, nil
}
func (s *BrowserLogins) Cancel(id string) bool { return s.backend.Cancel(id) }
