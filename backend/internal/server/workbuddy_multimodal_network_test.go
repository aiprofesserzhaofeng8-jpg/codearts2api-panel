package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/ratelimit"
	"codearts2api/internal/storage"
)

func TestWorkBuddyMultimodalAllInputProtocols(t *testing.T) {
	image := "data:image/png;base64,AAAA"
	for _, tc := range []struct{ name, path, body string }{
		{"chat", "/v1/chat/completions", `{"model":"workbuddy/cn/shared","messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"` + image + `"}}]}]}`},
		{"responses", "/v1/responses", `{"model":"workbuddy/cn/shared","input":[{"role":"user","content":[{"type":"input_text","text":"describe"},{"type":"input_image","image_url":"` + image + `"}]}]}`},
		{"messages", "/v1/messages", `{"model":"workbuddy/cn/shared","max_tokens":32,"messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]}`},
		{"tool_image", "/v1/messages", `{"model":"workbuddy/cn/shared","max_tokens":32,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"tool-photo","name":"read","input":{"file":"test.png"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-photo","content":[{"type":"text","text":"screenshot"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]}]}`},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(tc.name+"/"+map[bool]string{false: "json", true: "sse"}[stream], func(t *testing.T) {
				calls := 0
				s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
					calls++
					var body struct {
						Messages []struct {
							Role    string          `json:"role"`
							Content json.RawMessage `json:"content"`
						} `json:"messages"`
					}
					if json.NewDecoder(r.Body).Decode(&body) != nil {
						t.Fatal("bad upstream JSON")
					}
					found := false
					toolIndex, mediaIndex := -1, -1
					for i, m := range body.Messages {
						if m.Role == "tool" {
							toolIndex = i
							if strings.Contains(string(m.Content), "base64") {
								t.Error("tool image serialized as text")
							}
						}
						var parts []struct {
							Type  string `json:"type"`
							Image struct {
								URL string `json:"url"`
							} `json:"image_url"`
						}
						if json.Unmarshal(m.Content, &parts) == nil {
							for _, p := range parts {
								if p.Type == "image_url" && p.Image.URL == image {
									found = true
									mediaIndex = i
								}
							}
						}
					}
					if !found {
						t.Error("image lost before upstream")
					}
					if tc.name == "tool_image" && (toolIndex < 0 || mediaIndex <= toolIndex) {
						t.Error("tool response sequence invalid")
					}
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"SEEN\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				})
				addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
				body := tc.body
				if stream {
					body = strings.TrimSuffix(body, "}") + `,"stream":true}`
				}
				w := responseRequest(s, key, tc.path, body)
				if w.Code != 200 || calls != 1 {
					t.Fatal(w.Code, w.Body.String(), calls)
				}
			})
		}
	}
}

func TestWorkBuddyNetworkRecoveryWithLocalLimiter(t *testing.T) {
	broken := true
	s, key, client := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if broken {
			io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n")
			return
		}
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"RECOVERED\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	})
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	s.Store.UpdateSettings(func(c *storage.Settings) {
		c.MaxRetries = 1
		c.Pool.BreakerThreshold = 1
		c.Pool.BreakerCooldownSec = 3600
		c.Pool.MaxInFlight = 1
	})
	s.Limits.SetConfig(ratelimit.Config{Enabled: true, QPS: 100, Burst: 100, Concurrency: 1})
	transport := client.HTTP.Transport
	dialFailure := false
	client.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if dialFailure {
			return nil, errors.New("fixture network outage")
		}
		return transport.RoundTrip(r)
	})
	for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
		for _, stream := range []bool{false, true} {
			body := fmt.Sprintf(`{"model":"workbuddy/cn/shared","stream":%v,"max_tokens":32,"messages":[{"role":"user","content":"hello"}],"input":"hello"}`, stream)
			for _, network := range []bool{true, false} {
				broken, dialFailure = true, network
				w := responseRequest(s, key, path, body)
				if w.Code == 429 || w.Code == 503 {
					t.Fatal("network test unexpectedly hit local limiter", w.Code, w.Body.String())
				}
				a := s.Store.Account("wb-cn")
				if a.Failures != 0 || a.CooldownUntil != 0 || s.Store.PoolSnapshot()[a.ID].InFlight != 0 {
					t.Fatal("transport failure retained health or lease state")
				}
				broken, dialFailure = false, false
				w = responseRequest(s, key, path, body)
				if w.Code != 200 || !strings.Contains(w.Body.String(), "RECOVERED") {
					t.Fatal("next request requires clearing limiter/restarting", path, stream, w.Code, w.Body.String())
				}
			}
		}
	}
}

func TestWorkBuddyNetworkFailuresDoNotRequirePanelReset(t *testing.T) {
	s, _, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {})
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	s.Store.UpdateSettings(func(c *storage.Settings) {
		c.MaxRetries = 1
		c.Pool.BreakerThreshold = 1
		c.Pool.BreakerCooldownSec = 3600
	})
	ctx := withRoutingModel(context.Background(), "workbuddy/cn/shared")
	for _, cause := range []error{errors.New("dial fixture failure"), io.ErrUnexpectedEOF, io.EOF} {
		_, err := s.withAccountRetry(ctx, time.Second, func(context.Context, *accounts.Account) error { return provider.NewTransportError("WorkBuddy", cause) })
		if err == nil {
			t.Fatal("network failure reported as success")
		}
		a := s.Store.Account("wb-cn")
		if a.Failures != 0 || a.CooldownUntil != 0 || s.Store.PoolSnapshot()[a.ID].InFlight != 0 {
			t.Fatal("network failure penalized account or leaked lease", a.Failures, a.CooldownUntil)
		}
	}
	called := false
	_, err := s.withAccountRetry(ctx, time.Second, func(context.Context, *accounts.Account) error { called = true; return nil })
	if err != nil || !called {
		t.Fatal("healthy follow-up required a reset", err)
	}
}
