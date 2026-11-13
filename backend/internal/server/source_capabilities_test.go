package server

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

func TestReviewedSourceCapabilitiesAgreeInListAndClientProfile(t *testing.T) {
	var data struct {
		Sources []struct {
			ID, Name string
			Image    string `json:"image_input"`
			Tool     string `json:"tool_calling"`
		} `json:"source_capabilities"`
	}
	raw, err := os.ReadFile("../catalog/default_models.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	for i, e := range data.Sources {
		t.Run(e.ID, func(t *testing.T) {
			s, key := unifiedAPIFixture(t)
			platform, scope, id := provider.ParseModel(e.ID)
			name := e.Name
			if name == "" {
				name = id
			}
			model := provider.Model{ID: id, Name: name, Source: "upstream"}
			s.Reg.ReplaceProvider(platform, scope, []provider.Model{model})
			public := fmt.Sprintf("r2-reviewed-%d", i)
			_, err := s.Store.SaveUnifiedModel(config.UnifiedModel{Name: public, Enabled: true, Sources: []config.ModelSource{{ID: e.ID, Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision, nil)
			if err != nil {
				t.Fatal(err)
			}
			w := nativeProfileGET(s, key, "/v1/client-profile?model="+public)
			var profile map[string]any
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &profile) != nil {
				t.Fatal(w.Code, w.Body.String())
			}
			w = nativeProfileGET(s, key, "/v1/models")
			var list struct {
				Data []map[string]any `json:"data"`
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil {
				t.Fatal(w.Code, w.Body.String())
			}
			var row map[string]any
			for _, m := range list.Data {
				if m["id"] == public {
					row = m
				}
			}
			for field, value := range map[string]string{"supports_tools": e.Tool, "supports_images": e.Image} {
				if value == "supported" && (profile[field] != true || row[field] != true) {
					t.Fatalf("%s list/profile mismatch: %v %v", field, row, profile)
				}
			}
			// Revoking the actual catalog source removes evidence from the profile.
			s.Reg.ReplaceProvider(platform, scope, nil)
			w = nativeProfileGET(s, key, "/v1/client-profile?model="+public)
			profile = nil
			json.Unmarshal(w.Body.Bytes(), &profile)
			if profile["supports_tools"] == true || profile["supports_images"] == true {
				t.Fatal("missing source inherited live capability evidence", profile)
			}
		})
	}
}
