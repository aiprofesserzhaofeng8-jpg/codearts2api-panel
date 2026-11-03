<script setup lang="ts">
import { t as tr, dateLocale } from '../../i18n';

import { onMounted, ref } from 'vue';
import UserIcon from './UserIcon.vue';
import UserStatCard from './UserStatCard.vue';
import { api } from '../../api/client';
import type { paths } from '../../api/generated/admin';
import { credits,consumerDate } from './consumer-format';
const emit=defineEmits<{changed:[]}>();
const data=ref<paths['/user/api/entitlements']['get']['responses'][200]['content']['application/json']>();const history=ref<'redemptions'|'ledger'>('redemptions');const code=ref(''),busy=ref(false),error=ref(''),message=ref('');
async function load(){data.value=await api('/user/api/entitlements');}
async function redeem(){if(busy.value)return;busy.value=true;error.value='';message.value='';try{await api('/user/api/redeem',{method:'POST',body:{code:code.value}});code.value='';await load();emit('changed');message.value=tr('兑换成功');}catch(e){error.value=(e as Error).message;}finally{busy.value=false;}}
onMounted(()=>load().catch(e=>error.value=(e as Error).message));
</script>
<template>
<section class="portal-stack" id="userEntitlements">
  <div v-if="data" class="portal-stats three"><UserStatCard :label="tr('账户余额')" :value="data.wallet.enabled?'$'+credits(data.wallet.balance):tr('未启用余额限制')" :note="tr('参考 USD 额度')" icon="wallet" tone="green" /><UserStatCard :label="tr('在途预留')" :value="'$'+credits(data.reserved)" :note="tr('请求结算后自动释放')" icon="clock" tone="blue" /><UserStatCard :label="tr('当前套餐')" :value="data.subscription.id?data.subscription.plan.name:tr('暂无订阅')" :note="tr('由管理员分配或兑换获取')" icon="gift" /></div>
  <p v-if="data?.wallet.review_required" class="portal-notice danger">{{tr('部分请求缺少用量或价格，额度调用已暂停，请联系管理员核对。')}}</p>
  <div class="portal-entitlements-grid">
    <section class="portal-card portal-redeem-card"><div class="portal-redeem-heading"><span class="portal-empty-icon"><UserIcon name="gift" /></span><h2>{{tr('兑换权益')}}</h2><p>{{tr('使用兑换码获取额度、并发或套餐。')}}</p></div><form @submit.prevent="redeem"><label for="userRedeemCode">{{tr('兑换码')}}</label><input id="userRedeemCode" v-model="code" :aria-label="tr('兑换码')" :placeholder="tr('输入你的兑换码')" required maxlength="200" autocomplete="off"><button class="btn primary" :disabled="busy">{{busy?tr('兑换中…'):tr('兑换')}}</button></form><p v-if="message" class="portal-notice success" role="status">{{message}}</p><p v-if="error" class="portal-notice danger" role="alert">{{error}}</p></section>
    <section class="portal-card"><div class="portal-section-heading"><div><h2>{{tr('我的订阅')}}</h2><p>{{tr('当前套餐与使用情况')}}</p></div><span class="portal-icon-surface"><UserIcon name="shield" /></span></div><template v-if="data?.subscription.id"><div class="portal-subscription-name"><h3>{{data.subscription.plan.name}}</h3><span class="portal-badge" :class="data.subscription.disabled||data.subscription.expires_at<Date.now()/1000?'':'success'">{{data.subscription.disabled?tr('已停用'):data.subscription.expires_at<Date.now()/1000?tr('已到期'):tr('生效中')}}</span></div><dl class="portal-detail-list"><div><dt>{{tr('到期时间')}}</dt><dd>{{consumerDate(data.subscription.expires_at)}}</dd></div><div><dt>{{tr('套餐并发')}}</dt><dd>{{data.subscription.plan.max_concurrency||tr('不限')}}</dd></div><div><dt>{{tr('本周期请求')}}</dt><dd>{{data.subscription.usage.requests}} / {{data.subscription.plan.budget.max_requests||tr('不限')}}</dd></div><div><dt>{{tr('本周期 Token')}}</dt><dd>{{data.subscription.usage.tokens.toLocaleString(dateLocale)}}</dd></div><div><dt>{{tr('本周期费用')}}</dt><dd>${{data.subscription.usage.usd.toFixed(6)}}</dd></div></dl></template><div v-else class="portal-empty compact"><UserIcon name="shield" /><h3>{{tr('暂无订阅套餐')}}</h3><p>{{tr('可通过兑换码获取，或联系管理员分配。')}}</p></div></section>
  </div>
  <section class="portal-card portal-table-card"><div class="portal-section-heading"><div class="portal-segmented"><button :class="{on:history==='redemptions'}" :aria-pressed="history==='redemptions'" @click="history='redemptions'">{{tr('兑换记录')}}</button><button :class="{on:history==='ledger'}" :aria-pressed="history==='ledger'" @click="history='ledger'">{{tr('额度流水')}}</button></div></div>
    <template v-if="history==='redemptions'"><div class="table-wrap"><table><thead><tr><th>{{tr('兑换时间')}}</th><th>{{tr('兑换码')}}</th><th>{{tr('获取权益')}}</th></tr></thead><tbody><tr v-for="r in data?.redemptions.slice().reverse()??[]" :key="r.id"><td class="portal-muted">{{consumerDate(r.used_at)}}</td><td class="mono">{{r.hint}}</td><td>{{r.kind==='balance'?'$'+credits(r.amount):r.kind==='concurrency'?tr('并发 +')+r.concurrency:tr('套餐订阅')}}</td></tr></tbody></table></div><div v-if="!data?.redemptions.length" class="portal-empty compact"><UserIcon name="gift" /><h3>{{tr('暂无兑换记录')}}</h3><p>{{tr('成功兑换的权益会显示在这里。')}}</p></div></template>
    <template v-else><div class="table-wrap"><table><thead><tr><th>{{tr('时间')}}</th><th>{{tr('类型')}}</th><th>{{tr('变动 USD')}}</th><th>{{tr('结余 USD')}}</th><th>{{tr('说明')}}</th></tr></thead><tbody><tr v-for="e in data?.entries??[]" :key="e.id"><td class="portal-muted">{{consumerDate(e.time)}}</td><td>{{e.kind==='admin'?tr('管理员调整'):e.kind==='redeem'?tr('兑换'):tr('调用消耗')}}</td><td class="portal-numeric" :class="e.delta>0?'portal-positive':''">{{e.delta>0?'+':''}}{{credits(e.delta)}}</td><td class="portal-numeric">{{credits(e.balance)}}</td><td>{{e.note||'—'}}</td></tr></tbody></table></div><div v-if="!data?.entries.length" class="portal-empty compact"><UserIcon name="wallet" /><h3>{{tr('暂无流水')}}</h3><p>{{tr('额度变动后将在这里显示。')}}</p></div></template>
  </section>
  <p class="portal-note">{{tr('账户额度与上游积分独立，按已配置参考价格结算。流水保留在全站最近 ')}}{{data?.retention_limit??10000}}{{tr(' 条范围内，累计余额独立保留。')}}</p>
</section>
</template>
