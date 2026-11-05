<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { api, isAbortError } from '../../api/client';
import { serverState } from '../../shared/read-model';
import { refresh } from '../../shared/refresh';
import { t as tr, dateLocale } from '../../i18n';
import { canonicalSource, editError, parseMigrationReport, reloadUnifiedModels, routingRevision, unifiedModels, type MigrationReport, type ModelPolicy, type UnifiedDetail, type UnifiedModel } from '../../shared/unified-models';
const dialog = ref<HTMLDialogElement>(), error = ref(''), listError = ref(''), busy = ref(false), loading = ref(false), draft = ref<UnifiedModel>(), detail = ref<UnifiedDetail>();
const name = ref(''), enabled = ref(true), sources = ref<Array<{
    id: string;
    enabled: boolean;
}>>([]), policyEnabled = ref(false), policy = ref<Partial<ModelPolicy>>({});
const expected = ref(0), search = ref(''), testing = ref(''), report = ref<MigrationReport>(), reportError = ref('');
const probeResults = ref<Record<string, {
    ok: boolean;
    message: string;
    time: string;
}>>({});
let epoch = 0;
const rawModels = computed(() => (serverState.data?.models ?? []).filter(m => m.source !== 'alias'));
const available = computed(() => new Map(rawModels.value.map(m => [canonicalSource(m.id), m])));
const filtered = computed(() => unifiedModels.value.filter(m => m.name.toLowerCase().includes(search.value.toLowerCase())));
const fields = [['context_window', '推荐上下文'], ['max_output_tokens', '最大输出'], ['reserve_tokens', '压缩预留'], ['keep_recent_tokens', '保留近期历史'], ['trigger_tokens', '压缩触发值'], ['summary_max_tokens', '摘要输出预算']] as const;
async function load() { loading.value = true; listError.value = ''; try {
    await reloadUnifiedModels();
}
catch (e) {
    if (!isAbortError(e))
        listError.value = editError(e);
}
finally {
    loading.value = false;
} }
onMounted(load);
watch(() => serverState.data?.settings?.model_routing_revision, value => { if (value && value !== routingRevision.value)
    void load(); });
function close() { if (busy.value || testing.value)
    return; epoch++; dialog.value?.close(); }
async function open(model?: UnifiedModel) {
    const current = ++epoch;
    loading.value = true;
    listError.value = '';
    try {
        await reloadUnifiedModels();
        const value = model ? await api<UnifiedDetail>('/admin/api/unified-models/' + model.id) : undefined;
        if (current !== epoch)
            return;
        detail.value = value;
        draft.value = value?.model;
        name.value = value?.model.name ?? '';
        enabled.value = value?.model.enabled ?? true;
        sources.value = structuredClone(value?.model.sources ?? []);
        policyEnabled.value = !!value?.client_policy;
        policy.value = { ...(value?.client_policy ?? {}) };
        expected.value = value?.routing_revision ?? routingRevision.value;
        error.value = '';
        if (!dialog.value?.open)
            dialog.value?.showModal();
    }
    catch (e) {
        if (!isAbortError(e))
            listError.value = editError(e);
    }
    finally {
        loading.value = false;
    }
}
function addSource(id: string) { if (id && !sources.value.some(s => s.id === id))
    sources.value.push({ id, enabled: true }); }
