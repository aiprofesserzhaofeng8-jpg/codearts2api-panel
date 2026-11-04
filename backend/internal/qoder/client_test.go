package qoder

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/provider"
)

func credential() json.RawMessage {
	b, _ := json.Marshal(Credentials{AccessToken: "dt-private-fixture", RefreshToken: "drt-private-fixture", UID: "fixture-user", Name: "Fixture", ExpiresAt: time.Now().Add(time.Hour).Unix(), MachineID: "machine-fixture", Scope: "cn", UserType: "personal_standard"})
	return b
}

func fixture(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	c := New()
	for scope, region := range c.Regions {
		region.OpenAPI, region.Inference = s.URL, s.URL
		c.Regions[scope] = region
	}
	return c
}

// Decode using a separate standard-base64 permutation so tests inspect the
// actual signed HTTP payload, rather than just the request-builder helper.
func decodedRequest(t *testing.T, r *http.Request) []byte {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(b)
	parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer COSY."), ".")
	if len(parts) != 2 {
		t.Fatal("missing COSY authorization")
	}
	path := strings.TrimPrefix(r.URL.Path, "/algo")
	sig := md5.Sum([]byte(strings.Join([]string{parts[0], r.Header.Get("Cosy-Key"), r.Header.Get("Cosy-Date"), encoded, path}, "\n")))
	if parts[1] != hex.EncodeToString(sig[:]) || r.Header.Get("Cosy-User") != "fixture-user" || r.Header.Get("Cosy-MachineId") != "machine-fixture" {
		t.Fatal("signature or identity mismatch")
	}
	if r.Header.Get("Cosy-MachineToken") != "" || r.Header.Get("Cosy-MachineType") != "" {
		t.Fatal("fabricated UMID headers")
	}
	k := len(encoded) / 3
	s := encoded[len(encoded)-k:] + encoded[k:len(encoded)-k] + encoded[:k]
	b = []byte(s)
	const standard = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	for i, ch := range b {
		if ch == '$' {
			b[i] = '='
		} else {
			idx := strings.IndexByte(bodyAlphabet, ch)
			if idx < 0 {
				t.Fatal("invalid body alphabet")
			}
			b[i] = standard[idx]
		}
	}
	plain, err := base64.StdEncoding.DecodeString(string(b))
	if err != nil {
		t.Fatal(err)
	}
	return plain
}

func TestNormalizeExpiryAndRequiredIdentity(t *testing.T) {
	c := New()
	for _, expiry := range []string{`"2030-01-01T00:00:00Z"`, `1893456000`, `1893456000000`, `"1893456000"`} {
		_, who, err := c.Normalize([]byte(`{"access_token":"dt-fixture","uid":"user","scope":"global","machine_id":"device","expires_at":` + expiry + `}`))
		if err != nil || who.ExpiresAt != 1893456000 || who.Scope != "global" {
			t.Fatalf("expiry %s: %+v %v", expiry, who, err)
		}
	}
	for _, field := range []string{"access_token", "uid", "scope", "machine_id", "expires_at"} {
		var obj map[string]json.RawMessage
		_ = json.Unmarshal(credential(), &obj)
		delete(obj, field)
		b, _ := json.Marshal(obj)
		if _, _, err := c.Normalize(b); err == nil {
			t.Fatalf("accepted missing %s", field)
		}
	}
	var obj map[string]any
	_ = json.Unmarshal(credential(), &obj)
	obj["access_token"] = "private\r\nInjected: yes"
	b, _ := json.Marshal(obj)
	if _, _, err := c.Normalize(b); err == nil {
		t.Fatal("accepted header injection")
	}
}

func TestDeviceLoginPKCEScopeAndPreview(t *testing.T) {
	var nonce, challenge string
	var polls atomic.Int32
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/deviceToken/poll":
			q := r.URL.Query()
			digest := sha256.Sum256([]byte(q.Get("verifier")))
			if q.Get("nonce") != nonce || q.Get("challenge_method") != "S256" || base64.RawURLEncoding.EncodeToString(digest[:]) != challenge || r.Header.Get("Authorization") != "" {
				t.Error("PKCE exchange mismatch")
			}
			if polls.Add(1) == 1 {
				w.WriteHeader(202)
				return
			}
			io.WriteString(w, `{"token":"dt-fixture","refresh_token":"drt-fixture","expires_in":3600000}`)
		case "/api/v1/userinfo":
			if r.Header.Get("Authorization") != "Bearer dt-fixture" {
				t.Error("wrong userinfo token")
			}
			io.WriteString(w, `{"id":"fixture-user","name":"Verified","user_type":"personal_standard"}`)
		default:
			t.Error("unexpected request", r.URL.Path)
		}
	})
	for _, scope := range []string{"cn", "global"} {
		polls.Store(0)
		login, err := c.BeginLogin(context.Background(), scope)
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(login.URL)
		nonce, challenge = u.Query().Get("nonce"), u.Query().Get("challenge")
		if u.Scheme != "https" || u.Query().Get("machine_id") == "" || (scope == "cn") != (u.Query().Get("redirect_uri") != "") {
			t.Fatal("bad regional authorization URL")
		}
		if _, ready, err := c.CheckLogin(context.Background(), scope, login.State); ready || err != nil {
			t.Fatal("pending authorization failed")
		}
		raw, ready, err := c.CheckLogin(context.Background(), scope, login.State)
		if err != nil || !ready {
			t.Fatalf("login: %v", err)
		}
		_, who, err := c.Normalize(raw)
		if err != nil || who.ID != "fixture-user" || who.Name != "Verified" || who.Scope != scope || !who.HasRefresh {
			t.Fatalf("bad verified identity: %+v %v", who, err)
		}
		c.ReleaseLogin(login.State)
		if _, _, err := c.CheckLogin(context.Background(), scope, login.State); err == nil {
			t.Fatal("canceled login can still be polled")
		}
	}
}

