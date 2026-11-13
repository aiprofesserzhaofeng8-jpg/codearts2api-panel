package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestKiroJSONLabelledStreamThroughGateway(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, tools := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%v/tools=%v", stream, tools), func(t *testing.T) {
				calls := 0
				s, key, c := nativeKiroServer(t, func(w http.ResponseWriter, r *http.Request) {
					calls++
					w.Header().Set("Content-Type", "application/json")
					if tools {
						w.Write(kiroFrame("toolUseEvent", `{"toolUseId":"call-1","name":"lookup","input":"{\"id\":9007199254740993}","stop":true}`))
					} else {
						w.Write(kiroFrame("assistantResponseEvent", `{"content":"HEADER_FIX_OK"}`))
					}
				})
				addKiro(t, s, c, "us-east-1", false)
				body := map[string]any{"model": "kiro/us-east-1/kmodel", "messages": []map[string]string{{"role": "user", "content": "hello"}}, "stream": stream}
				if tools {
					body["tools"] = []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup", "parameters": map[string]any{"type": "object"}}}}
				}
				w := responseRequest(s, key, "/v1/chat/completions", string(mustJSON(body)))
				want := "HEADER_FIX_OK"
				if tools {
					want = "9007199254740993"
				}
				if w.Code != 200 || !strings.Contains(w.Body.String(), want) || calls != 1 {
					t.Fatalf("status=%d calls=%d body=%s", w.Code, calls, w.Body.String())
				}
				if s.Store.PoolSnapshot()["kiro-us-east-1"].InFlight != 0 {
					t.Fatal("lease leaked")
				}
			})
		}
	}
}
