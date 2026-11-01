package codearts

import (
	"context"
	"strings"
	"time"

	"codearts2api/internal/accounts"
	native "codearts2api/internal/codearts"
)

// AccountManagement borrows the existing Manager. It owns native policy and
// serialization; accounts.Management owns lookup, timeout and mutation order.
type AccountManagement struct {
	Manager *native.Manager
	Options func() native.ResolveOptions
}

func (m *AccountManagement) Export(ctx context.Context, a *accounts.Account) (accounts.CredentialExport, error) {
	session, err := m.Manager.Session(ctx, a, m.Options())
	if err != nil {
		return accounts.CredentialExport{}, &accounts.ManagementError{Kind: accounts.ManagementUpstream, Message: "导出失败: " + err.Error()}
	}
	raw, err := session.RawJSON()
	if err != nil {
		return accounts.CredentialExport{}, &accounts.ManagementError{Kind: accounts.ManagementInternal, Message: "序列化失败: " + err.Error()}
	}
	return accounts.CredentialExport{JSON: string(raw), ExpiresAt: session.ExpiresAt.Unix(), HasRefresh: session.HasRefresh()}, nil
}

func (m *AccountManagement) Reimport(ctx context.Context, a *accounts.Account) error {
	_, err := m.Manager.Session(ctx, a, native.ResolveOptions{LiveTTL: 0, Margin: 5 * time.Minute, AllowRefresh: true, WriteBack: false})
	if err != nil {
		return &accounts.ManagementError{Kind: accounts.ManagementUpstream, Message: err.Error()}
	}
	return nil
}

// ManualAccountOptions belongs to CodeArts; it is not shared account metadata.
type ManualAccountOptions struct {
	Name, AccessKey, SecretKey, SecurityToken, CredentialKind string
	ExpiresAt                                                 time.Time
}

func ManualAccount(input ManualAccountOptions) (*accounts.Account, bool, error) {
	permanent := strings.EqualFold(strings.TrimSpace(input.CredentialKind), string(native.CredentialKindAKSK))
	if permanent {
		a, err := native.ManualAKSKAccount(input.Name, input.AccessKey, input.SecretKey)
		return a, true, err
	}
	a, err := native.ManualAccount(input.Name, input.AccessKey, input.SecretKey, input.SecurityToken, input.ExpiresAt)
	return a, false, err
}

func AccountFromSessionJSON(name, input string) (*accounts.Account, error) {
	session, err := native.SessionFromJSON([]byte(input))
	if err != nil {
		return nil, &accounts.ManagementError{Kind: accounts.ManagementInvalid, Message: "session JSON 解析失败: " + err.Error()}
	}
	if session.AccessKey == "" || session.SecretKey == "" {
		return nil, &accounts.ManagementError{Kind: accounts.ManagementInvalid, Message: "session JSON 里缺少 accessKey / secretKey"}
	}
	if name == "" {
		name = session.AccountLabel
	}
	return &accounts.Account{ID: native.RandomHex(8), Name: name, Source: "manual", Enabled: true, Status: "ok", Session: session}, nil
}

type DesktopImport struct {
	Account            *accounts.Account
	Directory, KeyHint string
	ExpiresAt          int64
}

func ImportDesktop(dir string) ([]DesktopImport, error) {
	results, err := native.ImportFromDesktop(dir)
	if err != nil {
		return nil, err
	}
	imports := make([]DesktopImport, 0, len(results))
	for _, result := range results {
		imports = append(imports, DesktopImport{Account: result.Account, Directory: result.Dir, KeyHint: native.Mask(result.AK), ExpiresAt: result.Expires.Unix()})
	}
	return imports, nil
}
