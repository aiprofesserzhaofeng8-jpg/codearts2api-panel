<script setup lang="ts">
import { t as tr } from '../../i18n';
import { ref } from 'vue';
import UseKeyDialog from './UseKeyDialog.vue';
const guide = ref<InstanceType<typeof UseKeyDialog>>();
function showGuide(e:Event) {
  const {ref:id,name}=(e as CustomEvent<{ref:string,name:string}>).detail;
  guide.value?.open(id,name);
}
</script>
<template>
<UseKeyDialog ref="guide" role="admin" />
<section v-once id="tab-keys" role="tabpanel" aria-labelledby="nav-keys" @key-guide="showGuide">
        <div v-once class="card">
          <div v-once class="card-head">
            <div v-once>
              <h2>{{tr('API 密钥')}}</h2>
            </div>
            <div v-once class="card-actions">
              <button class="btn primary" id="addKey" aria-haspopup="dialog"><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14"/></svg>{{tr('创建密钥')}}</button>
            </div>
          </div>
          <div v-once class="table-wrap">
            <table class="admin-record-table">
              <thead><tr><th>{{tr('名称')}}</th><th>{{tr('API 密钥')}}</th><th>{{tr('分组')}}</th><th>{{tr('状态')}}</th><th>{{tr('请求数')}}</th><th>{{tr('创建时间')}}</th><th>{{tr('操作')}}</th></tr></thead>
              <tbody id="keys"></tbody>
            </table>
          </div>
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
  <p class="hint">{{tr('0 表示不限。请求次数在调用前预留；Token / USD 按上游用量结算，已准入的并发请求可能共同超额。摘要重试分别计入 Token 和费用。USD 使用价格表估算，不等于华为积分账单。')}}</p>
  <label><input id="budgetReset" type="checkbox">{{tr('重置已用预算（保留历史用量记录）')}}</label>
  <p id="keyBudgetError" class="hint" role="alert"></p><button class="btn primary" id="keyBudgetSave">{{tr('保存预算')}}</button>
</dialog>
<dialog v-once id="keyCreateDialog" class="modal key-create-modal" aria-labelledby="keyCreateTitle">
  <div v-once class="modal-head"><h2 id="keyCreateTitle">{{tr('创建 API 密钥')}}</h2><button class="btn ghost" id="keyCreateClose" type="button">{{tr('关闭')}}</button></div>
  <form id="keyCreateForm">
    <div v-once class="modal-body key-create-fields">
      <div v-once class="field"><label for="keyName">{{tr('名称')}} <span class="muted">{{tr('（可选）')}}</span></label><input id="keyName" :placeholder="tr('例如：日常开发')" autocomplete="off" maxlength="80"></div>
      <div v-once class="field"><label for="keyCreateGroup">{{tr('账号分组')}}</label><select id="keyCreateGroup" required><option value="" disabled>{{tr('选择分组')}}</option></select></div>
      <p id="keyCreateError" class="err-text" role="alert"></p>
    </div>
    <div v-once class="modal-actions"><button class="btn" id="keyCreateCancel" type="button">{{tr('取消')}}</button><button class="btn primary" id="keyCreateSave" type="submit">{{tr('创建密钥')}}</button></div>
  </form>
</dialog>
<dialog v-once id="keyGroupDialog" class="modal modal-small" aria-labelledby="keyGroupTitle">
  <div v-once class="modal-head"><h2 id="keyGroupTitle">{{tr('密钥分组')}}</h2><button class="btn ghost" id="keyGroupClose">{{tr('关闭')}}</button></div>
  <div v-once class="modal-body"><p id="keyGroupName"></p><div class="field"><label for="keyGroupSelect">{{tr('可访问的账号分组')}}</label><select id="keyGroupSelect" required></select></div><p id="keyGroupError" class="err-text" role="alert"></p></div>
  <div v-once class="modal-actions"><button class="btn" id="keyGroupCancel">{{tr('取消')}}</button><button class="btn primary" id="keyGroupSave">{{tr('保存')}}</button></div>
</dialog>
</template>
