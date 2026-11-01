package codearts

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	legacy "codearts2api/internal/codearts"
	"codearts2api/internal/gateway"
)

var _ gateway.ChatSession = Session{}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBoundSessionPreservesSigningBodyAndGeneration(t *testing.T) {
	body := []byte(`{"model":"physical","messages":[{"role":"user","content":"fixture"}],"future":9007199254740993}`)
	reply := []byte(` {"choices":[{"message":{"tool_calls":[{"id":"tool","type":"function","function":{"name":"read","arguments":"{\"id\":9007199254740993}"}}]},"finish_reason":"tool_calls"}],"future":9007199254740995} `)
	client := legacy.NewClient()
	credentials := &legacy.Session{AccessKey: "immutable-generation", SecretKey: "fixture", SecurityToken: "fixture-sts"}
	client.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Equal(got, body) || !strings.Contains(r.Header.Get("Authorization"), "immutable-generation") || r.Header.Get("x-security-token") != "fixture-sts" {
			t.Fatal("native adapter changed credentials, signing or body")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(reply)), Request: r}, nil
	})}
	bound := New(client).Bind(credentials)
	if bound.credentials != credentials {
		t.Fatal("immutable credential generation was copied")
	}
	got, err := bound.Chat(t.Context(), "physical", body)
	if err != nil || !bytes.Equal(got, reply) {
		t.Fatal("native reply was reserialized or lost tool data", err)
	}
}

func TestStreamBusinessErrorsRemainPlatformErrorsBeforeOutput(t *testing.T) {
	for _, chunk := range []string{
		`{"error_code":"TM.00001041","error_msg":"busy"}`,
		`{"error_code":"TM.00001041","error_msg":"busy TM.00001041"}`,
		`{"error_code":"TM.00001041"}`,
	} {
		t.Run(chunk, func(t *testing.T) {
			client := legacy.NewClient()
			client.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("data: " + chunk + "\n\n")), Request: r}, nil
			})}
			bound := New(client).Bind(&legacy.Session{AccessKey: "fixture", SecretKey: "fixture"})
			calls := 0
			err := bound.Stream(t.Context(), "physical", []byte(`{"stream":true}`), func([]byte) error { calls++; return nil })
			message, status, _ := legacy.IsUpstreamBusinessError([]byte(chunk))
			want := legacy.UpstreamBusinessError(status, message)
			if err == nil || err.Error() != want.Error() || legacy.IsTransientUpstream(err) != legacy.IsTransientUpstream(want) || calls != 0 {
				t.Fatal("business error reached downstream or changed legacy classification", err)
			}
		})
	}
}

func TestNativeConsumerErrorAndFallbackClassification(t *testing.T) {
	stopped := errors.New("consumer stopped")
	client := legacy.NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"first\"}}]}\n\n")), Request: r}, nil
	})}
	bound := New(client).Bind(&legacy.Session{AccessKey: "fixture", SecretKey: "fixture"})
	if err := bound.Stream(t.Context(), "physical", nil, func([]byte) error { return stopped }); !errors.Is(err, stopped) {
		t.Fatal("consumer error was replaced")
	}
	if !bound.NeedsStreamFallback(&legacy.UpstreamError{Status: 400}) || bound.NeedsStreamFallback(&legacy.UpstreamError{Status: 400, Message: "bad request"}) || bound.NeedsStreamFallback(context.Canceled) {
		t.Fatal("fallback eligibility changed")
	}
}
