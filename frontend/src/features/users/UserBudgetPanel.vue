<script setup lang="ts">
import { t as tr } from '../../i18n';

import type { components } from '../../api/generated/admin';
import UserIcon from './UserIcon.vue';
import { userNumber, userUSD } from './user-format';
defineProps<{user:components['schemas']['User']}>();
</script>
<template>
  <section class="portal-card portal-budget">
    <div class="portal-section-heading"><div><h2>{{tr('账户用量与限额')}}</h2><p>{{tr('所有 API 密钥共享以下累计额度。')}}</p></div><span class="portal-icon-surface"><UserIcon name="shield" /></span></div>
    <div class="portal-budget-grid">
      <div><span>{{tr('请求次数')}}</span><strong>{{userNumber(user.budget_usage.requests)}} <small>/ {{user.budget.max_requests?userNumber(user.budget.max_requests):tr('不限')}}</small></strong><progress v-if="user.budget.max_requests" :value="user.budget_usage.requests" :max="user.budget.max_requests" :aria-label="tr('请求额度已用比例')" /></div>
      <div><span>{{tr('Token 用量')}}</span><strong>{{userNumber(user.budget_usage.tokens)}} <small>/ {{user.budget.max_tokens?userNumber(user.budget.max_tokens):tr('不限')}}</small></strong><progress v-if="user.budget.max_tokens" :value="user.budget_usage.tokens" :max="user.budget.max_tokens" :aria-label="tr('Token 额度已用比例')" /></div>
      <div><span>{{tr('参考费用')}}</span><strong>{{userUSD(user.budget_usage.usd)}} <small>/ {{user.budget.max_usd?'$'+user.budget.max_usd:tr('不限')}}</small></strong><progress v-if="user.budget.max_usd" :value="user.budget_usage.usd" :max="user.budget.max_usd" :aria-label="tr('费用额度已用比例')" /></div>
    </div>
    <p class="portal-note" id="userConcurrency">{{tr('用户并发：')}}{{user.in_flight}} / {{user.max_concurrency||tr('不限')}}<span v-if="user.budget.max_tokens||user.budget.max_usd">{{tr(' · 当前计量预算最多允许一个在途请求')}}</span></p>
    <p v-if="user.budget_usage.incomplete || user.budget_usage.unpriced" class="portal-notice">{{tr('存在未报告用量或未配置价格的消耗；相应预算受限时，请联系管理员核查。')}}</p>
  </section>
</template>
