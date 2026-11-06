<script setup lang="ts">
import { t as tr } from '../../i18n';
import UnifiedModelsPanel from './UnifiedModelsPanel.vue';
</script>
<template>
<section id="tab-models" role="tabpanel" aria-labelledby="nav-models">
        <UnifiedModelsPanel />
        <div v-once class="card">
          <div v-once class="card-head model-catalog-head">
            <div v-once class="model-catalog-title"><h2>{{tr('平台原始型号')}}</h2><span class="badge" id="modelsCount">{{tr('0 个')}}</span></div>
            <div v-once class="model-catalog-actions">
              <button class="btn" id="refreshModels" :aria-label="tr('从上游刷新清单')" :title="tr('从上游刷新清单')"><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="M20 12a8 8 0 1 1-2.4-5.7M20 4v4.5h-4.5"/></svg><span class="model-refresh-label">{{tr('刷新清单')}}</span></button>
              <button class="btn" id="probeAllModels" :title="tr('测活当前列表中的模型')"><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="M3 12h4l3-7 4 14 3-7h4"/></svg>{{tr('批量测活')}}</button>
            </div>
          </div>
          <div v-once class="model-catalog-filters" role="group" :aria-label="tr('模型筛选')">
            <div v-once class="model-search"><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 4.5 4.5"/></svg><input id="modelSearch" type="search" :placeholder="tr('搜索模型')" :aria-label="tr('搜索模型名称或 ID')" autocomplete="off"></div>
            <select id="modelsGroupFilter" :aria-label="tr('按分组查看模型')"><option value="">{{tr('全部分组')}}</option></select>
          </div>
          <p class="hint model-catalog-status" id="modelHint" role="status" aria-live="polite"></p>
          <div v-once class="table-wrap">
            <table>
              <thead><tr><th>ID</th><th>{{tr('名称')}}</th><th>{{tr('积分倍率')}}</th><th>{{tr('上下文')}}</th><th>{{tr('最大输出')}}</th><th>{{tr('说明')}}</th><th>{{tr('测活')}}</th></tr></thead>
              <tbody id="models"></tbody>
            </table>
          </div>
        </div>
        <div v-once class="card">
          <div v-once class="card-head"><div><h2>{{tr('Token 价格表')}}</h2></div><button class="btn" id="reloadPricing">{{tr('刷新价格')}}</button></div>
          <div v-once class="table-wrap"><table class="pricing-table">
            <thead><tr><th>{{tr('模型')}}</th><th>{{tr('币种')}}</th><th>{{tr('输入（未缓存）')}}</th><th>{{tr('输出')}}</th><th>{{tr('缓存命中')}}</th><th>{{tr('来源 / 更新时间')}}</th><th>{{tr('操作')}}</th></tr></thead>
            <tbody id="pricingRows"></tbody>
          </table></div>
        </div>
        <div v-once class="card">
          <div v-once class="card-head"><div><h2>{{tr('客户端兼容与压缩策略')}}</h2></div><button class="btn" id="reloadModelPolicies">{{tr('刷新策略')}}</button></div>
          <p class="hint">{{tr('码道上游：Chat Completions。Responses / Messages 由网关转换。原生 Responses compact、WebSocket 和响应存储暂不支持。')}}</p>
          <div v-once class="table-wrap"><table><thead><tr><th>{{tr('模型')}}</th><th>{{tr('推荐上下文 / 输出')}}</th><th>{{tr('压缩触发 / 保留')}}</th><th>{{tr('摘要处理')}}</th><th>{{tr('来源')}}</th><th>{{tr('操作')}}</th></tr></thead><tbody id="modelPolicies"></tbody></table></div>
        </div>
      </section>
<dialog v-once id="priceEditDialog" class="modal" aria-labelledby="priceEditTitle">
      <div v-once class="card-head"><div><h2 id="priceEditTitle">{{tr('编辑模型价格')}}</h2><p class="card-sub" id="priceEditModel"></p></div><button class="btn xs" id="priceEditClose" :aria-label="tr('关闭价格编辑')">{{tr('关闭')}}</button></div>
      <p class="hint">{{tr('USD / 百万 Token。保存后只对新请求生效，历史费用不会变化。')}}</p>
      <div v-once class="field-grid">
        <div v-once class="field"><label for="priceEditInput">{{tr('输入（未缓存）')}}</label><input id="priceEditInput" type="number" min="0" max="1000000" step="any" :placeholder="tr('未配置')" required></div>
        <div v-once class="field"><label for="priceEditOutput">{{tr('输出')}}</label><input id="priceEditOutput" type="number" min="0" max="1000000" step="any" :placeholder="tr('未配置')" required></div>
        <div v-once class="field"><label for="priceEditCached">{{tr('缓存命中')}}</label><input id="priceEditCached" type="number" min="0" max="1000000" step="any" :placeholder="tr('未配置')" required></div>
      </div>
      <p class="hint" id="priceEditError" role="alert"></p>
      <div v-once class="row"><button class="btn" id="priceEditCancel">{{tr('取消')}}</button><button class="btn primary" id="priceEditSave">{{tr('保存价格')}}</button></div>
    </dialog>
<dialog v-once id="policyDialog" class="modal" aria-labelledby="policyTitle">
      <div v-once class="card-head"><div><h2 id="policyTitle">{{tr('模型兼容策略')}}</h2><p class="card-sub" id="policyModel"></p></div><button class="btn xs" id="policyClose">{{tr('关闭')}}</button></div>
      <div v-once class="field-grid">
        <div v-once class="field"><label for="policyContext">{{tr('推荐上下文')}}</label><input id="policyContext" type="number" min="8192" max="2097152" required></div>
        <div v-once class="field"><label for="policyOutput">{{tr('最大输出')}}</label><input id="policyOutput" type="number" min="256" required></div>
        <div v-once class="field"><label for="policyReserve">{{tr('压缩预留')}}</label><input id="policyReserve" type="number" min="4352" required></div>
        <div v-once class="field"><label for="policyKeep">{{tr('保留近期历史')}}</label><input id="policyKeep" type="number" min="512" required></div>
        <div v-once class="field"><label for="policyTrigger">{{tr('压缩触发值')}}</label><input id="policyTrigger" type="number" min="1024" required></div>
        <div v-once class="field"><label for="policySummaryMax">{{tr('摘要输出预算')}}</label><input id="policySummaryMax" type="number" min="256" required></div>
      </div>
      <div v-once class="field"><label for="policyFallback">{{tr('摘要备用模型（留空关闭）')}}</label><select id="policyFallback"></select><p class="hint">{{tr('主模型摘要截断后，最多切换一次；须启用重试。备用模型应能容纳同一段历史，会按其价格计费。')}}</p></div>
      <div v-once class="policy-options"><label><input id="policySummary" type="checkbox">{{tr('启用摘要完整性保护')}}</label><label><input id="policyRetry" type="checkbox">{{tr('截断后最多重试一次')}}</label><label><input id="policyThinking" type="checkbox">{{tr('摘要时关闭思考（须确认模型支持）')}}</label></div>
      <p class="hint">{{tr('只匹配 Pi 摘要系统提示，或客户端显式发送 X-CodeArts-Request-Kind: summary；不自动改写普通编程请求。工具调用请求不按摘要处理。')}}</p>
      <p class="hint" id="policyError" role="alert"></p><div class="row"><button class="btn" id="policyCancel">{{tr('取消')}}</button><button class="btn primary" id="policySave">{{tr('保存策略')}}</button></div>
    </dialog>
</template>
