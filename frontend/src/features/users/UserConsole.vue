<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue';
import { api, configureClient, disposeClient, isAbortError } from '../../api/client';
import type { components, paths } from '../../api/generated/admin';
import UserDashboard from './UserDashboard.vue';
import UserProfilePanel from './UserProfilePanel.vue';
type User = components['schemas']['User'];
type Profile = paths['/user/api/profile']['get']['responses'][200]['content']['application/json'];
type Usage = components['schemas']['UserUsagePage'];
const user=ref<User>(), profile=ref<Profile>(), keys=ref<components['schemas']['UserKey'][]>([]);
const models=ref<{id:string;name:string}[]>([]), usage=ref<Usage>();
const username=ref(''),password=ref(''),oldPassword=ref(''),newPassword=ref(''),keyName=ref('');
const error=ref(''),message=ref(''),busy=ref(false),ready=ref(false),revealed=ref<Record<string,string>>({});
const tab=ref('dashboard'), range=ref('7');let epoch=0;
const apiBase=location.origin+'/v1';
function expired(){epoch++;user.value=undefined;profile.value=undefined;keys.value=[];models.value=[];usage.value=undefined;revealed.value={};password.value='';oldPassword.value='';newPassword.value='';tab.value='dashboard';}
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
function login(){return act(async()=>{const d=await api('/user/auth/login',{method:'POST',body:{username:username.value,password:password.value}});password.value='';epoch++;user.value=d.user;await refresh();});}
function logout(){return act(async()=>{await api('/user/auth/logout',{method:'POST'});expired();});}
function createKey(){return act(async()=>{await api('/user/api/keys',{method:'POST',body:{name:keyName.value}});keyName.value='';await refresh();message.value='密钥已创建，可点击“显示 / 复制”接入客户端。';});}
function reveal(ref:string){return act(async()=>{if(revealed.value[ref]){delete revealed.value[ref];return;}const d=await api('/user/api/keys/reveal',{method:'POST',body:{ref}});revealed.value[ref]=d.value;});}
function copy(ref:string){return act(async()=>{const d=await api('/user/api/keys/reveal',{method:'POST',body:{ref}});try{await navigator.clipboard.writeText(d.value);message.value='密钥已复制';}catch{revealed.value[ref]=d.value;message.value='请选择显示的密钥手动复制';}});}
function change(ref:string,action:string){if(action==='delete'&&!confirm('删除该密钥？使用它的客户端将无法继续调用。'))return;return act(async()=>{await api('/user/api/keys?ref='+encodeURIComponent(ref)+'&action='+action,{method:'DELETE'});await refresh();});}
function changePassword(){return act(async()=>{await api('/user/api/password',{method:'POST',body:{old_password:oldPassword.value,password:newPassword.value}});expired();message.value='密码已修改，请重新登录。';});}
function saveProfile(update:components['schemas']['UserProfileUpdate']){return act(async()=>{const p=await api('/user/api/profile',{method:'POST',body:update});profile.value=p;user.value=p.user;message.value='个人资料已保存';});}
function theme(){const value=document.documentElement.dataset.theme==='light'?'dark':'light';document.documentElement.dataset.theme=value;try{localStorage.setItem('codearts2api_theme',value);}catch{/*Theme still works without storage.*/}}
const date=(time:number)=>time?new Date(time*1000).toLocaleString():'—';
const usd=(value:unknown)=>Number(value??0).toFixed(6);
onMounted(async()=>{configureClient(expired);try{const d=await api('/user/auth/session');if(d.authenticated){user.value=d.user;await refresh();}}catch(e){if(!isAbortError(e))error.value=(e as Error).message;}finally{ready.value=true;}});
onUnmounted(()=>{expired();disposeClient();});
</script>
<template>
<div class="user-console">
  <header class="user-header"><a href="/user" class="brand-name">CodeArts2API</a><div class="actions"><button class="btn" id="userTheme" @click="theme">切换主题</button><button v-if="user" class="btn" id="userLogout" :disabled="busy" @click="logout">退出登录</button><a v-else class="btn" href="/">管理员入口</a></div></header>
  <main class="user-main">
    <p v-if="!ready" role="status">加载中…</p>
    <div v-else-if="!user" class="card user-login"><h1>用户登录</h1><p class="hint">请使用管理员为你创建的账号。</p>
      <form id="userLoginForm" @submit.prevent="login"><div class="field"><label for="userUsername">用户名</label><input id="userUsername" v-model="username" required autocomplete="username" maxlength="64"></div><div class="field"><label for="userPassword">密码</label><input id="userPassword" v-model="password" type="password" required autocomplete="current-password" maxlength="256"></div><button class="btn primary" :disabled="busy">{{busy?'登录中…':'登录'}}</button></form>
    </div>
    <template v-else>
      <div class="card"><div class="card-head"><div class="user-identity"><img v-if="user.avatar" :src="user.avatar" class="user-avatar" alt="用户头像"><span v-else class="user-avatar" aria-label="默认头像">{{(user.display_name||user.username).slice(0,1).toUpperCase()}}</span><div><h1>{{user.display_name||user.username}}</h1><p class="hint">{{profile?.group_name}} · {{user.username}}</p></div></div><button class="btn" id="userRefresh" :disabled="busy" @click="act(()=>refresh())">刷新</button></div>
      <div class="user-summary"><div><span class="hint">已用请求 / 预算</span><strong>{{user.budget_usage.requests}} / {{user.budget.max_requests||'不限'}}</strong></div><div><span class="hint">Token / 预算</span><strong>{{user.budget_usage.tokens}} / {{user.budget.max_tokens||'不限'}}</strong></div><div><span class="hint">USD / 预算</span><strong>${{usd(user.budget_usage.usd)}} / {{user.budget.max_usd||'不限'}}</strong></div></div>
      <p v-if="user.budget_usage.incomplete || user.budget_usage.unpriced" class="hint">存在未报告用量或未配置价格的消耗；相应预算受限时，请联系管理员核查。</p>
      <p class="hint" id="userConcurrency">用户并发：{{user.in_flight}} / {{user.max_concurrency||'不限'}}<span v-if="user.budget.max_tokens||user.budget.max_usd"> · 当前计量预算最多允许一个在途请求</span></p>
      </div>
      <nav class="user-tabs" aria-label="用户功能"><button v-for="t in [{id:'dashboard',name:'仪表盘'},{id:'keys',name:'我的密钥'},{id:'models',name:'可用模型'},{id:'usage',name:'我的用量'},{id:'profile',name:'个人资料'},{id:'password',name:'修改密码'}]" :key="t.id" :class="['btn',tab===t.id?'primary':'']" :aria-pressed="tab===t.id" @click="tab=t.id;revealed={}">{{t.name}}</button></nav>
      <div v-if="tab==='dashboard'||tab==='usage'" class="actions user-usage-range"><label for="userUsageRange">统计范围</label><select id="userUsageRange" v-model="range" :disabled="busy" @change="act(()=>refresh())"><option value="7">近 7 天</option><option value="30">近 30 天</option><option value="0">全部历史</option></select></div>
      <UserDashboard v-if="tab==='dashboard'" :usage="usage" />
      <UserProfilePanel v-if="tab==='profile'" :user="user" :busy="busy" @save="saveProfile" />
      <section v-if="tab==='keys'" class="card" id="userKeys"><h2>我的 API 密钥</h2><p class="hint">API 地址：<code>{{apiBase}}</code>；所有密钥共享上方用户预算。</p>
        <form class="actions" @submit.prevent="createKey"><input v-model="keyName" aria-label="密钥备注" placeholder="密钥备注（可选）" maxlength="80"><button class="btn primary" id="userCreateKey" :disabled="busy||keys.length>=32">创建密钥</button></form>
        <div class="table-wrap"><table><thead><tr><th>密钥</th><th>备注</th><th>状态</th><th>已用请求</th><th>操作</th></tr></thead><tbody><tr v-for="k in keys" :key="k.ref"><td><input class="mono key-value" readonly :type="revealed[k.ref]?'text':'password'" :value="revealed[k.ref]??k.key" :aria-label="'密钥 '+(k.name||k.ref)" autocomplete="off"></td><td>{{k.name}}</td><td>{{k.enabled?'启用':'停用'}}</td><td>{{k.budget_usage.requests}}</td><td><div class="actions"><button class="btn xs" :disabled="busy" @click="reveal(k.ref)">{{revealed[k.ref]?'隐藏':'显示'}}</button><button class="btn xs" :disabled="busy" @click="copy(k.ref)">复制</button><button class="btn xs" :disabled="busy" @click="change(k.ref,'toggle')">{{k.enabled?'停用':'启用'}}</button><button class="btn xs danger" :disabled="busy" @click="change(k.ref,'delete')">删除</button></div></td></tr><tr v-if="!keys.length"><td colspan="5" class="empty">暂无密钥，请先创建</td></tr></tbody></table></div>
      </section>
      <section v-if="tab==='models'" class="card" id="userModels"><h2>可用模型</h2><p class="hint">显示名称由管理员配置；模型隐藏不等于禁止调用。</p><div class="table-wrap"><table><thead><tr><th>模型名称</th><th>显示名称</th></tr></thead><tbody><tr v-for="m in models" :key="m.id"><td class="mono">{{m.id}}</td><td>{{m.name}}</td></tr><tr v-if="!models.length"><td colspan="2" class="empty">暂无显示的模型，请联系管理员检查分组</td></tr></tbody></table></div></section>
      <section v-if="tab==='usage'" class="card" id="userUsage"><h2>我的用量</h2><p class="hint">保留范围内 {{usage?.total??0}} 次请求；Token {{Number(usage?.stats.input_tokens??0)+Number(usage?.stats.output_tokens??0)}}；参考费用 ${{usd(usage?.stats.estimated_usd)}}。删除密钥不删除历史用量。</p><div class="table-wrap"><table><thead><tr><th>时间</th><th>模型</th><th>状态</th><th>输入 / 输出 Token</th><th>参考 USD</th><th>耗时</th></tr></thead><tbody><tr v-for="entry in usage?.logs??[]" :key="entry.request_id"><td>{{date(entry.time)}}</td><td>{{entry.model}}</td><td>{{entry.status}}</td><td>{{entry.usage.known?`${entry.usage.input_tokens} / ${entry.usage.output_tokens}`:'未报告'}}</td><td>{{entry.estimated_cost?'$'+usd(entry.estimated_cost.amount):'未定价'}}</td><td>{{entry.latency_ms}} ms</td></tr></tbody></table></div><div class="actions"><button class="btn" :disabled="busy||(usage?.page??1)<=1" @click="act(()=>refresh((usage?.page??1)-1))">上一页</button><span>{{usage?.page??1}} / {{usage?.pages??1}}</span><button class="btn" :disabled="busy||(usage?.page??1)>=(usage?.pages??1)" @click="act(()=>refresh((usage?.page??1)+1))">下一页</button></div></section>
      <section v-if="tab==='password'" class="card user-password"><h2>修改密码</h2><form @submit.prevent="changePassword"><div class="field"><label for="userOldPassword">当前密码</label><input id="userOldPassword" v-model="oldPassword" type="password" autocomplete="current-password" required maxlength="256"></div><div class="field"><label for="userNewPassword">新密码（至少 12 位）</label><input id="userNewPassword" v-model="newPassword" type="password" autocomplete="new-password" required minlength="12" maxlength="256"></div><button class="btn primary" :disabled="busy">修改并重新登录</button></form></section>
    </template>
    <p v-if="error" id="userConsoleError" class="err-text" role="alert">{{error}}</p><p v-if="message" role="status">{{message}}</p>
  </main>
</div>
</template>
