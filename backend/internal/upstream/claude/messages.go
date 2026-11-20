package claude

import (
	"codearts2api/internal/messagewire"
	"codearts2api/internal/provider"
	"context"
	"io"
)

func messagesBody(r provider.Request) (any, error) { return messagewire.Encode(r, "Claude") }
func messagesStream(ctx context.Context, r io.Reader, model string, emit func([]byte) error) error {
	return messagewire.Decode(ctx, r, model, "Claude", emit)
}
