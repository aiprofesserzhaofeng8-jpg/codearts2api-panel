<script setup lang="ts">
import { computed, ref, onBeforeUnmount } from 'vue';
import { api } from '../../api/client';
import { t } from '../../i18n';
import { keyExample, type Client } from './key-example';
const props=defineProps<{role:'admin'|'user'}>();
const box=ref<HTMLDialogElement>(),client=ref<Client>('cURL'),model=ref(''),models=ref<string[]>([]);
const clients:Client[]=['cURL','Codex','Claude Code'];
const loading=ref(false),error=ref(''),copied=ref(false),copyError=ref(false),name=ref('');
const example=computed(()=>keyExample(client.value,location.origin,model.value));
let pending:AbortController|undefined;
async function open(refId:string,label:string) {
  pending?.abort(); const request=new AbortController();pending=request;
  model.value=''; models.value=[]; client.value='cURL'; error.value='';copied.value=false;copyError.value=false;
  name.value=label; loading.value=true;
  if(!box.value?.open)box.value?.showModal();
  try {
    const result=await api<{models:string[]}>(`/${props.role}/api/keys/models?ref=${encodeURIComponent(refId)}`,{signal:request.signal});
    if(!request.signal.aborted){models.value=result.models;model.value=result.models[0]??'';}
  } catch(e){if(!request.signal.aborted)error.value=e instanceof Error?e.message:String(e);}
  finally{if(!request.signal.aborted)loading.value=false;}
}
async function copy(){try{await navigator.clipboard.writeText(example.value);copied.value=true;copyError.value=false;}catch{copyError.value=true;}}
onBeforeUnmount(()=>pending?.abort());
defineExpose({open});
</script>
<template>
 <dialog ref="box" class="modal key-use-dialog" :aria-label="t('使用密钥')" @close="pending?.abort()">
  <form method="dialog" class="modal-head"><div><h2>{{t('使用密钥')}}</h2><small>{{name}}</small></div><button class="btn xs">{{t('关闭')}}</button></form>
  <div class="modal-body">
   <p v-if="loading" role="status">{{t('加载中…')}}</p><p v-else-if="error" role="alert">{{error}}</p>
   <p v-else-if="!model">{{t('这把密钥没有可推荐的模型，请检查分组授权与模型来源。')}}</p>
   <template v-else>
    <label class="field">{{t('模型')}}<select v-model="model" @change="copied=false;copyError=false"><option v-for="id in models" :key="id">{{id}}</option></select></label>
    <div class="row key-client-options"><button v-for="c in clients" :key="c" class="btn xs" :class="{primary:client===c}" :aria-pressed="client===c" @click="client=c;copied=false;copyError=false">{{c}}</button></div>
    <p class="hint">{{t('在 Bash 或 Zsh 中先设置环境变量 CODEARTS_API_KEY，再运行示例。')}}</p>
    <pre tabindex="0">{{example}}</pre>
    <button class="btn primary" @click="copy">{{t(copied?'已复制':'复制命令')}}</button>
    <p v-if="copyError" role="alert">{{t('请选中示例手动复制。')}}</p>
   </template>
  </div>
 </dialog>
</template>
<style scoped>
.key-client-options{margin:16px 0 8px}.key-use-dialog pre{white-space:pre-wrap;overflow-wrap:anywhere;padding:12px;border:1px solid var(--line);border-radius:6px;line-height:1.5}
</style>
