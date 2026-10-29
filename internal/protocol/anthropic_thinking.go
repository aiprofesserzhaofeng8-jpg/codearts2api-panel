package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// The native CodeArts/WorkBuddy switch differs from OpenAI reasoning_effort.
// Only an explicit disable has a supported mapping today. Do not guess an
// effort from Anthropic's token budget or inject a new default for old clients.
func applyAnthropicThinking(req *Request, out map[string]any) error {
	if len(req.OutputConfig) > 0 && !bytes.Equal(bytes.TrimSpace(req.OutputConfig), []byte("null")) {
		var config map[string]json.RawMessage
		if err := json.Unmarshal(req.OutputConfig, &config); err != nil || config == nil {
			return fmt.Errorf("output_config 必须为对象")
		}
		if _, exists := config["effort"]; exists {
			return fmt.Errorf("当前网关暂不支持 Messages output_config.effort 映射，请省略该参数")
		}
	}
	if len(req.Thinking) == 0 || bytes.Equal(bytes.TrimSpace(req.Thinking), []byte("null")) {
		return nil
	}
	var thinking map[string]json.RawMessage
	if err := json.Unmarshal(req.Thinking, &thinking); err != nil || thinking == nil {
		return fmt.Errorf("thinking 必须为对象")
	}
	if _, exists := thinking["budget_tokens"]; exists {
		return fmt.Errorf("当前网关暂不支持 Messages thinking.budget_tokens 映射，请省略该参数")
	}
	var typ string
	if err := json.Unmarshal(thinking["type"], &typ); err != nil || typ == "" {
		return fmt.Errorf("thinking.type 必须为字符串")
	}
	if typ != "disabled" {
		return fmt.Errorf("当前网关暂不支持此 Messages thinking.type 映射，仅支持 disabled 或省略 thinking")
	}
	if len(thinking) != 1 {
		return fmt.Errorf("当前网关暂不支持 thinking.type 之外的思考选项")
	}
	out["thinking"] = map[string]string{"type": "disabled"}
	return nil
}
