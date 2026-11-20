package workbuddy

// Bounded task-only chat wire. Read the full SSE completion before reporting
// use; the reference's early ID return is insufficient proof of completion.
import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (c *Client) growthDesktopChat(ctx context.Context, raw json.RawMessage, expertID string, onChunk func([]byte) error) (string, string, bool, error) {
	normalized, _, err := c.Normalize(raw)
	if err != nil {
		return "", "", false, err
	}
	a, _ := decode(normalized)
	conv := "wb2api-conv-" + randomID()
	body, _ := json.Marshal(map[string]any{"model": "fast-model", "messages": []map[string]string{{"role": "system", "content": "You are a helpful assistant. 当前处于中文环境，使用简体中文回答。"}, {"role": "user", "content": "1+1等于几？直接回答。"}}, "agent": "cli", "temperature": 1, "max_tokens": 32, "stream": true, "stream_options": map[string]bool{"include_usage": true}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.CNBase+"/v2/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", "", false, err
	}
	c.growthDesktopHeaders(req, a)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-Conversation-ID", conv)
	req.Header.Set("X-Agent-Intent", "craft")
	req.Header.Set("X-Agent-Type", "main")
	req.Header.Set("X-IDE-Name", "WorkBuddy")
	req.Header.Set("X-IDE-Type", "WorkBuddy")
	req.Header.Set("X-IDE-Version", "5.5.6")
	req.Header.Set("X-CodeBuddy-Request", "1")
	if expertID != "" {
		req.Header.Set("X-Expert-Id", expertID)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", "", false, fmt.Errorf("成长模型请求失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 16384))
		return "", "", false, upstreamError(resp.StatusCode, body)
	}
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, (256<<10)+1))
	scanner.Buffer(make([]byte, 4096), 256<<10)
	var responseID string
	var data []string
	size := 0
	finished, content, done := false, false, false
	emit := func() error {
		if len(data) == 0 {
			return nil
		}
		b := []byte(strings.Join(data, "\n"))
		data = nil
		if bytes.Equal(b, []byte("[DONE]")) {
			done = true
			return nil
		}
		if err := upstreamError(200, b); err != nil {
			return err
		}
		var frame struct {
			ID      string `json:"id"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				Finish *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if json.Unmarshal(b, &frame) != nil {
			return fmt.Errorf("成长模型响应 JSON 无效")
		}
		if frame.ID != "" {
			if len(frame.ID) > 256 || responseID != "" && responseID != frame.ID {
				return fmt.Errorf("成长模型响应标识不一致")
			}
			responseID = frame.ID
		}
		for _, ch := range frame.Choices {
			content = content || strings.TrimSpace(ch.Delta.Content) != ""
			finished = finished || ch.Finish != nil && *ch.Finish != ""
		}
		return onChunk(b)
	}
	for scanner.Scan() {
		line := scanner.Text()
		size += len(line) + 1
		if size > 256<<10 {
			return "", "", false, fmt.Errorf("成长模型响应超过限制")
		}
		if line == "" {
			if err = emit(); err != nil {
				return "", "", false, err
			}
			if done {
				break
			}
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err = scanner.Err(); err != nil {
		return "", "", false, err
	}
	if err = emit(); err != nil {
		return "", "", false, err
	}
	if !finished {
		return "", "", false, fmt.Errorf("成长模型对话未完整结束，不提交使用事件")
	}
	if responseID == "" || !content {
		return "", "", true, fmt.Errorf("成长模型缺少有效内容或服务端标识，不提交使用事件")
	}
	return conv, responseID, true, nil
}
