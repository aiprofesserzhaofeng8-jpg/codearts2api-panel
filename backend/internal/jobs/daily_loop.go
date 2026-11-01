package jobs

import (
	"context"
	"time"
)

// RunDailyLoop preserves the original startup delay and serial polling cadence.
// The first check runs after 8 seconds; the 5-minute ticker starts only after
// that check returns. Eligibility, dates and deduplication remain in Rewards.
// The caller owns the goroutine and cancellation.
func RunDailyLoop(ctx context.Context, run func(context.Context)) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(8 * time.Second):
	}
	run(ctx)

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run(ctx)
		}
	}
}
