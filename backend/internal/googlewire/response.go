// SPDX-License-Identifier: LGPL-3.0-only
// Shared native wire conversion extracted from internal/gemini; see UPSTREAM.md.
package googlewire

import (
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func Decode(ctx context.Context, reader io.Reader, name, model string, emit func([]byte) error) error {
	chunks := providerhttp.Chunks{Model: model, Emit: emit}
	seen := false
	toolIndex := 0
	return providerhttp.SSE(ctx, reader, name, func(event string, data []byte) error {
		var envelope map[string]json.RawMessage
		if json.Unmarshal(data, &envelope) != nil || envelope == nil {
			return providerhttp.Broken(name)
		}
		if v := envelope["error"]; event == "error" || len(v) > 0 && string(v) != "null" {
			return providerhttp.Broken(name)
		}
		native := data
		if v := envelope["response"]; len(v) > 0 {
			native = v
		}
		var result struct {
			Error    json.RawMessage `json:"error"`
			Feedback struct {
				Block string `json:"blockReason"`
			} `json:"promptFeedback"`
			Candidates []struct {
				Index   int    `json:"index"`
				Finish  string `json:"finishReason"`
				Content struct {
					Parts []struct {
						Text      string          `json:"text"`
						Thought   bool            `json:"thought"`
						Signature string          `json:"thoughtSignature"`
						Inline    json.RawMessage `json:"inlineData"`
						Call      *struct {
							Name string          `json:"name"`
							Args json.RawMessage `json:"args"`
						} `json:"functionCall"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
			Usage *struct {
				Input   *int64 `json:"promptTokenCount"`
				Output  *int64 `json:"candidatesTokenCount"`
				Cached  int64  `json:"cachedContentTokenCount"`
				Thought int64  `json:"thoughtsTokenCount"`
			} `json:"usageMetadata"`
		}
		if json.Unmarshal(native, &result) != nil || len(result.Error) > 0 && string(result.Error) != "null" {
			return providerhttp.Broken(name)
		}
		if result.Feedback.Block != "" {
			return &provider.Error{Status: 400, ContentBlocked: true, Message: name + " 拦截了请求内容"}
		}
		if len(result.Candidates) != 1 || result.Candidates[0].Index != 0 {
			return providerhttp.Broken(name)
		}
		candidate := result.Candidates[0]
		switch candidate.Finish {
		case "", "STOP", "MAX_TOKENS":
		case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
			return &provider.Error{Status: 400, ContentBlocked: true, Message: name + " 拦截了响应内容"}
		default:
			return providerhttp.Broken(name)
		}
		if err := chunks.Start(); err != nil {
			return err
		}
		for _, p := range candidate.Content.Parts {
			if len(p.Inline) > 0 {
				return providerhttp.Invalid(name, "CLI 生成的媒体输出尚未接入")
			}
			if p.Text != "" {
				key := "content"
				if p.Thought {
					key = "reasoning_content"
				}
				if err := chunks.Send(map[string]string{key: p.Text}, nil); err != nil {
					return err
				}
				seen = true
			}
			if p.Call != nil {
				if len(p.Call.Args) == 0 {
					p.Call.Args = json.RawMessage(`{}`)
				}
				if p.Call.Name == "" || !json.Valid(p.Call.Args) || !strings.HasPrefix(strings.TrimSpace(string(p.Call.Args)), "{") {
					return providerhttp.Broken(name)
				}
				call := map[string]any{"index": toolIndex, "id": fmt.Sprintf("%s-tool-%d", chunks.ID, toolIndex), "type": "function", "function": map[string]string{"name": p.Call.Name, "arguments": string(p.Call.Args)}}
				if p.Signature != "" {
					call["extra_content"] = map[string]any{"google": map[string]string{"thought_signature": p.Signature}}
				}
				if err := chunks.Send(map[string]any{"tool_calls": []any{call}}, nil); err != nil {
					return err
				}
				toolIndex++
				seen = true
			}
		}
		if u := result.Usage; u != nil && u.Input != nil && u.Output != nil {
			if *u.Input < 0 || *u.Output < 0 || u.Cached < 0 || u.Thought < 0 || *u.Input > 1<<50 || *u.Output > 1<<50 || u.Cached > *u.Input || u.Thought > 1<<50 {
				return providerhttp.Broken(name)
			}
			output := *u.Output + u.Thought
			usage := map[string]any{"prompt_tokens": *u.Input, "completion_tokens": output, "total_tokens": *u.Input + output, "prompt_tokens_details": map[string]int64{"cached_tokens": u.Cached}, "completion_tokens_details": map[string]int64{"reasoning_tokens": u.Thought}}
			b, _ := json.Marshal(map[string]any{"id": chunks.ID, "object": "chat.completion.chunk", "model": model, "choices": []any{}, "usage": usage})
			if err := emit(b); err != nil {
				return err
			}
		}
		if candidate.Finish != "" {
			if !seen {
				return providerhttp.Broken(name)
			}
			finish := "stop"
			switch candidate.Finish {
			case "STOP":
				if toolIndex > 0 {
					finish = "tool_calls"
				}
			case "MAX_TOKENS":
				finish = "length"
			case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
				return &provider.Error{Status: 400, ContentBlocked: true, Message: name + " 拦截了响应内容"}
			default:
				return providerhttp.Broken(name)
			}
			if err := chunks.Send(map[string]any{}, finish); err != nil {
				return err
			}
			return providerhttp.End
		}
		return nil
	})
}
