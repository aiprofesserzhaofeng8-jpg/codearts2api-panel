package kiro

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"codearts2api/internal/provider"
)

func TestResponseUsesBodyNotContentType(t *testing.T) {
	for _, ct := range []string{"application/vnd.amazon.eventstream", "application/json", "", "text/plain"} {
		t.Run(ct, func(t *testing.T) {
			c := New()
			payload := eventFrame("assistantResponseEvent", `{"content":"marker"}`)
			c.HTTP.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{ct}}, Body: io.NopCloser(bytes.NewReader(payload))}, nil
			})
			var output []byte
			err := c.Stream(t.Context(), rawJSON(fixtureOAuth()), provider.Request{Model: "auto", Body: []byte(`{"messages":[{"role":"user","content":"hello"}]}`)}, func(b []byte) error { output = append(output, b...); return nil })
			if err != nil || !bytes.Contains(output, []byte("marker")) || !bytes.Contains(output, []byte(`"finish_reason":"stop"`)) {
				t.Fatalf("output=%s err=%v", output, err)
			}
		})
	}
}

func TestResponseErrorsRemainFailuresAndPrivate(t *testing.T) {
	for _, tc := range []struct {
		code   string
		status int
		fault  bool
	}{
		{"UnauthorizedException", 401, true}, {"ExpiredTokenException", 401, true},
		{"AccessDeniedException", 403, false}, {"ServiceQuotaExceededException", 402, false},
		{"ThrottlingException", 429, false}, {"ValidationException", 400, false},
		{"private-unknown", 502, false},
	} {
		for _, format := range []string{"json", "event"} {
			t.Run(tc.code+"/"+format, func(t *testing.T) {
				body := []byte(` {"__type":"aws.test#` + tc.code + `","message":"private-secret"}`)
				if format == "event" {
					body = frameWithHeaders(map[string]string{":message-type": "exception", ":exception-type": tc.code}, `{"message":"private-secret"}`)
				}
				n := 0
				err := convertResponse(t.Context(), bytes.NewReader(body), "auto", func([]byte) error { n++; return nil })
				var pe *provider.Error
				if !errors.As(err, &pe) || pe.Status != tc.status || pe.AccountFault != tc.fault || n != 0 {
					t.Fatalf("error=%+v chunks=%d", err, n)
				}
				if strings.Contains(err.Error(), "private") || strings.Contains(pe.Code, "private") {
					t.Fatal("private upstream data exposed")
				}
			})
		}
	}
	for _, body := range []string{"", `{"content":"not a stream"}`, `{"code":42}`, `{broken`, "<html>error</html>", strings.Repeat(" ", maxErrorResponse+1)} {
		if err := convertResponse(t.Context(), strings.NewReader(body), "auto", func([]byte) error { t.Fatal("unexpected output"); return nil }); err == nil {
			t.Fatal("invalid response succeeded")
		}
	}
}

func TestResponseStreamsBeforeEOFAndPropagatesCancellation(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		defer writer.Close()
		writer.Write(eventFrame("assistantResponseEvent", `{"content":"incremental"}`))
		<-ctx.Done()
	}()
	sentinel := errors.New("consumer stopped")
	err := convertResponse(ctx, reader, "auto", func([]byte) error { cancel(); return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if err := convertResponse(ctx, strings.NewReader(""), "auto", func([]byte) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestResponseDoesNotResynchronizeAfterCorruption(t *testing.T) {
	valid := eventFrame("assistantResponseEvent", `{"content":"partial"}`)
	damaged := bytes.Clone(valid)
	damaged[len(damaged)-1] ^= 1
	for _, body := range [][]byte{append(damaged, valid...), append(bytes.Clone(valid), damaged...), valid[:len(valid)-1]} {
		var output []byte
		err := convertResponse(t.Context(), bytes.NewReader(body), "auto", func(b []byte) error { output = append(output, b...); return nil })
		if err == nil || bytes.Contains(output, []byte(`"finish_reason":"stop"`)) {
			t.Fatal("damaged response completed", err)
		}
	}
}

// An endless malformed JSON body cannot turn the error path into an unbounded
// download. No giant allocation is needed to exercise the limit.
type endlessWhitespace struct{ read int }

func (r *endlessWhitespace) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	r.read += len(p)
	return len(p), nil
}
func TestJSONErrorReadIsBounded(t *testing.T) {
	r := &endlessWhitespace{}
	err := convertResponse(t.Context(), r, "auto", func([]byte) error { t.Fatal("unexpected output"); return nil })
	if err == nil || r.read > maxErrorResponse+1 {
		t.Fatalf("err=%v bytes=%d", err, r.read)
	}
}
