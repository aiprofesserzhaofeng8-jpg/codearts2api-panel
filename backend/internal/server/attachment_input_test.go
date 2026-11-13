package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	apicompat "codearts2api/internal/responsescompat"
)

func TestUnsupportedAttachmentsRejectedBeforeUpstream(t *testing.T) {
	var calls atomic.Int32
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return responseUpstream(r, responsesTextFixture), nil
	})
	blocks := []string{
		`{"type":"input_file","file_data":"data:application/pdf;base64,JVBERg=="}`,
		`{"type":"file","file":{"file_id":"file-private"}}`,
		`{"type":"document","source":{"type":"url","url":"http://127.0.0.1/private"}}`,
		`{"type":"input_image","file_id":"file-private"}`,
		`{"type":"image_file","image_file":{"file_id":"file-private"}}`,
		`{"type":"tool_result","tool_use_id":"read","content":[{"type":"document","source":{"type":"base64","data":"QQ=="}}]}`,
	}
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		field := "messages"
		if path == "/v1/responses" {
			field = "input"
		}
		for _, stream := range []bool{false, true} {
			for i, block := range blocks {
				t.Run(fmt.Sprintf("%s/%v/%d", path, stream, i), func(t *testing.T) {
					body := fmt.Sprintf(`{"model":"test-model","stream":%v,"max_tokens":128,%q:[{"role":"user","content":"hello"},{"role":"user","content":[{"type":"text","text":"read this"},%s]}]}`, stream, field, block)
					w := responseRequest(s, key, path, body)
					if w.Code != 400 || !strings.Contains(w.Body.String(), "unsupported_file_input") {
						t.Fatalf("status %d: %s", w.Code, w.Body.String())
					}
					if strings.Contains(w.Header().Get("Content-Type"), "event-stream") {
						t.Fatal("committed streaming response before rejection")
					}
				})
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unsupported attachment reached upstream")
	}
}

func TestAttachmentGuardPreservesTextImagesAndOpaqueTools(t *testing.T) {
	bodies := []string{
		`{"messages":[{"role":"user","content":"input_file file_id document"}]}`,
		`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,QQ=="}}]}]}`,
		`{"input":[{"type":"input_image","image_url":"data:image/png;base64,QQ=="}]}`,
		`{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"read","name":"read","input":{"type":"document","file_id":"example","content":{"type":"file"}}}]}]}`,
		`{"input":[{"type":"function_call","name":"read","arguments":"{\"type\":\"document\"}","call_id":"c"},{"type":"function_call_output","call_id":"c","output":"{\"type\":\"document\"}"}]}`,
		`{"messages":[{"role":"user","content":[{"type":"tool_result","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"QQ=="}}]}]}],"tools":[{"type":"document","file_id":"schema"}],"number":9007199254740993}`,
	}
	for _, body := range bodies {
		if err := rejectUnsupportedFiles([]byte(body)); err != nil {
			t.Fatalf("valid content rejected: %v: %s", err, body)
		}
	}
}

func TestFileRepositoryRoutesAndCapabilityRemoved(t *testing.T) {
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) { t.Fatal("unexpected upstream call"); return nil, nil })
	for _, path := range []string{"/v1/files", "/v1/files/file-example", "/v1/files/file-example/content"} {
		for _, method := range []string{"GET", "POST", "DELETE"} {
			r := httptest.NewRequest(method, path, strings.NewReader("private-file"))
			r.Header.Set("Authorization", "Bearer "+key)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != 404 {
				t.Fatalf("%s %s: %d", method, path, w.Code)
			}
		}
	}
	if _, ok := gatewayCapabilities()["files"]; ok {
		t.Fatal("file support still advertised")
	}
}

func TestResponsesConversionDoesNotDropFiles(t *testing.T) {
	for _, input := range []string{
		`[{"type":"input_file","file_id":"file-example"}]`,
		`[{"role":"user","content":[{"type":"input_text","text":"read"},{"type":"input_file","file_id":"file-example"}]}]`,
	} {
		var req apicompat.ResponsesRequest
		if err := json.Unmarshal([]byte(`{"model":"test-model","input":`+input+`}`), &req); err != nil {
			t.Fatal(err)
		}
		if _, err := apicompat.ResponsesToChatCompletionsRequest(&req); err == nil {
			t.Fatal("conversion silently accepted file")
		}
	}
}
