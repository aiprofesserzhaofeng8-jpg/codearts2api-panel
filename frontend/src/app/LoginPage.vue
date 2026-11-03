<script setup lang="ts">
import { t as tr } from '../i18n';

import { onMounted, onUnmounted, ref } from 'vue';
import AuthCard from '../layouts/AuthCard.vue';
import ThemeToggle from '../layouts/ThemeToggle.vue';
import LocaleSwitcher from '../layouts/LocaleSwitcher.vue';
import UserRecovery from '../features/users/UserRecovery.vue';
import { api, isAbortError } from '../api/client';
import { loginNotice, setPrincipal, type Principal } from './authentication';
const username = ref(''), password = ref(''), error = ref(''), busy = ref(false);
onMounted(() => { document.title = tr('登录 · CodeArts2API'); });
onUnmounted(() => { password.value = ''; });
async function login() {
  if (busy.value) return;
  busy.value = true; error.value = '';
  try {
    const result = await api<{user: Principal}>('/auth/login', {method:'POST', body:{username:username.value, password:password.value}});
    password.value = '';
    setPrincipal(result.user);
  } catch(e) { if (!isAbortError(e)) error.value = (e as Error).message; }
  finally { busy.value = false; }
}
</script>
<template>
  <div class="auth-page">
    <div class="auth-theme"><LocaleSwitcher /><ThemeToggle /></div>
    <AuthCard :title="tr('欢迎回来')" :description="tr('登录您的账号以继续')">
      <form id="loginForm" @submit.prevent="login">
        <div class="field"><label for="loginUsername">{{tr('用户名')}}</label><input id="loginUsername" v-model="username" required autocomplete="username" maxlength="64"></div>
        <div class="field"><label for="loginPassword">{{tr('密码')}}</label><input id="loginPassword" v-model="password" required type="password" autocomplete="current-password" maxlength="4096"></div>
        <button class="btn primary" :disabled="busy">{{busy ? tr('登录中…') : tr('登录')}}</button>
      </form>
      <UserRecovery />
      <p v-if="error" class="err-text" role="alert">{{error}}</p>
      <p v-if="loginNotice" role="status">{{loginNotice}}</p>
    </AuthCard>
  </div>
</template>
