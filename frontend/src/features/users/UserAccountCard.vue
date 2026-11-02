<script setup lang="ts">
import type { components } from '../../api/generated/admin';
import { credits } from './consumer-format';
defineProps<{user:components['schemas']['User']; groupName?:string}>();
</script>
<template>
  <section class="portal-account">
    <img v-if="user.avatar" :src="user.avatar" class="user-avatar large" alt="用户头像">
    <span v-else class="user-avatar large" aria-label="默认头像">{{(user.display_name||user.username).slice(0,1).toUpperCase()}}</span>
    <div class="portal-account-body">
      <div class="portal-account-name"><h2>{{user.display_name||user.username}}</h2><span class="portal-badge">用户</span><span class="portal-badge" :class="user.enabled?'success':'danger'">{{user.enabled?'启用':'停用'}}</span></div>
      <p class="portal-muted">{{user.email || user.username}}<span v-if="groupName"> · {{groupName}}</span></p>
      <div class="portal-account-metrics">
        <div><span>账户余额</span><strong>{{user.wallet.enabled?'$'+credits(user.wallet.balance):'未启用余额限制'}}</strong></div>
        <div><span>并发限制</span><strong>{{user.max_concurrency||'不限'}}</strong></div>
        <div><span>注册时间</span><strong>{{new Date(user.created_at*1000).toLocaleDateString('zh-CN',{year:'numeric',month:'long'})}}</strong></div>
      </div>
    </div>
  </section>
</template>
