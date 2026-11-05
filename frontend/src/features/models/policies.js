import { t as tr } from '../../i18n';
import { btn, cell, emptyRow, mk, on, row } from '../../components/controls';
import { serverState } from '../../shared/read-model';
import { isAbortError } from '../../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { api } from '../../api/client';
import { domState, toast } from '../../shared/dom';
import { listen } from '../../shared/lifecycle';
const policiesState = {};
policiesState.modelPolicies = {};

policiesState.editingPolicy = null;

policiesState.savingPolicy = false;
policiesState.routingRevision = 0;
policiesState.editingRevision = 0;

policiesState.policyFields = { context_window: "#policyContext", max_output_tokens: "#policyOutput", reserve_tokens: "#policyReserve", keep_recent_tokens: "#policyKeep", trigger_tokens: "#policyTrigger", summary_max_tokens: "#policySummaryMax" };

export function loadModelPolicies() {
    if (!serverState.data)
        return Promise.resolve();
    return api("/admin/api/model-policies").then(function (d) { policiesState.modelPolicies = d.policies || {}; policiesState.routingRevision=d.routing_revision; renderModelPolicies(); }).catch(function (e) { if (isAbortError(e)) return; toast(tr("策略加载失败：") + e.message, "err"); });
}

export function renderModelPolicies() {
    var ids = Object.keys(policiesState.modelPolicies).filter(id=>!id.startsWith('unified/'));
    ((serverState.data || {}).models || []).forEach(function (m) { if (m.source !== "alias" && ids.indexOf(m.id) < 0)
        ids.push(m.id); });
    var tb = domState.$("#modelPolicies");
    tb.textContent = "";
    ids.sort().forEach(function (id) {
        var p = policiesState.modelPolicies[id], ops = cell("");
        ops.appendChild(btn(tr("编辑"), "xs", function () { openModelPolicy(id); }));
        tb.appendChild(row([
            cell(id, "mono"), cell(p ? p.context_window + " / " + p.max_output_tokens : tr("未配置"), "mono"), cell(p ? p.trigger_tokens + " / " + p.keep_recent_tokens : "—", "mono"),
            cell(p && p.summary_enabled ? (p.summary_retry ? tr("完整性保护 · 最多 2 次") : tr("完整性保护 · 单次")) : tr("关闭"), "muted"), cell(p ? p.source : tr("先验证模型能力，再配置"), "muted pricing-source"), ops
        ]));
    });
    if (!ids.length)
        tb.appendChild(emptyRow(6, tr("暂无模型")));
}

export function openModelPolicy(id) {
    if (policiesState.savingPolicy)
        return;
    policiesState.editingPolicy = id;
    policiesState.editingRevision = policiesState.routingRevision;
    domState.$("#policyModel").textContent = id;
    domState.$("#policyError").textContent = "";
    var p = policiesState.modelPolicies[id] || {};
    Object.keys(policiesState.policyFields).forEach(function (k) { domState.$(policiesState.policyFields[k]).value = p[k] || ""; });
    var fallback = domState.$("#policyFallback");
    fallback.textContent = "";
    var none = mk("option", "", tr("不使用备用模型"));
    none.value = "";
    fallback.appendChild(none);
    ((serverState.data || {}).models || []).forEach(function (m) { if (m.source !== "alias" && m.id !== id) {
        var o = mk("option", "", m.id);
        o.value = m.id;
        fallback.appendChild(o);
    } });
    fallback.value = p.summary_fallback_model || "";
    domState.$("#policySummary").checked = !!p.summary_enabled;
    domState.$("#policyRetry").checked = !!p.summary_retry;
    domState.$("#policyThinking").checked = !!p.disable_summary_thinking;
    domState.$("#policyDialog").showModal();
}

export function closeModelPolicy() { if (!policiesState.savingPolicy) {
    domState.$("#policyDialog").close();
    policiesState.editingPolicy = null;
} }

export function saveModelPolicy() {
    if (policiesState.savingPolicy || !policiesState.editingPolicy)
        return;
    var policy = { source: (policiesState.modelPolicies[policiesState.editingPolicy]||{}).source || tr('管理员配置，非上游能力声明'), summary_enabled: domState.$("#policySummary").checked, summary_retry: domState.$("#policyRetry").checked, disable_summary_thinking: domState.$("#policyThinking").checked };
    for (var k in policiesState.policyFields) {
        var input = domState.$(policiesState.policyFields[k]);
        if (!input.checkValidity() || input.value.trim() === "") {
            domState.$("#policyError").textContent = tr("请填写有效整数参数");
            input.focus();
            return;
        }
        policy[k] = Number(input.value);
    }
    policy.summary_fallback_model = domState.$("#policyFallback").value;
    policiesState.savingPolicy = true;
    domState.$("#policyError").textContent = "";
    domState.$('#policyDialog').querySelectorAll('input,select,button').forEach(el=>el.disabled=true);
    return api("/admin/api/model-policies", { method: "POST", body: { model: policiesState.editingPolicy, policy: policy, expected_routing_revision:policiesState.editingRevision } }).then(function (d) { policiesState.modelPolicies = d.policies; policiesState.routingRevision=d.routing_revision; renderModelPolicies(); domState.$("#policyDialog").close(); policiesState.editingPolicy = null; toast(tr("策略已保存；重新运行客户端启动器获取更新"), "ok"); }).catch(function (e) { if (isAbortError(e)) return; domState.$("#policyError").textContent = e.message + (e.status===409||e.status===428 ? ' · '+tr('草稿已保留，请关闭后重新读取策略再编辑。') : ''); }).finally(function () { policiesState.savingPolicy = false; domState.$('#policyDialog').querySelectorAll('input,select,button').forEach(el=>el.disabled=false); });
}

export function mountPolicies() {
on("#reloadModelPolicies", loadModelPolicies);
on("#policySave", saveModelPolicy);
on("#policyCancel", closeModelPolicy);
on("#policyClose", closeModelPolicy);
listen(domState.$("#policyDialog"), "cancel", function (e) { if (policiesState.savingPolicy)
    e.preventDefault();
else
    policiesState.editingPolicy = null; });
}
