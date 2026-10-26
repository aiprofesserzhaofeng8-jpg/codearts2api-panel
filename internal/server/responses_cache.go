package server

import (
	apicompat "codearts2api/internal/responsescompat"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// The same reasoning-item lookup hook as Sub2API's fallback, scoped per API key.
// Bounded and memory-only: no conversations/credentials are written to disk.
const responsesReasoningTTL = 7 * 24 * time.Hour
const responsesReasoningMaxBytes = 16 << 20
const responsesReasoningMaxItems = 2048

type responsesReasoningEntry struct {
	text    string
	expires time.Time
}
type responsesReasoningCache struct {
	mu      sync.Mutex
	entries map[string]responsesReasoningEntry
	bytes   int
}

func responsesCacheScope(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
func (c *responsesReasoningCache) get(scope, id string, now time.Time) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := scope + ":" + id
	e, ok := c.entries[k]
	if !ok {
		return ""
	}
	if !e.expires.After(now) {
		delete(c.entries, k)
		c.bytes -= len(e.text)
		return ""
	}
	return e.text
}
func (c *responsesReasoningCache) put(scope, id, text string, now time.Time) {
	if id == "" || text == "" || len(text) > 1<<20 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]responsesReasoningEntry)
	}
	k := scope + ":" + id
	if old, ok := c.entries[k]; ok {
		c.bytes -= len(old.text)
		delete(c.entries, k)
	}
	for key, e := range c.entries {
		if !e.expires.After(now) {
			c.bytes -= len(e.text)
			delete(c.entries, key)
		}
	}
	for len(c.entries) >= responsesReasoningMaxItems || c.bytes+len(text) > responsesReasoningMaxBytes {
		var oldest string
		var expiry time.Time
		for key, e := range c.entries {
			if oldest == "" || e.expires.Before(expiry) {
				oldest, expiry = key, e.expires
			}
		}
		if oldest == "" {
			break
		}
		c.bytes -= len(c.entries[oldest].text)
		delete(c.entries, oldest)
	}
	c.entries[k] = responsesReasoningEntry{text, now.Add(responsesReasoningTTL)}
	c.bytes += len(text)
}
func (c *responsesReasoningCache) recacheInput(scope string, input json.RawMessage) {
	var items []json.RawMessage
	if json.Unmarshal(input, &items) != nil {
		return
	}
	for _, item := range items {
		if id, text, ok := apicompat.ExtractResponsesReasoningItem(item); ok {
			c.put(scope, id, text, time.Now())
		}
	}
}
func (c *responsesReasoningCache) rememberOutput(scope string, items []apicompat.ResponsesOutput) {
	for _, item := range items {
		if item.Type != "reasoning" {
			continue
		}
		var b strings.Builder
		for _, part := range item.Summary {
			b.WriteString(part.Text)
		}
		c.put(scope, item.ID, b.String(), time.Now())
	}
}
