package catalog

import (
	"context"
	"strings"
	"time"

	"codearts2api/internal/catalog/modelmeta"
	"codearts2api/internal/provider"
)

// The native providers are reference authorities, not extra gateway platforms.
// Exact model IDs may appear on several channels; prefer the original vendor.
var referenceProviders = []string{"openai", "anthropic", "google", "deepseek", "zhipuai", "zai", "moonshotai", "moonshotai-cn", "alibaba-cn", "alibaba", "minimax", "minimax-cn"}

// Called only by the existing serialized directory refresh. A failed optional
// fetch keeps prior references and backs off; it never fails model discovery.
func (s *Runtime) refreshReferences(ctx context.Context) {
	if s.loadReferences == nil || time.Now().Before(s.referenceRetry) {
		return
	}
	s.referenceRetry = time.Now().Add(30 * time.Minute)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	body, err := s.loadReferences(ctx)
	if err != nil {
		return
	}
	references, err := modelmeta.Parse(body)
	if err != nil {
		return
	}
	s.references = references
	s.referenceRetry = time.Now().Add(6 * time.Hour)
}

func (s *Runtime) withClientReferences(platform, scope string, models []provider.Model) []provider.Model {
	out := append([]provider.Model(nil), models...)
	for i := range out {
		m := out[i]
		m.Provider, m.Scope = platform, scope
		if m.UpstreamID == "" {
			m.UpstreamID = m.ID
		}
		m.ID = provider.ModelID(platform, scope, m.UpstreamID)
		name, _ := defaultModelName(m)
		// Internal slots require the existing reviewed name mapping. A generic
		// slot or a changed identity must not inherit a guessed native model.
		if !publicModelIdentity.MatchString(name) || strings.ContainsAny(name, " /\t\r\n") {
			continue
		}
		ref, ok := s.references.Lookup(name, referenceProviders)
		if !ok || (ref.ContextWindow <= 0 && ref.MaxOutputTokens <= 0) {
			continue
		}
		out[i].ClientReference = &provider.ClientReference{ModelID: ref.ID,
			ContextWindow:   int(min(max(ref.ContextWindow, 0), 2097152)),
			MaxOutputTokens: int(min(max(ref.MaxOutputTokens, 0), 2097152)), Reference: modelmeta.RegistryURL}
	}
	return out
}
