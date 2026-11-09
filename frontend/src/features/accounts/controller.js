import { t as tr } from '../../i18n';
import { copyText } from '../../shared/dom';
import { btn, busyable, cell, emptyRow, mk, on, pill, row } from '../../components/controls';
import { serverState } from '../../shared/read-model';
import { isAbortError } from '../../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { domState, fmtDur, fmtTime, toast } from '../../shared/dom';
import { usageSVG } from '../usage/controller';
import { cleanup, listen } from '../../shared/lifecycle';
import { checkin, claim } from '../jobs/welfare';
import { creditCell, openGrowth, openGrowthAccounts, invalidateCredit, refreshAccountCredit } from './credits';
import { api } from '../../api/client';
import { refresh } from '../../shared/refresh';
import { readPreferences, savePreferences, optionalColumns, sortAccounts, runSelected } from './table';
export const accountsState = {};
const preferences = readPreferences();
const selected = new Set();
let visibleAccounts = [], bulkBusy = false, bulkPlan = null, mounted = false, lifetime = 0;
const rewardQueries = new Map();
function refreshCredentialLabel(a) {
    if (a.non_expiring || a.permanent) return tr('无需续期');
    return a.has_refresh === true ? tr('有刷新凭据') : a.has_refresh === false ? tr('无刷新凭据') : tr('刷新凭据未知');
}
const columnNames = { credential: tr('凭据'), reward: tr('奖励'), lastUsed: tr('最近使用') };
function updateSelection() {
    const all = domState.$('#accountSelectAll');
    all.checked = visibleAccounts.length > 0 && visibleAccounts.every(a => selected.has(a.id));
    all.indeterminate = selected.size > 0 && !all.checked;
    domState.$('#accountSelectionCount').textContent = tr('已选择 ') + selected.size;
    domState.$('#accountBulkStart').disabled = bulkBusy || selected.size === 0;
    domState.$('#accountClearSelection').disabled = bulkBusy || selected.size === 0;
    all.disabled = bulkBusy || !visibleAccounts.length;
}
function applyColumns() {
    document.querySelectorAll('.account-table [data-column]').forEach(el => { el.hidden = preferences.hidden.includes(el.dataset.column); });
}
function saveTablePreferences() {
    if (!savePreferences(preferences)) toast(tr('显示偏好仅在本次页面生效'), 'warn');
}

accountsState.accountFilter = "all";

accountsState.accountMenuTrigger = null;

export function closeAccountMenu(restoreFocus) {
    var trigger = accountsState.accountMenuTrigger, menu = domState.$("#accountActionMenu");
    if (menu.matches(":popover-open"))
        menu.hidePopover();
    if (restoreFocus && trigger && trigger.isConnected)
        trigger.focus();
}

export function accountMenuButton(label, account, items) {
    var trigger = btn(label, "xs account-menu-trigger", function (event) {
        event.preventDefault();
        var menu = domState.$("#accountActionMenu"), same = accountsState.accountMenuTrigger === trigger && menu.matches(":popover-open");
        closeAccountMenu(false);
        if (same)
            return;
        menu.textContent = "";
        menu.setAttribute("aria-label", (account.name || tr("未命名账号")) + " · " + label);
        items.forEach(function (item) {
            var action = btn(item.label, "account-menu-item" + (item.danger ? " danger" : ""), function () {
                closeAccountMenu(true);
                return busyable(trigger, async () => { try { await item.run(); } catch (e) { if (!isAbortError(e)) toast(tr('失败：') + e.message, 'err'); } })();
            });
            if (item.danger)
                action.classList.add("account-menu-danger");
            menu.appendChild(action);
        });
        accountsState.accountMenuTrigger = trigger;
        trigger.classList.add("account-menu-open");
        trigger.setAttribute("aria-expanded", "true");
        menu.showPopover({ source: trigger });
        menu.querySelector("button").focus();
    });
    trigger.type = "button";
    trigger.setAttribute("aria-expanded", "false");
    trigger.setAttribute("aria-controls", "accountActionMenu");
    // Associate the invoker so native light-dismiss does not close the menu
    // before this button's click handler decides whether to toggle it shut.
    trigger.popoverTargetElement = domState.$("#accountActionMenu");
    var chevron = usageSVG("svg", { viewBox: "0 0 24 24", class: "ico", "aria-hidden": "true" });
    chevron.appendChild(usageSVG("path", { d: "m6 9 6 6 6-6" }));
    trigger.appendChild(chevron);
    return trigger;
}

