package mobileclaw

import (
	"bytes"
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"mime"
	"net/http"
)

func (c *Client) Stream(ctx context.Context, raw json.RawMessage, r provider.Request, emit func([]byte) error) error {
	normalized, _, err := c.Normalize(raw)
	if err != nil {
		return err
	}
	if !providerhttp.ModelID(r.Model) {
		return invalid("型号无效")
	}
	var a Credentials
	_ = json.Unmarshal(normalized, &a)
	if a.AIProvider == "" {
		return invalid("账号尚未完成供应商授权，请重新扫码登录并确认替换原账号；桌面导入账号请重新导入已选择供应商的凭据")
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
	// All downstream protocols use this stream, including non-stream requests.
	// Ask explicitly for usage so accounting does not depend on upstream defaults.
	options := map[string]json.RawMessage{}
	if b := body["stream_options"]; len(b) > 0 && string(b) != "null" {
		if json.Unmarshal(b, &options) != nil || options == nil {
			return invalid("stream_options 无效")
		}
	}
	options["include_usage"] = json.RawMessage("true")
	body["stream_options"], _ = json.Marshal(options)
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", c.Origin+"/desktop/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("service-version", ServiceVersion)
	req.Header.Set("device-id", a.DeviceID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("pkg-types", "FREE")
	if a.AIProvider != "" {
		req.Header.Set("ai-provider", a.AIProvider)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return provider.NewTransportError("MobileClaw", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return providerhttp.FailureResponseWithCode("MobileClaw", resp)
	}
	typ, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if typ != "text/event-stream" {
		return providerhttp.Broken("MobileClaw")
	}
	finished := false
	seen := false
	return providerhttp.SSE(ctx, resp.Body, "MobileClaw", func(event string, b []byte) error {
		if string(bytes.TrimSpace(b)) == "[DONE]" {
			if !seen || !finished {
				return providerhttp.Broken("MobileClaw")
			}
			return providerhttp.End
		}
		var chunk map[string]json.RawMessage
		if json.Unmarshal(b, &chunk) != nil || chunk == nil {
			return providerhttp.Broken("MobileClaw")
		}
		if er := chunk["error"]; len(er) > 0 && string(er) != "null" {
			return providerhttp.Broken("MobileClaw")
		}
		var choices []struct {
			Index  int                        `json:"index"`
			Delta  map[string]json.RawMessage `json:"delta"`
			Finish *string                    `json:"finish_reason"`
		}
		if json.Unmarshal(chunk["choices"], &choices) != nil {
			return providerhttp.Broken("MobileClaw")
		}
		if len(choices) == 0 {
			if !seen || len(chunk["usage"]) == 0 || string(chunk["usage"]) == "null" {
				return providerhttp.Broken("MobileClaw")
			}
		} else {
			if len(choices) != 1 || choices[0].Index != 0 {
				return providerhttp.Broken("MobileClaw")
			}
			ch := choices[0]
			if finished || ch.Delta == nil && (ch.Finish == nil || *ch.Finish == "") {
				return providerhttp.Broken("MobileClaw")
			}
			seen = true
			if ch.Finish != nil && *ch.Finish != "" {
				finished = true
			}
		}
		return emit(b)
	})
}
