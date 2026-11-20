package bedrock

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/provider"
)

func frame(eventType, payload string) []byte {
	name := ":event-type"
	headers := append([]byte{byte(len(name))}, name...)
	headers = append(headers, 7, 0, byte(len(eventType)))
	headers = append(headers, eventType...)
	out := make([]byte, 12)
	binary.BigEndian.PutUint32(out, uint32(16+len(headers)+len(payload)))
	binary.BigEndian.PutUint32(out[4:], uint32(len(headers)))
	binary.BigEndian.PutUint32(out[8:], crc32.ChecksumIEEE(out[:8]))
	out = append(out, headers...)
	out = append(out, payload...)
	sum := crc32.ChecksumIEEE(out)
	out = binary.BigEndian.AppendUint32(out, sum)
	return out
}
func chunk(event string) []byte {
	b, _ := json.Marshal(map[string][]byte{"bytes": []byte(event)})
	return frame("chunk", string(b))
}
func TestEventStreamIntegrityAndBounds(t *testing.T) {
	valid := frame("chunk", `{"bytes":"e30="}`)
	for _, where := range []int{8, len(valid) - 1} {
		b := append([]byte(nil), valid...)
		b[where] ^= 1
		if _, err := newBedrockEventStreamDecoder(bytes.NewReader(b)).Decode(); err == nil {
			t.Fatal("bad CRC accepted")
		}
	}
	for _, total := range []uint32{0, 15, 32 << 20, 0xffffffff} {
		b := make([]byte, 12)
		binary.BigEndian.PutUint32(b, total)
		binary.BigEndian.PutUint32(b[8:], crc32.ChecksumIEEE(b[:8]))
		if _, err := newBedrockEventStreamDecoder(bytes.NewReader(b)).Decode(); err == nil {
			t.Fatal("bad length accepted")
		}
	}
	for i := 1; i < len(valid); i++ {
		if _, err := newBedrockEventStreamDecoder(bytes.NewReader(valid[:i])).Decode(); err == nil {
			t.Fatal("truncated frame accepted", i)
		}
	}
	b, err := newBedrockEventStreamDecoder(bytes.NewReader(valid)).Decode()
	if err != nil || string(b) != `{"bytes":"e30="}` {
		t.Fatal(string(b), err)
	}
}
func TestBedrockWireTextAndUsage(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"id":"fixture","type":"message","role":"assistant","content":[],"usage":{"input_tokens":12,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"{\n\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"OK\"}}",
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"amazon-bedrock-invocationMetrics":{"inputTokenCount":12,"outputTokenCount":3}}`,
		`{"type":"message_stop"}`,
	}
	var body []byte
	for _, e := range events {
		body = append(body, chunk(e)...)
	}
	var got []byte
	err := Decode(t.Context(), bytes.NewReader(body), "anthropic.claude-fixture", func(b []byte) error { got = append(got, b...); return nil })
	if err != nil || !bytes.Contains(got, []byte("OK")) || !bytes.Contains(got, []byte(`"completion_tokens":3`)) {
		t.Fatal(string(got), err)
	}
	if err := Decode(t.Context(), bytes.NewReader(body[:len(body)-len(chunk(events[5]))]), "fixture", func([]byte) error { return nil }); err == nil {
		t.Fatal("missing stop accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Decode(ctx, bytes.NewReader(body), "fixture", func([]byte) error { return nil }); err == nil {
		t.Fatal("cancel ignored")
	}
	for _, payload := range []string{`{}`, `{"bytes":"broken-base64"}`, `{"bytes":"bnVsbA=="}`} {
		if _, err := io.ReadAll(&eventReader{ctx: t.Context(), decoder: newBedrockEventStreamDecoder(bytes.NewReader(frame("chunk", payload)))}); err == nil {
			t.Fatal("bad envelope", payload)
		}
	}
}
func TestNativeEncodeAndURL(t *testing.T) {
	b, err := Encode(provider.Request{Model: "upstream", Body: []byte(`{"messages":[{"role":"user","content":"hi"}],"max_tokens":64,"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]json.RawMessage
	json.Unmarshal(b, &obj)
	if obj["model"] != nil || obj["stream"] != nil || string(obj["anthropic_version"]) != `"bedrock-2023-05-31"` || !bytes.Contains(b, []byte("lookup")) {
		t.Fatal(string(b))
	}
	url, err := InvokeURL("us-east-1", "us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	if err != nil || !strings.Contains(url, "v1%3A0/invoke-with-response-stream") {
		t.Fatal(url, err)
	}
	for _, region := range []string{"evil.example/path", "us-gov-west-1", "cn-north-1", ""} {
		if _, err := InvokeURL(region, "model"); err == nil {
			t.Fatal("unsupported region", region)
		}
	}
}
func TestSignerIncludesPayloadAndSessionToken(t *testing.T) {
	url, _ := InvokeURL("us-east-1", "anthropic.claude-fixture-v1:0")
	signer := NewBedrockSigner("AKIDEXAMPLE", "fixture-secret", "fixture-session", "us-east-1")
	makeReq := func(body string) *http.Request {
		r, _ := http.NewRequest("POST", url, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/vnd.amazon.eventstream")
		return r
	}
	r := makeReq(`{}`)
	if err := signer.SignRequest(t.Context(), r, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	auth := r.Header.Get("Authorization")
	if !strings.Contains(auth, "/us-east-1/bedrock/aws4_request") || !strings.Contains(auth, "x-amz-security-token") || r.Header.Get("X-Amz-Security-Token") != "fixture-session" {
		t.Fatal("missing signed credentials")
	}
	at, err := time.Parse("20060102T150405Z", r.Header.Get("X-Amz-Date"))
	if err != nil || time.Since(at) > time.Minute {
		t.Fatal("invalid signed time")
	}
	r2 := makeReq(`{"x":1}`)
	if err := signer.SignRequest(t.Context(), r2, []byte(`{"x":1}`)); err != nil {
		t.Fatal(err)
	}
	if r2.Header.Get("Authorization") == auth {
		t.Fatal("payload not signed")
	}
}

func TestBedrockToolsAndImageEncoding(t *testing.T) {
	body := `{"messages":[{"role":"user","content":[{"type":"text","text":"inspect"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}]},{"role":"assistant","tool_calls":[{"id":"tool_1","type":"function","function":{"name":"lookup","arguments":"{\"id\":9007199254740993}"}}]},{"role":"tool","tool_call_id":"tool_1","content":"found"}]}`
	b, err := Encode(provider.Request{Model: "fixture", Body: []byte(body)})
	if err != nil || !bytes.Contains(b, []byte(`9007199254740993`)) || !bytes.Contains(b, []byte(`"type":"base64"`)) || !bytes.Contains(b, []byte(`"tool_use_id":"tool_1"`)) {
		t.Fatal(string(b), err)
	}
	events := []string{
		`{"type":"message_start","message":{"id":"fixture","role":"assistant","usage":{"input_tokens":12,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_1","name":"lookup","input":{}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"id\":9007199254740993}"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}`,
		`{"type":"message_stop"}`,
	}
	var stream, got []byte
	for _, e := range events {
		stream = append(stream, chunk(e)...)
	}
	err = Decode(t.Context(), bytes.NewReader(stream), "fixture", func(b []byte) error { got = append(got, b...); return nil })
	if err != nil || !bytes.Contains(got, []byte("9007199254740993")) || !bytes.Contains(got, []byte("tool_calls")) {
		t.Fatal(string(got), err)
	}
}

func TestMetricsDoNotOverwriteOrInventUsage(t *testing.T) {
	b, err := invocationMetrics([]byte(`{"type":"message_delta","usage":{"output_tokens":7},"amazon-bedrock-invocationMetrics":{"outputTokenCount":8}}`))
	if err != nil || !bytes.Contains(b, []byte(`"output_tokens":7`)) || bytes.Contains(b, []byte("input_tokens")) {
		t.Fatal(string(b), err)
	}
	for _, raw := range []string{`{"amazon-bedrock-invocationMetrics":{"outputTokenCount":-1}}`, `{"amazon-bedrock-invocationMetrics":{"outputTokenCount":1.5}}`} {
		if _, err := invocationMetrics([]byte(raw)); err == nil {
			t.Fatal("invalid metrics accepted")
		}
	}
}

func TestExceptionDoesNotExposePayload(t *testing.T) {
	name := ":message-type"
	value := "exception"
	headers := append([]byte{byte(len(name))}, name...)
	headers = append(headers, 7, 0, byte(len(value)))
	headers = append(headers, value...)
	payload := []byte(`{"message":"secret-token-and-prompt"}`)
	b := make([]byte, 12)
	binary.BigEndian.PutUint32(b, uint32(16+len(headers)+len(payload)))
	binary.BigEndian.PutUint32(b[4:], uint32(len(headers)))
	binary.BigEndian.PutUint32(b[8:], crc32.ChecksumIEEE(b[:8]))
	b = append(b, headers...)
	b = append(b, payload...)
	b = binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
	_, err := newBedrockEventStreamDecoder(bytes.NewReader(b)).Decode()
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("exception disclosure", err)
	}
}
