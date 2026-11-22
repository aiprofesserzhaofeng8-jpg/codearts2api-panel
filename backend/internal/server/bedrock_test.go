package server

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/bedrock"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"io"
	"net/http"
	"strings"
	"testing"
)

type bedrockRoundTrip func(*http.Request) (*http.Response, error)

func (f bedrockRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func bedrockFixtureStream() string {
	var stream []byte
	events := []string{`{"type":"message_start","message":{"id":"x","role":"assistant","usage":{"input_tokens":12,"output_tokens":0}}}`, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"BEDROCK_OK"}}`, `{"type":"content_block_stop","index":0}`, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`, `{"type":"message_stop"}`}
	for _, e := range events {
		payload, _ := json.Marshal(map[string][]byte{"bytes": []byte(e)})
		headers := append([]byte{11}, []byte(":event-type")...)
		headers = append(headers, 7, 0, 5)
		headers = append(headers, []byte("chunk")...)
		b := make([]byte, 12)
		binary.BigEndian.PutUint32(b, uint32(16+len(headers)+len(payload)))
		binary.BigEndian.PutUint32(b[4:], uint32(len(headers)))
		binary.BigEndian.PutUint32(b[8:], crc32.ChecksumIEEE(b[:8]))
		b = append(b, headers...)
		b = append(b, payload...)
		b = binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
		stream = append(stream, b...)
	}
	return string(stream)
}
func TestBedrockProtocolsSharedMappingAndPermissions(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		for _, stream := range []bool{false, true} {
			t.Run(path+map[bool]string{true: "stream", false: "json"}[stream], func(t *testing.T) {
				s := adminTestServerWithKeys(t, []string{"fixture"})
				c := bedrock.New()
				calls := 0
				c.HTTP.Transport = bedrockRoundTrip(func(r *http.Request) (*http.Response, error) {
					calls++
					if !strings.Contains(r.Header.Get("Authorization"), "/bedrock/aws4_request") || r.URL.Host != "bedrock-runtime.us-east-1.amazonaws.com" {
						t.Error("bad signed destination")
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/vnd.amazon.eventstream"}}, Body: io.NopCloser(strings.NewReader(bedrockFixtureStream()))}, nil
				})
				s.Providers = provider.NewRegistry()
				s.Providers.Register(c)
				raw := json.RawMessage(`{"aws_access_key_id":"fixture","aws_secret_access_key":"secret","aws_region":"us-east-1","model_ids":["anthropic.claude-fixture"]}`)
				_, id, err := c.Normalize(raw)
				if err != nil {
					t.Fatal(err)
				}
				s.Store.UpsertAccount(&accounts.Account{ID: "aws", Provider: provider.Bedrock, Scope: id.Scope, ExternalID: id.ID, Credentials: raw, Enabled: true})
				model := "bedrock/us-east-1/anthropic.claude-fixture"
				s.Reg.ReplaceProvider(provider.Bedrock, id.Scope, []provider.Model{{ID: "anthropic.claude-fixture", Name: "fixture"}})
				if _, err = s.Store.SaveUnifiedModel(config.UnifiedModel{Name: "bedrock-fixture-v1", Enabled: true, Sources: []config.ModelSource{{ID: model, Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision, nil); err != nil {
					t.Fatal(err)
				}
				grantFixtureAccounts(t, s.Store)
				key := s.Store.Keys()[0].Key
				_ = s.Handler()
				markModelsFresh(s)
				w := responseRequest(s, key, path, mappedBody(path, stream, "bedrock-fixture-v1"))
				if w.Code != 200 || calls != 1 || !strings.Contains(w.Body.String(), "BEDROCK_OK") {
					t.Fatal(w.Code, w.Body.String(), calls)
				}
				logs := s.Store.Logs(1)
				if len(logs) != 1 || logs[0].Model != model || logs[0].Usage.InputTokens != 12 || logs[0].Usage.OutputTokens != 3 || s.Store.PoolSnapshot()["aws"].InFlight != 0 {
					t.Fatal(logs)
				}
				g, err := s.Store.SaveGroup(storage.AccountGroup{Name: "empty", AccountIDs: []string{}})
				if err != nil {
					t.Fatal(err)
				}
				if err = s.Store.SetKeyGroupAndSave(key, g.ID); err != nil {
					t.Fatal(err)
				}
				w = responseRequest(s, key, path, mappedBody(path, stream, "bedrock-fixture-v1"))
				if w.Code != 403 || calls != 1 {
					t.Fatal("permission bypass", w.Code, calls)
				}
			})
		}
	}
}
