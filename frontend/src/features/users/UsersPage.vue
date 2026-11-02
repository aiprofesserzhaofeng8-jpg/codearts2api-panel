<script setup lang="ts">
import { onMounted, onUnmounted, reactive, ref } from 'vue';
import { api, isAbortError } from '../../api/client';
import type { components } from '../../api/generated/admin';
type User = components['schemas']['User'];
const users = ref<User[]>([]);
const groups = ref<components['schemas']['Group'][]>([]);
const dialog = ref<HTMLDialogElement>();
const busy = ref(false), error = ref(''), loadError = ref('');
const draft = reactive({id:'',username:'',password:'',group_id:'',enabled:true,max_requests:0,max_tokens:0,max_usd:0,reset_budget:false});
let epoch = 0;
async function load() {
  const current=++epoch;
  try { const [u,g]=await Promise.all([api('/admin/api/users'),api('/admin/api/groups')]);
    if(current!==epoch)return; users.value=u.users; groups.value=g.groups??[]; loadError.value='';
  } catch(e) {if(!isAbortError(e))loadError.value=(e as Error).message;}
}
function edit(user?:User) {
  Object.assign(draft,{id:user?.id??'',username:user?.username??'',password:'',group_id:user?.group_id??'',enabled:user?.enabled??true,
    max_requests:user?.budget.max_requests??0,max_tokens:user?.budget.max_tokens??0,max_usd:user?.budget.max_usd??0,reset_budget:false});
  error.value='';dialog.value?.showModal();
}
function close(){if(!busy.value){draft.password='';dialog.value?.close();}}
async function save(){
  if(busy.value)return;busy.value=true;error.value='';
  try {await api('/admin/api/users',{method:'POST',body:{id:draft.id,username:draft.username,group_id:draft.group_id,enabled:draft.enabled,
    password:draft.password||undefined,budget:{max_requests:draft.max_requests,max_tokens:draft.max_tokens,max_usd:draft.max_usd},reset_budget:draft.reset_budget}});
    draft.password='';dialog.value?.close();await load();
  }catch(e){if(!isAbortError(e))error.value=(e as Error).message;}finally{busy.value=false;}
}
onMounted(()=>window.addEventListener('users-refresh',load));
onUnmounted(()=>{epoch++;window.removeEventListener('users-refresh',load);draft.password='';});
</script>
<template>
<section id="tab-users" role="tabpanel" aria-labelledby="nav-users">
  <div class="card">
    <div class="card-head"><h2>用户管理</h2><div class="actions"><a class="btn" href="/user" target="_blank" rel="noopener">用户登录入口</a><button class="btn primary" id="addUser" @click="edit()">创建用户</button></div></div>
    <p class="hint">用户仅可管理自己的密钥与用量。分组和跨密钥共享预算由管理员设置；0 表示不限。</p>
    <p v-if="loadError" class="err-text" role="alert">{{loadError}}</p>
    <div class="table-wrap"><table><thead><tr><th>用户名</th><th>分组</th><th>状态</th><th>已用请求 / 预算</th><th>Token / 预算</th><th>USD / 预算</th><th>操作</th></tr></thead>
    <tbody id="users"><tr v-for="u in users" :key="u.id"><td>{{u.username}}</td><td>{{groups.find(g=>g.id===u.group_id)?.name??'分组不存在'}}</td><td><span :class="['badge',u.enabled?'ok':'']">{{u.enabled?'启用':'停用'}}</span></td><td>{{u.budget_usage.requests}} / {{u.budget.max_requests||'不限'}}</td><td>{{u.budget_usage.tokens}} / {{u.budget.max_tokens||'不限'}}</td><td>${{u.budget_usage.usd.toFixed(6)}} / {{u.budget.max_usd||'不限'}}</td><td><button class="btn xs" @click="edit(u)">编辑</button></td></tr>
    <tr v-if="!users.length"><td colspan="7" class="empty">暂无用户，创建后将用户登录入口交给使用者</td></tr></tbody></table></div>
  </div>
  <dialog ref="dialog" id="userDialog" class="modal" aria-labelledby="userTitle" @cancel.prevent="close">
    <form @submit.prevent="save"><div class="modal-head"><h2 id="userTitle">{{draft.id?'编辑用户':'创建用户'}}</h2><button class="btn ghost" type="button" :disabled="busy" @click="close">关闭</button></div>
    <div class="modal-body"><fieldset :disabled="busy" class="user-fields">
      <div class="field-grid cols-2"><div class="field"><label for="uUsername">用户名</label><input id="uUsername" v-model="draft.username" required minlength="3" maxlength="64" autocomplete="off" placeholder="字母、数字、点、横线或下划线"></div><div class="field"><label for="uPassword">{{draft.id?'重设密码（留空保留）':'初始密码'}}</label><input id="uPassword" v-model="draft.password" type="password" :required="!draft.id" minlength="12" maxlength="256" autocomplete="new-password"></div></div>
      <div class="field"><label for="uGroup">允许访问的账号分组</label><select id="uGroup" v-model="draft.group_id" required><option disabled value="">选择分组</option><option v-for="g in groups" :key="g.id" :value="g.id">{{g.name}}</option></select></div>
      <p v-if="!groups.length" class="hint">请先在“账号分组”创建分组，并选择可用账号和模型。</p>
      <label class="check"><input id="uEnabled" v-model="draft.enabled" type="checkbox">启用用户</label>
      <h3>用户共享预算</h3><div class="field-grid cols-2"><div class="field"><label for="uRequests">请求次数</label><input id="uRequests" v-model.number="draft.max_requests" type="number" min="0" step="1" required></div><div class="field"><label for="uTokens">Token 总量</label><input id="uTokens" v-model.number="draft.max_tokens" type="number" min="0" step="1" required></div><div class="field"><label for="uUSD">USD 参考费用</label><input id="uUSD" v-model.number="draft.max_usd" type="number" min="0" step="any" required></div></div>
      <p class="hint">Token / USD 按实际用量结算，最多一个在途请求可能超额。缺少用量或价格时停止相应预算的后续请求。</p>
      <label v-if="draft.id" class="check"><input id="uReset" v-model="draft.reset_budget" type="checkbox">重置用户预算累计（历史用量保留）</label>
      <p class="hint">保存后该用户需重新登录；停用立即阻止后续调用，已返回的内容无法撤回。</p>
    </fieldset><p id="userError" class="err-text" role="alert">{{error}}</p></div>
    <div class="modal-actions"><button class="btn" type="button" :disabled="busy" @click="close">取消</button><button class="btn primary" id="userSave" type="submit" :disabled="busy">{{busy?'保存中…':'保存用户'}}</button></div></form>
  </dialog>
</section>
</template>
