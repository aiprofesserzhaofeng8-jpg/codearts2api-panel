<script setup lang="ts">
import { t as tr } from '../../i18n';

import { computed, ref } from 'vue';
import type { components } from '../../api/generated/admin';
import { api } from '../../api/client';
import { credits } from './consumer-format';
import ConsumerPlans from './ConsumerPlans.vue';
import ConsumerCodes from './ConsumerCodes.vue';
import ConsumerIdentitySettings from './ConsumerIdentitySettings.vue';
type User=components['schemas']['User'];
const props=defineProps<{users:User[];groups:components['schemas']['Group'][]}>();
const emit=defineEmits<{changed:[]}>();
const tab=ref('wallet'),userID=ref(''),amount=ref('0'),note=ref(''),enabled=ref(true),review=ref(false),busy=ref(false),error=ref(''),message=ref('');
const selected=computed(()=>props.users.find(u=>u.id===userID.value));
function choose(){error.value='';message.value='';enabled.value=selected.value?.wallet.enabled??false;review.value=false;note.value='';amount.value='0';}
const balanceAfter=computed(()=>{
 const m=/^([+-]?)(\d+)(?:\.(\d{1,8}))?$/.exec(amount.value);
 if(!selected.value||!m||!Number.isSafeInteger(selected.value.wallet.balance))return '';
 const delta=BigInt(m[2]!+(m[3]??'').padEnd(8,'0'))*(m[1]==='-'?-1n:1n);
 const total=Number(BigInt(selected.value.wallet.balance)+delta);
 return delta!==0n && Number.isSafeInteger(total)?credits(total):'';
});
async function adjust(){if(!selected.value||busy.value)return;busy.value=true;error.value='';message.value='';try{await api('/admin/api/consumer/wallet',{method:'POST',body:{user_id:userID.value,expected_revision:selected.value.wallet.revision,amount:amount.value,enabled:enabled.value,clear_review:review.value,note:note.value}});message.value=tr('额度已保存');amount.value='0';note.value='';emit('changed');}catch(e){error.value=(e as Error).message;emit('changed');}finally{busy.value=false;}}
</script>
<template>
<div class="card consumer-admin"><div class="card-head"><h2>{{tr('用户权益')}}</h2></div>
<nav class="rights-tabs" :aria-label="tr('用户权益管理')"><button v-for="t in [{id:'wallet',name:tr('额度管理')},{id:'plans',name:tr('套餐管理')},{id:'subscriptions',name:tr('用户订阅')},{id:'codes',name:tr('兑换码')},{id:'identity',name:tr('登录与邮件')}]" :key="t.id" :aria-current="tab===t.id?'page':undefined" @click="tab=t.id">{{t.name}}</button></nav>
<form v-if="tab==='wallet'" class="consumer-form" @submit.prevent="adjust">
<div class="field rights-picker"><label for="creditUser">{{tr('用户')}}</label><select id="creditUser" v-model="userID" required @change="choose"><option value="" disabled>{{tr('选择用户')}}</option><option v-for="u in users" :key="u.id" :value="u.id">{{u.username}}</option></select></div>
<template v-if="selected">
<dl class="rights-stats">
 <div><dt>{{tr('当前余额')}}</dt><dd>{{credits(selected.wallet.balance)}} USD</dd></div>
 <div><dt>{{tr('累计费用')}}</dt><dd>{{selected.budget_usage.usd.toFixed(6)}} USD</dd></div>
 <div><dt>{{tr('累计费用上限')}}</dt><dd>{{selected.budget.max_usd?selected.budget.max_usd+' USD':tr('不限')}}</dd></div>
</dl><p v-if="selected.budget.max_usd>0 && selected.budget_usage.usd>=selected.budget.max_usd*0.9" class="err-text">{{tr('累计费用已接近或达到上限，补充余额不会重置累计费用。')}}</p>
<h3>{{tr('调整额度')}}</h3><div class="rights-amount">
<div class="field"><label for="creditDelta">{{tr('调整金额')}}（USD）</label><input id="creditDelta" v-model="amount" inputmode="decimal" required :placeholder="tr('例如 10 或 -2.5')"></div>
<div class="field"><label for="creditNote">{{tr('操作原因')}}</label><input id="creditNote" v-model="note" required maxlength="500"></div></div><p v-if="balanceAfter" role="status">{{tr(amount.trim().startsWith('-')?'扣减':'发放')}} {{amount.trim().replace(/^[+-]/,'')}} USD · {{tr('调整后余额')}}：{{balanceAfter}} USD</p><div class="rights-bar"><label class="check"><input v-model="enabled" type="checkbox">{{tr('启用余额限制')}}</label><label v-if="selected.wallet.review_required" class="check"><input v-model="review" type="checkbox">{{tr('已人工核对缺失用量，解除暂停')}}</label><button class="btn primary" :disabled="busy">{{tr('保存更改')}}</button></div></template>
<p v-if="error" class="err-text" role="alert">{{error}}</p><p v-if="message" role="status">{{message}}</p></form>
<ConsumerPlans v-if="tab==='plans'||tab==='subscriptions'" :view="tab" @navigate="tab=$event" :users="users" :groups="groups" @changed="emit('changed')" />
<ConsumerCodes v-if="tab==='codes'" />
<ConsumerIdentitySettings v-if="tab==='identity'" />
</div>
</template>
<style src="./rights.css"></style>
