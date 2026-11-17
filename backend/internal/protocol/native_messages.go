package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MessagesCollector aggregates native events only for a buffered client. Raw
// block fields, signatures and exact tool JSON never pass through Chat types.
type MessagesCollector struct {
	message map[string]json.RawMessage
	blocks  []map[string]json.RawMessage
	args    map[int]*strings.Builder
	size    int
	done    bool
}

func (c *MessagesCollector) Consume(raw []byte) error {
	c.size += len(raw)
	if c.size > 32<<20 {
		return fmt.Errorf("upstream response exceeds 32 MiB")
	}
	var e struct {
		Type    string
		Index   int
		Message json.RawMessage
		Block   json.RawMessage `json:"content_block"`
		Delta   map[string]json.RawMessage
		Usage   json.RawMessage
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		return err
	}
	switch e.Type {
	case "message_start":
		if c.message != nil {
			return fmt.Errorf("duplicate message_start")
		}
		if err := json.Unmarshal(e.Message, &c.message); err != nil {
			return err
		}
		c.args = map[int]*strings.Builder{}
	case "content_block_start":
		if c.message == nil || e.Index != len(c.blocks) {
			return fmt.Errorf("invalid content block index")
		}
		var block map[string]json.RawMessage
		if json.Unmarshal(e.Block, &block) != nil || block == nil {
			return fmt.Errorf("invalid content block")
		}
		c.blocks = append(c.blocks, block)
	case "content_block_delta":
		if e.Index < 0 || e.Index >= len(c.blocks) {
			return fmt.Errorf("invalid content block index")
		}
		block := c.blocks[e.Index]
		var typ string
		_ = json.Unmarshal(e.Delta["type"], &typ)
		if typ == "input_json_delta" {
			var text string
			if json.Unmarshal(e.Delta["partial_json"], &text) != nil {
				return fmt.Errorf("invalid tool delta")
			}
			if c.args[e.Index] == nil {
				c.args[e.Index] = &strings.Builder{}
			}
			c.args[e.Index].WriteString(text)
			break
		}
		field := ""
		switch typ {
		case "text_delta":
			field = "text"
		case "thinking_delta":
			field = "thinking"
		case "signature_delta":
			field = "signature"
		default:
			return fmt.Errorf("unsupported buffered Messages delta %q", typ)
		}
		var prior, delta string
		_ = json.Unmarshal(block[field], &prior)
		if json.Unmarshal(e.Delta[field], &delta) != nil {
			return fmt.Errorf("invalid content delta")
		}
		block[field], _ = json.Marshal(prior + delta)
	case "message_delta":
		if c.message == nil {
			return fmt.Errorf("missing message_start")
		}
		for k, v := range e.Delta {
			c.message[k] = v
		}
		var usage, delta map[string]json.RawMessage
		_ = json.Unmarshal(c.message["usage"], &usage)
		_ = json.Unmarshal(e.Usage, &delta)
		if usage == nil {
			usage = map[string]json.RawMessage{}
		}
		for k, v := range delta {
			usage[k] = v
		}
		c.message["usage"], _ = json.Marshal(usage)
	case "message_stop":
		c.done = true
	}
	return nil
}
func (c *MessagesCollector) Result() ([]byte, error) {
	if !c.done || c.message == nil {
		return nil, fmt.Errorf("Messages stream incomplete")
	}
	for index, args := range c.args {
		if !json.Valid([]byte(args.String())) {
			return nil, fmt.Errorf("工具参数不是完整 JSON")
		}
		c.blocks[index]["input"] = json.RawMessage(args.String())
	}
	c.message["content"], _ = json.Marshal(c.blocks)
	return json.Marshal(c.message)
}
