import { t as tr } from '../../i18n';
import { btn, cell, emptyRow, mk, on, row } from '../../components/controls';
import { serverState } from '../../shared/read-model';
import { isAbortError } from '../../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { domState, toast } from '../../shared/dom';
import { api } from '../../api/client';
import { listen } from '../../shared/lifecycle';
import { refresh } from '../../shared/refresh';
const groupsState = {};
groupsState.editingGroup = "";

groupsState.groupSaving = false;
groupsState.rules = {};
groupsState.presets = null;





export function groupModelAllowed(model, group, data) {
    var aliases = (data.settings || {}).model_aliases || {};
    var resolved = model.source === "alias" ? ((data.models || []).find(function (m) { return m.id === aliases[model.id]; }) || model) : model;
    if (group.allowlist_enabled && !(group.model_ids || []).includes(resolved.id))
        return false;
    return (data.accounts || []).some(function (a) { return (group.account_ids || []).includes(a.id) && a.enabled && (a.provider || "codearts") === (resolved.provider || "codearts") && ((resolved.provider || "codearts") === "codearts" || (a.scope || "") === (resolved.scope || "")); });
}

export function fillGroupSelect(select, value, first) {
    select.textContent = "";
    var option = document.createElement("option");
    option.value = "";
    option.textContent = first;
    select.appendChild(option);
    var groups = (serverState.data || {}).groups || [];
    groups.forEach(function (g) { var o = document.createElement("option"); o.value = g.id; o.textContent = g.name; select.appendChild(o); });
    if (value && !groups.some(function (g) { return g.id === value; })) {
        var missing = document.createElement("option");
        missing.value = value;
        missing.textContent = tr("分组不存在，请重新选择");
        missing.disabled = true;
        select.appendChild(missing);
    }
    select.value = value || "";
}

export function keyGroupCell(key) {
    var result = cell(key.name), group = ((serverState.data || {}).groups || []).find(function (g) { return g.id === key.group_id; });
    result.appendChild(mk("div", "sub", key.group_id ? (group ? group.name : tr("分组不存在")) : tr("不分组")));
    return result;
}

export function renderGroups(d) {
    fillGroupSelect(domState.$("#keyCreateGroup"), domState.$("#keyCreateGroup").value, tr("不分组（全部账号）"));
    fillGroupSelect(domState.$("#modelsGroupFilter"), domState.$("#modelsGroupFilter").value, tr("全部分组"));
    var rows = domState.$("#groups");
    rows.textContent = "";
    (d.groups || []).forEach(function (g) {
        var bindings = (d.keys || []).filter(function (k) { return k.group_id === g.id; }).length, ops = cell("", "actions-cell");
        ops.appendChild(btn(tr("编辑"), "xs", function () { openGroup(g); }));
        var remove = btn(tr("删除"), "xs danger", function () { if (!confirm(tr("确定删除分组「") + g.name + "」？"))
            return; return api("/admin/api/groups?id=" + encodeURIComponent(g.id), { method: "DELETE" }).then(refresh).catch(function (e) { if (isAbortError(e)) return; toast(tr("删除失败：") + e.message, "err"); }); });
        remove.disabled = bindings > 0;
        if (bindings)
            remove.title = tr("请先修改绑定密钥的分组");
        ops.appendChild(remove);
        rows.appendChild(row([cell(g.name), cell((g.account_ids || []).length), cell((g.model_mappings || []).length ? (g.model_mappings.length + tr(" 个统一模型")) : (g.allowlist_enabled ? (g.model_ids || []).length + tr(" 个模型") : tr("组内平台全部模型"))), cell(bindings), ops]));
    });
    if (!rows.children.length)
        rows.appendChild(emptyRow(5, tr("尚未创建分组")));
}

