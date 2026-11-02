<script setup lang="ts">
import { computed } from 'vue';
import type { components } from '../../api/generated/admin';
const props = defineProps<{ usage?: components['schemas']['UserUsagePage'] }>();
const stats = computed(() => props.usage?.stats);
const success = computed(() => stats.value?.total_requests ? ((stats.value.total_requests-stats.value.failed_requests)/stats.value.total_requests*100).toFixed(1)+'%' : '—');
const average = computed(() => stats.value?.latency_samples ? Math.round(stats.value.total_latency_ms/stats.value.latency_samples)+' ms' : '—');
const trend = computed(() => props.usage?.analysis.trend ?? []);
const models = computed(() => [...(props.usage?.analysis.models ?? [])].sort((a,b) => b.stats.total_requests-a.stats.total_requests).slice(0,10));
const peak = computed(() => Math.max(1, ...trend.value.map(p => p.stats.total_requests)));
const number = (n: number|undefined) => (n??0).toLocaleString();
const usd = (n: number|undefined) => '$'+(n??0).toFixed(6);
</script>
<template>
  <section class="card" id="userDashboard">
    <h2>用量概览</h2>
    <p class="hint">{{usage?.analysis.time_zone}} · 当前时间范围内的全部记录；参考费用仅统计已定价的请求。</p>
    <div class="user-dashboard-metrics">
      <div><span class="hint">请求数</span><strong>{{number(stats?.total_requests)}}</strong></div>
      <div><span class="hint">成功率</span><strong>{{success}}</strong></div>
      <div><span class="hint">Token 用量</span><strong>{{number((stats?.input_tokens??0)+(stats?.output_tokens??0))}}</strong></div>
      <div><span class="hint">参考费用</span><strong>{{usd(stats?.estimated_usd)}}</strong></div>
      <div><span class="hint">平均耗时</span><strong>{{average}}</strong></div>
      <div><span class="hint">缓存 Token</span><strong>{{number(stats?.cached_tokens)}}</strong></div>
    </div>
    <div class="user-dashboard-grid">
      <section><h3>请求趋势</h3>
        <p v-if="!trend.length" class="empty">当前范围暂无请求记录</p>
        <div v-else class="user-trend" role="list" aria-label="每日请求趋势">
          <div v-for="p in trend" :key="p.time" class="user-trend-row" role="listitem">
            <span>{{new Date(p.time*1000).toLocaleDateString()}}</span><meter :value="p.stats.total_requests" :max="peak" :aria-label="new Date(p.time*1000).toLocaleDateString()+' 请求数'"></meter><strong>{{p.stats.total_requests}}</strong>
          </div>
        </div>
        <p v-if="(usage?.analysis.bucket_days??1)>1" class="hint">每 {{usage?.analysis.bucket_days}} 天合并显示。</p>
      </section>
      <section><h3>常用模型</h3><p v-if="!models.length" class="empty">当前范围暂无模型用量</p>
        <div v-for="m in models" :key="m.name" class="user-model-usage"><span class="mono">{{m.name}}</span><strong>{{number(m.stats.total_requests)}} 次</strong><span class="hint">{{number(m.stats.input_tokens+m.stats.output_tokens)}} Token · {{usd(m.stats.estimated_usd)}}</span></div>
      </section>
    </div>
    <p class="hint">已报告用量 {{number(stats?.usage_requests)}} 次 · 已定价 {{number(stats?.priced_usd_requests)}} 次。历史在全实例最近 {{number(usage?.retention_limit)}} 条中保留；累计预算独立保存。</p>
  </section>
</template>
