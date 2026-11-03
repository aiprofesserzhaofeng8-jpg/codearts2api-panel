<script setup lang="ts">
import { t as tr, dateLocale } from '../../i18n';

import type { components } from '../../api/generated/admin';
import { credits } from './consumer-format';
defineProps<{user:components['schemas']['User']; groupName?:string}>();
</script>
<template>
  <section class="portal-account">
    <img v-if="user.avatar" :src="user.avatar" class="user-avatar large" :alt="tr('用户头像')">
    <span v-else class="user-avatar large" :aria-label="tr('默认头像')">{{(user.display_name||user.username).slice(0,1).toUpperCase()}}</span>
    <div class="portal-account-body">
      <div class="portal-account-name"><h2>{{user.display_name||user.username}}</h2><span class="portal-badge">{{tr('用户')}}</span><span class="portal-badge" :class="user.enabled?'success':'danger'">{{user.enabled?tr('启用'):tr('停用')}}</span></div>
      <p class="portal-muted">{{user.email || user.username}}<span v-if="groupName"> · {{groupName}}</span></p>
      <div class="portal-account-metrics">
        <div><span>{{tr('账户余额')}}</span><strong>{{user.wallet.enabled?'$'+credits(user.wallet.balance):tr('未启用余额限制')}}</strong></div>
        <div><span>{{tr('并发限制')}}</span><strong>{{user.max_concurrency||tr('不限')}}</strong></div>
        <div><span>{{tr('注册时间')}}</span><strong>{{new Date(user.created_at*1000).toLocaleDateString(dateLocale,{year:'numeric',month:'long'})}}</strong></div>
      </div>
    </div>
  </section>
</template>
