package chatgpt

import (
	"codearts2api/internal/provider"
	"codearts2api/internal/responsewire"
	"context"
	"io"
)

func responsesBody(r provider.Request, codex bool) (any, error) {
	return responsewire.Encode(r, codex, "ChatGPT")
}
func responsesStream(ctx context.Context, r io.Reader, model string, emit func([]byte) error) error {
	return responsewire.Decode(ctx, r, model, "ChatGPT", emit)
}
