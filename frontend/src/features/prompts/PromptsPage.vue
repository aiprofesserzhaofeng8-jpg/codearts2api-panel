<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { api, isAbortError } from '../../api/client';
import { t as tr } from '../../i18n';
import type { components } from '../../api/generated/admin';
type Configuration = components['schemas']['PromptConfiguration'];
type Prompt = components['schemas']['PromptPolicy'];
type Platform = components['schemas']['PlatformPrompt'];
const platforms = [
  {id:'common',name:'公共默认值'}, {id:'codearts',name:'CodeArts'}, {id:'workbuddy',name:'WorkBuddy'},
  {id:'trae',name:'Trae'}, {id:'qoder',name:'Qoder'}, {id:'kiro',name:'Kiro'},
  {id:'chatgpt',name:'ChatGPT / Codex'}, {id:'claude',name:'Claude / Anthropic'}, {id:'teleagent',name:'TeleAgent'},
];
const draft=ref<Configuration>(), baseline=ref<Configuration>(), selected=ref('common'), busy=ref(false), loading=ref(false), error=ref(''), saved=ref('');
const protocol=ref('chat_completions'), sample=ref(''), preview=ref(''), previewError=ref(''), previewBusy=ref(false);
let alive=true, sequence=0, previewSequence=0;
const clone=<T,>(value:T):T=>JSON.parse(JSON.stringify(value));
const dirty=computed(()=>!!draft.value && JSON.stringify(draft.value)!==JSON.stringify(baseline.value));
const current=computed<Prompt|undefined>(()=>draft.value && (selected.value==='common'?draft.value.common:draft.value.platforms[selected.value]));
function inherited(field:keyof Prompt):boolean {
 if(selected.value==='common' || !draft.value)return false;
 const p=draft.value.platforms[selected.value]!;
 return p.overrides!=null && p.overrides[field]==null;
}
function value<K extends keyof Prompt>(field:K):Prompt[K]|undefined{
 if(!draft.value || !current.value)return undefined;
 if(inherited(field))return draft.value.common[field];
 const p=current.value as Platform;
 return (p.overrides?.[field] ?? current.value[field]) as Prompt[K];
}
function setValue<K extends keyof Prompt>(field:K,next:Prompt[K]){
 if(!current.value)return;
 current.value[field]=next;
 if(selected.value!=='common'){
  const p=current.value as Platform;
  if(p.overrides!=null)p.overrides[field]=next as never;
 }
 saved.value='';preview.value='';
}
function toggleInheritance(field:keyof Prompt,enabled:boolean){
 if(!draft.value || selected.value==='common')return;
 const p=draft.value.platforms[selected.value]!;
 if(p.overrides==null)p.overrides={prompt_mode:p.prompt_mode,prompt_text:p.prompt_text,prompt_fallback:p.prompt_fallback,sanitize:p.sanitize};
 if(enabled)delete p.overrides[field];else p.overrides[field]=draft.value.common[field] as never;
 preview.value='';saved.value='';
}
const mode=computed(()=>value('prompt_mode') || 'passthrough');
const text=computed(()=>value('prompt_text') || '');
const description=computed(()=>mode.value==='append'?'保留客户端原有系统指令，补充下面的要求。':mode.value==='custom'?'替换客户端系统指令；普通聊天历史和工具定义保持。':'使用客户端自带的系统指令，下方保存的文本不参与正常请求。');
watch([mode,text,selected,protocol,sample,()=>value('sanitize')],()=>{previewSequence++;preview.value='';previewError.value='';});
const bytes=computed(()=>new TextEncoder().encode(text.value).length);
async function load(force=false){
 if(busy.value || loading.value || dirty.value && !force)return;
 if(force && dirty.value && !confirm(tr('重新读取将丢弃当前草稿，是否继续？')))return;
 const id=++sequence;loading.value=true;error.value='';
 try{const response=await api('/admin/api/prompts');if(alive && id===sequence && response.common && response.platforms){baseline.value=clone(response);draft.value=clone(response);saved.value='';preview.value='';}}
 catch(e){if(alive && !isAbortError(e))error.value=e instanceof Error?e.message:String(e);}
 finally{if(alive && id===sequence)loading.value=false;}
}
async function save(){
 if(!draft.value || !baseline.value || busy.value)return;
 busy.value=true;error.value='';saved.value='';
 try{const response=await api('/admin/api/prompts',{method:'POST',body:{expected:baseline.value,configuration:draft.value}});if(alive){baseline.value=clone(response);draft.value=clone(response);saved.value=tr('提示词已保存，后续请求生效');}}
 catch(e){if(alive && !isAbortError(e))error.value=(e instanceof Error?e.message:String(e))+' · '+tr('草稿已保留');}
 finally{if(alive)busy.value=false;}
}
function resetSample(){
 const request=protocol.value==='responses'?{model:'example',instructions:'Keep the client rules.',input:'Explain this code.'}:protocol.value==='anthropic'?{model:'example',system:'Keep the client rules.',max_tokens:128,messages:[{role:'user',content:'Explain this code.'}]}:{model:'example',messages:[{role:'system',content:'Keep the client rules.'},{role:'user',content:'Explain this code.'}]};
 sample.value=JSON.stringify(request,null,2);preview.value='';previewError.value='';
}
async function runPreview(){
 const id=++previewSequence;previewBusy.value=true;previewError.value='';preview.value='';
 try{
 JSON.parse(sample.value);
 const output=await api('/admin/api/prompts/preview',{method:'POST',body:{protocol:protocol.value,mode:mode.value,text:text.value,sanitize:!!value('sanitize'),request_json:sample.value}});
 if(alive && id===previewSequence)preview.value=output.request_json;
 }catch(e){if(alive && id===previewSequence && !isAbortError(e))previewError.value=e instanceof Error?e.message:String(e);}
 finally{if(alive)previewBusy.value=false;}
}
function onRefresh(){void load();}
function beforeUnload(e:BeforeUnloadEvent){if(dirty.value){e.preventDefault();e.returnValue='';}}
onMounted(()=>{resetSample();window.addEventListener('prompts-refresh',onRefresh);window.addEventListener('beforeunload',beforeUnload);if(location.hash==='#prompts')void load();});
onUnmounted(()=>{alive=false;sequence++;window.removeEventListener('prompts-refresh',onRefresh);window.removeEventListener('beforeunload',beforeUnload);});
</script>

