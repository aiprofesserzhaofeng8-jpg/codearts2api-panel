package catalog

import (
	"context"
	"strings"
	"time"

	"codearts2api/internal/catalog/modelmeta"
	"codearts2api/internal/provider"
)

// The native providers are reference authorities, not extra gateway platforms.
// Exact model IDs may appear on several channels; prefer OpenCode Zen, then Go, then vendor catalogs.
var referenceProviders = []string{"opencode", "opencode-go", "openai", "anthropic", "google", "deepseek", "zhipuai", "zai", "moonshotai", "moonshotai-cn", "alibaba-cn", "alibaba", "minimax", "minimax-cn", "xai", "huawei-maas", "volcengine", "tencent-tokenhub"}

// These aliases select numeric client defaults only; they do not rename public
// models, merge routes or change the upstream model sent in a request. Keep
// versions explicit rather than stripping arbitrary suffixes or custom slots.
var clientReferenceAliases = map[string]string{
	"deepseek-v4-flash-official": "deepseek-v4-flash",
	"deepseek-v4-pro-official":   "deepseek-v4-pro",
	"doubao-seed-2.0-code":       "doubao-seed-2-0-code-preview-260215",
	"doubao-seed-2.1-pro":        "doubao-seed-2-1-pro-260628",
	"doubao-seed-2.1-turbo":      "doubao-seed-2-1-turbo-260628",
	"qwen-3.7-plus":              "qwen3.7-plus",
}

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
	s.commitMu.Lock()
	s.references = references
	s.commitMu.Unlock()
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
		var previous *provider.ClientReference
		identityChanged := false
		if s.directory != nil {
			old, found := s.directory.Resolve(m.ID, nil)
			if found && old.ID == m.ID {
				oldName, _ := defaultModelName(old)
				identityChanged = oldName != name || !strings.EqualFold(strings.TrimSpace(old.Name), strings.TrimSpace(m.Name))
				if !identityChanged {
					previous = old.ClientReference
				}
			}
		}
		ref, ok := s.references.Lookup(name, referenceProviders)
		if !ok {
			if alias, exists := clientReferenceAliases[strings.ToLower(name)]; exists {
				ref, ok = s.references.Lookup(alias, referenceProviders)
			}
		}
		if !ok || (ref.ContextWindow <= 0 && ref.MaxOutputTokens <= 0) {
			if previous != nil {
				out[i].ClientReference = previous
				continue
			}
			if identityChanged {
				continue
			}
			ref, ok = modelmeta.Bundled.Lookup(name, referenceProviders)
			if !ok {
				if alias, exists := clientReferenceAliases[strings.ToLower(name)]; exists {
					ref, ok = modelmeta.Bundled.Lookup(alias, referenceProviders)
				}
			}
			if !ok {
				continue
			}
		}
		if previous != nil && previous.ModelID == ref.ID {
			if ref.ContextWindow <= 0 {
				ref.ContextWindow = int64(previous.ContextWindow)
			}
			if ref.MaxOutputTokens <= 0 {
				ref.MaxOutputTokens = int64(previous.MaxOutputTokens)
			}
		}
		out[i].ClientReference = &provider.ClientReference{ModelID: ref.ID,
			ContextWindow:   int(min(max(ref.ContextWindow, 0), 2097152)),
			MaxOutputTokens: int(min(max(ref.MaxOutputTokens, 0), 2097152)), Reference: modelmeta.RegistryURL}
	}
	return out
}
