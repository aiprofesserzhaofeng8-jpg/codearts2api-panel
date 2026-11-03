<script setup lang="ts">
import { t as tr } from '../../i18n';

import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import ConsoleLayout from '../../layouts/ConsoleLayout.vue';
import UserNavigation from './UserNavigation.vue';
import { userPages, userPageFromHash, type UserPage } from './navigation';
import { api, isAbortError } from '../../api/client';
import type { components, paths } from '../../api/generated/admin';
import UserEntitlements from './UserEntitlements.vue';
import UserIdentityPanel from './UserIdentityPanel.vue';
import UserDashboard from './UserDashboard.vue';
import UserProfilePanel from './UserProfilePanel.vue';
import UserAccountCard from './UserAccountCard.vue';
import UserBudgetPanel from './UserBudgetPanel.vue';
import UserKeysPanel from './UserKeysPanel.vue';
import UserModelsPanel from './UserModelsPanel.vue';
import UserUsagePanel from './UserUsagePanel.vue';
import UserIcon from './UserIcon.vue';
import { credits } from './consumer-format';
import { expireSession } from '../../app/authentication';
type User = components['schemas']['User'];
type Profile = paths['/user/api/profile']['get']['responses'][200]['content']['application/json'];
type Usage = components['schemas']['UserUsagePage'];
const props=defineProps<{initialUser:User}>();
const user=ref<User|undefined>(props.initialUser), profile=ref<Profile>(), keys=ref<components['schemas']['UserKey'][]>([]);
const models=ref<{id:string;name:string}[]>([]), usage=ref<Usage>();
const oldPassword=ref(''),newPassword=ref(''),keyName=ref('');
const error=ref(''),message=ref(''),busy=ref(false),revealed=ref<Record<string,string>>({});
const tab=ref<UserPage>(userPageFromHash()), range=ref('7');let epoch=0;
const currentPage=computed(()=>userPages.find(page=>page.id===tab.value)!);
function readRoute(){tab.value=userPageFromHash();revealed.value={};oldPassword.value='';newPassword.value='';}
function navigate(page:UserPage){
  if(tab.value!==page)history.pushState(null,'','#'+page);
  readRoute();
}
watch([tab,user],()=>{document.title=(user.value?currentPage.value.name:tr('用户登录'))+' · CodeArts2API';});
const apiBase=location.origin+'/v1';
function expired(){epoch++;user.value=undefined;profile.value=undefined;keys.value=[];models.value=[];usage.value=undefined;revealed.value={};oldPassword.value='';newPassword.value='';tab.value='dashboard';}
function usagePath(page:number){
  const query=new URLSearchParams({page:String(page),timezone:Intl.DateTimeFormat().resolvedOptions().timeZone||'UTC'});
  if(range.value!=='0'){const start=new Date();start.setHours(0,0,0,0);start.setDate(start.getDate()-Number(range.value)+1);query.set('start',String(Math.floor(start.getTime()/1000)));}
  return '/user/api/usage?'+query;
}
async function refresh(page=1){
  const current=epoch;
  const [p,k,m,u]=await Promise.all([api('/user/api/profile'),api('/user/api/keys'),api('/user/api/models'),api<Usage>(usagePath(page))]);
  if(current!==epoch)return;profile.value=p;user.value=p.user;keys.value=k.keys;models.value=m.models;usage.value=u;revealed.value={};
}
async function act(fn:()=>Promise<void>){if(busy.value)return;busy.value=true;error.value='';message.value='';try{await fn();}catch(e){if(!isAbortError(e))error.value=(e as Error).message;}finally{busy.value=false;}}
function logout(){return act(async()=>{await api('/auth/logout',{method:'POST'});expireSession();});}
function createKey(name:string){return act(async()=>{await api('/user/api/keys',{method:'POST',body:{name}});keyName.value='';await refresh();message.value=tr('密钥已创建，可点击“显示 / 复制”接入客户端。');});}
function reveal(ref:string){return act(async()=>{if(revealed.value[ref]){delete revealed.value[ref];return;}const d=await api('/user/api/keys/reveal',{method:'POST',body:{ref}});revealed.value[ref]=d.value;});}
function copy(ref:string){return act(async()=>{const d=await api('/user/api/keys/reveal',{method:'POST',body:{ref}});try{await navigator.clipboard.writeText(d.value);message.value=tr('密钥已复制');}catch{revealed.value[ref]=d.value;message.value=tr('请选择显示的密钥手动复制');}});}
function change(ref:string,action:string){if(action==='delete'&&!confirm(tr('删除该密钥？使用它的客户端将无法继续调用。')))return;return act(async()=>{await api('/user/api/keys?ref='+encodeURIComponent(ref)+'&action='+action,{method:'DELETE'});await refresh();});}
function changePassword(){return act(async()=>{await api('/user/api/password',{method:'POST',body:{old_password:oldPassword.value,password:newPassword.value}});expireSession(tr('密码已修改，请重新登录。'));});}
function saveProfile(update:components['schemas']['UserProfileUpdate']){return act(async()=>{const p=await api('/user/api/profile',{method:'POST',body:update});profile.value=p;user.value=p.user;message.value=tr('个人资料已保存');});}
onMounted(async()=>{window.addEventListener('hashchange',readRoute);try{await refresh();}catch(e){if(!isAbortError(e))error.value=(e as Error).message;}});
onUnmounted(()=>{window.removeEventListener('hashchange',readRoute);expired();});
</script>
<template>
<div class="user-console">
  <ConsoleLayout v-if="user" role="user" :name="user.display_name || user.username" class="user-workspace">
    <template #navigation><UserNavigation :active="tab" @navigate="navigate" /></template>
    <template #heading><h1>{{currentPage.name}}</h1><p class="page-hint">{{currentPage.hint}}</p></template>
    <template #identity>
      <span v-if="user.wallet.enabled" class="portal-wallet-chip"><UserIcon name="wallet" />${{credits(user.wallet.balance)}}</span>
      <div class="portal-topbar-user"><img v-if="user.avatar" :src="user.avatar" class="user-avatar" :alt="tr('用户头像')"><span v-else class="user-avatar">{{(user.display_name||user.username).slice(0,1).toUpperCase()}}</span><div><strong>{{user.display_name||user.username}}</strong><span>{{tr('普通用户')}}</span></div></div>
    </template>
    <template #actions><button class="btn ghost" id="userLogout" :disabled="busy" @click="logout">{{tr('退出登录')}}</button></template>
    <template #status><span class="portal-sidebar-user"><UserIcon name="shield" />{{tr('个人工作空间')}}</span></template>
    <div class="user-main" :class="{'portal-narrow':['profile','identity','password','entitlements'].includes(tab)}">
      <div class="portal-page-actions">
        <div v-if="tab==='dashboard'" class="portal-welcome"><h2>{{tr('你好，')}}{{user.display_name||user.username}}</h2><p>{{tr('查看你的 API 用量与账户状态。')}}</p></div>
        <span v-else class="portal-breadcrumb">{{tr('个人空间 ')}}<span>/</span> {{currentPage.name}}</span>
        <div class="portal-filter-actions">
          <label v-if="tab==='dashboard'||tab==='usage'" class="portal-range" for="userUsageRange"><UserIcon name="clock" /><span class="portal-sr-only">{{tr('统计范围')}}</span><select id="userUsageRange" v-model="range" :disabled="busy" @change="act(()=>refresh())"><option value="7">{{tr('近 7 天')}}</option><option value="30">{{tr('近 30 天')}}</option><option value="0">{{tr('全部历史')}}</option></select></label>
          <button class="btn" id="userRefresh" :disabled="busy" @click="act(()=>refresh())"><UserIcon name="refresh" /><span>{{tr('刷新')}}</span></button>
        </div>
      </div>
      <p v-if="error" id="userConsoleError" class="portal-notice danger" role="alert">{{error}}</p><p v-if="message" class="portal-notice success" role="status">{{message}}</p>
      <UserDashboard v-if="tab==='dashboard'" :usage="usage" @navigate="navigate" />
      <UserBudgetPanel v-if="tab==='dashboard'" :user="user" />
      <UserKeysPanel v-if="tab==='keys'" v-model:name="keyName" :keys="keys" :busy="busy" :revealed="revealed" :api-base="apiBase" @create="createKey" @reveal="reveal" @copy="copy" @change="change" />
      <UserModelsPanel v-if="tab==='models'" :models="models" />
      <UserUsagePanel v-if="tab==='usage'" :usage="usage" :busy="busy" @page="page=>act(()=>refresh(page))" />
      <UserEntitlements v-if="tab==='entitlements'" @changed="act(()=>refresh())" />
      <UserIdentityPanel v-if="tab==='identity'" :user="user" @changed="act(()=>refresh())" />
      <template v-if="tab==='profile'"><UserAccountCard :user="user" :group-name="profile?.group_name" /><UserProfilePanel :user="user" :busy="busy" @save="saveProfile" /></template>
      <section v-if="tab==='password'" class="portal-card portal-security">
        <div class="portal-section-heading"><div><h2>{{tr('修改密码')}}</h2><p>{{tr('定期更新密码，保护你的账户安全。')}}</p></div><span class="portal-icon-surface"><UserIcon name="lock" /></span></div>
        <form class="portal-password-form" @submit.prevent="changePassword"><div class="field"><label for="userOldPassword">{{tr('当前密码')}}</label><input id="userOldPassword" v-model="oldPassword" type="password" autocomplete="current-password" :placeholder="tr('输入当前密码')" required maxlength="256"></div><div class="field"><label for="userNewPassword">{{tr('新密码')}}</label><input id="userNewPassword" v-model="newPassword" type="password" autocomplete="new-password" :placeholder="tr('至少 12 位，建议包含字母与数字')" required minlength="12" maxlength="256"></div><p class="portal-note">{{tr('修改后，当前登录及其他已登录会话将失效，需要重新登录。')}}</p><button class="btn primary" :disabled="busy">{{tr('修改并重新登录')}}</button></form>
      </section>
    </div>
  </ConsoleLayout>
</div>
</template>