<template>
<section id="tab-prompts" role="tabpanel" aria-labelledby="nav-prompts">
 <div class="card">
  <div class="card-head"><div><h2>{{tr('提示词管理')}}</h2><p class="card-sub">{{tr('保留客户端规则，或明确选择替换。保存后生效。')}}</p></div><div class="actions"><span v-if="dirty" class="badge warn">{{tr('有未保存的改动')}}</span><button class="btn" :disabled="busy || loading" @click="load(true)">{{tr('重新读取')}}</button><button id="savePrompts" class="btn primary" :disabled="!dirty || busy || loading" @click="save">{{busy?tr('保存中…'):tr('保存提示词')}}</button></div></div>
  <p v-if="error" class="err-text prompt-notice" role="alert">{{error}}</p><p v-if="saved" class="prompt-notice" role="status">{{saved}}</p>
  <p v-if="loading && !draft" class="prompt-notice">{{tr('读取中')}}</p>
  <div v-if="draft" class="prompt-workspace">
   <nav class="prompt-scopes" :aria-label="tr('应用范围')"><button v-for="p in platforms" :key="p.id" class="prompt-scope" :class="{selected:selected===p.id}" :aria-pressed="selected===p.id" :disabled="busy" @click="selected=p.id;preview='';previewError=''">{{tr(p.name)}}<small v-if="p.id==='common'">{{tr('继承此配置的平台共用')}}</small></button></nav>
   <div class="prompt-editor">
    <h3>{{tr(platforms.find(p=>p.id===selected)!.name)}}</h3>
    <p v-if="selected==='common'" class="hint">{{tr('平台启用继承的字段才采用公共值。其他已接入平台使用公共默认值。')}}</p>
    <fieldset :disabled="busy">
     <label v-if="selected!=='common'" class="check"><input type="checkbox" :checked="inherited('prompt_mode')" @change="toggleInheritance('prompt_mode',($event.target as HTMLInputElement).checked)">{{tr('继承公共模式')}}</label>
     <div class="field"><label for="promptMode">{{tr('处理方式')}}</label><select id="promptMode" :value="mode" :disabled="inherited('prompt_mode')" @change="setValue('prompt_mode',($event.target as HTMLSelectElement).value as Prompt['prompt_mode'])"><option value="passthrough">{{tr('透传 · 保留客户端指令')}}</option><option value="append">{{tr('追加 · 保留原指令并补充')}}</option><option value="custom">{{tr('替换 · 使用自定义指令')}}</option></select><p class="hint" :class="{'prompt-replace-warning':mode==='custom'}">{{tr(description)}}</p></div>
     <label v-if="selected!=='common'" class="check"><input type="checkbox" :checked="inherited('prompt_text')" @change="toggleInheritance('prompt_text',($event.target as HTMLInputElement).checked)">{{tr('继承公共文本')}}</label>
     <div class="field"><label for="promptText">{{tr('提示词内容')}} <span v-if="mode==='passthrough'" class="muted">{{tr('（当前模式不使用）')}}</span></label><textarea id="promptText" rows="10" :value="text" :disabled="inherited('prompt_text')" @input="setValue('prompt_text',($event.target as HTMLTextAreaElement).value)" :placeholder="tr('例如：在满足需求和正确性的前提下，优先采用简单、易维护的方案。')"></textarea><div class="row between"><small :class="bytes>65536?'err-text':'muted'">{{bytes.toLocaleString()}} / 65,536 bytes</small><small class="muted">{{tr(mode==='custom'?'替换模式留空时使用内置工程助手指令。':'追加模式留空时不增加内容。')}}</small></div></div>
     <details class="prompt-advanced"><summary>{{tr('兼容性设置')}}</summary><p class="hint">{{tr('保留原有兼容策略；拦截恢复只在透传模式下触发。')}}</p><label v-if="selected!=='common'" class="check"><input type="checkbox" :checked="inherited('prompt_fallback')" @change="toggleInheritance('prompt_fallback',($event.target as HTMLInputElement).checked)">{{tr('继承公共值 · 拦截恢复')}}</label><label class="check"><input type="checkbox" :checked="value('prompt_fallback')" :disabled="inherited('prompt_fallback')" @change="setValue('prompt_fallback',($event.target as HTMLInputElement).checked)">{{tr('透传遇明确拦截时重试一次')}}</label><label v-if="selected!=='common'" class="check"><input type="checkbox" :checked="inherited('sanitize')" @change="toggleInheritance('sanitize',($event.target as HTMLInputElement).checked)">{{tr('继承公共值 · 清洗')}}</label><label class="check"><input type="checkbox" :checked="value('sanitize')" :disabled="inherited('sanitize')" @change="setValue('sanitize',($event.target as HTMLInputElement).checked)">{{tr('仅清洗系统提示词中的客户端模板')}}</label></details>
    </fieldset>
   </div>
  </div>
 </div>
 <div v-if="draft" class="card"><div class="card-head"><div><h2>{{tr('注入预览')}}</h2><p class="card-sub">{{tr('使用当前编辑内容预览请求，不调用上游、不消耗模型额度。')}}</p></div><button id="previewPrompt" class="btn" :disabled="previewBusy || busy" @click="runPreview">{{tr('预览效果')}}</button></div><div class="card-body"><div class="field"><label for="promptProtocol">{{tr('API 协议')}}</label><select id="promptProtocol" v-model="protocol" @change="resetSample"><option value="chat_completions">Chat Completions</option><option value="responses">Responses</option><option value="anthropic">Anthropic Messages</option></select></div><div class="prompt-preview-grid"><div class="field"><label for="promptSample">{{tr('原始请求')}}</label><textarea id="promptSample" v-model="sample" rows="12" spellcheck="false" @input="preview=''"/></div><div class="field"><label for="promptOutput">{{tr('处理后的请求')}}</label><textarea id="promptOutput" :value="preview" rows="12" readonly spellcheck="false" :placeholder="tr('点击预览效果，查看指令变化')"/></div></div><p v-if="previewError" class="err-text" role="alert">{{previewError}}</p></div></div>