async function save() {
    if (busy.value)
        return;
    if (!name.value || !/\d/.test(name.value) || /[\s/]/.test(name.value) || new TextEncoder().encode(name.value).length > 200 || sources.value.length < 1 || sources.value.length > 16) {
        error.value = tr('公开名称须带版本号、不含空白或斜线，并选择 1–16 个来源。');
        return;
    }
    const partitions = sources.value.map(s => s.id.startsWith('codearts/') ? 'codearts' : s.id.split('/').slice(0, 2).join('/'));
    if (new Set(partitions).size !== partitions.length) {
        error.value = tr('每个平台和地区只能绑定一个来源型号。');
        return;
    }
    let clientPolicy: ModelPolicy | null = null;
    if (policyEnabled.value) {
        if (fields.some(([key]) => !Number.isSafeInteger(Number(policy.value[key])) || Number(policy.value[key]) <= 0)) {
            error.value = tr('请填写有效整数参数');
            return;
        }
        clientPolicy = { ...policy.value, ...Object.fromEntries(fields.map(([key]) => [key, Number(policy.value[key])])), summary_enabled: false, summary_retry: false, disable_summary_thinking: false, summary_fallback_model: '', source: policy.value.source || tr('管理员公共建议；以来源已验证边界为限') } as ModelPolicy;
    }
    busy.value = true;
    error.value = '';
    try {
        const body = { name: name.value, enabled: enabled.value, sources: sources.value, client_policy: clientPolicy, expected_routing_revision: expected.value, ...(draft.value ? { expected_revision: draft.value.revision } : {}) };
        const saved=await api<UnifiedDetail>('/admin/api/unified-models' + (draft.value ? '/' + draft.value.id : ''), { method: draft.value ? 'PUT' : 'POST', body });
        if(!saved?.model || saved.model.name!==name.value || !/^um_[0-9a-f]{32}$/.test(saved.model.id) || !saved.model.revision || !saved.routing_revision)throw new Error(tr('保存结果未确认，请重新读取核对。'));
        dialog.value?.close();
        await load();
        await refresh();
    }
    catch (e) {
        if (!isAbortError(e))
            error.value = editError(e);
    }
    finally {
        busy.value = false;
    }
}
async function remove(model: UnifiedModel) {
    if (busy.value || !confirm(tr('确定删除统一模型') + ' ' + model.name + '？'))
        return;
    busy.value = true;
    listError.value = '';
    try {
        await api('/admin/api/unified-models/' + model.id, { method: 'DELETE', body: { expected_revision: model.revision, expected_routing_revision: routingRevision.value } });
        await load();
        await refresh();
    }
    catch (e) {
        if (!isAbortError(e))
            listError.value = editError(e);
    }
    finally {
        busy.value = false;
    }
}
async function probe(id: string) {
    if (testing.value || busy.value || !confirm(tr('测活会发起一次真实文本请求并消耗额度，是否继续？')))
        return;
    testing.value = id;
    error.value = '';
    try {
        const result = await api<{
            results: Array<{
                model: string;
                ok: boolean;
                error?: string;
            }>;
        }>('/admin/api/models/probe', { method: 'POST', body: { models: [id] } });
        const row = result.results?.find(x => canonicalSource(x.model) === id);
        probeResults.value[id] = { ok: row?.ok === true, message: row?.ok ? tr('文本测活成功') : row?.error || tr('上游未返回测活结果'), time: new Date().toLocaleString(dateLocale) };
    }
    catch (e) {
        if (!isAbortError(e))
            probeResults.value[id] = { ok: false, message: editError(e), time: new Date().toLocaleString(dateLocale) };
    }
    finally {
        testing.value = '';
    }
}
async function readReport(event: Event) {
    reportError.value = '';
    const file = (event.target as HTMLInputElement).files?.[0];
    if (!file)
        return;
    try {
        if (file.size > 1 << 20)
            throw new Error(tr('迁移报告不能超过 1 MiB'));
        report.value = parseMigrationReport(await file.text());
    }
    catch (e) {
        report.value = undefined;
        reportError.value = editError(e);
    }
    (event.target as HTMLInputElement).value = '';
}
function reread() { if (draft.value && confirm(tr('重新读取将丢弃当前草稿，是否继续？')))
    void open(draft.value); }
