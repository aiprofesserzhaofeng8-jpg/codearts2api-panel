<script setup lang="ts">
import { computed, ref } from 'vue';
import { t as tr } from '../../i18n';
import type { Grant, UnifiedModel } from '../../shared/unified-models';

const props = defineProps<{ models: UnifiedModel[] }>();
const grants = defineModel<Grant[]>({ required: true });
const search = ref('');
const selectedOnly = ref(false);
const expanded = ref('');
const byModel = computed(() => new Map(grants.value.map(grant => [grant.model_id, grant])));
const rows = computed(() => {
  const needle = search.value.trim().toLowerCase();
  return props.models
    .filter(model => model.name.toLowerCase().includes(needle) && (!selectedOnly.value || byModel.value.has(model.id)))
    .map(model => ({ model, grant: byModel.value.get(model.id) }));
});

const selectedInView = computed(() => rows.value.filter(row => row.grant).length);
const allSelected = computed(() => rows.value.length > 0 && selectedInView.value === rows.value.length);
// Matches validateUnifiedGroup in backend/internal/storage/unified_models.go.
const maxGroupModels = 128;
const exceedsLimit = computed(() => grants.value.length + rows.value.length - selectedInView.value > maxGroupModels);
const bulkLabel = computed(() => allSelected.value ? tr('取消全选当前列表') : tr('全选当前列表'));

function setSelection(models: UnifiedModel[], checked: boolean) {
  if (checked) {
    const added = models.filter(model => !byModel.value.has(model.id)).map((model): Grant => ({
      model_id: model.id, visible: true, callable: true,
      source_mode: 'selected', source_ids: model.sources.map(source => source.id),
    }));
    if (grants.value.length + added.length <= maxGroupModels) grants.value = [...grants.value, ...added];
  } else {
    const removed = new Set(models.map(model => model.id));
    grants.value = grants.value.filter(grant => !removed.has(grant.model_id));
    if (removed.has(expanded.value)) expanded.value = '';
  }
}
</script>

<template>
  <section id="groupUnifiedGrants" class="picker" aria-labelledby="grantPickerTitle">
    <header>
      <h3 id="grantPickerTitle">{{ tr('统一模型授权') }}</h3>
      <span>{{ tr('已选择') }} {{ grants.length }} / {{ models.length }}</span>
    </header>
    <div class="filters">
      <input v-model="search" type="search" :placeholder="tr('搜索模型名称…')" :aria-label="tr('搜索模型名称…')" @keydown.enter.prevent>
      <div class="segments">
        <button type="button" :aria-pressed="!selectedOnly" @click="selectedOnly = false">{{ tr('全部') }}</button>
        <button type="button" :aria-pressed="selectedOnly" @click="selectedOnly = true">{{ tr('仅看已选') }}</button>
      </div>
    </div>
    <p v-if="exceedsLimit" class="hint" role="status">{{ tr('每组最多可选 128 个模型，请缩小搜索范围或减少已选模型。') }}</p>
    <div class="grant-scroll">
      <table class="grants" aria-labelledby="grantPickerTitle">
        <thead><tr>
          <th scope="col">
            <label class="grant-name" :title="bulkLabel">
              <input type="checkbox" :checked="allSelected" :indeterminate="selectedInView > 0 && !allSelected"
                :disabled="!rows.length || exceedsLimit" :aria-label="bulkLabel"
                @change="setSelection(rows.map(row => row.model), ($event.target as HTMLInputElement).checked)">
              <span>{{ tr('模型') }}</span>
            </label>
          </th><th scope="col">{{ tr('显示') }}</th>
          <th scope="col">{{ tr('允许调用') }}</th><th scope="col">{{ tr('来源限制') }}</th>
        </tr></thead>
        <tbody>
          <template v-for="{ model, grant } in rows" :key="model.id">
            <tr>
              <th scope="row"><label class="grant-name">
                <input type="checkbox" :checked="!!grant" :aria-label="tr('授权')+' '+model.name" :disabled="!grant && grants.length >= maxGroupModels" @change="setSelection([model], ($event.target as HTMLInputElement).checked)">
                <span>{{ model.name }} <small v-if="!model.enabled" class="badge warn">{{ tr('停用') }}</small></span>
              </label></th>
              <td><input v-if="grant" v-model="grant.visible" type="checkbox" :aria-label="model.name+' '+tr('显示')"><span v-else>—</span></td>
              <td><input v-if="grant" v-model="grant.callable" type="checkbox" :aria-label="model.name+' '+tr('允许调用')"><span v-else>—</span></td>
              <td><button v-if="grant" type="button" class="source-button" :aria-expanded="expanded===model.id"
                :aria-label="model.name+' '+tr('来源限制')" @click="expanded = expanded === model.id ? '' : model.id">
                {{ grant.source_mode === 'all' ? tr('全部来源') : tr('指定来源')+' '+(grant.source_ids?.length ?? 0) }}
              </button><span v-else>—</span></td>
            </tr>
            <tr v-if="grant && expanded===model.id" class="source-editor"><td colspan="4">
              <label class="field">{{ tr('授权来源模式') }}
                <select v-model="grant.source_mode" :aria-label="model.name+' '+tr('授权来源模式')">
                  <option value="selected">{{ tr('固定选中的来源') }}</option>
                  <option value="all">{{ tr('跟随定义的全部来源（包含以后新增）') }}</option>
                </select>
              </label>
              <template v-if="grant.source_mode==='selected'">
                <label v-for="source in model.sources" :key="source.id" class="grant-name">
                  <input type="checkbox" :checked="grant.source_ids?.includes(source.id)"
                    @change="grant.source_ids = ($event.target as HTMLInputElement).checked ? [...(grant.source_ids ?? []), source.id] : grant.source_ids.filter(id => id !== source.id)">
                  <span>{{ source.id }} <small v-if="!source.enabled" class="badge warn">{{ tr('停用') }}</small></span>
                </label>
                <p v-if="!grant.source_ids?.length" class="hint">{{ tr('未选择来源：禁止调用，不代表全部来源。') }}</p>
              </template>
            </td></tr>
          </template>
          <tr v-if="!rows.length"><td class="empty" colspan="4">{{ !models.length ? tr('尚未配置统一模型，请先到模型页创建。') : selectedOnly && !grants.length ? tr('尚未选择模型') : tr('没有匹配的模型') }}</td></tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<style scoped>
