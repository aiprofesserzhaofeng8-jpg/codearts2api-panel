<script setup lang="ts">
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
function choose(){enabled.value=selected.value?.wallet.enabled??false;review.value=false;note.value='';amount.value='0';}
async function adjust(){if(!selected.value||busy.value)return;busy.value=true;error.value='';message.value='';try{await api('/admin/api/consumer/wallet',{method:'POST',body:{user_id:userID.value,expected_revision:selected.value.wallet.revision,amount:amount.value,enabled:enabled.value,clear_review:review.value,note:note.value}});message.value='额度已保存';amount.value='0';note.value='';emit('changed');}catch(e){error.value=(e as Error).message;emit('changed');}finally{busy.value=false;}}
</script>
<template>
<div class="card consumer-admin"><div class="card-head"><h2>用户权益</h2></div>
<nav class="user-tabs" aria-label="用户权益管理"><button v-for="t in [{id:'wallet',name:'额度管理'},{id:'plans',name:'套餐与订阅'},{id:'codes',name:'兑换码'},{id:'identity',name:'登录与邮件'}]" :key="t.id" :class="['btn',tab===t.id?'primary':'']" @click="tab=t.id">{{t.name}}</button></nav>
<form v-if="tab==='wallet'" class="consumer-form" @submit.prevent="adjust"><h3>人工发放与扣减</h3><p class="hint">额度按模型参考 USD 价格结算，与上游积分独立。正数发放，负数扣减；历史消耗保留。</p>
<div class="field"><label for="creditUser">用户</label><select id="creditUser" v-model="userID" required @change="choose"><option value="" disabled>选择用户</option><option v-for="u in users" :key="u.id" :value="u.id">{{u.username}}</option></select></div>
<template v-if="selected"><p>当前额度：{{credits(selected.wallet.balance)}} USD · {{selected.wallet.enabled?'已启用额度结算':'不限余额'}}<span v-if="selected.wallet.review_required" class="err-text"> · 用量待核对</span></p>
<div class="field"><label for="creditDelta">调整额度</label><input id="creditDelta" v-model="amount" inputmode="decimal" required placeholder="例如 10 或 -2.5"></div>
<div class="field"><label for="creditNote">操作原因</label><input id="creditNote" v-model="note" required maxlength="500"></div><label class="check"><input v-model="enabled" type="checkbox">启用余额限制</label><label v-if="selected.wallet.review_required" class="check"><input v-model="review" type="checkbox">已人工核对缺失用量，解除暂停</label><button class="btn primary" :disabled="busy">保存额度</button></template>
<p v-if="error" class="err-text" role="alert">{{error}}</p><p v-if="message" role="status">{{message}}</p></form>
<ConsumerPlans v-if="tab==='plans'" :users="users" :groups="groups" @changed="emit('changed')" />
<ConsumerCodes v-if="tab==='codes'" />
<ConsumerIdentitySettings v-if="tab==='identity'" />
</div>
</template>
