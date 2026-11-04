<script setup lang="ts">
import { t as tr } from '../../i18n';

import ProviderAuthorization from './providers/ProviderAuthorization.vue';
import CodeArtsAuthorization from './providers/CodeArtsAuthorization.vue';
import CredentialImport from './providers/CredentialImport.vue';
</script>
<template>
<section v-once id="tab-accounts" role="tabpanel" aria-labelledby="nav-accounts">
        <div v-once class="pool-stats" id="poolStats" :aria-label="tr('账号池状态概览')"></div>
        <div v-once class="connection-strip">
          <div v-once class="connection-info"><span class="connection-dot"></span><span>{{tr('API 接入地址')}}</span><code id="apiBaseURL"></code></div>
          <button class="btn xs ghost" id="copyBaseURL">{{tr('复制地址')}}</button>
        </div>
        <div v-once class="card">
          <div v-once class="card-head">
            <div v-once>
              <h2>{{tr('账号管理 ')}}<span class="heading-count" id="accountCount">{{tr('0 个账号')}}</span></h2>
            </div>
            <div v-once class="card-actions">
              <button class="btn primary" id="addAccount">{{tr('＋ 添加账号')}}</button>
              <button class="btn hidden" id="accountGrowth" type="button">{{tr('成长任务')}}</button>
              <button class="btn" id="checkinAll">{{tr('全部探活')}}</button>
              <button class="btn" id="refreshCredits">{{tr('刷新额度')}}</button>
              <button class="btn" id="claimAll">{{tr('领取活动福利')}}</button>
            </div>
          </div>

          <div v-once class="card hidden subpanel" id="exportPanel">
            <div v-once class="card-head">
              <div v-once>
                <h2>{{tr('导出 session JSON')}}</h2>
                <p class="card-sub" id="exportMeta"></p>
              </div>
              <div v-once class="card-actions">
                <button class="btn primary" id="copyExport">{{tr('复制')}}</button>
                <button class="btn danger" id="closeExport">{{tr('关闭')}}</button>
              </div>
            </div>
            <textarea class="w-full tall" id="exportJSON" readonly data-select-on-click="1"
                      :aria-label="tr('导出的 session JSON')"></textarea>
            <div v-once class="callout">
              <span class="callout-title">{{tr('注意')}}</span>
              <span>{{tr('该内容等同于账号登录态：包含上游访问令牌和续期凭据。 不要外传、不要提交到版本库、不要贴到聊天里。')}}</span>
            </div>
          </div>

          <div v-once class="account-toolbar">
            <label><select id="accountProvider" :aria-label="tr('按上游筛选账号')"><option value="">{{tr('全部上游')}}</option><option value="codearts">{{tr('华为云码道')}}</option><option value="workbuddy">WorkBuddy</option><option value="trae">Trae CN / SOLO</option><option value="qoder">Qoder</option><option value="kiro">Kiro</option><option value="chatgpt">ChatGPT / Codex</option><option value="claude">Claude</option></select></label>
            <label class="account-search"><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/></svg><input id="accountSearch" :placeholder="tr('搜索账号名称或来源')" :aria-label="tr('搜索账号')"></label>
            <div v-once class="account-filters" role="group" :aria-label="tr('账号状态筛选')">
              <button class="filter-chip on" data-account-filter="all" aria-pressed="true">{{tr('全部')}}</button>
              <button class="filter-chip" data-account-filter="ready" aria-pressed="false">{{tr('可调度')}}</button>
              <button class="filter-chip" data-account-filter="cooling" aria-pressed="false">{{tr('冷却中')}}</button>
              <button class="filter-chip" data-account-filter="paused" aria-pressed="false">{{tr('已暂停')}}</button>
              <button class="filter-chip" data-account-filter="disabled" aria-pressed="false">{{tr('已停用')}}</button>
            </div>
            <span class="filter-count" id="accountFilterCount" aria-live="polite"></span>
          </div>
          <div v-once class="table-wrap account-table-wrap">
            <table class="account-table">
              <thead><tr><th>{{tr('名称')}}</th><th>{{tr('来源')}}</th><th>AK</th><th>{{tr('凭据有效期')}}</th><th>{{tr('积分额度')}}</th><th>{{tr('状态')}}</th><th>{{tr('今日领取')}}</th><th>{{tr('失败')}}</th><th>{{tr('最近使用')}}</th><th>{{tr('操作')}}</th></tr></thead>
              <tbody id="accounts"></tbody>
            </table>
          </div>
        </div>

        <div v-once class="card">
          <div v-once class="card-head">
            <div v-once>
              <h2>{{tr('活动福利')}}</h2>
            </div>
            <div v-once class="card-actions">
              <label class="inline-field">
                <span>{{tr('账号')}}</span>
                <select id="welfareAccount" :aria-label="tr('选择要查看活动福利的账号')"></select>
              </label>
              <button class="btn" id="loadWelfare">{{tr('刷新列表')}}</button>
              <button class="btn" id="claimWelfareNow">{{tr('领取可领奖励')}}</button>
              <button class="btn" id="genInvite">{{tr('生成邀请链接')}}</button>
              <span class="hint m-0" id="welfareHint"></span>
            </div>
          </div>
          <div v-once class="table-wrap">
            <table>
              <thead><tr><th>{{tr('活动')}}</th><th>{{tr('类型')}}</th><th>{{tr('奖励')}}</th><th>{{tr('状态')}}</th><th>{{tr('可自动领取')}}</th><th>{{tr('触发方式')}}</th><th>{{tr('有效期')}}</th></tr></thead>
              <tbody id="welfare"></tbody>
            </table>
          </div>
          <p class="hint">{{tr(' 只有')}}<b>{{tr('靠时间推进')}}</b>{{tr('的活动（如「每日签到领 1000 积分」）可以自动领取； ')}}<b>{{tr('邀请')}}</b>{{tr('与')}}<b>{{tr('学生认证')}}</b>{{tr('类必须由真实事件触发，服务端会直接拒绝直接领取。 ')}}</p>
          <div v-once class="card hidden mt-12 subpanel" id="invitePanel">
            <div v-once class="card-head">
              <div v-once>
                <h2>{{tr('我的邀请链接')}}</h2>
                <p class="card-sub" id="inviteStat"></p>
              </div>
              <div v-once class="card-actions">
                <button class="btn" id="copyInvite">{{tr('复制')}}</button>
              </div>
            </div>
            <input class="w-full" id="inviteURL" readonly data-select-on-click="1" :aria-label="tr('邀请链接')">
          </div>
        </div>

      </section>
