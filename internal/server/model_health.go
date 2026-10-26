package server

import (
	"codearts2api/internal/codearts"
	"errors"
	"strings"
)

func modelUnavailable(err error) bool {
	var upstream *codearts.UpstreamError
	if !errors.As(err, &upstream) {
		return false
	}
	text := strings.ToLower(upstream.Message)
	return (upstream.Status == 404 || upstream.Status == 400) && strings.Contains(text, "model") &&
		(strings.Contains(text, "not found") || strings.Contains(text, "not registered") || strings.Contains(text, "not available") || strings.Contains(text, "unavailable"))
}
