<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue';
import { api, isAbortError } from '../../api/client';
import { t } from '../../i18n';
import type { components } from '../../api/generated/admin';
type Service = components['schemas']['SearchService'];
const path = '/admin/api/search-services';
const catalog = ref<components['schemas']['SearchCatalog']>();
const dialog = ref<HTMLDialogElement>();
const form = ref({ id: '', name: '', provider: 'deepseek', endpoint: '', key: '', clear: false, keySet: false });
const busy = ref(false), error = ref(''), notice = ref('');
const sample = ref<components['schemas']['ExternalSearchResult'][]>([]);
async function load() { catalog.value = await api(path); }
function visit() {
  if (location.hash === '#settings') load().then(() => error.value = '').catch(e => { if (!isAbortError(e)) error.value = e.message; });
}
onMounted(() => window.addEventListener('settings-refresh', visit));
onUnmounted(() => window.removeEventListener('settings-refresh', visit));
function edit(service?: Service) {
  form.value = { id: service?.id ?? '', name: service?.name ?? '', provider: service?.provider ?? 'deepseek', endpoint: service?.endpoint ?? '', key: '', clear: false, keySet: service?.api_key_set ?? false };
  error.value = ''; dialog.value?.showModal();
}
function close() { form.value.key = ''; dialog.value?.close(); }
async function change(method: string, suffix: string, values: Record<string,unknown> = {}) {
  if (busy.value || !catalog.value) return;
  busy.value = true; error.value = ''; notice.value = ''; sample.value = [];
  const revision = catalog.value.revision;
  try {
    if (method === 'DELETE') await api(path + suffix + '?expected_revision=' + revision, { method });
    else await api(path + suffix, { method, body: { ...values, expected_revision: revision } });
    close(); await load();
  } catch (e) { error.value = (e as Error).message; }
  finally { busy.value = false; }
}
function save() {
  const f = form.value;
  void change(f.id ? 'PATCH' : 'POST', f.id ? '/' + f.id : '', { name: f.name, provider: f.provider, ...(f.provider === 'searxng' ? {endpoint:f.endpoint} : {api_key:f.key, clear_api_key:f.clear, ...(f.clear ? {enabled:false} : {})}) });
}
async function test(s: Service) {
  if (busy.value || !window.confirm(t('执行一次搜索，可能消耗服务商额度。继续？'))) return;
  busy.value = true; error.value = ''; sample.value = []; notice.value = '';
  try {
    const data = await api<{latency_ms:number;result_count:number;results:components['schemas']['ExternalSearchResult'][]}>(path+'/'+s.id+'/test',{method:'POST'});
    notice.value = s.name+' · '+data.result_count+' '+t('条结果')+' · '+data.latency_ms+' ms'; sample.value = data.results;
  } catch(e) { error.value=(e as Error).message; } finally { busy.value=false; }
}
function remove(s: Service) {
  if (window.confirm(t('删除此搜索服务并解除分组授权？'))) void change('DELETE','/'+s.id);
}
</script>
<template>
<div class="card" id="externalSearch">
 <div class="card-head"><div><h2>{{t('外部搜索')}}</h2></div><div class="actions"><button class="btn" @click="visit" :disabled="busy"><span aria-hidden="true">↻</span>{{t('重新读取')}}</button><button class="btn primary" :disabled="busy || !catalog" @click="edit()"><span aria-hidden="true">＋</span>{{t('添加搜索服务')}}</button></div></div>
 <div class="card-body">
 <div class="table-wrap" v-if="catalog?.services.length"><table><thead><tr><th>{{t('名称')}}</th><th>{{t('服务商')}}</th><th>{{t('状态')}}</th><th>{{t('默认')}}</th><th>{{t('操作')}}</th></tr></thead><tbody>
 <tr v-for="s in catalog.services" :key="s.id"><td><strong>{{s.name}}</strong></td><td>{{s.provider==='deepseek'?'DeepSeek':'SearXNG'}}</td><td><span class="service-status" :class="{ active: s.enabled }">{{t(s.enabled?'已启用':'已停用')}}</span></td><td><button class="btn" :disabled="busy || !s.enabled" @click="change('PUT','/default',{service_id:catalog.default_service_id===s.id?'':s.id})">{{t(catalog.default_service_id===s.id?'取消默认':'设为默认')}}</button></td>
 <td><div class="actions"><button class="btn row-action row-probe" :disabled="busy" @click="test(s)">{{t('测试')}}</button><button class="btn row-action row-edit" :disabled="busy" @click="edit(s)">{{t('编辑')}}</button><button class="btn row-action" :class="s.enabled?'row-disable':'row-enable'" :disabled="busy" @click="change('PATCH','/'+s.id,{enabled:!s.enabled})">{{t(s.enabled?'停用':'启用')}}</button><button class="btn row-action row-delete" :disabled="busy" @click="remove(s)">{{t('删除')}}</button></div></td></tr></tbody></table></div>
 <p v-else-if="catalog">{{t('尚未添加搜索服务')}}</p>
 <p v-if="notice" role="status">{{notice}}</p><ul v-if="sample.length"><li v-for="r in sample" :key="r.url"><a :href="r.url" target="_blank" rel="noopener noreferrer">{{r.title || r.url}}</a></li></ul>
 <p v-if="error && !dialog?.open" role="alert">{{error}}</p></div>
 <dialog class="modal search-editor" aria-labelledby="searchServiceTitle" ref="dialog" @close="form.key=''"><form @submit.prevent="save"><div class="modal-head"><h2 id="searchServiceTitle">{{t(form.id?'编辑搜索服务':'添加搜索服务')}}</h2></div>
 <fieldset class="editor-fields" :disabled="busy"><label class="field"><span>{{t('名称')}}</span><input v-model="form.name" maxlength="80" required></label>
 <label class="field"><span>{{t('服务商')}}</span><select v-model="form.provider" :disabled="!!form.id"><option value="deepseek">DeepSeek</option><option value="searxng">SearXNG</option></select></label>
 <template v-if="form.provider==='deepseek'"><label class="field"><span>API Key</span><input type="password" v-model="form.key" autocomplete="new-password" :disabled="form.clear" :placeholder="form.keySet?t('已保存，留空保留'):''"></label>
 <label class="check" v-if="form.keySet"><input v-model="form.clear" type="checkbox" @change="form.key=''">{{t('清除密钥并停用')}}</label></template>
 <label v-else class="field"><span>{{t('搜索地址')}}</span><input v-model="form.endpoint" type="url" placeholder="http://127.0.0.1:8080/search"></label>
 <p v-if="error" role="alert">{{error}}</p></fieldset><div class="modal-actions"><button type="button" class="btn" :disabled="busy" @click="close">{{t('取消')}}</button><button class="btn primary" :disabled="busy">{{t('保存')}}</button></div></form></dialog>
</div></template>

<style scoped>
.actions { display:flex; align-items:center; gap:6px; }
.card-head { flex-wrap:wrap; justify-content:space-between; }
.card-head .btn { display:inline-flex; align-items:center; gap:6px; }
.service-status { display:inline-flex; align-items:center; gap:6px; white-space:nowrap; }
.service-status::before { content:""; width:7px; height:7px; border-radius:50%; background:var(--text-3); }
.service-status.active::before { background:var(--ok, #188356); }
.search-editor { width:min(440px, calc(100vw - 32px)); }
.editor-fields { display:grid; gap:16px; border:0; margin:0; min-width:0; padding:20px 22px; }
.editor-fields .field { display:grid; gap:6px; margin:0; }

@media (max-width: 640px) {
  .table-wrap { overflow: visible; }
  table, tbody { display: block; width: 100%; min-width: 0; }
  thead { display: none; }
  tr { display: grid; grid-template-columns: 1fr auto; align-items: center; gap: .55rem; padding: .75rem 0; }
  td { display: block; padding: 0; border: 0; }
  td:first-child { font-weight: 650; overflow-wrap: anywhere; }
  td:last-child { grid-column: 1 / -1; }
  .actions { flex-wrap: wrap; }
}
</style>
