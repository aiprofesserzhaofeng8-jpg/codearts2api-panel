<script setup lang="ts">
import { t as tr } from '../../i18n';
</script>
<template>
<section v-once id="tab-overview" class="on" role="tabpanel" aria-labelledby="nav-overview">
        <div v-once class="usage-kpis" id="kpis" :aria-label="tr('用量汇总')"></div>
        <div v-once class="usage-range-card">
          <span class="usage-range-label">{{tr('时间范围：')}}</span>
          <div v-once class="usage-date-picker" id="usageDatePicker">
            <button class="btn usage-date-trigger" id="usageDateToggle" aria-expanded="false" aria-controls="usageDatePopover"><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="5" width="18" height="16" rx="2"/><path d="M16 3v4M8 3v4M3 11h18"/></svg><span id="usagePeriod">{{tr('全部已保存记录')}}</span><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6"/></svg></button>
            <div v-once class="usage-date-popover hidden" id="usageDatePopover" role="group" :aria-label="tr('选择日期范围')">
              <div v-once class="usage-date-presets" id="usageDatePresets">
                <button type="button" data-usage-preset="today">{{tr('今天')}}</button><button type="button" data-usage-preset="yesterday">{{tr('昨天')}}</button>
                <button type="button" data-usage-preset="7">{{tr('最近 7 天')}}</button><button type="button" data-usage-preset="30">{{tr('最近 30 天')}}</button>
                <button type="button" data-usage-preset="month">{{tr('本月')}}</button><button type="button" data-usage-preset="lastMonth">{{tr('上月')}}</button>
                <button type="button" data-usage-preset="all" aria-pressed="true">{{tr('全部记录')}}</button>
              </div>
              <select id="usagePreset" class="hidden" :aria-label="tr('快捷选择')"><option value="all">{{tr('全部记录')}}</option><option value="today">{{tr('今天')}}</option><option value="yesterday">{{tr('昨天')}}</option><option value="7">{{tr('近 7 天')}}</option><option value="30">{{tr('近 30 天')}}</option><option value="month">{{tr('本月')}}</option><option value="lastMonth">{{tr('上月')}}</option><option value="custom">{{tr('自定义')}}</option></select>
              <div v-once class="usage-date-custom"><label for="usageStart">{{tr('开始日期')}}<input id="usageStart" type="date"></label><span>→</span><label for="usageEnd">{{tr('结束日期')}}<input id="usageEnd" type="date"></label></div>
              <div v-once class="usage-date-actions"><button class="btn primary" id="applyUsageDates">{{tr('应用')}}</button></div>
            </div>
          </div>
          <span class="usage-timezone" id="usageTimezone"></span>
        </div>
        <div v-once class="usage-charts-grid" :aria-label="tr('用量图表')">
          <article class="usage-chart"><div class="usage-chart-head"><h3>{{tr('模型分布')}}</h3><div class="usage-metric-tabs" data-usage-metric="models" role="group" :aria-label="tr('模型分布统计方式')"></div></div><div id="usageModelsChart" class="usage-distribution"></div></article>
          <article class="usage-chart"><div class="usage-chart-head"><h3>{{tr('上游分布')}}</h3><div class="usage-metric-tabs" data-usage-metric="providers" role="group" :aria-label="tr('上游分布统计方式')"></div></div><div id="usageProvidersChart" class="usage-distribution"></div></article>
          <article class="usage-chart"><div class="usage-chart-head"><h3>{{tr('接口分布')}}</h3><div class="usage-metric-tabs" data-usage-metric="protocols" role="group" :aria-label="tr('接口分布统计方式')"></div></div><div id="usageProtocolsChart" class="usage-distribution"></div></article>
          <article class="usage-chart"><div class="usage-chart-head"><h3>{{tr('Token 使用趋势')}}</h3><span class="usage-chart-period" id="usageTrendPeriod">{{tr('按日统计')}}</span></div><div id="usageTrendChart" class="usage-trend"></div></article>
        </div>
        <div v-once class="usage-record-filters" :aria-label="tr('用量筛选')">
          <div v-once class="usage-record-fields">
            <label>{{tr('上游')}}<select id="usageProvider" :aria-label="tr('按上游筛选用量')"><option value="">{{tr('全部上游')}}</option><option value="codearts">{{tr('华为云码道')}}</option><option value="workbuddy">WorkBuddy</option><option value="trae">Trae CN / SOLO</option><option value="qoder">Qoder</option><option value="kiro">Kiro</option><option value="chatgpt">ChatGPT / Codex</option><option value="claude">Claude</option><option value="teleagent">TeleAgent</option></select></label>
            <label>{{tr('模型')}}<select id="usageModel" :aria-label="tr('按模型筛选用量')"><option value="">{{tr('全部模型')}}</option></select></label>
            <label>{{tr('接口')}}<select id="usageProtocol" :aria-label="tr('按接口筛选用量')"><option value="">{{tr('全部接口')}}</option><option value="chat_completions">Chat Completions</option><option value="responses">Responses</option><option value="messages">Messages</option><option value="search">{{tr('联网搜索')}}</option><option value="growth">{{tr('成长任务')}}</option></select></label>
            <label>{{tr('请求状态')}}<select id="usageStatus" :aria-label="tr('按请求状态筛选用量')"><option value="">{{tr('全部状态')}}</option><option value="success">{{tr('成功')}}</option><option value="error">{{tr('失败')}}</option></select></label>
          </div>
          <div v-once class="usage-record-actions"><button class="btn primary" id="reloadUsage"><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="M20 12a8 8 0 1 1-2.4-5.7M20 4v4.5h-4.5"/></svg>{{tr('刷新')}}</button><button class="btn" id="resetUsageFilters"><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="M3 11a9 9 0 1 1 2.6 7M3 4v7h7"/></svg>{{tr('重置')}}</button></div>
        </div>
        <div v-once class="card usage-history-card">
          <div v-once class="card-head"><div><h2>{{tr('用量记录')}}</h2><p class="card-sub" id="usageHistoryHint">{{tr('所选时间范围内的请求，按时间倒序展示')}}</p></div></div>
          <div v-once class="table-wrap"><table class="usage-table">
            <thead><tr><th>{{tr('模型')}}</th><th>{{tr('接口')}}</th><th>{{tr('状态')}}</th><th>Token</th><th>{{tr('估算费用')}}</th><th>{{tr('耗时')}}</th><th>{{tr('时间')}}</th></tr></thead>
            <tbody id="usageLogs"><tr><td colspan="7" class="muted">{{tr('加载后显示请求用量')}}</td></tr></tbody>
          </table></div>
          <div v-once class="usage-pagination" id="usagePagination">
            <div v-once class="pagination-info"><span id="usagePageInfo">{{tr('共 0 条')}}</span><label>{{tr('每页')}}<select id="usagePageSize"><option value="20">{{tr('20 条')}}</option><option value="50">{{tr('50 条')}}</option><option value="100">{{tr('100 条')}}</option></select></label></div>
            <div v-once class="pagination-buttons" id="usagePageButtons" role="group" :aria-label="tr('用量分页')"></div>
            <div v-once class="pagination-jump"><label>{{tr('跳至')}}<input id="usagePageJump" type="number" min="1" value="1" :aria-label="tr('跳转页码')"></label><button class="btn xs" id="usageJump">{{tr('跳转')}}</button></div>
          </div>
        </div>
        <details class="usage-insights"><summary>{{tr('统计详情')}}<span id="usageCoverage" class="usage-coverage"></span></summary>
        <div v-once class="usage-detail-grid">
          <div v-once class="card">
            <div v-once class="card-head"><div><h2>{{tr('Token 构成')}}</h2><p class="card-sub">{{tr('输入、输出与缓存分别统计，避免重复计数')}}</p></div></div>
            <div v-once id="usageBreakdown"></div>
          </div>
          <div v-once class="card">
            <div v-once class="card-head"><div><h2>{{tr('请求概况')}}</h2><p class="card-sub">{{tr('所选时间范围内的请求结果')}}</p></div></div>
            <div v-once id="usageHealth"></div>
          </div>
        </div>
        </details>
      </section>
</template>
