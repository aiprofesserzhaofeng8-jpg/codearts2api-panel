import { t as tr } from '../i18n';
import { api } from '../api/client';
import { isAbortError } from '../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { setHealth } from '../shared/dom';
export const shellState = {};
export function loadHealth() {
    api("/health").then(function (h) {
        var ready = h.ready || 0, total = h.accounts || 0;
        var ok = total > 0 && ready > 0;
        setHealth(ok ? "ok" : (total === 0 ? "warn" : "err"), tr("账号 ") + ready + "/" + total + tr(" 就绪 · 模型 ") + (h.models || 0));
    }).catch(function (error) { if (isAbortError(error)) return; setHealth("err", tr("不可达")); });
}
