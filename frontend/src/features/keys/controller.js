import { btn, cell, emptyRow, mk, on, pill, row } from '../../components/controls';
import { isAbortError } from '../../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { copyText, domState, fmtTime, toast } from '../../shared/dom';
import { listen } from '../../shared/lifecycle';
import { api } from '../../api/client';
import { fillGroupSelect, keyGroupCell } from '../groups/controller';
import { refresh } from '../../shared/refresh';
const keysState = {};
keysState.editingKeyGroup = "";
keysState.keyGroupSaving = false;
export function renderKeys(d) {
    var tb = domState.$("#keys");
    tb.textContent = "";
    var keys = d.keys || [];
    if (!keys.length) {
        tb.appendChild(emptyRow(7, "暂无密钥（未签发时 /v1 仅允许本机直连）"));
        return;
    }
    keys.forEach(function (k) {
        var keyCell = cell("", "key-cell");
        var keyInput = mk("input", "mono key-value");
        keyInput.type = "password";
        keyInput.readOnly = true;
        keyInput.value = k.key;
        keyInput.setAttribute("aria-label", "API 密钥 " + (k.name || k.key));
        keyInput.autocomplete = "off";
        keyInput.addEventListener("click", function () { keyInput.select(); });
        keyCell.appendChild(keyInput);
        var st = cell("");
        st.appendChild(pill(k.enabled ? "启用" : "停用", k.enabled ? "ok" : ""));
        var ops = cell("", "actions-cell");
        function fetchKey() { return api("/admin/api/keys/reveal", { method: "POST", body: { key: k.ref || k.key } }).then(function (d) { if (!d.value)
            throw new Error("密钥不存在"); return d.value; }); }
        ops.appendChild(btn("复制", "xs primary", function () {
            return fetchKey().then(function (value) { return copyText(value, "密钥已复制"); }).catch(function (e) { if (isAbortError(e)) return; toast(e.message, "err"); });
        }));
        var reveal = btn("显示", "xs", function () {
            if (keyInput.type === "text") {
                keyInput.type = "password";
                keyInput.value = k.key;
                reveal.textContent = "显示";
                return;
            }
            return fetchKey().then(function (value) { keyInput.value = value; keyInput.type = "text"; reveal.textContent = "隐藏"; }).catch(function (e) { if (isAbortError(e)) return; toast(e.message, "err"); });
        });
        ops.appendChild(reveal);
        ops.appendChild(btn("预算", "xs", function () { openKeyBudget(k); }));
        if (!k.user_id) ops.appendChild(btn("分组", "xs", function () { openKeyGroup(k); }));
        ops.appendChild(btn(k.enabled ? "停用" : "启用", "xs", function () { keyOp("toggle", k.ref || k.key); }));
        ops.appendChild(btn("删除", "xs danger", function () {
            if (confirm("确定删除该密钥？"))
                keyOp("key", k.ref || k.key);
        }));
        tb.appendChild(row([
            keyCell, keyGroupCell(k), st, cell(k.requests, "mono"),
            cell(fmtTime(k.created_at), "muted"), cell(fmtTime(k.last_used), "muted"), ops
        ]));
    });
}

export function keyOp(kind, key) {
    return api("/admin/api/keys?" + kind + "=" + encodeURIComponent(key), { method: "DELETE" })
        .then(function () { toast("操作成功", "ok"); return refresh(); })
        .catch(function (e) { if (isAbortError(e)) return; toast("失败：" + e.message, "err"); });
}

keysState.keyCreating = false;

export function closeKeyCreate() { if (!keysState.keyCreating)
    domState.$("#keyCreateDialog").close(); }

export function openKeyGroup(key) { keysState.editingKeyGroup = key.ref || key.key; domState.$("#keyGroupName").textContent = key.name || "未命名密钥"; fillGroupSelect(domState.$("#keyGroupSelect"), key.group_id, "不分组（全部账号）"); domState.$("#keyGroupError").textContent = ""; domState.$("#keyGroupDialog").showModal(); }

export function closeKeyGroup() { if (!keysState.keyGroupSaving)
    domState.$("#keyGroupDialog").close(); }

keysState.budgetKeyRef = "";

keysState.budgetBusy = false;

