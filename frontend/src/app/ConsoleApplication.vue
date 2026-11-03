<script setup lang="ts">
import { t as tr } from '../i18n';

import { onMounted, onUnmounted, ref, watch } from 'vue';
import App from '../App.vue';
import UserConsole from '../features/users/UserConsole.vue';
import LoginPage from './LoginPage.vue';
import SetupPage from './SetupPage.vue';
import { api, configureClient, disposeClient } from '../api/client';
import { expireSession, principal, setPrincipal, type Principal } from './authentication';
const ready = ref(false);
const needsSetup = ref(false);
// The application owns one API lifetime. Rotate it before mounting a different
// identity, so child mount hooks cannot cancel each other's initial requests.
watch(() => principal.value ? principal.value.id + ':' + principal.value.role : '', () => {
  disposeClient();
  configureClient(expireSession);
}, {flush:'sync'});
watch([ready, principal, needsSetup], () => {
  if (!ready.value) return;
  const path = needsSetup.value ? '/setup' : principal.value?.role === 'admin' ? '/' : principal.value ? '/user' : '/login';
  if (location.pathname !== path) history.replaceState(null, '', path + location.search + location.hash);
}, {flush:'sync'});
onMounted(async () => {
  configureClient(expireSession);
  try {
    const installation = await api('/setup/status');
    needsSetup.value = installation.needs_setup || installation.ready === false;
    if (needsSetup.value) return;
    const session = await api<{authenticated:boolean; user?:Principal}>('/auth/session');
    if (session.authenticated && session.user) setPrincipal(session.user);
    else expireSession();
  } catch { expireSession(tr('无法检查登录状态，请重新登录。')); }
  finally { ready.value = true; }
});
onUnmounted(disposeClient);
</script>
<template>
  <p v-if="!ready" class="auth-page" role="status">{{tr('加载中…')}}</p>
  <SetupPage v-else-if="needsSetup" />
  <LoginPage v-else-if="!principal" />
  <App v-else-if="principal.role === 'admin'" />
  <UserConsole v-else :key="principal.id" :initial-user="principal" />
</template>
