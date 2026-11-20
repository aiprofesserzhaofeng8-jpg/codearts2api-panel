package server

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/config"
	"codearts2api/internal/protocol"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"codearts2api/internal/upstream/cline"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClineProtocolAccounting(t *testing.T) {
	for _, scope := range []string{"global"} {
		t.Run(scope, func(t *testing.T) {
			for _, public := range []bool{false, true} {
				for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
					for _, stream := range []bool{false, true} {
						t.Run(fmt.Sprintf("public=%v/%s/stream=%v", public, path, stream), func(t *testing.T) {
							calls := 0
							up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
								calls++
								var body map[string]json.RawMessage
								_ = json.NewDecoder(r.Body).Decode(&body)
								var options map[string]json.RawMessage
								_ = json.Unmarshal(body["stream_options"], &options)
								if r.URL.Path != "/api/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture-key" || string(body["model"]) != `"deepseek/deepseek-v4-flash"` || string(options["include_usage"]) != "true" {
									t.Error("wrong upstream route or missing usage request")
								}
								w.Header().Set("Content-Type", "text/event-stream")
								io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"API_FIXTURE_OK\"},\"finish_reason\":\"stop\"}]}\n\n")
								if string(options["include_usage"]) == "true" {
									io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":4,\"total_tokens\":16,\"prompt_tokens_details\":{\"cached_tokens\":3}}}\n\n")
								}
								io.WriteString(w, "data: [DONE]\n\n")
							}))
							defer up.Close()
							s := newAdminTestServer(t)
							c := cline.New()
							c.Origin = up.URL
							s.Providers = provider.NewRegistry()
							if err := s.Providers.Register(c); err != nil {
								t.Fatal(err)
							}
							raw := json.RawMessage(`{"api_key":"fixture-key","scope":"` + scope + `"}`)

							_, identity, err := c.Normalize(raw)
							if err != nil {
								t.Fatal(err)
							}
							s.Store.UpsertAccount(&accounts.Account{ID: "mc", Provider: provider.Cline, Scope: identity.Scope, ExternalID: identity.ID, Credentials: raw, Enabled: true})
							s.Reg.ReplaceProvider(provider.Cline, identity.Scope, []provider.Model{{ID: "deepseek/deepseek-v4-flash", Name: "deepseek/deepseek-v4-flash", Context: 32768, MaxOutput: 4096}})
							model := provider.Cline + "/" + identity.Scope + "/deepseek/deepseek-v4-flash"
							if public {
								_, err := s.Store.SaveUnifiedModel(config.UnifiedModel{Name: "shared-fixture-v1", Enabled: true, Sources: []config.ModelSource{{ID: model, Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision, nil)
								if err != nil {
									t.Fatal(err)
								}
								model = "shared-fixture-v1"
							}
							key := s.Store.AddKey("fixture").Key
							_ = s.Handler()
							markModelsFresh(s)
							body := map[string]any{"model": model, "stream": stream, "max_tokens": 64, "messages": []map[string]string{{"role": "user", "content": "fixture"}}}
							if path == "/v1/responses" {
								delete(body, "messages")
								delete(body, "max_tokens")
								body["input"] = "fixture"
								body["max_output_tokens"] = 64
							}
							w := responseRequest(s, key, path, string(mustJSON(body)))
							if w.Code != 200 || calls != 1 || !strings.Contains(w.Body.String(), "API_FIXTURE_OK") {
								t.Fatal(w.Code, calls, w.Body.String())
							}
							logs := s.Store.Logs(1)
							if len(logs) != 1 || logs[0].Usage.InputTokens != 12 || logs[0].Usage.OutputTokens != 4 || logs[0].Usage.CachedTokens != 3 || logs[0].Provider != provider.Cline || logs[0].Model != provider.Cline+"/"+identity.Scope+"/deepseek/deepseek-v4-flash" || s.Store.PoolSnapshot()["mc"].InFlight != 0 {
								t.Fatal("accounting/source/lease mismatch", logs)
							}
							group, err := s.Store.SaveGroup(storage.AccountGroup{Name: "empty restricted group", AccountIDs: []string{}})
							if err != nil {
								t.Fatal(err)
							}
							if err = s.Store.SetKeyGroupAndSave(key, group.ID); err != nil {
								t.Fatal(err)
							}
							blocked := responseRequest(s, key, path, string(mustJSON(body)))
							if blocked.Code != 403 || calls != 1 {
								t.Fatal("group bypass", blocked.Code, calls)
							}

						})
					}
				}
			}

		})
	}
}

