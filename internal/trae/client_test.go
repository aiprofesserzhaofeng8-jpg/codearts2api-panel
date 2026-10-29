package trae

import (
	"codearts2api/internal/provider"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func fixtureCredentials() json.RawMessage {
	raw, _ := json.Marshal(Credentials{AccessToken: "trae-fixture-access", RefreshToken: "trae-fixture-refresh", ExpiresAt: time.Now().Add(time.Hour).Unix(), UID: "user-1", Scope: "cn", ClientID: ClientID, MachineID: "machine", DeviceID: "device"})
	return raw
}
func fixtureClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New()
	c.AgentBase = srv.URL
	c.OAuthBase = srv.URL
	c.CreditBase = srv.URL
	return c
}
func TestCredentialNormalizationAndFixedDestinations(t *testing.T) {
	c := New()
	raw := fmt.Sprintf(`{"auth":{"accessToken":"token","refreshToken":"rt","expiresAt":%d,"machineId":"m","deviceId":"d","apiHost":"https://untrusted.invalid"},"account":{"uid":"u","nickname":"test"}}`, time.Now().Add(time.Hour).UnixMilli())
	norm, id, err := c.Normalize([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := decode(norm)
	if id.ID != "u" || id.Scope != "cn" || a.ExpiresAt > 1e12 || strings.Contains(string(norm), "untrusted.invalid") {
		t.Fatal("credential normalization or endpoint isolation failed")
	}
	for _, bad := range []string{`{}`, strings.Replace(raw, `"apiHost":"https://untrusted.invalid"`, `"domain":"trae.ai"`, 1), strings.Replace(raw, `"machineId":"m",`, "", 1)} {
		if _, _, err := c.Normalize([]byte(bad)); err == nil {
			t.Fatal("invalid credential accepted")
		}
	}
}
func TestPayloadPreservesToolProtocolAndCallerLimit(t *testing.T) {
	raw := []byte(`{"messages":[{"role":"system","content":"rules"},{"role":"assistant","content":null,"tool_calls":[{"id":"c","type":"function","function":{"name":"read","arguments":"{}"}}]},{"role":"tool","tool_call_id":"c","content":"result"}],"tools":[{"type":"function","function":{"name":"read","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"read"}},"max_tokens":12,"max_completion_tokens":64}`)
	body, err := prepareBody(raw, "model-a")
	if err != nil {
		t.Fatal(err)
	}
	if body["max_tokens"] != float64(12) || body["config_name"] != "model-a" || body["function"] != "solo_work_lite" || body["tool_choice"] != "read" {
		t.Fatal("request contract changed")
	}
	messages := body["messages"].([]any)
	assistant := messages[1].(map[string]any)
	call := assistant["tool_calls"].([]any)[0].(map[string]any)
	if call["function_call"] == nil || call["function"] != nil {
		t.Fatal("tool history translation missing")
	}
	fn := body["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if _, ok := fn["parameters"].(string); !ok {
		t.Fatal("native schema should be JSON text")
	}
	if _, err := prepareBody([]byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.invalid/image"}}]}]}`), "m"); err == nil {
		t.Fatal("unvalidated image capability accepted")
	}
}
func TestNativeStreamTextUsageAndDone(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ChatPath || r.Header.Get("Authorization") != "Cloud-IDE-JWT trae-fixture-access" || r.Header.Get("X-Cloudide-Token") != "trae-fixture-access" || r.Header.Get("X-Refresh-Token") != "" {
			t.Error("wrong transport credentials")
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "trae-fixture-refresh") {
			t.Error("refresh token sent to inference")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: output\ndata: {\"response\":\"Hello\",\"reasoning_content\":\"Thinking\"}\n\nevent: token_usage\ndata: {\"prompt_tokens\":10,\"completion_tokens\":5,\"reasoning_tokens\":2,\"cache_read_input_tokens\":4}\n\nevent: done\ndata: {\"finish_reason\":\"stop\"}")
	})
	var chunks []map[string]any
	err := c.Stream(context.Background(), fixtureCredentials(), provider.Request{Model: "model-a", Body: []byte(`{"messages":[{"role":"user","content":"hello"}]}`)}, func(raw []byte) error {
		var x map[string]any
		_ = json.Unmarshal(raw, &x)
		chunks = append(chunks, x)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 3 || chunks[0]["model"] != "trae/cn/model-a" {
		t.Fatal("native stream not normalized")
	}
	u := chunks[2]["usage"].(map[string]any)
	if u["prompt_tokens_details"].(map[string]any)["cached_tokens"] != float64(4) {
		t.Fatal("cache usage lost")
	}
}
func TestToolFragmentsAndParallelIdentity(t *testing.T) {
	s := newStream("trae/cn/m")
	var chunks [][]byte
	emit := func(b []byte) error { chunks = append(chunks, b); return nil }
	for _, raw := range []string{`{"tool_calls":[{"index":0,"id":"a","function_call":{"name":"read","arguments":"{\"path\":"}},{"index":1,"id":"b","function_call":{"name":"list","arguments":"{}"}}]}`, `{"tool_calls":[{"index":0,"id":"a","function_call":{"arguments":"\"file\"}"}}]}`} {
		if err := s.event("output", []byte(raw), emit); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.event("done", []byte(`{"finish_reason":"stop"}`), emit); err != nil {
		t.Fatal(err)
	}
	if !s.done || !strings.Contains(string(chunks[len(chunks)-1]), "tool_calls") || s.tools[0].Arguments != `{"path":"file"}` || s.tools[1].Arguments != `{}` {
		t.Fatal("tools merged incorrectly")
	}
}
func TestIncompleteOrInvalidStreamNeverSucceeds(t *testing.T) {
	for _, body := range []string{"event: output\ndata: {\"response\":\"partial\"}\n\n", "event: output\ndata: invalid\n\n", "event: done\ndata: {}\n\n", "event: error\ndata: {\"code\":1001,\"message\":\"trae-fixture-access trae-fixture-refresh\"}\n\n"} {
		c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, body)
		})
		err := c.Stream(context.Background(), fixtureCredentials(), provider.Request{Model: "m", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil })
		if err == nil {
			t.Fatal("bad stream accepted")
		}
		if strings.Contains(err.Error(), "trae-fixture-access") || strings.Contains(err.Error(), "trae-fixture-refresh") {
			t.Fatal("upstream leaked credentials")
		}
	}
}
func TestRedirectAndCredentialHostOverrideRejected(t *testing.T) {
	calls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer target.Close()
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) })
	if _, err := c.Verify(context.Background(), fixtureCredentials()); err == nil || calls != 0 {
		t.Fatal("credential redirect followed")
	}
}
func TestRefreshPreservesVerifiedIdentity(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case ExchangePath:
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["RefreshToken"] != "trae-fixture-refresh" || b["ClientID"] != ClientID || r.Header.Get("Authorization") != "" {
				t.Error("refresh request boundary")
			}
			fmt.Fprintf(w, `{"Result":{"Token":"rotated","RefreshToken":"rotated-rt","TokenExpireAt":%d}}`, time.Now().Add(2*time.Hour).UnixMilli())
		case UserPath:
			if r.Header.Get("X-Cloudide-Token") != "rotated" {
				t.Error("identity not checked with new token")
			}
			io.WriteString(w, `{"Result":{"UserID":"user-1","ScreenName":"User"}}`)
		}
	})
	raw, err := c.Refresh(context.Background(), fixtureCredentials())
	if err != nil {
		t.Fatal(err)
	}
	_, id, err := c.Normalize(raw)
	if err != nil || id.ID != "user-1" || id.ExpiresAt <= time.Now().Unix() {
		t.Fatal("refresh lost identity or expiry")
	}
}
func callbackURL(t *testing.T, f provider.Login, access bool) string {
	t.Helper()
	u, _ := url.Parse(f.URL)
	q := url.Values{"loginTraceID": {u.Query().Get("login_trace_id")}, "userInfo": {`{"UserID":"user-1"}`}}
	if access {
		q.Set("userJwt", fmt.Sprintf(`{"Token":"trae-fixture-access","TokenExpireAt":%d}`, time.Now().Add(time.Hour).UnixMilli()))
	} else {
		q.Set("refreshToken", "trae-fixture-refresh")
	}
	return callbackAddress + "?" + q.Encode()
}
func TestCallbackBindingAndNoRepeatedRefreshAfterIdentityFailure(t *testing.T) {
	exchanges, identities := 0, 0
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case ExchangePath:
			exchanges++
			fmt.Fprintf(w, `{"Result":{"Token":"new-token","RefreshToken":"rotated-rt","TokenExpireAt":%d}}`, time.Now().Add(time.Hour).UnixMilli())
		case UserPath:
			identities++
			if identities == 1 {
				w.WriteHeader(503)
				return
			}
			io.WriteString(w, `{"Result":{"UserID":"user-1","ScreenName":"User"}}`)
		}
	})
	f, err := c.BeginLogin(context.Background(), "cn")
	if err != nil {
		t.Fatal(err)
	}
	callback := callbackURL(t, f, false)
	if _, _, err = c.CheckCallback(context.Background(), "cn", f.State, strings.Replace(callback, "loginTraceID=", "loginTraceID=wrong", 1)); err == nil || exchanges != 0 {
		t.Fatal("unbound callback exchanged token")
	}
	if _, _, err = c.CheckCallback(context.Background(), "cn", f.State, callback); err == nil || exchanges != 1 {
		t.Fatal("identity failure fixture missing")
	}
	_, ready, err := c.CheckCallback(context.Background(), "cn", f.State, callback)
	if err != nil || !ready || exchanges != 1 {
		t.Fatal("one-time refresh replayed")
	}
	c.ReleaseLogin(f.State)
	if _, _, err = c.CheckCallback(context.Background(), "cn", f.State, callback); err == nil {
		t.Fatal("released login accepted")
	}
}
func TestModelsAndCreditFailClosed(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case ModelsPath:
			io.WriteString(w, `{"config_info_list":[{"config_name":"exact-model","display_config":{"display_name":"Exact"}}]}`)
		case CreditPath:
			io.WriteString(w, `{"user_entitlement_pack_list":[{"entitlement_base_info":{"quota":{"credits_limit":100}},"usage":{"credits_amount":0.25}}]}`)
		}
	})
	models, err := c.Models(context.Background(), fixtureCredentials())
	if err != nil || len(models) != 1 || models[0].Context != 0 {
		t.Fatal("invented context/model")
	}
	raw, err := c.Credit(context.Background(), fixtureCredentials())
	if err != nil {
		t.Fatal(err)
	}
	var credit map[string]any
	_ = json.Unmarshal(raw, &credit)
	if credit["remaining"] != 99.75 {
		t.Fatal("fractional credit lost")
	}
}

