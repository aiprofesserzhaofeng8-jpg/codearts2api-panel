import { cell, emptyRow, on, pill, row } from '../../components/controls';
import { isAbortError } from '../../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { domState, fmtTime, toast } from '../../shared/dom';
import { api } from '../../api/client';
import { sessionState } from '../../app/session';
const logsState = {};
export function renderLogs(logs) {
    var tb = domState.$("#logs");
    tb.textContent = "";
    if (!logs || !logs.length) {
        tb.appendChild(emptyRow(9, "暂无日志"));
        return;
    }
    logs.forEach(function (l) {
        var st = cell("");
        st.appendChild(pill(l.status, l.status >= 500 ? "err" : (l.status >= 400 ? "warn" : "ok")));
        var errCell = cell(l.err || "", "muted trunc");
        if (l.err)
            errCell.title = l.err;
        var elapsed = cell((l.first_token_ms == null ? "—" : l.first_token_ms + "ms") + " / " + l.latency_ms + "ms", "mono");
        elapsed.title = (l.attempt_details || []).map(function (a) {
            return "第 " + a.number + " 次：" + (a.duration_ms == null ? "—" : a.duration_ms + "ms") +
                (a.status ? " · " + a.status : "") + (a.error_kind ? " · " + a.error_kind : "");
        }).join("\n");
        tb.appendChild(row([
            cell(fmtTime(l.time), "muted"), cell((l.requested_model && l.requested_model !== l.model ? l.requested_model + " → " : "") + l.model, "mono"), cell(l.stream ? "是" : "否", "mono"),
            st, elapsed, cell(l.prompt_len + " / " + l.output_len, "mono"),
            cell(l.account, "mono"), cell(l.key, "mono"), errCell
        ]));
    });
}

export function refreshLogs() {
    var limit = domState.$("#logLimit").value;
    return api("/admin/api/logs?limit=" + encodeURIComponent(limit)).then(function (d) {
        renderLogs(d.logs);
    }).catch(function (e) { if (isAbortError(e)) return; toast("日志加载失败：" + e.message, "err"); });
}

logsState.auditPage = 1;

logsState.auditTotal = 0;

export function loadAudit() {
    if (!sessionState.adminAuthenticated)
        return Promise.resolve();
    return api("/admin/api/audit?page=" + logsState.auditPage + "&page_size=50").then(function (d) {
        logsState.auditTotal = d.total || 0;
        var tb = domState.$("#auditRows");
        tb.textContent = "";
        (d.events || []).forEach(function (e) { tb.appendChild(row([cell(fmtTime(e.time)), cell(e.actor), cell(e.operation), cell(e.path, "mono"), cell(e.peer, "mono"), cell(e.status, "mono")])); });
        if (!tb.children.length)
            tb.appendChild(emptyRow(6, "暂无管理记录"));
        domState.$("#auditPage").textContent = "第 " + logsState.auditPage + " 页 · 共 " + logsState.auditTotal + " 条";
        domState.$("#auditPrev").disabled = logsState.auditPage <= 1;
        domState.$("#auditNext").disabled = logsState.auditPage * 50 >= logsState.auditTotal;
    }).catch(function (e) { if (isAbortError(e)) return; toast(e.message, "err"); });
}

export function mountLogs() {
on("#reloadLogs", refreshLogs);
on("#clearLogs", function () {
    if (!confirm("确定清空全部日志？"))
        return;
    api("/admin/api/logs", { method: "DELETE" }).then(function () { toast("已清空", "ok"); return refreshLogs(); })
        .catch(function (e) { if (isAbortError(e)) return; toast("失败：" + e.message, "err"); });
});
domState.$("#logLimit").onchange = refreshLogs;
on("#reloadAudit", loadAudit);
on("#auditPrev", function () { logsState.auditPage = Math.max(1, logsState.auditPage - 1); return loadAudit(); });
on("#auditNext", function () { logsState.auditPage++; return loadAudit(); });
}