.picker {margin:20px 0 16px;min-width:0}
.picker header,.filters,.segments,.grant-name {display:flex;align-items:center}
.picker header,.filters {justify-content:space-between;gap:12px;margin-bottom:10px}
.picker h3 {margin:0}
.picker header span {color:var(--text-2);font-variant-numeric:tabular-nums}
.filters input {min-width:0;flex:1}
.segments {border:1px solid var(--line-2);border-radius:7px;padding:2px;background:var(--surface-2)}
.segments button,.source-button {font:inherit;cursor:pointer;color:var(--text-2);border:0;background:transparent}
.segments button {border-radius:4px;padding:5px 10px;white-space:nowrap}
.segments button[aria-pressed=true] {background:var(--accent-soft);color:var(--text);font-weight:600}
.grant-scroll {max-height:min(42vh,360px);overflow:auto;scrollbar-gutter:stable;border:1px solid var(--line);border-radius:8px}
.grants {width:100%;min-width:0;table-layout:fixed;border-spacing:0}
.grants th,.grants td {padding:6px 8px;vertical-align:middle;text-align:center}
.grants thead th {position:sticky;top:0;z-index:1;background:var(--surface-2);color:var(--text-2)}
.grants th:first-child {text-align:left}
.grants th:nth-child(2),.grants th:nth-child(3) {width:13%}
.grants th:nth-child(4) {width:23%}
.grants tbody td>span {color:var(--text-3)}
.grant-name {gap:8px;text-align:left;cursor:pointer;overflow-wrap:anywhere;color:var(--text);font-weight:500;min-height:26px}
.picker input[type=checkbox] {appearance:auto;width:16px;height:16px;margin:0;accent-color:var(--accent);vertical-align:middle}
.picker input[type=checkbox]::after {content:none}
.source-button {color:var(--text);padding:3px}
.source-button::after {content:'›';display:inline-block;margin-inline-start:5px}
.source-button[aria-expanded=true]::after {transform:rotate(90deg)}
.source-editor td {background:var(--surface-2);text-align:left;padding:10px 14px}
.source-editor .field {margin-bottom:8px}
@media(max-width:600px) {
  .grants th,.grants td {padding:6px 3px;font-size:12px}
  .grants thead th {white-space:normal}
  .grant-name {gap:4px}
  .grants th:nth-child(2),.grants th:nth-child(3) {width:14%}
  .grants th:nth-child(4) {width:25%}
  .filters {gap:6px}
  .segments button {padding:5px 7px}
}
</style>
