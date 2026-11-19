package accounts

import (
	"codearts2api/internal/provider"
	"time"
)

// ModelObservation contains only fields reported by the selected account's catalog.
type ModelObservation struct {
	NonStreaming bool                `json:"non_streaming,omitempty"`
	Credit       string              `json:"credit,omitempty"`
	ImageInput   string              `json:"image_input,omitempty"`
	ToolCalling  string              `json:"tool_calling,omitempty"`
	Protocols    []provider.Protocol `json:"supported_protocols,omitempty"`
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Context      int                 `json:"context"`
	MaxOutput    int                 `json:"max_output"`
}
type ModelList struct {
	Kind       string             `json:"kind,omitempty"` // Empty in older live Qoder/ChatGPT snapshots.
	AccountID  string             `json:"account_id"`
	Provider   string             `json:"provider"`
	Scope      string             `json:"scope"`
	ObservedAt time.Time          `json:"observed_at"`
	Models     []ModelObservation `json:"models"`
}
type ModelDelta struct {
	Added    []string `json:"added"`
	Missing  []string `json:"missing"`
	Retained []string `json:"retained"`
}
type ModelSnapshot struct {
	Published map[string]ModelObservation `json:"published,omitempty"`
	List      ModelList                   `json:"list"`
	First     bool                        `json:"first"`
	Delta     ModelDelta                  `json:"delta"`
}
