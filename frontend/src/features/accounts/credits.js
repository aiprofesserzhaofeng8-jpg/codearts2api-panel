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
export function creditCell(a, compact = true) {
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
    if (appendQuota(c, info, Date.now() / 1000, compact)) return c;
    if (!info.available) {
        var accessDenied = a.status === "error" && String(a.last_err || "").indexOf("TM.00001005") >= 0;
        var none = mk("span", "muted", accessDenied ? tr("需开通码道权限") : (info.is_token_package ? tr("Token 套餐") : tr("额度未返回")));
        none.title = accessDenied ? tr("最近一次模型调用被上游拒绝：尚未获得此功能的访问权限。请使用该账号到码道官方页面核对开通状态。") : (info.reason || tr("上游未提供积分数据，不能据此判定没有套餐或余额"));
        c.appendChild(none);
        return c;
    }
    const known = value => typeof value === 'number' && Number.isFinite(value) && value >= 0;
    const total = known(info.total) ? info.total : null;
    const remain = known(info.remaining) ? info.remaining : null;
    const used = known(info.used) ? info.used : total !== null && remain !== null ? Math.max(0, total - remain) : null;
    c.appendChild(mk('span', 'mono', remain !== null ? tr('剩余 ') + fmtNum(remain) + (total !== null ? ' / ' + fmtNum(total) : '') : tr('用量未知')));
    const pct = total > 0 && used !== null ? Math.max(0, Math.min(100, used / total * 100)) : null;
    if (pct !== null) {
        const bar = mk('div', 'bar credit-bar'); const fill = document.createElement('progress');
        fill.max = 100; fill.value = pct; fill.setAttribute('aria-label', tr('已用 ') + pct.toFixed(0) + '%');
        if (pct >= 95) bar.classList.add('err'); else if (pct >= 80) bar.classList.add('warn');
        bar.appendChild(fill); c.appendChild(bar);
    }
    var bits = used === null ? [] : [tr('已用 ') + fmtNum(used) + (pct !== null ? ' · ' + pct.toFixed(1) + '%' : '')];
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

// Single-account refresh for explicit selected-account actions, without expanding scope.
export async function refreshAccountCredit(id) {
    await pendingCredits();
    const r = await api('/admin/api/credits?id=' + encodeURIComponent(id) + '&fresh=1');
    const entry = (r.accounts || []).find(x => x.id === id);
    if (!entry) throw new Error(tr('查询结果未确认'));
    creditsState.creditState[id] = entry.ok ? { ok: true, credit: entry.credit } : { ok: false, error: entry.error };
    if (serverState.data) renderAccounts(serverState.data);
    if (!entry.ok) throw new Error(entry.error || tr('查询失败'));
}
