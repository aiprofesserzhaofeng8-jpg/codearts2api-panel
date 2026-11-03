import { t as tr } from '../../../i18n';
import { on } from '../../../components/controls';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { copyText, domState, fmtTime, toast } from '../../../shared/dom';
import { finishAccountCreation, updateAccountCreateBusy } from './controller';
import { api } from '../../../api/client';
import { refresh } from '../../../shared/refresh';
export const codeartsState = {};
codeartsState.loginSid = null;

codeartsState.loginDraft = null;

codeartsState.loginPreview = null;

codeartsState.loginBusy = false;

codeartsState.LOGIN_KEY = "codearts2api_login_flow";

export function stopLogin() {
    codeartsState.loginSid = null;
    codeartsState.loginDraft = null;
    codeartsState.loginPreview = null;
    try {
        sessionStorage.removeItem(codeartsState.LOGIN_KEY);
    }
    catch (e) { }
}

export function rememberLogin(r) {
    codeartsState.loginDraft = { session_id: r.session_id, auth_url: r.auth_url, callback: r.callback,
        tunnel_hint: r.tunnel_hint, expires_at: Date.now() + (r.expires_in || 300) * 1000 };
    try {
        sessionStorage.setItem(codeartsState.LOGIN_KEY, JSON.stringify(codeartsState.loginDraft));
    }
    catch (e) { }
}

export function resumeLogin() {
    var saved;
    try {
        saved = JSON.parse(sessionStorage.getItem(codeartsState.LOGIN_KEY) || "null");
    }
    catch (e) { }
    if (!saved || !saved.session_id || saved.expires_at <= Date.now()) {
        stopLogin();
        return;
    }
    if (!isTrustedAuthURL(saved.auth_url)) {
        stopLogin();
        return;
    }
    codeartsState.loginDraft = saved;
    codeartsState.loginSid = saved.session_id;
}

export function drawLogin() {
    var active = !!codeartsState.loginDraft;
    domState.$("#loginInitial").classList.toggle("hidden", active);
    domState.$("#loginDetails").classList.toggle("hidden", !active);
    domState.$("#loginCancel").classList.toggle("hidden", !active);
    domState.$("#loginRestart").classList.toggle("hidden", !active);
    domState.$("#loginPreview").classList.toggle("hidden", !codeartsState.loginPreview);
    domState.$("#loginComplete").classList.toggle("hidden", !codeartsState.loginPreview);
    if (active)
        setLoginURL(codeartsState.loginDraft.auth_url, codeartsState.loginDraft.callback, codeartsState.loginDraft.tunnel_hint);
    var step = codeartsState.loginPreview ? 3 : active ? 2 : 1;
    ["loginStep1", "loginStep2", "loginStep3"].forEach(function (id, i) { domState.$("#" + id).classList.toggle("active", i + 1 === step); });
}

export function setLoginBusy(busy) {
    codeartsState.loginBusy = busy;
    updateAccountCreateBusy();
    ["loginGenerate", "loginSubmit", "loginCheck", "loginCancel", "loginRestart", "loginComplete"].forEach(function (id) { domState.$("#" + id).disabled = busy; });
}

export function loginFailure(e) { domState.$("#loginError").textContent = e.message || String(e); domState.$("#loginError").classList.remove("hidden"); }

export function openLogin() {
    domState.$("#loginError").classList.add("hidden");
    drawLogin();
    domState.$("#loginPanel").classList.remove("hidden");
    if (codeartsState.loginSid && !codeartsState.loginBusy) {
        setLoginBusy(true);
        return pollLogin().catch(loginFailure).finally(function () { setLoginBusy(false); });
    }
}

export function generateLogin() {
    if (codeartsState.loginBusy)
        return;
    setLoginBusy(true);
    domState.$("#loginError").classList.add("hidden");
    return api("/admin/api/login/start", { method: "POST", body: {} }).then(function (r) {
        if (!isTrustedAuthURL(r.auth_url))
            throw new Error(tr("授权地址无效"));
        codeartsState.loginSid = r.session_id;
        codeartsState.loginPreview = null;
        rememberLogin(r);
        drawLogin();
        domState.$("#loginPasted").value = "";
        domState.$("#loginStatus").textContent = tr("链接已生成。请复制到另一浏览器完成登录，再返回提交授权信息。");
    }).catch(loginFailure).finally(function () { setLoginBusy(false); });
}

export function pollLogin() {
    if (!codeartsState.loginSid)
        return Promise.reject(new Error(tr("请先生成登录链接")));
    return api("/admin/api/login/poll", { method: "POST", body: { session_id: codeartsState.loginSid } }).then(function (r) {
        if (r.saved) {
            stopLogin();
            drawLogin();
            domState.$("#loginStatus").textContent = tr("本次登录已保存，可关闭窗口。");
            return refresh();
        }
        if (r.status === "confirmed" && r.ready) {
            domState.$("#loginError").textContent = "";
            domState.$("#loginError").classList.add("hidden");
            codeartsState.loginPreview = r;
            domState.$("#loginPasted").value = "";
            domState.$("#loginAccountName").textContent = r.account_name || r.account || tr("码道账号");
            domState.$("#loginAccountInfo").textContent = (r.domain_id ? tr("账号标识：") + r.domain_id + " · " : "") + (r.has_refresh ? tr("可自动续期") : tr("临时凭据，到期需重新登录")) + tr(" · 有效期至 ") + fmtTime(r.expires_at);
            domState.$("#loginReplaceLabel").classList.toggle("hidden", !r.existing);
            domState.$("#loginReplace").checked = false;
            domState.$("#loginStatus").textContent = tr("授权已验证，尚未保存。请核对账号，再点击「确认完成登录」。");
        }
        else {
            codeartsState.loginPreview = null;
            domState.$("#loginStatus").textContent = r.message || tr("尚未收到授权，请在另一浏览器登录后重试。");
            if (["failed", "expired", "canceled"].indexOf(r.status) >= 0)
                loginFailure(new Error((r.message || tr("本次授权未完成")) + tr("。请重新生成登录链接。")));
        }
        drawLogin();
    });
}

