package protocol

import (
	"encoding/json"
	"fmt"
)

// MappedRequestModel only rewrites the top-level model of an already validated
// request. Raw values preserve unknown fields, exact numbers and tool JSON.
func MappedRequestModel(body []byte, name string) ([]byte, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, err
	}
	obj["model"], _ = json.Marshal(name)
	return json.Marshal(obj)
}

// MappedResponseModel preserves the existing object-envelope validation. Normal
// forwarding does not call this function or parse the upstream body again.
func MappedResponseModel(raw []byte, name string) ([]byte, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, fmt.Errorf("上游响应不是 JSON 对象")
	}
	obj["model"], _ = json.Marshal(name)
	return json.Marshal(obj)
}
