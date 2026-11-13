package server

import (
	"encoding/json"
	"errors"
)

// rejectUnsupportedFiles checks message content only. It does not read, decode,
// fetch or store attachments, or interpret tool arguments and literal strings.
// Existing protocol decoders remain responsible for malformed JSON.
func rejectUnsupportedFiles(body []byte) error {
	var root map[string]json.RawMessage
	if json.Unmarshal(body, &root) != nil {
		return nil
	}
	var visit func(json.RawMessage, int) error
	visit = func(raw json.RawMessage, depth int) error {
		if depth > 64 {
			return errors.New("消息内容嵌套过深")
		}
		var list []json.RawMessage
		if json.Unmarshal(raw, &list) == nil {
			for _, item := range list {
				if err := visit(item, depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			return nil
		}
		var typ string
		_ = json.Unmarshal(obj["type"], &typ)
		switch typ {
		case "file", "input_file", "document", "file_id", "image_file":
			return errors.New("网关不支持文件上传、文件引用或文档解析；请由客户端读取为文本，图片请直接随消息发送")
		case "tool_use", "function_call", "custom_tool_call":
			return nil // Tool payloads are opaque, even if they contain file-like fields.
		}
		if id := obj["file_id"]; len(id) > 0 && string(id) != "null" && string(id) != `""` {
			return errors.New("网关不支持 file_id 文件引用；请直接发送文本或图片")
		}
		for _, field := range []string{"content", "output"} {
			if value, ok := obj[field]; ok {
				if err := visit(value, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, field := range []string{"messages", "input", "system"} {
		if value, ok := root[field]; ok {
			if err := visit(value, 0); err != nil {
				return err
			}
		}
	}
	return nil
}
