<script setup lang="ts">
import { ref } from 'vue';
import type { components } from '../../api/generated/admin';
import UserIcon from './UserIcon.vue';
import { userNumber, userDate } from './user-format';
defineProps<{keys:components['schemas']['UserKey'][]; busy:boolean; revealed:Record<string,string>; apiBase:string}>();
const emit=defineEmits<{create:[name:string]; reveal:[ref:string]; copy:[ref:string]; change:[ref:string,action:string]}>();
const name=defineModel<string>('name',{required:true});
const copied=ref(false), copyError=ref('');
async function copyAddress(value:string){try{await navigator.clipboard.writeText(value);copied.value=true;}catch{copyError.value='请选中上方 API 地址手动复制。';}}
</script>
<template>
  <section id="userKeys" class="portal-stack">
    <div class="portal-card portal-endpoint"><span class="portal-icon-surface"><UserIcon name="link" /></span><div><span class="portal-eyebrow">API 接入地址</span><code>{{apiBase}}</code></div><button class="btn" :aria-label="copied?'地址已复制':'复制 API 地址'" @click="copyAddress(apiBase)"><UserIcon :name="copied?'check':'copy'" /><span>{{copied?'已复制':'复制地址'}}</span></button><p v-if="copyError" class="portal-note">{{copyError}}</p></div>
    <div class="portal-card portal-table-card">
      <div class="portal-section-heading"><div><h2>我的 API 密钥 <span class="portal-count">{{keys.length}}</span></h2><p>为不同客户端创建密钥，所有密钥共享账户额度。</p></div></div>
      <form class="portal-toolbar" @submit.prevent="emit('create',name)"><input v-model="name" aria-label="密钥备注" placeholder="为新密钥添加备注（可选）" maxlength="80" :disabled="busy"><button class="btn primary" id="userCreateKey" :disabled="busy||keys.length>=32"><UserIcon name="plus" />创建密钥</button></form>
      <div class="table-wrap"><table><thead><tr><th>名称 / 密钥</th><th>状态</th><th>请求次数</th><th>最近使用</th><th class="portal-align-right">操作</th></tr></thead><tbody>
        <tr v-for="k in keys" :key="k.ref"><td><strong class="portal-key-name">{{k.name||'未命名密钥'}}</strong><input class="mono key-value" readonly :type="revealed[k.ref]?'text':'password'" :value="revealed[k.ref]??k.key" :aria-label="'密钥 '+(k.name||k.ref)" autocomplete="off"></td><td><span class="portal-badge" :class="k.enabled?'success':''"><i></i>{{k.enabled?'启用':'停用'}}</span></td><td class="portal-numeric">{{userNumber(k.budget_usage.requests)}}</td><td class="portal-muted">{{userDate(k.last_used)}}</td><td><div class="portal-row-actions"><button class="btn xs" :disabled="busy" @click="emit('reveal',k.ref)">{{revealed[k.ref]?'隐藏':'显示'}}</button><button class="btn xs" :disabled="busy" @click="emit('copy',k.ref)">复制</button><button class="btn xs" :disabled="busy" @click="emit('change',k.ref,'toggle')">{{k.enabled?'停用':'启用'}}</button><button class="btn xs danger" :disabled="busy" @click="emit('change',k.ref,'delete')">删除</button></div></td></tr>
      </tbody></table></div>
      <div v-if="!keys.length" class="portal-empty"><span class="portal-empty-icon"><UserIcon name="key" /></span><h3>还没有 API 密钥</h3><p>创建第一把密钥，即可在客户端使用模型服务。</p></div>
      <div class="portal-table-footer"><span>共 {{keys.length}} 把密钥</span><span>最多可创建 32 把</span></div>
    </div>
  </section>
</template>
