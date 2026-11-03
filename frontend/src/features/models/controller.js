import { t as tr } from '../../i18n';
import { serverState } from '../../shared/read-model';
import { listen } from '../../shared/lifecycle';
import { cell, emptyRow, mk, on, pill, row } from '../../components/controls';
import { isAbortError } from '../../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { domState, fmtNum, toast } from '../../shared/dom';
import { groupModelAllowed } from '../groups/controller';
import { api } from '../../api/client';
import { refresh } from '../../shared/refresh';
const modelsState = {};
export function renderOverview(d) {
    var tb = domState.$("#models");
    tb.textContent = "";
    var models = d.models || [];
    var group = (d.groups || []).find(function (g) { return g.id === domState.$("#modelsGroupFilter").value; });
    if (group)
        models = models.filter(function (m) { return groupModelAllowed(m, group, d); });
    var query = domState.$("#modelSearch").value.trim().toLowerCase();
    if (query)
        models = models.filter(function (m) { return (m.id + " " + (m.name || "")).toLowerCase().includes(query); });
    domState.$("#modelsCount").textContent = models.length + tr(" 个");
    domState.$("#modelHint").textContent = d.upstream_error ? (tr("最近一次上游刷新失败：") + d.upstream_error) : "";
    // 每次渲染重建元数据索引：上游清单可能删掉了模型，留着旧条目会让
    // 「测活全部」去探测已经不存在的模型，并让单行重绘拿到 undefined。
    modelsState.lastModels = {};
    if (!models.length) {
        tb.appendChild(emptyRow(7, tr("没有符合条件的模型")));
        return;
    }
    models.forEach(function (m) {
        modelsState.lastModels[m.id] = m;
        tb.appendChild(modelRow(m));
    });
}

modelsState.modelProbeState = {};

modelsState.lastModels = {};

export function modelRow(m) {
    var actionCell = cell("", "actions-cell");
    // 别名只是名字映射，指向的仍是上游真实模型，单独测活没有意义。
    if (m.source === "alias") {
        actionCell.appendChild(mk("span", "hint m-0", tr("别名")));
        return row([
            cell(m.id, "mono"), cell(m.name),
            cell(m.credit || "—", "mono"),
            cell(m.context ? fmtNum(m.context) : "—", "mono"),
            cell(m.max_output ? fmtNum(m.max_output) : "—", "mono"),
            cell(m.description, "muted"),
            actionCell
        ]);
    }
    var btn = mk("button", "btn xs", tr("测活"));
    var st = modelsState.modelProbeState[m.id] || { state: "idle" };
    if (st.state === "busy") {
        btn.disabled = true;
        btn.textContent = tr("测活中…");
    }
    else if (st.state === "ok" || st.state === "err") {
        btn.textContent = tr("重测");
    }
    btn.onclick = function () { probeOne(m.id, m.name || m.id); };
    actionCell.appendChild(btn);
    if (st.state === "ok") {
        actionCell.appendChild(document.createTextNode(" "));
        actionCell.appendChild(pill(tr("可用"), "ok"));
    }
    else if (st.state === "err") {
        actionCell.appendChild(document.createTextNode(" "));
        var bad = pill(tr("不可用"), "err");
        bad.title = st.err || "";
        actionCell.appendChild(bad);
    }
    var r = row([
        cell(m.id, "mono"), cell(m.name),
        cell(m.credit || "—", "mono"),
        cell(m.context ? fmtNum(m.context) : "—", "mono"),
        cell(m.max_output ? fmtNum(m.max_output) : "—", "mono"),
        cell(m.description, "muted"),
        actionCell
    ]);
    if (st.state === "err" && st.err)
        r.title = st.err;
    return r;
}

