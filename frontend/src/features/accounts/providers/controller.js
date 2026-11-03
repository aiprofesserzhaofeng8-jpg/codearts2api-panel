import { t as tr } from '../../../i18n';
import { on } from '../../../components/controls';
import { serverState } from '../../../shared/read-model';
import { isAbortError } from '../../../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { codeartsState, drawLogin, openLogin, stopLogin } from './codearts';
import { copyText, domState, toast } from '../../../shared/dom';
import { sessionState, showAdminLogin } from '../../../app/session';
import { api } from '../../../api/client';
import { listen } from '../../../shared/lifecycle';
import { accountsState, renderAccounts } from '../controller';
import { refresh } from '../../../shared/refresh';
const providersState = {};
providersState.activeProvider = "workbuddy";

providersState.providerSession = "";

providersState.providerPreviewID = "";

providersState.providerBusy = false;

providersState.providerDevices = [];

providersState.accountCreatePlatform = "codearts";

providersState.accountCreateMethod = "browser";

providersState.accountCreateStep = 1;

providersState.accountCreateBusy = false;

export function updateAccountCreateBusy() {
    var busy = !!(providersState.accountCreateBusy || providersState.providerBusy || codeartsState.loginBusy);
    ["#addAccountClose", "#accountCreateCancel", "#accountCreateBack", "#accountCreateNext"].forEach(function (id) { domState.$(id).disabled = busy; });
}

export function accountPlatformLabel() { return { codearts: tr("华为云码道"), workbuddy: "WorkBuddy", trae: "Trae CN / SOLO" }[providersState.accountCreatePlatform]; }

export function renderAccountChoices() {
    document.querySelectorAll("[data-account-platform]").forEach(function (b) { var selected = b.dataset.accountPlatform === providersState.accountCreatePlatform; b.classList.toggle("selected", selected); b.setAttribute("aria-pressed", String(selected)); });
    document.querySelectorAll("[data-account-method]").forEach(function (b) { var selected = b.dataset.accountMethod === providersState.accountCreateMethod; b.classList.toggle("selected", selected); b.setAttribute("aria-pressed", String(selected)); });
    domState.$("#accountBrowserHint").textContent = tr("通过 ") + accountPlatformLabel() + tr(" 官方页面登录并授权");
    domState.$("#accountImportHint").textContent = providersState.accountCreatePlatform === "codearts" ? tr("本机客户端、STS / AK/SK 或 session JSON") : tr("从已有客户端导入完整凭据 JSON");
    domState.$("#accountChoiceHint").textContent = providersState.accountCreatePlatform === "trae" && providersState.accountCreateMethod === "browser" ? tr("下一步填写官方客户端设备信息，再生成授权链接。") : tr("选择平台和添加方式后继续，账号核对完成后再保存。");
}

export function showAccountChoices() {
    providersState.accountCreateStep = 1;
    domState.$("#accountChoose").classList.remove("hidden");
    domState.$("#accountAuthorize").classList.add("hidden");
    domState.$("#accountCreateNext").classList.remove("hidden");
    domState.$("#accountCreateBack").classList.add("hidden");
    domState.$("#accountChooseStep").classList.add("active");
    domState.$("#accountAuthorizeStep").classList.remove("active");
    domState.$("#accountAuthorizeLabel").textContent = tr("账号授权");
    renderAccountChoices();
}

export function openAccountCreate() {
    if (!sessionState.adminAuthenticated) {
        showAdminLogin();
        return;
    }
    domState.$("#accountCreateError").textContent = "";
    domState.$("#accountCreateName").value = "";
    showAccountChoices();
    domState.$("#addAccountDialog").showModal();
}