const referenceLabels={group_grant:'分组授权',group_default:'分组默认',client_default:'客户端推荐',global_default:'全局默认'} as const;
function observed(value?:string){return value?new Date(value).toLocaleString(dateLocale):'—';}
</script>
<template>
  <div class="card unified-panel">
    <div class="card-head">
      <div>
        <h2>{{tr('统一模型')}}</h2>
        <p class="card-sub">{{tr('在此定义公开名称，再到分组页分配权限。')}}</p>
      </div>
      <div class="actions">
        <button id="reloadUnifiedModels" class="btn" :disabled="loading||busy" @click="load">{{tr('重新读取')}}</button>
        <button id="addUnifiedModel" class="btn primary" :disabled="loading||busy" @click="open()">{{tr('创建统一模型')}}</button>
      </div>
    </div>
    <div class="unified-toolbar">
      <input v-model="search" type="search" :placeholder="tr('搜索统一名称')" :aria-label="tr('搜索统一名称')">
      <span class="hint">{{filtered.length}} / {{unifiedModels.length}}</span>
    </div>
    <p v-if="listError" class="err-text" role="alert">{{listError}}</p>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>{{tr('公开名称')}}</th>
            <th>{{tr('来源')}}</th>
            <th>{{tr('状态')}}</th>
            <th>{{tr('操作')}}</th>
          </tr>
        </thead>
        <tbody id="unifiedModels">
          <tr v-for="model in filtered" :key="model.id">
            <td class="mono">{{model.name}}</td>
            <td>{{model.sources.length}}<span v-if="model.sources.some(s=>!available.has(s.id))" class="badge warn">{{tr('目录缺失')}}</span>
            </td>
            <td>
              <span :class="['badge',model.enabled?'ok':'warn']">{{model.enabled?tr('启用'):tr('停用')}}</span>
            </td>
            <td class="actions-cell">
              <button class="btn xs" :disabled="busy||loading" @click="open(model)">{{tr('编辑')}}</button>
              <button class="btn xs danger" :disabled="busy||loading" @click="remove(model)">{{tr('删除')}}</button>
            </td>
          </tr>
          <tr v-if="!filtered.length">
            <td colspan="4" class="empty">{{tr('尚未配置统一模型，可从下方原始目录选择来源。')}}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <details class="unified-help">
      <summary>{{tr('迁移报告与冲突定位')}}</summary>
        <p class="hint">{{tr('旧分组映射已迁移为全局定义与固定来源授权。升级前请用 -check-state-migration 生成检查报告；此处只在本机读取报告，不上传状态或凭据。')}}</p><p class="hint">{{tr('离线报告仅反映生成时的配置，不代表当前实例状态。')}}</p>
      <label>{{tr('读取迁移报告')}}<input id="migrationReport" type="file" accept="application/json,.json" @change="readReport">
      </label>
      <p v-if="reportError" class="err-text" role="alert">{{reportError}}</p>
      <div v-if="report" id="migrationReportResult">
        <p>{{report.from_schema}} → {{report.to_schema}} · {{report.ready?tr('检查通过'):tr('存在冲突')}} · {{report.definitions}} {{tr('个统一模型')}}</p>
        <div v-for="(issue,index) in [...report.conflicts,...report.warnings]" :key="index" class="unified-issue">
          <strong>{{issue.model_name}} · {{issue.reason}}</strong>
          <div>{{issue.field_path}} · {{issue.group_ids.join(', ')}}</div>
          <p>{{issue.message}}</p>
        </div>
      </div>
    </details>
  </div>
  <dialog ref="dialog" id="unifiedModelDialog" class="modal unified-modal" aria-labelledby="unifiedModelTitle" @cancel.prevent="close">
    <div class="modal-head">
      <h2 id="unifiedModelTitle">{{draft?tr('编辑统一模型'):tr('创建统一模型')}}</h2>
      <button class="btn" :disabled="busy||!!testing" @click="close">{{tr('关闭')}}</button>
    </div>
    <form @submit.prevent="save">
      <fieldset :disabled="busy||!!testing" class="unified-fieldset">
        <div class="modal-body">
          <div class="field">
            <label for="unifiedModelName">{{tr('公开名称')}}</label>
            <input id="unifiedModelName" v-model="name" :readonly="!!draft" maxlength="200" required placeholder="deepseek-4.1-flash">
            <p class="hint">{{tr('名称须带版本号，创建后不可更名；只绑定已确认属于同一型号的来源。')}}</p>
          </div>
          <label class="check">
            <input id="unifiedModelEnabled" v-model="enabled" type="checkbox">{{tr('启用统一模型')}}</label>
            <div class="field">
              <label for="unifiedSourceSelect">{{tr('添加原始来源')}}</label>
              <select id="unifiedSourceSelect" value="" @change="addSource(($event.target as HTMLSelectElement).value);($event.target as HTMLSelectElement).value='' ">
                <option value="">{{tr('从当前真实目录选择')}}</option>
                <option v-for="[id,m] in available" :key="id" :value="id" :disabled="sources.some(s=>s.id===id)">{{id}} · {{m.name}}</option>
              </select>
              <p v-if="!available.size" class="hint">{{tr('原始目录为空，请先添加账号并刷新模型清单。')}}</p>
            </div>
            <div id="unifiedSourceRows">
              <div v-for="(source,index) in sources" :key="source.id" class="unified-source">
                <strong class="mono">{{source.id}}</strong>
                <span :class="['badge',available.has(source.id)?'ok':'warn']">{{available.has(source.id)?tr('目录存在'):tr('目录缺失，配置保留')}}</span>
                <div class="actions">
                  <label class="check">
                    <input v-model="source.enabled" type="checkbox" :aria-label="source.id+' '+tr('启用来源')">{{tr('启用来源')}}</label>
                    <button type="button" class="btn xs" :disabled="!available.has(source.id)" @click="probe(source.id)">{{testing===source.id?tr('测活中…'):tr('测活')}}</button>
                    <button type="button" class="btn xs danger" @click="sources.splice(index,1)">{{tr('移除')}}</button>
                  </div>
                  <p v-if="probeResults[source.id]" :class="probeResults[source.id].ok?'hint':'err-text'">{{probeResults[source.id].message}} · {{probeResults[source.id].time}}</p>
                  <p v-else-if="detail?.source_status.find(s=>s.id===source.id)" class="hint">{{tr('可调度账号快照')}}: {{detail.source_status.find(s=>s.id===source.id)?.eligible_account_count}} · {{observed(detail.source_status.find(s=>s.id===source.id)?.observed_at)}}</p>
                </div>
              </div>
              <p class="hint">{{tr('一次文本测活不代表已验证图片或工具能力；目录缺失项不会被刷新自动删除。')}}</p>
              <details>
                <summary>{{tr('公共客户端策略')}}</summary>
                <label class="check">
                  <input id="unifiedPolicyEnabled" v-model="policyEnabled" type="checkbox">{{tr('设置公共建议；关闭后按来源合并')}}</label>
                  <div v-if="policyEnabled" class="field-grid cols-2">
                    <div v-for="[key,label] in fields" :key="key" class="field">
                      <label :for="'unifiedPolicy-'+key">{{tr(label)}}</label>
                      <input :id="'unifiedPolicy-'+key" v-model.number="policy[key]" type="number" min="1" step="1" required>
                    </div>
                  </div>
                  <p class="hint">{{tr('公共建议不能超过任何启用来源的已验证边界；来源缺少资料时请先编辑原始策略。')}}</p>
                </details>
                <div v-if="detail?.references.length" class="unified-help">
                  <h3>{{tr('引用此模型的配置')}}</h3>
                  <p v-for="(reference,index) in detail.references" :key="index">{{tr(referenceLabels[reference.kind])}} · {{reference.name}} <small class="muted">{{reference.group_id}}</small></p>
                  <p class="hint">{{tr('引用仍存在时不能删除模型或被固定授权引用的来源；可先禁用来源。')}}</p>
                </div>
                <p id="unifiedModelError" class="err-text" role="alert">{{error}}</p>
              </div>
              <div class="modal-actions">
                <button type="button" class="btn" @click="close">{{tr('取消')}}</button>
                <button v-if="draft" type="button" class="btn" @click="reread">{{tr('重新读取')}}</button>
                <button id="unifiedModelSave" class="btn primary" type="submit">{{busy?tr('保存中…'):tr('保存统一模型')}}</button>
              </div>
            </fieldset>
          </form>
        </dialog>
      </template>
