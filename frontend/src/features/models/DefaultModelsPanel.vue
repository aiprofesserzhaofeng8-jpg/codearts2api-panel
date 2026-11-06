<script setup lang="ts">
import { computed, ref } from 'vue';
import { api } from '../../api/client';
import type { components } from '../../api/generated/admin';
import { t as tr } from '../../i18n';
import { serverState } from '../../shared/read-model';
import { refresh } from '../../shared/refresh';
import { editError, reloadUnifiedModels } from '../../shared/unified-models';
const emit = defineEmits<{ applied: [] }>();
const dialog = ref<HTMLDialogElement>();
const preview = ref<components['schemas']['DefaultModelsPreview']>();
const chosen = ref<string[]>([]), groupID = ref(''), search = ref(''), platform = ref('');
const busy = ref(false), error = ref(''), result = ref('');
const groups = ref<Array<{ id: string; name: string; revision: number }>>([]);
const rows = computed(() => (preview.value?.models ?? []).filter(row =>
    (!search.value || row.name.toLowerCase().includes(search.value.toLowerCase()) || row.sources.some(s => s.id.toLowerCase().includes(search.value.toLowerCase()))) &&
    (!platform.value || row.sources.some(s => s.id.startsWith(platform.value + '/')))));
const labels = { create: '新建', update: '追加来源', unchanged: '已配置', conflict: '需要处理' } as const;
async function open() {
    if (busy.value) return;
    busy.value = true; error.value = ''; result.value = '';
    try {
        await refresh();
        preview.value = await api<components['schemas']['DefaultModelsPreview']>('/admin/api/model-defaults');
        groups.value = (serverState.data?.groups ?? []).map(g => ({ id: g.id!, name: g.name!, revision: g.revision! }));
        chosen.value = preview.value.models.filter(m => m.status !== 'conflict').map(m => m.name);
        groupID.value = ''; search.value = ''; platform.value = '';
        dialog.value?.showModal();
    } catch (e) { error.value = editError(e); }
    finally { busy.value = false; }
}
function selectAll() { chosen.value = [...new Set([...chosen.value, ...rows.value.filter(r => r.status !== 'conflict').map(r => r.name)])]; }
async function apply() {
    if (!preview.value || busy.value || !chosen.value.length) return;
    busy.value = true; error.value = '';
    try {
        const response = await api<{ok:boolean; routing_revision:number}>('/admin/api/model-defaults', { method: 'POST', body: {
            version: preview.value.version, catalog_revision: preview.value.catalog_revision,
            expected_routing_revision: preview.value.routing_revision, names: chosen.value,
            group_id: groupID.value, expected_group_revision: groups.value.find(g => g.id === groupID.value)?.revision ?? 0
        }});
        if (response.ok !== true || !response.routing_revision) throw new Error(tr('保存结果未确认，请重新读取核对。'));
        dialog.value?.close();
        result.value = tr('默认模型已应用；客户端使用公开名称调用。');
        await reloadUnifiedModels(); await refresh(); emit('applied');
    } catch (e) { error.value = editError(e); }
    finally { busy.value = false; }
}
</script>
<template>
  <div class="card unified-panel">
    <div class="card-head">
      <div><h2>{{tr('默认模型目录')}}</h2><p class="card-sub">{{tr('汇总所有已接入平台，一次配置用户可见的模型名称。')}}</p></div>
      <button id="openDefaultModels" class="btn primary" :disabled="busy" @click="open">{{busy?tr('读取中…'):tr('归并同名模型')}}</button>
    </div>
    <p class="unified-help hint">{{tr('按完整型号归并各平台来源，用户只看到一个模型名称。不同版本保留区别；auto 等平台专用入口单独展示。')}}</p>
    <p class="unified-help hint">{{tr('统一模型能力受已核实的原生规格约束；平台额外提供的能力请使用完整来源名称。未核实的型号不推断原生能力。')}}</p>
    <p v-if="result" role="status" class="unified-help">{{result}}</p>
    <p v-if="error && !dialog?.open" class="err-text" role="alert">{{error}}</p>
  </div>
  <dialog ref="dialog" id="defaultModelsDialog" class="modal unified-modal" aria-labelledby="defaultModelsTitle" @cancel="busy && $event.preventDefault()">
    <div class="card-head default-models-head"><h2 id="defaultModelsTitle">{{tr('应用默认模型目录')}} · v{{preview?.version}}</h2><button class="btn" :disabled="busy" @click="dialog?.close()">{{tr('关闭')}}</button></div>
    <div class="modal-body">
      <p class="hint">{{tr('仅添加当前目录中存在的来源。保留已有名称、停用状态和手动配置；不会自动发起模型请求。')}}</p>
      <div class="field"><label for="defaultModelsGroup">{{tr('同时授权到分组')}}</label>
        <select id="defaultModelsGroup" v-model="groupID" :disabled="busy"><option value="">{{tr('仅配置全局目录（不分组密钥可用）')}}</option><option v-for="group in groups" :key="group.id" :value="group.id">{{group.name}}</option></select>
        <p class="hint">{{tr('只为尚未授权的模型添加分组允许的来源；已有授权与限制保持，固定来源需在分组页调整。')}}</p>
      </div>
      <div class="row"><input v-model="search" type="search" :placeholder="tr('搜索模型或来源')" :aria-label="tr('搜索默认模型')"><select v-model="platform" :aria-label="tr('按平台筛选')"><option value="">{{tr('全部平台')}}</option><option v-for="p in preview?.providers" :key="p" :value="p">{{p}}</option></select></div>
      <div class="row"><button class="btn" :disabled="busy" @click="selectAll">{{tr('全选当前列表')}}</button><button class="btn" :disabled="busy" @click="chosen=[]">{{tr('清空选择')}}</button><span>{{tr('已选择')}} {{chosen.length}}</span></div>
      <div class="table-wrap default-models-table"><table><thead><tr><th>{{tr('选择')}}</th><th>{{tr('用户调用名称')}}</th><th>{{tr('平台来源')}}</th><th>{{tr('操作')}}</th></tr></thead><tbody>
        <tr v-for="row in rows" :key="row.name"><td><input v-model="chosen" type="checkbox" :value="row.name" :disabled="busy || row.status==='conflict'" :aria-label="tr('选择')+' '+row.name"></td><td class="mono">{{row.name}}</td><td><div v-for="source in row.sources" :key="source.id" class="mono">{{source.id}} <span v-if="!source.enabled">{{tr('停用')}}</span></div></td><td>{{tr(labels[row.status])}}<p v-if="row.message" class="err-text">{{row.message}}</p></td></tr>
        <tr v-if="!rows.length"><td colspan="4">{{tr('暂无匹配型号，请先添加账号并刷新模型清单。')}}</td></tr>
      </tbody></table></div>
      <p id="defaultModelsError" role="alert" class="err-text">{{error}}</p>
    </div>
    <div class="modal-actions"><button class="btn" :disabled="busy" @click="dialog?.close()">{{tr('取消')}}</button><button id="applyDefaultModels" class="btn primary" :disabled="busy || !chosen.length" @click="apply">{{busy?tr('保存中…'):tr('应用所选模型')}}</button></div>
  </dialog>
</template>
