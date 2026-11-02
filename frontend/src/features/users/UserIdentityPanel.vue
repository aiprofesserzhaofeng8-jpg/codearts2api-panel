<script setup lang="ts">
import { onMounted, ref } from 'vue';
import UserIcon from './UserIcon.vue';
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
<template>
<section class="portal-card" id="userIdentity">
  <div class="portal-section-heading"><div><h2>登录方式绑定</h2><p>查看当前绑定状态，管理用于登录和找回密码的账号。</p></div><span class="portal-icon-surface"><UserIcon name="shield" /></span></div>
  <div class="portal-binding-list">
    <div class="portal-binding"><span class="portal-binding-icon email"><UserIcon name="mail" /></span><div class="portal-binding-body"><h3>邮箱 <span class="portal-badge" :class="user.email_verified?'success':''">{{user.email_verified?'已验证':'未绑定'}}</span></h3><p>{{user.email_verified?user.email:'绑定邮箱后，可用于找回登录密码。'}}</p><p v-if="!options.email" class="portal-note">邮件服务未启用，请联系管理员配置。</p></div></div>
    <form v-if="options.email" class="portal-binding-form" @submit.prevent="send"><div class="field"><label for="userBindEmail">绑定邮箱</label><input id="userBindEmail" v-model="email" type="email" required autocomplete="email" placeholder="输入你的邮箱地址"></div><button class="btn" :disabled="busy">发送验证码</button></form>
    <form v-if="token" class="portal-binding-form" @submit.prevent="verify"><input v-model="code" aria-label="邮箱验证码" autocomplete="one-time-code" inputmode="numeric" pattern="[0-9]{6}" required placeholder="6 位验证码"><button class="btn primary" :disabled="busy">验证并绑定</button></form>
    <div v-for="p in (['github','linuxdo'] as const)" :key="p" class="portal-binding"><span class="portal-binding-icon" :class="p"><UserIcon :name="p==='github'?'github':'link'" /></span><div class="portal-binding-body"><h3>{{p==='github'?'GitHub':'LinuxDO'}} <span class="portal-badge" :class="identities.some(i=>i.provider===p)?'success':''">{{identities.some(i=>i.provider===p)?'已绑定':'未绑定'}}</span></h3><p>{{identities.some(i=>i.provider===p)?'已关联，可使用此账号登录。':options[p]?'关联账号，使用第三方身份快速登录。':'此登录方式暂未启用。'}}</p></div><button v-if="identities.some(i=>i.provider===p)" class="btn" :disabled="busy" @click="unbind(p)">解绑</button><button v-else class="btn" :disabled="busy||!options[p]" @click="bind(p)">{{options[p]?'绑定账号':'未启用'}}</button></div>
  </div>
  <p class="portal-note">第三方账号需要先绑定才能登录。解绑后需要重新登录。</p>
  <p v-if="error" class="portal-notice danger" role="alert">{{error}}</p><p v-if="message" class="portal-notice success" role="status">{{message}}</p>
</section>
</template>
