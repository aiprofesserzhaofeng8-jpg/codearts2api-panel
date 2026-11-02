<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue';
import ConsoleBrand from '../layouts/ConsoleBrand.vue';
import ThemeToggle from '../layouts/ThemeToggle.vue';
import { api, isAbortError } from '../api/client';

const step = ref(1);
const token = ref('');
const password = ref('');
const confirmation = ref('');
const error = ref('');
const busy = ref(false);
const serviceReady = ref(false);
let timer: ReturnType<typeof setTimeout> | undefined;
let disposed = false;
let checks = 0;

onMounted(() => { document.title = '首次安装 · CodeArts2API'; });
onUnmounted(() => {
  disposed = true;
  clearTimeout(timer);
  token.value = password.value = confirmation.value = '';
});

function login() { window.location.assign('/login'); }

function review() {
  error.value = '';
  if (password.value !== confirmation.value) {
    error.value = '两次输入的密码不一致';
    return;
  }
  step.value = 2;
}

async function checkReady() {
  try {
    const status = await api('/setup/status');
    if (disposed) return;
    if (!status.needs_setup && status.ready) { serviceReady.value = true; return; }
  } catch { if (disposed) return; }
  if (++checks < 30) timer = setTimeout(checkReady, 500);
  else error.value = '安装信息已保存，服务仍在启动。请稍后刷新页面进入登录。';
}

async function install() {
  if (busy.value) return;
  busy.value = true;
  error.value = '';
  try {
    await api('/setup/install', { method: 'POST', body: {
      setup_token: token.value.trim(), password: password.value, confirm_password: confirmation.value,
    } });
    showComplete();
    // The temporary installer drains its response before starting the normal
    // server. Poll only during this one transition, never after login.
    await checkReady();
  } catch (e) {
    if (!isAbortError(e)) {
      // A response may be lost after the durable commit. Reconcile the state
      // before asking the owner to retry an installation that already finished.
      try {
        const status = await api('/setup/status');
        if (!status.needs_setup) { showComplete(); await checkReady(); return; }
      } catch { /* Keep the original error when the service is unreachable. */ }
      error.value = (e as Error).message;
    }
  } finally { busy.value = false; }
}

function showComplete() {
  token.value = password.value = confirmation.value = '';
  step.value = 3;
}
</script>

