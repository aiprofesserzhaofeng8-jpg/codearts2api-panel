package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"codearts2api/internal/codearts"
	"codearts2api/internal/store"
)

// bootstrapAccess is called only for a new state file. Secrets are never printed
// into logs, embedded in the UI or committed as example configuration.
func bootstrapAccess(st *store.Store, password string) (string, error) {
	if password == "" {
		password = codearts.RandomHex(24)
	}
	st.UpdateSettings(func(s *store.Settings) { s.AdminPassword = password; s.WriteBackToIDE = false; s.AutoClaim = false })
	key := st.AddKey("默认调用密钥")
	path := st.Path() + ".access.json"
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(map[string]string{"admin_password": password, "api_key": key.Key}, "", "  ")
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return "", err
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return "", err
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	if err := st.Save(); err != nil {
		return "", err
	}
	return path, nil
}
