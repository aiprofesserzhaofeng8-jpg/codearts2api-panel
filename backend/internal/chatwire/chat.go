// Package chatwire validates Chat Completions without accounts, HTTP or storage.
package chatwire

import (
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"io"
	"strings"
)

func Encode(r provider.Request, name string) (any, error) {
	if !providerhttp.ModelID(r.Model) {
		return nil, providerhttp.Invalid(name, "型号无效")
	}
	return EncodeQualified(r, name)
}

// EncodeQualified accepts vendor-qualified model IDs in a JSON body, never a URL.
// Platform adapters must validate their own catalog and protocol boundaries.
func EncodeQualified(r provider.Request, name string) (any, error) {
	if r.Model == "" || len(r.Model) > 200 || strings.ContainsAny(r.Model, "\r\n\x00") {
		return nil, providerhttp.Invalid(name, "型号无效")
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(r.Body, &body) != nil || body == nil {
		return nil, providerhttp.Invalid(name, "请求须为 JSON 对象")
	}
	var msgs []json.RawMessage
	if json.Unmarshal(body["messages"], &msgs) != nil || len(msgs) == 0 {
		return nil, providerhttp.Invalid(name, "缺少 messages")
	}
	if n := body["n"]; len(n) > 0 && string(n) != "null" && string(n) != "1" {
		return nil, providerhttp.Invalid(name, "仅支持 n=1")
	}
	body["model"], _ = json.Marshal(r.Model)
	body["stream"] = json.RawMessage("true")
	opts := map[string]json.RawMessage{}
	if v := body["stream_options"]; len(v) > 0 && string(v) != "null" {
		if json.Unmarshal(v, &opts) != nil || opts == nil {
			return nil, providerhttp.Invalid(name, "stream_options 无效")
		}
	}
	opts["include_usage"] = json.RawMessage("true")
	body["stream_options"], _ = json.Marshal(opts)
	return body, nil
}
func Decode(ctx context.Context, reader io.Reader, name string, emit func([]byte) error) error {
	return DecodeWithError(ctx, reader, name, nil, emit)
}

// DecodeWithError lets a provider classify its own structured SSE errors.
func DecodeWithError(ctx context.Context, reader io.Reader, name string, classify func([]byte) error, emit func([]byte) error) error {
	seen, finished := false, false
	return providerhttp.SSE(ctx, reader, name, func(event string, b []byte) error {
		if strings.TrimSpace(string(b)) == "[DONE]" {
			if !seen || !finished {
				return providerhttp.Broken(name)
			}
			return providerhttp.End
		}
		var chunk map[string]json.RawMessage
		if json.Unmarshal(b, &chunk) != nil || chunk == nil {
			return providerhttp.Broken(name)
		}
		if er := chunk["error"]; event == "error" || (len(er) > 0 && string(er) != "null") {
			if classify != nil {
				return classify(b)
			}
			return providerhttp.Broken(name)
		}
		var choices []struct {
			Index  int                        `json:"index"`
			Delta  map[string]json.RawMessage `json:"delta"`
			Finish *string                    `json:"finish_reason"`
		}
		if json.Unmarshal(chunk["choices"], &choices) != nil || choices == nil {
			return providerhttp.Broken(name)
		}
		if len(choices) == 0 {
			if !seen || len(chunk["usage"]) == 0 || string(chunk["usage"]) == "null" {
				return providerhttp.Broken(name)
			}
		} else {
			if len(choices) != 1 || choices[0].Index != 0 || finished {
				return providerhttp.Broken(name)
			}
			ch := choices[0]
			if ch.Delta == nil && (ch.Finish == nil || *ch.Finish == "") {
				return providerhttp.Broken(name)
			}
			// Role-only chunks followed by DONE are not a generation result.
			for _, k := range []string{"content", "reasoning_content", "reasoning", "reasoning_details", "refusal", "tool_calls"} {
				if v := ch.Delta[k]; len(v) > 0 && string(v) != "null" && string(v) != `""` && string(v) != "[]" {
					seen = true
				}
			}
			if ch.Finish != nil && *ch.Finish != "" {
				finished = true
			}
		}
		return emit(b)
	})
}
