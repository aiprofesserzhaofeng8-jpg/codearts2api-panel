package vertex

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T) (credentials, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	b, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return credentials{Service: serviceAccount{Type: "service_account", Project: "test-project", Email: "test@test-project.iam.gserviceaccount.com", KeyID: "fixture-key", Key: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b})), TokenURI: "https://untrusted.invalid/token"}, Location: "global"}, key
}
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}
func TestExchangeSignatureAndIdentity(t *testing.T) {
	c, key := fixture(t)
	raw, _ := json.Marshal(c)
	a := NewAuth()
	canonical, id, err := a.Normalize(raw)
	if err != nil || !id.HasRefresh || id.NonExpiring || id.ExpiresAt != 0 {
		t.Fatal(id, err)
	}
	if strings.Contains(string(canonical), "untrusted.invalid") {
		t.Fatal("custom endpoint retained")
	}
	a.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != tokenURL || r.Method != "POST" {
			t.Fatal("unexpected token endpoint")
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Fatal("grant")
		}
		parts := strings.Split(r.Form.Get("assertion"), ".")
		if len(parts) != 3 {
			t.Fatal("JWT")
		}
		h, _ := base64.RawURLEncoding.DecodeString(parts[0])
		var header map[string]string
		_ = json.Unmarshal(h, &header)
		if header["alg"] != "RS256" || header["kid"] != "fixture-key" {
			t.Fatal(header)
		}
		b, _ := base64.RawURLEncoding.DecodeString(parts[1])
		var claims map[string]any
		_ = json.Unmarshal(b, &claims)
		if claims["iss"] != c.Service.Email || claims["aud"] != tokenURL || claims["scope"] != cloudScope || claims["exp"].(float64)-claims["iat"].(float64) != 3600 {
			t.Fatal(claims)
		}
		sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
		digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		if rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig) != nil {
			t.Fatal("signature")
		}
		return response(200, `{"access_token":"fresh-token","token_type":"Bearer","expires_in":3600}`), nil
	})
	fresh, err := a.Refresh(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	_, next, err := a.Normalize(fresh)
	if err != nil || next.ID != id.ID || next.ExpiresAt < time.Now().Unix()+3500 {
		t.Fatal(next, err)
	}
	c.Service.KeyID = "rotated-key"
	raw, _ = json.Marshal(c)
	_, rotated, err := a.Normalize(raw)
	if err != nil || rotated.ID != id.ID {
		t.Fatal("key rotation changed identity", err)
	}
	c.Project = "other-project"
	raw, _ = json.Marshal(c)
	_, other, err := a.Normalize(raw)
	if err != nil || other.ID == id.ID {
		t.Fatal("project collision", err)
	}
}
func TestValidationErrorsAndCancellation(t *testing.T) {
	c, _ := fixture(t)
	a := NewAuth()
	for _, mutate := range []func(*credentials){
		func(c *credentials) { c.Service.Key = "bad-private-secret" }, func(c *credentials) { c.Service.Type = "external_account" }, func(c *credentials) { c.Location = "evil.invalid/path" }, func(c *credentials) { c.Scope = "elsewhere" }, func(c *credentials) { c.Access = "token" }, func(c *credentials) { c.Project = "../escape" },
	} {
		bad := c
		mutate(&bad)
		b, _ := json.Marshal(bad)
		if _, _, err := a.Normalize(b); err == nil {
			t.Fatal("invalid credential accepted")
		}
	}
	raw, _ := json.Marshal(c)
	for _, tc := range []struct {
		status int
		body   string
		want   int
	}{
		{400, `{"error":"invalid_grant","error_description":"private-secret"}`, 401},
		{403, `{"error":"permission_denied","error_description":"private-secret"}`, 403},
		{429, `{"error":"rate_limited"}`, 429},
		{429, `rate limited`, 429},
		{503, `<html>private-secret</html>`, 503},
		{200, `{"access_token":"token","token_type":"Bearer","expires_in":0}`, 502},
		{200, `{"access_token":"token","token_type":"Bearer","expires_in":7200}`, 502},
		{200, `{"access_token":"token","token_type":"other","expires_in":3600}`, 502},
		{200, strings.Repeat("x", (1<<20)+1), 502},
	} {
		a.HTTP.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return response(tc.status, tc.body), nil })
		_, err := a.Refresh(t.Context(), raw)
		var fault *provider.Error
		if !errors.As(err, &fault) || fault.Status != tc.want || strings.Contains(err.Error(), "private-secret") {
			t.Fatal(tc.status, err)
		}
	}
	a.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := a.Refresh(ctx, raw); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestCoordinatorExchangesOnceAndPersists(t *testing.T) {
	c, _ := fixture(t)
	raw, _ := json.Marshal(c)
	a := NewAuth()
	raw, id, err := a.Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	st, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.UpsertAccountAndSave(&accounts.Account{ID: "a", Provider: "vertex", Scope: id.Scope, ExternalID: id.ID, Credentials: raw}, false); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	a.HTTP.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return response(200, `{"access_token":"fresh-token","token_type":"Bearer","expires_in":3600}`), nil
	})
	runtime := accounts.NewProviderCredentials(st, func() bool { return true })
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, err := runtime.Resolve(t.Context(), "a", a)
			if err != nil || !strings.Contains(string(b), "fresh-token") {
				t.Error("resolve", err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("duplicate exchanges", calls.Load())
	}
	reopened, err := storage.Open(st.Path())
	if err != nil || !strings.Contains(string(reopened.Account("a").Credentials), "fresh-token") {
		t.Fatal("token not persisted", err)
	}
}
func TestRegionalAndGlobalEndpoints(t *testing.T) {
	for _, loc := range []string{"global", "us-central1", "europe-west4"} {
		u, err := GeminiURL("test-project", loc, "gemini-2.5-pro")
		if err != nil {
			t.Fatal(err)
		}
		host := loc + "-aiplatform.googleapis.com"
		if loc == "global" {
			host = "aiplatform.googleapis.com"
		}
		if !strings.HasPrefix(u, "https://"+host+"/v1/projects/test-project/locations/"+loc+"/") || !strings.HasSuffix(u, "gemini-2.5-pro:streamGenerateContent?alt=sse") {
			t.Fatal(u)
		}
	}
	for _, m := range []string{"../escape", "gemini-test?key=secret", "claude-sonnet", "gemini-test/path"} {
		if _, err := GeminiURL("test-project", "global", m); err == nil {
			t.Fatal(m)
		}
	}
}
