package app

import (
	"context"
	"time"

	"codearts2api/internal/jobs"
)

// startBackground is the process-owned set of three existing background tasks.
// Startup model refresh deliberately retains its independent deadline. Cancel
// stops polling but does not join an in-flight task, as in the original main.
func startBackground(save func() error, refresh func(context.Context) (string, error), claim func(context.Context), logf func(string, ...any)) context.CancelFunc {
	ctx, stop := context.WithCancel(context.Background())
	go jobs.RunDailyLoop(ctx, claim)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := save(); err != nil {
					logf("定期保存用量失败: %v", err)
				}
			}
		}
	}()
	go func() {
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		if status, err := refresh(ctx); err != nil {
			logf("刷新上游模型清单失败: %v", err)
		} else {
			logf("模型目录同步结果 %s", status)
		}
	}()
	return stop
}