export function renderAccounts(d) {
    closeAccountMenu(false);
    var tb = domState.$("#accounts");
    tb.textContent = "";
    var accts = d.accounts || [];
    domState.$("#accountGrowth").classList.toggle("hidden", domState.$("#accountProvider").value !== "workbuddy");
    domState.$("#accountGrowth").disabled = !accts.some(function (a) { return a.provider === "workbuddy" && a.growth_tasks_supported; });
    var counts = { ready: 0, cooling: 0, paused: 0, disabled: 0, busy: 0 };
    var inFlight = 0;
    accts.forEach(function (a) {
        var status = accountState(a);
        counts[status] = (counts[status] || 0) + 1;
        inFlight += a.pool ? a.pool.in_flight || 0 : 0;
    });
    domState.$("#navAccountCount").textContent = accts.length;
    domState.$("#accountCount").textContent = accts.length + tr(" 个账号");
    var stats = domState.$("#poolStats");
    stats.textContent = "";
    [
        { label: tr("账号总数"), value: accts.length },
        { label: tr("可调度"), value: counts.ready, tone: "good" },
        { label: tr("冷却中"), value: counts.cooling, tone: "warning" },
        { label: tr("暂停 / 停用"), value: counts.paused + " / " + counts.disabled },
        { label: tr("在途请求"), value: inFlight },
        { label: tr("调度策略"), value: ({ least_inflight: tr("最少并发"), round_robin: tr("轮询"), sequential_exhaust: tr("顺序耗尽") })[((d.settings || {}).pool || {}).strategy] || "—", text: true }
    ].forEach(function (item) {
        var stat = mk("div", "pool-stat");
        if (item.tone)
            stat.classList.add(item.tone);
        stat.appendChild(mk("div", item.text ? "stat-value text-value" : "stat-value", item.value));
        stat.appendChild(mk("div", "stat-label", item.label));
        stats.appendChild(stat);
    });
    var query = domState.$("#accountSearch").value.trim().toLowerCase();
    var visible = accts.filter(function (a) {
        return (!domState.$("#accountProvider").value || (a.provider || "codearts") === domState.$("#accountProvider").value) && (accountsState.accountFilter === "all" || accountState(a) === accountsState.accountFilter) &&
            (!query || [a.name, a.source, sourceName(a.source), a.provider, a.scope].join(" ").toLowerCase().indexOf(query) !== -1);
    });
    visible = sortAccounts(visible, preferences.sort);
    visibleAccounts = visible;
    // Filtering drops hidden selections instead of silently operating on hidden accounts.
    const visibleIDs = new Set(visible.map(a => a.id));
    selected.forEach(id => { if (!visibleIDs.has(id)) selected.delete(id); });
    updateSelection();
    domState.$("#accountFilterCount").textContent = visible.length + " / " + accts.length + tr(" 个账号");
    if (!accts.length) {
        tb.appendChild(emptyRow(7, tr("还没有账号。点击「浏览器登录」，复制链接授权后确认添加。")));
        return;
    }
    if (!visible.length) {
        tb.appendChild(emptyRow(7, tr("没有符合条件的账号，请更换筛选条件。")));
        return;
    }
    visible.forEach(function (a) {
        var statusCell = cell("");
        const state = accountState(a);
        const labels = { ready: tr('可调度'), paused: tr('已暂停选号'), cooling: tr('冷却中'), busy: tr('并发已满'), disabled: tr('已停用'), unknown: tr('待检查') };
        statusCell.appendChild(pill(labels[state] || tr('待检查'), state === 'ready' ? 'ok' : 'warn'));
        if (a.pool?.cooldown_remaining_sec > 0) statusCell.appendChild(mk('div', 'sub', tr('剩余 ') + fmtDur(a.pool.cooldown_remaining_sec)));
        if (a.last_err) statusCell.appendChild(mk('div', 'sub err-text account-last-error', a.last_err));
        else if (a.status === 'error') statusCell.appendChild(mk('div', 'sub err-text', tr('最近检查失败')));
        if (a.failures > 0) statusCell.appendChild(mk('div', 'sub', tr('失败次数：') + a.failures));
        statusCell.title = tr('调度状态来自账号池，不代表所有模型或额度窗口均可用') + (a.cooldown_reason ? ' · ' + a.cooldown_reason : '');
        if (a.pool)
            statusCell.appendChild(mk("div", "sub", tr("在途 ") + a.pool.in_flight + " / " + a.pool.max_in_flight));
        statusCell.title = (a.max_in_flight > 0 ? tr("独立并发上限：") + a.max_in_flight : tr("并发上限使用全局默认值")) + (statusCell.title ? "；" + statusCell.title : "");
        var expCell = cell("", "cell-stack");
        if (a.non_expiring) {
            expCell.appendChild(mk("span", "mono", tr("长期密钥")));
            expCell.appendChild(mk("span", "sub", tr("有效性以上游为准")));
        }
        else if (a.permanent) {
            // 永久 AK/SK：没有过期时间，也不参与续期。
            expCell.appendChild(mk("span", "mono", tr("永不过期")));
            expCell.appendChild(mk("span", "sub", "IAM AK/SK"));
        }
        else if (a.expires_at) {
            expCell.title = fmtTime(a.expires_at);
            var left = mk('span', 'mono', a.expires_in <= 0 ? tr('已过期') : fmtDur(a.expires_in) + tr('后到期'));
            if (a.expires_in !== undefined && a.expires_in <= 300)
                left.classList.add("warn-text");
            expCell.appendChild(left);
        }
        else {
            expCell.appendChild(mk("span", "mono", "—"));
        }
        expCell.appendChild(mk('span', 'sub', refreshCredentialLabel(a)));
        var claimCell = cell("", "cell-stack");
        const queried = rewardQueries.get(a.id);
        const rewardState = queried && Date.now() - queried.at < 300000 ? queried.state : null;
        if (a.daily_claim_supported === false) {
            claimCell.appendChild(mk("span", "muted", tr("未提供签到接口")));
        }
        else if (rewardState === 'claimed' || rewardState === 'already_claimed' || (!rewardState && a.claimed_today)) {
            claimCell.appendChild(pill(tr("已领"), "ok"));
        }
        else if (rewardState === 'claimable') claimCell.appendChild(pill(tr('可领取'), 'ok'));
        else {
            claimCell.appendChild(pill(({ unavailable: tr("暂不可领"), unknown: tr("待确认"), failed: tr("领取失败") })[rewardState || a.last_claim_state] || tr("待查询"), "warn"));
        }
        if (a.last_claim_msg)
            claimCell.title = a.last_claim_msg;
        if (a.last_claim_err)
            claimCell.title = a.last_claim_err;
        if (queried && rewardState) claimCell.appendChild(mk('span', 'sub', tr('查询于 ') + fmtTime(queried.at / 1000)));
        var ops = cell("", "actions-cell"), actions = mk("div", "account-actions");
        actions.appendChild(btn(tr("详情"), "xs", function () { openAccountDetail(a); }));
        actions.appendChild(btn(tr("探活"), "xs", function () { return checkin(a.id); }));
        if (rewardState === 'claimable' && a.daily_claim_supported !== false)
            actions.appendChild(btn(a.provider && a.provider !== "codearts" ? tr("每日签到") : tr("领福利"), "xs", async function () { rewardQueries.delete(a.id); await claim(a.id); }));
        var more = [
            { label: tr('并发设置'), run: () => openAccountConcurrency(a) },
            { label: tr('模型健康'), run: () => openModelHealth(a) },
            { label: tr('刷新额度'), run: () => refreshAccountCredit(a.id) },
            { label: tr("导出"), run: function () { return exportSession(a.id); } },
            { label: a.enabled ? tr("停用") : tr("启用"), run: function () { return accountOp("toggle", { id: a.id }); } },
            { label: a.paused ? tr("恢复选号") : tr("暂停选号"), run: function () { return accountOp(a.paused ? "resume" : "pause", { id: a.id }); } }
        ];
        if (a.daily_claim_supported !== false) {
            more.unshift({ label: tr('查询奖励'), run: () => queryRewards(a) });
            more.push({ label: tr('领取可领奖励'), run: async () => { rewardQueries.delete(a.id); await claim(a.id); } });
        }
        if (a.growth_tasks_supported)
            more.unshift({ label: tr("成长任务"), run: function () { return openGrowth(a); } });
        if (a.source === "desktop")
            more.push({ label: tr("重读"), run: function () { return accountOp("reimport", { id: a.id }); } });
        if (a.failures > 0 || (a.pool && a.pool.cooldown_remaining_sec > 0) || Object.keys(a.model_health || {}).some(function (m) { return a.model_health[m].failures > 0; }))
            more.push({ label: tr("解除冷却 / 清错"), run: function () { return accountOp("clear-errors", { id: a.id }); } });
        more.push({ label: tr("删除"), danger: true, run: function () { showDeleteAccount(a); } });
        actions.appendChild(accountMenuButton(tr("更多"), a, more));
        if (bulkBusy) actions.querySelectorAll('button').forEach(b => { b.disabled = true; });
        ops.appendChild(actions);
        var identity = cell("", "account-identity");
        var who = mk("div", "account-who");
        const select = document.createElement('input'); select.type = 'checkbox'; select.checked = selected.has(a.id); select.disabled = bulkBusy;
        select.setAttribute('aria-label', tr('选择账号：') + (a.name || a.id));
        select.onchange = () => { if (select.checked) selected.add(a.id); else selected.delete(a.id); updateSelection(); };
        who.appendChild(select);
        who.appendChild(mk("span", "account-avatar", Array.from(a.name || tr("码"))[0].toUpperCase()));
        var info = mk("div", "account-name");
        info.appendChild(mk("strong", "account-title", a.name || tr("未命名账号")));
        info.appendChild(pill(a.provider === "teleagent" ? "TeleAgent · " + tr("国内") : a.provider === "chatgpt" ? "ChatGPT / Codex" : a.provider === "claude" ? "Claude" : a.provider === "kiro" ? "Kiro · " + a.scope : a.provider === "qoder" ? "Qoder · " + (a.scope === "global" ? tr("国际") : tr("国内")) : a.provider === "trae" ? tr("Trae · 国内") : (a.provider === "workbuddy" ? "WorkBuddy · " + (a.scope === "global" ? tr("国际") : tr("国内")) : tr("华为云码道"))));
        info.appendChild(mk("span", "account-caption", sourceName(a.source)));
        who.appendChild(info);
        identity.appendChild(who);
        const quota = creditCell(a);
        quota.appendChild(btn(tr('额度详情'), 'xs ghost', () => openAccountDetail(a)));
        var r = row([identity, expCell, quota, statusCell, claimCell, cell(fmtTime(a.last_used), 'muted nowrap'), ops]);
        r.dataset.state = accountState(a);
        r.dataset.accountId = a.id;
        const columns = ['identity', 'credential', 'quota', 'status', 'reward', 'lastUsed', 'actions'];
        const labelsForCells = [tr('账号 / 平台'), tr('凭据'), tr('额度与用量'), tr('调度 / 并发'), tr('奖励'), tr('最近使用'), tr('操作')];
        Array.from(r.children).forEach(function (td, i) { td.dataset.label = labelsForCells[i]; td.dataset.column = columns[i]; });
        if (a.last_err)
            r.title = a.last_err;
        tb.appendChild(r);
    });
    applyColumns();
}

