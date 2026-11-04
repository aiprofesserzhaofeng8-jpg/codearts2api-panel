package teleagent

import (
	"bytes"
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"strconv"
	"time"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

func (c *Client) Stream(ctx context.Context, raw json.RawMessage, r provider.Request, emit func([]byte) error) error {
	a, e := decode(raw)
	if e != nil {
		return e
	}
	if !providerhttp.ModelID(r.Model) {
		return invalid("型号无效")
	}
	if parts, _ := jwtParts(a.Token); len(parts) != 3 {
		return invalid("账号令牌无效")
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(r.Body, &body) != nil || body == nil {
		return invalid("请求须为 JSON 对象")
	}
	var messages []json.RawMessage
	if json.Unmarshal(body["messages"], &messages) != nil || len(messages) == 0 {
		return invalid("缺少 messages")
	}
	if n := body["n"]; len(n) > 0 && string(n) != "null" && string(n) != "1" {
		return invalid("目前仅支持 n=1")
	}
	body["model"], _ = json.Marshal(r.Model)
	body["stream"] = json.RawMessage("true")
	// Keep other stream options, tools, developer/system messages and numeric JSON.
	options := map[string]json.RawMessage{}
	if b := body["stream_options"]; len(b) > 0 && string(b) != "null" {
		if json.Unmarshal(b, &options) != nil || options == nil {
			return invalid("stream_options 无效")
		}
	}
	options["include_usage"] = json.RawMessage("true")
	body["stream_options"], _ = json.Marshal(options)
	b, e := json.Marshal(body)
	if e != nil {
		return invalid("请求序列化失败")
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	n, e := nonce()
	if e != nil {
		return e
	}
	h := identityHeaders(a)
	h.Set("Authorization", "Bearer "+a.APIKey)
	h.Set("X-Superagent-Timestamp", ts)
	h.Set("X-Superagent-Nonce", n)
	h.Set("X-Superagent-Sign-Version", "v1")
	h.Set("X-Superagent-Signature", signature(a, http.MethodPost, ChatPath, ts, n, b))
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "text/event-stream")
	h.Set("User-Agent", "codearts2api-panel/teleagent")
	sid := r.SessionID
	if sid == "" {
		sid = "ses_" + n
	}
	id := r.RequestID
	if id == "" {
		id = n
	}
	h.Set("X-Request-Id", id)
	h.Set("X-Session-Id", sid)
	h.Set("X-Teleai-Root-Session-Id", sid)
	h.Set("X-Message-Id", "msg_"+n)
	h.Set("X-Teleai-Upstream-Request-Id", n)
	h.Set("X-Teleai-Run-Unit-Type", "agent_loop")
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.Origin+ChatPath, bytes.NewReader(b))
	if e != nil {
		return providerhttp.Broken("TeleAgent")
	}
	req.Header = h
	resp, e := c.HTTP.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &provider.Error{Status: 502, Message: "TeleAgent 上游连接失败"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return providerhttp.Failure("TeleAgent", resp.StatusCode)
	}
	typ, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if typ != "text/event-stream" {
		o, e := unwrap(resp)
		if e != nil {
			return e
		}
		_ = o
		return providerhttp.Broken("TeleAgent")
	}
	finished := false
	seen := false
	return providerhttp.SSE(ctx, resp.Body, "TeleAgent", func(event string, b []byte) error {
		if string(bytes.TrimSpace(b)) == "[DONE]" {
			if !seen || !finished {
				return providerhttp.Broken("TeleAgent")
			}
			return providerhttp.End
		}
		var chunk map[string]json.RawMessage
		if json.Unmarshal(b, &chunk) != nil || chunk == nil {
			return providerhttp.Broken("TeleAgent")
		}
		if er := chunk["error"]; len(er) > 0 && string(er) != "null" {
			var v map[string]json.RawMessage
			if json.Unmarshal(er, &v) == nil && len(v["code"]) > 0 {
				return businessError(v["code"])
			}
			return providerhttp.Broken("TeleAgent")
		}
		if code := chunk["code"]; len(code) > 0 && string(code) != "0" && string(code) != `"0"` {
			return businessError(code)
		}
		var choices []struct {
			Index  int                        `json:"index"`
			Delta  map[string]json.RawMessage `json:"delta"`
			Finish *string                    `json:"finish_reason"`
		}
		if json.Unmarshal(chunk["choices"], &choices) != nil {
			return providerhttp.Broken("TeleAgent")
		}
		if len(choices) == 0 {
			if !seen || len(chunk["usage"]) == 0 || string(chunk["usage"]) == "null" {
				return providerhttp.Broken("TeleAgent")
			}
		} else {
			if len(choices) != 1 || choices[0].Index != 0 {
				return providerhttp.Broken("TeleAgent")
			}
			ch := choices[0]
			if finished || ch.Delta == nil && (ch.Finish == nil || *ch.Finish == "") {
				return providerhttp.Broken("TeleAgent")
			}
			seen = true
			if ch.Finish != nil && *ch.Finish != "" {
				finished = true
			}
		}
		return emit(b)
	})
}
