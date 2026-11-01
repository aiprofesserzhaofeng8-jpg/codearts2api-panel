// Search Messages protocol adapted from workbuddy2api-panel (MIT; LICENSE.workbuddy).
package protocol

import (
	"encoding/json"
	"strings"
)

const SearchQueryPrefix = "Perform a web search for the query:"

type SearchMessagesRequest struct {
	Model          string `json:"model"`
	Stream         bool   `json:"stream"`
	MaxResults     int    `json:"max_results"`
	ConversationID string `json:"conversation_id"`
	Tools          []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"tools"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
}

func SearchQuery(req SearchMessagesRequest) string {
	query := ""
	for _, m := range req.Messages {
		if m.Role != "user" {
			continue
		}
		var plain string
		if json.Unmarshal(m.Content, &plain) != nil {
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(m.Content, &blocks) != nil {
				continue
			}
			parts := []string{}
			for _, b := range blocks {
				if b.Type == "text" {
					parts = append(parts, b.Text)
				}
			}
			plain = strings.Join(parts, "\n")
		}
		if strings.TrimSpace(plain) != "" {
			query = strings.TrimSpace(plain)
		}
	}
	if len(query) >= len(SearchQueryPrefix) && strings.EqualFold(query[:len(SearchQueryPrefix)], SearchQueryPrefix) {
		query = strings.TrimSpace(query[len(SearchQueryPrefix):])
	}
	return query
}