func TestUnknownCreditAndCanceledStream(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"user_entitlement_pack_list":[{"entitlement_base_info":{"quota":{"credits_limit":0}},"usage":{"credits_amount":0}}]}`)
	})
	raw, err := c.Credit(context.Background(), fixtureCredentials())
	if err != nil {
		t.Fatal(err)
	}
	var info map[string]any
	_ = json.Unmarshal(raw, &info)
	if info["available"] != false {
		t.Fatal("unmeasurable quota reported as a zero balance")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Stream(ctx, fixtureCredentials(), provider.Request{Model: "m", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil }); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestWrappedCatalogAndQuotaPayload(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case ModelsPath:
			io.WriteString(w, `{"code":0,"data":{"config_info_list":[{"config_name":"m"}]}}`)
		case CreditPath:
			io.WriteString(w, `{"code":0,"data":{"user_entitlement_pack_list":[{"entitlement_base_info":{"quota":{"credits_limit":25}},"usage":{"credits_amount":1.5}}]}}`)
		}
	})
	models, err := c.Models(context.Background(), fixtureCredentials())
	if err != nil || len(models) != 1 {
		t.Fatal("wrapped catalog lost")
	}
	raw, err := c.Credit(context.Background(), fixtureCredentials())
	if err != nil {
		t.Fatal(err)
	}
	var quota map[string]any
	_ = json.Unmarshal(raw, &quota)
	if quota["available"] != true || quota["remaining"] != 23.5 {
		t.Fatal("wrapped quota lost")
	}
}