export function accountState(a) {
    if (!a.enabled)
        return "disabled";
    if (a.paused)
        return "paused";
    return a.pool?.state || "unknown";
}

export function sourceName(source) { return ({ browser: tr("浏览器登录"), desktop: tr("桌面端导入"), manual: tr("手工录入"), session: tr("会话导入") })[source] || source || "—"; }

export function accountOp(action, extra) {
    var body = { action: action };
    if (extra)
        for (var k in extra)
            body[k] = extra[k];
    return api("/admin/api/accounts", { method: "POST", body: body }).then(function () {
        toast(tr("操作成功"), "ok");
        return refresh();
    }).catch(function (e) { if (isAbortError(e)) return; toast(tr("失败：") + e.message, "err"); });
}

export function exportSession(id) {
    toast(tr("导出中…"));
    return api("/admin/api/accounts", { method: "POST", body: { action: "export", id: id } }).then(function (r) {
        domState.$("#exportJSON").value = r.session_json || "";
        domState.$("#exportMeta").textContent = tr("账号 ") + (r.name || "") +
            tr(" · 凭据有效期至 ") + fmtTime(r.expires_at) +
            tr(" · 可续期 ") + (r.has_refresh ? tr("是") : tr("否"));
        domState.$("#exportPanel").classList.remove("hidden");
        toast(tr("已导出（含 refresh_token，请勿外传）"), "ok");
    }).catch(function (e) { if (isAbortError(e)) return; toast(tr("导出失败：") + e.message, "err"); });
}

