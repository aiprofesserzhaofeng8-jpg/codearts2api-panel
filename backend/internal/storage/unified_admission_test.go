package storage

import (
	"codearts2api/internal/access"
	"codearts2api/internal/accounts"
	"codearts2api/internal/config"
	"testing"
)

func unifiedSelection(s *Store, m config.UnifiedModel) access.UnifiedSelection {
	return access.UnifiedSelection{ID: m.ID, Revision: m.Revision, RoutingRevision: s.UnifiedSnapshot().Revision}
}
func TestUnifiedAtomicSelectionUngroupedAndBusy(t *testing.T) {
	s := testPool(t, 0)
	m := createUnified(t, s)
	selection := unifiedSelection(s, m)
	for _, p := range []string{"workbuddy", "trae"} {
		s.UpsertAccount(&accounts.Account{ID: p, Provider: p, Scope: "cn", Enabled: true, MaxInFlight: 1})
	}
	ids := []string{m.Sources[0].ID, m.Sources[1].ID}
	key, _ := s.AddKeyInGroupAndSave("fixture", "")
	a, model, release, err := s.AcquireUnifiedAccount("", m.Name, ids, nil, key.Key, selection)
	if err != nil || a == nil || model != ids[0] {
		t.Fatal(a, model, err)
	}
	b, _, release2, err := s.AcquireUnifiedAccount("", m.Name, ids, nil, key.Key, selection)
	if err != nil || b == nil || a.ID == b.ID {
		t.Fatal("busy source did not switch", err)
	}
	if extra, _, r, e := s.AcquireUnifiedAccount("", m.Name, ids, nil, key.Key, selection); e != nil || extra != nil {
		r()
		t.Fatal("lease limit bypassed")
	}
	release()
	release()
	release2()
	for _, x := range s.PoolSnapshot() {
		if x.InFlight != 0 {
			t.Fatal("lease leaked")
		}
	}
}
func TestUnifiedAtomicRevisionAndPermissions(t *testing.T) {
	s := testPool(t, 0)
	m := createUnified(t, s)
	s.UpsertAccount(&accounts.Account{ID: "wb", Provider: "workbuddy", Scope: "cn", Enabled: true})
	g, err := s.SaveUnifiedGroup(AccountGroup{Name: "fixture", AccountIDs: []string{"wb"}, UnifiedModels: []access.UnifiedGrant{{ModelID: m.ID, Visible: false, Callable: true, SourceMode: "selected", SourceIDs: []string{m.Sources[0].ID}}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	k, _ := s.AddKeyInGroupAndSave("fixture", g.ID)
	selection := unifiedSelection(s, m)
	ids := []string{m.Sources[0].ID, m.Sources[1].ID}
	g.ModelMappings = nil
	g.ModelRules = nil
	g.UnifiedModels[0].Callable = false
	g, err = s.SaveUnifiedGroup(g, g.Revision)
	if err != nil {
		t.Fatal(err)
	}
	m.Enabled = false
	if _, err = s.SaveUnifiedModel(m, nil, m.Revision, s.UnifiedSnapshot().Revision, nil); err != nil {
		t.Fatal(err)
	}
	_, _, release, err := s.AcquireUnifiedAccount("", m.Name, ids, nil, k.Key, selection)
	release()
	expectModelReason(t, err, "model_permission_denied")
	g.ModelMappings = nil
	g.ModelRules = nil
	g.UnifiedModels[0].Callable = true
	g, err = s.SaveUnifiedGroup(g, g.Revision)
	if err != nil {
		t.Fatal(err)
	}
	_, _, release, err = s.AcquireUnifiedAccount("", m.Name, ids, nil, k.Key, selection)
	release()
	expectModelReason(t, err, "model_configuration_changed")
	for _, x := range s.PoolSnapshot() {
		if x.InFlight != 0 {
			t.Fatal("rejected plan occupied lease")
		}
	}
}
func TestUnifiedAtomicNeverAddsNewGrantCandidates(t *testing.T) {
	s := testPool(t, 0)
	m := createUnified(t, s)
	s.UpsertAccount(&accounts.Account{ID: "trae", Provider: "trae", Scope: "cn", Enabled: true})
	g, err := s.SaveUnifiedGroup(AccountGroup{Name: "fixture", AccountIDs: []string{"trae"}, UnifiedModels: []access.UnifiedGrant{{ModelID: m.ID, Visible: true, Callable: true, SourceMode: "selected", SourceIDs: []string{m.Sources[0].ID}}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	k, _ := s.AddKeyInGroupAndSave("fixture", g.ID)
	selection := unifiedSelection(s, m)
	g.ModelMappings = nil
	g.ModelRules = nil
	g.UnifiedModels[0].SourceMode = "all"
	g.UnifiedModels[0].SourceIDs = []string{}
	if _, err = s.SaveUnifiedGroup(g, g.Revision); err != nil {
		t.Fatal(err)
	}
	a, _, r, err := s.AcquireUnifiedAccount("", m.Name, []string{m.Sources[0].ID}, nil, k.Key, selection)
	r()
	if err != nil || a != nil {
		t.Fatal("newly authorized source entered old request", a, err)
	}
}
