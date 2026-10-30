package store

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

type usagePersistenceUnavailable struct{}

func (*usagePersistenceUnavailable) Error() string {
	return "用量账本暂时无法保存，已暂停新请求；请检查磁盘或目录权限"
}
func (*usagePersistenceUnavailable) ServiceUnavailable() bool  { return true }
func (*usagePersistenceUnavailable) NonRetryableRequest() bool { return true }

var ErrUsagePersistence = &usagePersistenceUnavailable{}

// Only settlement metadata is journaled: no API key, credentials or conversation
// bodies. The monotonic sequence makes replay independent of log retention and
// prevents a saved budget reset/log cleanup from being applied a second time.
type usageJournalRecord struct {
	Sequence uint64   `json:"sequence"`
	KeyHash  string   `json:"key_hash,omitempty"`
	Entry    LogEntry `json:"entry"`
	OutChars int      `json:"out_chars"`
}

func usageKeyHash(key string) string {
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (s *Store) usageJournalPath() string { return s.path + ".usage.jsonl" }

// Record durably appends a small settlement before publishing its counters and
// returning to the handler (which still owns its budget reservation). Full JSON
// snapshots remain periodic; model generation and SSE forwarding are unchanged.
func (s *Store) Record(e LogEntry, outChars int, keys ...string) error {
	s.mu.RLock()
	needsBase := !s.usageBaseReady
	s.mu.RUnlock()
	var persistErr error
	if needsBase {
		// Mostly for direct Store users; the server bootstraps before listening.
		persistErr = s.Save()
	}
	s.journalMu.Lock()
	defer s.journalMu.Unlock()
	s.mu.Lock()
	if persistErr == nil {
		persistErr = s.journalErr
	}
	if e.Usage.Known {
		// Own the attempt slice before attaching historical price snapshots.
		e.AttemptDetails = append([]RequestAttempt(nil), e.AttemptDetails...)
		e.EstimatedCost = s.costForEntryLocked(&e)
	}
	sequence := s.st.UsageSequence + 1
	s.mu.Unlock()
	key := ""
	if len(keys) > 0 {
		key = keys[0]
	}
	if persistErr == nil {
		persistErr = s.appendUsageJournal(usageJournalRecord{
			Sequence: sequence, KeyHash: usageKeyHash(key), Entry: e, OutChars: outChars,
		})
	}
	s.mu.Lock()
	// Real upstream consumption must stay visible even if storage fails. Further
	// inference is stopped until Save has durably captured all pending counters.
	s.recordPreparedLocked(e, outChars, key)
	s.st.UsageSequence = sequence
	if persistErr != nil {
		s.journalErr = persistErr
	}
	s.mu.Unlock()
	return persistErr
}

// Caller holds journalMu. Opening per append avoids keeping an obsolete file
// descriptor across compaction and needs no Store.Close lifecycle or goroutine.
func (s *Store) appendUsageJournal(record usageJournalRecord) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.usageJournalPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() != s.journalSize {
		return errors.New("用量账本长度已变化，请勿由多个进程共用状态文件")
	}
	n, err := f.Write(append(raw, '\n'))
	s.journalSize += int64(n) // includes a partial write, to checkpoint it safely
	if err != nil {
		return err
	}
	if n != len(raw)+1 {
		return io.ErrShortWrite
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if record.Sequence == 1 || info.Size() == 0 {
		// Also cover the initial directory entry, not just file contents.
		return syncUsageDirectory(filepath.Dir(s.path))
	}
	return nil
}

// Startup only. A missing newline is an interrupted append, not a committed
// settlement. Complete but invalid records or sequence gaps fail startup, so
// corruption cannot silently reset a budget to an apparently unused state.
func (s *Store) replayUsageJournal() error {
	f, err := os.OpenFile(s.usageJournalPath(), os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() > 0 && !s.usageBaseReady {
		return errors.New("用量账本存在但状态快照缺失，请恢复配套状态文件")
	}
	reader := bufio.NewReader(f)
	var previous uint64
	for {
		line, err := reader.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			if len(line) > 0 {
				if err := f.Truncate(s.journalSize); err != nil {
					return err
				}
				return f.Sync()
			}
			return nil
		}
		if err != nil {
			return err
		}
		var record usageJournalRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return fmt.Errorf("字节偏移 %d 的账本记录无效: %w", s.journalSize, err)
		}
		if record.Sequence == 0 || (previous != 0 && record.Sequence != previous+1) {
			return fmt.Errorf("字节偏移 %d 的账本序号无效", s.journalSize)
		}
		previous = record.Sequence
		s.journalSize += int64(len(line))
		if record.Sequence <= s.st.UsageSequence {
			continue
		}
		if record.Sequence != s.st.UsageSequence+1 {
			return errors.New("用量账本与状态快照的序号不连续")
		}
		key := ""
		if record.KeyHash != "" {
			for _, k := range s.st.Keys {
				if usageKeyHash(k.Key) == record.KeyHash {
					key = k.Key
					break
				}
			}
		}
		s.recordPreparedLocked(record.Entry, record.OutChars, key)
		s.st.UsageSequence = record.Sequence
	}
}

// Remove only the prefix covered by a durably renamed snapshot. Appends can
// continue while that snapshot is written; their suffix is copied unchanged.
// Caller holds journalMu, but not mu, so reads/admission remain available.
func (s *Store) compactUsageJournal(offset int64) error {
	if offset == 0 {
		info, err := os.Stat(s.usageJournalPath())
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() != s.journalSize {
			return errors.New("用量账本文件不可用")
		}
		return nil
	}
	f, err := os.Open(s.usageJournalPath())
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() != s.journalSize || offset > info.Size() {
		return errors.New("用量账本长度异常，未清理已保存记录")
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "usage-journal-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	n, err := io.Copy(tmp, f)
	if err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.usageJournalPath()); err != nil {
		return err
	}
	s.journalSize = n
	return syncUsageDirectory(filepath.Dir(s.path))
}

func syncUsageDirectory(path string) error {
	// Windows does not support fsync on directory handles. File data still uses
	// Sync, and the process-crash recovery contract is the same on both systems.
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
