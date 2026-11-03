import { t as tr } from '../i18n';
import { serverState } from '../shared/read-model';
import { cancelPageQueries } from '../api/client';
import { every } from '../shared/lifecycle';
import { isAbortError } from '../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { closeAccountMenu } from '../features/accounts/controller';
import { renderKeys } from '../features/keys/controller';
import { domState } from '../shared/dom';
import { sessionState, showAdminLogin, updateAuthControls } from './session';
import { loadAudit, refreshLogs } from '../features/logs/controller';
import { refreshUsageLogs } from '../features/usage/controller';
import { loadPricing } from '../features/models/pricing';
import { loadModelPolicies } from '../features/models/policies';
import { loadCredits } from '../features/accounts/credits';
import { listen } from '../shared/lifecycle';
import { loadHealth } from './shell';
import { api } from '../api/client';
import { resumeLogin } from '../features/accounts/providers/codearts';
import { refresh } from '../shared/refresh';
export const routesState = {};
routesState.TAB_IDS = { accounts: 1, groups: 1, users: 1, overview: 1, models: 1, keys: 1, logs: 1, audit: 1, settings: 1 };

routesState.TAB_META = {
    overview: { title: tr("用量"), hint: tr("请求、Token 与耗时统计"), doc: tr("用量") },
    models: { title: tr("模型"), hint: tr("模型目录、积分倍率与可用性"), doc: tr("模型") },
    accounts: { title: tr("账号池"), hint: tr("账号状态、额度与调度"), doc: tr("账号池") },
    groups: { title: tr("账号分组"), hint: tr("账号与模型访问范围"), doc: tr("账号分组") },
    users: { title: tr("用户管理"), hint: tr("用户、分组权限与共享预算"), doc: tr("用户管理") },
    keys: { title: tr("API 密钥"), hint: tr("客户端接入与密钥管理"), doc: tr("API 密钥") },
    logs: { title: tr("运行日志"), hint: tr("请求记录与错误排查"), doc: tr("运行日志") },
    audit: { title: tr("管理审计"), hint: tr("管理操作、登录与敏感信息访问记录"), doc: tr("管理审计") },
    settings: { title: tr("配置"), hint: tr("调度策略与运行参数"), doc: tr("配置") }
};

// activateTab 是唯一的页签切换入口：同步 class / aria-selected / URL hash。
// warm=false 时只切显示、不触发额外请求（首次加载用）。
export function activateTab(name, updateHash, warm) {
    cancelPageQueries();
    closeAccountMenu(false);
    if (!routesState.TAB_IDS[name])
        return;
    if (name !== "keys" && serverState.data)
        renderKeys(serverState.data);
    document.querySelectorAll('#adminNavigation button[data-tab]').forEach(function (x) {
        var sel = x.dataset.tab === name;
        x.classList.toggle("on", sel);
        x.setAttribute("aria-selected", sel ? "true" : "false");
        x.tabIndex = sel ? 0 : -1;
    });
    document.querySelectorAll("section[role=tabpanel]").forEach(function (x) {
        x.classList.toggle("on", x.id === "tab-" + name);
    });
    var meta = routesState.TAB_META[name];
    domState.$("#pageTitle").textContent = meta.title;
    domState.$("#pageHint").textContent = meta.hint;
    document.title = meta.doc + tr(" · CodeArts2API 控制台");
    if (updateHash) {
        try {
            history.replaceState(null, "", "#" + name);
        }
        catch {
            location.hash = name;
        }
    }
    if (warm === false || (!sessionState.adminAuthenticated))
        return;
    if (name === "audit")
        loadAudit();
    if (name === "users")
        window.dispatchEvent(new Event("users-refresh"));
    if (name === "logs")
        refreshLogs();
    if (name === "overview")
        refreshUsageLogs();
    if (name === "models") {
        loadPricing();
        loadModelPolicies();
    }
    if (name === "accounts")
        loadCredits(false);
}

routesState.initialTab = location.hash.slice(1);

export function mountRoutes() {
document.querySelectorAll('#adminNavigation button[data-tab]').forEach(function (b) {
    listen(b, "click", function () { activateTab(b.dataset.tab, true); });
    // WAI-ARIA tabs：左右方向键切换
    listen(b, "keydown", function (e) {
        var keys = Object.keys(routesState.TAB_IDS);
        var i = keys.indexOf(b.dataset.tab);
        if (e.key === "ArrowRight") {
            i = (i + 1) % keys.length;
        }
        else if (e.key === "ArrowLeft") {
            i = (i - 1 + keys.length) % keys.length;
        }
        else
            return;
        e.preventDefault();
        activateTab(keys[i], true);
        document.querySelector('#adminNavigation button[data-tab="' + keys[i] + '"]').focus();
    });
});
listen(window, "hashchange", function () {
    activateTab(location.hash.slice(1), false);
});
routesState.initialTab = routesState.TAB_IDS[location.hash.slice(1)] ? location.hash.slice(1) : "accounts";
activateTab(routesState.initialTab, false, true);
resumeLogin();
refresh();
every(function () { if (sessionState.adminAuthenticated)
    loadHealth(); }, 20000);
}
