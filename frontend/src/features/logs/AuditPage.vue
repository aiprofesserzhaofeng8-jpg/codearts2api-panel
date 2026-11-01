<template>
<section v-once id="tab-audit" role="tabpanel" aria-labelledby="nav-audit">
        <div v-once class="card"><div class="card-head"><div><h2>管理操作审计</h2><p class="card-sub">保留最近 5000 条；记录操作与结果，不记录密码、凭据或请求正文</p></div><button class="btn" id="reloadAudit">刷新</button></div>
        <div v-once class="table-wrap"><table><thead><tr><th>时间</th><th>身份</th><th>操作</th><th>接口</th><th>来源</th><th>结果</th></tr></thead><tbody id="auditRows"></tbody></table></div>
        <div v-once class="row"><button class="btn xs" id="auditPrev">上一页</button><span id="auditPage" class="hint"></span><button class="btn xs" id="auditNext">下一页</button></div></div>
      </section>
</template>
