package kiro

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"codearts2api/internal/provider"
)

// AWS EventStream validates both CRCs before allocating or exposing payloads.
// No resynchronization: a damaged stream must never be reported as completed.
func readFrame(r io.Reader) (map[string]string, []byte, error) {
	var pre [12]byte
	n, e := io.ReadFull(r, pre[:])
	if e == io.EOF && n == 0 {
		return nil, nil, io.EOF
	}
	if e != nil {
		return nil, nil, provider.NewTransportError("Kiro", e)
	}
	total, hlen := binary.BigEndian.Uint32(pre[:4]), binary.BigEndian.Uint32(pre[4:8])
	if crc32.ChecksumIEEE(pre[:8]) != binary.BigEndian.Uint32(pre[8:]) || total < 16 || total > 8<<20 || hlen > 128<<10 || hlen > total-16 {
		return nil, nil, broken()
	}
	data := make([]byte, total-12)
	if _, e = io.ReadFull(r, data); e != nil {
		return nil, nil, provider.NewTransportError("Kiro", e)
	}
	hash := crc32.NewIEEE()
	_, _ = hash.Write(pre[:])
	_, _ = hash.Write(data[:len(data)-4])
	if hash.Sum32() != binary.BigEndian.Uint32(data[len(data)-4:]) {
		return nil, nil, broken()
	}
	headers := map[string]string{}
	b := data[:hlen]
	for len(b) > 0 {
		size := int(b[0])
		b = b[1:]
		if size == 0 || len(b) < size+1 {
			return nil, nil, broken()
		}
		name := string(b[:size])
		typ := b[size]
		b = b[size+1:]
		length := 0
		switch typ {
		case 0, 1:
			length = 0
		case 2:
			length = 1
		case 3:
			length = 2
		case 4:
			length = 4
		case 5, 8:
			length = 8
		case 9:
			length = 16
		case 6, 7:
			if len(b) < 2 {
				return nil, nil, broken()
			}
			length = int(binary.BigEndian.Uint16(b[:2]))
			b = b[2:]
		default:
			return nil, nil, broken()
		}
		if len(b) < length {
			return nil, nil, broken()
		}
		if typ == 7 {
			if !utf8.Valid(b[:length]) {
				return nil, nil, broken()
			}
			if _, ok := headers[name]; ok {
				return nil, nil, broken()
			}
			headers[name] = string(b[:length])
		}
		b = b[length:]
	}
	return headers, data[hlen : len(data)-4], nil
}

type streamTool struct {
	index   int
	name    string
	input   strings.Builder
	stopped bool
}

