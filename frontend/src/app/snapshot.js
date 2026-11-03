import { t as tr } from '../i18n';
import { serverState } from '../shared/read-model';
import { isAbortError } from '../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { api } from '../api/client';
import { renderGroups } from '../features/groups/controller';
import { renderOverview } from '../features/models/controller';
import { domState, toast } from '../shared/dom';
import { loadPricing } from '../features/models/pricing';
import { loadModelPolicies } from '../features/models/policies';
import { refreshUsageLogs } from '../features/usage/controller';
import { renderAccounts } from '../features/accounts/controller';
import { renderKeys } from '../features/keys/controller';
import { renderSettings } from '../features/settings/controller';
import { loadAudit } from '../features/logs/controller';
import { loadHealth } from './shell';
import { loadCredits } from '../features/accounts/credits';
import { sessionState, showAdminLogin } from './session';



export function refresh() {
    return api("/admin/api/state").then(function (d) {
        serverState.data = d;
        if (domState.$("#tab-users").classList.contains("on")) window.dispatchEvent(new Event("users-refresh"));
        renderGroups(d);
        renderOverview(d);
        if (domState.$("#tab-models").classList.contains("on")) {
            if (!domState.$("#pricingRows").children.length)
                loadPricing();
            if (!domState.$("#modelPolicies").children.length)
                loadModelPolicies();
        }
        if (domState.$("#tab-overview").classList.contains("on"))
            refreshUsageLogs();
        renderAccounts(d);
        renderKeys(d);
        renderSettings(d.settings || {});
        if (domState.$("#tab-audit").classList.contains("on"))
            loadAudit();
        loadHealth();
        // 账号页可见时补一次额度（首次进页签时 refresh 可能还没回来）
        if (domState.$("#tab-accounts").classList.contains("on"))
            loadCredits(false);
    }).catch(function (e) { if (isAbortError(e)) return;
        if (e.status === 401) {
            showAdminLogin();
        }
        else if (e.status === 403) {
            toast(tr("管理接口拒绝访问：") + e.message, "err");
        }
        else {
            toast(tr("加载失败：") + e.message, "err");
        }
    });
}

export function mountSnapshot() {

}
