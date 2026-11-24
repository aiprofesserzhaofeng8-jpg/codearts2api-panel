package server

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"codearts2api/internal/catalog"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

func TestClientMetadataListProfileAndAdminShareEvidenceWithoutInference(t *testing.T) {
	s, key := unifiedAPIFixture(t)
	s.ModelPolicies = config.NewModelPolicies(s.Store, s.Reg, catalog.RecommendClientPolicy)
	s.Reg.ReplaceProvider(provider.CodeArts, "", []provider.Model{{ID: "glm-5.3-flash", Name: "GLM-5.3-Flash", Source: "upstream"}})
	s.Reg.ReplaceProvider(provider.Qoder, "cn", []provider.Model{{ID: "gfmodel", Name: "GLM-5.3-Flash", Source: "upstream"}})
	s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "codewise-jump", Name: "Jump", Context: 65536, MaxOutput: 8192, Source: "upstream"}})
	_, err := s.Store.SaveUnifiedModel(config.UnifiedModel{Name: "shared-flash-v1", Enabled: true, Sources: []config.ModelSource{{ID: "codearts/glm-5.3-flash", Enabled: true}, {ID: "qoder/cn/gfmodel", Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	markModelsFresh(s)
	var list struct {
		Data []struct {
			ID            string
			Compatibility catalog.ClientCompatibility `json:"client_compatibility"`
		} `json:"data"`
	}
	grantFixtureAccounts(t, s.Store)
	w := nativeProfileGET(s, key, "/v1/models")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, row := range list.Data {
		w = nativeProfileGET(s, key, "/v1/client-profile?model="+row.ID)
		var profile struct {
			Context       int                         `json:"context_window"`
			Status        string                      `json:"client_profile_status"`
			Compatibility catalog.ClientCompatibility `json:"client_compatibility"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &profile) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		a, _ := json.Marshal(row.Compatibility)
		b, _ := json.Marshal(profile.Compatibility)
		if string(a) != string(b) || profile.Status != row.Compatibility.Status {
			t.Fatal("list and profile diverged", row.ID, string(a), string(b))
		}
		if row.ID == "shared-flash-v1" && (profile.Context != 32768 || !profile.Compatibility.Exportable) {
			t.Fatal("verified Flash recommendation unavailable", profile)
		}
		if row.ID == "workbuddy/cn/codewise-jump" && profile.Compatibility.Exportable {
			t.Fatal("jump endpoint exported as chat")
		}
	}
	w = adminProbeRequest(t, s, "GET", "/admin/api/model-policies", "")
	if w.Code != 200 || !json.Valid(w.Body.Bytes()) || len(s.Store.Logs(10)) != 0 {
		t.Fatal("metadata caused inference or failed admin projection", w.Code)
	}
	if _, _, err := s.Routing.Resolve(key, s.Store.Settings(), "workbuddy/cn/codewise-jump"); err != nil {
		t.Fatal("export recommendation restricted explicit routing", err)
	}
}

func TestClientMetadataChangedQoderIdentityDoesNotRaiseSharedLimits(t *testing.T) {
	s, key := unifiedAPIFixture(t)
	s.ModelPolicies = config.NewModelPolicies(s.Store, s.Reg, catalog.RecommendClientPolicy)
	s.Reg.ReplaceProvider(provider.Qoder, "cn", []provider.Model{{ID: "gfmodel", Name: "GLM-5.3-Flash", Context: 65536, MaxOutput: 8192, Source: "upstream"}})
	_, err := s.Store.SaveUnifiedModel(config.UnifiedModel{Name: "glm-5.3-flash", Enabled: true, Sources: []config.ModelSource{{ID: "qoder/cn/gfmodel", Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Reg.ReplaceProvider(provider.Qoder, "cn", []provider.Model{{ID: "gfmodel", Name: "An updated different model", Context: 1000000, MaxOutput: 131072, Source: "upstream"}})
	grantFixtureAccounts(t, s.Store)
	w := nativeProfileGET(s, key, "/v1/client-profile?model=glm-5.3-flash")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"client_profile_status":"missing_limits"`) || strings.Contains(w.Body.String(), `"context_window":1000000`) {
		t.Fatal("changed slot acquired shared numeric recommendation", w.Code, w.Body.String())
	}
}

func TestClientProfileDefaultsAndConfiguredTeleAgentListAgreeWithoutInferringCapabilities(t *testing.T) {
	data, err := os.ReadFile("../../../deploy/teleagent-model-policies.json")
	if err != nil {
		t.Fatal(err)
	}
	var presets map[string]config.ModelPolicy
	if err := json.Unmarshal(data, &presets); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id              string
		context, output int
	}{
		{"chat-unlisted", 32768, 4096},
		{"chat-flagship", 825737, 32768},
		{"chat-flash", 510173, 8192},
		{"chat-lite", 761525, 8192},
		{"chat-nano", 262144, 32768},
		{"chat-pro", 510167, 8192},
	} {
		t.Run(tc.id, func(t *testing.T) {
			s, key := unifiedAPIFixture(t)
			model := provider.Model{ID: tc.id, Name: tc.id, Source: "upstream"}
			if tc.id != "chat-unlisted" {
				// Keep numeric recommendations independent from the catalog's
				// existing reviewed tool/image observations for these IDs.
				model.ImageInput, model.ToolCalling = "unsupported", "unsupported"
			}
			s.Reg.ReplaceProvider(provider.TeleAgent, "cn", []provider.Model{model})
			if tc.id != "chat-unlisted" {
				id := "teleagent/cn/" + tc.id
				w := adminProbeRequest(t, s, "POST", "/admin/api/model-policies", string(mustJSON(map[string]any{
					"model": id, "policy": presets[id], "expected_routing_revision": s.Store.UnifiedSnapshot().Revision,
				})))
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			_, err := s.Store.SaveUnifiedModel(config.UnifiedModel{Name: "teleagent-" + tc.id, Enabled: true, Sources: []config.ModelSource{{ID: "teleagent/cn/" + tc.id, Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision, nil)
			if err != nil {
				t.Fatal(err)
			}
			grantFixtureAccounts(t, s.Store)
			w := nativeProfileGET(s, key, "/v1/client-profile?model=teleagent-"+tc.id)
			var profile map[string]any
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &profile) != nil || profile["context_window"] != float64(tc.context) || profile["max_output_tokens"] != float64(tc.output) || profile["client_profile_status"] != "ready" {
				t.Fatal("missing directory limits blocked client setup", w.Code, w.Body.String())
			}
			if value, present := profile["supports_images"]; value == true || (tc.id == "chat-unlisted" && present) {
				t.Fatal("numeric default manufactured vision support", profile)
			}
			if value, present := profile["supports_tools"]; value == true || (tc.id == "chat-unlisted" && present) {
				t.Fatal("numeric default manufactured tool support", profile)
			}
			var listing struct{ Data []map[string]any }
			w = nativeProfileGET(s, key, "/v1/models")
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &listing) != nil {
				t.Fatal(w.Code, w.Body.String())
			}
			for _, row := range listing.Data {
				if row["id"] != "teleagent-"+tc.id {
					continue
				}
				fromList, _ := json.Marshal(row["client_profile"])
				fromProfile, _ := json.Marshal(profile)
				if string(fromList) != string(fromProfile) || strings.Contains(string(fromProfile), "teleagent/cn/") || len(s.Store.Logs(10)) != 0 {
					t.Fatal("list/profile disagreed, leaked sources or caused inference", string(fromList), string(fromProfile))
				}
				return
			}
			t.Fatal("shared name disappeared")
		})
	}
}