<template>
  <div class="auth-page setup-page">
    <div class="auth-theme"><ThemeToggle /></div>
    <main class="auth-card setup-card">
      <ConsoleBrand />
      <div class="auth-heading"><h1>欢迎使用 CodeArts2API</h1><p class="hint">完成首次安装，创建你的管理员登录密码。</p></div>
      <ol class="setup-steps" aria-label="安装步骤">
        <li v-for="(title, index) in ['设置管理员', '确认安装', '安装完成']" :key="title" :class="{active: step === index + 1, done: step > index + 1}" :aria-current="step === index + 1 ? 'step' : undefined"><span>{{ step > index + 1 ? '✓' : index + 1 }}</span>{{ title }}</li>
      </ol>
      <form v-if="step === 1" id="setupForm" @submit.prevent="review">
        <div class="field"><label for="setupToken">初始化凭证</label><input id="setupToken" v-model="token" type="password" required minlength="32" maxlength="256" autocomplete="off" placeholder="粘贴服务器上的初始化凭证"><p class="hint">由本次部署生成，保存在状态文件旁的 <code>*.setup.json</code> 文件中。它仅用于这次安装。</p></div>
        <div class="field"><label for="setupUsername">管理员用户名</label><input id="setupUsername" value="admin" readonly autocomplete="username"><p class="hint">使用 admin 登录管理界面，普通用户由管理员创建。</p></div>
        <div class="field"><label for="setupPassword">管理员密码</label><input id="setupPassword" v-model="password" type="password" required minlength="12" maxlength="256" autocomplete="new-password" placeholder="至少 12 位，建议包含字母与数字"></div>
        <div class="field"><label for="setupConfirm">确认密码</label><input id="setupConfirm" v-model="confirmation" type="password" required minlength="12" maxlength="256" autocomplete="new-password" placeholder="再次输入管理员密码"></div>
        <button class="btn primary">下一步</button>
      </form>
      <section v-else-if="step === 2" class="setup-review" aria-labelledby="setupReviewTitle">
        <h2 id="setupReviewTitle">确认安装信息</h2>
        <dl><div><dt>管理员账号</dt><dd>admin</dd></div><div><dt>登录密码</dt><dd>已设置</dd></div><div><dt>普通用户</dt><dd>由管理员创建</dd></div></dl>
        <p class="hint">完成后，初始化入口将关闭。请妥善保存你刚刚设置的密码。</p>
        <div class="setup-actions"><button class="btn" :disabled="busy" @click="step = 1; error = ''">上一步</button><button class="btn primary" :disabled="busy" @click="install">{{ busy ? '正在安装…' : '完成安装' }}</button></div>
      </section>
      <section v-else class="setup-complete" aria-labelledby="setupCompleteTitle">
        <span class="setup-success-icon" aria-hidden="true">✓</span><h2 id="setupCompleteTitle">安装完成</h2>
        <p class="hint">使用 admin 和你设置的密码登录，然后添加上游账号、创建分组和普通用户。</p>
        <button class="btn primary" :disabled="!serviceReady" @click="login">{{ serviceReady ? '前往登录' : '正在启动服务…' }}</button>
      </section>
      <p v-if="error" class="err-text setup-error" role="alert">{{ error }}</p>
      <p class="setup-footer">初始化凭证与登录密码仅用于当前部署。</p>
    </main>
  </div>
</template>

<style scoped>
.setup-card{max-width:600px;padding:32px}.setup-steps{display:flex;gap:14px;list-style:none;margin:0 0 28px;padding:0}.setup-steps li{display:flex;align-items:center;gap:8px;flex:1;color:var(--text-3);font-size:12px;white-space:nowrap}.setup-steps li>span{display:grid;place-items:center;width:28px;height:28px;flex-shrink:0;border-radius:50%;background:var(--surface-3);font-weight:600}.setup-steps li.active{color:var(--accent-text);font-weight:600}.setup-steps li.active>span,.setup-steps li.done>span{background:var(--accent-solid);color:var(--accent-on-solid)}.setup-page .field .hint{font-size:12px;line-height:1.8;margin:8px 0 0}.setup-review h2,.setup-complete h2{font-size:20px;margin:0 0 18px}.setup-review dl{margin:0 0 20px;border:1px solid var(--line);border-radius:12px;padding:6px 18px}.setup-review dl>div{display:flex;justify-content:space-between;gap:16px;padding:14px 0;border-bottom:1px solid var(--line);font-size:13px}.setup-review dl>div:last-child{border:0}.setup-review dt{color:var(--text-3)}.setup-review dd{margin:0}.setup-actions{display:flex;gap:12px;margin-top:24px}.setup-actions>.primary{flex:1}.setup-complete{text-align:center}.setup-complete .hint{line-height:1.8}.setup-complete>.btn{width:100%;margin-top:20px}.setup-success-icon{display:grid;place-items:center;width:64px;height:64px;border-radius:50%;margin:12px auto 24px;background:var(--ok-soft);color:var(--ok);font-size:32px}.setup-error{margin:20px 0 0;overflow-wrap:anywhere}.setup-footer{border-top:1px solid var(--line);margin:26px 0 0;padding-top:18px;font-size:12px;text-align:center;color:var(--text-3)}@media(max-width:520px){.setup-card{padding:24px 20px}.setup-steps{gap:8px}.setup-steps li{flex-direction:column;gap:7px;font-size:11px}.setup-page .auth-heading h1{font-size:21px}}
</style>
