<script setup lang="ts">
import { computed } from 'vue';
import type { components } from '../../api/generated/admin';
import type { UserPage } from './navigation';
import UserStatCard from './UserStatCard.vue';
import UserIcon from './UserIcon.vue';
import { userNumber, userUSD, userDate } from './user-format';
const props = defineProps<{usage?:components['schemas']['UserUsagePage']}>();
defineEmits<{navigate:[page:UserPage]}>();
const stats=computed(()=>props.usage?.stats);
const success=computed(()=>stats.value?.total_requests?((stats.value.total_requests-stats.value.failed_requests)/stats.value.total_requests*100).toFixed(1)+'%':'—');
const average=computed(()=>stats.value?.latency_samples?Math.round(stats.value.total_latency_ms/stats.value.latency_samples)+' ms':'—');
const trend=computed(()=>props.usage?.analysis.trend??[]);
const models=computed(()=>[...(props.usage?.analysis.models??[])].sort((a,b)=>b.stats.total_requests-a.stats.total_requests).slice(0,5));
const peak=computed(()=>Math.max(1,...trend.value.map(p=>p.stats.total_requests)));
const points=computed(()=>trend.value.map((p,i)=>({x:trend.value.length===1?300:20+i*560/(trend.value.length-1),y:155-p.stats.total_requests/peak.value*125,point:p})));
const line=computed(()=>points.value.map(p=>`${p.x},${p.y}`).join(' '));
const area=computed(()=>points.value.length?`${points.value[0]!.x},170 ${line.value} ${points.value[points.value.length-1]!.x},170`:'');
const axis=computed(()=>trend.value.filter((_,i)=>i===0||i===trend.value.length-1||i%Math.max(1,Math.ceil(trend.value.length/5))===0));
const maxModel=computed(()=>Math.max(1,...models.value.map(m=>m.stats.total_requests)));
</script>
<template>
  <div id="userDashboard" class="portal-stack">
    <div class="portal-stats"><UserStatCard label="请求次数" :value="userNumber(stats?.total_requests)" note="所选时间范围内的全部请求" icon="activity" /><UserStatCard label="Token 用量" :value="userNumber((stats?.input_tokens??0)+(stats?.output_tokens??0))" note="输入与输出 Token 合计" icon="token" tone="blue" /><UserStatCard label="参考费用" :value="userUSD(stats?.estimated_usd)" note="仅统计已定价请求" icon="wallet" tone="green" /><UserStatCard label="请求成功率" :value="success" :note="userNumber(stats?.failed_requests)+' 次失败请求'" icon="shield" tone="violet" /></div>
    <div class="portal-dashboard-grid">
      <section class="portal-card portal-chart-card"><div class="portal-section-heading"><div><h2>请求趋势</h2><p>了解你的 API 使用情况</p></div><span class="portal-chart-legend"><i></i>请求数</span></div>
        <div v-if="!trend.length" class="portal-empty"><span class="portal-empty-icon"><UserIcon name="chart" /></span><h3>当前范围暂无请求记录</h3><p>开始使用模型后，在这里查看请求趋势。</p></div>
        <template v-else><div class="portal-line-chart"><div class="portal-chart-scale"><span>{{peak}}</span><span>{{Math.round(peak/2)}}</span><span>0</span></div><svg viewBox="0 0 600 180" preserveAspectRatio="none" role="img" aria-label="每日请求趋势" aria-describedby="userTrendDescription"><desc id="userTrendDescription">{{trend.map(p=>new Date(p.time*1000).toLocaleDateString()+'：'+p.stats.total_requests+' 次请求').join('；')}}</desc><defs><linearGradient id="userRequestArea" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stop-color="currentColor" stop-opacity=".18"/><stop offset="100%" stop-color="currentColor" stop-opacity="0"/></linearGradient></defs><path class="portal-chart-grid" d="M0 30h600M0 92h600M0 155h600"/><polygon :points="area" fill="url(#userRequestArea)"/><polyline :points="line" class="portal-chart-line"/><circle v-for="p in points" :key="p.point.time" :cx="p.x" :cy="p.y" r="3"><title>{{new Date(p.point.time*1000).toLocaleDateString()}}：{{p.point.stats.total_requests}} 次请求</title></circle></svg></div><div class="portal-chart-axis"><span v-for="p in axis" :key="p.time">{{new Date(p.time*1000).toLocaleDateString('zh-CN',{month:'numeric',day:'numeric'})}}</span></div><p v-if="(usage?.analysis.bucket_days??1)>1" class="portal-note">每 {{usage?.analysis.bucket_days}} 天合并显示。</p></template>
        <div class="portal-chart-footer"><span>平均耗时 <strong>{{average}}</strong></span><span>缓存 Token <strong>{{userNumber(stats?.cached_tokens)}}</strong></span></div>
      </section>
      <section class="portal-card"><div class="portal-section-heading"><div><h2>常用模型</h2><p>按请求次数排序</p></div><UserIcon name="model" /></div>
        <div v-if="!models.length" class="portal-empty"><span class="portal-empty-icon"><UserIcon name="model" /></span><h3>当前范围暂无模型用量</h3><p>模型使用分布将在这里显示。</p></div>
        <div v-for="m in models" :key="m.name" class="portal-model-usage"><div><span :title="m.name">{{m.name}}</span><strong>{{userNumber(m.stats.total_requests)}} 次</strong></div><progress :value="m.stats.total_requests" :max="maxModel" :aria-label="m.name+' 相对请求次数'" /><p>{{userNumber(m.stats.input_tokens+m.stats.output_tokens)}} Token <span>{{userUSD(m.stats.estimated_usd)}}</span></p></div>
      </section>
    </div>
    <section class="portal-card portal-table-card"><div class="portal-section-heading"><div><h2>请求记录</h2><p>当前记录页的调用明细</p></div><button class="btn ghost xs" @click="$emit('navigate','usage')">查看全部记录 →</button></div><div class="table-wrap"><table><thead><tr><th>请求时间</th><th>模型</th><th>状态</th><th>Token 用量</th><th>参考费用</th></tr></thead><tbody><tr v-for="entry in (usage?.logs??[]).slice(0,5)" :key="entry.request_id"><td class="portal-muted portal-nowrap">{{userDate(entry.time)}}</td><td><span class="portal-model-label">{{entry.model}}</span></td><td><span class="portal-badge" :class="entry.status>=200&&entry.status<300?'success':'danger'">{{entry.status}}</span></td><td class="portal-numeric">{{entry.usage.known?userNumber(Number(entry.usage.input_tokens??0)+Number(entry.usage.output_tokens??0)):'未报告'}}</td><td class="portal-numeric">{{entry.estimated_cost?userUSD(entry.estimated_cost.amount):'未定价'}}</td></tr></tbody></table></div><div v-if="!usage?.logs.length" class="portal-empty compact"><UserIcon name="activity" /><p>暂无调用记录，创建密钥后即可开始使用。</p><button class="btn" @click="$emit('navigate','keys')">创建 API 密钥</button></div></section>
    <p class="portal-note">{{usage?.analysis.time_zone}} · 已报告用量 {{userNumber(stats?.usage_requests)}} 次 · 已定价 {{userNumber(stats?.priced_usd_requests)}} 次。</p>
  </div>
</template>
