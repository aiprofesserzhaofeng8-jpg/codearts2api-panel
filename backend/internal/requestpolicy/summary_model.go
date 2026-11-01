package requestpolicy

import "encoding/json"

// PrepareSummaryModel changes only the protocol envelope of an authorized
// attempt. A different model must not inherit the original model's reasoning
// controls; its own verified summary options are applied afterwards.
func PrepareSummaryModel(body []byte, model string, options SummaryOptions, retry, changed bool) ([]byte, error) {
	var request map[string]json.RawMessage
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	request["model"], _ = json.Marshal(model)
	request["stream"] = json.RawMessage("true")
	if changed {
		delete(request, "thinking")
		delete(request, "reasoning_effort")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	return PrepareSummary(raw, options, retry)
}
