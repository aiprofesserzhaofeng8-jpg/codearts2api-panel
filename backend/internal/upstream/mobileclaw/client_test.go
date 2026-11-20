package mobileclaw

import (
	"codearts2api/internal/provider"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) json.RawMessage {
	t.Helper()
	claims, _ := json.Marshal(map[string]any{"uid": "user", "aip": "CIDC-RP-48", "exp": time.Now().Add(time.Hour).Unix()})
	raw, _ := json.Marshal(Credentials{Token: "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(claims) + ".fixture", UserID: "user", DeviceID: "device", WorkspaceMode: "personal"})
	return raw
}
func TestCatalogIdentityQuotaAndStream(t *testing.T) {
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || r.Header.Get("service-version") != ServiceVersion {
			t.Error("missing auth/version")
		}
		switch r.URL.Path {
		case "/desktop/manager/user-subjects":
			fmt.Fprint(w, `[{"userId":"user","username":"fixture"}]`)
		case "/desktop/gate/model/modelList":
			fmt.Fprint(w, `[{"modelName":"deepseek-v4.1-flash","pkgTypes":["FREE"]},{"modelName":"paid","pkgTypes":["TOKEN_PLAN_PERSONAL"]}]`)
		case "/desktop/manager/subscriptions/getAiProviders":
			fmt.Fprint(w, `[{"provider":"CIDC-RP-48","packageList":[{"providerType":"FREE","available":true},{"providerType":"TOKEN_PLAN_PERSONAL","available":true}]}]`)
		case "/desktop/manager/credits/v2/exp-credit":
			fmt.Fprint(w, `{"remainTokens":35976266,"totalTokens":23734}`)
		case "/desktop/v1/chat/completions":
			if r.Header.Get("ai-provider") != "CIDC-RP-48" {
				t.Error("token supplier was not sent")
			}
			if r.Header.Get("pkg-types") != "FREE" {
				t.Error("wrong billing route")
			}
			var body map[string]json.RawMessage
			_ = json.NewDecoder(r.Body).Decode(&body)
			var options map[string]json.RawMessage
			_ = json.Unmarshal(body["stream_options"], &options)
			if string(options["include_usage"]) != "true" || string(options["fixture"]) != "true" {
				t.Error("usage request or existing stream options lost")
			}
			if !strings.Contains(string(body["tools"]), "9007199254740993") || !strings.Contains(string(body["messages"]), "image_url") {
				t.Error("request fields lost")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	c := New()
	c.Origin = server.URL
	raw := fixture(t)
	if id, err := c.Verify(t.Context(), raw); err != nil || id.ID != "user" || id.HasRefresh {
		t.Fatal(id, err)
	}
	models, err := c.Models(t.Context(), raw)
	if err != nil || len(models) != 1 || models[0].ID != "deepseek-v4.1-flash" {
		t.Fatal(models, err)
	}
	quota, err := c.Credit(t.Context(), raw)
	if err != nil || strings.Contains(string(quota), `"total"`) || !strings.Contains(string(quota), "35976266") {
		t.Fatal(string(quota), err)
	}
	before := calls["/desktop/gate/model/modelList"]
	emitted := 0
	err = c.Stream(t.Context(), raw, provider.Request{Model: "deepseek-v4.1-flash", Body: []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,fixture"}}]}],"tools":[{"number":9007199254740993}],"stream_options":{"include_usage":false,"fixture":true}}`)}, func(b []byte) error { emitted++; return nil })
	if err != nil || emitted != 1 || calls["/desktop/gate/model/modelList"] != before {
		t.Fatal("stream failed or performed catalog queries", err)
	}
}
func TestRejectInvalidCredentialsAndTruncatedStream(t *testing.T) {
	c := New()
	raw := fixture(t)
	for _, bad := range []json.RawMessage{[]byte(`{}`), []byte(`{"token":"encrypted"}`), []byte(strings.Replace(string(raw), "personal", "enterprise", 1))} {
		if _, _, err := c.Normalize(bad); err == nil {
			t.Fatal("invalid credentials accepted")
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n")
	}))
	defer server.Close()
	c.Origin = server.URL
	if err := c.Stream(context.Background(), raw, provider.Request{Model: "test", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil }); err == nil {
		t.Fatal("truncated response accepted")
	}
}

func TestStreamUnboundAccountRequiresAuthorization(t *testing.T) {
	c := New()
	c.Origin = "http://127.0.0.1:1"
	claims, _ := json.Marshal(map[string]any{"uid": "user", "exp": time.Now().Add(time.Hour).Unix()})
	raw, _ := json.Marshal(Credentials{Token: "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".fixture", UserID: "user", DeviceID: "device"})
	err := c.Stream(t.Context(), raw, provider.Request{Model: "test", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { t.Fatal("unexpected output"); return nil })
	if err == nil || !strings.Contains(err.Error(), "供应商授权") {
		t.Fatalf("missing reauthorization guidance: %v", err)
	}
}
