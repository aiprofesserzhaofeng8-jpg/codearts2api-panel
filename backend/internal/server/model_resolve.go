package server

import (
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
)

// resolveModel delegates catalog policy; account admission remains atomic in Store.
func (s *Server) resolveModel(settings store.Settings, requested string) (registry.Model, error) {
	return s.Routing.ResolveDefault(settings, requested)
}
