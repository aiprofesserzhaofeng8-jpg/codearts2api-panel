package qoder

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"codearts2api/internal/provider"
)

func (c *Client) Models(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	a, err := decode(raw)
	if err != nil {
		return nil, err
	}
	resp, err := c.signedRequest(ctx, a, http.MethodGet, ModelsPath, "", []byte(`{}`))
	if err != nil {
		return nil, err
	}
	b, err := readJSON(resp)
	if err != nil {
		return nil, err
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(b, &obj) != nil {
		return nil, failure(502)
	}
	if data := obj["data"]; len(data) > 0 {
		if json.Unmarshal(data, &obj) != nil {
			return nil, failure(502)
		}
	}
	var rows []map[string]json.RawMessage
	if b := obj["chat"]; len(b) == 0 || string(b) == "null" || json.Unmarshal(b, &rows) != nil {
		return nil, failure(502)
	}
	models := make(map[string]provider.Model, len(rows))
	for _, row := range rows {
		key := stringField(row, "key", "model_key")
		if key == "" || len(key) > 200 || strings.ContainsAny(key, "/\r\n\x00") {
			continue
		}
		// Later entries supersede earlier entries, including explicit disablement.
		delete(models, key)
		if v := row["enable"]; string(v) == "false" || string(v) == "0" {
			continue
		}
		if source := stringField(row, "source"); source != "" && source != "system" {
			continue // Do not expose third-party BYOK secrets or unsupported routes.
		}
		m := provider.Model{ID: key, Name: stringField(row, "display_name", "name"), Provider: provider.Qoder, Scope: a.Scope, UpstreamID: key, Source: "Qoder", StreamOnly: true}
		if m.Name == "" {
			m.Name = key
		}
		_ = json.Unmarshal(row["max_input_tokens"], &m.Context)
		_ = json.Unmarshal(row["max_output_tokens"], &m.MaxOutput)
		if m.Context < 0 || m.MaxOutput < 0 {
			continue
		}
		models[key] = m
	}
	out := make([]provider.Model, 0, len(models))
	for _, m := range models {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
