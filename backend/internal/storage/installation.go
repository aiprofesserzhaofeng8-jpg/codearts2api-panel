package storage

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codearts2api/internal/access"
)

// CheckFreshInstallation prevents a deleted state file from reopening setup.
// Existing installations must be recovered from their latest state/ledger.
func (s *Store) CheckFreshInstallation() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.checkFreshInstallationLocked()
}

func (s *Store) checkFreshInstallationLocked() error {
	if s.st.Settings.AdminPassword != "" || len(s.st.UnifiedModels)+len(s.st.Accounts)+len(s.st.Keys)+len(s.st.Users)+len(s.st.Groups)+len(s.st.UsageRecords)+len(s.st.Logs) != 0 || s.st.UsageSequence != 0 || s.st.Stats.TotalRequests != 0 {
		return access.ErrInstallationClosed
	}
	return checkInitialFiles(s.path, true)
}

// CheckInstallationPath runs before Open can replay a journal into a missing
// snapshot. A missing production snapshot must not turn into a fresh install.
func CheckInstallationPath(path string) error {
	return checkInitialFiles(path, false)
}

func checkInitialFiles(path string, requireAbsent bool) error {
	if _, err := os.Lstat(path); err == nil && !requireAbsent {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, suffix := range []string{"", ".installed", ".access.json", ".admin-recovery.json", ".growth.json", ".usage.jsonl"} {
		if _, err := os.Lstat(path + suffix); err == nil {
			return fmt.Errorf("检测到已有安装文件，请恢复完整状态后启动: %s", path+suffix)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// PrepareInstallation records pending setup for deployment backup/health tools.
// No ownership token is needed; only a fresh instance can enter this runtime.
func (s *Store) PrepareInstallation() error {
	if err := s.CheckFreshInstallation(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	path := s.path + ".setup.json"
	if raw, err := os.ReadFile(path); err == nil {
		// Accept pending files from previous releases without rotating credentials.
		if !json.Valid(raw) {
			return fmt.Errorf("初始化标记文件无效")
		}
		return os.Chmod(path, 0600)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeInstallationFile(path, []byte("{\"pending\":true}\n"), 0600)
}

func writeInstallationFile(path string, raw []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err = f.Write(raw); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	ok = true
	return syncUsageDirectory(filepath.Dir(path))
}

// InstallInitialAdmin is an all-or-nothing initial state transaction. No
// plaintext administrator password is written. A default key keeps the normal
// gateway's legacy no-key local access from becoming active after installation.
func (s *Store) InstallInitialAdmin(email, hash string) error {
	var err error
	if email, err = access.NormalizeEmail(email); err != nil {
		return err
	}
	if !strings.HasPrefix(hash, access.PasswordHashPrefix) {
		return fmt.Errorf("初始密码必须先完成哈希")
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkFreshInstallationLocked(); err != nil {
		return err
	}
	previousSettings, previousKeys := s.st.Settings, s.st.Keys
	s.st.Settings.AdminPassword = hash
	s.st.Settings.AdminEmail = email
	s.st.Settings.AutoClaim, s.st.Settings.WriteBackToIDE = false, false
	s.st.Keys = []*APIKey{{Key: "sk-ca-" + rand.Text() + rand.Text(), Name: "默认调用密钥", Enabled: true, CreatedAt: time.Now().Unix()}}
	if err := s.saveGroupStateLocked(); err != nil {
		s.st.Settings, s.st.Keys = previousSettings, previousKeys
		return err
	}
	s.usageBaseReady = true
	return nil
}

// SealInstallation is also applied to upgrades. State credentials are the
// primary installed condition; the independent marker stops accidental
// reinitialization when only the state file is removed. Marker deletion cannot
// unlock a store that still contains credentials or other installation data.
func (s *Store) SealInstallation() error {
	if s.Settings().AdminPassword == "" {
		return fmt.Errorf("尚未设置管理员密码")
	}
	path := s.path + ".installed"
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := writeInstallationFile(path, []byte("installed_at="+time.Now().UTC().Format(time.RFC3339)+"\n"), 0400); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := os.Remove(s.path + ".setup.json"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
