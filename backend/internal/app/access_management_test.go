package app_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"codearts2api/internal/access"
	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"codearts2api/internal/store"
)

func TestAccessManagementUsesCurrentCatalogAndPreservesMappingOmission(t *testing.T) {
	srv, st, _ := managementFixture(t)
	srv.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "one"}, {ID: "two"}})
	st.UpdateSettings(func(s *store.Settings) { s.ModelAliases["configured-alias"] = "workbuddy/cn/one" })
	g, err := srv.AccessManagement.SaveGroup(access.AccountGroup{Name: "mapped", AccountIDs: []string{"workbuddy"}, AllowlistEnabled: true,
		ModelIDs: []string{" configured-alias ", "unlisted-future-model"}, ModelMappings: []access.ModelMapping{{Name: "shared-4.1", Sources: []string{"workbuddy/cn/one"}}}})
	if err != nil || g.ModelIDs[0] != "workbuddy/cn/one" || g.ModelIDs[1] != "unlisted-future-model" {
		t.Fatal("catalog normalization changed", g, err)
	}
	g.Name, g.ModelMappings = "legacy update", nil
	if g, err = srv.AccessManagement.SaveGroup(g); err != nil || len(g.ModelMappings) != 1 {
		t.Fatal("omitted mappings were cleared", g, err)
	}
	g.ModelMappings = []access.ModelMapping{}
	if g, err = srv.AccessManagement.SaveGroup(g); err != nil || len(g.ModelMappings) != 0 {
		t.Fatal("explicit empty mappings were retained", g, err)
	}
	st.UpdateSettings(func(s *store.Settings) { s.ModelAliases["configured-alias"] = "workbuddy/cn/two" })
	g.ModelIDs = []string{"configured-alias"}
	if g, err = srv.AccessManagement.SaveGroup(g); err != nil || g.ModelIDs[0] != "workbuddy/cn/two" {
		t.Fatal("group manager cached old aliases", g, err)
	}
	before, _ := os.ReadFile(st.Path())
	g.ModelMappings = []access.ModelMapping{{Name: "shared-4.1", Sources: []string{"configured-alias"}}}
	if _, err = srv.AccessManagement.SaveGroup(g); err == nil || !strings.Contains(err.Error(), "原始型号不在当前目录") {
		t.Fatal("alias accepted as a physical mapping source", err)
	}
	after, _ := os.ReadFile(st.Path())
	if string(before) != string(after) || len(st.Groups()[0].ModelMappings) != 0 {
		t.Fatal("rejected catalog source changed persisted access")
	}
}

func TestAccessManagementStrictTransactionsRetainPublishedAccessOnFailure(t *testing.T) {
	srv, st, _ := managementFixture(t)
	g, err := srv.AccessManagement.SaveGroup(access.AccountGroup{Name: "bound", AccountIDs: []string{"native"}, AllowlistEnabled: true, ModelIDs: []string{"deepseek-v4.1-flash"}})
	if err != nil {
		t.Fatal(err)
	}
	other, err := srv.AccessManagement.SaveGroup(access.AccountGroup{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	key, err := srv.AccessManagement.AddKey("client", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	groups, keys := st.Groups(), st.Keys()
	restore := blockManagementWrites(t, st)
	changed := g
	changed.Name = "must not publish"
	for _, operation := range []func() error{
		func() error { _, err := srv.AccessManagement.SaveGroup(changed); return err },
		func() error { return srv.AccessManagement.DeleteGroup(other.ID) },
		func() error { _, err := srv.AccessManagement.AddKey("must not publish", g.ID); return err },
		func() error { return srv.AccessManagement.SetKeyGroup(access.KeyReference(key.Key), other.ID) },
		func() error {
			return srv.AccessManagement.SetKeyBudget(codearts.Mask(key.Key), access.KeyBudget{MaxTokens: 77}, true)
		},
	} {
		if err := operation(); err == nil || !reflect.DeepEqual(groups, st.Groups()) || !reflect.DeepEqual(keys, st.Keys()) {
			t.Fatal("failed access transaction changed a published snapshot", err)
		}
	}
	restore()
	reopened, err := store.Open(st.Path())
	if err != nil || !reflect.DeepEqual(groups, reopened.Groups()) || !reflect.DeepEqual(keys, reopened.Keys()) {
		t.Fatal("failed access operation changed durable state", err)
	}
	release, err := st.BeginKeyBudget(key.Key)
	if err != nil {
		t.Fatal(err)
	}
	if err = srv.AccessManagement.SetKeyBudget(access.KeyReference(key.Key), access.KeyBudget{MaxRequests: 1}, true); err == nil || !strings.Contains(err.Error(), "在途请求") {
		t.Fatal("manager bypassed the in-flight budget reset guard", err)
	}
	release()
	if err = srv.AccessManagement.SetKeyBudget(access.KeyReference(key.Key), access.KeyBudget{MaxRequests: 1}, true); err != nil {
		t.Fatal(err)
	}
}

func TestAccessManagementLegacyKeyChangesKeepSaveFailureSemantics(t *testing.T) {
	_, st, _ := managementFixture(t)
	first, _ := st.AddKeyInGroupAndSave("delete", "")
	second, _ := st.AddKeyInGroupAndSave("toggle", "")
	logged := 0
	mgr := access.NewManagement(st, access.ModelResolver{}, codearts.Mask, func(string, ...any) { logged++ })
	for _, ref := range []string{second.Key, access.KeyReference(second.Key), codearts.Mask(second.Key)} {
		if key, ok := mgr.RevealKey(ref); !ok || key != second.Key {
			t.Fatal("legacy key reference no longer resolves")
		}
	}
	if mgr.ResolveKeyRef("unknown") != "unknown" {
		t.Fatal("unknown reference no longer passes through")
	}
	restore := blockManagementWrites(t, st)
	mgr.ChangeKeys(access.KeyReference(first.Key), codearts.Mask(second.Key))
	if len(st.Keys()) != 1 || st.Keys()[0].Enabled || logged != 1 {
		t.Fatal("legacy deletion/toggle or logged-save-failure behavior changed")
	}
	if key, ok := mgr.RevealKey(access.KeyReference(second.Key)); !ok || key != second.Key {
		t.Fatal("administrator cannot retrieve a disabled key")
	}
	restore()
	reopened, err := store.Open(st.Path())
	if err != nil || len(reopened.Keys()) != 2 || !reopened.Keys()[1].Enabled {
		t.Fatal("failed legacy save unexpectedly modified disk", err)
	}
	if err = st.Save(); err != nil {
		t.Fatal(err)
	}
	reopened, err = store.Open(st.Path())
	if err != nil || len(reopened.Keys()) != 1 || reopened.Keys()[0].Enabled {
		t.Fatal("legacy memory changes could not be saved after recovery", err)
	}
}
