<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { api } from '../../api/client';
import type { components } from '../../api/generated/admin';
defineProps<{user:components['schemas']['User']}>();const emit=defineEmits<{changed:[]}>();
const identities=ref<components['schemas']['ExternalIdentity'][]>([]),options=ref({email:false,github:false,linuxdo:false});
const email=ref(''),code=ref(''),token=ref(''),busy=ref(false),error=ref(''),message=ref('');
async function load(){const [i,o]=await Promise.all([api('/user/api/identity'),api('/auth/options')]);identities.value=i.identities;options.value=o;}
async function run(fn:()=>Promise<void>){if(busy.value)return;busy.value=true;error.value='';message.value='';try{await fn();}catch(e){error.value=(e as Error).message;}finally{busy.value=false;}}
function send(){return run(async()=>{const d=await api('/user/api/identity',{method:'POST',body:{action:'send',email:email.value}});token.value=d.token??'';message.value='验证码已发送，10 分钟内有效';});}
function verify(){return run(async()=>{await api('/user/api/identity',{method:'POST',body:{action:'verify',token:token.value,code:code.value}});token.value='';code.value='';emit('changed');message.value='邮箱已验证';});}
function bind(provider:string){return run(async()=>{const d=await api('/auth/oauth/start',{method:'POST',body:{provider,bind:true}});location.assign(d.url);});}
function unbind(provider:string){return run(async()=>{await api('/user/api/identity',{method:'POST',body:{action:'unbind',provider}});location.reload();});}
onMounted(()=>load().catch(e=>error.value=(e as Error).message));
</script>
<template><section class="card consumer-form"><h2>登录方式</h2><h3>邮箱验证</h3><p>{{user.email_verified?user.email+' · 已验证':'尚未绑定邮箱'}}</p><form v-if="options.email" @submit.prevent="send"><div class="field"><label>绑定邮箱<input v-model="email" type="email" required autocomplete="email"></label></div><button class="btn" :disabled="busy">发送验证码</button></form><p v-else class="hint">邮件服务未启用，请联系管理员配置。</p><form v-if="token" class="actions" @submit.prevent="verify"><input v-model="code" aria-label="邮箱验证码" autocomplete="one-time-code" inputmode="numeric" pattern="[0-9]{6}" required placeholder="6 位验证码"><button class="btn primary" :disabled="busy">验证并绑定</button></form><h3>第三方账号</h3><p class="hint">先绑定后才能使用第三方登录；解绑后需要重新登录。</p><div v-for="p in (['github','linuxdo'] as const)" :key="p" class="actions"><strong>{{p==='github'?'GitHub':'LinuxDO'}}</strong><span>{{identities.some(i=>i.provider===p)?'已绑定':options[p]?'尚未绑定':'管理员未启用'}}</span><button v-if="identities.some(i=>i.provider===p)" class="btn" :disabled="busy" @click="unbind(p)">解绑</button><button v-else-if="options[p]" class="btn" :disabled="busy" @click="bind(p)">绑定账号</button></div><p v-if="error" class="err-text" role="alert">{{error}}</p><p v-if="message" role="status">{{message}}</p></section></template>