func tokenValue(o map[string]json.RawMessage, keys ...string) (int64, bool) {
	for _, key := range keys {
		if b := o[key]; len(b) > 0 {
			v, e := strconv.ParseInt(string(b), 10, 64)
			return v, e == nil && v >= 0 && v < 1<<50
		}
	}
	return 0, false
}
func convertStream(ctx context.Context, r io.Reader, model string, emit func([]byte) error) error {
	id, e := uuid()
	if e != nil {
		return e
	}
	created := time.Now().Unix()
	started, observed := false, false
	tools := map[string]*streamTool{}
	toolBytes := 0
	var usage map[string]any
	send := func(delta any, finish any) error {
		b, e := json.Marshal(map[string]any{"id": "chatcmpl-" + id, "object": "chat.completion.chunk", "created": created, "model": model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
		if e != nil {
			return e
		}
		return emit(b)
	}
	start := func() error {
		if !started {
			started = true
			return send(map[string]string{"role": "assistant"}, nil)
		}
		return nil
	}
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		h, p, e := readFrame(r)
		if e == io.EOF {
			break
		}
		if e != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return e
		}
		if h[":message-type"] == "exception" || h[":message-type"] == "error" {
			code := h[":exception-type"]
			if code == "" {
				code = h[":error-code"]
			}
			status := 502
			fault := false
			switch code {
			case "throttlingException", "ThrottlingException":
				status = 429
			case "accessDeniedException", "AccessDeniedException":
				status = 403
			case "validationException", "ValidationException":
				status = 400
			case "UnauthorizedException":
				status = 401
				fault = true
			}
			return &provider.Error{Status: status, AccountFault: fault, Code: code, Message: "Kiro 上游流返回异常"}
		}
		if h[":message-type"] != "event" {
			return broken()
		}
		var o map[string]json.RawMessage
		if !utf8.Valid(p) || json.Unmarshal(p, &o) != nil || o == nil {
			return broken()
		}
		switch h[":event-type"] {
		case "assistantResponseEvent":
			delta := map[string]any{}
			for _, key := range []string{"content", "refusal", "reasoning_content"} {
				if b, ok := o[key]; ok {
					var s string
					if json.Unmarshal(b, &s) != nil {
						return broken()
					}
					delta[key] = s
					if s != "" {
						observed = true
					}
				}
			}
			if len(delta) > 0 {
				if e = start(); e != nil {
					return e
				}
				if e = send(delta, nil); e != nil {
					return e
				}
			}
		case "reasoningContentEvent":
			text := field(o, "text", "content")
			if text != "" {
				observed = true
				if e = start(); e != nil {
					return e
				}
				if e = send(map[string]string{"reasoning_content": text}, nil); e != nil {
					return e
				}
			}
		case "toolUseEvent":
			tid, name := field(o, "toolUseId"), field(o, "name")
			if tid == "" || len(tid) > 256 {
				return broken()
			}
			tool := tools[tid]
			fresh := tool == nil
			if fresh {
				if name == "" || len(name) > 256 || len(tools) >= 128 {
					return broken()
				}
				tool = &streamTool{index: len(tools), name: name}
				tools[tid] = tool
			} else if tool.stopped || name != "" && name != tool.name {
				return broken()
			}
			fragment := ""
			if b := o["input"]; len(b) > 0 && json.Unmarshal(b, &fragment) != nil {
				return broken()
			}
			if tool.input.Len()+len(fragment) > 4<<20 || toolBytes+len(fragment) > 8<<20 {
				return broken()
			}
			toolBytes += len(fragment)
			tool.input.WriteString(fragment)
			fn := map[string]any{"arguments": fragment}
			call := map[string]any{"index": tool.index, "function": fn}
			if fresh {
				call["id"] = tid
				call["type"] = "function"
				fn["name"] = name
			}
			var stopped bool
			if b := o["stop"]; len(b) > 0 && json.Unmarshal(b, &stopped) != nil {
				return broken()
			}
			if stopped {
				var obj map[string]json.RawMessage
				if json.Unmarshal([]byte(tool.input.String()), &obj) != nil || obj == nil {
					return broken()
				}
				tool.stopped = true
			}
			observed = true
			if e = start(); e != nil {
				return e
			}
			if e = send(map[string]any{"tool_calls": []any{call}}, nil); e != nil {
				return e
			}
		case "meteringEvent":
			input, iok := tokenValue(o, "input_tokens", "inputTokens")
			output, ook := tokenValue(o, "output_tokens", "outputTokens")
			if iok && ook {
				usage = map[string]any{"prompt_tokens": input, "completion_tokens": output, "total_tokens": input + output}
				if n, ok := tokenValue(o, "cache_read_input_tokens", "cacheReadInputTokens"); ok {
					usage["prompt_tokens_details"] = map[string]int64{"cached_tokens": n}
				}
			}
		// Kiro's context percentage / credits are not token counts. Never fabricate
		// usage from these informational events or translate credits into USD.
		case "messageMetadataEvent", "metadataEvent", "contextUsageEvent", "codeReferenceEvent", "followupPromptEvent":
		default:
		}
	}
	if !observed {
		return broken()
	}
	finish := "stop"
	if len(tools) > 0 {
		finish = "tool_calls"
		for _, tool := range tools {
			if !tool.stopped {
				return broken()
			}
		}
	}
	if usage != nil {
		b, e := json.Marshal(map[string]any{"id": "chatcmpl-" + id, "object": "chat.completion.chunk", "created": created, "model": model, "choices": []any{}, "usage": usage})
		if e != nil {
			return e
		}
		if e = emit(b); e != nil {
			return e
		}
	}
	return send(map[string]any{}, finish)
}
