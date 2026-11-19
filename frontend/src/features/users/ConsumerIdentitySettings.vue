<script setup lang="ts">
import { t as tr } from '../../i18n';

import { computed, onMounted, ref } from 'vue';
import { api } from '../../api/client';
import type { components } from '../../api/generated/admin';
const cfg=ref<components['schemas']['ConsumerIdentitySettings']>();const flags=ref({smtp_password_set:false,github_secret_set:false,linuxdo_secret_set:false});
const clearSMTP=ref(false),clearGitHub=ref(false),clearLinuxDO=ref(false),busy=ref(false),error=ref(''),message=ref('');
type Section='origin'|'smtp'|'github'|'linuxdo';
const sections:Section[]=['origin','smtp','github','linuxdo'];
const names={origin:'公开访问地址',smtp:'邮件服务',github:'GitHub',linuxdo:'LinuxDO'};
const editing=ref<Section>(),snapshot=ref<components['schemas']['ConsumerIdentitySettings']>();
function close(){editing.value=undefined;cfg.value=JSON.parse(JSON.stringify(snapshot.value));clearSMTP.value=clearGitHub.value=clearLinuxDO.value=false;}
function edit(id:Section){close();editing.value=id;message.value='';error.value='';}
function clear(id:Section){
 if(!confirm(tr('保存后将清除该凭据，是否继续？')))return;
 if(id==='smtp')clearSMTP.value=true;else if(id==='github')clearGitHub.value=true;else clearLinuxDO.value=true;
}
const callback=computed(()=>{try{const u=new URL(cfg.value?.public_origin??'');return /^https?:$/.test(u.protocol)&&u.pathname==='/'&&!u.search&&!u.hash?u.origin+'/auth/oauth/callback':'';}catch{return '';}});
async function copy(){try{await navigator.clipboard.writeText(callback.value);message.value=tr('已复制');}catch{error.value=tr('请手动复制回调地址');}}
async function load(){const d=await api('/admin/api/consumer/identity');cfg.value=d.settings;if(!cfg.value.smtp_port)cfg.value.smtp_port=587;snapshot.value=JSON.parse(JSON.stringify(cfg.value));flags.value=d;}
async function save(){if(!cfg.value||busy.value)return;busy.value=true;error.value='';message.value='';try{await api('/admin/api/consumer/identity',{method:'POST',body:{settings:cfg.value,clear_smtp:clearSMTP.value,clear_github:clearGitHub.value,clear_linuxdo:clearLinuxDO.value}});await load();editing.value=undefined;clearSMTP.value=clearGitHub.value=clearLinuxDO.value=false;message.value=tr('登录配置已保存');}catch(e){error.value=(e as Error).message;}finally{busy.value=false;}}
onMounted(()=>load().catch(e=>error.value=(e as Error).message));
</script>
<template><div class="consumer-form"><template v-if="snapshot&&cfg">
<div v-for="id in sections" :key="id" class="identity-section">
<div class="rights-bar"><div><h3>{{tr(names[id])}}</h3><span>{{id==='origin'?snapshot.public_origin||tr('未配置'):id==='smtp'?[snapshot.smtp_host,snapshot.smtp_from].filter(Boolean).join(' · ')||tr('未配置'):tr(snapshot[id].enabled?'已启用':'未启用')}}</span></div>
<button v-if="editing!==id" class="btn" :disabled="busy" :aria-label="tr('配置')+' '+tr(names[id])" @click="edit(id)">{{tr('配置')}}</button></div>
<form v-if="editing===id" @submit.prevent="save"><fieldset class="rights-fields" :disabled="busy">
<template v-if="id==='origin'"><label class="field">{{tr('公开访问地址')}}<input v-model="cfg.public_origin" type="url" placeholder="https://panel.example.com"></label></template>
<template v-else-if="id==='smtp'"><div class="field-grid cols-2">
<label class="field">{{tr('SMTP 主机')}}<input v-model="cfg.smtp_host" placeholder="smtp.example.com"></label>
<label class="field">{{tr('SMTP 端口')}}<input v-model.number="cfg.smtp_port" type="number" min="1" max="65535"></label>
<label class="field">{{tr('发件邮箱')}}<input v-model="cfg.smtp_from" type="email"></label>
<label class="field">{{tr('SMTP 用户')}}<input v-model="cfg.smtp_user" autocomplete="off"></label>
<label class="field">{{tr('SMTP 密码')}}<input v-model="cfg.smtp_password" type="password" :disabled="clearSMTP" autocomplete="new-password" :placeholder="tr(flags.smtp_password_set?'已保存，留空保留':'未设置')"></label></div>
<p v-if="snapshot.smtp_host&&!cfg.smtp_host">{{tr('关闭邮件后，邮箱验证和密码找回将不可用。')}}</p></template>
<template v-else><label class="check"><input v-model="cfg[id].enabled" type="checkbox">{{tr('启用登录与绑定')}}</label><p v-if="cfg[id].enabled&&!snapshot[id].enabled">{{tr('仅已绑定账号可登录，不会自动注册或按邮箱合并。')}}</p>
<div class="field-grid cols-2"><label class="field">Client ID<input v-model="cfg[id].client_id" autocomplete="off"></label>
<label class="field">Client Secret<input v-model="cfg[id].client_secret" :disabled="id==='github'?clearGitHub:clearLinuxDO" type="password" autocomplete="new-password" :placeholder="tr(flags[id==='github'?'github_secret_set':'linuxdo_secret_set']?'已保存，留空保留':'未设置')"></label></div></template>
<button v-if="id!=='origin'&&(id==='smtp'?flags.smtp_password_set:flags[id==='github'?'github_secret_set':'linuxdo_secret_set'])" class="btn danger" type="button" :disabled="clearSMTP||clearGitHub||clearLinuxDO" @click="clear(id)">{{tr(clearSMTP||clearGitHub||clearLinuxDO?'保存后清除':'清除已保存凭据')}}</button>
<div v-if="id!=='smtp'&&callback" class="rights-actions"><span>{{tr('回调地址')}}：<code>{{callback}}</code></span><button type="button" class="btn" @click="copy">{{tr('复制')}}</button></div>
<div class="rights-actions"><button class="btn primary">{{tr('保存登录配置')}}</button><button type="button" class="btn" @click="close">{{tr('取消')}}</button></div>
</fieldset></form></div></template><p v-if="error" class="err-text" role="alert">{{error}}</p><p v-if="message" role="status">{{message}}</p></div></template>
