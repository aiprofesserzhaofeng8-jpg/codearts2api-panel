package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestUsageSnapshotAndIsolation(t *testing.T) {
	ctx := withUsageCapture(context.Background(), "chat")
	captureUsage(ctx, []byte(`{"usage":null}`))
	if requestUsage(ctx).Known {
		t.Fatal("missing usage must stay unknown")
	}
	raw := []byte(`{"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":70},"completion_tokens_details":{"reasoning_tokens":10}}}`)
	captureUsage(ctx, raw)
	captureUsage(ctx, raw)
	u := requestUsage(ctx)
	if !u.Known || u.InputTokens != 100 || u.OutputTokens != 20 || u.CachedTokens != 70 || u.ReasoningTokens != 10 {
		t.Fatalf("bad snapshot: %+v", u)
	}
	other := withUsageCapture(context.Background(), "responses")
	if requestUsage(other).Known {
		t.Fatal("usage leaked between requests")
	}
	resetUsage(ctx)
	if requestUsage(ctx).InputTokens != 100 {
		t.Fatal("previous attempt usage lost")
	}
	if ctx.Value(usageContextKey{}).(*usageCapture).usage.Known {
		t.Fatal("current attempt snapshot not reset")
	}
	captureUsage(ctx, []byte(`{"usage":{"prompt_tokens":10,"completion_tokens":0,"prompt_cache_hit_tokens":8}}`))
	if requestUsage(ctx).CachedTokens != 78 {
		t.Fatal("cache-hit alias lost")
	}
}

func TestUsageRecordedAcrossProtocols(t *testing.T) {
	for _, protocol := range []string{"chat", "messages", "responses"} {
		for _, stream := range []bool{true, false} {
			name := protocol + "-buffered"
			if stream {
				name = protocol + "-stream"
			}
			t.Run(name, func(t *testing.T) {
				s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
					var req struct {
						Stream  bool `json:"stream"`
						Options struct {
							Include bool `json:"include_usage"`
						} `json:"stream_options"`
					}
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Fatal(err)
					}
					if req.Stream {
						if !req.Options.Include {
							t.Fatal("stream usage was not requested")
						}
						return responseUpstream(r, responsesTextFixture), nil
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":3}}}`)), Request: r}, nil
				})
				path := "/v1/chat/completions"
				body := map[string]any{"model": "test-model", "stream": stream, "messages": []map[string]string{{"role": "user", "content": "hi"}}}
				if protocol == "messages" {
					path = "/v1/messages"
					body["max_tokens"] = 128
				}
				if protocol == "responses" {
					path = "/v1/responses"
					delete(body, "messages")
					body["input"] = "hi"
				}
				raw, _ := json.Marshal(body)
				w := responseRequest(s, key, path, string(raw))
				if w.Code != 200 {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
				stats := s.Store.Stats()
				if stats.UsageRequests != 1 || stats.InputTokens != 12 || stats.OutputTokens != 4 || stats.CachedTokens != 3 || stats.LatencySamples != 1 {
					t.Fatalf("bad stats: %+v", stats)
				}
				log := s.Store.Logs(1)[0]
				if !log.Usage.Known || log.Protocol != protocol {
					t.Fatalf("bad log: %+v", log)
				}
				if stream != (log.FirstTokenMS != nil) || log.FirstTokenMS != nil && *log.FirstTokenMS > log.LatencyMS {
					t.Fatalf("wrong first delta timing: %+v", log)
				}
				if len(log.AttemptDetails) != 1 || log.AttemptDetails[0].DurationMS == nil || log.AttemptDetails[0].Status != 200 || log.AttemptDetails[0].ErrorKind != "" {
					t.Fatalf("wrong attempt timing: %+v", log.AttemptDetails)
				}
			})
		}
	}
}

func TestEmptyUsageIsUnknownAndRetryCoverageIsExplicit(t *testing.T) {
	ctx := withUsageCapture(context.Background(), "chat")
	resetUsage(ctx)
	captureUsage(ctx, []byte(`{"usage":{}}`))
	if requestUsage(ctx).Known {
		t.Fatal("empty usage treated as measured zero")
	}
	resetUsage(ctx)
	captureUsage(ctx, []byte(`{"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	if !requestUsage(ctx).Known || !requestUsageIncomplete(ctx) {
		t.Fatal("partial retry coverage not exposed")
	}
}
