<template>
<dialog v-once id="growthDialog" class="modal growth-modal" aria-labelledby="growthTitle">
  <div v-once class="modal-head"><div><h2 id="growthTitle">成长任务</h2><p id="growthAccountName"></p></div><button class="btn ghost" id="growthClose" type="button">关闭</button></div>
  <div v-once class="modal-body">
    <div v-once class="field hidden" id="growthAccountPicker"><label for="growthAccountSelect">账号</label><select id="growthAccountSelect" aria-label="选择成长任务账号"></select></div>
    <p id="growthSummary" class="hint" role="status" aria-live="polite"></p>
    <div v-once id="growthTaskList" aria-busy="false"></div>
  </div>
  <div v-once class="growth-footer">
    <label class="check hidden growth-buddy-consent" id="growthBuddyConsent"><input id="growthBuddyAgree" type="checkbox"><span>我已阅读并同意<a href="https://www.workbuddy.cn/profile/growth-center" target="_blank" rel="noopener noreferrer">官方活动规则</a>，为当前所选账号领养 Buddy</span></label>
    <p id="growthNotice" class="hint" role="status" aria-live="polite"></p>
    <p id="growthError" class="err-text" role="alert"></p>
    <div v-once class="modal-actions"><button class="btn" id="growthRefresh" type="button">刷新进度</button><button class="btn" id="growthAcceptAll" type="button" disabled>接取未报名任务</button><button class="btn" id="growthRunAll" type="button" disabled>执行对话任务</button><button class="btn primary" id="growthClaimAll" type="button" disabled>领取可领奖励</button></div>
  </div>
</dialog>
</template>