export function updateGroupModels(selected) {
    var data = serverState.data || {}, ids = Array.from(document.querySelectorAll('#groupAccounts input:checked')).map(function (i) { return i.value; });
    var select = domState.$("#groupModels"), chosen = selected || Array.from(select.selectedOptions).map(function (o) { return o.value; });
    select.textContent = "";
    var group = { account_ids: ids, allowlist_enabled: false }, seen = {};
    (data.models || []).filter(function (m) { return m.source !== "alias" && groupModelAllowed(m, group, data); }).forEach(function (m) { var o = document.createElement("option"); o.value = m.id; o.textContent = m.id; o.selected = chosen.includes(m.id); seen[m.id] = true; select.appendChild(o); });
    [{ id: "codearts/web-search", provider: "codearts", scope: "", label: tr("CodeArts 联网搜索 · SearXNG") }, { id: "trae/cn/web-search", provider: "trae", scope: "cn", label: tr("Trae 联网搜索 · SearXNG") }, { id: "workbuddy/cn/web-search", provider: "workbuddy", scope: "cn", label: tr("WorkBuddy 联网搜索 · 国内") }, { id: "workbuddy/global/web-search", provider: "workbuddy", scope: "global", label: tr("WorkBuddy 联网搜索 · 国际") }].forEach(function (capability) { if (groupModelAllowed(capability, group, data)) {
        var option = document.createElement("option");
        option.value = capability.id;
        option.textContent = capability.label;
        option.selected = chosen.includes(capability.id);
        seen[capability.id] = true;
        select.appendChild(option);
    } });
    chosen.filter(function (id) { return !seen[id]; }).forEach(function (id) { var o = document.createElement("option"); o.value = id; o.textContent = id + tr("（当前账号范围外或目录缺失）"); o.selected = true; select.appendChild(o); });
    select.disabled = !domState.$("#groupAllowlist").checked;
    domState.$("#groupModelsField").classList.toggle("hidden", select.disabled);
    updateGroupMappingOptions();
    updateGroupRules();
}

export function groupMappingValues() {
    return Array.from(domState.$("#groupMappingRows").children).map(function (row) { return { name: row.querySelector("input").value.trim(), sources: Array.from(row.querySelector("select").selectedOptions).map(function (o) { return o.value; }) }; });
}

export function updateGroupMappingOptions() {
    var data = serverState.data || {}, group = { account_ids: Array.from(document.querySelectorAll('#groupAccounts input:checked')).map(function (i) { return i.value; }), allowlist_enabled: domState.$("#groupAllowlist").checked, model_ids: Array.from(domState.$("#groupModels").selectedOptions).map(function (o) { return o.value; }) };
    domState.$("#groupMappingRows").querySelectorAll("select").forEach(function (select) {
        var chosen = Array.from(select.selectedOptions).map(function (o) { return o.value; }), seen = {};
        select.textContent = "";
        (data.models || []).filter(function (m) { return m.source !== "alias" && groupModelAllowed(m, group, data); }).forEach(function (m) { var o = document.createElement("option"); o.value = m.id; o.textContent = (m.provider || "codearts") === "codearts" ? "codearts/" + m.id : m.id; o.selected = chosen.includes(m.id); seen[m.id] = true; select.appendChild(o); });
        chosen.filter(function (id) { return !seen[id]; }).forEach(function (id) { var o = document.createElement("option"); o.value = id; o.textContent = id + tr("（当前范围外，请检查账号及白名单）"); o.selected = true; select.appendChild(o); });
    });
}

export function addGroupMapping(mapping) {
    mapping = mapping || {};
    var holder = mk("div", "group-mapping-row"), head = mk("div", "group-mapping-name"), field = mk("div", "field"), label = mk("label", "", tr("公开模型名")), input = document.createElement("input");
    input.placeholder = "deepseek-4.1-flash";
    input.maxLength = 200;
    input.addEventListener("input", updateGroupRules);
    input.value = mapping.name || "";
    input.setAttribute("aria-label", tr("公开模型名"));
    field.appendChild(label);
    field.appendChild(input);
    head.appendChild(field);
    head.appendChild(btn(tr("移除"), "danger", function () { holder.remove(); updateGroupRules(); }));
    holder.appendChild(head);
    var sources = mk("div", "field"), select = document.createElement("select");
    select.multiple = true;
    select.size = 6;
    select.setAttribute("aria-label", tr("映射来源型号"));
    sources.appendChild(mk("label", "", tr("来源型号（可多选）")));
    sources.appendChild(select);
    holder.appendChild(sources);
    (mapping.sources || []).forEach(function (id) { var o = document.createElement("option"); o.value = id; o.selected = true; select.appendChild(o); });
    domState.$("#groupMappingRows").appendChild(holder);
    updateGroupMappingOptions();
    updateGroupRules();
}