</section>
</template>

<style scoped>
.prompt-workspace{display:grid;grid-template-columns:210px minmax(0,1fr);border-top:1px solid var(--line)}
.prompt-scopes{display:flex;flex-direction:column;gap:4px;padding:16px;border-right:1px solid var(--line)}
.prompt-scope{display:flex;flex-direction:column;align-items:flex-start;gap:4px;border:1px solid transparent;border-radius:8px;background:transparent;padding:11px 12px;font:inherit;color:var(--text-2);cursor:pointer;text-align:left}
.prompt-scope small{font-size:12px;color:var(--text-3)}
.prompt-scope.selected{background:var(--accent-soft);border-color:var(--accent-line);color:var(--text)}
.prompt-editor{padding:24px;min-width:0}.prompt-editor h3{margin:0 0 16px}.prompt-editor fieldset{border:0;padding:0;margin:0;min-width:0}.prompt-editor .field{margin:12px 0 18px}.prompt-editor .check{margin:8px 0}.prompt-editor textarea{min-height:220px;resize:vertical}.prompt-replace-warning{color:var(--accent-text)}.prompt-advanced{border-top:1px solid var(--line);padding-top:18px;margin-top:20px}.prompt-advanced summary{cursor:pointer}.prompt-notice{padding:0 24px}.prompt-preview-grid{display:grid;grid-template-columns:1fr 1fr;gap:18px}.prompt-preview-grid textarea{font-family:var(--font-mono,monospace);font-size:13px;resize:vertical}.prompt-preview-grid .field{min-width:0}
@media(max-width:700px){.prompt-workspace{grid-template-columns:1fr}.prompt-scopes{flex-direction:row;overflow-x:auto;border-right:0;border-bottom:1px solid var(--line)}.prompt-scope{flex-shrink:0}.prompt-editor{padding:16px}.prompt-preview-grid{grid-template-columns:1fr}.prompt-scope small{display:none}}
</style>
