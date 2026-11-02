package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Path 返回状态文件路径。
func (s *Store) Path() string { return s.path }

// Save 原子写回状态文件。
//
// 并发安全：saveMu 保证同一时刻只有一个 goroutine 在写；临时文件名带随机后缀，
// 即使有第二个进程共享同一个状态文件也不会互相截断。
func (s *Store) Save() error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	s.journalMu.Lock()
	s.mu.Lock()
	if err := s.hashPasswordLocked(); err != nil {
		s.mu.Unlock()
		s.journalMu.Unlock()
		return err
	}
	raw, err := json.MarshalIndent(s.st, "", "  ")
	sequence, offset := s.st.UsageSequence, s.journalSize
	s.mu.Unlock()
	s.journalMu.Unlock()
	if err != nil {
		return err
	}
	if err := s.writeState(raw); err != nil {
		return err
	}
	// The snapshot's rename must be durable before removing its journal prefix.
	if err := syncUsageDirectory(filepath.Dir(s.path)); err != nil {
		return err
	}
	s.journalMu.Lock()
	defer s.journalMu.Unlock()
	err = s.compactUsageJournal(offset)
	s.mu.Lock()
	s.usageBaseReady = true
	if err != nil {
		s.journalErr = err
	} else if s.st.UsageSequence == sequence {
		// A later failed settlement must not be cleared by an older snapshot.
		s.journalErr = nil
	}
	s.mu.Unlock()
	return err
}

// writeState requires saveMu. Callers may additionally hold mu for a transaction.
func (s *Store) writeState(raw []byte) error {
	dir := filepath.Dir(s.path)
	// 目录只给本人：状态文件里是 STS 凭据与 refresh_token，别让同机其他用户
	// 通过目录遍历拿到文件名与备份。
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	// os.CreateTemp 以 0600 创建 —— 状态文件里含 STS 凭据与 refresh_token。
	f, err := os.CreateTemp(dir, filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }() // rename 成功后这里是 no-op

	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
