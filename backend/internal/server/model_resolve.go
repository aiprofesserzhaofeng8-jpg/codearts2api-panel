package server

import (
	"codearts2api/internal/registry"
	"codearts2api/internal/storage"
)

// resolveModel delegates catalog policy; account admission remains atomic in Store.
func (s *Server) resolveModel(settings storage.Settings, requested string) (registry.Model, error) {
	return s.Routing.ResolveDefault(settings, requested)
}
