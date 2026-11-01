import { on } from '../components/controls';
import { isAbortError } from '../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { copyText, domState, toast } from '../shared/dom';
import { api } from '../api/client';
import { resumeLogin } from '../features/accounts/providers/codearts';
import { listen } from '../shared/lifecycle';
import { clearSettingsDirty, renderSettings } from '../features/settings/controller';
import { refresh } from '../shared/refresh';
export const sessionState = {};
sessionState.localPanelMeta = document.querySelector('meta[name="local-panel-session"]');

sessionState.localPanelSession = sessionState.localPanelMeta ? sessionState.localPanelMeta.content : "";

sessionState.adminAuthenticated = false;

export function pw() { return domState.$("#pw").value; }

export function clearStoredPw() {
    try {
        localStorage.removeItem("codearts2api_pw");
        sessionStorage.removeItem("codearts2api_pw");
    }
    catch (e) { }
}

export function showAdminLogin() {
    sessionState.adminAuthenticated = false;
    document.body.classList.add("admin-locked");
    if (!domState.$("#adminLoginDialog").open)
        domState.$("#adminLoginDialog").showModal();
    updateAuthControls();
}

export function adminLogin(password) {
    return api("/admin/auth/login", { method: "POST", body: { password: password } }).then(function () {
        sessionState.adminAuthenticated = true;
        clearStoredPw();
        domState.$("#pw").value = "";
        domState.$("#adminLoginError").textContent = "";
        domState.$("#adminLoginDialog").close();
        document.body.classList.remove("admin-locked");
        updateAuthControls();
    });
}

export function updateAuthControls() {
    domState.$("#localPanelStatus").classList.toggle("hidden", !sessionState.localPanelSession);
    domState.$("#forgetPw").classList.toggle("hidden", !sessionState.adminAuthenticated);
}

export function mountSession() {
on("#savePw", function () {
    return adminLogin(pw()).then(function () { resumeLogin(); return refresh(); }).catch(function (e) { if (isAbortError(e)) return; domState.$("#adminLoginError").textContent = e.message; });
});
on("#forgetPw", function () {
    return api("/admin/auth/logout", { method: "POST" }).then(function () { clearStoredPw(); location.reload(); });
});
listen(domState.$("#adminLoginDialog"), "cancel", function (e) { e.preventDefault(); });
clearStoredPw();
updateAuthControls();
listen(domState.$("#pw"), "keydown", function (e) { if (e.key === "Enter") {
    domState.$("#savePw").click();
} });
}
