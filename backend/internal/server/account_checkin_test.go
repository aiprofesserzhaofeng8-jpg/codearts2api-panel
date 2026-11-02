package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
)

type checkinAdapter struct {
	provider.Adapter
	failure bool
	calls   int
}

func (a *checkinAdapter) Describe() provider.Descriptor {
	return provider.Descriptor{ID: provider.WorkBuddy, Available: true}
}
func (a *checkinAdapter) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	return raw, provider.Identity{ID: "fixture", Scope: "cn", ExpiresAt: 4102444800}, nil
}
func (a *checkinAdapter) Models(context.Context, json.RawMessage) ([]provider.Model, error) {
	a.calls++
	if a.failure {
		return nil, errors.New("catalog fixture failure")
	}
	return []provider.Model{{ID: "new-model", Context: 131072, MaxOutput: 8192}}, nil
}

// The same test can run against the frozen pre-migration checkout. Stable wire
// records cover permissive decoding, native failures and catalog partitioning.
func TestAccountCheckinWireContract(t *testing.T) {
	for _, name := range []string{"empty", "malformed", "get", "unknown", "untrimmed", "native-error", "missing-model", "no-session", "provider-ok", "provider-error"} {
		t.Run(name, func(t *testing.T) {
			s := newAdminTestServer(t, "native")
			s.Store.SetAccountEnabled("native", false)
			s.Store.SetAccountPaused("native", true)
			calls := 0
			s.Up.HTTP = &http.Client{Transport: probeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				reply := `{"choices":[{"message":{"content":"hi"}}]}`
				if name == "native-error" {
					reply = `{"error_code":"4005","error_msg":"fixture unavailable"}`
				}
				return probeFakeResponse(r, 200, reply), nil
			})}
			method, body := http.MethodPost, `{"id":"native"}`
			switch name {
			case "empty":
				body = ""
			case "malformed":
				body = "{"
			case "get":
				method = http.MethodGet
			case "unknown":
				body = `{"id":"unknown"}`
			case "untrimmed":
				body = `{"id":" native "}`
			case "missing-model":
				s.Store.UpdateSettings(func(st *storage.Settings) { st.DefaultModel = "absent-probe-model" })
			case "no-session":
				if _, err := s.Store.RemoveAccountAndSave("native"); err != nil {
					t.Fatal(err)
				}
				s.Store.UpsertAccount(&accounts.Account{ID: "native", Name: "acct-native", Source: "manual", Enabled: false})
			}
			adapter := &checkinAdapter{failure: name == "provider-error"}
			if name == "provider-ok" || name == "provider-error" {
				s.Providers = provider.NewRegistry()
				if err := s.Providers.Register(adapter); err != nil {
					t.Fatal(err)
				}
				s.Store.UpsertAccount(&accounts.Account{ID: "external", Name: "external", Provider: provider.WorkBuddy, Scope: "cn", ExternalID: "fixture", Credentials: json.RawMessage(`{"fixture":true}`)})
				s.Reg.ReplaceProvider(provider.WorkBuddy, "global", []provider.Model{{ID: "preserved"}})
				s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "old-model"}})
				body = `{"id":"external"}`
			}
			rec := adminProbeRequest(t, s, method, "/admin/api/checkin", body)
			if rec.Code != 200 {
				t.Fatal(rec.Code, rec.Body.String())
			}
			var response struct {
				OK      bool             `json:"ok"`
				Results []map[string]any `json:"results"`
			}
			if json.Unmarshal(rec.Body.Bytes(), &response) != nil || !response.OK || response.Results == nil {
				t.Fatal("wire envelope changed", rec.Body.String())
			}
			wantCount := 1
			if name == "unknown" || name == "untrimmed" {
				wantCount = 0
			}
			if len(response.Results) != wantCount {
				t.Fatal("target selection changed", rec.Body.String())
			}
			if wantCount > 0 {
				row := response.Results[0]
				wantOK := name != "native-error" && name != "missing-model" && name != "no-session" && name != "provider-error"
				if row["ok"] != wantOK {
					t.Fatal("probe outcome changed", rec.Body.String())
				}
				_, nativeHints := row["expires_at"]
				if nativeHints != (name != "no-session" && name != "provider-ok" && name != "provider-error") {
					t.Fatal("session hints changed", row)
				}
				if nativeHints && row["access_key"] != "a***" {
					t.Fatal("native masking changed", row)
				}
			}
			if adapter.calls > 0 {
				_, global := s.Reg.ResolveRaw("workbuddy/global/preserved")
				_, old := s.Reg.ResolveRaw("workbuddy/cn/old-model")
				_, fresh := s.Reg.ResolveRaw("workbuddy/cn/new-model")
				if !global || old != adapter.failure || fresh == adapter.failure || adapter.calls != 1 || calls != 0 {
					t.Fatal("provider result damaged catalog partition")
				}
			} else if wantCount == 0 || name == "missing-model" || name == "no-session" {
				if calls != 0 {
					t.Fatal("unexpected native request", calls)
				}
			} else if calls != 1 {
				t.Fatal("native call count changed", calls)
			}
			// Compare the persisted publication after final Save, excluding timestamps.
			reopened, err := storage.Open(s.Store.Path())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(reopened.ModelPolicies(), s.Store.ModelPolicies()) {
				t.Fatal("unrelated policy changed")
			}
			if name != "no-session" {
				fmt.Printf("M321_CONTRACT %s %s\n", name, rec.Body.String())
			}
		})
	}
}

