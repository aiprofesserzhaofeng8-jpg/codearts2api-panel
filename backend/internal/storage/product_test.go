package storage

import (
	"codearts2api/internal/access"
	"codearts2api/internal/config"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func productFlag(v bool) *bool { return &v }
func TestProductAtomicRevocationRollbackAndSnapshot(t *testing.T) {
	s := testPool(t, 1)
	source := "deepseek-v4.1-flash"
	public := "deepseek-4.1-flash"
	g, err := s.SaveGroup(AccountGroup{Name: "product", AccountIDs: []string{"0"}, ModelMappings: []ModelMapping{{Name: public, Sources: []string{source}}}, DefaultModel: public, ClientDefaultModel: public})
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.AddKeyInGroupAndSave("key", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	denied := cloneGroup(g)
	denied.ModelRules = []access.ModelRule{{ID: public, Callable: productFlag(false)}}
	if _, err = s.SaveGroup(denied); err == nil {
		t.Fatal("disabled default accepted")
	}
	denied.DefaultModel = ""
	denied.ClientDefaultModel = ""
	saved, err := s.SaveGroup(denied)
	if err != nil {
		t.Fatal(err)
	}
	if a, _, release, err := s.AcquireMappedAccount("sticky", public, []string{source}, nil, key.Key); a != nil || err == nil {
		release()
		t.Fatal("stale mapped plan bypassed disable")
	}
	saved.ModelRules = []access.ModelRule{{ID: source, Callable: productFlag(false)}}
	if _, err = s.SaveGroup(saved); err != nil {
		t.Fatal(err)
	}
	if a, _, release, err := s.AcquireMappedAccount("sticky", public, []string{source}, nil, key.Key); a != nil || err == nil {
		release()
		t.Fatal("disabled source selected")
	}
	snapshot := s.Groups()
	*snapshot[0].ModelRules[0].Callable = true
	scope, _ := s.AccessScope(key.Key)
	if scope.Callable(source) {
		t.Fatal("snapshot alias modified live permission")
	}
	oldpath := s.path
	blocker := filepath.Join(t.TempDir(), "file")
	if err = os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocker, "state.json")
	before := s.Groups()
	saved.ModelRules = nil
	if _, err = s.SaveGroup(saved); err == nil || !reflect.DeepEqual(s.Groups(), before) {
		t.Fatal("failed save changed permission", err)
	}
	s.path = oldpath
	reloaded, err := Open(s.path)
	if err != nil || reloaded.Groups()[0].ModelRules[0].Callable == nil || *reloaded.Groups()[0].ModelRules[0].Callable {
		t.Fatal("reload lost deny", err)
	}
}
func TestProductRulePublicationRace(t *testing.T) {
	s := testPool(t, 1)
	source := "deepseek-v4.1-flash"
	g, err := s.SaveGroup(AccountGroup{Name: "concurrent", AccountIDs: []string{"0"}})
	if err != nil {
		t.Fatal(err)
	}
	k, _ := s.AddKeyInGroupAndSave("key", g.ID)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			scope, _ := s.AccessScope(k.Key)
			if len(scope.ModelRules) > 0 && scope.ModelRules[0].Callable != nil {
				*scope.ModelRules[0].Callable = true
			}
			a, release, _ := s.AcquireAccountForKey("", source, nil, k.Key)
			if a != nil {
				release()
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			g.ModelRules = []access.ModelRule{{ID: source, Callable: productFlag(i%2 == 0)}}
			if _, err := s.SaveGroup(g); err != nil {
				t.Error(err)
			}
		}
	}()
	wg.Wait()
	if s.PoolSnapshot()["0"].InFlight != 0 {
		t.Fatal("lease leaked")
	}
}
func TestProductSchemaSixMigrationPreservesLegacyPrompts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	raw := `{"version":6,"settings":{"workbuddy":{"prompt_mode":"custom","prompt_text":"WB","prompt_fallback":false,"sanitize":false,"search_scope":"cn","search_backend":"native"},"trae":{"prompt_mode":"passthrough","prompt_text":"TR","prompt_fallback":false,"sanitize":false,"search_scope":"cn","search_backend":"searxng"}},"groups":[{"id":"g","name":"legacy","account_ids":[],"model_ids":[]}]}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st := s.Settings()
	if st.WorkBuddy.PromptOverrides != nil || st.WorkBuddy.PromptText != "WB" || st.ProviderPolicy("workbuddy").Sanitize || st.ProviderPolicy("trae").PromptFallback {
		t.Fatal("migration changed policy", st.WorkBuddy)
	}
	if s.st.Version != 7 || s.Groups()[0].DefaultModel != "" || len(s.Groups()[0].ModelRules) != 0 {
		t.Fatal("migration added routing behavior")
	}
	off := false
	s.UpdateSettings(func(st *Settings) { st.Trae.PromptOverrides = &config.PromptOverrides{Sanitize: &off} })
	snap := s.Settings()
	*snap.Trae.PromptOverrides.Sanitize = true
	if s.Settings().ProviderPolicy("trae").Sanitize {
		t.Fatal("settings alias")
	}
}
