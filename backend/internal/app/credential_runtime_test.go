package app_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/app"
	"codearts2api/internal/codearts"
	"codearts2api/internal/server"
	"codearts2api/internal/storage"
)

type credentialTransport func(*http.Request) (*http.Response, error)

func (f credentialTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise the assembled native refresh, runtime and real conditional store
// transaction together, without contacting an external token endpoint.
func credentialFixture(t *testing.T, beforeReply func()) (*server.Server, *storage.Store, *codearts.Session, *atomic.Int32) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encode := func(v *big.Int) string { return base64.RawURLEncoding.EncodeToString(v.FillBytes(make([]byte, 32))) }
	jwk, _ := json.Marshal(map[string]string{"kty": "EC", "crv": "P-256", "x": encode(key.X), "y": encode(key.Y), "d": encode(key.D)})
	original := &codearts.Session{AccessKey: "fixture-old-ak", SecretKey: "fixture-old-sk", DomainID: "fixture-domain", RefreshToken: "fixture-original-token", CodeVerifier: "fixture-verifier", DPoPPrivateJWK: jwk, ExpiresAt: time.Now().Add(-time.Hour)}
	st, err := storage.Open(filepath.Join(t.TempDir(), "data", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	st.UpsertAccount(&accounts.Account{ID: "fixture", Name: "Fixture", Enabled: true, NativeCredential: original})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	srv, err := app.Build(st)
	if err != nil {
		t.Fatal(err)
	}
	srv.Up.TokenURL = "https://credential-fixture.invalid/token"
	calls := new(atomic.Int32)
	srv.Up.HTTP.Transport = credentialTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.String() != srv.Up.TokenURL || r.Method != "POST" {
			t.Error("unexpected native token endpoint")
		}
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != original.RefreshToken || r.Form.Get("code_verifier") != original.CodeVerifier || r.Form.Get("client_id") != codearts.ClientID {
			t.Error("native refresh form changed")
		}
		proof := strings.Split(r.Header.Get("DPoP"), ".")
		if len(proof) != 3 {
			t.Error("missing DPoP proof")
		} else {
			signature, _ := base64.RawURLEncoding.DecodeString(proof[2])
			digest := sha256.Sum256([]byte(proof[0] + "." + proof[1]))
			if len(signature) != 64 || !ecdsa.Verify(&key.PublicKey, digest[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])) {
				t.Error("DPoP signature no longer matches original login key")
			}
			raw, _ := base64.RawURLEncoding.DecodeString(proof[1])
			var payload map[string]any
			_ = json.Unmarshal(raw, &payload)
			if payload["htm"] != "POST" || payload["htu"] != srv.Up.TokenURL {
				t.Error("DPoP target changed")
			}
		}
		if beforeReply != nil {
			beforeReply()
		}
		body := fmt.Sprintf(`{"credentials":{"access_key_id":"fixture-new-ak","secret_access_key":"fixture-new-sk","security_token":"fixture-new-sts","expiration":%q},"refresh_token":"fixture-rotated-token"}`, time.Now().Add(2*time.Hour).Format(time.RFC3339))
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	return srv, st, original, calls
}

func TestCredentialRuntimeAssembledConcurrentRefreshPublishesOnce(t *testing.T) {
	srv, st, original, calls := credentialFixture(t, nil)
	var wg sync.WaitGroup
	results := make(chan *codearts.Session, 16)
	for range 16 {
		snapshot := st.Account("fixture")
		wg.Go(func() {
			value, err := srv.Cred.Session(t.Context(), snapshot, codearts.ResolveOptions{AllowRefresh: true})
			if err != nil {
				t.Error(err)
				return
			}
			results <- value
		})
	}
	wg.Wait()
	close(results)
	latest := codearts.StoredSession(st.Account("fixture"))
	count := 0
	for value := range results {
		count++
		if value != latest {
			t.Fatal("concurrent requests borrowed different generations")
		}
	}
	if count != 16 || calls.Load() != 1 || latest.RefreshToken != "fixture-rotated-token" || latest.Source != "refresh" || string(latest.DPoPPrivateJWK) != string(original.DPoPPrivateJWK) {
		t.Fatal("rotation or native credential continuity failed")
	}
	reopened, err := storage.Open(st.Path())
	if err != nil || codearts.StoredSession(reopened.Account("fixture")).RefreshToken != latest.RefreshToken {
		t.Fatal("rotation not durable", err)
	}
}

func TestCredentialRuntimeAssembledSaveFailureKeepsRotatedGeneration(t *testing.T) {
	srv, st, original, calls := credentialFixture(t, nil)
	dir := filepath.Dir(st.Path())
	if err := os.Rename(dir, dir+"-saved"); err != nil {
		t.Fatal(err)
	}
	restored := false
	t.Cleanup(func() {
		if !restored {
			_ = os.Remove(dir)
			_ = os.Rename(dir+"-saved", dir)
		}
	})
	if err := os.WriteFile(dir, []byte("block persistence"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Cred.Session(t.Context(), st.Account("fixture"), codearts.ResolveOptions{AllowRefresh: true}); err == nil {
		t.Fatal("failed persistence reported success")
	}
	latest := codearts.StoredSession(st.Account("fixture"))
	if latest == original || latest.RefreshToken != "fixture-rotated-token" {
		t.Fatal("single-use token rolled back after persistence failure")
	}
	got, err := srv.Cred.Session(t.Context(), &accounts.Account{ID: "fixture", NativeCredential: original}, codearts.ResolveOptions{AllowRefresh: false})
	if err != nil || got != latest || calls.Load() != 1 {
		t.Fatal("retry reused old token or lost rotated generation", err)
	}
	srv.Cred.Invalidate("fixture")
	got, err = srv.Cred.Session(t.Context(), st.Account("fixture"), codearts.ResolveOptions{AllowRefresh: true})
	if err != nil || got != latest || calls.Load() != 1 {
		t.Fatal("cache invalidation caused duplicate refresh", err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir+"-saved", dir); err != nil {
		t.Fatal(err)
	}
	restored = true
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(st.Path())
	if err != nil || codearts.StoredSession(reopened.Account("fixture")).RefreshToken != latest.RefreshToken {
		t.Fatal("rotation not recoverable", err)
	}
}

func TestCredentialRuntimeAssembledStaleRefreshRejected(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(fmt.Sprintf("delete=%t", remove), func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			srv, st, _, calls := credentialFixture(t, func() { close(entered); <-release })
			result := make(chan error, 1)
			snapshot := st.Account("fixture")
			go func() {
				_, err := srv.Cred.Session(t.Context(), snapshot, codearts.ResolveOptions{AllowRefresh: true})
				result <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("refresh not entered")
			}
			fresh := &codearts.Session{AccessKey: "fixture-manual-login", SecretKey: "fixture-new-secret", DomainID: "fixture-domain", ExpiresAt: time.Now().Add(time.Hour)}
			if remove {
				if _, err := st.RemoveAccountAndSave("fixture"); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := st.UpsertAccountAndSave(&accounts.Account{ID: "fixture", NativeCredential: fresh}, true); err != nil {
					t.Fatal(err)
				}
			}
			unblock()
			select {
			case err := <-result:
				if !errors.Is(err, accounts.ErrCredentialChanged) {
					t.Fatal("stale refresh accepted", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("refresh not released")
			}
			got, err := srv.Cred.Session(t.Context(), snapshot, codearts.ResolveOptions{AllowRefresh: true})
			if remove {
				if !errors.Is(err, accounts.ErrCredentialChanged) || st.Account("fixture") != nil {
					t.Fatal("deleted account resurrected", err)
				}
			} else if err != nil || got != fresh {
				t.Fatal("new login not preferred", err)
			}
			if calls.Load() != 1 {
				t.Fatal("replayed a consumed refresh token")
			}
		})
	}
}