<dialog v-once id="accountConcurrencyDialog" class="modal modal-small" aria-labelledby="accountConcurrencyTitle">
  <div v-once class="modal-head"><h2 id="accountConcurrencyTitle">{{tr('账号并发设置')}}</h2><button class="btn ghost" id="accountConcurrencyClose">{{tr('关闭')}}</button></div>
  <div v-once class="modal-body">
    <p id="accountConcurrencyName"></p>
    <label class="check"><input id="accountConcurrencyDefault" type="checkbox">{{tr('使用全局默认值')}}</label>
    <p class="hint" id="accountConcurrencyDefaultHint"></p>
    <div v-once class="field"><label for="accountConcurrencyLimit">{{tr('独立并发上限')}}</label><input id="accountConcurrencyLimit" type="number" min="1" max="32" step="1"></div>
    <p class="hint">{{tr('独立设置覆盖全局默认值。保存后立即影响新请求，正在处理的请求继续完成。')}}</p>
    <p id="accountConcurrencyError" class="err-text" role="alert"></p>
  </div>
  <div v-once class="modal-actions"><button class="btn" id="accountConcurrencyCancel">{{tr('取消')}}</button><button class="btn primary" id="accountConcurrencySave">{{tr('保存')}}</button></div>
</dialog>
<dialog v-once id="addAccountDialog" class="modal account-create-modal" aria-labelledby="addAccountTitle">
  <div v-once class="modal-head"><h2 id="addAccountTitle">{{tr('添加账号')}}</h2><button class="btn ghost" id="addAccountClose" :aria-label="tr('关闭添加账号窗口')">{{tr('关闭')}}</button></div>
  <div v-once class="account-create-body">
    <div v-once class="account-create-steps" :aria-label="tr('添加账号步骤')"><span id="accountChooseStep" class="active"><b>1</b>{{tr(' 选择平台与方式')}}</span><i aria-hidden="true"></i><span id="accountAuthorizeStep"><b>2</b> <span id="accountAuthorizeLabel">{{tr('账号授权')}}</span></span></div>
    <div v-once id="accountChoose">
      <div v-once class="field"><label for="accountCreateName">{{tr('账号名称（可选）')}}</label><input id="accountCreateName" autocomplete="off" :placeholder="tr('输入便于识别的账号名称')"></div>
      <p class="account-field-label" id="accountPlatformLabel">{{tr('平台')}}</p>
      <div v-once class="account-platforms" role="group" aria-labelledby="accountPlatformLabel">
        <button id="browserLogin" class="account-platform selected" data-account-platform="codearts" aria-pressed="true"><span class="platform-symbol" aria-hidden="true">C</span><span>{{tr('华为云码道')}}</span></button>
        <button id="workbuddyLogin" class="account-platform" data-account-platform="workbuddy" aria-pressed="false"><span class="platform-symbol" aria-hidden="true">W</span><span>WorkBuddy</span></button>
        <button id="traeLogin" class="account-platform" data-account-platform="trae" aria-pressed="false"><span class="platform-symbol" aria-hidden="true">T</span><span>Trae CN / SOLO</span></button>
        <button id="kiroLogin" class="account-platform" data-account-platform="kiro" aria-pressed="false"><span class="platform-symbol" aria-hidden="true">K</span><span>Kiro</span></button>
        <button id="qoderLogin" class="account-platform" data-account-platform="qoder" aria-pressed="false"><span class="platform-symbol" aria-hidden="true">Q</span><span>Qoder</span></button>
        <button id="chatgptLogin" class="account-platform" data-account-platform="chatgpt" aria-pressed="false"><span class="platform-symbol" aria-hidden="true">G</span><span>ChatGPT / Codex</span></button>
        <button id="claudeLogin" class="account-platform" data-account-platform="claude" aria-pressed="false"><span class="platform-symbol" aria-hidden="true">A</span><span>Claude</span></button>
      </div>
      <p class="account-field-label" id="accountMethodLabel">{{tr('添加方式')}}</p>
      <div v-once class="account-methods" role="group" aria-labelledby="accountMethodLabel">
        <button id="accountMethodBrowser" class="account-method selected" data-account-method="browser" aria-pressed="true"><span class="method-symbol" aria-hidden="true">↗</span><span><strong>{{tr('浏览器授权')}}</strong><small id="accountBrowserHint">{{tr('通过官方页面登录并授权')}}</small></span></button>
        <button id="accountMethodImport" class="account-method" data-account-method="import" aria-pressed="false"><span class="method-symbol" aria-hidden="true">＋</span><span><strong>{{tr('凭据导入')}}</strong><small id="accountImportHint">{{tr('本机客户端、STS / AK/SK 或 session JSON')}}</small></span></button>
      </div>
      <p class="hint account-choice-hint" id="accountChoiceHint">{{tr('选择平台和添加方式后继续，账号核对完成后再保存。')}}</p>
    </div>
    <div v-once id="accountAuthorize" class="hidden">