accountsState.deletingAccount = null;

accountsState.deleteBusy = false;

export function showDeleteAccount(account) {
    accountsState.deletingAccount = account;
    domState.$("#deleteAccountName").textContent = account.name || account.id;
    domState.$("#deleteAccountError").classList.add("hidden");
    domState.$("#deleteAccountConfirm").disabled = false;
    if (!domState.$("#deleteAccountDialog").open)
        domState.$("#deleteAccountDialog").showModal();
}

accountsState.concurrencyAccountID = "";

accountsState.concurrencySaving = false;

export function openAccountConcurrency(a) {
    accountsState.concurrencyAccountID = a.id;
    var defaults = ((serverState.data || {}).settings || {}).pool || {};
    var fallback = defaults.max_in_flight || 2;
    domState.$("#accountConcurrencyName").textContent = a.name || tr("未命名账号");
    domState.$("#accountConcurrencyDefault").checked = !(a.max_in_flight > 0);
    domState.$("#accountConcurrencyDefaultHint").textContent = tr("当前全局默认值：") + fallback;
    domState.$("#accountConcurrencyLimit").value = a.max_in_flight > 0 ? a.max_in_flight : fallback;
    domState.$("#accountConcurrencyLimit").disabled = domState.$("#accountConcurrencyDefault").checked;
    domState.$("#accountConcurrencyError").textContent = "";
    domState.$("#accountConcurrencyDialog").showModal();
}

