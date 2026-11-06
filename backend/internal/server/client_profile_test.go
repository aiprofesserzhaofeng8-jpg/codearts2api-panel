package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"codearts2api/internal/requestpolicy"
	"codearts2api/internal/storage"
)

func TestClientSummaryAdaptation(t *testing.T) {
	ordinary := []byte(`{"messages":[{"role":"user","content":"You are a context summarization assistant."}]}`)
	out, err := adaptClientRequest(ordinary, "deepseek-v4.1-flash")
	if err != nil || string(out) != string(ordinary) {
		t.Fatal("ordinary request modified")
	}
	summary := []byte(`{"messages":[{"role":"system","content":"You are a context summarization assistant. Test."}],"reasoning_effort":"high","max_tokens":8192}`)
	out, err = adaptClientRequest(summary, "deepseek-v4.1-flash")
	if err != nil {
		t.Fatal(err)
	}
	var req map[string]any
	json.Unmarshal(out, &req)
	if req["thinking"].(map[string]any)["type"] != "disabled" || req["reasoning_effort"] != nil || req["max_tokens"] != float64(8192) {
		t.Fatal("summary adaptation failed")
	}
	again, _ := adaptClientRequest(out, "deepseek-v4.1-flash")
	if strings.Count(string(again), "Produce a concise checkpoint") != 1 {
		t.Fatal("guidance duplicated")
	}
	other, _ := adaptClientRequest(summary, "glm-5.3")
	if string(other) != string(summary) {
		t.Fatal("other model modified")
	}
}
func TestClientProfileRequiresKey(t *testing.T) {
	s := poolServer(t, 0)
	s.Store.AddKey("client")
	r := httptest.NewRequest("GET", "/v1/client-profile?model=deepseek-v4.1-flash", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("profile bypassed model auth")
	}
}

func TestWorkBuddyImageCapabilityExposedToClients(t *testing.T) {
	s := poolServer(t, 0)
	s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "vision", Context: 65536, MaxOutput: 4096, ImageInput: "supported"}, {ID: "unknown", Context: 65536, MaxOutput: 4096}})
	key := s.Store.AddKey("images").Key
	for _, path := range []string{"/v1/models", "/v1/client-profile?model=workbuddy/cn/vision"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"input_modalities":["text","image"]`) || !strings.Contains(w.Body.String(), `"supports_images":true`) {
			t.Fatal("client capability lost", w.Code, w.Body.String())
		}
	}
}

func TestTraeClientProfileAvailableWithoutManualPolicy(t *testing.T) {
	s := poolServer(t, 0)
	known, unknown := "trae/cn/deepseek-v4.1-flash", "trae/cn/unknown"
	s.Reg.Replace([]registry.Model{{ID: known, Provider: provider.Trae, Scope: "cn"}, {ID: unknown, Provider: provider.Trae, Scope: "cn"}})
	key := s.Store.AddKey("profile").Key
	for _, model := range []string{known, unknown} {
		r := httptest.NewRequest("GET", "/v1/client-profile?model="+model, nil)
		r.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if model == unknown {
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"client_profile_status":"missing_limits"`) || strings.Contains(w.Body.String(), `"context_window"`) {
				t.Fatal("missing limits must retain independent capabilities without guessing", w.Body.String())
			}
			continue
		}
		var profile struct {
			Model      string `json:"model"`
			Context    int    `json:"context_window"`
			Output     int    `json:"max_output_tokens"`
			Summary    bool   `json:"pi_summary_adapter"`
			ClaudeCode struct {
				Thinking bool `json:"thinking_control"`
				Effort   bool `json:"effort_control"`
			} `json:"claude_code"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &profile); err != nil || w.Code != 200 || profile.Model != known || profile.Context != 65536 || profile.Output != 4096 || profile.Summary || profile.ClaudeCode.Thinking || profile.ClaudeCode.Effort {
			t.Fatalf("invalid automatic Trae recommendation: status=%d %s", w.Code, w.Body.String())
		}
	}
}

// Apply defaults in the legacy client-profile fixture; live requests use Store policies.
func adaptClientRequest(body []byte, model string) ([]byte, error) {
	p, ok := storage.DefaultModelPolicies()[model]
	if !ok || !p.SummaryEnabled || !requestpolicy.IsSummaryRequest(body, false) {
		return body, nil
	}
	return requestpolicy.PrepareSummary(body, requestpolicy.SummaryOptions{MaxTokens: p.SummaryMaxTokens, DisableThinking: p.DisableSummaryThinking}, false)
}
