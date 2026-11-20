package teleagent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
)

func fixture() Credentials {
	return Credentials{Token: "tenant:eyJhbGciOiJIUzI1NiJ9.eyJhY2NvdW50SWQiOiJmaXh0dXJlLWFjY291bnQiLCJhY2NvdW50TmFtZSI6IkZpeHR1cmUiLCJleHAiOjIwMDAwMDAwMDB9.signature-fixture", APIKey: "sk-authorized-fixture", DeviceID: "fixture-device", InstallID: "fixture-install", AppVersion: "2.7.1", AccountID: "fixture-account"}
}
func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func normalized(t *testing.T, c *Client) json.RawMessage {
	t.Helper()
	b, id, e := c.Normalize(raw(fixture()))
	if e != nil || id.ID != "fixture-account" || id.ExpiresAt != 2000000000 || id.HasRefresh || id.NonExpiring {
		t.Fatal("invalid identity", id, e)
	}
	return b
}
func TestSignatureGoldenAndBodyBinding(t *testing.T) {
	a := fixture()
	body := []byte(`{"messages":[{"role":"user","content":"你好"}],"model":"chat-pro","stream":true}`)
	sig := signature(a, "POST", ChatPath, "1793800000", "00000000-0000-4000-8000-000000000001", body)
	if sig != "594be319d4c4b4053d240380cc1f1dda0b6758a93ff92bde1c352860ad8232f9" {
		t.Fatal("signature contract changed", sig)
	}
	if signature(a, "POST", ChatPath, "1793800000", "00000000-0000-4000-8000-000000000001", append(body, ' ')) == sig {
		t.Fatal("body not signed")
	}
	a.InstallID = "other"
	if signature(a, "POST", ChatPath, "1793800000", "00000000-0000-4000-8000-000000000001", body) == sig {
		t.Fatal("install binding lost")
	}
}
func TestNormalizeBoundCredentialsAndNoFakeRefresh(t *testing.T) {
	c := New()
	normalized(t, c)
	for _, change := range []func(map[string]any){func(o map[string]any) { o["token"] = "not-jwt" }, func(o map[string]any) { o["account_id"] = "other" }, func(o map[string]any) { o["device_id"] = "" }, func(o map[string]any) { o["install_id"] = "" }, func(o map[string]any) { o["app_version"] = "" }, func(o map[string]any) { o["api_key"] = "" }, func(o map[string]any) { o["api_key"] = "sk-" }, func(o map[string]any) { o["scope"] = "global" }, func(o map[string]any) { o["refresh_token"] = "secret" }, func(o map[string]any) { o["token"] = "secret\r\n" }, func(o map[string]any) { o["operation_token"] = "a;other=b" }} {
		var o map[string]any
		_ = json.Unmarshal(raw(fixture()), &o)
		change(o)
		if _, _, e := c.Normalize(raw(o)); e == nil {
			t.Fatal("invalid credentials accepted")
		}
	}
	a := fixture()
	a.ExpiresAt = 2100000000
	b, id, e := c.Normalize(raw(a))
	if e != nil || id.ExpiresAt != 2000000000 {
		t.Fatal("JWT expiry extended", e)
	}
	if _, e = c.Refresh(context.Background(), b); e == nil {
		t.Fatal("pretended to refresh")
	}
	if !c.Describe().Login {
		t.Fatal("local browser helper login is unavailable")
	}
}
func TestReadOnlyIdentityModelsAndCredits(t *testing.T) {
	points := `"123.45"`
	bad := false
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.Header.Get("X-Token") != fixture().Token || r.Header.Get("Authorization") != "" {
			t.Error("wrong business authentication")
		}
		switch r.URL.Path {
		case UserPath:
			io.WriteString(w, `{"code":0,"data":{"accountId":"fixture-account","accountName":"verified"}}`)
		case CreditPath:
			if bad {
				io.WriteString(w, `{"code":40109,"message":"private-upstream-secret"}`)
			} else {
				io.WriteString(w, `{"code":200,"data":{"current_points_balance":`+points+`,"plan_display_name":"免费版"}}`)
			}
		default:
			t.Error("unexpected path", r.URL.Path)
		}
	}))
	defer up.Close()
	c := New()
	c.Origin = up.URL
	b := normalized(t, c)
	if id, e := c.Verify(context.Background(), b); e != nil || id.Name != "verified" {
		t.Fatal("identity failed", e)
	}
	if models, e := c.Models(context.Background(), b); e != nil || len(models) != 5 || models[0].Context != 0 || models[0].Provider != provider.TeleAgent {
		t.Fatal("catalog unavailable or invented limits", e)
	}
	for _, v := range []struct {
		value     string
		available bool
		amount    float64
	}{{`"123.45"`, true, 123.45}, {`0`, true, 0}, {`null`, false, 0}} {
		points = v.value
		out, e := c.Credit(context.Background(), b)
		if e != nil {
			t.Fatal(e)
		}
		var info accounts.CreditInfo
		_ = json.Unmarshal(out, &info)
		if info.Available != v.available || info.Total != 0 || info.Used != 0 {
			t.Fatal("unknown became zero or invented totals")
		}
		if v.available && (len(info.Windows) != 1 || *info.Windows[0].Remaining != v.amount) {
			t.Fatal("balance lost")
		}
	}
	bad = true
	if _, e := c.Credit(context.Background(), b); e == nil || strings.Contains(e.Error(), "private-upstream-secret") {
		t.Fatal("quota error hidden or leaked")
	}
	if calls != 6 {
		t.Fatal("unexpected implicit requests", calls)
	}
}
func TestPortalCredentialsStaySeparate(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != PortalPath || r.Header.Get("X-Token") != "portal-fixture" || r.Header.Get("Userid") != "fixture-account" || r.Header.Get("Authorization") != "" {
			t.Error("portal credentials mixed with inference")
		}
		if !strings.Contains(r.Header.Get("Cookie"), "operationToken=portal-fixture") {
			t.Error("portal cookie missing")
		}
		io.WriteString(w, `{"code":200,"data":{"pointsBalance":"0"}}`)
	}))
	defer up.Close()
	c := New()
	c.Origin = up.URL
	a := fixture()
	a.OperationToken = "portal-fixture"
	b, _, e := c.Normalize(raw(a))
	if e != nil {
		t.Fatal(e)
	}
	out, e := c.Credit(context.Background(), b)
	if e != nil || !strings.Contains(string(out), `"remaining":0`) {
		t.Fatal("zero portal balance lost", e)
	}
}

