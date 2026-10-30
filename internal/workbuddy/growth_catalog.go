package workbuddy

// Provider-local catalog: task semantics and caps are independent of routing,
// other providers and upstream-provided auto_supported fields.
import "codearts2api/internal/provider"

type growthTaskSpec struct {
	action      provider.GrowthAction
	description string
	expertName  string
	expertType  string
}

func growthSpec(code string) (growthTaskSpec, bool) {
	var spec growthTaskSpec
	switch code {
	case "RichMeow_Chat", "Buddy_App", "Buddy_App_QQ", "automation_1", "Library_read", "playbook_prompt", "create_canvas":
		spec.action.MaxReports = 1
		spec.description = "提交活动事件并核对原生进度，不调用模型；达标后自动领取"
	case "template_5":
		spec.action.MaxReports = 5
		spec.description = "按已确认进度补足不同模板事件，最多 5 组，不调用模型"
	case "Hp_Appearance":
		spec.action.MaxReports = 1
		spec.description = "设置账号的和平精英主题并提交生效事件，不调用模型"
	case "expert_5":
		spec.action.MaxCalls, spec.expertType = 5, "agent"
	case "Expert_team_use_3":
		spec.action.MaxCalls, spec.expertType = 3, "team"
	case "skill_1":
		spec.action.MaxCalls = 1
	case "Expert_lighthouse":
		spec.action.MaxCalls, spec.expertName = 1, "腾讯轻量云专家"
	case "WorkBuddy_Valorant":
		spec.action.MaxCalls, spec.expertName = 1, "无畏契约拼豆专家"
	case "expert_use_maidehao":
		spec.action.MaxCalls, spec.expertName = 1, "中国电商运营专家"
	case "expert_use_douyinyin":
		spec.action.MaxCalls, spec.expertName = 1, "抖音策略师"
	case "expert_use_shangjia_xuanchuan":
		spec.action.MaxCalls, spec.expertName = 1, "商家宣传页与海报设计师"
	case "expert_use_bowangwang":
		spec.action.MaxCalls, spec.expertName = 1, "直播电商教练"
	default:
		return spec, false
	}
	spec.action.Prerequisite = "first_buddy"
	if spec.action.MaxCalls > 0 {
		spec.action.Model = "fast-model"
		spec.action.NativeModel = true
		spec.description = "查询真实专家/技能资源，执行 fast-model 短对话及使用事件；会消耗积分，进度未确认不重复调用"
	}
	return spec, true
}
