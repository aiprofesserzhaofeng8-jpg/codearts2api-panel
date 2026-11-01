<script setup lang="ts">
import ProviderAuthorization from './providers/ProviderAuthorization.vue';
import CodeArtsAuthorization from './providers/CodeArtsAuthorization.vue';
import CredentialImport from './providers/CredentialImport.vue';
</script>
<template>
<section v-once id="tab-accounts" role="tabpanel" aria-labelledby="nav-accounts">
        <div v-once class="pool-stats" id="poolStats" aria-label="账号池状态概览"></div>
        <div v-once class="connection-strip">
          <div v-once class="connection-info"><span class="connection-dot"></span><span>API 接入地址</span><code id="apiBaseURL"></code></div>
          <button class="btn xs ghost" id="copyBaseURL">复制地址</button>
        </div>
        <div v-once class="card">
          <div v-once class="card-head">
            <div v-once>
              <h2>账号管理 <span class="heading-count" id="accountCount">0 个账号</span></h2>
            </div>
            <div v-once class="card-actions">
              <button class="btn primary" id="addAccount">＋ 添加账号</button>
              <button class="btn hidden" id="accountGrowth" type="button">成长任务</button>
              <button class="btn" id="checkinAll">全部探活</button>
              <button class="btn" id="refreshCredits">刷新额度</button>
              <button class="btn" id="claimAll">领取活动福利</button>
            </div>
          </div>

          <div v-once class="card hidden subpanel" id="exportPanel">
            <div v-once class="card-head">
              <div v-once>
                <h2>导出 session JSON</h2>
                <p class="card-sub" id="exportMeta"></p>
              </div>
              <div v-once class="card-actions">
                <button class="btn primary" id="copyExport">复制</button>
                <button class="btn danger" id="closeExport">关闭</button>
              </div>
            </div>
            <textarea class="w-full tall" id="exportJSON" readonly data-select-on-click="1"
                      aria-label="导出的 session JSON"></textarea>
            <div v-once class="callout">
              <span class="callout-title">注意</span>
              <span>该内容等同于账号登录态：包含上游访问令牌和续期凭据。
                不要外传、不要提交到版本库、不要贴到聊天里。</span>
            </div>
          </div>

          <div v-once class="account-toolbar">
            <label><select id="accountProvider" aria-label="按上游筛选账号"><option value="">全部上游</option><option value="codearts">华为云码道</option><option value="workbuddy">WorkBuddy</option><option value="trae">Trae CN / SOLO</option></select></label>
            <label class="account-search"><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/></svg><input id="accountSearch" placeholder="搜索账号名称或来源" aria-label="搜索账号"></label>
            <div v-once class="account-filters" role="group" aria-label="账号状态筛选">
              <button class="filter-chip on" data-account-filter="all" aria-pressed="true">全部</button>
              <button class="filter-chip" data-account-filter="ready" aria-pressed="false">可调度</button>
              <button class="filter-chip" data-account-filter="cooling" aria-pressed="false">冷却中</button>
              <button class="filter-chip" data-account-filter="paused" aria-pressed="false">已暂停</button>
              <button class="filter-chip" data-account-filter="disabled" aria-pressed="false">已停用</button>
            </div>
            <span class="filter-count" id="accountFilterCount" aria-live="polite"></span>
          </div>
          <div v-once class="table-wrap account-table-wrap">
            <table class="account-table">
              <thead><tr><th>名称</th><th>来源</th><th>AK</th><th>凭据有效期</th><th>积分额度</th><th>状态</th><th>今日领取</th><th>失败</th><th>最近使用</th><th>操作</th></tr></thead>
              <tbody id="accounts"></tbody>
            </table>
          </div>
        </div>

        <div v-once class="card">
          <div v-once class="card-head">
            <div v-once>
              <h2>活动福利</h2>
            </div>
            <div v-once class="card-actions">
              <label class="inline-field">
                <span>账号</span>
                <select id="welfareAccount" aria-label="选择要查看活动福利的账号"></select>
              </label>
              <button class="btn" id="loadWelfare">刷新列表</button>
              <button class="btn" id="claimWelfareNow">领取可领奖励</button>
              <button class="btn" id="genInvite">生成邀请链接</button>
              <span class="hint m-0" id="welfareHint"></span>
            </div>
          </div>
          <div v-once class="table-wrap">
            <table>
              <thead><tr><th>活动</th><th>类型</th><th>奖励</th><th>状态</th><th>可自动领取</th><th>触发方式</th><th>有效期</th></tr></thead>
              <tbody id="welfare"></tbody>
            </table>
          </div>
          <p class="hint">
            只有<b>靠时间推进</b>的活动（如「每日签到领 1000 积分」）可以自动领取；
            <b>邀请</b>与<b>学生认证</b>类必须由真实事件触发，服务端会直接拒绝直接领取。
          </p>
          <div v-once class="card hidden mt-12 subpanel" id="invitePanel">
            <div v-once class="card-head">
              <div v-once>
                <h2>我的邀请链接</h2>
                <p class="card-sub" id="inviteStat"></p>
              </div>
              <div v-once class="card-actions">
                <button class="btn" id="copyInvite">复制</button>
              </div>
            </div>
            <input class="w-full" id="inviteURL" readonly data-select-on-click="1" aria-label="邀请链接">
          </div>
        </div>

      </section>
