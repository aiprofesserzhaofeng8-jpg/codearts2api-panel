import { t as tr } from '../../i18n';
import { appendQuota } from './quota';
import { cleanup } from '../../shared/lifecycle';
import { btn, cell, mk, on, pill } from '../../components/controls';
import { serverState } from '../../shared/read-model';
import { isAbortError } from '../../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { api } from '../../api/client';
import { fmtNum, toast } from '../../shared/dom';
import { renderAccounts } from './controller';
import { createGrowth } from '../jobs/growth';
const creditsState = {};
creditsState.creditState = {};

creditsState.creditLoading = null;

// loadCredits 查询账号积分额度（上游统计插件）。
// 默认走服务端 60 秒缓存；force=true 时带 ?fresh=1 强制回源。
export function loadCredits(force) {
    if (creditsState.creditLoading)
        return creditsState.creditLoading;
    var accts = (serverState.data && serverState.data.accounts) || [];
    if (!accts.length)
        return Promise.resolve();
    accts.forEach(function (a) { creditsState.creditState[a.id] = { loading: true }; });
    if (serverState.data)
        renderAccounts(serverState.data);
    var url = "/admin/api/credits" + (force ? "?fresh=1" : "");
    creditsState.creditLoading = api(url).then(function (d) {
        (d.accounts || []).forEach(function (x) {
            creditsState.creditState[x.id] = x.ok
                ? { loading: false, ok: true, credit: x.credit }
                : { loading: false, ok: false, error: x.error };
        });
        if (serverState.data)
            renderAccounts(serverState.data);
    }).catch(function (e) { if (isAbortError(e)) return;
        var msg = (e && e.message) || tr("查询失败");
        accts.forEach(function (a) { creditsState.creditState[a.id] = { loading: false, ok: false, error: msg }; });
        if (serverState.data)
            renderAccounts(serverState.data);
        toast(tr("查询积分额度失败：") + msg, "err");
    }).then(function () { creditsState.creditLoading = null; });
    return creditsState.creditLoading;
}

// creditCell 渲染一个账号的积分额度：剩余/总量 + 进度条 + 重置日期。
export function creditCell(a) {
    var c = cell("", "cell-stack");
    var st = creditsState.creditState[a.id];
    if (!st || st.loading) {
        c.appendChild(mk("span", "muted", st ? tr("查询中…") : "—"));
        return c;
    }
    if (!st.ok) {
        var bad = mk("span", "muted", tr("查询失败"));
        bad.title = st.error || "";
        c.appendChild(bad);
        if (st.error) c.appendChild(mk("span", "sub quota-query-error", st.error));
        return c;
    }
    var info = st.credit || {};
    if (appendQuota(c, info)) return c;
    if (!info.available) {
        var accessDenied = a.status === "error" && String(a.last_err || "").indexOf("TM.00001005") >= 0;
        var none = mk("span", "muted", accessDenied ? tr("需开通码道权限") : (info.is_token_package ? tr("Token 套餐") : tr("额度未返回")));
        none.title = accessDenied ? tr("最近一次模型调用被上游拒绝：尚未获得此功能的访问权限。请使用该账号到码道官方页面核对开通状态。") : (info.reason || tr("上游未提供积分数据，不能据此判定没有套餐或余额"));
        c.appendChild(none);
        return c;
    }
    var total = Number(info.total) || 0;
    var remain = Number(info.remaining) || 0;
    var used = Number(info.used) || 0;
    c.appendChild(mk("span", "mono", fmtNum(remain) + " / " + fmtNum(total)));
    var bar = mk("div", "bar credit-bar");
    var pct = total > 0 ? Math.max(0, Math.min(100, (remain / total) * 100)) : 0;
    var fill = document.createElement("progress");
    fill.max = 100;
    fill.value = pct;
    fill.setAttribute("aria-label", tr("剩余积分 ") + pct.toFixed(0) + "%");
    if (pct <= 20)
        bar.classList.add("err");
    else if (pct <= 50)
        bar.classList.add("warn");
    bar.appendChild(fill);
    c.appendChild(bar);
    var bits = [tr("已用 ") + fmtNum(used)];
    if (info.expiring)
        bits.push(tr("即将到期 ") + fmtNum(info.expiring));
    if (info.reset_date)
        bits.push(tr("重置 ") + info.reset_date);
    if (info.status && info.status !== "active" && info.status !== "unknown")
        bits.push(info.status);
    var sub = mk("span", "sub", bits.join(" · "));
    sub.title = (info.package_name || "") + (info.spec_code ? (" / " + info.spec_code) : "");
    c.appendChild(sub);
    return c;
}

export function mountCredits() {
cleanup(function(){ if(creditsState.growthTasks)creditsState.growthTasks.dispose(); });
creditsState.growthTasks = createGrowth({ api: api, mk: mk, btn: btn, pill: pill, onBalance: function (id, balance) {
        creditsState.creditState[id] = { loading: false, ok: true, credit: balance };
        if (serverState.data)
            renderAccounts(serverState.data);
    } });
on("#refreshCredits", function () { return loadCredits(true); });
}

export function openGrowth(account) { return creditsState.growthTasks.open(account); }
export function openGrowthAccounts(accounts) { return creditsState.growthTasks.openAccounts(accounts); }
export function invalidateCredit(id) { delete creditsState.creditState[id]; }

export function pendingCredits() { return creditsState.creditLoading; }