export function closeAccountConcurrency() { if (!accountsState.concurrencySaving) {
    domState.$("#accountConcurrencyDialog").close();
    accountsState.concurrencyAccountID = "";
} }

export function openModelHealth(a) {
    var tb = domState.$("#modelHealthRows");
    tb.textContent = "";
    Object.keys(a.model_health || {}).sort().forEach(function (model) { var v = a.model_health[model]; var remaining = Math.max(0, v.cooldown_until - Math.floor(Date.now() / 1000)); tb.appendChild(row([cell(model, "mono"), cell(v.failures), cell(remaining ? tr("冷却 ") + remaining + tr(" 秒") : tr("可调度")), cell(fmtTime(v.updated_at)), cell(v.last_error || "—", "muted")])); });
    if (!tb.children.length)
        tb.appendChild(emptyRow(5, tr("暂无模型请求记录")));
    domState.$("#modelHealthDialog").showModal();
}

export function mountAccounts() {
mounted = true; lifetime++;
cleanup(() => { mounted = false; lifetime++; selected.clear(); rewardQueries.clear(); bulkPlan = null; bulkBusy = false; });
domState.$('#accountSort').value = preferences.sort;
listen(domState.$('#accountSort'), 'change', e => { preferences.sort = e.target.value; saveTablePreferences(); if (serverState.data) renderAccounts(serverState.data); });
const columnOptions = domState.$('#accountColumnOptions'); columnOptions.textContent = '';
optionalColumns.forEach(key => { const label = mk('label', 'check'); const box = document.createElement('input'); box.type = 'checkbox'; box.checked = !preferences.hidden.includes(key);
    box.onchange = () => { preferences.hidden = box.checked ? preferences.hidden.filter(x => x !== key) : [...preferences.hidden, key]; saveTablePreferences(); applyColumns(); };
    label.append(box, document.createTextNode(columnNames[key])); columnOptions.appendChild(label);
});
listen(domState.$('#accountSelectAll'), 'change', e => { visibleAccounts.forEach(a => { if (e.target.checked) selected.add(a.id); else selected.delete(a.id); }); if (serverState.data) renderAccounts(serverState.data); });
on('#accountClearSelection', () => { selected.clear(); if (serverState.data) renderAccounts(serverState.data); });
on('#accountDetailClose', () => domState.$('#accountDetailDialog').close());
on('#accountBulkStart', prepareBulk);
listen(domState.$('#accountBulkConfirm'), 'click', executeBulk);
on('#accountBulkClose', () => { if (!bulkBusy) domState.$('#accountBulkDialog').close(); });
listen(domState.$('#accountBulkDialog'), 'cancel', e => { if (bulkBusy) e.preventDefault(); });

listen(domState.$("#accountActionMenu"), "beforetoggle", function (event) {
    if (event.newState === "closed" && accountsState.accountMenuTrigger) {
        accountsState.accountMenuTrigger.classList.remove("account-menu-open");
        accountsState.accountMenuTrigger.setAttribute("aria-expanded", "false");
        accountsState.accountMenuTrigger = null;
    }
});
listen(domState.$("#accountActionMenu"), "keydown", function (event) {
    if (event.key === "Escape") {
        event.preventDefault();
        closeAccountMenu(true);
        return;
    }
    var buttons = Array.from(this.querySelectorAll("button")), index = buttons.indexOf(document.activeElement);
    if (event.key === "ArrowDown")
        index = (index + 1) % buttons.length;
    else if (event.key === "ArrowUp")
        index = (index - 1 + buttons.length) % buttons.length;
    else if (event.key === "Home")
        index = 0;
    else if (event.key === "End")
        index = buttons.length - 1;
    else
        return;
    event.preventDefault();
    buttons[index].focus();
});
listen(domState.$("#accountActionMenu"), "focusout", function (event) {
    if (event.relatedTarget && !this.contains(event.relatedTarget) && event.relatedTarget !== accountsState.accountMenuTrigger)
        closeAccountMenu(false);
});
listen(domState.$("#accountProvider"), "change", function () { if (serverState.data)
    renderAccounts(serverState.data); });
on("#accountGrowth", function () {
    return openGrowthAccounts(((serverState.data && serverState.data.accounts) || []).filter(function (a) { return a.provider === "workbuddy" && a.growth_tasks_supported; }));
});
listen(domState.$("#accountSearch"), "input", function () { if (serverState.data)
    renderAccounts(serverState.data); });
on("#deleteAccountCancel", function () { if (!accountsState.deleteBusy) {
    domState.$("#deleteAccountDialog").close();
    accountsState.deletingAccount = null;
} });
listen(domState.$("#deleteAccountDialog"), "cancel", function (e) { if (accountsState.deleteBusy)
    e.preventDefault();
else
    accountsState.deletingAccount = null; });
on("#deleteAccountConfirm", function () {
    if (!accountsState.deletingAccount || accountsState.deleteBusy)
        return;
    var id = accountsState.deletingAccount.id;
    accountsState.deleteBusy = true;
    domState.$("#deleteAccountCancel").disabled = true;
    domState.$("#deleteAccountError").classList.add("hidden");
    return api("/admin/api/accounts", { method: "POST", body: { action: "remove", id: id } }).then(function (r) {
        if (!r.ok || r.removed_id !== id)
            throw new Error(tr("删除未完成，请刷新后重试"));
        invalidateCredit(id);
        accountsState.deletingAccount = null;
        domState.$("#deleteAccountDialog").close();
        toast(tr("账号已删除"), "ok");
        return refresh();
    }).catch(function (e) { if (isAbortError(e)) return; domState.$("#deleteAccountError").textContent = e.message; domState.$("#deleteAccountError").classList.remove("hidden"); })
        .finally(function () { accountsState.deleteBusy = false; domState.$("#deleteAccountCancel").disabled = false; });
});
listen(domState.$("#accountConcurrencyDefault"), "change", function () { domState.$("#accountConcurrencyLimit").disabled = this.checked; });
on("#accountConcurrencyClose", closeAccountConcurrency);
on("#accountConcurrencyCancel", closeAccountConcurrency);
listen(domState.$("#accountConcurrencyDialog"), "cancel", function (e) { e.preventDefault(); closeAccountConcurrency(); });
on("#accountConcurrencySave", function () {
    if (accountsState.concurrencySaving || !accountsState.concurrencyAccountID)
        return;
    var useDefault = domState.$("#accountConcurrencyDefault").checked, input = domState.$("#accountConcurrencyLimit"), value = useDefault ? 0 : Number(input.value);
    if (!useDefault && (!input.value.trim() || !input.checkValidity() || !Number.isInteger(value) || value < 1 || value > 32)) {
        domState.$("#accountConcurrencyError").textContent = tr("请填写 1–32 的整数并发上限");
        return;
    }
    accountsState.concurrencySaving = true;
    domState.$("#accountConcurrencyError").textContent = "";
    ["#accountConcurrencyDefault", "#accountConcurrencyLimit", "#accountConcurrencyCancel", "#accountConcurrencyClose"].forEach(function (id) { domState.$(id).disabled = true; });
    var id = accountsState.concurrencyAccountID;
    return api("/admin/api/accounts", { method: "POST", body: { action: "concurrency", id: id, max_in_flight: value } }).then(function (r) {
        if (!r.ok || r.id !== id || r.max_in_flight !== value)
            throw new Error(tr("保存结果未确认，请刷新账号列表核对"));
        domState.$("#accountConcurrencyDialog").close();
        accountsState.concurrencyAccountID = "";
        toast(tr("并发设置已保存"), "ok");
        return refresh();
    }).catch(function (e) { if (isAbortError(e)) return; domState.$("#accountConcurrencyError").textContent = tr("保存失败：") + e.message; }).finally(function () {
        accountsState.concurrencySaving = false;
        ["#accountConcurrencyDefault", "#accountConcurrencyCancel", "#accountConcurrencyClose"].forEach(function (id) { domState.$(id).disabled = false; });
        domState.$("#accountConcurrencyLimit").disabled = domState.$("#accountConcurrencyDefault").checked;
    });
});
on("#modelHealthClose", function () { domState.$("#modelHealthDialog").close(); });
on("#copyBaseURL", function () { return copyText(location.origin + "/v1", tr("API 地址已复制")); });
domState.$("#apiBaseURL").textContent = location.origin + "/v1";
on("#copyExport", function () {
    return copyText(domState.$("#exportJSON").value, tr("已复制到剪贴板（注意不要外传）"));
});
on("#closeExport", function () {
    domState.$("#exportJSON").value = "";
    domState.$("#exportPanel").classList.add("hidden");
});
}