// probeOne 对单个模型测活，只重绘那一行。
export function probeOne(modelID, label) {
    if (modelsState.modelProbeState[modelID] && modelsState.modelProbeState[modelID].state === "busy")
        return;
    modelsState.modelProbeState[modelID] = { state: "busy" };
    redrawModelRow(modelID);
    domState.$("#modelHint").textContent = tr("正在测活 ") + label + "…";
    return api("/admin/api/models/probe", { method: "POST", body: { models: [modelID] } }).then(function (r) {
        var row0 = (r.results || [])[0] || {};
        modelsState.modelProbeState[modelID] = row0.ok
            ? { state: "ok", account: row0.account }
            : { state: "err", err: row0.error || tr("未知错误"), account: row0.account };
        redrawModelRow(modelID);
        domState.$("#modelHint").textContent = row0.ok
            ? (label + tr(" 可用") + (row0.account ? tr("（账号 ") + row0.account + "）" : ""))
            : (label + tr(" 不可用：") + (row0.error || tr("未知错误")));
    }).catch(function (e) { if (isAbortError(e)) return;
        modelsState.modelProbeState[modelID] = { state: "err", err: (e && e.message) || tr("请求失败") };
        redrawModelRow(modelID);
        domState.$("#modelHint").textContent = label + tr(" 测活失败：") + ((e && e.message) || tr("未知错误"));
    });
}

// redrawModelRow 只替换指定模型那一行，避免整表重绘打断其他行。
export function redrawModelRow(modelID) {
    var meta = modelsState.lastModels[modelID];
    if (!meta)
        return; // 清单已刷新、该模型不在了，无事可做
    var tb = domState.$("#models");
    var rows = tb.children;
    for (var i = 0; i < rows.length; i++) {
        var first = rows[i].firstChild;
        if (first && first.textContent === modelID) {
            tb.replaceChild(modelRow(meta), rows[i]);
            return;
        }
    }
}

export function mountModels() {
on("#refreshModels", function () {
    toast(tr("正在从上游刷新模型清单…"));
    api("/admin/api/models/refresh", { method: "POST" }).then(function (r) {
        toast(tr("已刷新，共 ") + (r.models || []).length + tr(" 个模型") + (Object.keys(r.provider_errors || {}).length ? tr("；部分上游刷新失败，请查看状态") : ""), Object.keys(r.provider_errors || {}).length ? "err" : "ok");
        return refresh();
    }).catch(function (e) { if (isAbortError(e)) return; toast(tr("刷新失败：") + e.message, "err"); });
});
on("#probeAllModels", function () {
    var ids = Object.keys(modelsState.lastModels).filter(function (id) {
        return modelsState.lastModels[id].source !== "alias";
    });
    if (!ids.length) {
        toast(tr("当前列表没有可测活的模型，请调整筛选或刷新清单"), "err");
        return;
    }
    // 每个模型都是一次真实请求、真实扣积分，全量探测前必须二次确认。
    if (!window.confirm(tr("将向 ") + ids.length + tr(" 个模型各发一次真实请求（max_tokens=8，会真实扣积分），确定继续？")))
        return;
    ids.forEach(function (id) { modelsState.modelProbeState[id] = { state: "busy" }; redrawModelRow(id); });
    domState.$("#modelHint").textContent = tr("正在测活 ") + ids.length + tr(" 个模型…（每个会真实消耗极少量积分）");
    return api("/admin/api/models/probe", { method: "POST", body: { models: ids } }).then(function (r) {
        (r.results || []).forEach(function (x) {
            modelsState.modelProbeState[x.model] = x.ok
                ? { state: "ok", account: x.account }
                : { state: "err", err: x.error || tr("未知错误"), account: x.account };
            redrawModelRow(x.model);
        });
        domState.$("#modelHint").textContent = tr("可用 ") + (r.available || 0) + " / " + (r.total || 0) +
            (r.unavailable ? tr("，不可用 ") + r.unavailable : "");
        toast(tr("测活完成：") + (r.available || 0) + "/" + (r.total || 0) + tr(" 可用"), "ok");
    }).catch(function (e) { if (isAbortError(e)) return;
        var msg = (e && e.message) ? e.message : tr("未知错误");
        ids.forEach(function (id) { modelsState.modelProbeState[id] = { state: "err", err: msg }; redrawModelRow(id); });
        domState.$("#modelHint").textContent = tr("测活失败：") + msg;
        toast(tr("测活失败：") + msg, "err");
    });
});
listen(domState.$("#modelsGroupFilter"), "change", function () { if (serverState.data)
    renderOverview(serverState.data); });
listen(domState.$("#modelSearch"), "input", function () { if (serverState.data)
    renderOverview(serverState.data); });
}
