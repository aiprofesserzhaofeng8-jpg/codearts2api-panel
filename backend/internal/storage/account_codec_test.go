package storage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"codearts2api/internal/accounts"
	"codearts2api/internal/codearts"
)

// These are schema-6 fields emitted before the neutral account extraction.
// Large credential numbers and platform-specific extensions must stay opaque.
const legacyAccountsJSON = `[
 {"id":"ca","name":"CodeArts fixture","source":"manual","user_data":"/fixture/ide","enabled":true,"paused":false,"status":"ok","failures":0,"last_used":123,
  "session":{"account_label":"fixture","domain_id":"domain","access_key":"ak","secret_key":"sk","security_token":"sts","access_token":"access","expires_at":"2030-01-02T03:04:05Z","refresh_token":"refresh","dpop_private_jwk":{"kty":"EC","d":"fixture"},"dpop_public_jwk":{"kty":"EC","x":"fixture"},"code_verifier":"verifier","source":"store"}},
 {"provider":"codearts","id":"permanent","name":"IAM fixture","source":"manual","user_data":"","enabled":true,"paused":false,"status":"ok","failures":0,"last_used":0,
  "session":{"account_label":"","domain_id":"iam","access_key":"iam-ak","secret_key":"iam-sk","security_token":"","access_token":"","expires_at":"0001-01-01T00:00:00Z","refresh_token":"","credential_kind":"aksk"}},
 {"provider":"workbuddy","scope":"cn","external_id":"wb-user","provider_credentials":{"token":"wb-fixture","nested":{"id":9007199254740993,"future":[false,null,"value"]}},"id":"wb","name":"WorkBuddy fixture","source":"manual","user_data":"","enabled":true,"paused":true,"max_in_flight":2,"cooldown_until":1234,"cooldown_reason":"fixture","status":"ok","failures":1,"last_err":"fixture error","last_used":456,"note":"keep note","last_claim_state":"unknown","next_claim_at":567,"last_claim_date":"2026-11-01","last_claim_at":789,"last_claim_err":"pending","last_claim_msg":"keep message"},
 {"provider":"trae","scope":"global","external_id":"tr-user","provider_credentials":{"token":"tr-fixture","extra":{"id":9007199254740995}},"id":"tr","name":"Trae fixture","source":"manual","user_data":"","enabled":true,"paused":false,"status":"ok","failures":0,"last_used":0}
]`

func normalizedJSON(t *testing.T, raw []byte) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestAccountCodecPreservesLegacySchemaAndCredentialGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	raw := []byte(`{"version":6,"accounts":` + legacyAccountsJSON + `,"settings":{"request_timeout_sec":123}}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ca := st.Account("ca")
	if st.Settings().RequestTimeout != 123 {
		t.Fatal("account codec lost other state fields")
	}
	session := codearts.StoredSession(ca)
	if ca.ProviderID() != "codearts" || session == nil || !session.HasRefresh() || session.CodeVerifier != "verifier" {
		t.Fatal("legacy CodeArts credentials were not reconstructed")
	}
	if codearts.StoredSession(st.Account("ca")) != session {
		t.Fatal("snapshots must share the immutable credential generation")
	}
	if !codearts.StoredSession(st.Account("permanent")).IsPermanent() {
		t.Fatal("permanent IAM credentials changed kind")
	}
	for _, id := range []string{"wb", "tr"} {
		if st.Account(id).NativeCredential != nil {
			t.Fatal("non-CodeArts account acquired a legacy session")
		}
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Version  int             `json:"version"`
		Accounts json.RawMessage `json:"accounts"`
	}
	if err := json.Unmarshal(written, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Version != StateVersion || !reflect.DeepEqual(normalizedJSON(t, []byte(legacyAccountsJSON)), normalizedJSON(t, wire.Accounts)) {
		t.Fatal("schema or serialized account fields changed")
	}
	reloaded, err := Open(path)
	if err != nil || codearts.StoredSession(reloaded.Account("ca")).RefreshToken != "refresh" {
		t.Fatal("saved credential could not be restored", err)
	}
	if !reflect.DeepEqual(normalizedJSON(t, st.Account("wb").Credentials), normalizedJSON(t, reloaded.Account("wb").Credentials)) {
		t.Fatal("opaque provider credentials changed after restart")
	}
}

func TestAccountCodecMissingNullAndInvalidSessions(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
		want int
	}{
		{"missing accounts preserves defaults", `{}`, 1},
		{"null accounts", `{"accounts":null}`, 0},
		{"empty accounts", `{"accounts":[]}`, 0},
		{"missing session", `{"accounts":[{"id":"fixture"}]}`, 1},
		{"null session", `{"accounts":[{"id":"fixture","session":null}]}`, 1},
		{"null element", `{"accounts":[null]}`, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := State{Accounts: []*accounts.Account{{ID: "default"}}, Settings: DefaultSettings()}
			if err := json.Unmarshal([]byte(test.raw), &state); err != nil {
				t.Fatal(err)
			}
			if len(state.Accounts) != test.want || state.Settings.RequestTimeout != DefaultSettings().RequestTimeout {
				t.Fatal("missing fields changed default decoding")
			}
			if len(state.Accounts) == 1 && state.Accounts[0] != nil && state.Accounts[0].NativeCredential != nil {
				t.Fatal("missing session must be a nil interface")
			}
		})
	}
	for _, raw := range []string{`{"accounts":{}}`, `{"accounts":[{"session":"invalid"}]}`, `{"accounts":[{"session":{"expires_at":"invalid"}}]}`} {
		var state State
		if err := json.Unmarshal([]byte(raw), &state); err == nil {
			t.Fatal("invalid state was silently accepted")
		}
	}
}

func TestClearingLegacySessionKeepsNilAndCAS(t *testing.T) {
	st := testPool(t, 1)
	original := codearts.StoredSession(st.Account("0"))
	if err := st.CommitAccountSession("0", original, nil); err != nil {
		t.Fatal(err)
	}
	if st.Account("0").NativeCredential != nil {
		t.Fatal("clearing session stored a typed nil interface")
	}
	if err := st.CommitAccountSession("0", original, original); err != accounts.ErrCredentialChanged {
		t.Fatal("stale generation was accepted after clearing")
	}
	st.SetAccountSession("0", original)
	if st.SetAccountSession("0", nil) || codearts.StoredSession(st.Account("0")) != original {
		t.Fatal("session setter must continue rejecting nil updates")
	}
}