func TestRefreshRotatesWithoutFallibleFollowup(t *testing.T) {
	var calls int
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/deviceToken/refresh" || r.Method != "POST" {
			t.Error("wrong refresh request")
		}
		io.WriteString(w, `{"token":"dt-rotated","refresh_token":"drt-rotated","expires_in":7200000}`)
	})
	b, err := c.Refresh(context.Background(), credential())
	if err != nil || calls != 1 {
		t.Fatalf("refresh calls=%d err=%v", calls, err)
	}
	a, _ := decode(b)
	if a.AccessToken != "dt-rotated" || a.RefreshToken != "drt-rotated" || a.UID != "fixture-user" || a.MachineID != "machine-fixture" || a.ExpiresAt < time.Now().Add(time.Hour).Unix() {
		t.Fatal("rotated credentials not retained")
	}
}

func TestCatalogSignedBodyEnabledModelsOnly(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/algo/api/v2/model/list" || string(decodedRequest(t, r)) != `{}` || r.Header.Get("X-Model-Key") != "" {
			t.Error("wrong catalog request")
		}
		io.WriteString(w, `{"chat":[{"key":"qmodel","display_name":"Qwen Version","max_input_tokens":200000},{"key":"disabled","enable":false},{"key":"zero","enable":0},{"key":"custom","source":"custom"},{"key":"same"},{"key":"same","enable":false}],"embedding":[{"key":"not-chat"}]}`)
	})
	models, err := c.Models(context.Background(), credential())
	if err != nil || len(models) != 1 || models[0].ID != "qmodel" || models[0].Context != 200000 || models[0].Name != "Qwen Version" || models[0].Provider != "qoder" {
		t.Fatalf("catalog: %+v %v", models, err)
	}
}

func frame(inner string) string {
	b, _ := json.Marshal(map[string]any{"statusCodeValue": 200, "body": inner})
	return "data: " + string(b) + "\n\n"
}

const completion = `{"id":"qoder-test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"OK","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"id\":9007199254740993}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":12,"completion_tokens":4}}`

func TestStreamPreservesMessagesToolsAndSignature(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		plain := decodedRequest(t, r)
		var body map[string]json.RawMessage
		_ = json.Unmarshal(plain, &body)
		if r.URL.Path != "/algo/api/v2/service/pro/sse/agent_chat_generation" || r.Header.Get("X-Model-Key") != "qmodel" || r.URL.Query().Get("AgentId") != "agent_common" {
			t.Error("wrong inference route")
		}
		if !strings.Contains(string(body["messages"]), "9007199254740993") || !strings.Contains(string(body["messages"]), "developer") || !strings.Contains(string(body["tools"]), "read") || !strings.Contains(string(body["parameters"]), "max_tokens") {
			t.Error("message/tool/parameter lost")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, frame(completion)+frame("[DONE]")+"event: finish\ndata: {}\n\n")
	})
	request := provider.Request{Model: "qmodel", SessionID: "session", Body: []byte(`{"messages":[{"role":"developer","content":"Preserve me"},{"role":"user","content":"9007199254740993"}],"tools":[{"type":"function","function":{"name":"read","parameters":{"type":"object"}}}],"max_completion_tokens":32}`)}
	var chunks [][]byte
	err := c.Stream(context.Background(), credential(), request, func(b []byte) error { chunks = append(chunks, b); return nil })
	if err != nil || len(chunks) != 1 || !strings.Contains(string(chunks[0]), "qoder/cn/qmodel") || !strings.Contains(string(chunks[0]), "9007199254740993") {
		t.Fatalf("stream: %v, %q", err, chunks)
	}
}

func TestStreamRejectsEmptyTruncatedAndErrorEnvelopes(t *testing.T) {
	for name, response := range map[string]string{
		"empty":          "event: finish\ndata: {}\n\n",
		"truncated":      frame(completion) + frame("[DONE]"),
		"envelope error": `data: {"statusCodeValue":401,"body":"secret fixture"}` + "\n\n",
		"body error":     frame(`{"error":{"message":"secret fixture"}}`),
		"invalid":        "data: bad json\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, response)
			})
			err := c.Stream(context.Background(), credential(), provider.Request{Model: "qmodel", Body: []byte(`{"messages":[{"role":"user","content":"test"}]}`)}, func([]byte) error { return nil })
			if err == nil || strings.Contains(err.Error(), "secret fixture") {
				t.Fatalf("unsafe stream result: %v", err)
			}
		})
	}
}

func TestStreamCancellationAndConsumerErrors(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, frame(completion)+"event: finish\ndata: {}\n\n")
	})
	want := errors.New("downstream disconnected")
	request := provider.Request{Model: "qmodel", Body: []byte(`{"messages":[{"role":"user","content":"test"}]}`)}
	if err := c.Stream(context.Background(), credential(), request, func([]byte) error { return want }); !errors.Is(err, want) {
		t.Fatalf("consumer error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Stream(ctx, credential(), request, func([]byte) error { t.Fatal("canceled request emitted data"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestStreamCancelsBlockedUpstream(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(stopped)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- c.Stream(ctx, credential(), provider.Request{Model: "qmodel", Body: []byte(`{"messages":[{"role":"user","content":"test"}]}`)}, func([]byte) error { return nil })
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled stream stayed blocked")
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream connection not canceled")
	}
}
