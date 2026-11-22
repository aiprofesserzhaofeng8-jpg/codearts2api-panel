package catalog

import (
	"codearts2api/internal/provider"
	"sort"
	"strings"
)

type publishedStore interface {
	PublishedModels() map[string]provider.Model
}
type publishedDirectory struct {
	base  RoutingDirectory
	store publishedStore
}

func (d publishedDirectory) bound(m provider.Model, ok bool) (provider.Model, bool) {
	if !ok {
		return m, false
	}
	if owners, managed := d.store.PublishedModels()[m.ID]; managed {
		if owners.ID == "" {
			return provider.Model{}, false
		}
		m.SyncedOwners = owners.SyncedOwners
	}
	return m, true
}
func (d publishedDirectory) ResolveRaw(name string) (provider.Model, bool) {
	if m, ok := d.base.ResolveRaw(name); ok {
		return d.bound(m, true)
	}
	if strings.HasPrefix(name, "codearts/") {
		raw := strings.TrimPrefix(name, "codearts/")
		if strings.Contains(raw, "/") {
			return provider.Model{}, false
		}
		name = raw
	}
	m := d.store.PublishedModels()[name]
	return m, m.ID != ""
}
func (d publishedDirectory) Resolve(name string, aliases map[string]string) (provider.Model, bool) {
	if m, ok := d.base.Resolve(name, aliases); ok {
		return d.bound(m, true)
	}
	if alias := aliases[name]; alias != "" {
		name = alias
	}
	if strings.Contains(name, "/") {
		return d.ResolveRaw(name)
	}
	var found provider.Model
	for _, m := range d.store.PublishedModels() {
		if m.ID != "" && m.UpstreamID == name {
			if found.ID != "" {
				return provider.Model{}, false
			}
			found = m
		}
	}
	return found, found.ID != ""
}
func (d publishedDirectory) List(aliases map[string]string) []provider.Model {
	out := d.base.List(aliases)
	seen := map[string]bool{}
	for _, m := range out {
		seen[m.ID] = true
	}
	additions := []provider.Model{}
	for _, m := range d.store.PublishedModels() {
		if m.ID != "" && !seen[m.ID] {
			additions = append(additions, m)
			seen[m.ID] = true
		}
	}
	// The base registry cannot enumerate aliases whose targets exist only
	// in an account's published catalog. Use the same resolver as requests.
	for alias := range aliases {
		if seen[alias] || strings.Contains(alias, "/") {
			continue
		}
		if m, ok := d.Resolve(alias, aliases); ok {
			m.ID, m.Name, m.Source = alias, alias+" → "+m.Name, "alias"
			additions = append(additions, m)
		}
	}
	sort.Slice(additions, func(i, j int) bool { return additions[i].ID < additions[j].ID })
	return append(out, additions...)
}
