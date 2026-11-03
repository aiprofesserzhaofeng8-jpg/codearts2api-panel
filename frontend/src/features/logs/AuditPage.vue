<script setup lang="ts">
import { t as tr } from '../../i18n';
</script>
<template>
<section v-once id="tab-audit" role="tabpanel" aria-labelledby="nav-audit">
        <div v-once class="card"><div class="card-head"><div><h2>{{tr('管理操作审计')}}</h2><p class="card-sub">{{tr('保留最近 5000 条；记录操作与结果，不记录密码、凭据或请求正文')}}</p></div><button class="btn" id="reloadAudit">{{tr('刷新')}}</button></div>
        <div v-once class="table-wrap"><table><thead><tr><th>{{tr('时间')}}</th><th>{{tr('身份')}}</th><th>{{tr('操作')}}</th><th>{{tr('接口')}}</th><th>{{tr('来源')}}</th><th>{{tr('结果')}}</th></tr></thead><tbody id="auditRows"></tbody></table></div>
        <div v-once class="row"><button class="btn xs" id="auditPrev">{{tr('上一页')}}</button><span id="auditPage" class="hint"></span><button class="btn xs" id="auditNext">{{tr('下一页')}}</button></div></div>
      </section>
</template>