export function openKeyBudget(k) {
    var b = k.budget || {}, u = k.budget_usage || {};
    keysState.budgetKeyRef = k.ref;
    domState.$("#budgetRequests").value = b.max_requests || 0;
    domState.$("#budgetTokens").value = b.max_tokens || 0;
    domState.$("#budgetUSD").value = b.max_usd || 0;
    domState.$("#keyBudgetUsage").textContent = "已用：" + (u.requests || 0) + " 次 / " + (u.tokens || 0) + " Token / $" + Number(u.usd || 0).toFixed(6) + (u.incomplete ? " · 存在未报告用量，需核查后重置预算" : "") + (u.unpriced ? " · 存在未定价消耗，USD 预算需核查后重置" : "");
    domState.$("#budgetReset").checked = false;
    domState.$("#keyBudgetError").textContent = "";
    domState.$("#keyBudgetDialog").showModal();
}

export function mountKeys() {
on("#keyCreateClose", closeKeyCreate);
on("#keyCreateCancel", closeKeyCreate);
listen(domState.$("#keyCreateDialog"), "cancel", function (e) { e.preventDefault(); closeKeyCreate(); });
listen(domState.$("#keyCreateForm"), "submit", function (e) {
    e.preventDefault();
    if (keysState.keyCreating)
        return;
    var body = { name: domState.$("#keyName").value.trim(), group_id: domState.$("#keyCreateGroup").value };
    keysState.keyCreating = true;
    domState.$("#keyCreateError").textContent = "";
    domState.$("#keyCreateDialog").querySelectorAll("input,select,button").forEach(function (el) { el.disabled = true; });
    domState.$("#keyCreateSave").textContent = "创建中…";
    api("/admin/api/keys", { method: "POST", body: body }).then(function (r) {
        if (!r.ok || !r.key)
            throw new Error("密钥创建结果未确认");
        toast("密钥已创建，可在列表中随时显示或复制", "ok");
        domState.$("#keyCreateDialog").close();
        domState.$("#keyName").value = "";
        return refresh();
    }).catch(function (e) { if (isAbortError(e)) return; domState.$("#keyCreateError").textContent = "创建失败：" + e.message; }).finally(function () {
        keysState.keyCreating = false;
        domState.$("#keyCreateSave").textContent = "创建密钥";
        domState.$("#keyCreateDialog").querySelectorAll("input,select,button").forEach(function (el) { el.disabled = false; });
    });
});
on("#keyGroupClose", closeKeyGroup);
on("#keyGroupCancel", closeKeyGroup);
listen(domState.$("#keyGroupDialog"), "cancel", function (e) { e.preventDefault(); closeKeyGroup(); });
on("#keyGroupSave", function () {
    if (keysState.keyGroupSaving)
        return;
    keysState.keyGroupSaving = true;
    domState.$("#keyGroupError").textContent = "";
    domState.$("#keyGroupSelect").disabled = true;
    return api("/admin/api/keys/group", { method: "POST", body: { ref: keysState.editingKeyGroup, group_id: domState.$("#keyGroupSelect").value } }).then(function (r) { if (!r.ok)
        throw new Error("分组绑定未确认"); domState.$("#keyGroupDialog").close(); return refresh(); }).catch(function (e) { if (isAbortError(e)) return; domState.$("#keyGroupError").textContent = "保存失败：" + e.message; }).finally(function () { keysState.keyGroupSaving = false; domState.$("#keyGroupSelect").disabled = false; });
});
on("#keyBudgetClose", function () { if (!keysState.budgetBusy)
    domState.$("#keyBudgetDialog").close(); });
listen(domState.$("#keyBudgetDialog"), "cancel", function (e) { if (keysState.budgetBusy)
    e.preventDefault(); });
on("#keyBudgetSave", function () {
    if (keysState.budgetBusy)
        return;
    var budget = {}, fields = { max_requests: "#budgetRequests", max_tokens: "#budgetTokens", max_usd: "#budgetUSD" };
    for (var name in fields) {
        var el = domState.$(fields[name]);
        if (!el.checkValidity() || !el.value.trim()) {
            domState.$("#keyBudgetError").textContent = "请填写非负预算";
            return;
        }
        budget[name] = Number(el.value);
    }
    keysState.budgetBusy = true;
    return api("/admin/api/keys/budget", { method: "POST", body: { ref: keysState.budgetKeyRef, budget: budget, reset: domState.$("#budgetReset").checked } }).then(function () { domState.$("#keyBudgetDialog").close(); toast("预算已保存", "ok"); return refresh(); }).catch(function (e) { if (isAbortError(e)) return; domState.$("#keyBudgetError").textContent = e.message; }).finally(function () { keysState.budgetBusy = false; });
});
on("#addKey", function () {
    domState.$("#keyName").value = "";
    domState.$("#keyCreateError").textContent = "";
    fillGroupSelect(domState.$("#keyCreateGroup"), "", "不分组（全部账号）");
    domState.$("#keyCreateDialog").showModal();
    domState.$("#keyName").focus();
});
}
