// Package providerhttp contains bounded HTTP and SSE transport helpers for
// native providers. It does not select accounts, persist state or retry calls.
package providerhttp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"codearts2api/internal/provider"
)

func New() *http.Client {
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second, IdleConnTimeout: 90 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func Invalid(name, text string) error {
	return &provider.Error{Status: 400, Message: name + ": " + text}
}
func Broken(name string) error {
	return &provider.Error{Status: 502, Message: name + " 返回了无效或不完整的响应"}
}
func Failure(name string, status int) error {
	if status < 400 || status > 599 {
		status = 502
	}
	return &provider.Error{Status: status, AccountFault: status == 401, Message: fmt.Sprintf("%s 上游请求失败（HTTP %d）", name, status)}
}
func Do(ctx context.Context, client *http.Client, name, method, endpoint string, headers http.Header, body any) (*http.Response, error) {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, Invalid(name, "无效的 JSON 请求")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, Broken(name)
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = http.Header{}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "codearts2api-panel/"+strings.ToLower(name))
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &provider.Error{Status: 502, Message: name + " 上游连接失败"}
	}
	return resp, nil
}
func JSON(resp *http.Response, name string) (map[string]json.RawMessage, error) {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, Failure(name, resp.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	var o map[string]json.RawMessage
	if e != nil || len(b) > 4<<20 || json.Unmarshal(b, &o) != nil || o == nil || (len(o["error"]) > 0 && string(o["error"]) != "null") {
		return nil, Broken(name)
	}
	return o, nil
}
func Field(o map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		var s string
		if json.Unmarshal(o[k], &s) == nil && s != "" {
			return s
		}
	}
	return ""
}
func Timestamp(b json.RawMessage) int64 {
	var s string
	if json.Unmarshal(b, &s) != nil {
		s = string(b)
	}
	if n, e := strconv.ParseInt(s, 10, 64); e == nil {
		if n > 1e12 {
			n /= 1000
		}
		return n
	}
	if t, e := time.Parse(time.RFC3339Nano, s); e == nil {
		return t.Unix()
	}
	return 0
}
func Safe(values ...string) bool {
	for _, v := range values {
		if len(v) > 32768 || strings.ContainsAny(v, "\r\n\x00") {
			return false
		}
	}
	return true
}
func ModelID(id string) bool {
	return id != "" && len(id) <= 200 && !strings.ContainsAny(id, "/\r\n\x00")
}

// End is returned by a consumer after an authoritative terminal event.
var End = errors.New("upstream terminal event")

func SSE(ctx context.Context, r io.Reader, name string, consume func(string, []byte) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 8<<20)
	var event string
	var data []byte
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Text()
		if line == "" {
			if len(data) > 0 {
				if e := consume(event, data); e != nil {
					if errors.Is(e, End) {
						return nil
					}
					return e
				}
			}
			event = ""
			data = nil
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		if key == "event" {
			event = value
		}
		if key == "data" {
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, value...)
			if len(data) > 8<<20 {
				return Broken(name)
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if scanner.Err() != nil || len(data) > 0 {
		return Broken(name)
	}
	// EOF alone is never an authoritative successful completion.
	return Broken(name)
}

type Chunks struct {
	Model, ID string
	Created   int64
	Started   bool
	Emit      func([]byte) error
}

func (c *Chunks) Send(delta any, finish any) error {
	if c.Created == 0 {
		c.Created = time.Now().Unix()
	}
	if c.ID == "" {
		c.ID = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	}
	b, e := json.Marshal(map[string]any{"id": c.ID, "object": "chat.completion.chunk", "created": c.Created, "model": c.Model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
	if e != nil {
		return e
	}
	return c.Emit(b)
}
func (c *Chunks) Start() error {
	if c.Started {
		return nil
	}
	c.Started = true
	return c.Send(map[string]string{"role": "assistant"}, nil)
}
func (c *Chunks) Usage(input, output, cached, cacheWrite int64) error {
	if input < 0 || output < 0 || cached < 0 || cacheWrite < 0 || input > 1<<50 || output > 1<<50 {
		return Broken("Provider")
	}
	u := map[string]any{"prompt_tokens": input, "completion_tokens": output, "total_tokens": input + output, "prompt_tokens_details": map[string]int64{"cached_tokens": cached, "cache_creation_tokens": cacheWrite}}
	b, e := json.Marshal(map[string]any{"id": c.ID, "object": "chat.completion.chunk", "created": c.Created, "model": c.Model, "choices": []any{}, "usage": u})
	if e != nil {
		return e
	}
	return c.Emit(b)
}