export function openGroup(group) {
    group = group || {};
    groupsState.editingGroup = group.id || "";
    groupsState.rules = Object.fromEntries((group.model_rules || []).map(r => [r.id, {...r}]));
    groupsState.presets = null;
    domState.$("#applyGroupPresets").disabled = true;
    domState.$("#groupTitle").textContent = groupsState.editingGroup ? tr("编辑账号分组") : tr("创建账号分组");
    domState.$("#groupName").value = group.name || "";
    domState.$("#groupAllowlist").checked = !!group.allowlist_enabled;
    domState.$("#groupError").textContent = "";
    var holder = domState.$("#groupAccounts");
    holder.textContent = "";
    ((serverState.data || {}).accounts || []).forEach(function (a) { var label = mk("label", "check"), input = document.createElement("input"); input.type = "checkbox"; input.value = a.id; input.checked = (group.account_ids || []).includes(a.id); input.addEventListener("change", function () { updateGroupModels(); }); label.appendChild(input); label.appendChild(document.createTextNode(a.name + " · " + (a.provider || "codearts"))); holder.appendChild(label); });
    if (!holder.children.length)
        holder.appendChild(mk("span", "hint", tr("请先添加账号")));
    domState.$("#groupMappingRows").textContent = "";
    updateGroupModels(group.model_ids || []);
    (group.model_mappings || []).forEach(addGroupMapping);
    updateGroupRules();
    domState.$("#groupDefaultModel").value = group.default_model || "";
    domState.$("#groupClientDefaultModel").value = group.client_default_model || "";
    domState.$("#groupDialog").showModal();
    const current = ++groupsState.epoch || (groupsState.epoch = 1);
    api("/admin/api/groups/presets").then(p => { if (groupsState.epoch !== current || !domState.$("#groupDialog").open) return; groupsState.presets = p; domState.$("#applyGroupPresets").disabled = groupsState.groupSaving; }).catch(e => {if (!isAbortError(e)) toast(tr("读取映射预设失败：") + e.message, "err");});
}

export function lockGroupForm(busy) { domState.$("#groupDialog").querySelectorAll("input,select,button").forEach(function (el) { el.disabled = busy; }); if (!busy)
    domState.$("#groupModels").disabled = !domState.$("#groupAllowlist").checked; }

export function closeGroup() { if (!groupsState.groupSaving)
    domState.$("#groupDialog").close(); }

export function mountGroups() {
on("#applyGroupPresets", applyGroupPresets);
on("#addGroupMapping", function () { addGroupMapping(); });
listen(domState.$("#groupModels"), "change", updateGroupMappingOptions);
on("#addGroup", function () { openGroup(); });
on("#groupClose", closeGroup);
on("#groupCancel", closeGroup);
listen(domState.$("#groupDialog"), "cancel", function (e) { e.preventDefault(); closeGroup(); });
listen(domState.$("#groupAllowlist"), "change", function () { updateGroupModels(); });
on("#groupSave", function () {
    if (groupsState.groupSaving)
        return;
    var name = domState.$("#groupName").value.trim();
    if (!name) {
        domState.$("#groupError").textContent = tr("请填写分组名称");
        return;
    }
    var body = { id: groupsState.editingGroup, name: name, account_ids: Array.from(document.querySelectorAll('#groupAccounts input:checked')).map(function (i) { return i.value; }), allowlist_enabled: domState.$("#groupAllowlist").checked, model_ids: Array.from(domState.$("#groupModels").selectedOptions).map(function (o) { return o.value; }), model_mappings: groupMappingValues(), model_rules: Object.values(groupsState.rules), default_model: domState.$("#groupDefaultModel").value, client_default_model: domState.$("#groupClientDefaultModel").value };
    var invalid = body.model_mappings.some(function (m) { return !/[0-9]/.test(m.name) || /[\s/]/.test(m.name) || !m.sources.length; });
    if (invalid) {
        domState.$("#groupError").textContent = tr("请填写带版本号且不含空白、斜线的公开名称，并选择来源型号");
        return;
    }
    groupsState.groupSaving = true;
    lockGroupForm(true);
    domState.$("#groupError").textContent = "";
    return api("/admin/api/groups", { method: "POST", body: body }).then(function (r) { if (!r.ok || !r.group)
        throw new Error(tr("分组保存结果未确认")); domState.$("#groupDialog").close(); toast(tr("分组已保存"), "ok"); return refresh(); }).catch(function (e) { if (isAbortError(e)) return; domState.$("#groupError").textContent = tr("保存失败：") + e.message; }).finally(function () { groupsState.groupSaving = false; lockGroupForm(false); });
});
}

