import { on } from '../components/controls';
import { api } from '../api/client';
import { isAbortError } from '../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { domState, setHealth } from '../shared/dom';
export const shellState = {};
export function updateThemeLabel() {
    var light = document.documentElement.dataset.theme === "light";
    domState.$("#themeLabel").textContent = light ? "深色" : "浅色";
    domState.$("#themeToggle").title = light ? "切换到深色主题" : "切换到浅色主题";
}

export function loadHealth() {
    api("/health").then(function (h) {
        var ready = h.ready || 0, total = h.accounts || 0;
        var ok = total > 0 && ready > 0;
        setHealth(ok ? "ok" : (total === 0 ? "warn" : "err"), "账号 " + ready + "/" + total + " 就绪 · 模型 " + (h.models || 0));
    }).catch(function (error) { if (isAbortError(error)) return; setHealth("err", "不可达"); });
}

export function mountShell() {
// 顶栏密码框折叠：默认只露一个锁图标，点开才展开输入框。
on("#themeToggle", function () {
    var theme = document.documentElement.dataset.theme === "light" ? "dark" : "light";
    document.documentElement.dataset.theme = theme;
    try {
        localStorage.setItem("codearts2api_theme", theme);
    }
    catch (e) { }
    updateThemeLabel();
});
}