export function beginAccountAuthorization() {
    if (providersState.accountCreateBusy || providersState.providerBusy || codeartsState.loginBusy)
        return;
    domState.$("#accountCreateError").textContent = "";
    providersState.accountCreateStep = 2;
    domState.$("#accountChoose").classList.add("hidden");
    domState.$("#accountAuthorize").classList.remove("hidden");
    domState.$("#accountCreateNext").classList.add("hidden");
    domState.$("#accountCreateBack").classList.remove("hidden");
    domState.$("#accountChooseStep").classList.remove("active");
    domState.$("#accountAuthorizeStep").classList.add("active");
    domState.$("#accountAuthorizeLabel").textContent = accountPlatformLabel() + (providersState.accountCreateMethod === "browser" ? tr("授权") : tr("导入"));
    ["#providerLoginDialog", "#loginPanel", "#manualAccountPanel"].forEach(function (id) { domState.$(id).classList.add("hidden"); });
    var name = domState.$("#accountCreateName").value.trim();
    if (providersState.accountCreatePlatform === "codearts") {
        if (providersState.accountCreateMethod === "browser")
            openLogin();
        else {
            domState.$("#manualAccountPanel").classList.remove("hidden");
            domState.$("#manualName").value = name;
            domState.$("#sessionName").value = name;
        }
    }
    else {
        openProvider(providersState.accountCreatePlatform);
        domState.$("#providerAccountName").value = name;
        domState.$("#providerBrowserFields").classList.toggle("hidden", providersState.accountCreateMethod !== "browser");
        domState.$("#providerImportSection").classList.toggle("hidden", providersState.accountCreateMethod !== "import");
        domState.$("#providerImportSection").open = providersState.accountCreateMethod === "import";
    }
    domState.$("#addAccountDialog").scrollTop = 0;
}

export function clearAccountSensitiveFields() {
    ["#providerLoginCallback", "#providerImportJSON", "#loginPasted", "#manualAK", "#manualSK", "#manualSTS", "#sessionJSON", "#providerDeviceJSON"].forEach(function (id) { domState.$(id).value = ""; });
}

export function finishAccountCreation() { clearAccountSensitiveFields(); domState.$("#addAccountDialog").close(); }

export function leaveAccountCreate(close) {
    if (providersState.accountCreateBusy || providersState.providerBusy || codeartsState.loginBusy)
        return Promise.resolve();
    providersState.accountCreateBusy = true;
    updateAccountCreateBusy();
    domState.$("#accountCreateError").textContent = "";
    var cancel = Promise.resolve();
    if (providersState.accountCreateStep === 2) {
        if (providersState.accountCreatePlatform !== "codearts" && providersState.providerSession)
            cancel = providerCall("cancel");
        if (providersState.accountCreatePlatform === "codearts" && codeartsState.loginSid)
            cancel = api("/admin/api/login/cancel", { method: "POST", body: { session_id: codeartsState.loginSid } });
    }
    return cancel.then(function () {
        if (providersState.accountCreateStep === 2) {
            if (providersState.accountCreatePlatform === "codearts") {
                stopLogin();
                drawLogin();
            }
            else
                resetProviderLogin();
        }
        clearAccountSensitiveFields();
        if (close)
            domState.$("#addAccountDialog").close();
        else
            showAccountChoices();
    }).catch(function (e) { if (isAbortError(e)) return; domState.$("#accountCreateError").textContent = tr("取消授权失败：") + e.message; }).finally(function () { providersState.accountCreateBusy = false; updateAccountCreateBusy(); });
}

export function addAccountCredentials(action, extra) {
    if (providersState.accountCreateBusy)
        return Promise.resolve(false);
    providersState.accountCreateBusy = true;
    updateAccountCreateBusy();
    domState.$("#accountCreateError").textContent = "";
    return api("/admin/api/accounts", { method: "POST", body: Object.assign({ action: action }, extra) }).then(function () {
        toast(tr("账号已导入"), "ok");
        finishAccountCreation();
        return refresh().then(function () { return true; });
    }).catch(function (e) { if (isAbortError(e)) return; domState.$("#accountCreateError").textContent = tr("导入失败：") + e.message; return false; }).finally(function () { providersState.accountCreateBusy = false; updateAccountCreateBusy(); });
}

export function providerLabel() { return providersState.activeProvider === "trae" ? "Trae" : "WorkBuddy"; }

export function clearDeviceResults() { providersState.providerDevices = []; domState.$("#providerDeviceChoices").classList.add("hidden"); domState.$("#providerDeviceChoice").textContent = ""; domState.$("#providerDeviceStatus").textContent = ""; }

export function setDeviceInputsLocked(locked) { ["#providerMachineID", "#providerDeviceID", "#providerDeviceRead", "#providerDeviceApply", "#providerDeviceChoice", "#providerDeviceJSON"].forEach(function (id) { domState.$(id).disabled = locked; }); }

