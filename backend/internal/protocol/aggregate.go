package protocol

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// AggregateSSE 把上游的 OpenAI SSE 分片聚合为一个非流式 chat.completion 响应。
//
// 用途：上游对个别模型（实测 glm-5.3）的非流式接口会返回一个**空 body 的 400**，
// 只有流式可用。网关检测到这种情况后自动改用流式请求，再用本函数把分片拼回
// 一次性响应，对客户端保持「非流式」语义。
func AggregateSSE(chunks [][]byte) ([]byte, error) {
	if len(chunks) == 0 {
		return nil, fmt.Errorf("上游没有返回任何分片")
	}

	type toolCall struct {
		ID        string
		Type      string
		Name      string
		Arguments strings.Builder
	}

	// Choice and tool indices are independent namespaces. Keep each candidate's
	// text, refusal, reasoning, tool arguments and finish reason separate.
	type choiceState struct {
		content, think, refusal, signature strings.Builder
		nativeThinking                     []json.RawMessage
		hasRefusal                         bool
		finish                             string
		calls                              map[int]*toolCall
		order                              []int
	}
	var (
		id          string
		model       string
		created     int64
		usage       json.RawMessage
		choices     = map[int]*choiceState{}
		choiceOrder []int
	)

	for _, raw := range chunks {
		var c struct {
			Error   json.RawMessage `json:"error"`
			ID      string          `json:"id"`
			Object  string          `json:"object"`
			Created int64           `json:"created"`
			Model   string          `json:"model"`
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Refusal            *string           `json:"refusal"`
					Content            string            `json:"content"`
					ReasoningContent   string            `json:"reasoning_content"`
					ReasoningSignature string            `json:"reasoning_signature"`
					NativeThinking     []json.RawMessage `json:"anthropic_thinking_blocks"`
					ToolCalls          []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage json.RawMessage `json:"usage"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("上游流包含无效 JSON 分片，无法生成完整回复")
		}
		if len(c.Error) > 0 && string(c.Error) != "null" {
			return nil, fmt.Errorf("上游流包含错误，无法生成成功回复")
		}
		if id == "" {
			id = c.ID
		}
		if model == "" {
			model = c.Model
		}
		if created == 0 {
			created = c.Created
		}
		for _, ch := range c.Choices {
			state := choices[ch.Index]
			if state == nil {
				state = &choiceState{calls: map[int]*toolCall{}}
				choices[ch.Index] = state
				choiceOrder = append(choiceOrder, ch.Index)
			}
			state.content.WriteString(ch.Delta.Content)
			state.think.WriteString(ch.Delta.ReasoningContent)
			state.signature.WriteString(ch.Delta.ReasoningSignature)
			state.nativeThinking = append(state.nativeThinking, ch.Delta.NativeThinking...)
			if ch.Delta.Refusal != nil {
				state.hasRefusal = true
				state.refusal.WriteString(*ch.Delta.Refusal)
			}
			for _, tc := range ch.Delta.ToolCalls {
				call, ok := state.calls[tc.Index]
				if !ok {
					call = &toolCall{}
					state.calls[tc.Index] = call
					state.order = append(state.order, tc.Index)
				}
				if tc.ID != "" {
					call.ID = tc.ID
				}
				if tc.Type != "" {
					call.Type = tc.Type
				}
				if tc.Function.Name != "" {
					call.Name = tc.Function.Name
				}
				call.Arguments.WriteString(tc.Function.Arguments)
			}
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				state.finish = *ch.FinishReason
			}
		}
		if len(c.Usage) > 0 && string(c.Usage) != "null" {
			usage = c.Usage
		}
	}

	sort.Ints(choiceOrder)
	if len(choiceOrder) == 0 {
		return nil, fmt.Errorf("上游没有返回有效候选回复")
	}
	outputChoices := make([]any, 0, len(choiceOrder))
	for _, index := range choiceOrder {
		state := choices[index]

		msg := map[string]any{"role": "assistant", "content": state.content.String()}
		if state.hasRefusal {
			msg["refusal"] = state.refusal.String()
		}
		if state.think.Len() > 0 {
			msg["reasoning_content"] = state.think.String()
			if state.signature.Len() > 0 {
				msg["reasoning_signature"] = state.signature.String()
			}
		}
		if len(state.nativeThinking) > 0 {
			msg["anthropic_thinking_blocks"] = state.nativeThinking
		}
		if len(state.order) > 0 {
			sort.Ints(state.order)
			list := make([]map[string]any, 0, len(state.order))
			for _, idx := range state.order {
				call := state.calls[idx]
				callID := call.ID
				if callID == "" {
					callID = "call_" + randomHex(12)
				}
				typ := call.Type
				if typ == "" {
					typ = "function"
				}
				list = append(list, map[string]any{
					"index": idx,
					"id":    callID,
					"type":  typ,
					"function": map[string]any{
						"name":      call.Name,
						"arguments": call.Arguments.String(),
					},
				})
			}
			msg["tool_calls"] = list
			if state.finish == "" {
				state.finish = "tool_calls"
			}
		}
		if state.finish == "" {
			state.finish = "stop"
		}
		outputChoices = append(outputChoices, map[string]any{"index": index, "message": msg, "finish_reason": state.finish})
	}

	if id == "" {
		id = "chatcmpl-" + randomHex(12)
	}

	resp := map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": outputChoices,
	}
	if len(usage) > 0 {
		resp["usage"] = usage
	}
	return json.Marshal(resp)
}