<dialog v-once id="accountConcurrencyDialog" class="modal modal-small" aria-labelledby="accountConcurrencyTitle">
  <div v-once class="modal-head"><h2 id="accountConcurrencyTitle">账号并发设置</h2><button class="btn ghost" id="accountConcurrencyClose">关闭</button></div>
  <div v-once class="modal-body">
    <p id="accountConcurrencyName"></p>
    <label class="check"><input id="accountConcurrencyDefault" type="checkbox">使用全局默认值</label>
    <p class="hint" id="accountConcurrencyDefaultHint"></p>
    <div v-once class="field"><label for="accountConcurrencyLimit">独立并发上限</label><input id="accountConcurrencyLimit" type="number" min="1" max="32" step="1"></div>
    <p class="hint">独立设置覆盖全局默认值。保存后立即影响新请求，正在处理的请求继续完成。</p>
    <p id="accountConcurrencyError" class="err-text" role="alert"></p>
  </div>
  <div v-once class="modal-actions"><button class="btn" id="accountConcurrencyCancel">取消</button><button class="btn primary" id="accountConcurrencySave">保存</button></div>
</dialog>
<dialog v-once id="addAccountDialog" class="modal account-create-modal" aria-labelledby="addAccountTitle">
  <div v-once class="modal-head"><h2 id="addAccountTitle">添加账号</h2><button class="btn ghost" id="addAccountClose" aria-label="关闭添加账号窗口">关闭</button></div>
  <div v-once class="account-create-body">
    <div v-once class="account-create-steps" aria-label="添加账号步骤"><span id="accountChooseStep" class="active"><b>1</b> 选择平台与方式</span><i aria-hidden="true"></i><span id="accountAuthorizeStep"><b>2</b> <span id="accountAuthorizeLabel">账号授权</span></span></div>
    <div v-once id="accountChoose">
      <div v-once class="field"><label for="accountCreateName">账号名称（可选）</label><input id="accountCreateName" autocomplete="off" placeholder="输入便于识别的账号名称"></div>
      <p class="account-field-label" id="accountPlatformLabel">平台</p>
      <div v-once class="account-platforms" role="group" aria-labelledby="accountPlatformLabel">
        <button id="browserLogin" class="account-platform selected" data-account-platform="codearts" aria-pressed="true"><span class="platform-symbol" aria-hidden="true">C</span><span>华为云码道</span></button>
        <button id="workbuddyLogin" class="account-platform" data-account-platform="workbuddy" aria-pressed="false"><span class="platform-symbol" aria-hidden="true">W</span><span>WorkBuddy</span></button>
        <button id="traeLogin" class="account-platform" data-account-platform="trae" aria-pressed="false"><span class="platform-symbol" aria-hidden="true">T</span><span>Trae CN / SOLO</span></button>
      </div>
      <p class="account-field-label" id="accountMethodLabel">添加方式</p>
      <div v-once class="account-methods" role="group" aria-labelledby="accountMethodLabel">
        <button id="accountMethodBrowser" class="account-method selected" data-account-method="browser" aria-pressed="true"><span class="method-symbol" aria-hidden="true">↗</span><span><strong>浏览器授权</strong><small id="accountBrowserHint">通过官方页面登录并授权</small></span></button>
        <button id="accountMethodImport" class="account-method" data-account-method="import" aria-pressed="false"><span class="method-symbol" aria-hidden="true">＋</span><span><strong>凭据导入</strong><small id="accountImportHint">本机客户端、STS / AK/SK 或 session JSON</small></span></button>
      </div>
      <p class="hint account-choice-hint" id="accountChoiceHint">选择平台和添加方式后继续，账号核对完成后再保存。</p>
    </div>
    <div v-once id="accountAuthorize" class="hidden">
<ProviderAuthorization />
<CodeArtsAuthorization />
        <CredentialImport />

    </div>
    <p id="accountCreateError" class="err-text" role="alert"></p>
  </div>
  <div v-once class="modal-actions account-create-actions"><button class="btn hidden" id="accountCreateBack">上一步</button><button class="btn" id="accountCreateCancel">取消</button><button class="btn primary" id="accountCreateNext">下一步</button></div>
</dialog>
<dialog v-once id="modelHealthDialog" class="modal" aria-labelledby="modelHealthTitle">
  <div v-once class="card-head"><h2 id="modelHealthTitle">账号模型健康</h2><button class="btn xs" id="modelHealthClose">关闭</button></div>
  <p class="hint">某个模型限流或暂不可用时单独冷却。账号凭据失效仍对整个账号生效。</p>
  <div v-once class="table-wrap"><table><thead><tr><th>模型</th><th>连续失败</th><th>状态</th><th>最近检查</th><th>原因</th></tr></thead><tbody id="modelHealthRows"></tbody></table></div>
</dialog>
<dialog v-once id="deleteAccountDialog" class="modal modal-small" aria-labelledby="deleteAccountTitle">
  <div v-once class="modal-head"><h2 id="deleteAccountTitle">删除账号</h2></div>
  <div v-once class="modal-body"><p>确定从账号池删除 <strong id="deleteAccountName"></strong>？</p><p class="hint">将移除网关保存的登录凭据；上游账号本身不会被注销。需要时可重新添加。</p><p id="deleteAccountError" class="err-text hidden" role="alert"></p></div>
  <div v-once class="modal-actions"><button class="btn" id="deleteAccountCancel" autofocus>取消</button><button class="btn danger" id="deleteAccountConfirm">确认删除</button></div>
</dialog>
<div v-once id="accountActionMenu" class="account-action-menu" popover="auto" role="group" aria-label="账号操作"></div>
</template>
