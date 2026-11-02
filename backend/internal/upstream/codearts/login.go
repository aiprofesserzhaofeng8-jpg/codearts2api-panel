package codearts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"codearts2api/internal/accounts"
	legacy "codearts2api/internal/codearts"
)

// BrowserLogin borrows the original manager and client. Callback listeners,
// PKCE/DPoP, exchange, cancellation and commit locking remain native.
type BrowserLogin struct {
	manager *legacy.LoginManager
	client  *legacy.Client
}

func NewBrowserLogin(manager *legacy.LoginManager, client *legacy.Client) *BrowserLogin {
	return &BrowserLogin{manager: manager, client: client}
}
func (b *BrowserLogin) Start(options accounts.BrowserLoginOptions) (accounts.BrowserLoginStart, error) {
	flow, err := b.manager.Start(legacy.LoginOptions{PortalHost: options.PortalHost, Locale: options.Locale, Port: options.Port, PluginVersion: legacy.LoginPluginVersion})
	if err != nil {
		return accounts.BrowserLoginStart{}, err
	}
	return accounts.BrowserLoginStart{ID: flow.ID, AuthURL: flow.AuthURL, Port: flow.Port, Callback: "http://127.0.0.1:" + strconv.Itoa(flow.Port) + legacy.AuthRedirectPath, Hint: flow.CallbackHint()}, nil
}
func (b *BrowserLogin) Get(id string) accounts.BrowserLoginFlow {
	flow := b.manager.Get(id)
	if flow == nil {
		return nil
	}
	return browserFlow{flow: flow, client: b.client}
}
func (b *BrowserLogin) Cancel(id string) bool { return b.manager.Cancel(id) }

type browserFlow struct {
	flow   *legacy.LoginFlow
	client *legacy.Client
}

func loginCredential(sess *legacy.Session) accounts.BrowserCredential {
	if sess == nil {
		return accounts.BrowserCredential{}
	}
	sum := sha256.Sum256([]byte(sess.DomainID + "\x00" + sess.AccessKey))
	return accounts.BrowserCredential{Value: sess, HasRefresh: sess.HasRefresh(), Name: sess.AccountLabel, Fingerprint: hex.EncodeToString(sum[:]), KeyHint: legacy.Mask(sess.AccessKey), IdentityHint: legacy.Mask(sess.DomainID), ExpiresAt: sess.ExpiresAt.Unix()}
}
func (f browserFlow) Snapshot() accounts.BrowserLoginSnapshot {
	status, message, name, sess := f.flow.Snapshot()
	return accounts.BrowserLoginSnapshot{Status: string(status), Message: message, AccountName: name, SavedAccountID: f.flow.SavedAccountID(), Credential: loginCredential(sess)}
}
func (f browserFlow) Commit(save func(accounts.BrowserCredential) (string, error)) (string, error) {
	return f.flow.Commit(func(sess *legacy.Session) (string, error) { return save(loginCredential(sess)) })
}
func (f browserFlow) Submit(ctx context.Context, input string) error {
	input = strings.TrimSpace(input)
	switch {
	case strings.HasPrefix(input, "{"):
		var expiry struct {
			Expires string `json:"expires_at"`
		}
		_ = json.Unmarshal([]byte(input), &expiry)
		expires, expiryErr := time.Parse(time.RFC3339Nano, expiry.Expires)
		if expiryErr != nil || !expires.After(time.Now()) {
			return fmt.Errorf("session JSON 缺少有效的 expires_at 时间，请粘贴完整凭据")
		}
		sess, err := legacy.SessionFromJSON([]byte(input))
		if err != nil || sess == nil || !sess.Valid(0) || sess.SecurityToken == "" {
			return fmt.Errorf("需要有效且完整的 STS session JSON（含 accessKey、secretKey、securitytoken 和 expires_at）")
		}
		verifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, name, domain, verifyErr := f.client.CallerIdentity(verifyCtx, sess)
		cancel()
		if verifyErr != nil || domain == "" {
			return fmt.Errorf("凭据未通过华为身份验证，请检查完整性和有效期")
		}
		if sess.DomainID != "" && sess.DomainID != domain {
			return fmt.Errorf("凭据与账号身份不一致")
		}
		sess.DomainID = domain
		sess.AccountLabel = name
		sess.Source = "browser-import"
		return f.flow.StageSession(sess)
	case strings.Contains(input, "://"):
		_, err := f.flow.SubmitCallbackURL(input)
		return err
	default:
		return f.flow.SubmitCode(input)
	}
}
