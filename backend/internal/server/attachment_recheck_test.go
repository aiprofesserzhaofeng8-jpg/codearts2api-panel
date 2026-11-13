package server

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"codearts2api/internal/provider"
)

// Exercise HTTP conversion and platform dispatch, not only the guard.
func TestR1RecheckImagesToolsAndNumbersReachUpstream(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", path, stream), func(t *testing.T) {
				calls := 0
				s, _, _ := mappedFixture(t, func(platform string, w http.ResponseWriter, r *http.Request) {
					calls++
					raw, _ := io.ReadAll(r.Body)
					for _, marker := range []string{"data:image/png;base64,R1IMAGE", "9007199254740993", "lookup", "R1TEXT"} {
						if !strings.Contains(string(raw), marker) {
							t.Errorf("upstream lost %s: %s", marker, raw)
						}
					}
					policySuccess(w, platform, true)
				})
				s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "shared", ImageInput: "supported", ToolCalling: "supported", Context: 65536, MaxOutput: 8192}})
				key, err := s.Store.AddKeyInGroupAndSave("r1-recheck", "")
				if err != nil {
					t.Fatal(err)
				}
				var body string
				switch path {
				case "/v1/chat/completions":
					body = `{"model":"` + mappedPublic + `","messages":[{"role":"user","content":[{"type":"text","text":"R1TEXT"},{"type":"image_url","image_url":{"url":"data:image/png;base64,R1IMAGE"}}]}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"file_id":{"const":9007199254740993}}}}}]}`
				case "/v1/responses":
					body = `{"model":"` + mappedPublic + `","input":[{"role":"user","content":[{"type":"input_text","text":"R1TEXT"},{"type":"input_image","image_url":"data:image/png;base64,R1IMAGE"}]}],"tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{"file_id":{"const":9007199254740993}}}}]}`
				default:
					body = `{"model":"` + mappedPublic + `","max_tokens":128,"messages":[{"role":"user","content":[{"type":"text","text":"R1TEXT"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"R1IMAGE"}}]}],"tools":[{"name":"lookup","input_schema":{"type":"object","properties":{"file_id":{"const":9007199254740993}}}}]}`
				}
				body = strings.TrimSuffix(body, "}") + fmt.Sprintf(`,"stream":%v}`, stream)
				w := responseRequest(s, key.Key, path, body)
				if w.Code != 200 || calls != 1 {
					t.Fatalf("status=%d calls=%d: %s", w.Code, calls, w.Body.String())
				}
				for _, a := range s.Store.Accounts() {
					if a.CooldownUntil != 0 || a.Failures != 0 {
						t.Fatal("request changed account health")
					}
				}
				for _, lease := range s.Store.PoolSnapshot() {
					if lease.InFlight != 0 {
						t.Fatal("lease leaked")
					}
				}
			})
		}
	}
}

func TestR1RecheckFilesInAdditionalContentPositions(t *testing.T) {
	bodies := []string{
		`{"model":"test-model","input":[{"type":"input_file","file_id":"file-x"}]}`,
		`{"model":"test-model","input":{"type":"input_file","file_url":"http://127.0.0.1/private"}}`,
		`{"model":"test-model","system":[{"type":"document","source":{"type":"base64","data":"QQ=="}}],"messages":[{"role":"user","content":"hi"}]}`,
		`{"model":"test-model","input":[{"type":"function_call_output","call_id":"read","output":[{"type":"input_file","file_id":"file-x"}]}]}`,
	}
	calls := 0
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		calls++
		return responseUpstream(r, responsesTextFixture), nil
	})
	for _, body := range bodies {
		for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
			w := responseRequest(s, key, path, body)
			if w.Code != 400 || !strings.Contains(w.Body.String(), "unsupported_file_input") {
				t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
			}
		}
	}
	if calls != 0 {
		t.Fatal("unsupported files reached upstream")
	}
	for _, a := range s.Store.Accounts() {
		if a.CooldownUntil != 0 || a.Failures != 0 {
			t.Fatal("rejected file changed account health")
		}
	}
	for _, lease := range s.Store.PoolSnapshot() {
		if lease.InFlight != 0 {
			t.Fatal("rejected file leaked lease")
		}
	}
}
