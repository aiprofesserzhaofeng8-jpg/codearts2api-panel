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
	tokenPath := path + ".setup.json"
	err = s.PrepareInstallation()
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
	err = restarted.PrepareInstallation()
	if err != nil {
		t.Fatal("interrupted install could not resume", err)
	}
	i := access.NewInstallation(restarted)
	if err := i.Install(context.Background(), "Owner@Example.com", "new-owner-password", "new-owner-password"); err != nil {
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
	if bytes.Contains(raw, []byte("new-owner-password")) {
		t.Fatal("plaintext secret in state")
	}
	loaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !access.VerifyAdminPassword(loaded.Settings().AdminPassword, "new-owner-password") || loaded.Settings().AdminEmail != "owner@example.com" || len(loaded.Keys()) != 1 || loaded.Settings().AutoClaim || loaded.Settings().WriteBackToIDE {
		t.Fatal("invalid installed state")
	}
	if err := loaded.PrepareInstallation(); err == nil {
		t.Fatal("installed store reopened setup")
	}
	if err := loaded.InstallInitialAdmin("other@example.com", loaded.Settings().AdminPassword); err == nil {
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
	if err := s.InstallInitialAdmin("owner@example.com", hash); err == nil {
		t.Fatal("failed persistence accepted")
	}
	if s.Settings().AdminPassword != "" || s.Settings().AdminEmail != "" || len(s.Keys()) != 0 {
		t.Fatal("failed transaction left credentials in memory")
	}
	s.st.Stats.EstimatedUSD = 0
	if err := s.InstallInitialAdmin("owner@example.com", hash); err != nil {
		t.Fatal("retry failed", err)
	}
}