export function showDeviceChoices(devices) {
    clearDeviceResults();
    providersState.providerDevices = devices.map(function (d) {
        if (!d || typeof d.machine_id !== "string" || typeof d.device_id !== "string" || !/^[A-Za-z0-9_.-]{1,256}$/.test(d.machine_id) || !/^[0-9]{8,32}$/.test(d.device_id))
            throw new Error(tr("设备信息不完整或格式无效，请复制官方客户端读取结果"));
        return d;
    });
    if (!providersState.providerDevices.length)
        return;
    var select = domState.$("#providerDeviceChoice");
    var placeholder = document.createElement("option");
    placeholder.value = "";
    placeholder.textContent = tr("请选择要使用的客户端设备");
    select.appendChild(placeholder);
    providersState.providerDevices.forEach(function (d, i) { var option = document.createElement("option"); option.value = String(i); option.textContent = (d.client || "Trae") + " · " + d.device_id + (d.source ? " · " + d.source : ""); select.appendChild(option); });
    if (providersState.providerDevices.length === 1)
        fillDeviceInfo(providersState.providerDevices[0]);
    else {
        domState.$("#providerMachineID").value = "";
        domState.$("#providerDeviceID").value = "";
        domState.$("#providerDeviceChoices").classList.remove("hidden");
        domState.$("#providerDeviceStatus").textContent = tr("发现多组客户端记录，请选择一组后填入。");
    }
}

export function fillDeviceInfo(d) {
    if (providersState.providerSession)
        throw new Error(tr("本次授权已绑定设备，请先关闭登录窗口再修改"));
    domState.$("#providerMachineID").value = d.machine_id;
    domState.$("#providerDeviceID").value = d.device_id;
    domState.$("#providerDeviceStatus").textContent = tr("已填入 ") + (d.client || "Trae") + tr(" 的设备信息") + (d.source ? tr("，来源：") + d.source : "") + tr("。可继续生成登录链接。");
}

providersState.traeDeviceWindowsCommand = String.raw `Get-ChildItem "$env:APPDATA\*Trae*\User\globalStorage\storage.json", "$env:LOCALAPPDATA\*Trae*\User\globalStorage\storage.json" -ErrorAction SilentlyContinue |
ForEach-Object {
  $clientName = $_.Directory.Parent.Parent.Name
  $s = Get-Content $_.FullName -Raw | ConvertFrom-Json
  if ($s.'telemetry.machineId' -match '^[A-Za-z0-9_.-]{1,256}$') {
    $s.PSObject.Properties.Name |
    Where-Object { $_ -match '^iCubeAuthInfo://icube-dc:[0-9]{8,32}$' } |
    ForEach-Object {
      [pscustomobject]@{client=$clientName; machine_id=$s.'telemetry.machineId'; device_id=($_ -split ':')[-1]}
    }
  }
} | ConvertTo-Json
`;

providersState.traeDeviceUnixCommand = String.raw `python3 - <<'PY'
import json, os, re, sys
from pathlib import Path
home = Path.home()
roots = [Path(os.environ.get('XDG_CONFIG_HOME', home / '.config')), home / 'Library/Application Support']
found, seen = [], set()
for root in roots:
    if not root.is_dir(): continue
    for client in sorted(root.iterdir()):
        if 'trae' not in client.name.lower() or not client.is_dir(): continue
        path = client / 'User/globalStorage/storage.json'
        try: data = json.loads(path.read_text(encoding='utf-8-sig'))
        except (OSError, ValueError): continue
        machine = data.get('telemetry.machineId', '')
        if not isinstance(machine, str) or not re.fullmatch(r'[A-Za-z0-9_.-]{1,256}', machine): continue
        for key in data:
            match = re.fullmatch(r'iCubeAuthInfo://icube-dc:([0-9]{8,32})', key)
            if match and (machine, match[1]) not in seen:
                seen.add((machine, match[1]))
                found.append(dict(client=client.name, machine_id=machine, device_id=match[1]))
print(json.dumps(found, ensure_ascii=False, indent=2))
if not found: print('No registered Trae device found on this computer.', file=sys.stderr)
PY
`;

