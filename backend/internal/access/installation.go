package access

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"sync"
	"sync/atomic"
)

var (
	ErrInstallationClosed = errors.New("初始安装已完成，不能重复初始化")
	ErrInstallationToken  = errors.New("初始化凭证无效，请查看服务器上的初始化凭证文件")
	ErrInstallationBusy   = errors.New("正在完成初始安装，请稍后查看安装状态")
	ErrPasswordConfirm    = errors.New("两次输入的密码不一致")
)

// InstallationStore commits only into an empty, previously uninstalled store.
// The persistent owner rechecks that condition in the same write transaction.
type InstallationStore interface {
	InstallInitialAdmin(passwordHash string) error
}

// Installation exists only in the temporary installer runtime. It never owns
// login sessions, accepts roles or participates in inference admission.
type Installation struct {
	store    InstallationStore
	token    [32]byte
	mu       sync.Mutex
	complete atomic.Bool
}

func NewInstallation(store InstallationStore, token string) *Installation {
	return &Installation{store: store, token: sha256.Sum256([]byte(token))}
}

func (i *Installation) Pending() bool { return !i.complete.Load() }

func (i *Installation) Install(ctx context.Context, token, password, confirmation string) error {
	if !i.Pending() {
		return ErrInstallationClosed
	}
	digest := sha256.Sum256([]byte(token))
	if token == "" || subtle.ConstantTimeCompare(i.token[:], digest[:]) != 1 {
		return ErrInstallationToken
	}
	if err := ValidateUserPassword(password); err != nil {
		return err
	}
	if password != confirmation {
		return ErrPasswordConfirm
	}
	if !i.mu.TryLock() {
		return ErrInstallationBusy
	}
	defer i.mu.Unlock()
	if !i.Pending() {
		return ErrInstallationClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	hash, err := HashAdminPassword(password)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := i.store.InstallInitialAdmin(hash); err != nil {
		return err
	}
	i.complete.Store(true)
	return nil
}
