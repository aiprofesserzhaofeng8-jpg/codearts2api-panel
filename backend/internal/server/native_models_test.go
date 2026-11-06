package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"codearts2api/internal/provider"
)

func nativeProfileGET(s *Server, key, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestNativeModelListAndProfilesShareUpperBound(t *testing.T) {
	s, key := unifiedAPIFixture(t)
	createUnifiedHTTP(t, s, "glm-5.3")
	assert := func(payload map[string]any) {
		t.Helper()
		native, ok := payload["native_model"].(map[string]any)
		if !ok || native["id"] != "glm-5.3" || native["image_input"] != "unsupported" || payload["supports_images"] != false || payload["supports_tools"] != true || payload["capability_basis"] != "native_and_authorized_sources" {
			t.Fatal("inconsistent native capability", payload)
		}
	}
	w := nativeProfileGET(s, key, "/v1/client-profile?model=glm-5.3")
	var profile map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &profile) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	assert(profile)
	var list struct{ Data []map[string]any }
	w = scopedModelRequest(s, key)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	found := false
	for _, row := range list.Data {
		if row["id"] == "glm-5.3" {
			assert(row)
			assert(row["client_profile"].(map[string]any))
			found = true
		}
	}
	if !found {
		t.Fatal("native name disappeared")
	}
	// A missing numeric recommendation cannot restore the provider's image claim.
	s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "verified-v1", ImageInput: "supported", ToolCalling: "supported"}})
	s.Reg.ReplaceProvider(provider.Trae, "cn", []provider.Model{{ID: "verified-v1", ImageInput: "supported", ToolCalling: "supported"}})
	w = nativeProfileGET(s, key, "/v1/client-profile?model=glm-5.3")
	profile = nil
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &profile) != nil || profile["client_profile_status"] != "missing_limits" {
		t.Fatal(w.Code, w.Body.String())
	}
	assert(profile)
}

func TestNativeModelImagesRejectedAcrossProtocolsAndProviderServiceExplicit(t *testing.T) {
	s, key := unifiedAPIFixture(t)
	createUnifiedHTTP(t, s, "glm-5.3")
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		body := map[string]any{"model": "glm-5.3", "max_tokens": 64}
		switch path {
		case "/v1/chat/completions":
			body["messages"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64,aGVsbG8="}}}}}
		case "/v1/responses":
			body["input"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": "data:image/png;base64,aGVsbG8="}}}}
		case "/v1/messages":
			body["messages"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "image", "source": map[string]string{"type": "base64", "media_type": "image/png", "data": "aGVsbG8="}}}}}
		}
		for _, stream := range []bool{false, true} {
			body["stream"] = stream
			w := responseRequest(s, key, path, string(mustJSON(body)))
			// Rejection precedes admission: the fixture has no usable source accounts.
			if w.Code != 400 || !strings.Contains(w.Body.String(), "native_model_capability_unsupported") {
				t.Fatal(path, stream, w.Code, w.Body.String())
			}
		}
	}
	s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "glm-5.3", Context: 65536, MaxOutput: 4096, ImageInput: "supported", ToolCalling: "supported"}})
	w := nativeProfileGET(s, key, "/v1/client-profile?model=workbuddy/cn/glm-5.3")
	var profile struct {
		Images bool                         `json:"supports_images"`
		Basis  string                       `json:"capability_basis"`
		Native provider.NativeModelContract `json:"native_model"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &profile) != nil || !profile.Images || profile.Basis != "provider_catalog" || profile.Native.ImageInput != "unsupported" {
		t.Fatal("provider observations presented as native", w.Code, w.Body.String())
	}
}
