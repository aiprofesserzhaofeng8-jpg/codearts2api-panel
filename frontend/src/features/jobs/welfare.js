import { t as tr } from '../../i18n';
import { copyText } from '../../shared/dom';
import { cell, emptyRow, on, pill, row } from '../../components/controls';
import { isAbortError } from '../../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { domState, toast } from '../../shared/dom';
import { api } from '../../api/client';
import { pendingCredits, invalidateCredit, loadCredits } from '../accounts/credits';
import { listen } from '../../shared/lifecycle';
import { refresh } from '../../shared/refresh';
const welfareState = {};
export function checkin(id) {
    toast(tr("探活中，请稍候…"));
    return api("/admin/api/checkin", { method: "POST", body: id ? { id: id } : {} }).then(function (r) {
        var results = r.results || [];
        var bad = results.filter(function (x) { return !x.ok; });
        toast(tr("探活完成：") + results.length + tr(" 个账号，失败 ") + bad.length +
            (bad.length ? "\n" + bad.map(function (x) { return x.name + ": " + x.err; }).join("\n") : ""), bad.length ? "err" : "ok");
        return refresh();
    }).catch(function (e) { if (isAbortError(e)) return; toast(tr("探活失败：") + e.message, "err"); });
}

export function claim(id) {
    toast(tr("领取中，请稍候…"));
    return api("/admin/api/claim", { method: "POST", body: id ? { id: id } : {} }).then(function (r) {
        var results = r.results || [];
        var bad = results.filter(function (x) { return !x.ok; });
        toast(tr("领取完成：") + (r.succeeded || 0) + "/" + (r.total || 0) + tr(" 成功") +
            (results.length ? "\n" + results.map(function (x) { return x.name + ": " + x.message; }).join("\n") : ""), bad.length ? "err" : "ok");
        // Discard balances fetched before the claim, including any in-flight query.
        return Promise.resolve(pendingCredits()).then(function () {
            results.forEach(function (x) { invalidateCredit(x.id); });
            return refresh();
        }).then(function () { return pendingCredits(); }).then(function () { return loadCredits(true); });
    }).catch(function (e) { if (isAbortError(e)) return; toast(tr("领取失败：") + e.message, "err"); });
}

welfareState.WELFARE_STATUS = {
    claimable: [tr("可签到"), "ok"], claimed: [tr("已签到"), "ok"], already_claimed: [tr("已签到"), "ok"], unavailable: [tr("暂不可领"), "warn"], unknown: [tr("待确认"), ""],
    ELIGIBLE: [tr("可领取"), "ok"],
    CLAIMED: [tr("已领取"), "warn"],
    CONFIRMED: [tr("已到账"), "ok"],
    CONSUMED: [tr("已使用"), ""],
    ENTRY: [tr("需先完成前置"), "warn"]
};

export function renderWelfare(campaigns) {
    var tb = domState.$("#welfare");
    tb.textContent = "";
    if (!campaigns || !campaigns.length) {
        tb.appendChild(emptyRow(7, tr("暂无活动")));
        return;
    }
    campaigns.forEach(function (c) {
        var st = welfareState.WELFARE_STATUS[c.status] || [c.status || "—", ""];
        var stCell = cell("");
        stCell.appendChild(pill(st[0], st[1]));
        var autoCell = cell("");
        autoCell.appendChild(c.auto_claimable ? pill(tr("是"), "ok") : pill(tr("否"), ""));
        var reward = c.benefit_amount ? (c.benefit_amount + " " + (c.benefit_unit || "")) : "—";
        var period = c.type === "daily_checkin" ? tr("每日（UTC+8）") : (c.start_time || "").slice(0, 10) + " ~ " + (c.end_time || "").slice(0, 10);
        tb.appendChild(row([
            cell(c.title), cell(c.type === "daily_checkin" ? tr("签到") : c.type, "mono"), cell(reward, "mono"),
            stCell, autoCell, cell(c.trigger_event || c.trigger_mode || "—", "mono"),
            cell(period, "muted")
        ]));
    });
}

welfareState.welfareAccountID = "";

export function loadWelfare() {
    domState.$("#welfareHint").textContent = tr("加载中…");
    var q = welfareState.welfareAccountID ? ("?id=" + encodeURIComponent(welfareState.welfareAccountID)) : "";
    return api("/admin/api/welfare" + q).then(function (r) {
        renderWelfare(r.campaigns);
        renderWelfareAccountPicker(r);
        domState.$("#welfareHint").textContent = r.message || (r.claimable_count
            ? (tr("当前账号 ") + (r.account || "") + tr("，可领 ") + r.claimable_count + tr(" 项"))
            : (tr("当前账号 ") + (r.account || "") + tr("，暂无可领")));
    }).catch(function (e) { if (isAbortError(e)) return;
        domState.$("#welfareHint").textContent = "";
        toast(tr("获取活动列表失败：") + e.message, "err");
    });
}

// renderWelfareAccountPicker 把账号下拉框填满，并同步当前选择。
//
// 之前只显示一个账号名，很容易被误解成「只有这个号能领」——实际上
// 活动清单是按账号查的，每个号的可领项都不同。
export function renderWelfareAccountPicker(r) {
    var sel = domState.$("#welfareAccount");
    if (!sel)
        return;
    var list = r.accounts || [];
    sel.textContent = "";
    list.forEach(function (a) {
        var o = document.createElement("option");
        o.value = a.id;
        o.textContent = (a.provider && a.provider !== "codearts" ? a.provider + " · " : "") + a.name + (a.permanent ? tr("（永久 AK/SK）") : "");
        if (a.id === r.id)
            o.selected = true;
        sel.appendChild(o);
    });
    if (r.id)
        welfareState.welfareAccountID = r.id;
    sel.disabled = list.length <= 1;
    // 账号只有一个时说明白，别让用户以为漏了
    sel.title = list.length <= 1
        ? tr("当前只有 1 个账号")
        : tr("活动清单按账号查询，每个账号的可领项不同");
}

export function mountWelfare() {
on("#checkinAll", function () { return checkin(null); });
on("#claimAll", function () { return claim(null); });
on("#loadWelfare", loadWelfare);
// 切换账号后重新拉该账号的活动清单（各账号的领取状态不同）
listen(domState.$("#welfareAccount"), "change", function () {
    welfareState.welfareAccountID = this.value;
    loadWelfare();
});
on("#claimWelfareNow", function () { if (!welfareState.welfareAccountID) {
    toast(tr("请先选择活动账号"), "err");
    return;
} return claim(welfareState.welfareAccountID).then(loadWelfare); });
on("#genInvite", function () {
    toast(tr("生成中…"));
    // 用当前选中的账号生成，避免「看到的是 A 号、生成的是 B 号」
    var body = welfareState.welfareAccountID ? { id: welfareState.welfareAccountID } : {};
    return api("/admin/api/invite", { method: "POST", body: body }).then(function (r) {
        domState.$("#inviteURL").value = r.invite_url || "";
        domState.$("#inviteStat").textContent = tr("已激活 ") + (r.activation_count || 0) + " / " + (r.max_activations || 0) +
            tr(" 次 · 状态 ") + (r.status || "") + tr(" · 账号 ") + (r.account || "");
        domState.$("#invitePanel").classList.remove("hidden");
        toast(tr("已生成邀请链接"), "ok");
    }).catch(function (e) { if (isAbortError(e)) return; toast(tr("生成失败：") + e.message, "err"); });
});
on("#copyInvite", function () { return copyText(domState.$("#inviteURL").value, tr("已复制邀请链接")); });
}