func TestModelRecommendationWireContract(t *testing.T) {
	s := newAdminTestServer(t)
	s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "first", Context: 131072, MaxOutput: 8192}, {ID: "second", Context: 32768, MaxOutput: 4096}})
	for _, model := range []string{"deepseek-v4.1-flash", "workbuddy/cn/first", "workbuddy/cn/second", "missing"} {
		rec := httptest.NewRecorder()
		s.handleClientProfile(rec, httptest.NewRequest(http.MethodGet, "/v1/client-profile?model="+model, nil))
		if model == "missing" {
			if rec.Code != 404 {
				t.Fatal(rec.Code, rec.Body.String())
			}
		} else if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(rec.Code, rec.Body.String())
		}
		fmt.Printf("M321_CONTRACT profile-%s %s\n", model, rec.Body.String())
	}
	rec := adminProbeRequest(t, s, http.MethodGet, "/admin/api/model-policies", "")
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	fmt.Printf("M321_CONTRACT all-policies %s\n", rec.Body.String())
}

func TestMappedRecommendationWireContract(t *testing.T) {
	s, key, _ := mappedFixture(t, nil)
	p := storage.DefaultModelPolicies()["deepseek-v4.1-flash"]
	if err := s.Store.SetModelPolicyAndSave("test-model", p); err != nil {
		t.Fatal(err)
	}
	for _, narrow := range []bool{false, true} {
		if narrow {
			p.ContextWindow, p.MaxOutputTokens, p.ReserveTokens, p.TriggerTokens, p.KeepRecentTokens, p.SummaryMaxTokens = 32768, 4096, 20000, 12000, 4000, 4096
			p.SummaryEnabled, p.SummaryRetry = false, false
			if err := s.Store.SetModelPolicyAndSave("workbuddy/cn/shared", p); err != nil {
				t.Fatal(err)
			}
		}
		req := httptest.NewRequest(http.MethodGet, "/v1/client-profile?model="+mappedPublic, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		var response struct {
			Context int `json:"context_window"`
			Output  int `json:"max_output_tokens"`
			Summary struct {
				Enabled bool `json:"enabled"`
				Retry   bool `json:"bounded_retry"`
			}
		}
		if json.Unmarshal(rec.Body.Bytes(), &response) != nil || rec.Code != 200 || response.Summary.Enabled || response.Summary.Retry {
			t.Fatal("mapped profile failed", rec.Code, rec.Body.String())
		}
		wantContext, wantOutput := 65536, 8192
		if narrow {
			wantContext, wantOutput = 32768, 4096
		}
		if response.Context != wantContext || response.Output != wantOutput {
			t.Fatal("mapped recommendations were not conservative", rec.Body.String())
		}
		fmt.Printf("M321_CONTRACT mapped-profile-%v %s\n", narrow, rec.Body.String())
	}
}
