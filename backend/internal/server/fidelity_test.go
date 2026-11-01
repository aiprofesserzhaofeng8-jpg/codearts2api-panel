package server

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"codearts2api/internal/registry"
)

func TestChatAggregationPreservesRefusal(t *testing.T) {
	for _, name := range []string{"codearts", "workbuddy"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", name, stream), func(t *testing.T) {
				fixture := "data: {\"choices\":[{\"index\":0,\"delta\":{\"refusal\":\"Cannot comply.\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
				calls := 0
				h := func(w http.ResponseWriter, r *http.Request) {
					calls++
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, fixture)
				}
				var s *Server
				var key, model string
				if name == "workbuddy" {
					s, key, _ = nativeWBServer(t, h)
					addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
					model = "workbuddy/cn/shared"
				} else {
					s, key = responseTestServer(t, func(r *http.Request) (*http.Response, error) {
						w := httptest.NewRecorder()
						h(w, r)
						return w.Result(), nil
					})
					model = "test-model"
					s.Reg.Replace([]registry.Model{{ID: model, StreamOnly: true}})
				}
				w := responseRequest(s, key, "/v1/chat/completions", fmt.Sprintf(`{"model":%q,"stream":%v,"messages":[{"role":"user","content":"fixture"}]}`, model, stream))
				if w.Code != 200 || !strings.Contains(w.Body.String(), `"refusal":"Cannot comply."`) || calls != 1 {
					t.Fatalf("refusal lost or retried: %d %s calls=%d", w.Code, w.Body.String(), calls)
				}
			})
		}
	}
}

// Gate the upstream completion until the actual HTTP client has received the
// first content event. This detects full-response buffering without timing the
// upstream model or relying on a mock ResponseRecorder's Flush flag.
func TestFidelityStreamReturnsBeforeUpstreamCompletion(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	defer open()
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		reader, writer := io.Pipe()
		go func() {
			defer writer.Close()
			io.WriteString(writer, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"FIRST\"}}]}\n\n")
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			io.WriteString(writer, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader, Request: r}, nil
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := ts.Client()
	client.Timeout = 3 * time.Second
	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer "+key)
	got, done := make(chan error, 1), make(chan struct{})
	go func() {
		defer close(done)
		resp, err := client.Do(req)
		if err != nil {
			got <- err
			return
		}
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "FIRST") {
				got <- nil
				open()
				io.Copy(io.Discard, resp.Body)
				return
			}
		}
		got <- fmt.Errorf("missing first delta")
	}()
	select {
	case err := <-got:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		open()
		t.Fatal("stream buffered until completion")
	}
	<-done
}
