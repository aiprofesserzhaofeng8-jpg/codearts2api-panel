<script setup lang="ts">
import type { components } from '../../api/generated/admin';
import { t as tr, dateLocale } from '../../i18n';
defineProps<{ status: components['schemas']['UnifiedSourceStatus']; localProbe?: {ok:boolean;message:string;time:string} }>();
const at = (timestamp?: number) => timestamp ? new Date(timestamp * 1000).toLocaleString(dateLocale) : tr('未知');
const capability = (value?: string) => value === 'supported' ? tr('支持') : value === 'unsupported' ? tr('不支持') : tr('未知');
const purpose = (value: string) => tr(({chat:'通用聊天',auto:'平台自动路由',completion:'专用补全',jump:'代码跳转',rewrite:'代码重写',image_generation:'图片生成',agent:'平台专用智能体',summary:'平台摘要',unknown:'用途未确认',mixed:'来源用途不一致'} as Record<string,string>)[value] ?? '用途未确认');
const basis = (value: string) => tr(({provider_catalog:'上游目录',administrator_config:'管理员配置',tested_recommendation:'已验证的保守建议',public_registry:'公开模型资料',compatibility_default:'兼容默认参数',unknown:'未知'} as Record<string,string>)[value] ?? value);
</script>
<template>
  <div class="source-observations hint">
    <p>{{tr('来源能力')}}: {{tr('图片')}} {{capability(status.image_input)}} · {{tr('工具')}} {{capability(status.tool_calling)}}</p>
    <template v-if="status.client_compatibility">
      <p>{{tr('模型用途')}}: {{purpose(status.client_compatibility.purpose)}} · {{status.client_compatibility.exportable ? tr('可导出到聊天客户端') : tr('暂不自动导出')}}</p>
      <details>
        <summary>{{tr('客户端参数依据')}}</summary>
        <p v-for="item in status.client_compatibility.limits" :key="item.source">
          {{basis(item.basis)}} · {{tr('推荐上下文 / 输出')}}: {{item.context_window || tr('未知')}} / {{item.max_output_tokens || tr('未知')}}
          <span v-if="item.provider_context || item.provider_max_output"> · {{tr('上游目录上限')}}: {{item.provider_context || tr('未知')}} / {{item.provider_max_output || tr('未知')}}</span>
          <span v-if="item.official_context || item.official_max_output"> · {{tr('原生官方规格')}}: {{item.official_context || tr('未知')}} / {{item.official_max_output || tr('未知')}}</span>
          <span v-if="item.verified_at"> · {{item.verified_at}}</span>
        </p>
        <p v-if="status.client_compatibility.reason">{{tr(status.client_compatibility.reason)}}</p>
        <p>{{tr('客户端建议不代表官方或平台最大值；缺失来源不自动套用其他来源参数。')}}</p>
      </details>
    </template>
    <p v-if="status.runtime">
      {{tr('账号调度快照')}}: {{tr('可用')}} {{status.runtime.ready}} · {{tr('冷却')}} {{status.runtime.cooling}} · {{tr('忙碌')}} {{status.runtime.busy}} · {{tr('暂停')}} {{status.runtime.paused}} · {{tr('停用')}} {{status.runtime.disabled}}
    </p>
    <p v-if="status.runtime?.next_retry_at">{{tr('冷却结束后可重试')}}: {{at(status.runtime.next_retry_at)}} · {{tr('不代表配额重置时间')}}</p>
    <p v-if="status.runtime?.last_request">{{tr('最近来源请求')}}: HTTP {{status.runtime.last_request.status}}<span v-if="status.runtime.last_request.error_kind"> · {{status.runtime.last_request.error_kind}}</span> · {{at(status.runtime.last_request.checked_at)}}</p>
    <p v-else>{{tr('最近来源请求')}}: {{tr('尚无记录')}}</p>
    <p v-if="localProbe" :class="localProbe.ok ? 'hint' : 'err-text'">{{tr('本页文本测活')}}: {{localProbe.message}} · {{localProbe.time}}</p>
    <p v-else-if="!status.last_probe">{{tr('文本测活记录')}}: {{tr('尚无记录')}}</p>
    <p v-if="!status.quota?.length">{{tr('额度快照')}}: {{tr('未知，请在账号页刷新额度')}}</p>
    <div v-for="quota in status.quota" :key="quota.account_id">
      <p>{{tr('额度快照')}} · {{quota.account_id}} · {{quota.stale ? tr('已过期') : tr('缓存')}} · {{new Date(quota.observed_at).toLocaleString(dateLocale)}}</p>
      <template v-if="quota.available && quota.snapshot?.available">
        <p v-if="quota.snapshot.kind==='credits' || quota.snapshot.unit==='credits'">{{tr('剩余额度')}}: {{quota.snapshot.remaining ?? tr('未知')}} {{quota.snapshot.unit}}</p>
        <p v-for="window in quota.snapshot.windows" :key="window.id">
          {{tr(window.name)}}: <span v-if="window.remaining!=null">{{tr('剩余')}} {{window.remaining}} {{window.unit}}</span><span v-else-if="window.utilization!=null">{{tr('已用')}} {{window.utilization}}%</span><span v-else>{{tr('未知')}}</span>
          · {{tr('重置时间')}}: {{at(window.resets_at)}}
        </p>
      </template>
      <p v-else>{{tr('额度未知，不按零处理')}}</p>
    </div>
    <p>{{tr('来源级快照，不代表当前分组权限或图片、工具实测结果。')}} · {{new Date(status.observed_at).toLocaleString(dateLocale)}}</p>
  </div>
</template>
