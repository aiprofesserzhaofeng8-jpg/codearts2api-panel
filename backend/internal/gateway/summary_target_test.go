package gateway

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"codearts2api/internal/requestpolicy"
)

type summaryTargets struct {
	events     []string
	forced     string
	target     SummaryTarget
	found      bool
	cooling    bool
	options    requestpolicy.SummaryOptions
	configured bool
	denied     error
	context    context.Context
}

func (s *summaryTargets) ForcedModel() string {
	s.events = append(s.events, "force")
	return s.forced
}
func (s *summaryTargets) Resolve(name string) (SummaryTarget, bool) {
	s.events = append(s.events, "resolve:"+name)
	return s.target, s.found
}
func (s *summaryTargets) Cooling(model string) bool {
	s.events = append(s.events, "cooling:"+model)
	return s.cooling
}
func (s *summaryTargets) Options(model string) (requestpolicy.SummaryOptions, bool) {
	s.events = append(s.events, "options:"+model)
	return s.options, s.configured
}
func (s *summaryTargets) Authorize(ctx context.Context, model string) error {
	s.context = ctx
	s.events = append(s.events, "authorize:"+model)
	return s.denied
}

func TestSummaryReplacementStopsBeforePreparationAndAccounting(t *testing.T) {
	base := SummaryTarget{ID: "primary", Provider: "platform", Scope: "cn"}
	denied := errors.New("current group/budget denied")
	for _, tc := range []struct {
		name, message string
		change        func(*summaryTargets)
		events        []string
	}{
		{"forced", "强制模型", func(s *summaryTargets) { s.forced = "other" }, []string{"force"}},
		{"missing", "模型目录", func(s *summaryTargets) { s.found = false }, []string{"force", "resolve:backup"}},
		{"platform", "模型目录", func(s *summaryTargets) { s.target.Provider = "other" }, []string{"force", "resolve:backup"}},
		{"region", "模型目录", func(s *summaryTargets) { s.target.Scope = "intl" }, []string{"force", "resolve:backup"}},
		{"cooldown", "正在冷却", func(s *summaryTargets) { s.cooling = true }, []string{"force", "resolve:backup", "cooling:backup"}},
		{"authorization", denied.Error(), func(s *summaryTargets) { s.denied = denied }, []string{"force", "resolve:backup", "cooling:backup", "options:backup", "authorize:backup"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &summaryTargets{target: SummaryTarget{"backup", "platform", "cn"}, found: true}
			tc.change(source)
			started, calls := 0, 0
			_, err := RunSummary(t.Context(), fixtureSession{stream: func(context.Context, string, []byte, func([]byte) error) error {
				calls++
				return nil
			}}, false, SummaryHooks{Prepare: func(bool) (SummaryAttempt, error) {
				// Invalid JSON proves authorization happens before body preparation.
				return PrepareSummaryAttempt(t.Context(), base, []byte(`{`), requestpolicy.SummaryOptions{MaxTokens: 512}, "backup", true, source)
			}, Start: func(int, string) { started++ }})
			if err == nil || !strings.Contains(err.Error(), tc.message) || calls != 0 || started != 0 || !reflect.DeepEqual(source.events, tc.events) {
				t.Fatalf("replacement crossed a policy boundary: err=%v events=%v starts=%d calls=%d", err, source.events, started, calls)
			}
			if tc.name == "authorization" && !errors.Is(err, denied) {
				t.Fatal("authorization error identity lost")
			}
		})
	}
}

func TestSummaryRetryUsesCurrentOptionsAndOriginalContext(t *testing.T) {
	base := SummaryTarget{"primary", "platform", "cn"}
	body := []byte(`{"thinking":{"type":"enabled"},"reasoning_effort":"high","messages":[{"role":"user","content":"history"}]}`)
	initial := requestpolicy.SummaryOptions{MaxTokens: 512, DisableThinking: true}
	for _, tc := range []struct {
		name, fallback string
		retry          bool
		configured     bool
		want           string
		thinking       bool
	}{
		{"initial", "backup", false, true, `"max_tokens":512`, true},
		{"same", "", true, true, `"max_tokens":512`, true},
		{"unverified", "backup", true, false, `"max_tokens":512`, false},
		{"configured", "backup", true, true, `"max_tokens":256`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &summaryTargets{target: SummaryTarget{"backup", "platform", "cn"}, found: true, configured: tc.configured, options: requestpolicy.SummaryOptions{MaxTokens: 256, DisableThinking: true}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			got, err := PrepareSummaryAttempt(ctx, base, body, initial, tc.fallback, tc.retry, source)
			if err != nil || !strings.Contains(string(got.Body), tc.want) || strings.Contains(string(got.Body), `"thinking"`) != tc.thinking {
				t.Fatalf("wrong target policy: %v %s", err, got.Body)
			}
			if tc.retry && source.context != ctx || !tc.retry && len(source.events) != 0 {
				t.Fatal("initial admission changed or retry lost original context")
			}
			if tc.retry && tc.fallback == "" && !reflect.DeepEqual(source.events, []string{"authorize:primary"}) {
				t.Fatal("same-model retry skipped authorization or re-resolved original policy", source.events)
			}
		})
	}
}
