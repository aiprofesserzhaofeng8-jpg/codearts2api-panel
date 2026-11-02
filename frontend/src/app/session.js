import { on } from '../components/controls';
import { api } from '../api/client';
import { domState } from '../shared/dom';
import { expireSession, setPrincipal } from './authentication';
// Native administrator controls consume the common role session.
export const sessionState = { adminAuthenticated: false };
export function clearStoredPw() {
    try { localStorage.removeItem("codearts2api_pw"); sessionStorage.removeItem("codearts2api_pw"); } catch {}
}
export function showAdminLogin() {
    sessionState.adminAuthenticated = false;
    expireSession();
}
// Reauthenticate after the administrator explicitly changes their password.
export async function adminLogin(password) {
    const result = await api("/auth/login", { method: "POST", body: { username: "admin", password } });
    setPrincipal(result.user);
    sessionState.adminAuthenticated = result.user.role === "admin";
}
export function updateAuthControls() {
    domState.$("#forgetPw").classList.toggle("hidden", !sessionState.adminAuthenticated);
}
export function mountSession() {
    sessionState.adminAuthenticated = true;
    on("#forgetPw", async function () {
        await api("/auth/logout", { method: "POST" });
        showAdminLogin();
    });
    clearStoredPw();
    updateAuthControls();
}
