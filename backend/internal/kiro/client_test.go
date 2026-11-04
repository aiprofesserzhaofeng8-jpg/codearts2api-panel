package kiro

import (
	"bytes"
	"codearts2api/internal/provider"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	c := New()
	for key := range c.Regions {
		c.Regions[key] = Region{server.URL, server.URL, server.URL, server.URL}
	}
	return c
}
func rawJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func fixtureOAuth() Credentials {
	return Credentials{AccessToken: "access-private-fixture", RefreshToken: "refresh-private-fixture", AuthMethod: "social", Region: "us-east-1", AuthRegion: "eu-central-1", ProfileARN: "arn:aws:codewhisperer:us-east-1:123:profile/test", ExpiresAt: time.Now().Add(time.Hour).Unix()}
}
func TestCredentialNormalizationAndLifetime(t *testing.T) {
	c := New()
	a := fixtureOAuth()
	raw, id, e := c.Normalize(rawJSON(a))
	if e != nil || !id.HasRefresh || id.NonExpiring || id.Scope != "us-east-1" {
		t.Fatal(id, e)
	}
	var old map[string]any
	_ = json.Unmarshal(raw, &old)
	old["expiresAt"] = time.Unix(a.ExpiresAt, 0).UTC().Format(time.RFC3339)
	delete(old, "expires_at")
	old["authMethod"] = "Social"
	delete(old, "auth_method")
	_, compatible, e := c.Normalize(rawJSON(old))
	if e != nil || id != compatible {
		t.Fatal("camelCase compatibility failed", e)
	}
	_, key, e := c.Normalize([]byte(`{"api_key":"ksk_private_fixture"}`))
	if e != nil || !key.NonExpiring || key.HasRefresh || key.ExpiresAt != 0 || strings.Contains(key.ID, "private") {
		t.Fatal(key, e)
	}
	for _, input := range []string{`{}`, `{"api_key":"arbitrary"}`, `{"api_key":"ksk_x","refresh_token":"x"}`, `{"api_key":"ksk_x","region":"evil.example"}`, `{"api_key":"ksk_x\n"}`, `{"api_key":"ksk_x","profile_arn":"http://localhost"}`, `{"api_key":"ksk_x","profile_arn":"arn:aws:codewhisperer:eu-central-1:123:profile/test","region":"us-east-1"}`} {
		if _, _, e := c.Normalize([]byte(input)); e == nil {
			t.Fatalf("accepted invalid credential: %s", input)
		}
	}
}
func TestRefreshUsesIssuerAndRetainsIdentity(t *testing.T) {
	for _, method := range []string{"social", "idc"} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			a := fixtureOAuth()
			a.AuthMethod = method
			a.ClientID = "fixture-client"
			a.ClientSecret = "fixture-secret"
			a.ClientSecretExpiresAt = time.Now().Add(time.Hour).Unix()
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "" {
					t.Error("access token leaked to refresh")
				}
				var body map[string]string
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body["refreshToken"] != a.RefreshToken {
					t.Error("wrong refresh token")
				}
				if method == "idc" {
					if r.URL.Path != "/token" || body["clientId"] != a.ClientID || body["grantType"] != "refresh_token" {
						t.Error("wrong OIDC route")
					}
				} else if r.URL.Path != "/refreshToken" {
					t.Error("wrong social route")
				}
				io.WriteString(w, `{"accessToken":"rotated","idToken":"rotated-id","refreshToken":"new-refresh","expiresIn":3600}`)
			})
			wrong := c.Regions["us-east-1"]
			wrong.OIDC = "http://127.0.0.1:1"
			wrong.Social = wrong.OIDC
			c.Regions["us-east-1"] = wrong
			raw, e := c.Refresh(t.Context(), rawJSON(a))
			if e != nil || calls != 1 {
				t.Fatal(calls, e)
			}
			fresh, e := decode(raw)
			want := "rotated"
			if method == "idc" {
				want = "rotated-id"
			}
			if e != nil || fresh.AccessToken != want || fresh.RefreshToken != "new-refresh" || fresh.ExpiresAt <= time.Now().Unix() || identity(fresh).ID != identity(a).ID {
				t.Fatal(fresh, e)
			}
		})
	}
}
func TestDeviceFlowPollingCancellationAndExpiry(t *testing.T) {
	polls := 0
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client/register":
			io.WriteString(w, `{"clientId":"fixture-client","clientSecret":"fixture-secret","clientSecretExpiresAt":4102444800}`)
		case "/device_authorization":
			io.WriteString(w, `{"deviceCode":"private-device","expiresIn":600,"interval":5,"userCode":"ABCD-EFGH","verificationUriComplete":"https://view.awsapps.com/start/#/device?user_code=ABCD-EFGH"}`)
		case "/token":
			polls++
			if polls == 1 {
				w.WriteHeader(400)
				io.WriteString(w, `{"error":"slow_down"}`)
			} else {
				io.WriteString(w, `{"accessToken":"access","refreshToken":"rotated-refresh","expiresIn":3600}`)
			}
		default:
			t.Error("unexpected route", r.URL.Path)
			w.WriteHeader(500)
		}
	})
	login, e := c.BeginLogin(t.Context(), "us-east-1")
	if e != nil || !trustedDeviceURL(login.URL) {
		t.Fatal(login, e)
	}
	for range 2 {
		_, ready, e := c.CheckLogin(t.Context(), "us-east-1", login.State)
		if e != nil || ready {
			t.Fatal(ready, e)
		}
	}
	if polls != 1 {
		t.Fatal("poll interval ignored")
	}
	p := c.logins[login.State]
	p.mu.Lock()
	p.next = time.Now().Add(-time.Second)
	p.mu.Unlock()
	raw, ready, e := c.CheckLogin(t.Context(), "us-east-1", login.State)
	if e != nil || !ready {
		t.Fatal(e)
	}
	again, ready, e := c.CheckLogin(t.Context(), "us-east-1", login.State)
	if e != nil || !ready || !bytes.Equal(raw, again) || polls != 2 {
		t.Fatal("redeemed device code replayed", e)
	}
	if _, _, e = c.CheckLogin(t.Context(), "eu-central-1", login.State); e == nil {
		t.Fatal("cross-region flow accepted")
	}
	c.ReleaseLogin(login.State)
	if _, _, e = c.CheckLogin(t.Context(), "us-east-1", login.State); e == nil {
		t.Fatal("cancelled flow resumed")
	}
	login, e = c.BeginLogin(t.Context(), "us-east-1")
	if e != nil {
		t.Fatal(e)
	}
	c.logins[login.State].expires = time.Now().Add(-time.Second)
	if _, _, e = c.CheckLogin(t.Context(), "us-east-1", login.State); e == nil {
		t.Fatal("expired flow accepted")
	}
	for _, u := range []string{"https://view.awsapps.com.evil.test/", "https://user:pass@view.awsapps.com/", "http://view.awsapps.com/", "https://view.awsapps.com:1234/"} {
		if trustedDeviceURL(u) {
			t.Fatal("untrusted login URL", u)
		}
	}
}
func eventFrame(kind, payload string) []byte {
	return frameWithHeaders(map[string]string{":message-type": "event", ":event-type": kind}, payload)
}
func frameWithHeaders(h map[string]string, payload string) []byte {
	var headers bytes.Buffer
	for k, v := range h {
		headers.WriteByte(byte(len(k)))
		headers.WriteString(k)
		headers.WriteByte(7)
		_ = binary.Write(&headers, binary.BigEndian, uint16(len(v)))
		headers.WriteString(v)
	}
	b := make([]byte, 12)
	binary.BigEndian.PutUint32(b, uint32(16+headers.Len()+len(payload)))
	binary.BigEndian.PutUint32(b[4:], uint32(headers.Len()))
	binary.BigEndian.PutUint32(b[8:], crc32.ChecksumIEEE(b[:8]))
	b = append(b, headers.Bytes()...)
	b = append(b, []byte(payload)...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
}
func TestBinaryStreamPreservesToolsRefusalUsageAndRejectsDamage(t *testing.T) {
	stream := eventFrame("assistantResponseEvent", `{"content":"你好","refusal":"legitimate refusal"}`)
	for _, p := range []string{`{"toolUseId":"call-1","name":"lookup"}`, `{"toolUseId":"call-1","input":"{\"id\":9007199254740993,\"text\":\"You are Claude Code\"}"}`, `{"toolUseId":"call-1","stop":true}`} {
		stream = append(stream, eventFrame("toolUseEvent", p)...)
	}
	stream = append(stream, eventFrame("meteringEvent", `{"usage":0.5,"inputTokens":12,"outputTokens":4,"cacheReadInputTokens":3}`)...)
	var chunks [][]byte
	e := convertStream(t.Context(), bytes.NewReader(stream), "kiro/us-east-1/test", func(b []byte) error { chunks = append(chunks, b); return nil })
	if e != nil {
		t.Fatal(e)
	}
	joined := string(bytes.Join(chunks, []byte("\n")))
	for _, want := range []string{"你好", "legitimate refusal", "9007199254740993", "You are Claude Code", `"finish_reason":"tool_calls"`, `"prompt_tokens":12`, `"cached_tokens":3`} {
		if !strings.Contains(joined, want) {
			t.Fatal("lost stream data", want, joined)
		}
	}
	for _, damage := range []string{"prelude", "payload", "truncated", "garbage", "empty", "unfinished-tool", "exception"} {
		t.Run(damage, func(t *testing.T) {
			b := bytes.Clone(stream)
			switch damage {
			case "prelude":
				b[0] ^= 1
			case "payload":
				b[30] ^= 1
			case "truncated":
				b = b[:len(b)-3]
			case "garbage":
				b = append(b, 1, 2, 3)
			case "empty":
				b = nil
			case "unfinished-tool":
				b = eventFrame("toolUseEvent", `{"toolUseId":"call-1","name":"test","input":"{"}`)
			case "exception":
				b = frameWithHeaders(map[string]string{":message-type": "exception", ":exception-type": "throttlingException"}, `{"message":"private token must not leak"}`)
			}
			var output [][]byte
			e := convertStream(t.Context(), bytes.NewReader(b), "test", func(b []byte) error { output = append(output, b); return nil })
			if e == nil || strings.Contains(string(bytes.Join(output, nil)), `"finish_reason":"stop"`) {
				t.Fatal("damaged stream reported success")
			}
			if strings.Contains(e.Error(), "private token") {
				t.Fatal("upstream body exposed")
			}
		})
	}
	creditOnly := append(eventFrame("assistantResponseEvent", `{"content":"ok"}`), eventFrame("meteringEvent", `{"usage":1.5}`)...)
	var output [][]byte
	if e := convertStream(t.Context(), bytes.NewReader(creditOnly), "test", func(b []byte) error { output = append(output, b); return nil }); e != nil || strings.Contains(string(bytes.Join(output, nil)), `"usage"`) {
		t.Fatal("credits fabricated token usage", e)
	}
	sentinel := errors.New("consumer disconnected")
	if e := convertStream(t.Context(), bytes.NewReader(stream), "test", func([]byte) error { return sentinel }); !errors.Is(e, sentinel) {
		t.Fatal("emit cancellation swallowed")
	}
}
func TestRequestMappingPreservesContextToolsAndLargeIntegers(t *testing.T) {
	body := `{"messages":[{"role":"system","content":"SYSTEM CONTEXT"},{"role":"developer","content":"DEVELOPER CONTEXT"},{"role":"user","content":"question"},{"role":"assistant","content":"checking","tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"id\":9007199254740993}"}}]},{"role":"tool","tool_call_id":"call-1","content":"RESULT CONTEXT"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","const":9007199254740993}}}],"max_tokens":64}`
	native, e := chatBody(fixtureOAuth(), provider.Request{Model: "claude-sonnet-4.5", Body: []byte(body), SessionID: "session-fixture"})
	if e != nil {
		t.Fatal(e)
	}
	wire := string(rawJSON(native))
	for _, want := range []string{"SYSTEM CONTEXT", "DEVELOPER CONTEXT", "RESULT CONTEXT", "9007199254740993", "toolSpecification", "toolResults", "toolUses", "session-fixture"} {
		if !strings.Contains(wire, want) {
			t.Fatal("lost request content", want, wire)
		}
	}
	if strings.Contains(wire, "max_tokens") {
		t.Fatal("fabricated native output control")
	}
	for _, extra := range []string{`"tool_choice":"required"`, `"response_format":{"type":"json_object"}`, `"temperature":0.5`, `"thinking":{"type":"enabled","budget_tokens":1000}`, `"thinking": { "type": "disabled" }`, `"reasoning_effort":"none"`, `"parallel_tool_calls":false`} {
		_, e = chatBody(fixtureOAuth(), provider.Request{Model: "test", Body: []byte(`{"messages":[{"role":"user","content":"hi"}],` + extra + `}`)})
		if e == nil {
			t.Fatal("unsupported control silently ignored", extra)
		}
	}
	_, e = chatBody(fixtureOAuth(), provider.Request{Model: "test", Body: []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://127.0.0.1/private"}}]}]}`)})
	if e == nil {
		t.Fatal("remote image fetch permitted")
	}
}
func TestCatalogPaginationKeyAuthenticationAndMetadata(t *testing.T) {
	calls := 0
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("TokenType") != "API_KEY" || r.Header.Get("Authorization") != "Bearer ksk_fixture" || r.Header.Get("X-Amz-Target") != "AmazonCodeWhispererService.ListAvailableModels" {
			t.Error("wrong native authentication")
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if calls == 1 {
			io.WriteString(w, `{"models":[{"modelId":"model-a","modelName":"A","tokenLimits":{"maxInputTokens":200000,"maxOutputTokens":16000}}],"nextToken":"next"}`)
		} else {
			if body["nextToken"] != "next" {
				t.Error("pagination token missing")
			}
			io.WriteString(w, `{"models":[{"modelId":"model-b"}]}`)
		}
	})
	raw, _, e := c.Normalize([]byte(`{"api_key":"ksk_fixture"}`))
	if e != nil {
		t.Fatal(e)
	}
	models, e := c.Models(t.Context(), raw)
	if e != nil || len(models) != 2 || models[0].Context != 200000 || models[1].Context != 0 || calls != 2 {
		t.Fatal(models, e)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Capture production URLs without network access. A shared fake base URL alone
// cannot catch credentials sent to the wrong authentication surface or region.
func TestProductionRoutesSeparateAPIKeysFromOAuth(t *testing.T) {
	for _, region := range []string{"us-east-1", "eu-central-1"} {
		for _, keyAuth := range []bool{false, true} {
			t.Run(region+"/"+string(rawJSON(keyAuth)), func(t *testing.T) {
				c := New()
				a := fixtureOAuth()
				a.Region = region
				if keyAuth {
					a = Credentials{Region: region, AuthMethod: "api_key", APIKey: "ksk_fixture"}
				}
				calls := 0
				c.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					wantHost, wantPath := "q."+region+".amazonaws.com", "/"
					wantTarget := "AmazonCodeWhispererService.ListAvailableModels"
					if calls == 1 && !keyAuth && region == "us-east-1" {
						wantHost = "codewhisperer." + region + ".amazonaws.com"
					}
					if calls == 2 {
						wantPath, wantTarget = "/generateAssistantResponse", ""
						if keyAuth {
							wantPath, wantTarget = "/", "AmazonCodeWhispererStreamingService.GenerateAssistantResponse"
						}
					}
					if calls > 2 || r.Method != "POST" || r.URL.Scheme != "https" || r.URL.Host != wantHost || r.URL.Path != wantPath || r.Header.Get("X-Amz-Target") != wantTarget {
						t.Fatalf("unexpected provider route: %s %s target=%s", r.Method, r.URL, r.Header.Get("X-Amz-Target"))
					}
					contentType := "application/json"
					payload := []byte(`{"models":[{"modelId":"native-1.0"}]}`)
					if calls == 2 {
						contentType = "application/vnd.amazon.eventstream"
						payload = eventFrame("assistantResponseEvent", `{"content":"complete"}`)
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(bytes.NewReader(payload))}, nil
				})
				if _, e := c.Models(t.Context(), rawJSON(a)); e != nil {
					t.Fatal(e)
				}
				if e := c.Stream(t.Context(), rawJSON(a), provider.Request{Model: "native-1.0", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil }); e != nil || calls != 2 {
					t.Fatal(calls, e)
				}
			})
		}
	}
}
func TestHTTPRedirectsAndCancellation(t *testing.T) {
	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1); w.WriteHeader(200) }))
	defer target.Close()
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	_, e := c.Models(t.Context(), rawJSON(Credentials{APIKey: "ksk_fixture", AuthMethod: "api_key", Region: "us-east-1"}))
	if e == nil || leaked.Load() != 0 {
		t.Fatal("credential followed redirect", e)
	}
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	c = fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		w.Write(eventFrame("assistantResponseEvent", `{"content":"first"}`))
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
		close(cancelled)
	})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- c.Stream(ctx, rawJSON(fixtureOAuth()), provider.Request{Model: "test", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil })
	}()
	<-entered
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("upstream cancellation stalled")
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream connection retained")
	}
}
