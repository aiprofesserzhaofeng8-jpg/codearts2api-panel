package access

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

var (
	ErrInstallationClosed = errors.New("初始安装已完成，不能重复初始化")
	ErrInstallationBusy   = errors.New("正在完成初始安装，请稍后查看安装状态")
	ErrPasswordConfirm    = errors.New("两次输入的密码不一致")
)

// InstallationStore commits only into an empty, previously uninstalled store.
// The persistent owner rechecks that condition in the same write transaction.
type InstallationStore interface {
	InstallInitialAdmin(email, passwordHash string) error
}

// Installation exists only in the temporary installer runtime. It never owns
// login sessions, accepts roles or participates in inference admission.
type Installation struct {
	store    InstallationStore
	mu       sync.Mutex
	complete atomic.Bool
}

func NewInstallation(store InstallationStore) *Installation {
	return &Installation{store: store}
}

func (i *Installation) Pending() bool { return !i.complete.Load() }

func (i *Installation) Install(ctx context.Context, email, password, confirmation string) error {
	if !i.Pending() {
		return ErrInstallationClosed
	}
	email, err := NormalizeEmail(email)
	if err != nil {
		return err
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
	if err := i.store.InstallInitialAdmin(email, hash); err != nil {
		return err
	}
	i.complete.Store(true)
	return nil
}
