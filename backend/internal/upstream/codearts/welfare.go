package codearts

import (
	"context"
	"fmt"

	"codearts2api/internal/accounts"
	legacy "codearts2api/internal/codearts"
	"codearts2api/internal/jobs"
)

func (s *Adapter) WelfareSnapshot(ctx context.Context, manager *legacy.Manager, a *accounts.Account, options legacy.ResolveOptions) (*jobs.WelfareSnapshot, error) {
	sess, err := manager.Session(ctx, a, options)
	if err != nil {
		return nil, err
	}
	delivery, err := s.client.FetchWelfareDelivery(ctx, sess)
	if err != nil {
		return nil, err
	}
	result := &jobs.WelfareSnapshot{Campaigns: make([]jobs.WelfareCampaign, 0, len(delivery.Campaigns))}
	for _, c := range delivery.Campaigns {
		if c.Claimable {
			result.ClaimableCount++
		}
		result.Campaigns = append(result.Campaigns, jobs.WelfareCampaign{
			CampaignID: c.CampaignID, Title: c.Title, Type: c.Type, Status: c.Status,
			Claimable: c.Claimable, AutoClaimable: c.IsAutoClaimable(),
			BenefitAmount: c.BenefitAmount, BenefitUnit: c.BenefitUnit, Description: c.Description,
			TriggerMode: c.Extra.TriggerMode, TriggerEvent: c.Extra.TriggerEvent,
			StartTime: c.Extra.StartTime, EndTime: c.Extra.EndTime,
		})
	}
	return result, nil
}

func (s *Adapter) WelfareInvite(ctx context.Context, manager *legacy.Manager, a *accounts.Account, options legacy.ResolveOptions, campaignID int) (*jobs.WelfareInvite, error) {
	sess, err := manager.Session(ctx, a, options)
	if err != nil {
		return nil, err
	}
	code, err := s.client.GenerateInviteCode(ctx, sess, campaignID)
	if err != nil {
		// Credential errors above keep their original unprefixed response.
		return nil, fmt.Errorf("生成邀请链接失败: %w", err)
	}
	return &jobs.WelfareInvite{
		CampaignID: code.CampaignID, InviteCode: code.InviteCode, InviteURL: code.InviteURL,
		Valid: code.Valid, Status: code.Status, ActivationCount: code.ActivationCount, MaxActivations: code.MaxActivations,
	}, nil
}
