// Package provider defines the upstream boundary. Protocol conversion, accounts,
// scheduling, pricing and UI remain gateway responsibilities.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

const CodeArts = "codearts"
const WorkBuddy = "workbuddy"
const Trae = "trae"

type Model struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Context    int    `json:"context"`
	MaxOutput  int    `json:"max_output"`
	Desc       string `json:"description"`
	Source     string `json:"source"`
	Credit     string `json:"credit,omitempty"`
	StreamOnly bool   `json:"stream_only,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Scope      string `json:"scope,omitempty"`
	UpstreamID string `json:"upstream_id,omitempty"`
}
type Identity struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Scope      string `json:"scope"`
	ExpiresAt  int64  `json:"expires_at"`
	HasRefresh bool   `json:"has_refresh"`
}
type Descriptor struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Available bool     `json:"available"`
	Scopes    []string `json:"scopes"`
	Login     bool     `json:"browser_login"`
	Reason    string   `json:"reason,omitempty"`
}
type Request struct {
	Model     string
	Body      []byte
	SessionID string
	RequestID string
}
type Login struct {
	State string
	URL   string
}
type Adapter interface {
	Describe() Descriptor
	Normalize(json.RawMessage) (json.RawMessage, Identity, error)
	Refresh(context.Context, json.RawMessage) (json.RawMessage, error)
	Stream(context.Context, json.RawMessage, Request, func([]byte) error) error
	Models(context.Context, json.RawMessage) ([]Model, error)
	Credit(context.Context, json.RawMessage) (json.RawMessage, error)
}
type BrowserLogin interface {
	BeginLogin(context.Context, string) (Login, error)
	CheckLogin(context.Context, string, string) (json.RawMessage, bool, error)
}

// DeviceBrowserLogin binds authorization to device identifiers supplied by the user.
type DeviceBrowserLogin interface {
	BeginLoginWithDevice(context.Context, string, string, string) (Login, error)
}

// CallbackLogin supports providers that return credentials to a browser's
// loopback redirect instead of a remotely pollable device state.
type LoginReleaser interface{ ReleaseLogin(string) }
type CallbackLogin interface {
	CheckCallback(context.Context, string, string, string) (json.RawMessage, bool, error)
}
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

func NewRegistry() *Registry { return &Registry{adapters: map[string]Adapter{}} }
func (r *Registry) Register(a Adapter) error {
	d := a.Describe()
	if d.ID == "" || strings.Contains(d.ID, "/") {
		return fmt.Errorf("invalid provider id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.adapters[d.ID]; ok {
		return fmt.Errorf("provider already registered")
	}
	r.adapters[d.ID] = a
	return nil
}
func (r *Registry) Get(id string) (Adapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[id]
	return a, ok
}

// Unqualified model IDs retain the legacy CodeArts meaning. Explicit prefixes
// can never fall back to another upstream when resolution fails.
func ParseModel(id string) (name, scope, model string) {
	p := strings.SplitN(id, "/", 3)
	if len(p) == 1 {
		return CodeArts, "", id
	}
	if p[0] == CodeArts {
		return CodeArts, "", strings.TrimPrefix(id, CodeArts+"/")
	}
	if len(p) == 3 {
		return p[0], p[1], p[2]
	}
	return p[0], "", p[1]
}
func ModelID(name, scope, id string) string {
	if name == CodeArts || name == "" {
		return id
	}
	return name + "/" + scope + "/" + id
}

type Error struct {
	ContentBlocked bool
	Pending        bool
	Status         int
	Code           string
	Message        string
	AccountFault   bool
}

func (e *Error) Error() string       { return e.Message }
func (e *Error) PromptBlocked() bool { return e.ContentBlocked }

func (r *Registry) List() []Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Descriptor, 0, len(r.adapters))
	for _, a := range r.adapters {
		out = append(out, a.Describe())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// DailyRewards is optional. Status queries must not claim rewards.
type RewardResult struct {
	State   string   `json:"state"` // claimable, claimed, already_claimed, unavailable, unknown
	Message string   `json:"message"`
	Credits *float64 `json:"credits,omitempty"`
}

func (r RewardResult) Confirmed() bool { return r.State == "claimed" || r.State == "already_claimed" }

type DailyRewards interface {
	DailyStatus(context.Context, json.RawMessage) (RewardResult, error)
	ClaimDaily(context.Context, json.RawMessage) (RewardResult, error)
}

// Search is an optional provider capability, independent from model inference.
type SearchRequest struct {
	Query                                                string
	MaxResults                                           int
	SessionID, RequestID, ConversationRequestID, TraceID string
}
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}
type SearchOutcome struct {
	Query        string         `json:"query"`
	Results      []SearchResult `json:"results"`
	TotalResults int            `json:"total_results"`
}
type Searcher interface {
	Search(context.Context, json.RawMessage, SearchRequest) (*SearchOutcome, error)
}
