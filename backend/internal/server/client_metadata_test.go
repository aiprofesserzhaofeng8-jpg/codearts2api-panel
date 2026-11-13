package server

import (
	"encoding/json"
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
	w := nativeProfileGET(s, key, "/v1/client-profile?model=glm-5.3-flash")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"client_profile_status":"missing_limits"`) || strings.Contains(w.Body.String(), `"context_window":1000000`) {
		t.Fatal("changed slot acquired shared numeric recommendation", w.Code, w.Body.String())
	}
}

func TestDefaultClientParametersListAndProfileAgreeWithoutInferringCapabilities(t *testing.T) {
	s, key := unifiedAPIFixture(t)
	s.Reg.ReplaceProvider(provider.TeleAgent, "cn", []provider.Model{{ID: "chat-pro", Name: "Chat Pro", Source: "upstream"}})
	_, err := s.Store.SaveUnifiedModel(config.UnifiedModel{Name: "teleagent-chat-pro", Enabled: true, Sources: []config.ModelSource{{ID: "teleagent/cn/chat-pro", Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := nativeProfileGET(s, key, "/v1/client-profile?model=teleagent-chat-pro")
	var profile map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &profile) != nil || profile["context_window"] != float64(32768) || profile["max_output_tokens"] != float64(4096) || profile["client_profile_status"] != "ready" {
		t.Fatal("missing directory limits blocked client setup", w.Code, w.Body.String())
	}
	if _, present := profile["supports_images"]; present {
		t.Fatal("numeric default manufactured vision support", profile)
	}
	if _, present := profile["supports_tools"]; present {
		t.Fatal("numeric default manufactured tool support", profile)
	}
	var listing struct{ Data []map[string]any }
	w = nativeProfileGET(s, key, "/v1/models")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &listing) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, row := range listing.Data {
		if row["id"] != "teleagent-chat-pro" {
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
}
