package storage

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"codearts2api/internal/access"
)

func TestFirstInstallationResumesSealsAndPreservesState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	token, tokenPath, err := s.PrepareInstallation()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("setup credential is not private")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("pending installer wrote application state")
	}
	restarted, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	reused, _, err := restarted.PrepareInstallation()
	if err != nil || reused != token {
		t.Fatal("interrupted install rotated owner credential", err)
	}
	i := access.NewInstallation(restarted, token)
	if err := i.Install(context.Background(), token, "new-owner-password", "new-owner-password"); err != nil {
		t.Fatal(err)
	}
	if err := restarted.SealInstallation(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tokenPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("setup credential remains")
	}
	if _, err := os.Stat(path + ".access.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("wizard wrote plaintext password")
	}
	raw, _ := os.ReadFile(path)
	if bytes.Contains(raw, []byte("new-owner-password")) || bytes.Contains(raw, []byte(token)) {
		t.Fatal("plaintext secret in state")
	}
	loaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !access.VerifyAdminPassword(loaded.Settings().AdminPassword, "new-owner-password") || len(loaded.Keys()) != 1 || loaded.Settings().AutoClaim || loaded.Settings().WriteBackToIDE {
		t.Fatal("invalid installed state")
	}
	if _, _, err := loaded.PrepareInstallation(); err == nil {
		t.Fatal("installed store reopened setup")
	}
	if err := loaded.InstallInitialAdmin(loaded.Settings().AdminPassword); err == nil {
		t.Fatal("existing state overwritten")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(raw, after) {
		t.Fatal("rejected reinstall mutated state")
	}
	if err := os.Remove(path + ".installed"); err != nil {
		t.Fatal(err)
	}
	if err := loaded.CheckFreshInstallation(); err == nil {
		t.Fatal("removing marker unlocked existing state")
	}
}

func TestInstallationRejectsMissingSnapshotWithExistingArtifacts(t *testing.T) {
	for _, suffix := range []string{".installed", ".access.json", ".admin-recovery.json", ".growth.json", ".usage.jsonl"} {
		t.Run(suffix, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if err := os.WriteFile(path+suffix, []byte("existing installation"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := CheckInstallationPath(path); err == nil {
				t.Fatal("missing production snapshot treated as new install")
			}
		})
	}
}

func TestInitialCommitFailureRollsBackAndCanRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := access.HashAdminPassword("owner-password-here")
	if err != nil {
		t.Fatal(err)
	}
	// Force serialization to fail after credentials have been prepared, without
	// relying on chmod (which does not fail writes when CI runs as root).
	s.st.Stats.EstimatedUSD = math.NaN()
	if err := s.InstallInitialAdmin(hash); err == nil {
		t.Fatal("failed persistence accepted")
	}
	if s.Settings().AdminPassword != "" || len(s.Keys()) != 0 {
		t.Fatal("failed transaction left credentials in memory")
	}
	s.st.Stats.EstimatedUSD = 0
	if err := s.InstallInitialAdmin(hash); err != nil {
		t.Fatal("retry failed", err)
	}
}
