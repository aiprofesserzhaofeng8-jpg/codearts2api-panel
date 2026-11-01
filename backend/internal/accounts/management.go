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
// Save is deliberately separate for legacy operations with logged save failure.
type ManagementStore interface {
	Account(string) *Account
	UpsertAccount(*Account) bool
	SetAccountConcurrencyAndSave(string, int) (bool, error)
	RemoveAccountAndSave(string) (bool, error)
	SetAccountEnabled(string, bool) bool
	SetAccountPaused(string, bool) bool
	ClearModelHealth(string)
	ClearAccountErrors(string) bool
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

func (s *Management) persist() {
	if err := s.store.Save(); err != nil && s.logf != nil {
		s.logf("保存状态文件失败: %v", err)
	}
}

// Add and Import retain the old upsert -> invalidate -> logged-save sequence.
func (s *Management) Add(a *Account) bool {
	isNew := s.store.UpsertAccount(a)
	s.invalidate(a.ID)
	s.persist()
	return isNew
}

func (s *Management) Import(list []*Account) []bool {
	created := make([]bool, 0, len(list))
	for _, a := range list {
		created = append(created, s.store.UpsertAccount(a))
		s.invalidate(a.ID)
	}
	s.persist()
	return created
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

func (s *Management) Toggle(id string) error {
	a := s.store.Account(id)
	if a == nil {
		return &ManagementError{ManagementInvalid, "账号不存在"}
	}
	s.store.SetAccountEnabled(a.ID, !a.Enabled)
	s.persist()
	return nil
}

func (s *Management) ClearErrors(id string) {
	s.store.ClearModelHealth(id)
	if s.store.ClearAccountErrors(id) {
		s.persist()
	}
}

func (s *Management) SetPaused(id string, paused bool) error {
	if !s.store.SetAccountPaused(id, paused) {
		return &ManagementError{ManagementMissing, "账号不存在"}
	}
	s.persist()
	return nil
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
	s.persist()
	return nil
}
