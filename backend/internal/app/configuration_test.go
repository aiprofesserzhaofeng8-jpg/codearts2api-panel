package app_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
)

func TestConfigurationValidationAndIndependentPartialUpdates(t *testing.T) {
	srv, st, _ := managementFixture(t)
	before := st.Settings()
	name, clear := "must not publish", true
	for _, patch := range []config.SettingsUpdate{
		{DefaultModel: &name, WorkBuddy: json.RawMessage(`{"prompt_text":"pending"}`), Pool: json.RawMessage(`{"max_in_flight":0}`)},
		{DefaultModel: &name, WorkBuddy: json.RawMessage(`null`)},
		{DefaultModel: &name, ClearPassword: &clear},
	} {
		if err := srv.Configuration.UpdateSettings(patch); err == nil || !reflect.DeepEqual(before, st.Settings()) {
			t.Fatal("invalid configuration partially changed settings", err)
		}
	}
	if err := srv.Configuration.UpdateSettings(config.SettingsUpdate{WorkBuddy: json.RawMessage(`{"prompt_text":"only WorkBuddy","sanitize":false}`)}); err != nil {
		t.Fatal(err)
	}
	got := st.Settings()
	if got.WorkBuddy.PromptText != "only WorkBuddy" || got.WorkBuddy.Sanitize || got.WorkBuddy.SearchEnabled != before.WorkBuddy.SearchEnabled || got.CodeArts != before.CodeArts || got.Trae != before.Trae || got.Pool != before.Pool {
		t.Fatal("partial settings update leaked across platforms or reset omitted fields")
	}
}

func TestConfigurationRetainsLegacySettingsAndStrictPolicySaveFailures(t *testing.T) {
	srv, st, _ := managementFixture(t)
	before, policies := st.Settings(), st.ModelPolicies()
	logged, retries := 0, 7
	manager := config.NewManagement(st, nil, func(string, ...any) { logged++ })
	restore := blockManagementWrites(t, st)
	if err := manager.UpdateSettings(config.SettingsUpdate{MaxRetries: &retries}); err != nil || logged != 1 || st.Settings().MaxRetries != retries {
		t.Fatal("legacy configuration Save failure no longer retains memory changes", err)
	}
	policy := storage.DefaultModelPolicies()["deepseek-v4.1-flash"]
	policy.SummaryRetry = false
	if err := srv.Configuration.SaveModelPolicy("deepseek-v4.1-flash", policy); !errors.Is(err, config.ErrModelPolicySave) || !reflect.DeepEqual(policies, st.ModelPolicies()) {
		t.Fatal("model policy Save failure changed the published snapshot", err)
	}
	restore()
	reopened, err := storage.Open(st.Path())
	if err != nil || reopened.Settings().MaxRetries != before.MaxRetries || !reflect.DeepEqual(policies, reopened.ModelPolicies()) {
		t.Fatal("failed configuration writes changed durable state", err)
	}
	if err = st.Save(); err != nil {
		t.Fatal(err)
	}
	reopened, err = storage.Open(st.Path())
	if err != nil || reopened.Settings().MaxRetries != retries {
		t.Fatal("legacy settings could not be persisted after recovery", err)
	}
}

func TestConfigurationPolicyUsesCurrentAliasesAndEnforcesProviderScope(t *testing.T) {
	srv, st, _ := managementFixture(t)
	srv.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "first"}, {ID: "second"}})
	srv.Reg.ReplaceProvider(provider.WorkBuddy, "global", []provider.Model{{ID: "second"}})
	srv.Reg.ReplaceProvider(provider.Trae, "cn", []provider.Model{{ID: "second"}})
	st.UpdateSettings(func(s *storage.Settings) { s.ModelAliases["current-target"] = "workbuddy/cn/first" })
	policy := storage.DefaultModelPolicies()["deepseek-v4.1-flash"]
	policy.SummaryEnabled, policy.SummaryFallbackModel = false, " current-target "
	if err := srv.Configuration.SaveModelPolicy("workbuddy/cn/second", policy); err != nil {
		t.Fatal(err)
	}
	got, _ := st.ModelPolicy("workbuddy/cn/second")
	if got.SummaryFallbackModel != "workbuddy/cn/first" || got.Source != "管理员配置，非上游能力声明" {
		t.Fatal("model policy was not canonicalized")
	}
	before := st.ModelPolicies()
	for _, fallback := range []string{"workbuddy/cn/second", "workbuddy/global/second", "trae/cn/second", "missing"} {
		policy.SummaryFallbackModel = fallback
		if err := srv.Configuration.SaveModelPolicy("workbuddy/cn/second", policy); err == nil || !strings.Contains(err.Error(), "相同上游和地区") || !reflect.DeepEqual(before, st.ModelPolicies()) {
			t.Fatal("invalid fallback crossed model/provider/scope boundary", fallback, err)
		}
	}
	st.UpdateSettings(func(s *storage.Settings) { s.ModelAliases["current-target"] = "workbuddy/cn/second" })
	policy.SummaryFallbackModel = "current-target"
	if err := srv.Configuration.SaveModelPolicy("workbuddy/cn/second", policy); err == nil {
		t.Fatal("model policy manager cached an old alias")
	}
	srv.Reg.ReplaceProvider(provider.WorkBuddy, "cn", nil)
	policy.SummaryFallbackModel = ""
	if err := srv.Configuration.SaveModelPolicy("workbuddy/cn/second", policy); err == nil || !strings.Contains(err.Error(), "当前目录") {
		t.Fatal("model policy manager cached a removed catalog model", err)
	}
}
