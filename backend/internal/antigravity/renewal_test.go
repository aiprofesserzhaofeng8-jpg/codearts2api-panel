package antigravity_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/antigravity"
	"codearts2api/internal/storage"
)

func TestCLIConcurrentRenewalUsesSingleCASOwner(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.WriteString(w, `{"access_token":"new-access","refresh_token":"rotated-refresh","expires_in":3600,"token_type":"Bearer"}`)
	}))
	defer up.Close()
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := antigravity.New()
	c.TokenURL = up.URL
	raw, _ := json.Marshal(map[string]any{"scope": "global", "access_token": "old-access", "refresh_token": "old-refresh", "subject": "user-1", "project_id": "project-1", "expires_at": time.Now().Unix() + 60})
	raw, id, err := c.Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertAccountAndSave(&accounts.Account{ID: "global", Provider: "antigravity", Scope: "global", ExternalID: id.ID, Credentials: raw, Enabled: true}, false); err != nil {
		t.Fatal(err)
	}
	runtime := accounts.NewProviderCredentials(store, func() bool { return true })
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, err := runtime.Resolve(context.Background(), "global", c)
			if err != nil || !strings.Contains(string(b), "rotated-refresh") {
				t.Error("rotation lost", err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("duplicate refresh", calls.Load())
	}
	reopened, err := storage.Open(store.Path())
	if err != nil || !strings.Contains(string(reopened.Account("global").Credentials), "rotated-refresh") {
		t.Fatal("rotation not persisted", err)
	}
}
