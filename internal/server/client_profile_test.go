package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
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
