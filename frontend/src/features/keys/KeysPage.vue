<script setup lang="ts">
import { t as tr } from '../../i18n';
</script>
<template>
<section v-once id="tab-keys" role="tabpanel" aria-labelledby="nav-keys">
        <div v-once class="card">
          <div v-once class="card-head">
            <div v-once>
              <h2>{{tr('API 密钥')}}</h2>
              <p class="card-sub">{{tr('密钥保存在本机，可随时显示或一键复制，刷新页面后仍可使用')}}</p>
            </div>
            <div v-once class="card-actions">
              <button class="btn primary" id="addKey" aria-haspopup="dialog"><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14"/></svg>{{tr('创建密钥')}}</button>
            </div>
          </div>
          <div v-once class="table-wrap">
            <table>
              <thead><tr><th>{{tr('密钥')}}</th><th>{{tr('备注')}}</th><th>{{tr('状态')}}</th><th>{{tr('请求数')}}</th><th>{{tr('创建时间')}}</th><th>{{tr('最近使用')}}</th><th>{{tr('操作')}}</th></tr></thead>
              <tbody id="keys"></tbody>
            </table>
          </div>
          <p class="hint">{{tr(' 点击「复制」获取完整密钥；点击「显示」可查看或手动选中复制。')}}<br>{{tr(' 未签发任何密钥时，')}}<code>/v1/*</code>{{tr(' 仅允许')}}<b>{{tr('本机直连')}}</b>{{tr('（可在「设置 → 访问控制」里放开）。 ')}}</p>
        </div>
      </section>
<dialog v-once id="keyBudgetDialog" class="modal" aria-labelledby="keyBudgetTitle">
  <div v-once class="card-head"><h2 id="keyBudgetTitle">{{tr('密钥预算')}}</h2><button class="btn xs" id="keyBudgetClose">{{tr('关闭')}}</button></div>
  <p id="keyBudgetUsage" class="hint"></p>
  <div v-once class="field-grid">
    <div v-once class="field"><label for="budgetRequests">{{tr('请求次数上限')}}</label><input id="budgetRequests" type="number" min="0" step="1" required></div>
    <div v-once class="field"><label for="budgetTokens">{{tr('Token 上限（输入 + 输出）')}}</label><input id="budgetTokens" type="number" min="0" step="1" required></div>
    <div v-once class="field"><label for="budgetUSD">{{tr('参考费用上限（USD）')}}</label><input id="budgetUSD" type="number" min="0" step="any" required></div>
  </div>
  <p class="hint">{{tr('0 表示不限。请求次数在调用前预留；Token / USD 按上游用量结算，单次请求可能超额，有这两项限额时每个密钥只允许一个在途请求。摘要重试分别计入 Token 和费用。USD 使用价格表估算，不等于华为积分账单。')}}</p>
  <label><input id="budgetReset" type="checkbox">{{tr('重置已用预算（保留历史用量记录）')}}</label>
  <p id="keyBudgetError" class="hint" role="alert"></p><button class="btn primary" id="keyBudgetSave">{{tr('保存预算')}}</button>
</dialog>
<dialog v-once id="keyCreateDialog" class="modal key-create-modal" aria-labelledby="keyCreateTitle">
  <div v-once class="modal-head"><h2 id="keyCreateTitle">{{tr('创建 API 密钥')}}</h2><button class="btn ghost" id="keyCreateClose" type="button">{{tr('关闭')}}</button></div>
  <form id="keyCreateForm">
    <div v-once class="modal-body key-create-fields">
      <div v-once class="field"><label for="keyName">{{tr('密钥备注 ')}}<span class="muted">{{tr('（可选）')}}</span></label><input id="keyName" :placeholder="tr('例如：日常开发')" autocomplete="off" maxlength="80"></div>
      <div v-once class="field"><label for="keyCreateGroup">{{tr('账号分组')}}</label><select id="keyCreateGroup"><option value="">{{tr('不分组（全部账号）')}}</option></select></div>
      <p id="keyCreateError" class="err-text" role="alert"></p>
    </div>
    <div v-once class="modal-actions"><button class="btn" id="keyCreateCancel" type="button">{{tr('取消')}}</button><button class="btn primary" id="keyCreateSave" type="submit">{{tr('创建密钥')}}</button></div>
  </form>
</dialog>
<dialog v-once id="keyGroupDialog" class="modal modal-small" aria-labelledby="keyGroupTitle">
  <div v-once class="modal-head"><h2 id="keyGroupTitle">{{tr('密钥分组')}}</h2><button class="btn ghost" id="keyGroupClose">{{tr('关闭')}}</button></div>
  <div v-once class="modal-body"><p id="keyGroupName"></p><div class="field"><label for="keyGroupSelect">{{tr('可访问的账号分组')}}</label><select id="keyGroupSelect"></select></div><p class="hint">{{tr('绑定后，模型列表和请求调度均受分组约束。不分组的密钥可使用全部账号。')}}</p><p id="keyGroupError" class="err-text" role="alert"></p></div>
  <div v-once class="modal-actions"><button class="btn" id="keyGroupCancel">{{tr('取消')}}</button><button class="btn primary" id="keyGroupSave">{{tr('保存')}}</button></div>
</dialog>
</template>
