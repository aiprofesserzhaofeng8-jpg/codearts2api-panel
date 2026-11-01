package codearts

import (
	"fmt"
	"strings"
	"time"
)

// Commit is the sole browser-login persistence gate. Polling must never call it.
func (f *LoginFlow) Commit(save func(*Session) (string, error)) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.savedAccountID != "" {
		return f.savedAccountID, nil
	}
	if f.Status != LoginConfirmed || f.Session == nil {
		return "", fmt.Errorf("请先提交授权信息并检查账号，再确认完成登录")
	}
	if !f.Session.Valid(0) || (!f.Finished.IsZero() && time.Since(f.Finished) > 15*time.Minute) {
		return "", fmt.Errorf("授权凭据已过期，请重新获取链接")
	}
	id, err := save(f.Session)
	if err != nil {
		return "", err
	}
	f.saved = true
	f.savedAccountID = id
	return id, nil
}

func (f *LoginFlow) SavedAccountID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.savedAccountID
}

func (f *LoginFlow) StageSession(sess *Session) error {
	if sess == nil || !sess.Valid(0) {
		return fmt.Errorf("凭据不完整或已过期")
	}
	f.mu.Lock()
	if f.closed || f.exchanging {
		f.mu.Unlock()
		return fmt.Errorf("本次授权已经处理，请重新获取链接后提交另一份凭据")
	}
	f.exchanging = true
	f.mu.Unlock()
	f.finish(LoginConfirmed, "凭据已验证，等待你确认保存", sess)
	return nil
}

func (f *LoginFlow) SubmitCode(code string) error {
	code = strings.TrimSpace(code)
	if len(code) < 8 || len(code) > 8192 || strings.ContainsAny(code, " \r\n\t") {
		return fmt.Errorf("请粘贴完整回调 URL、OAuth 授权码或完整凭据 JSON；不支持任意设备短码")
	}
	return f.beginExchange(code, "")
}