<ProviderAuthorization />
<CodeArtsAuthorization />
        <CredentialImport />

    </div>
    <p id="accountCreateError" class="err-text" role="alert"></p>
  </div>
  <div v-once class="modal-actions account-create-actions"><button class="btn hidden" id="accountCreateBack">{{tr('上一步')}}</button><button class="btn" id="accountCreateCancel">{{tr('取消')}}</button><button class="btn primary" id="accountCreateNext">{{tr('下一步')}}</button></div>
</dialog>
<dialog v-once id="modelHealthDialog" class="modal" aria-labelledby="modelHealthTitle">
  <div v-once class="card-head"><h2 id="modelHealthTitle">{{tr('账号模型健康')}}</h2><button class="btn xs" id="modelHealthClose">{{tr('关闭')}}</button></div>
  <p class="hint">{{tr('某个模型限流或暂不可用时单独冷却。账号凭据失效仍对整个账号生效。')}}</p>
  <div v-once class="table-wrap"><table><thead><tr><th>{{tr('模型')}}</th><th>{{tr('连续失败')}}</th><th>{{tr('状态')}}</th><th>{{tr('最近检查')}}</th><th>{{tr('原因')}}</th></tr></thead><tbody id="modelHealthRows"></tbody></table></div>
</dialog>
<dialog v-once id="deleteAccountDialog" class="modal modal-small" aria-labelledby="deleteAccountTitle">
  <div v-once class="modal-head"><h2 id="deleteAccountTitle">{{tr('删除账号')}}</h2></div>
  <div v-once class="modal-body"><p>{{tr('确定从账号池删除 ')}}<strong id="deleteAccountName"></strong>？</p><p class="hint">{{tr('将移除网关保存的登录凭据；上游账号本身不会被注销。需要时可重新添加。')}}</p><p id="deleteAccountError" class="err-text hidden" role="alert"></p></div>
  <div v-once class="modal-actions"><button class="btn" id="deleteAccountCancel" autofocus>{{tr('取消')}}</button><button class="btn danger" id="deleteAccountConfirm">{{tr('确认删除')}}</button></div>
</dialog>
<div v-once id="accountActionMenu" class="account-action-menu" popover="auto" role="group" :aria-label="tr('账号操作')"></div>
</template>
