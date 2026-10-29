package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"codearts2api/internal/store"
)

func anthropicToolChunk(index int, id, name, args string) string {
	call := map[string]any{"index": index, "function": map[string]string{"name": name, "arguments": args}}
	if id != "" {
		call["id"] = id
	}
	raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{call}}}}})
	return string(raw)
}

func anthropicToolFixture(chunks []string, finish string) string {
	var b strings.Builder
	for _, c := range chunks {
		fmt.Fprintf(&b, "data: %s\n\n", c)
	}
	fmt.Fprintf(&b, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\n", finish)
	b.WriteString("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":20}}\n\ndata: [DONE]\n\n")
	return b.String()
}

type anthropicTestBlock struct {
	ID, Name, Type, Text, Args string
	Closed                     bool
}

func readAnthropicBlocks(t *testing.T, raw string) (map[string]anthropicTestBlock, string, bool) {
	t.Helper()
	blocks := map[int]*anthropicTestBlock{}
	stop := ""
	failed, ended := false, false
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event struct {
			Type  string                                `json:"type"`
			Index int                                   `json:"index"`
			Block struct{ Type, ID, Name, Text string } `json:"content_block"`
			Delta struct {
				Type, Text, Thinking string
				Partial              string `json:"partial_json"`
				Stop                 string `json:"stop_reason"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(line[5:])), &event); err != nil {
			t.Fatal(err)
		}
		if ended {
			t.Fatal("event after message_stop")
		}
		switch event.Type {
		case "content_block_start":
			if _, ok := blocks[event.Index]; ok || event.Index != len(blocks) {
				t.Fatal("duplicate or noncontiguous block start")
			}
			blocks[event.Index] = &anthropicTestBlock{ID: event.Block.ID, Name: event.Block.Name, Type: event.Block.Type, Text: event.Block.Text}
		case "content_block_delta", "content_block_stop":
			b := blocks[event.Index]
			if b == nil || b.Closed {
				t.Fatalf("event %s for unstarted/closed block %d", event.Type, event.Index)
			}
			if event.Type == "content_block_stop" {
				b.Closed = true
			} else {
				b.Args += event.Delta.Partial
				b.Text += event.Delta.Text + event.Delta.Thinking
			}
		case "message_delta":
			stop = event.Delta.Stop
		case "error":
			failed = true
		case "message_stop":
			ended = true
		}
	}
	if !failed && !ended {
		t.Fatal("missing message_stop")
	}
	out := map[string]anthropicTestBlock{}
	for _, b := range blocks {
		if !failed && !b.Closed {
			t.Fatal("message ended with open block")
		}
		if b.Type == "tool_use" {
			if _, exists := out[b.ID]; exists || b.ID == "" {
				t.Fatal("duplicate or missing tool ID")
			}
			out[b.ID] = *b
		}
	}
	return out, stop, failed
}

func TestAnthropicToolBlocks(t *testing.T) {
	left, right := `{"path":"left.txt","n":9007199254740993}`, `{"path":"right.txt","s":"cc_version=42;11128"}`
	for _, tc := range []struct {
		name   string
		chunks []string
		want   map[string]string
	}{
		{"single", []string{anthropicToolChunk(0, "left", "read", left)}, map[string]string{"left": left}},
		{"sequential_same_name", []string{anthropicToolChunk(0, "left", "read", left), anthropicToolChunk(1, "right", "read", right)}, map[string]string{"left": left, "right": right}},
		{"interleaved", []string{anthropicToolChunk(0, "left", "read", left[:9]), anthropicToolChunk(1, "right", "read", right[:9]), anthropicToolChunk(0, "", "", left[9:]), anthropicToolChunk(1, "", "", right[9:])}, map[string]string{"left": left, "right": right}},
		{"late_metadata", []string{anthropicToolChunk(0, "", "", left[:9]), anthropicToolChunk(1, "right", "", right), anthropicToolChunk(1, "", "read", ""), anthropicToolChunk(0, "left", "read", left[9:])}, map[string]string{"left": left, "right": right}},
		{"text_between_tool_fragments", []string{anthropicToolChunk(0, "left", "read", left[:9]), `{"choices":[{"delta":{"content":"still streaming"}}]}`, anthropicToolChunk(1, "right", "read", right), anthropicToolChunk(0, "", "", left[9:])}, map[string]string{"left": left, "right": right}},
		{"repeated_metadata", []string{anthropicToolChunk(0, "left", "read", left[:9]), anthropicToolChunk(0, "left", "read", left[9:])}, map[string]string{"left": left}},
		{"empty_input", []string{anthropicToolChunk(0, "empty", "read", "")}, map[string]string{"empty": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
				calls++
				return responseUpstream(r, anthropicToolFixture(tc.chunks, "tool_calls")), nil
			})
			w := responseRequest(s, key, "/v1/messages", `{"model":"test-model","stream":true,"max_tokens":100,"messages":[{"role":"user","content":"fixture"}]}`)
			got, stop, failed := readAnthropicBlocks(t, w.Body.String())
			if w.Code != 200 || failed || stop != "tool_use" || calls != 1 || len(got) != len(tc.want) {
				t.Fatalf("status=%d stop=%s failed=%v calls=%d blocks=%+v", w.Code, stop, failed, calls, got)
			}
			for id, args := range tc.want {
				if b, ok := got[id]; !ok || b.Name != "read" || b.Args != args {
					t.Fatalf("tool %s: got %+v, want %s", id, b, args)
				}
			}
			if log := s.Store.Logs(1)[0]; log.Status != 200 || log.Attempts != 1 || log.Usage.OutputTokens != 20 {
				t.Fatalf("accounting: %+v", log)
			}
			if s.Store.PoolSnapshot()["a"].InFlight != 0 {
				t.Fatal("lease leaked")
			}
			if dir := os.Getenv("CODEARTS_TOOL_SSE_FIXTURES"); dir != "" {
				if err := os.WriteFile(filepath.Join(dir, tc.name+".sse"), w.Body.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestAnthropicToolFailuresAndTruncation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chunks []string
		finish string
		fail   bool
	}{
		{"invalid_complete", []string{anthropicToolChunk(0, "left", "read", `{"path":`)}, "tool_calls", true},
		{"truncated", []string{anthropicToolChunk(0, "left", "read", `{"path":`)}, "length", false},
		{"changed_id", []string{anthropicToolChunk(0, "left", "read", `{`), anthropicToolChunk(0, "right", "read", `}`)}, "tool_calls", true},
		{"duplicate_id", []string{anthropicToolChunk(0, "left", "read", `{}`), anthropicToolChunk(1, "left", "read", `{}`)}, "tool_calls", true},
		{"missing_name", []string{anthropicToolChunk(0, "left", "", `{}`)}, "tool_calls", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
				calls++
				return responseUpstream(r, anthropicToolFixture(tc.chunks, tc.finish)), nil
			})
			s.Store.UpdateSettings(func(st *store.Settings) { st.MaxRetries = 3 })
			w := responseRequest(s, key, "/v1/messages", `{"model":"test-model","stream":true,"messages":[{"role":"user","content":"fixture"}]}`)
			_, stop, failed := readAnthropicBlocks(t, w.Body.String())
			log := s.Store.Logs(1)[0]
			if failed != tc.fail || calls != 1 {
				t.Fatalf("failed=%v calls=%d body=%s", failed, calls, w.Body.String())
			}
			if tc.fail {
				if strings.Contains(w.Body.String(), "event: message_stop") || log.Status != 502 || s.Store.Stats().FailedRequests != 1 {
					t.Fatal("invalid tool reported success")
				}
			} else if stop != "max_tokens" || log.Status != 200 {
				t.Fatal("truncation lost")
			}
			if s.Store.PoolSnapshot()["a"].InFlight != 0 {
				t.Fatal("lease leaked")
			}
			if a := s.Store.Account("a"); a.Status != "ok" || a.Failures != 0 {
				t.Fatal("upstream tool format error treated as a credential fault")
			}
		})
	}
}

func TestAnthropicToolDeltaArrivesBeforeCompletion(t *testing.T) {
	gate := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(gate) }) }
	defer open()
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		reader, writer := io.Pipe()
		go func() {
			defer writer.Close()
			fmt.Fprintf(writer, "data: %s\n\n", anthropicToolChunk(0, "early", "read", `{"path":`))
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
			io.WriteString(writer, anthropicToolFixture([]string{anthropicToolChunk(0, "", "", `"file"}`)}, "tool_calls"))
		}()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader, Request: r}, nil
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := ts.Client()
	client.Timeout = 3 * time.Second
	req, _ := http.NewRequest("POST", ts.URL+"/v1/messages", strings.NewReader(`{"model":"test-model","stream":true,"messages":[{"role":"user","content":"fixture"}]}`))
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	seen := false
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "input_json_delta") {
			seen = true
			open()
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if !seen {
		t.Fatal("tool delta waited for upstream completion")
	}
}

func TestAnthropicToolsRealUpstreamReplay(t *testing.T) {
	raw, err := os.ReadFile("testdata/anthropic_parallel_tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Provider  string            `json:"provider"`
		Chunks    []json.RawMessage `json:"chunks"`
		Arguments map[string]string `json:"arguments_by_id"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.Provider, func(t *testing.T) {
			w := httptest.NewRecorder()
			s := newAnthropicStream(w, w, "real-replay", "test-model", 1)
			s.start()
			for _, chunk := range f.Chunks {
				if _, err := s.consume(chunk); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.finish(""); err != nil {
				t.Fatal(err)
			}
			blocks, stop, failed := readAnthropicBlocks(t, w.Body.String())
			if failed || stop != "tool_use" || len(blocks) != 2 {
				t.Fatalf("replay failed: %s", w.Body.String())
			}
			for id, args := range f.Arguments {
				if b, ok := blocks[id]; !ok || b.Args != args || b.Name != "acceptance_read_fixture" {
					t.Fatalf("tool %s lost or changed", id)
				}
			}
		})
	}
}

func BenchmarkAnthropicStreaming(b *testing.B) {
	for _, tools := range []bool{false, true} {
		b.Run(fmt.Sprintf("tools=%v", tools), func(b *testing.B) {
			chunks := make([][]byte, 0, 100)
			for i := 0; i < 100; i++ {
				if tools {
					args := strings.Repeat("x", 32)
					id, name := "", ""
					if i == 0 {
						id, name = "call", "read"
						args = `{"text":"` + args
					}
					if i == 99 {
						args += `"}`
					}
					chunks = append(chunks, []byte(anthropicToolChunk(0, id, name, args)))
				} else {
					chunks = append(chunks, []byte(`{"choices":[{"delta":{"content":"abcdefghijklmnopqrstuvwxyz012345"}}]}`))
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				w := httptest.NewRecorder()
				s := newAnthropicStream(w, w, "benchmark", "fixture", 1)
				s.start()
				for _, c := range chunks {
					_, _ = s.consume(c)
				}
				s.finish("")
			}
		})
	}
}