async function queryRewards(a) {
    const r = await api('/admin/api/welfare?id=' + encodeURIComponent(a.id));
    if (r.id !== a.id || !Array.isArray(r.campaigns)) throw new Error(tr('查询结果未确认'));
    const state = r.daily_status || (r.claimable_count > 0 ? 'claimable' : 'unknown');
    rewardQueries.set(a.id, { state, at: Date.now() });
    if (serverState.data) renderAccounts(serverState.data);
    return { state, message: r.message || tr('奖励查询完成') };
}

function openAccountDetail(a) {
    const body = domState.$('#accountDetailBody'); body.textContent = '';
    body.appendChild(mk('h3', '', a.name || a.id));
    const fields = [
        [tr('来源'), sourceName(a.source)], [tr('凭据有效期'), a.expires_at ? fmtTime(a.expires_at) : a.permanent || a.non_expiring ? tr('长期密钥') : tr('未知')],
        [tr('刷新凭据'), refreshCredentialLabel(a)],
        [tr('最近使用'), fmtTime(a.last_used)], [tr('失败次数'), a.failures || 0],
        [tr('最近错误'), a.last_err || '—'], [tr('冷却原因'), a.cooldown_reason || '—'],
        [tr('最近奖励记录'), a.last_claim_msg || '—'], [tr('奖励记录时间'), fmtTime(a.last_claim_at)], [tr('奖励错误'), a.last_claim_err || '—']
    ];
    fields.forEach(([label, value]) => { const p = mk('p', 'account-detail-field'); p.append(mk('strong', '', label + ': '), document.createTextNode(value)); body.appendChild(p); });
    body.appendChild(mk('p', 'hint', tr('刷新凭据的存在不代表自动续期已启用或续期成功。额度快照不代表所有模型均可调用。')));
    const quota = mk('div', 'cell-stack'); const rendered = creditCell(a, false); quota.append(...Array.from(rendered.childNodes)); body.appendChild(quota);
    if (!domState.$('#accountDetailDialog').open) domState.$('#accountDetailDialog').showModal();
}