export function selectProvider(name) { domState.$("#providerTraeDevice").classList.toggle("hidden", name !== "trae"); domState.$("#providerLoginCallback").type = name === "trae" ? "password" : "text"; if (providersState.activeProvider !== name) {
    domState.$("#providerMachineID").value = "";
    domState.$("#providerDeviceID").value = "";
    clearDeviceResults();
    domState.$("#providerDeviceJSON").value = "";
    domState.$("#providerImportJSON").value = "";
    domState.$("#providerAccountName").value = "";
    domState.$("#providerImportReplace").checked = false;
} providersState.activeProvider = name; domState.$("#providerLoginTitle").textContent = tr("添加 ") + providerLabel() + tr(" 账号"); domState.$("#providerImportTitle").textContent = tr("导入 ") + providerLabel() + tr(" 凭据 JSON"); domState.$("#providerScope").querySelector("option[value=global]").disabled = name === "trae"; if (name === "trae")
    domState.$("#providerScope").value = "cn"; domState.$("#providerImportHint").textContent = name === "trae" ? tr("支持 Trae 国内版扁平或 auth/account 嵌套凭据，需包含 access_token、uid、expires_at、machine_id、device_id；国际版尚未接入。") : tr("支持已有 WorkBuddy 导出的扁平或 auth/account 嵌套 JSON。导入前会向上游核对身份。"); domState.$("#providerLoginCallback").placeholder = name === "trae" ? tr("粘贴 127.0.0.1:18080/authorize?... 完整回调地址（含 loginTraceID）") : tr("完成授权后可直接检查，也可粘贴带 state 的回调地址"); }

export function openProvider(name) { if (!sessionState.adminAuthenticated) {
    showAdminLogin();
    return;
} if (providersState.providerSession && providersState.activeProvider !== name) {
    providerJob(function () { return providerCall("cancel").then(function () { resetProviderLogin(); selectProvider(name); domState.$("#providerLoginDialog").classList.remove("hidden"); }); });
    return;
} selectProvider(name); domState.$("#providerLoginDialog").classList.remove("hidden"); }

export function providerError(e) { domState.$("#providerLoginError").textContent = e.message || String(e); }

export function resetProviderLogin() { providersState.providerSession = ""; providersState.providerPreviewID = ""; domState.$("#providerLoginURL").value = ""; domState.$("#providerLoginCallback").value = ""; domState.$("#providerPreview").classList.add("hidden"); domState.$("#providerLoginOpen").classList.add("hidden"); domState.$("#providerLoginOpen").removeAttribute("href"); domState.$("#providerLoginCopy").disabled = true; domState.$("#providerLoginCheck").disabled = true; domState.$("#providerScope").disabled = false; setDeviceInputsLocked(false); domState.$("#providerLoginStatus").textContent = ""; domState.$("#providerLoginError").textContent = ""; domState.$("#providerReplace").checked = false; }

export function providerJob(fn) { if (providersState.providerBusy)
    return Promise.resolve(); providersState.providerBusy = true; updateAccountCreateBusy(); domState.$("#providerLoginError").textContent = ""; return Promise.resolve().then(fn).catch(providerError).finally(function () { providersState.providerBusy = false; updateAccountCreateBusy(); }); }

export function providerCall(action, extra) { return api("/admin/api/provider-login", { method: "POST", body: Object.assign({ action: action, session_id: providersState.providerSession }, extra || {}) }); }

export function refreshAfterProviderSave() { resetProviderLogin(); domState.$("#providerImportJSON").value = ""; finishAccountCreation(); toast(providerLabel() + tr(" 账号已保存，正在刷新模型目录"), "ok"); return api("/admin/api/models/refresh", { method: "POST" }).catch(function (e) { if (isAbortError(e)) return; toast(tr("账号已保存，模型目录刷新失败：") + e.message, "err"); }).then(refresh); }

export function trustedProviderURL(raw) { try {
    var u = new URL(raw);
    return u.protocol === "https:" && !u.username && !u.password && (providersState.activeProvider === "trae" ? ["trae.cn"] : ["workbuddy.cn", "workbuddy.ai", "codebuddy.cn", "codebuddy.ai", "tencent.com", "qq.com"]).some(function (h) { return u.hostname === h || u.hostname.endsWith("." + h); });
}
catch (e) {
    return false;
} }