const finishSSE = "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":4,\"total_tokens\":16}}\n\ndata: [DONE]\n\n"

func TestStreamSignsExactPayloadAndPreservesTools(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		a := fixture()
		if r.URL.Path != ChatPath || r.Header.Get("Authorization") != "Bearer "+a.APIKey || r.Header.Get("X-Token") != a.Token || r.Header.Get("X-Superagent-Sign-Version") != "v1" || r.Header.Get("X-Superagent-Signature") != signature(a, r.Method, r.URL.Path, r.Header.Get("X-Superagent-Timestamp"), r.Header.Get("X-Superagent-Nonce"), b) {
			t.Error("request signature/auth changed")
		}
		if !strings.Contains(string(b), "9007199254740993") || !strings.Contains(string(b), "SYSTEM_FIXTURE") || !strings.Contains(string(b), `"include_usage":true`) {
			t.Error("payload changed")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_fixture\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"n\\\":9007199254740993}\"}}]},\"finish_reason\":null}]}\n\n"+finishSSE)
	}))
	defer up.Close()
	c := New()
	c.Origin = up.URL
	var chunks [][]byte
	e := c.Stream(context.Background(), normalized(t, c), provider.Request{Model: "chat-pro", Body: []byte(`{"messages":[{"role":"developer","content":"SYSTEM_FIXTURE"},{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"n":{"const":9007199254740993}}}}}]}`)}, func(b []byte) error { chunks = append(chunks, append([]byte(nil), b...)); return nil })
	if e != nil || len(chunks) != 3 || !strings.Contains(string(chunks[0]), "9007199254740993") {
		t.Fatal("tool stream failed", e, len(chunks))
	}
}
func TestStreamRejectsErrorsTruncationAndSupportsCancellation(t *testing.T) {
	for _, s := range []string{`data: {"error":{"code":40104,"message":"secret"}}` + "\n\n", `data: {"choices":[{}]}` + "\n\n", "data: [DONE]\n\n", `data: {"choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}]}` + "\n\n"} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, s)
		}))
		c := New()
		c.Origin = up.URL
		e := c.Stream(context.Background(), normalized(t, c), provider.Request{Model: "chat-pro", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil })
		up.Close()
		if e == nil || strings.Contains(e.Error(), "secret") {
			t.Fatal("bad stream accepted or leaked", e)
		}
	}
	started := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer up.Close()
	c := New()
	c.Origin = up.URL
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { <-started; cancel() }()
	e := c.Stream(ctx, normalized(t, c), provider.Request{Model: "chat-pro", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil })
	if !errors.Is(e, context.Canceled) {
		t.Fatal("cancel not preserved", e)
	}
}
func TestJWTNumericAccountIdentity(t *testing.T) {
	a := fixture()
	a.AccountID = ""
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"accountId":9007199254740993,"exp":2000000000}`))
	a.Token = "tenant:header." + payload + ".signature"
	_, id, e := New().Normalize(raw(a))
	if e != nil || id.ID != "9007199254740993" {
		t.Fatal("identity rounded", id, e)
	}
}

func TestVerifyRejectsOtherIdentityAndRedirects(t *testing.T) {
	foreignCalls := 0
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreignCalls++ }))
	defer foreign.Close()
	redirect := false
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if redirect {
			http.Redirect(w, r, foreign.URL, 302)
			return
		}
		io.WriteString(w, `{"code":0,"data":{"accountId":"different-account"}}`)
	}))
	defer up.Close()
	c := New()
	c.Origin = up.URL
	b := normalized(t, c)
	if _, e := c.Verify(t.Context(), b); e == nil {
		t.Fatal("different account verified")
	}
	redirect = true
	if _, e := c.Verify(t.Context(), b); e == nil || foreignCalls != 0 {
		t.Fatal("redirect followed with account credentials")
	}
}