func TestClineQuotaDoesNotCoolOtherWallets(t *testing.T) {
	s := newAdminTestServer(t)
	a := &accounts.Account{ID: "cline-fixture", Provider: provider.Cline, Scope: "global", Enabled: true}
	s.Store.UpsertAccount(a)
	for _, model := range []string{"cline/global/cline-pass/glm-test", "cline/global/deepseek/deepseek-test", "cline/global/cline-free/test"} {
		s.providerAccountError(context.Background(), a, model, &provider.Error{Status: 429, Code: "INFERENCE_CAP_ERROR", Message: "safe fixture"})
		current := s.Store.Account(a.ID)
		if current.CooldownUntil != 0 || current.Failures != 0 {
			t.Fatal("account-wide penalty", current)
		}
		if s.Store.ModelHealthSnapshot()[a.ID][model].CooldownUntil == 0 {
			t.Fatal("model cooldown missing")
		}
	}
}

func TestClineChatOpaqueReasoningToolRoundTrip(t *testing.T) {
	calls := 0
	details := `[{"type":"reasoning.encrypted","id":"opaque-id","index":0,"data":"opaque-secret","vendor_number":9007199254740993}]`
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&request)
		if calls == 2 {
			var messages []map[string]json.RawMessage
			json.Unmarshal(request["messages"], &messages)
			if len(messages) != 3 || string(messages[1]["reasoning_details"]) != details || !strings.Contains(string(messages[1]["tool_calls"]), "call-fixture") || string(messages[2]["tool_call_id"]) != `"call-fixture"` {
				t.Error("opaque history changed", string(request["messages"]))
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"reasoning":"planning","reasoning_details":`+details+`,"tool_calls":[{"index":0,"id":"call-fixture","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":"DONE"},"finish_reason":"stop"}]}`+"\n\n")
		}
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer up.Close()
	s := newAdminTestServer(t)
	c := cline.New()
	c.Origin = up.URL
	s.Providers = provider.NewRegistry()
	s.Providers.Register(c)
	raw := json.RawMessage(`{"api_key":"fixture-key"}`)
	_, identity, err := c.Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	s.Store.UpsertAccount(&accounts.Account{ID: "cline-roundtrip", Provider: provider.Cline, Scope: identity.Scope, ExternalID: identity.ID, Credentials: raw, Enabled: true})
	s.Reg.ReplaceProvider(provider.Cline, "global", []provider.Model{{ID: "anthropic/claude-test"}})
	key := s.Store.AddKey("test").Key
	_ = s.Handler()
	markModelsFresh(s)
	user := map[string]any{"role": "user", "content": "read x"}
	body := map[string]any{"model": "cline/global/anthropic/claude-test", "stream": false, "messages": []any{user}, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "read_file", "parameters": map[string]any{"type": "object"}}}}}
	first := responseRequest(s, key, "/v1/chat/completions", string(mustJSON(body)))
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	var response struct {
		Choices []struct {
			Message json.RawMessage `json:"message"`
		} `json:"choices"`
	}
	json.Unmarshal(first.Body.Bytes(), &response)
	if len(response.Choices) != 1 || !strings.Contains(string(response.Choices[0].Message), "reasoning_details") {
		t.Fatal(first.Body.String())
	}
	body["messages"] = []any{user, response.Choices[0].Message, map[string]any{"role": "tool", "tool_call_id": "call-fixture", "content": "file result"}}
	second := responseRequest(s, key, "/v1/chat/completions", string(mustJSON(body)))
	if second.Code != 200 || calls != 2 || !strings.Contains(second.Body.String(), "DONE") || s.Store.PoolSnapshot()["cline-roundtrip"].InFlight != 0 {
		t.Fatal(second.Code, second.Body.String(), calls)
	}
}

func TestClineResponsesOpaqueReasoningToolRoundTrip(t *testing.T) {
	calls := 0
	details := `[{"type":"reasoning.encrypted","id":"opaque-id","index":0,"data":"opaque-secret","vendor_number":9007199254740993}]`
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&request)
		if calls == 2 {
			var messages []map[string]json.RawMessage
			json.Unmarshal(request["messages"], &messages)
			if len(messages) != 3 || !strings.Contains(string(messages[1]["reasoning_details"]), "opaque-secret") || !strings.Contains(string(messages[1]["reasoning_details"]), "9007199254740993") || !strings.Contains(string(messages[1]["tool_calls"]), "call-fixture") || string(messages[2]["tool_call_id"]) != `"call-fixture"` {
				t.Error("opaque history changed", string(request["messages"]))
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"reasoning":"planning","reasoning_details":`+details+`,"tool_calls":[{"index":0,"id":"call-fixture","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":"DONE"},"finish_reason":"stop"}]}`+"\n\n")
		}
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer up.Close()
	s := newAdminTestServer(t)
	c := cline.New()
	c.Origin = up.URL
	s.Providers = provider.NewRegistry()
	s.Providers.Register(c)
	raw := json.RawMessage(`{"api_key":"fixture-key"}`)
	_, identity, err := c.Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	s.Store.UpsertAccount(&accounts.Account{ID: "cline-roundtrip", Provider: provider.Cline, Scope: identity.Scope, ExternalID: identity.ID, Credentials: raw, Enabled: true})
	s.Reg.ReplaceProvider(provider.Cline, "global", []provider.Model{{ID: "anthropic/claude-test"}})
	key := s.Store.AddKey("test").Key
	_ = s.Handler()
	markModelsFresh(s)
	user := map[string]any{"role": "user", "content": "read x"}
	body := map[string]any{"model": "cline/global/anthropic/claude-test", "stream": false, "messages": []any{user}, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "read_file", "parameters": map[string]any{"type": "object"}}}}}
	delete(body, "messages")
	body["input"] = []any{user}
	body["tools"] = []any{map[string]any{"type": "function", "name": "read_file", "parameters": map[string]any{"type": "object"}}}
	first := responseRequest(s, key, "/v1/responses", string(mustJSON(body)))
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	var response struct {
		Output []json.RawMessage `json:"output"`
	}
	json.Unmarshal(first.Body.Bytes(), &response)
	if len(response.Output) == 0 {
		t.Fatal(first.Body.String())
	}
	input := []any{user}
	for _, item := range response.Output {
		input = append(input, item)
	}
	input = append(input, map[string]any{"type": "function_call_output", "call_id": "call-fixture", "output": "file result"})
	body["input"] = input
	second := responseRequest(s, key, "/v1/responses", string(mustJSON(body)))
	if second.Code != 200 || calls != 2 || !strings.Contains(second.Body.String(), "DONE") || s.Store.PoolSnapshot()["cline-roundtrip"].InFlight != 0 {
		t.Fatal(second.Code, second.Body.String(), calls)
	}
}

