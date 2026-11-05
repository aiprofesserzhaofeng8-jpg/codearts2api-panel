package server

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/storage"
)

func TestAuditMessagesMalformedToolArgumentsReturnError(t *testing.T) {
	calls := 0
	s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"a\",\"type\":\"function\",\"function\":{\"name\":\"execute\",\"arguments\":\"{broken\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
	})
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	s.Store.UpdateSettings(func(c *storage.Settings) { c.MaxRetries = 1 })
	w := responseRequest(s, key, "/v1/messages", `{"model":"workbuddy/cn/shared","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`)
	if w.Code != 502 || strings.Contains(w.Body.String(), `"input":{}`) || calls != 1 {
		t.Fatal("bad arguments became successful executable tool", w.Code, w.Body.String(), calls)
	}
	if s.Store.PoolSnapshot()["wb-cn"].InFlight != 0 || s.Store.Account("wb-cn").Failures != 0 {
		t.Fatal("protocol failure leaked lease or penalized account")
	}
}

func TestAuditMessagesUnknownInputRejectedBeforeUpstream(t *testing.T) {
	calls := 0
	s, key, _ := nativeWBServer(t, func(http.ResponseWriter, *http.Request) { calls++ })
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	w := responseRequest(s, key, "/v1/messages", `{"model":"workbuddy/cn/shared","max_tokens":32,"messages":[{"role":"user","content":[{"type":"future_content","value":"important"}]}]}`)
	if w.Code != 400 || calls != 0 {
		t.Fatal("unknown block was silently sent", w.Code, calls)
	}
}