// isTrustedAuthURL 只允许华为云门户 / 本机回环的授权地址。
//
// 授权 URL 由网关自己拼出来（/admin/api/login/start），理论上不可注入；
// 但这里是唯一一处「拿后端返回的 URL 去跳转」的地方，加一道白名单成本很低，
// 可以挡住管理密码泄露、或将来某个 bug 让 portal_host 变成可控的情况。
export function isTrustedAuthURL(raw) {
    var u;
    try {
        u = new URL(raw);
    }
    catch {
        return false;
    }
    if (u.protocol !== "https:" && u.protocol !== "http:")
        return false;
    var host = u.hostname.toLowerCase();
    if (u.protocol === "http:") {
        // 明文 http 只允许本机（本地联调）
        return host === "127.0.0.1" || host === "localhost" || host === "::1";
    }
    return host === "huaweicloud.com" || host.endsWith(".huaweicloud.com")
        || host === "huaweicloud.cn" || host.endsWith(".huaweicloud.cn")
        || host === "huawei.com" || host.endsWith(".huawei.com");
}

// setLoginURL 把授权地址写进只读输入框，并只在通过白名单时才挂到链接上。
export function setLoginURL(raw, callback, tunnel) {
    var ok = isTrustedAuthURL(raw);
    domState.$("#loginURL").value = raw || "";
    domState.$("#loginCallback").textContent = callback || "—";
    domState.$("#loginTunnel").value = tunnel || "";
    var a = domState.$("#loginLink");
    if (ok) {
        a.href = raw;
        a.classList.remove("hidden");
    }
    else {
        a.removeAttribute("href");
        a.classList.add("hidden");
        toast(tr("授权地址不在白名单内，已禁止直接打开：\n") + raw +
            tr("\n（仅允许 *.huaweicloud.com / *.huaweicloud.cn / *.huawei.com 与本机回环）"), "err");
    }
    return ok;
}

export function cancelLogin(restart) {
    if (codeartsState.loginBusy)
        return;
    setLoginBusy(true);
    domState.$("#loginError").classList.add("hidden");
    return api("/admin/api/login/cancel", { method: "POST", body: { session_id: codeartsState.loginSid } }).then(function () {
        stopLogin();
        drawLogin();
        domState.$("#loginPasted").value = "";
        domState.$("#loginStatus").textContent = tr("先生成链接，再到另一浏览器完成授权。");
        setLoginBusy(false);
        if (restart)
            return generateLogin();
    }).catch(loginFailure).finally(function () { setLoginBusy(false); });
}

export function mountCodearts() {
on("#loginGenerate", generateLogin);
on("#loginCopy", function () { return copyText(domState.$("#loginURL").value, tr("链接已复制，请在另一个浏览器打开")); });
on("#loginCheck", function () {
    if (codeartsState.loginBusy)
        return;
    setLoginBusy(true);
    domState.$("#loginError").classList.add("hidden");
    return pollLogin().catch(loginFailure).finally(function () { setLoginBusy(false); });
});
on("#loginComplete", function () {
    if (codeartsState.loginBusy || !codeartsState.loginPreview || !codeartsState.loginSid)
        return;
    if (codeartsState.loginPreview.existing && !domState.$("#loginReplace").checked) {
        loginFailure(new Error(tr("请勾选确认更新已有账号，或重新生成链接登录其他账号")));
        return;
    }
    setLoginBusy(true);
    domState.$("#loginError").classList.add("hidden");
    return api("/admin/api/login/complete", { method: "POST", body: { session_id: codeartsState.loginSid, preview_id: codeartsState.loginPreview.preview_id, replace_existing: domState.$("#loginReplace").checked, name: domState.$("#accountCreateName").value.trim() } }).then(function () {
        stopLogin();
        drawLogin();
        finishAccountCreation();
        toast(tr("账号已保存"), "ok");
        return refresh();
    }).catch(loginFailure).finally(function () { setLoginBusy(false); });
});
on("#loginSubmit", function () {
    if (codeartsState.loginBusy)
        return;
    var input = domState.$("#loginPasted").value.trim();
    if (!codeartsState.loginSid) {
        loginFailure(new Error(tr("请先生成登录链接")));
        return;
    }
    if (!input) {
        domState.$("#loginCheck").click();
        return;
    }
    setLoginBusy(true);
    codeartsState.loginPreview = null;
    drawLogin();
    domState.$("#loginError").classList.add("hidden");
    return api("/admin/api/login/callback", { method: "POST", body: { session_id: codeartsState.loginSid, input: input } }).then(function () {
        domState.$("#loginPasted").value = "";
        domState.$("#loginStatus").textContent = tr("授权信息已提交，正在换取凭据。稍后点击检查授权信息。");
        return pollLogin();
    }).catch(loginFailure).finally(function () { setLoginBusy(false); });
});
on("#loginCancel", function () { return cancelLogin(false); });
on("#loginRestart", function () { return cancelLogin(true); });
on("#copyTunnel", function () { return copyText(domState.$("#loginTunnel").value, tr("转发命令已复制")); });
}
