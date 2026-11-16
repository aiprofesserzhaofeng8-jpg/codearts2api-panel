package bedrock

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/provider"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const testKey = `{"aws_access_key_id":"fixture-id","aws_secret_access_key":"fixture-secret","aws_region":"us-east-1"}`

func testResponse(code int, typ, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {typ}}, Body: io.NopCloser(strings.NewReader(body))}
}
func successFrames() string {
	events := []string{`{"type":"message_start","message":{"id":"x","role":"assistant","usage":{"input_tokens":12,"output_tokens":0}}}`, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"OK"}}`, `{"type":"content_block_stop","index":0}`, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`, `{"type":"message_stop"}`}
	var b []byte
	for _, e := range events {
		b = append(b, chunk(e)...)
	}
	return string(b)
}
func TestCredentialsScopeExpiryAndNoAmbientIdentity(t *testing.T) {
	c := New()
	b, id, err := c.Normalize([]byte(testKey))
	if err != nil || !id.NonExpiring || id.HasRefresh || id.Scope != "us-east-1" {
		t.Fatal(id, err)
	}
	_, id2, err := c.Normalize(b)
	if err != nil || id2 != id {
		t.Fatal(err)
	}
	for _, raw := range []string{`{}`, strings.Replace(testKey, "us-east-1", "evil.invalid", 1), strings.TrimSuffix(testKey, "}") + `,"scope":"us-west-2"}`, strings.TrimSuffix(testKey, "}") + `,"aws_session_token":"temp"}`, strings.TrimSuffix(testKey, "}") + `,"base_url":"https://evil.invalid"}`} {
		if _, _, err = c.Normalize([]byte(raw)); err == nil {
			t.Fatal("invalid credentials accepted")
		}
	}
	raw := []byte(strings.TrimSuffix(testKey, "}") + `,"aws_session_token":"temp","expires_at":1}`)
	_, temp, err := c.Normalize(raw)
	if err != nil || temp.NonExpiring || temp.HasRefresh {
		t.Fatal(temp, err)
	}
	c.HTTP.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("expired credentials sent"); return nil, nil })
	if _, err = c.Models(t.Context(), raw); err == nil {
		t.Fatal("expiry ignored")
	}
	if _, err = c.Refresh(t.Context(), raw); err == nil {
		t.Fatal("invented refresh")
	}
}
func TestCatalogSigningAndInferenceVerification(t *testing.T) {
	c := New()
	calls := 0
	denied := false
	c.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if !strings.Contains(r.Header.Get("Authorization"), "/us-east-1/bedrock/aws4_request") {
			t.Error("unsigned request")
		}
		switch r.URL.Path {
		case "/foundation-models":
			return testResponse(200, "application/json", `{"modelSummaries":[{"modelId":"anthropic.claude-fixture-v1:0","modelName":"Claude","responseStreamingSupported":true,"inputModalities":["TEXT","IMAGE"]},{"modelId":"other-model","responseStreamingSupported":true}]}`), nil
		case "/inference-profiles":
			return testResponse(200, "application/json", `{"inferenceProfileSummaries":[{"inferenceProfileId":"us.anthropic.claude-fixture-v1:0","status":"ACTIVE"}]}`), nil
		case "/model/us.anthropic.claude-fixture-v1:0/invoke-with-response-stream":
			if r.URL.Host != "bedrock-runtime.us-east-1.amazonaws.com" || !strings.Contains(r.URL.RawPath, "%3A") {
				t.Error("wrong invocation URL")
			}
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), `"stream"`) || !strings.Contains(string(body), "bedrock-2023-05-31") {
				t.Error("wrong body")
			}
			if denied {
				return testResponse(403, "application/json", `{"__type":"AccessDeniedException","message":"secret"}`), nil
			}
			return testResponse(200, "application/vnd.amazon.eventstream", successFrames()), nil
		default:
			t.Fatal(r.URL)
			return nil, nil
		}
	})
	models, err := c.Models(t.Context(), []byte(testKey))
	if err != nil || len(models) != 2 || models[0].ImageInput != "supported" {
		t.Fatal(models, err)
	}
	before := calls
	if _, err = c.Verify(t.Context(), []byte(testKey)); err != nil || calls != before+3 {
		t.Fatal("verification did not infer", calls, err)
	}
	denied = true
	if _, err = c.Verify(t.Context(), []byte(testKey)); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("false success or disclosure", err)
	}
}
func TestExplicitModelsAvoidListPermissionsAndCancellation(t *testing.T) {
	c := New()
	raw := []byte(strings.TrimSuffix(testKey, "}") + `,"model_ids":["anthropic.claude-fixture"]}`)
	calls := 0
	c.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" {
			t.Fatal("unneeded catalog permission")
		}
		return testResponse(200, "application/vnd.amazon.eventstream", successFrames()), nil
	})
	if _, err := c.Verify(t.Context(), raw); err != nil || calls != 1 {
		t.Fatal(calls, err)
	}
	c.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	err := c.Stream(ctx, raw, provider.Request{Model: "anthropic.claude-fixture", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil })
	if err == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestCatalogPaginationLoopAndCredentialError(t *testing.T) {
	c := New()
	calls := 0
	c.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path == "/foundation-models" {
			return testResponse(200, "application/json", `{"modelSummaries":[]}`), nil
		}
		return testResponse(200, "application/json", `{"inferenceProfileSummaries":[],"nextToken":"loop"}`), nil
	})
	if _, err := c.Models(t.Context(), []byte(testKey)); err == nil || calls != 3 {
		t.Fatal("pagination loop", calls, err)
	}
	err := bedrockHTTPError(testResponse(403, "application/json", `{"__type":"ExpiredTokenException","message":"private"}`))
	var pe *provider.Error
	if !errors.As(err, &pe) || pe.Status != 401 || !pe.AccountFault || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}
}

func TestLegacyClaudeExcludedAndStreamErrors(t *testing.T) {
	for _, id := range []string{"anthropic.claude-v2", "anthropic.claude-instant-v1", "amazon.nova-pro"} {
		if supportedModel(id) {
			t.Fatal("unsupported wire model", id)
		}
	}
	for code, status := range map[string]int{"throttlingException": 429, "validationException": 400, "modelTimeoutException": 504, "internalServerException": 502} {
		var pe *provider.Error
		if !errors.As(streamException(code), &pe) || pe.Status != status || pe.AccountFault {
			t.Fatal(code, pe)
		}
	}
}
