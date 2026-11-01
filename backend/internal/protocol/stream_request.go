package protocol

import "encoding/json"

// RewriteStream 只改写请求体里的 stream 字段。
func RewriteStream(body []byte, stream bool) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	sb, err := json.Marshal(stream)
	if err != nil {
		return nil, err
	}
	m["stream"] = sb
	return json.Marshal(m)
}

// Ask the upstream for the final usage frame while preserving other options.
func RequestStreamUsage(body []byte) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, nil
	}
	var options map[string]json.RawMessage
	if raw := m["stream_options"]; len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &options); err != nil {
			return nil, err
		}
	}
	if options == nil {
		options = make(map[string]json.RawMessage)
	}
	options["include_usage"] = json.RawMessage("true")
	m["stream_options"], _ = json.Marshal(options)
	return json.Marshal(m)
}
