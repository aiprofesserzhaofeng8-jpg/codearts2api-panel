<script setup lang="ts">
import { t as tr } from '../../i18n';

import { computed, onMounted, reactive, ref, watch } from 'vue';
import { api } from '../../api/client';
import type { components } from '../../api/generated/admin';
import { consumerDate } from './consumer-format';
type Plan=components['schemas']['ConsumerPlan'];
const props=defineProps<{view:"plans"|"subscriptions";users:components['schemas']['User'][];groups:components['schemas']['Group'][]}>();const emit=defineEmits<{changed:[];navigate:[view:"plans"|"subscriptions"]}>();
const plans=ref<Plan[]>([]),error=ref(''),message=ref(''),busy=ref(false),userID=ref(''),planID=ref(''),days=ref<number|''>('');
const initial=()=>({id:'',name:'',group_id:'',enabled:true,delisted:false,days:30,max_concurrency:5,budget:{max_requests:0,max_tokens:0,max_usd:0},period:'month' as Plan['period']});
const draft=reactive(initial());
const filter=ref('active'),ready=ref(false),returnToUser=ref(false);
const references=ref<Record<string,{subscriptions:number;codes:number}>>({});
const lifecycle=(p:Plan)=>p.enabled?'active':p.delisted?'delisted':'disabled';
const stateName=(p:Plan)=>tr(p.enabled?'在用':p.delisted?'已下架':'已停用');
const filtered=computed(()=>plans.value.filter(p=>filter.value==='all'||lifecycle(p)===filter.value));
const available=computed(()=>plans.value.filter(p=>p.enabled));
const locked=computed(()=>!!references.value[draft.id]);
async function load(){const data=await api('/admin/api/consumer/plans');plans.value=data.plans;references.value=data.references??{};ready.value=true;}
async function run(fn:()=>Promise<void>){if(busy.value)return;busy.value=true;error.value='';message.value='';try{await fn();message.value=tr('已保存');await load();emit('changed');}catch(e){error.value=(e as Error).message;}finally{busy.value=false;}}
function save(){return run(async()=>{const result=await api('/admin/api/consumer/plans',{method:'POST',body:draft});
 if(returnToUser.value){planID.value=result.plan.id;emit('navigate','subscriptions');returnToUser.value=false;}else filter.value=lifecycle(result.plan);Object.assign(draft,initial());editing.value=false;});}