export function mountProviders() {
on("#addAccount", openAccountCreate);
document.querySelectorAll("[data-account-platform]").forEach(function (button) { listen(button, "click", function () { providersState.accountCreatePlatform = this.dataset.accountPlatform; renderAccountChoices(); }); });
document.querySelectorAll("[data-account-method]").forEach(function (button) { listen(button, "click", function () { providersState.accountCreateMethod = this.dataset.accountMethod; renderAccountChoices(); }); });
on("#accountCreateNext", beginAccountAuthorization);
on("#accountCreateBack", function () { return leaveAccountCreate(false); });
on("#accountCreateCancel", function () { return leaveAccountCreate(true); });
on("#addAccountClose", function () { return leaveAccountCreate(true); });
listen(domState.$("#addAccountDialog"), "cancel", function (e) { e.preventDefault(); leaveAccountCreate(true); });
on("#providerDeviceCommand", function () { return copyText(domState.$("#providerDeviceOS").value === "windows" ? providersState.traeDeviceWindowsCommand : providersState.traeDeviceUnixCommand, tr("读取命令已复制，请在已登录 Trae 官方客户端的电脑执行")); });
on("#providerDeviceRead", function () {
    return providerJob(function () {
        if (providersState.providerSession)
            throw new Error(tr("本次授权已绑定设备，请先关闭登录窗口再读取"));
        domState.$("#providerDeviceStatus").textContent = tr("正在读取面板运行电脑上的 Trae 客户端记录…");
        return api("/admin/api/trae-device").then(function (d) { showDeviceChoices(d.devices || []); if (!providersState.providerDevices.length)
            domState.$("#providerDeviceStatus").textContent = d.message || tr("未找到官方客户端设备信息，请在对应电脑执行读取命令。"); if (d.warnings && d.warnings.length)
            domState.$("#providerDeviceStatus").textContent += " " + d.warnings.join("；"); }).catch(function (e) { if (isAbortError(e)) return; domState.$("#providerDeviceStatus").textContent = tr("读取失败，可重试或复制命令到客户端所在电脑执行。"); throw e; });
    });
});
listen(domState.$("#providerDeviceChoice"), "change", function () { var value = this.value; if (value !== "" && providersState.providerDevices[Number(value)])
    fillDeviceInfo(providersState.providerDevices[Number(value)]); });
on("#providerDeviceApply", function () {
    return providerJob(function () {
        if (providersState.providerSession)
            throw new Error(tr("本次授权已绑定设备，请先关闭登录窗口再修改"));
        var data;
        try {
            data = JSON.parse(domState.$("#providerDeviceJSON").value.trim());
        }
        catch (e) {
            throw new Error(tr("请粘贴读取命令输出的设备信息 JSON"));
        }
        showDeviceChoices(Array.isArray(data) ? data : [data]);
        if (!providersState.providerDevices.length)
            throw new Error(tr("命令输出没有设备信息，请确认在已登录官方客户端的电脑执行"));
        domState.$("#providerDeviceJSON").value = "";
    });
});
on("#providerLoginStart", function () {
    return providerJob(function () {
        var options = { provider: providersState.activeProvider, scope: domState.$("#providerScope").value };
        if (providersState.activeProvider === "trae") {
            options.machine_id = domState.$("#providerMachineID").value.trim();
            options.device_id = domState.$("#providerDeviceID").value.trim();
            if (!options.machine_id || !options.device_id)
                throw new Error(tr("请填写 Trae 官方客户端的机器标识和设备标识"));
            if (!/^[A-Za-z0-9_.-]{1,256}$/.test(options.machine_id))
                throw new Error(tr("机器标识格式无效，请复制官方客户端的完整值"));
            if (!/^[0-9]{8,32}$/.test(options.device_id))
                throw new Error(tr("设备标识应为官方客户端的 8 至 32 位数字设备号"));
        }
        var previous = providersState.providerSession ? providerCall("cancel") : Promise.resolve();
        return previous.then(function () { resetProviderLogin(); return providerCall("start", options); }).then(function (d) { if (!trustedProviderURL(d.url))
            throw new Error(tr("授权链接不在所选上游官方域名范围")); providersState.providerSession = d.session_id; domState.$("#providerLoginURL").value = d.url; domState.$("#providerLoginOpen").href = d.url; domState.$("#providerLoginOpen").classList.remove("hidden"); domState.$("#providerScope").disabled = true; setDeviceInputsLocked(true); domState.$("#providerLoginCopy").disabled = false; domState.$("#providerLoginCheck").disabled = false; domState.$("#providerLoginStatus").textContent = providersState.activeProvider === "trae" ? tr("请在另一浏览器完成 Trae 授权，跳转到 127.0.0.1:18080 后复制完整地址并粘贴。该地址无人监听时会显示连接失败，但地址中的授权信息仍可提交；检查后再确认保存。") : tr("链接已生成。请复制到另一浏览器完成授权，再点击检查。"); });
    });
});
on("#providerLoginCopy", function () { return copyText(domState.$("#providerLoginURL").value, providerLabel() + tr(" 授权链接已复制")); });
on("#providerLoginCheck", function () { return providerJob(function () { providersState.providerPreviewID = ""; domState.$("#providerPreview").classList.add("hidden"); return providerCall("check", { input: domState.$("#providerLoginCallback").value.trim() }).then(function (d) { if (!d.ready) {
    domState.$("#providerLoginStatus").textContent = d.message || tr("尚未完成授权");
    return;
} providersState.providerPreviewID = d.preview_id; domState.$("#providerPreviewText").textContent = (d.name || providerLabel() + tr(" 账号")) + " · " + d.uid + " · " + (d.scope === "global" ? tr("国际版") : tr("国内版")) + (d.existing ? tr(" · 已存在，保存将更新凭据") : tr(" · 新账号")); domState.$("#providerPreview").classList.remove("hidden"); domState.$("#providerLoginStatus").textContent = tr("账号已验证，点击确认保存才会加入账号池。"); domState.$("#providerLoginCallback").value = ""; }); }); });
on("#providerLoginComplete", function () { return providerJob(function () { return providerCall("complete", { preview_id: providersState.providerPreviewID, replace_existing: domState.$("#providerReplace").checked, name: domState.$("#providerAccountName").value.trim() }).then(refreshAfterProviderSave); }); });
on("#providerImport", function () { return providerJob(function () { var credentials; try {
    credentials = JSON.parse(domState.$("#providerImportJSON").value);
}
catch (e) {
    throw new Error(tr("请粘贴有效的 ") + providerLabel() + tr(" JSON 凭据"));
} return api("/admin/api/provider-import", { method: "POST", body: { provider: providersState.activeProvider, credentials: credentials, replace_existing: domState.$("#providerImportReplace").checked, name: domState.$("#providerAccountName").value.trim() } }).then(refreshAfterProviderSave); }); });
document.querySelectorAll("[data-account-filter]").forEach(function (button) {
    listen(button, "click", function () {
        accountsState.accountFilter = button.dataset.accountFilter;
        document.querySelectorAll("[data-account-filter]").forEach(function (b) {
            b.classList.toggle("on", b === button);
            b.setAttribute("aria-pressed", String(b === button));
        });
        if (serverState.data)
            renderAccounts(serverState.data);
    });
});
on("#importLocal", function () { return addAccountCredentials("import", {}); });
// 凭据类型切换：永久 AK/SK 不需要 security_token，隐藏该项并更新提示。
// 注意 select 用 change 事件，不能复用只绑 onclick 的 on()。
listen(domState.$("#manualKind"), "change", function () {
    var isAKSK = this.value === "aksk";
    domState.$("#manualSTSField").classList.toggle("hidden", isAKSK);
    if (isAKSK)
        domState.$("#manualSTS").value = "";
    domState.$("#manualKindHint").textContent = isAKSK
        ? tr("永久 AK/SK 不过期、无需续期；请勿与 STS 临时凭据混用。")
        : tr("STS 临时凭据约 2 小时过期，到期需重新录入或用 refresh_token 续期。");
});
on("#addManual", function () {
    var ak = domState.$("#manualAK").value.trim(), sk = domState.$("#manualSK").value.trim();
    var kind = domState.$("#manualKind").value;
    if (!ak || !sk) {
        toast(tr("请填写 Access Key 与 Secret Key"), "err");
        return;
    }
    var payload = {
        name: domState.$("#manualName").value.trim(), access_key: ak, secret_key: sk,
        credential_kind: kind
    };
    if (kind !== "aksk")
        payload.security_token = domState.$("#manualSTS").value.trim();
    return addAccountCredentials("manual", payload).then(function (ok) {
        if (ok) {
            domState.$("#manualAK").value = "";
            domState.$("#manualSK").value = "";
            domState.$("#manualSTS").value = "";
        }
    });
});
on("#addSession", function () {
    var js = domState.$("#sessionJSON").value.trim();
    if (!js) {
        toast(tr("请粘贴 session JSON"), "err");
        return;
    }
    return addAccountCredentials("session-json", { name: domState.$("#sessionName").value.trim(), session_json: js }).then(function (ok) {
        if (ok)
            domState.$("#sessionJSON").value = "";
    });
});
}
