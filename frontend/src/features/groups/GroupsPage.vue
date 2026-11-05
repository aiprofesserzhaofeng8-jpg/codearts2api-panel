<script setup lang="ts">
import { computed, ref } from 'vue';
import { api, isAbortError } from '../../api/client';
import { serverState } from '../../shared/read-model';
import { refresh } from '../../shared/refresh';
import { t as tr } from '../../i18n';
import { canonicalSource, rawSource, editError, reloadUnifiedModels, unifiedModels, type Group, type Grant } from '../../shared/unified-models';
import { groupModelAllowed } from './controller';
const dialog = ref<HTMLDialogElement>(), draft = ref<Group>(), busy = ref(false), opening = ref(false), error = ref(''), listError = ref('');
const groups = computed(() => serverState.data?.groups ?? []), accounts = computed(() => serverState.data?.accounts ?? []);
const rawModels = computed(() => (serverState.data?.models ?? []).filter(m => m.source !== 'alias'));
const rawChoices = computed(() => {
    const group = draft.value;
    if (!group)
        return [];
    const virtual = [{ id: 'codearts/web-search', provider: 'codearts', scope: '' }, { id: 'trae/cn/web-search', provider: 'trae', scope: 'cn' }, { id: 'workbuddy/cn/web-search', provider: 'workbuddy', scope: 'cn' }, { id: 'workbuddy/global/web-search', provider: 'workbuddy', scope: 'global' }];
    return [...new Set([...rawModels.value, ...virtual].filter(m => groupModelAllowed(m, { account_ids: group.account_ids, allowlist_enabled: false }, serverState.data || {})).map(m => m.id).concat(group.model_ids || []))];
});
const rules = computed(() => [...new Set([...rawModels.value.map(m => m.id), ...(draft.value?.model_rules ?? []).map(r => r.id)])]);
let epoch = 0;
function close() { if (!busy.value) {
    epoch++;
    dialog.value?.close();
} }
async function open(group?: Group) {
    if (opening.value || busy.value)
        return;
    opening.value = true;
    listError.value = '';
    const current = ++epoch;
    try {
        await reloadUnifiedModels();
        const fresh = group ? (await api('/admin/api/groups')).groups?.find(g => g.id === group.id) : undefined;
        if (group && !fresh)
            throw new Error(tr('分组已删除，请刷新列表'));
        if (current !== epoch)
            return;
        draft.value = fresh ? JSON.parse(JSON.stringify(fresh)) : { id: '', name: '', revision: 0, account_ids: [], allowlist_enabled: false, model_ids: [], unified_models: [], raw_model_mode: 'qualified_only', raw_models_visible_default: false, model_rules: [], default_model: '', client_default_model: '' };
        error.value = '';
        if (!dialog.value?.open)
            dialog.value?.showModal();
    }
    catch (e) {
        if (!isAbortError(e))
            listError.value = editError(e);
    }
    finally {
        opening.value = false;
    }
}
function grant(id: string): Grant | undefined { return draft.value?.unified_models.find(g => g.model_id === id); }
function selectModel(id: string, checked: boolean) {
    const group = draft.value, model = unifiedModels.value.find(m => m.id === id);
    if (!group || !model)
        return;
    if (checked && !grant(id))
        group.unified_models.push({ model_id: id, visible: true, callable: true, source_mode: 'selected', source_ids: model.sources.map(s => s.id) });
    if (!checked)
        group.unified_models = group.unified_models.filter(g => g.model_id !== id);
}
function getRule(id: string, key: 'visible' | 'callable'): string { const value = draft.value?.model_rules?.find(r => r.id === id)?.[key]; return value == null ? 'inherit' : String(value); }
function setRule(id: string, key: 'visible' | 'callable', value: string) {
    if (!draft.value)
        return;
    const rules = draft.value.model_rules ?? (draft.value.model_rules = []);
    let rule = rules.find(r => r.id === id);
    if (!rule) {
        rule = { id };
        rules.push(rule);
    }
    if (value === 'inherit')
        delete rule[key];
    else
        rule[key] = value === 'true';
}
function allowedSource(id: string, g: Grant): boolean {
    const group = draft.value;
    if (!group)
        return false;
    return (g.source_mode === 'all' || g.source_ids.includes(id)) && (!group.allowlist_enabled || !!group.model_ids?.includes(rawSource(id))) && group.model_rules?.find(r => r.id === rawSource(id))?.callable !== false;
}
function defaults(client: boolean): string[] {
    const group = draft.value;
    if (!group)
        return [];
    const names = unifiedModels.value.filter(m => m.enabled && group.unified_models.some(g => g.model_id === m.id && g.callable && (!client || g.visible) && m.sources.some(s => s.enabled && allowedSource(s.id, g)))).map(m => m.name);
    for (const m of rawModels.value) {
        const id = group.raw_model_mode === 'qualified_only' ? canonicalSource(m.id) : m.id;
        if (group.allowlist_enabled && !group.model_ids?.includes(m.id))
            continue;
        const rule = group.model_rules?.find(r => r.id === m.id);
        if (rule?.callable === false || (client && !(rule?.visible ?? group.raw_models_visible_default)))
            continue;
        names.push(id);
    }
    const chosen = client ? group.client_default_model : group.default_model;
    if (chosen && !names.includes(chosen))
        names.push(chosen);
    return [...new Set(names)];
}
async function save() {
    if (!draft.value || busy.value)
        return;
    busy.value = true;
    error.value = '';
    const g = draft.value;
    const body = { name: g.name, account_ids: g.account_ids, allowlist_enabled: g.allowlist_enabled, model_ids: g.model_ids, unified_models: g.unified_models.map(a => ({ ...a, source_ids: a.source_mode === 'all' ? [] : a.source_ids })), raw_model_mode: g.raw_model_mode, raw_models_visible_default: g.raw_models_visible_default, model_rules: g.model_rules, default_model: g.default_model || '', client_default_model: g.client_default_model || '', ...(g.id ? { id: g.id, expected_revision: g.revision } : {}) };
    try {
        const saved=await api<{ok:boolean;group:Group}>('/admin/api/groups', { method: 'POST', body });
        if(saved?.ok!==true||!saved.group?.id||!saved.group.revision)throw new Error(tr('保存结果未确认，请重新读取核对。'));
        dialog.value?.close();
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
async function remove(g: Group) { if (busy.value || !confirm(tr('确定删除分组「') + g.name + '」？'))
    return; busy.value = true; listError.value = ''; try {
    await api('/admin/api/groups?id=' + encodeURIComponent(g.id) + '&expected_revision=' + g.revision, { method: 'DELETE' });
    await refresh();
}
catch (e) {
    if (!isAbortError(e))
        listError.value = editError(e);
}
finally {
    busy.value = false;
} }
function bindingCount(g: Group) { return (serverState.data?.keys ?? []).filter(k => k.group_id === g.id).length; }
async function reread() { if (draft.value && confirm(tr('重新读取将丢弃当前草稿，是否继续？')))
    await open(draft.value); }
</script>
<template>
  <section id="tab-groups" role="tabpanel" aria-labelledby="nav-groups">
    <div class="card">
      <div class="card-head">
        <h2>{{tr('账号分组')}}</h2>
        <button id="addGroup" class="btn primary" :disabled="busy||opening" @click="open()">{{tr('创建分组')}}</button>
      </div>
      <p v-if="listError" class="err-text" role="alert">{{listError}}</p>
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>{{tr('名称')}}</th>
              <th>{{tr('账号数')}}</th>
              <th>{{tr('模型范围')}}</th>
              <th>{{tr('绑定密钥')}}</th>
              <th>{{tr('操作')}}</th>
            </tr>
          </thead>
          <tbody id="groups">
            <tr v-for="g in groups" :key="g.id">
              <td>{{g.name}}</td>
              <td>{{g.account_ids?.length??0}}</td>
              <td>{{(g.unified_models??[]).length}} {{tr('个统一模型')}}</td>
              <td>{{bindingCount(g)}}</td>
              <td class="actions-cell">
                <button class="btn xs" :disabled="busy||opening" @click="open(g)">{{tr('编辑')}}</button>
                <button class="btn xs danger" :disabled="busy||bindingCount(g)>0" @click="remove(g)">{{tr('删除')}}</button>
              </td>
            </tr>
            <tr v-if="!groups.length">
              <td colspan="5" class="empty">{{tr('尚未创建分组')}}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
  <dialog ref="dialog" id="groupDialog" class="modal unified-modal" aria-labelledby="groupTitle" @cancel.prevent="close">
    <div class="modal-head">
      <h2 id="groupTitle">{{draft?.id?tr('编辑账号分组'):tr('创建账号分组')}}</h2>
      <button id="groupClose" class="btn" :disabled="busy" @click="close">{{tr('关闭')}}</button>
    </div>
    <form v-if="draft" @submit.prevent="save">
      <fieldset :disabled="busy" class="unified-fieldset">
        <div class="modal-body">
          <div class="field">
            <label for="groupName">{{tr('分组名称')}}</label>
            <input id="groupName" v-model="draft.name" maxlength="80" required>
          </div>
          <p>{{tr('可用账号')}}</p>
          <div id="groupAccounts" class="group-account-options">
            <label v-for="account in accounts" :key="account.id" class="check">
              <input v-model="draft.account_ids" type="checkbox" :value="account.id">{{account.name}} · {{account.provider||'codearts'}}</label>
              <p v-if="!accounts.length" class="hint">{{tr('请先添加账号')}}</p>
            </div>
            <h3 class="mt-14">{{tr('统一模型授权')}}</h3>
            <p class="hint">{{tr('只选择已有统一模型；名称与平台对应关系在模型页维护。')}}</p>
            <div id="groupUnifiedGrants">
              <div v-for="model in unifiedModels" :key="model.id" class="unified-source">
                <label class="check">
                  <input type="checkbox" :checked="!!grant(model.id)" :aria-label="tr('授权')+' '+model.name" @change="selectModel(model.id,($event.target as HTMLInputElement).checked)">
                  <strong>{{model.name}}</strong>
                  <span v-if="!model.enabled" class="badge warn">{{tr('停用')}}</span>
                </label>
                <template v-for="g in draft.unified_models.filter(a=>a.model_id===model.id)" :key="g.model_id">
                  <div class="actions">
                    <label class="check">
                      <input v-model="g.visible" type="checkbox" :aria-label="model.name+' '+tr('显示')">{{tr('显示')}}</label>
                      <label class="check">
                        <input v-model="g.callable" type="checkbox" :aria-label="model.name+' '+tr('允许调用')">{{tr('允许调用')}}</label>
                      </div>
                      <details>
                        <summary>{{tr('来源限制')}}</summary>
                        <div class="field">
                          <label>{{tr('授权来源模式')}}<select v-model="g.source_mode" :aria-label="model.name+' '+tr('授权来源模式')">
                            <option value="selected">{{tr('固定选中的来源')}}</option>
                            <option value="all">{{tr('跟随定义的全部来源（包含以后新增）')}}</option>
                          </select>
                        </label>
                      </div>
                      <div v-if="g.source_mode==='selected'">
                        <label v-for="source in model.sources" :key="source.id" class="check">
                          <input v-model="g.source_ids" type="checkbox" :value="source.id">{{source.id}}<span v-if="!source.enabled" class="badge warn">{{tr('停用')}}</span>
                        </label>
                        <p v-if="!g.source_ids.length" class="hint">{{tr('未选择来源：禁止调用，不代表全部来源。')}}</p>
                      </div>
                    </details>
                  </template>
                </div>
                <p v-if="!unifiedModels.length" class="hint">{{tr('尚未配置统一模型，请先到模型页创建。')}}</p>
              </div>
              <div class="field-grid cols-2 mt-14">
                <div class="field">
                  <label for="groupDefaultModel">{{tr('请求默认模型')}}</label>
                  <select id="groupDefaultModel" v-model="draft.default_model">
                    <option value="">{{tr('沿用全局默认')}}</option>
                    <option v-for="name in defaults(false)" :key="name" :value="name">{{name}}</option>
                  </select>
                  <p class="hint">{{tr('仅请求未填写 model 时使用；留空沿用全局默认。')}}</p>
                </div>
                <div class="field">
                  <label for="groupClientDefaultModel">{{tr('客户端推荐模型')}}</label>
                  <select id="groupClientDefaultModel" v-model="draft.client_default_model">
                    <option value="">{{tr('不指定')}}</option>
                    <option v-for="name in defaults(true)" :key="name" :value="name">{{name}}</option>
                  </select>
                  <p class="hint">{{tr('供支持的客户端预选，不改变请求路由。')}}</p>
                </div>
              </div>
              <details id="groupRawAccess">
                <summary>{{tr('原始型号访问权限')}}</summary>
                <p class="hint">{{tr('原始白名单和调用规则也约束统一模型来源；原始显示不改变统一模型可见性。')}}</p>
                <div class="field">
                  <label for="groupRawMode">{{tr('原始名称兼容模式')}}</label>
                  <select id="groupRawMode" v-model="draft.raw_model_mode">
                    <option value="qualified_only">{{tr('只接受完整平台型号')}}</option>
                    <option value="legacy">{{tr('兼容旧裸名称与别名')}}</option>
                  </select>
                </div>
                <label class="check">
                  <input id="groupRawVisible" v-model="draft.raw_models_visible_default" type="checkbox">{{tr('默认显示原始型号')}}</label>
                  <label class="check">
                    <input id="groupAllowlist" v-model="draft.allowlist_enabled" type="checkbox">{{tr('仅允许选中的模型')}}</label>
                    <div v-show="draft.allowlist_enabled" id="groupModelsField" class="field">
                      <label for="groupModels">{{tr('模型白名单（可多选）')}}</label>
                      <select id="groupModels" v-model="draft.model_ids" :disabled="!draft.allowlist_enabled" multiple size="7">
                        <option v-for="id in rawChoices" :key="id" :value="id">{{id}}</option>
                      </select>
                    </div>
                    <div class="table-wrap">
                      <table>
                        <thead>
                          <tr>
                            <th>{{tr('模型')}}</th>
                            <th>{{tr('显示')}}</th>
                            <th>{{tr('允许调用')}}</th>
                          </tr>
                        </thead>
                        <tbody id="groupModelRules">
                          <tr v-for="id in rules" :key="id">
                            <td class="mono">{{id}}</td>
                            <td v-for="key in (['visible','callable'] as const)" :key="key">
                              <select :aria-label="id+' '+tr(key==='visible'?'显示':'允许调用')" :value="getRule(id,key)" @change="setRule(id,key,($event.target as HTMLSelectElement).value)">
                                <option value="inherit">{{tr('继承默认')}}</option>
                                <option value="true">{{tr('允许')}}</option>
                                <option value="false">{{tr('禁止')}}</option>
                              </select>
                            </td>
                          </tr>
                        </tbody>
                      </table>
                    </div>
                  </details>
                  <p id="groupError" class="err-text" role="alert">{{error}}</p>
                </div>
                <div class="modal-actions">
                  <button type="button" id="groupCancel" class="btn" @click="close">{{tr('取消')}}</button>
                  <button v-if="draft.id" type="button" class="btn" @click="reread">{{tr('重新读取')}}</button>
                  <button id="groupSave" type="submit" class="btn primary">{{busy?tr('保存中…'):tr('保存分组')}}</button>
                </div>
              </fieldset>
            </form>
          </dialog>
        </template>