function assign(action:string){return run(async()=>{await api('/admin/api/consumer/subscription',{method:'POST',body:{user_id:userID.value,plan_id:planID.value,action,days:days.value||0}});});}
const editing=ref(false),changing=ref(false);
const user=computed(()=>props.users.find(u=>u.id===userID.value));
const sub=computed(()=>user.value?.subscription);
const plan=computed(()=>plans.value.find(p=>p.id===planID.value));
const renewing=computed(()=>!!sub.value?.id && sub.value.plan.id===planID.value && !sub.value.disabled && sub.value.expires_at>Date.now()/1000);
const end=computed(()=>(renewing.value?sub.value!.expires_at:Math.floor(Date.now()/1000))+(days.value||plan.value?.days||0)*86400);
function choose(){changing.value=false;planID.value=plans.value.find(p=>p.enabled&&p.id===sub.value?.plan?.id)?.id??'';days.value='';message.value='';error.value='';}
function editPlan(p?:Plan){Object.assign(draft,p?{...p,budget:{...p.budget}}:initial());editing.value=true;}
function remove(){if(confirm(tr('移除后保留分组，并恢复用户原有预算规则。')))void assign('remove');}
function newForUser(){emit('navigate','plans');editPlan();returnToUser.value=true;}
function copyPlan(p:Plan){editPlan({...p,id:'',name:p.name+' ('+tr('副本')+')',enabled:true,delisted:false});}
function status(p:Plan,next:string){
 const warning=next==='disabled'?'停用后，已发套餐码暂停兑换；已有订阅继续生效。':next==='delisted'?'下架后不可分配、手动续期或发新码；已发有效码仍可兑换。':'重新上架后可以分配套餐、续期和发码。';
 if(!confirm(p.name+'：'+tr(warning)))return;
 void run(async()=>{await api('/admin/api/consumer/plans',{method:'POST',body:{...p,enabled:next==='active',delisted:next==='delisted'}});editing.value=false;filter.value=next;});
}
function onAction(p:Plan,event:Event){const el=event.target as HTMLSelectElement;const action=el.value;el.value='';returnToUser.value=false;if(!action)return;if(action==='delete')deletePlan(p);else status(p,action);}
function deletePlan(p:Plan){
 const r=references.value[p.id];message.value='';error.value='';
 if(r){error.value=tr('不能删除')+'：'+r.subscriptions+' '+tr('个订阅')+'，'+r.codes+' '+tr('张兑换码');return;}
 if(confirm(tr('永久删除套餐？')+' '+p.name))void run(async()=>{await api('/admin/api/consumer/plans',{method:'DELETE',body:{id:p.id}});editing.value=false;});
}
watch(()=>props.view,(v)=>{error.value='';message.value='';if(v==='subscriptions'){returnToUser.value=false;editing.value=false;if(plan.value&&!plan.value.enabled)planID.value='';}});
onMounted(()=>load().catch(e=>error.value=(e as Error).message));
</script>
<template><section class="consumer-form">
<template v-if="view==='subscriptions'"><h3>{{tr('用户订阅')}}</h3><label class="field rights-picker">{{tr('订阅用户')}}<select :aria-label="tr('订阅用户')" v-model="userID" :disabled="busy" @change="choose"><option value="" disabled>{{tr('选择用户')}}</option><option v-for="u in users" :key="u.id" :value="u.id">{{u.username}}</option></select></label>
<template v-if="user"><dl class="rights-stats"><div><dt>{{tr('当前套餐')}}</dt><dd>{{sub?.id?sub.plan.name:tr('无订阅')}}</dd></div><template v-if="sub?.id"><div><dt>{{tr('状态')}}</dt><dd>{{tr(sub.disabled?'停用':sub.expires_at<=Date.now()/1000?'已过期':'启用')}}</dd></div><div><dt>{{tr('到期时间')}}</dt><dd>{{consumerDate(sub.expires_at)}}</dd></div><div><dt>{{tr('本周期请求')}}</dt><dd>{{sub.usage.requests}} / {{sub.plan.budget.max_requests||tr('不限')}}</dd></div><div><dt>{{tr('本周期 Token')}}</dt><dd>{{sub.usage.tokens.toLocaleString()}}</dd></div><div><dt>{{tr('本周期费用')}}</dt><dd>{{sub.usage.usd.toFixed(6)}} USD</dd></div></template></dl>
<button v-if="ready&&!available.length" class="btn" @click="newForUser">{{tr('去创建套餐')}}</button>
<form @submit.prevent="assign('assign')"><div class="field-grid cols-2">
<label class="field" v-if="!sub?.id||changing||!planID">{{tr('分配套餐')}}<select :aria-label="tr('分配套餐')" v-model="planID" required><option disabled value="">{{tr('选择套餐')}}</option><option v-for="p in plans.filter(p=>p.enabled)" :key="p.id" :value="p.id">{{p.name}}</option></select></label>
<label class="field">{{tr('有效天数')}}<input v-model.number="days" type="number" min="0" max="3650" :placeholder="String(plan?.days??30)"></label></div>
<p v-if="plan">{{tr('有效天数')}}：{{days||plan.days}} · {{tr('到期时间')}}：<template v-if="renewing">{{consumerDate(sub!.expires_at)}} → </template>{{consumerDate(end)}}<span v-if="renewing"> · {{tr('已用额度保留')}}</span><span v-else> · {{tr('所有用户密钥将使用分组')}}：{{groups.find(g=>g.id===plan?.group_id)?.name??'—'}}</span></p>
<div class="rights-actions"><button class="btn primary" :disabled="busy||!plan?.enabled">{{tr(renewing?'续期':sub?.id&&changing?'更换套餐':'分配套餐')}}</button>
<button v-if="sub?.id&&!changing&&planID" class="btn" type="button" @click="changing=true;planID=''">{{tr('更换套餐')}}</button>
<template v-if="sub?.id"><button class="btn" type="button" :disabled="busy" @click="assign(sub.disabled?'enable':'disable')">{{tr(sub.disabled?'恢复订阅':'停用订阅')}}</button><button class="btn danger" type="button" :disabled="busy" @click="remove">{{tr('移除订阅限制')}}</button></template></div></form></template>
</template><template v-else><div class="rights-heading"><h3>{{tr('套餐管理')}}</h3><button v-if="!editing" class="btn" @click="editPlan()">{{tr('新建套餐')}}</button></div>
<label class="field rights-picker" v-if="!editing">{{tr('套餐状态')}}<select v-model="filter"><option value="active">{{tr('在用')}}</option><option value="delisted">{{tr('已下架')}}</option><option value="disabled">{{tr('已停用')}}</option><option value="all">{{tr('全部')}}</option></select></label>
<form v-if="editing" @submit.prevent="save"><p v-if="locked">{{tr('已有订阅或兑换码引用，修改权益请复制为新套餐')}}</p><div class="field-grid cols-2"><div class="field"><label for="planName">{{tr('套餐名称')}}</label><input id="planName" v-model="draft.name" required maxlength="120"></div><div class="field"><label for="ConsumerPlansSelect1">{{tr('分组')}}</label><select id="ConsumerPlansSelect1" :disabled="locked" v-model="draft.group_id" required><option disabled value="">{{tr('选择分组')}}</option><option v-for="g in groups" :key="g.id" :value="g.id">{{g.name}}</option></select></div><div class="field"><label for="planDays">{{tr('有效天数')}}</label><input id="planDays" :disabled="locked" v-model.number="draft.days" type="number" min="1" max="3650" required></div><div class="field"><label for="planConcurrency">{{tr('并发上限')}}</label><input id="planConcurrency" :disabled="locked" :value="draft.max_concurrency||''" :placeholder="tr('不限')" @input="draft.max_concurrency=Number(($event.target as HTMLInputElement).value)" type="number" min="0" max="1024"></div><div class="field"><label for="ConsumerPlansSelect2">{{tr('额度周期')}}</label><select id="ConsumerPlansSelect2" :disabled="locked" v-model="draft.period"><option value="lifetime">{{tr('订阅总量')}}</option><option value="day">{{tr('每日 UTC')}}</option><option value="week">{{tr('每周 UTC')}}</option><option value="month">{{tr('每月 UTC')}}</option></select></div><div class="field"><label for="planRequests">{{tr('请求次数')}}</label><input id="planRequests" :disabled="locked" :value="draft.budget.max_requests||''" :placeholder="tr('不限')" @input="draft.budget.max_requests=Number(($event.target as HTMLInputElement).value)" type="number" min="0"></div><div class="field"><label for="planTokens">{{tr('Token')}}</label><input id="planTokens" :disabled="locked" :value="draft.budget.max_tokens||''" :placeholder="tr('不限')" @input="draft.budget.max_tokens=Number(($event.target as HTMLInputElement).value)" type="number" min="0"></div><div class="field"><label for="planCost">{{tr('参考 USD')}}</label><input id="planCost" :disabled="locked" :value="draft.budget.max_usd||''" :placeholder="tr('不限')" @input="draft.budget.max_usd=Number(($event.target as HTMLInputElement).value)" type="number" min="0" step="any"></div></div><div class="rights-actions"><button class="btn primary" :disabled="busy">{{tr('保存套餐')}}</button><button type="button" class="btn" @click="editing=false;if(returnToUser)emit('navigate','subscriptions')">{{tr('取消')}}</button></div></form>
<div class="table-wrap"><table><thead><tr><th>{{tr('套餐')}}</th><th>{{tr('分组')}}</th><th>{{tr('有效期')}}</th><th>{{tr('状态')}}</th><th>{{tr('操作')}}</th></tr></thead><tbody><tr v-for="p in filtered" :key="p.id"><td>{{p.name}}</td><td>{{groups.find(g=>g.id===p.group_id)?.name??tr('分组已失效')}}</td><td>{{p.days}}{{tr(' 天')}}</td><td>{{stateName(p)}}</td><td><div class="rights-actions"><button class="btn xs row-action row-edit" :disabled="busy" @click="returnToUser=false;editPlan(p)">{{tr('编辑')}}</button><button type="button" class="btn xs row-action row-copy" :aria-label="tr('复制为新套餐')+' '+p.name" :disabled="busy" @click="returnToUser=false;copyPlan(p)">{{tr('复制')}}</button><span class="btn xs row-action row-more plan-more"><span aria-hidden="true">{{tr('更多')}}</span><select class="plan-operations" :value="''" :aria-label="tr('更多操作')+' '+p.name" :disabled="busy" @change="onAction(p,$event)"><option value="" disabled>{{tr('更多')}}</option><option v-if="!p.delisted && groups.some(g=>g.id===p.group_id)" value="delisted">{{tr('下架')}}</option><option v-if="p.enabled||p.delisted" value="disabled">{{tr('停用')}}</option><option v-if="!p.enabled && groups.some(g=>g.id===p.group_id)" value="active">{{tr('重新上架')}}</option><option value="delete">{{tr('删除')}}</option></select></span></div></td></tr><tr v-if="!filtered.length"><td colspan="5" class="empty">{{tr('暂无套餐')}}</td></tr></tbody></table></div></template>
<p v-if="error" role="alert" class="err-text">{{error}}</p><p v-if="message" role="status">{{message}}</p></section></template>