func TestClineMessagesOpaqueReasoningToolRoundTrip(t *testing.T) {
	calls := 0
	details := `[{"type":"reasoning.encrypted","id":"opaque-id","index":0,"data":"opaque-secret","vendor_number":9007199254740993}]`
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&request)
		if calls == 2 {
			var messages []map[string]json.RawMessage
			json.Unmarshal(request["messages"], &messages)
			if len(messages) != 3 || !strings.Contains(string(messages[1]["reasoning_details"]), "opaque-secret") || !strings.Contains(string(messages[1]["reasoning_details"]), "9007199254740993") || !strings.Contains(string(messages[1]["tool_calls"]), "call-fixture") || string(messages[2]["tool_call_id"]) != `"call-fixture"` {
				t.Error("opaque history changed", string(request["messages"]))
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"reasoning":"planning","reasoning_details":`+details+`,"tool_calls":[{"index":0,"id":"call-fixture","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":"DONE"},"finish_reason":"stop"}]}`+"\n\n")
		}
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer up.Close()
	s := newAdminTestServer(t)
	c := cline.New()
	c.Origin = up.URL
	s.Providers = provider.NewRegistry()
	s.Providers.Register(c)
	raw := json.RawMessage(`{"api_key":"fixture-key"}`)
	_, identity, err := c.Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	s.Store.UpsertAccount(&accounts.Account{ID: "cline-roundtrip", Provider: provider.Cline, Scope: identity.Scope, ExternalID: identity.ID, Credentials: raw, Enabled: true})
	s.Reg.ReplaceProvider(provider.Cline, "global", []provider.Model{{ID: "anthropic/claude-test"}})
	key := s.Store.AddKey("test").Key
	_ = s.Handler()
	markModelsFresh(s)
	user := map[string]any{"role": "user", "content": "read x"}
	body := map[string]any{"model": "cline/global/anthropic/claude-test", "stream": false, "messages": []any{user}, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "read_file", "parameters": map[string]any{"type": "object"}}}}}
	body["max_tokens"] = 64
	body["tools"] = []any{map[string]any{"name": "read_file", "input_schema": map[string]any{"type": "object"}}}
	first := responseRequest(s, key, "/v1/messages", string(mustJSON(body)))
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	var response struct {
		Content json.RawMessage `json:"content"`
	}
	json.Unmarshal(first.Body.Bytes(), &response)
	if !strings.Contains(string(response.Content), "reasoning_details") {
		t.Fatal(first.Body.String())
	}
	body["messages"] = []any{user, map[string]any{"role": "assistant", "content": response.Content}, map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "call-fixture", "content": "file result"}}}}
	second := responseRequest(s, key, "/v1/messages", string(mustJSON(body)))
	if second.Code != 200 || calls != 2 || !strings.Contains(second.Body.String(), "DONE") || s.Store.PoolSnapshot()["cline-roundtrip"].InFlight != 0 {
		t.Fatal(second.Code, second.Body.String(), calls)
	}
}

func TestClineMessagesStreamOpaqueExtensionReplay(t *testing.T) {
	recorder := httptest.NewRecorder()
	a := newAnthropicStream(recorder, recorder, "msg-test", "test", 1)
	_, err := a.consume([]byte(`{"choices":[{"index":0,"delta":{"reasoning_details":[{"type":"reasoning.encrypted","data":"opaque","number":9007199254740993}],"reasoning_content":"planning"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var blocks []json.RawMessage
	for _, line := range strings.Split(recorder.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event struct {
			Type  string          `json:"type"`
			Block json.RawMessage `json:"content_block"`
		}
		json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event)
		if event.Type == "content_block_start" && strings.Contains(string(event.Block), "reasoning_details") {
			blocks = append(blocks, event.Block)
		}
	}
	if len(blocks) != 1 || strings.Contains(string(blocks[0]), "signature") {
		t.Fatal(recorder.Body.String())
	}
	content, _ := json.Marshal(blocks)
	out, err := protocol.ToOpenAI(&protocol.Request{Model: "test", MaxTokens: 64, Messages: []protocol.Message{{Role: "assistant", Content: content}}}, "test")
	b, _ := json.Marshal(out)
	if err != nil || !strings.Contains(string(b), "9007199254740993") || !strings.Contains(string(b), "reasoning_details") {
		t.Fatal(err, string(b))
	}
}
