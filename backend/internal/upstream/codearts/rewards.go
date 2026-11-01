package codearts

import (
	"codearts2api/internal/accounts"
	legacy "codearts2api/internal/codearts"
	"context"
	"strings"
)

func (s *Adapter) Credit(ctx context.Context, manager *legacy.Manager, a *accounts.Account, options legacy.ResolveOptions) (*accounts.CreditInfo, error) {
	sess, err := manager.Session(ctx, a, options)
	if err != nil {
		return nil, err
	}
	return s.client.FetchCreditInfo(ctx, sess)
}
func (s *Adapter) ClaimWelfare(ctx context.Context, manager *legacy.Manager, a *accounts.Account, options legacy.ResolveOptions) (string, error) {
	manager.Invalidate(a.ID)

	sess, err := manager.Session(ctx, a, options)
	if err != nil {
		return "", err
	}

	var (
		parts    []string
		firstErr error
	)

	// 运营活动（每日积分等）
	if results, err := s.client.ClaimDailyWelfare(ctx, sess); err != nil {
		if firstErr == nil {
			firstErr = err
		}
		parts = append(parts, "活动福利："+err.Error())
	} else if len(results) == 0 {
		parts = append(parts, "活动福利：暂无可领")
	} else {
		for _, r := range results {
			parts = append(parts, legacy.DescribeWelfareError(r))
		}
	}

	msg := strings.Join(parts, "；")
	return msg, firstErr
}
