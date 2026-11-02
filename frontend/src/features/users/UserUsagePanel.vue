<script setup lang="ts">
import type { components } from '../../api/generated/admin';
import UserIcon from './UserIcon.vue';
import UserStatCard from './UserStatCard.vue';
import { userNumber, userDate, userUSD } from './user-format';
defineProps<{usage?:components['schemas']['UserUsagePage'];busy:boolean}>();
defineEmits<{page:[page:number]}>();
</script>
<template>
  <section id="userUsage" class="portal-stack">
    <div class="portal-stats three"><UserStatCard label="请求次数" :value="userNumber(usage?.stats.total_requests)" note="所选时间范围" icon="activity" /><UserStatCard label="Token 用量" :value="userNumber((usage?.stats.input_tokens??0)+(usage?.stats.output_tokens??0))" note="输入与输出合计" icon="token" tone="blue" /><UserStatCard label="参考费用" :value="userUSD(usage?.stats.estimated_usd)" note="仅统计已定价请求" icon="wallet" tone="green" /></div>
    <div class="portal-card portal-table-card"><div class="portal-section-heading"><div><h2>请求记录</h2><p>查看每次调用的模型、用量与耗时。</p></div><span class="portal-count">{{userNumber(usage?.total)}} 条</span></div>
      <div class="table-wrap"><table><thead><tr><th>请求时间</th><th>模型</th><th>状态</th><th>输入 / 输出 Token</th><th>参考费用</th><th>耗时</th></tr></thead><tbody><tr v-for="entry in usage?.logs??[]" :key="entry.request_id"><td class="portal-muted portal-nowrap">{{userDate(entry.time)}}</td><td><span class="portal-model-label">{{entry.model}}</span></td><td><span class="portal-badge" :class="entry.status>=200&&entry.status<300?'success':'danger'">{{entry.status}}</span></td><td class="portal-numeric">{{entry.usage.known?`${userNumber(entry.usage.input_tokens)} / ${userNumber(entry.usage.output_tokens)}`:'未报告'}}</td><td class="portal-numeric">{{entry.estimated_cost?userUSD(entry.estimated_cost.amount):'未定价'}}</td><td class="portal-muted portal-nowrap">{{entry.latency_ms??'—'}} ms</td></tr></tbody></table></div>
      <div v-if="!usage?.logs.length" class="portal-empty"><span class="portal-empty-icon"><UserIcon name="chart" /></span><h3>暂无请求记录</h3><p>使用 API 密钥调用模型后，用量将在这里显示。</p></div>
      <div class="portal-table-footer"><span>共 {{userNumber(usage?.total)}} 条记录</span><div class="portal-pagination"><button class="btn" :disabled="busy||(usage?.page??1)<=1" @click="$emit('page',(usage?.page??1)-1)">上一页</button><span>{{usage?.page??1}} / {{usage?.pages??1}}</span><button class="btn" :disabled="busy||(usage?.page??1)>=(usage?.pages??1)" @click="$emit('page',(usage?.page??1)+1)">下一页</button></div></div>
    </div>
    <p class="portal-note">删除密钥不删除历史用量。记录保留在全实例最近 {{userNumber(usage?.retention_limit)}} 条范围内，累计预算独立保存。</p>
  </section>
</template>
