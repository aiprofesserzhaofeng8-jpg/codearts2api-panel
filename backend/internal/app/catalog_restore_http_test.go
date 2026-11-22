package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/catalog"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
)

func TestRestoredAuthorizedCatalogWhileDiscoveryBlocked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	open := func() *storage.Store {
		s, err := storage.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	st := open()
	a := &accounts.Account{ID: "owner", Provider: "trae", Scope: "cn", Enabled: true}
	st.UpsertAccount(a)
	var models []provider.Model
	for i := range 129 {
		models = append(models, provider.Model{ID: fmt.Sprintf("restored-%03d", i)})
	}
	if err := st.SaveCatalogPartition(a, models, time.Now()); err != nil {
		t.Fatal(err)
	}
	group, err := st.SaveGroup(storage.AccountGroup{Name: "one allowed", AccountIDs: []string{a.ID}, AllowlistEnabled: true, ModelIDs: []string{"trae/cn/restored-123"}, RawModelMode: "legacy", RawModelsVisibleDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	key, err := st.AddKeyInGroupAndSave("reader", group.ID)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := Build(open())
	if err != nil {
		t.Fatal(err)
	}
	process, stop := context.WithCancel(t.Context())
	defer stop()
	srv.Catalog.BindRefreshContext(process)
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- srv.Catalog.JoinRefresh(process, catalog.RefreshOperations{Provider: func(ctx context.Context, _ *accounts.Account) ([]provider.Model, error) {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		}})
	}()
	<-entered
	server := httptest.NewServer(srv.Handler())
	defer server.Close()
	client := server.Client()
	client.Timeout = time.Second
	req, _ := http.NewRequest("GET", server.URL+"/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+key.Key)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list struct{ Data []struct{ ID string } }
	if err = json.NewDecoder(resp.Body).Decode(&list); err != nil || resp.StatusCode != 200 || len(list.Data) != 1 || list.Data[0].ID != "trae/cn/restored-123" {
		t.Fatal(resp.StatusCode, list, err)
	}
	stop()
	<-done
}