export function updateGroupRules() {
    const data = serverState.data || {}, mappings = groupMappingValues();
    const group = {account_ids: Array.from(document.querySelectorAll('#groupAccounts input:checked')).map(i => i.value), allowlist_enabled: domState.$("#groupAllowlist").checked, model_ids: Array.from(domState.$("#groupModels").selectedOptions).map(o => o.value)};
    const ids = [...new Set([...mappings.map(m => m.name).filter(Boolean), ...(data.models || []).filter(m => m.source !== 'alias' && groupModelAllowed(m, group, data)).map(m => m.id), ...Object.keys(groupsState.rules)])];
    const holder = domState.$('#groupModelRules'); holder.textContent = '';
    ids.forEach(id => {
        const rule = groupsState.rules[id] || {}, line = row([cell(id)]);
        ['visible','callable'].forEach(key => {
            const td = cell(''), input = document.createElement('input'); input.type = 'checkbox'; input.setAttribute('aria-label', id + (key === 'visible' ? tr(" 显示") : tr(" 允许调用")));
            input.checked = rule[key] ?? (key === 'callable' || !mappings.length || mappings.some(m => m.name === id));
            input.addEventListener('change', () => {groupsState.rules[id] = {...(groupsState.rules[id] || {id}), [key]:input.checked}; updateGroupDefaults(ids, mappings);});
            td.appendChild(input); line.appendChild(td);
        }); holder.appendChild(line);
    });
    updateGroupDefaults(ids, mappings);
}
function updateGroupDefaults(ids, mappings) {
    ['groupDefaultModel','groupClientDefaultModel'].forEach(field => {
        const select = domState.$('#'+field), chosen = select.value; select.textContent = '';
        const option = document.createElement('option'); option.value='';option.textContent=field==='groupDefaultModel'?tr("沿用全局默认"):tr("不指定");select.appendChild(option);
        ids.filter(id => (!mappings.length || mappings.some(m=>m.name===id)) && groupsState.rules[id]?.callable !== false && (field!=='groupClientDefaultModel' || (groupsState.rules[id]?.visible ?? (!mappings.length || mappings.some(m=>m.name===id))))).forEach(id=>{const o=document.createElement('option');o.value=id;o.textContent=id;select.appendChild(o);});
        if (chosen && !Array.from(select.options).some(o=>o.value===chosen)) {const o=document.createElement('option');o.value=chosen;o.textContent=chosen+tr("（请检查可见性及调用权限）");select.appendChild(o);}
        select.value=chosen;
    });
}
function applyGroupPresets() {
    if (!groupsState.presets) return;
    const data=serverState.data || {}, existing=groupMappingValues();
    const allowed = new Set(Array.from(domState.$('#groupModels').options).map(o=>o.value).filter(id => !domState.$('#groupAllowlist').checked || Array.from(domState.$('#groupModels').selectedOptions).some(o=>o.value===id)));
    let count=0;
    groupsState.presets.mappings.forEach(p=>{
        if (existing.some(m=>m.name.toLowerCase()===p.name.toLowerCase())) return;
        const sources=p.sources.filter(s=>s.available && allowed.has(s.id) && groupsState.rules[s.id]?.callable!==false && (data.models || []).some(m=>m.id===s.id)).map(s=>s.id);
        if (sources.length) {addGroupMapping({name:p.name,sources});count++;}
    });
    const mappings=groupMappingValues();
    // Suggest once in the unsaved editor; administrator can clear either field.
    if (count && !domState.$('#groupDefaultModel').value) {domState.$('#groupDefaultModel').value=mappings[0].name;domState.$('#groupClientDefaultModel').value=mappings[0].name;}
    toast(count ? tr("已添加 ")+count+tr(" 项预设，请检查来源并保存") : tr("没有可添加的预设；已有映射保持不变"), count?'ok':'');
}
