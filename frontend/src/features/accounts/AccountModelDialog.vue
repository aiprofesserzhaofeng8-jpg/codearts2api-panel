<script setup lang="ts">
import {ref,onMounted,onUnmounted} from 'vue';
import {api,isAbortError} from '../../api/client';
import type {components} from '../../api/generated/admin';
import {t as tr,dateLocale} from '../../i18n';
type Snapshot=components['schemas']['AccountModelSnapshot'];
const dialog=ref<HTMLDialogElement>(), account=ref<{id:string;name:string;enabled:boolean}>(), snapshot=ref<Snapshot|null>(null), pending=ref(false), error=ref('');
let generation=0;
async function read(sync:boolean){
 if(!account.value || pending.value)return;
 const seq=++generation;
 pending.value=true;error.value='';
 try {
  const result=await api<{snapshot:Snapshot|null}>('/admin/api/accounts/models?id='+encodeURIComponent(account.value.id),{method:sync?'POST':'GET'});
  if(seq===generation)snapshot.value=result.snapshot;
 }catch(e){if(seq===generation&&!isAbortError(e))error.value=(e as Error).message}
 finally{if(seq===generation)pending.value=false}
}
function open(e:Event){generation++;account.value=(e as CustomEvent).detail;snapshot.value=null;pending.value=false;error.value='';dialog.value?.showModal();void read(false)}
onMounted(()=>window.addEventListener('account-model-list',open));
onUnmounted(()=>{generation++;window.removeEventListener('account-model-list',open)});
</script>
<template>
<dialog ref="dialog" class="modal" aria-labelledby="accountModelsTitle">
 <div class="modal-head"><h2 id="accountModelsTitle">{{tr('上游模型')}}</h2><button class="btn" @click="dialog?.close()">{{tr('关闭')}}</button></div>
 <div class="modal-body">
  <h3>{{account?.name}}</h3>
  <p v-if="error" class="err-text" role="alert">{{error}}</p>
  <template v-if="snapshot">
   <p>{{tr('上次同步成功')}}：{{new Date(snapshot.list.observed_at).toLocaleString(dateLocale)}}</p>
   <p>{{snapshot.first?tr('首次同步') : tr('本次目录')}} · {{snapshot.list.models.length}} {{tr('个模型')}}</p>
   <p v-if="snapshot.list.kind && snapshot.list.kind!=='live'">{{tr(snapshot.list.kind==='reference'?'预置参考目录':snapshot.list.kind==='configured'?'账号配置目录':'决策模型目录')}}</p>
   <div v-if="!snapshot.first" class="model-changes">
    <details v-for="(ids,key) in snapshot.delta" :key="key"><summary>{{tr(key==='added'?'新增型号':key==='missing'?'此次未返回':'仍在目录')}} ({{ids?.length??0}})</summary><ul v-if="ids?.length"><li v-for="id in ids" :key="id">{{id}}</li></ul></details>
   </div>
   <div v-if="snapshot.list.models.length" class="table-wrap"><table><thead><tr><th>ID</th><th>{{tr('名称')}}</th><th>{{tr('上下文')}}</th><th>{{tr('最大输出')}}</th></tr></thead><tbody>
    <tr v-for="m in snapshot.list.models" :key="m.id"><td>{{m.id}}</td><td>{{m.name}}</td><td>{{m.context||'—'}}</td><td>{{m.max_output||'—'}}</td></tr>
   </tbody></table></div>
  </template>
  <p v-else-if="!error">{{pending?tr('读取中'):tr('尚无同步记录')}}</p>
 </div>
 <div class="modal-actions"><button class="btn primary" :disabled="pending||!account?.enabled" @click="read(true)">{{pending?tr('处理中…'):tr('同步并保存')}}</button></div>
</dialog>
</template>
<style scoped>.modal .table-wrap table{min-width:0;width:100%}.model-changes{display:flex;gap:12px;flex-wrap:wrap;margin-bottom:12px}.model-changes summary{cursor:pointer}.modal-body{overflow:auto}.modal-body td:first-child,.model-changes li{overflow-wrap:anywhere}</style>
