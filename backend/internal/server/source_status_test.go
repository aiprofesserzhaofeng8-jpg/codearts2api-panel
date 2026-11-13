package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

func TestSourceDetailSeparatesQuotaHealthAndCapabilityWithoutUpstream(t *testing.T) {
	s, key := unifiedAPIFixture(t)
	s.Reg.ReplaceProvider(provider.Qoder, "cn", []provider.Model{{ID: "fast", Name: "fast", ImageInput: "supported", ToolCalling: "supported"}})
	_, err := s.Store.UpsertAccountAndSave(&accounts.Account{ID: "status-fixture", ExternalID: "fixture", Provider: provider.Qoder, Scope: "cn", Enabled: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	model, err := s.Store.SaveUnifiedModel(config.UnifiedModel{Name: "status-v1", Enabled: true, Sources: []config.ModelSource{{ID: "qoder/cn/fast", Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Credits = &accounts.Credits{}
	s.Credits.Store("status-fixture", &accounts.CreditInfo{Available: true, Kind: "subscription_windows", Windows: []accounts.QuotaWindow{{ID: "week", Name: "7 天窗口", Utilization: ptrFloat(100)}}}, nil)
	s.Store.MarkModelResult("status-fixture", "qoder/cn/fast", false, "rate limited", 60)
	w := adminProbeRequest(t, s, "GET", "/admin/api/unified-models/"+model.ID, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	text := w.Body.String()
	for _, want := range []string{`"catalog_status":"present"`, `"image_input":"supported"`, `"cooling":1`, `"last_probe":null`, `"utilization":100`, `"stale":false`} {
		if !strings.Contains(text, want) {
			t.Fatal("missing independent status", want, text)
		}
	}
	if strings.Contains(text, `"resets_at"`) {
		t.Fatal("invented quota reset")
	}
	s.Credits.Store("status-fixture", nil, errors.New("private diagnostic token"))
	w = adminProbeRequest(t, s, "GET", "/admin/api/unified-models/"+model.ID, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "private diagnostic") {
		t.Fatal("quota error leaked", w.Body.String())
	}
	if len(s.Store.Logs(10)) != 0 {
		t.Fatal("reading status generated inference")
	}
	if nativeProfileGET(s, key, "/v1/models").Code != 200 {
		t.Fatal("health removed model visibility")
	}
}
func ptrFloat(n float64) *float64 { return &n }

func TestRetryAfterAndErrorClassesRemainIndependent(t *testing.T) {
	for _, status := range []int{400, 401, 403, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			s, _, _ := nativeWBServer(t, func(http.ResponseWriter, *http.Request) {})
			addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
			a := s.Store.Account("wb-cn")
			at := time.Now().Add(120 * time.Second).Unix()
			s.providerAccountError(context.Background(), a, "workbuddy/cn/shared", &provider.Error{Status: status, AccountFault: status == 401, RetryAt: at, Message: "safe fixture"})
			current := s.Store.Account(a.ID)
			health := s.Store.ModelHealthSnapshot()[a.ID]["workbuddy/cn/shared"]
			if status == 401 {
				if current.CooldownUntil == 0 || current.Failures != 1 {
					t.Fatal(current)
				}
			} else if current.CooldownUntil != 0 || current.Failures != 0 {
				t.Fatal("request/model error penalized account")
			}
			if status == 429 || status == 503 {
				if health.CooldownUntil < at-1 || health.CooldownUntil > at+1 {
					t.Fatal("retry-after not honored", health)
				}
			}
			if status == 400 || status == 403 {
				if health.CooldownUntil != 0 {
					t.Fatal("request/permission error cooled model")
				}
			}
		})
	}
}

func TestKiroFailedStreamThenHealthyRequestNeedsNoRestart(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "blocking", true: "streaming"}[stream], func(t *testing.T) {
			calls := 0
			s, key, c := nativeKiroServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
				if calls == 1 {
					w.Write(kiroFrame("assistantResponseEvent", `{"content":""}`))
					w.Write([]byte{1, 2, 3})
					return
				}
				w.Write(kiroFrame("assistantResponseEvent", `{"content":"RECOVERED"}`))
			})
			addKiro(t, s, c, "us-east-1", false)
			body, _ := json.Marshal(map[string]any{"model": "kiro/us-east-1/kmodel", "messages": []map[string]string{{"role": "user", "content": "hello"}}, "stream": stream})
			first := responseRequest(s, key, "/v1/chat/completions", string(body))
			if first.Code == 200 {
				t.Fatal("empty stream reported success")
			}
			a := s.Store.Account("kiro-us-east-1")
			if a.Failures != 0 || s.Store.PoolSnapshot()[a.ID].InFlight != 0 {
				t.Fatal("failed stream leaked lease or damaged credentials")
			}
			if s.Store.ModelHealthSnapshot()[a.ID]["kiro/us-east-1/kmodel"].CooldownUntil != 0 {
				t.Fatal("transport fault cooled model")
			}

			second := responseRequest(s, key, "/v1/chat/completions", string(body))
			if second.Code != 200 || !strings.Contains(second.Body.String(), "RECOVERED") {
				t.Fatal(second.Code, second.Body.String())
			}
		})
	}
}

func TestChatGPT429CarriesRetryTimeIntoModelHealth(t *testing.T) {
	calls := 0
	s, key, g, _ := subscriptionServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(429)
	})
	addSubscription(t, s, g, true, time.Now())
	before := time.Now().Unix()
	w := responseRequest(s, key, "/v1/chat/completions", `{"model":"chatgpt/global/actual-model","messages":[{"role":"user","content":"hello"}]}`)
	if w.Code != 429 || calls != 1 {
		t.Fatal(w.Code, calls, w.Body.String())
	}
	health := s.Store.ModelHealthSnapshot()["chatgpt-fixture"]["chatgpt/global/actual-model"]
	if health.CooldownUntil < before+119 || health.CooldownUntil > time.Now().Unix()+120 {
		t.Fatal("Retry-After lost across HTTP and admission", health)
	}
	if a := s.Store.Account("chatgpt-fixture"); a.Failures != 0 || a.CooldownUntil != 0 || s.Store.PoolSnapshot()[a.ID].InFlight != 0 {
		t.Fatal("model rate limit penalized shared credentials or leaked lease")
	}
	// Subsequent calls honor cooldown before contacting the upstream.
	responseRequest(s, key, "/v1/chat/completions", `{"model":"chatgpt/global/actual-model","messages":[{"role":"user","content":"hello"}]}`)
	if calls != 1 {
		t.Fatal("upstream retried before Retry-After")
	}
}
