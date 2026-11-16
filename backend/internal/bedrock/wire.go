// SPDX-License-Identifier: LGPL-3.0-only
// Adapted native wire rules from Sub2API; see UPSTREAM.md.
package bedrock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"codearts2api/internal/messagewire"
	"codearts2api/internal/provider"
)

var regionPattern = regexp.MustCompile(`^(us|eu|ap|ca|sa|me|af|il|mx)-(central|north|south|east|west|northeast|southeast)-[1-9][0-9]?$`)

// InvokeURL keeps the supplied model/profile ID unchanged; no implicit regional
// profile substitution or arbitrary host is accepted in this first batch.
func InvokeURL(region, model string) (string, error) {
	if !regionPattern.MatchString(region) {
		return "", fmt.Errorf("unsupported AWS commercial region")
	}
	if model == "" || len(model) > 2048 || strings.TrimSpace(model) != model || strings.ContainsAny(model, "\r\n\x00?#") {
		return "", fmt.Errorf("invalid Bedrock model ID")
	}
	encoded := strings.ReplaceAll(url.PathEscape(model), ":", "%3A")
	return "https://bedrock-runtime." + region + ".amazonaws.com/model/" + encoded + "/invoke-with-response-stream", nil
}

func Encode(r provider.Request) ([]byte, error) {
	encoded, err := messagewire.Encode(r, "Bedrock")
	if err != nil {
		return nil, err
	}
	obj, ok := encoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid Messages conversion")
	}
	delete(obj, "model")
	delete(obj, "stream")
	obj["anthropic_version"] = "bedrock-2023-05-31"
	return json.Marshal(obj)
}

// Decode converts one bounded AWS frame at a time into the existing Messages
// parser. No goroutine, growing full-response buffer, or second stream parser.
func Decode(ctx context.Context, body io.Reader, model string, emit func([]byte) error) error {
	return messagewire.Decode(ctx, &eventReader{ctx: ctx, decoder: newBedrockEventStreamDecoder(body)}, model, "Bedrock", emit)
}

type eventReader struct {
	ctx     context.Context
	decoder *bedrockEventStreamDecoder
	pending *bytes.Reader
}

func (r *eventReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.pending != nil && r.pending.Len() > 0 {
		return r.pending.Read(p)
	}
	frame, err := r.decoder.Decode()
	if err != nil {
		return 0, err
	}
	// []byte uses the standard JSON base64 decoder, rejecting malformed envelopes.
	var envelope struct {
		Bytes []byte `json:"bytes"`
	}
	if json.Unmarshal(frame, &envelope) != nil || len(envelope.Bytes) == 0 {
		return 0, fmt.Errorf("invalid Bedrock chunk")
	}
	data, err := invocationMetrics(envelope.Bytes)
	if err != nil {
		return 0, err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return 0, err
	}
	event := append([]byte("data: "), compact.Bytes()...)
	event = append(event, '\n', '\n')
	r.pending = bytes.NewReader(event)
	return r.pending.Read(p)
}

func invocationMetrics(data []byte) ([]byte, error) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) != nil || obj == nil {
		return nil, fmt.Errorf("invalid Bedrock event")
	}
	metrics, exists := obj["amazon-bedrock-invocationMetrics"]
	if !exists {
		return data, nil
	}
	// Preserve existing standard usage exactly, as upstream does.
	if _, ok := obj["usage"]; !ok {
		var fields map[string]json.RawMessage
		if json.Unmarshal(metrics, &fields) != nil || fields == nil {
			return nil, fmt.Errorf("invalid Bedrock metrics")
		}
		usage := map[string]json.RawMessage{}
		for from, to := range map[string]string{"inputTokenCount": "input_tokens", "outputTokenCount": "output_tokens"} {
			if b, ok := fields[from]; ok {
				var n int64
				if json.Unmarshal(b, &n) != nil || n < 0 || n > 1<<48 {
					return nil, fmt.Errorf("invalid Bedrock token count")
				}
				usage[to] = b
			}
		}
		if len(usage) > 0 {
			b, _ := json.Marshal(usage)
			obj["usage"] = b
		}
	}
	delete(obj, "amazon-bedrock-invocationMetrics")
	return json.Marshal(obj)
}