function prepareBulk() {
    if (bulkBusy) return;
    const targets = visibleAccounts.filter(a => selected.has(a.id)).map(a => ({ ...a }));
    if (!targets.length) return;
    const control = domState.$('#accountBulkAction');
    bulkPlan = { action: control.value, targets };
    domState.$('#accountBulkSummary').textContent = control.selectedOptions[0].textContent + ' · ' + targets.length + tr(' 个账号');
    const list = domState.$('#accountBulkTargets'); list.textContent = '';
    targets.forEach(a => list.appendChild(mk('li', '', (a.name || a.id) + ' · ' + (a.provider || 'codearts'))));
    domState.$('#accountBulkResults').textContent = '';
    domState.$('#accountBulkConfirm').disabled = false;
    domState.$('#accountBulkConfirm').classList.toggle('danger', control.value === 'remove');
    domState.$('#accountBulkDialog').showModal();
}

async function executeBulk() {
    if (bulkBusy || !bulkPlan) return;
    const { action, targets } = bulkPlan; bulkPlan = null; bulkBusy = true;
    domState.$('#accountBulkConfirm').disabled = true;
    domState.$('#accountBulkClose').disabled = true;
    ['#checkinAll', '#claimAll', '#refreshCredits'].forEach(id => { domState.$(id).disabled = true; });
    if (serverState.data) renderAccounts(serverState.data);
    const currentLifetime = lifetime;
    const active = () => mounted && lifetime === currentLifetime;
    let completed = 0;
    try {
        await runSelected(targets, async a => {
            // Account disappearance never becomes an unscoped all-account request.
            if (!a.id || !(serverState.data?.accounts || []).some(current => current.id === a.id)) throw new Error(tr('账号已不存在'));
            if (action === 'credits') { await refreshAccountCredit(a.id); return tr('额度已刷新'); }
            if (action === 'rewards' || action === 'claim') {
                if (a.daily_claim_supported === false) return tr('已跳过：未提供签到接口');
                const reward = await queryRewards(a);
                if (action === 'rewards') return reward.message;
                if (reward.state !== 'claimable') return tr('已跳过：当前未确认可领取') + ' · ' + reward.message;
            }
            if (action === 'probe' || action === 'claim') {
                const r = await api(action === 'probe' ? '/admin/api/checkin' : '/admin/api/claim', { method: 'POST', body: { id: a.id } });
                const entry = (r.results || []).find(x => x.id === a.id);
                if (!entry || !entry.ok) throw new Error(entry?.err || entry?.message || tr('操作结果未确认'));
                if (action === 'claim') { rewardQueries.delete(a.id); invalidateCredit(a.id); }
                return entry.message || tr('操作成功');
            }
            if (!['pause', 'resume', 'clear-errors', 'remove'].includes(action)) throw new Error(tr('未知操作'));
            const r = await api('/admin/api/accounts', { method: 'POST', body: { action, id: a.id } });
            if (!r.ok || (action === 'remove' && r.removed_id !== a.id)) throw new Error(tr('操作结果未确认'));
            if (action === 'remove') { invalidateCredit(a.id); selected.delete(a.id); }
            return tr('操作成功');
        }, result => {
            completed++;
            if (!active()) return;
            domState.$('#accountBulkResults').appendChild(mk('p', result.ok ? 'ok-text' : 'err-text', result.name + ': ' + result.message));
        }, active);
        if (active()) {
            domState.$('#accountBulkResults').appendChild(mk('p', 'hint', tr('已处理 ') + completed + ' / ' + targets.length));
            await refresh();
        }
    } catch (e) {
        if (active()) domState.$('#accountBulkResults').appendChild(mk('p', 'err-text', tr('刷新列表失败：') + e.message));
    } finally {
        if (active()) {
            bulkBusy = false;
            domState.$('#accountBulkConfirm').disabled = true;
            domState.$('#accountBulkClose').disabled = false;
            ['#checkinAll', '#claimAll', '#refreshCredits'].forEach(id => { domState.$(id).disabled = false; });
            if (serverState.data) renderAccounts(serverState.data);
        }
    }
}
