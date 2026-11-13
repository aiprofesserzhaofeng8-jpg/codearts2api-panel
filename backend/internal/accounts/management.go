package accounts

import (
	"context"
	"strings"
	"time"
)

type ManagementErrorKind string

const (
	ManagementInvalid  ManagementErrorKind = "invalid"
	ManagementMissing  ManagementErrorKind = "missing"
	ManagementInternal ManagementErrorKind = "internal"
	ManagementUpstream ManagementErrorKind = "upstream"
)

// ManagementError carries business failure semantics; HTTP owns status mapping.
type ManagementError struct {
	Kind    ManagementErrorKind
	Message string
}

func (e *ManagementError) Error() string { return e.Message }

// ManagementStore preserves the existing store transactions and lock domain.
type ManagementStore interface {
	Account(string) *Account
	ImportAccountsAndSave([]*Account) ([]bool, error)
	ChangeAccountAndSave(string, string) (bool, error)
	SetAccountConcurrencyAndSave(string, int) (bool, error)
	RemoveAccountAndSave(string) (bool, error)
	Save() error
}

// CredentialExport is an explicitly requested admin export, never a public DTO.
type CredentialExport struct {
	JSON       string
	ExpiresAt  int64
	HasRefresh bool
}

// ManagementCredentials leaves native resolution and serialization to its owner.
type ManagementCredentials interface {
	Export(context.Context, *Account) (CredentialExport, error)
	Reimport(context.Context, *Account) error
}

type AccountExport struct {
	CredentialExport
	Name   string
	Native bool
}

// Management coordinates account operations without HTTP or concrete platforms.
// It does not add admission locks, stores, caches or credential representations.
type Management struct {
	store       ManagementStore
	credentials ManagementCredentials
	invalidate  func(string)
	logf        func(string, ...any)
}

func NewManagement(st ManagementStore, credentials ManagementCredentials, invalidate func(string), logf func(string, ...any)) *Management {
	return &Management{store: st, credentials: credentials, invalidate: invalidate, logf: logf}
}

func (s *Management) Add(a *Account) (bool, error) {
	created, err := s.Import([]*Account{a})
	if err != nil {
		return false, err
	}
	return created[0], nil
}

func (s *Management) Import(list []*Account) ([]bool, error) {
	created, err := s.store.ImportAccountsAndSave(list)
	if err != nil {
		return nil, &ManagementError{ManagementInternal, "账号未保存，请检查状态文件目录权限后重试"}
	}
	for _, a := range list {
		s.invalidate(a.ID)
	}
	return created, nil
}

func (s *Management) SetConcurrency(id string, limit *int) error {
	if strings.TrimSpace(id) == "" || limit == nil || *limit < 0 || *limit > 32 {
		return &ManagementError{ManagementInvalid, "请指定账号及 0–32 的并发上限；0 表示使用全局默认值"}
	}
	found, err := s.store.SetAccountConcurrencyAndSave(id, *limit)
	if err != nil {
		return &ManagementError{ManagementInternal, "并发设置未保存，请检查状态文件目录权限后重试"}
	}
	if !found {
		return &ManagementError{ManagementMissing, "账号不存在，请刷新列表"}
	}
	return nil
}

func (s *Management) Remove(id string) error {
	if strings.TrimSpace(id) == "" {
		return &ManagementError{ManagementInvalid, "缺少账号 ID"}
	}
	found, err := s.store.RemoveAccountAndSave(id)
	if err != nil {
		return &ManagementError{ManagementInternal, "删除未完成：无法保存状态文件，请检查目录权限"}
	}
	if !found {
		return &ManagementError{ManagementMissing, "账号已不存在，请刷新列表"}
	}
	s.invalidate(id)
	return nil
}

func (s *Management) change(id, action string) error {
	found, err := s.store.ChangeAccountAndSave(id, action)
	if err != nil {
		return &ManagementError{ManagementInternal, "账号变更未保存，请检查状态文件目录权限后重试"}
	}
	if !found {
		return &ManagementError{ManagementMissing, "账号不存在，请刷新列表"}
	}
	return nil
}
func (s *Management) Toggle(id string) error      { return s.change(id, "toggle") }
func (s *Management) ClearErrors(id string) error { return s.change(id, "clear-errors") }
func (s *Management) SetPaused(id string, paused bool) error {
	if paused {
		return s.change(id, "pause")
	}
	return s.change(id, "resume")
}

func (s *Management) Export(ctx context.Context, id string) (AccountExport, error) {
	a := s.store.Account(id)
	if a == nil {
		return AccountExport{}, &ManagementError{ManagementInvalid, "账号不存在"}
	}
	if a.ProviderID() != "codearts" {
		return AccountExport{Name: a.DisplayName(), CredentialExport: CredentialExport{JSON: string(a.Credentials)}}, nil
	}
	s.invalidate(a.ID)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	value, err := s.credentials.Export(ctx, a)
	if err != nil {
		return AccountExport{}, err
	}
	return AccountExport{Name: a.DisplayName(), Native: true, CredentialExport: value}, nil
}

func (s *Management) Reimport(ctx context.Context, id string) error {
	a := s.store.Account(id)
	if a == nil {
		return &ManagementError{ManagementInvalid, "账号不存在"}
	}
	s.invalidate(a.ID)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.credentials.Reimport(ctx, a); err != nil {
		return err
	}
	if err := s.store.Save(); err != nil {
		if s.logf != nil {
			s.logf("保存状态文件失败: %v", err)
		}
		return &ManagementError{ManagementInternal, "凭据已更新但未保存，请检查状态文件目录权限；新凭据仍保留在内存"}
	}
	return nil
}
