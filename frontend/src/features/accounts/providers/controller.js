import { t as tr } from '../../../i18n';
import { on } from '../../../components/controls';
import { serverState } from '../../../shared/read-model';
import { isAbortError } from '../../../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { codeartsState, drawLogin, openLogin, stopLogin } from './codearts';
import { copyText, domState, toast } from '../../../shared/dom';
import { sessionState, showAdminLogin } from '../../../app/session';
import { api } from '../../../api/client';
import { listen, cleanup } from '../../../shared/lifecycle';
import { connectorKey, pairConnector, forgetConnector, checkConnector, connectorLogin } from './teleagent-connector';
import { accountsState, renderAccounts } from '../controller';
import { refresh } from '../../../shared/refresh';
import teleagentHelperSource from '../../../../../scripts/teleagent-login.mjs?raw';
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
    domState.$("#teleagentResultFile").disabled = busy || !providersState.teleagentTask;
}

export function accountPlatformLabel() { return { codearts: tr("华为云码道"), workbuddy: "WorkBuddy", trae: "Trae CN / SOLO", qoder: "Qoder", kiro: "Kiro", chatgpt: "ChatGPT / Codex", claude: "Claude", teleagent: "TeleAgent" }[providersState.accountCreatePlatform]; }

export function renderAccountChoices() {
    var teleagent = providersState.accountCreatePlatform === "teleagent";
    var browserButton = document.querySelector('[data-account-method="browser"]');
    browserButton.disabled = false;
    browserButton.classList.remove("hidden");
    document.querySelectorAll("[data-account-platform]").forEach(function (b) { var selected = b.dataset.accountPlatform === providersState.accountCreatePlatform; b.classList.toggle("selected", selected); b.setAttribute("aria-pressed", String(selected)); });
    document.querySelectorAll("[data-account-method]").forEach(function (b) { var selected = b.dataset.accountMethod === providersState.accountCreateMethod; b.classList.toggle("selected", selected); b.setAttribute("aria-pressed", String(selected)); });
    domState.$("#accountBrowserHint").textContent = providersState.accountCreatePlatform === "teleagent" ? tr("通过本机登录助手完成官方网页登录，核对后保存。") : providersState.accountCreatePlatform === "kiro" ? tr("通过 AWS Builder ID 设备授权登录；其他登录方式请导入凭据。") : tr("通过 ") + accountPlatformLabel() + tr(" 官方页面登录并授权");
    domState.$("#accountImportHint").textContent = teleagent ? tr("在官方客户端登录后，导入完整账号与设备凭据") : providersState.accountCreatePlatform === "codearts" ? tr("本机客户端、STS / AK/SK 或 session JSON") : ["chatgpt", "claude"].includes(providersState.accountCreatePlatform) ? tr("导入 OAuth 凭据或官方 API Key JSON") : tr("从已有客户端导入完整凭据 JSON");
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
    clearTeleagentTask();
    domState.$("#teleagentPairKey").value = "";
}

export function finishAccountCreation() { clearAccountSensitiveFields(); domState.$("#addAccountDialog").close(); }

export function leaveAccountCreate(close) {
    if (providersState.accountCreateBusy || providersState.providerBusy || codeartsState.loginBusy)
        return Promise.resolve();
    stopTeleagentConnector();
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

export function providerLabel() { return {trae:"Trae",workbuddy:"WorkBuddy",qoder:"Qoder",kiro:"Kiro",chatgpt:"ChatGPT / Codex",claude:"Claude",teleagent:"TeleAgent"}[providersState.activeProvider] || providersState.activeProvider; }

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

export function selectProvider(name) {
    domState.$("#providerTraeDevice").classList.toggle("hidden", name !== "trae");
    domState.$("#providerTeleAgentHelper").classList.toggle("hidden", name !== "teleagent");
    domState.$("#providerBrowserSteps").textContent = name === "teleagent" ? tr("点击浏览器登录 → 完成官方认证 → 自动显示账号预览 → 确认保存。") : tr("生成链接 → 在另一浏览器授权 → 检查账号预览 → 确认保存。关闭窗口不会自动保存账号。");
    domState.$("#providerLoginCallback").type = name === "trae" || name === "teleagent" ? "password" : "text";
    domState.$("#providerLoginCallback").parentElement.classList.toggle("hidden", ["qoder", "kiro", "teleagent"].includes(name));
    domState.$("#providerLoginURL").parentElement.classList.toggle("hidden", name === "teleagent");
    domState.$("#providerLoginStart").textContent = name === "teleagent" ? tr("浏览器登录") : tr("生成登录链接");
    domState.$("#providerLoginCopy").classList.toggle("hidden", name === "teleagent");
    domState.$("#providerLoginCheck").classList.toggle("hidden", name === "teleagent");
    if (name === "teleagent") {
        domState.$("#teleagentInstallCommand").value = 'node teleagent-login.mjs --install --origin "' + location.origin + '"';
        domState.$("#teleagentConnectorSetup").open = !connectorKey();
        domState.$("#teleagentFileFallback").open = false;
        domState.$("#teleagentConnectorStatus").textContent = connectorKey() ? tr("本机组件已配对，点击浏览器登录即可开始。") : tr("首次使用请先连接本机登录组件");
    }
    if (providersState.activeProvider !== name) {
        domState.$("#providerMachineID").value = "";
        domState.$("#providerDeviceID").value = "";
        clearDeviceResults();
        domState.$("#providerDeviceJSON").value = "";
        domState.$("#providerImportJSON").value = "";
        domState.$("#providerAccountName").value = "";
        domState.$("#providerImportReplace").checked = false;
    }
    providersState.activeProvider = name;
    domState.$("#providerLoginTitle").textContent = tr("添加 ") + providerLabel() + tr(" 账号");
    domState.$("#providerImportTitle").textContent = tr("导入 ") + providerLabel() + tr(" 凭据 JSON");
    var scopeSelect = domState.$("#providerScope");
    var scopeValue = scopeSelect.value;
    scopeSelect.textContent = "";
    var scopes = name === "teleagent" ? [["cn", tr("国内版")]] : ["chatgpt", "claude"].includes(name) ? [["global", tr("国际版")]] : name === "kiro" ? [["us-east-1", "us-east-1"], ["eu-central-1", "eu-central-1"]] : [["cn", tr("国内版")], ["global", tr("国际版")]];
    scopes.forEach(function (entry) { var option = document.createElement("option"); option.value = entry[0]; option.textContent = entry[1]; option.disabled = name === "trae" && entry[0] === "global"; scopeSelect.appendChild(option); });
    if (scopes.some(function (entry) { return entry[0] === scopeValue && !(name === "trae" && scopeValue === "global"); })) scopeSelect.value = scopeValue;
    const hints = {
teleagent: "TeleAgent 需要 token、api_key、device_id、install_id、app_version，account_id 与 expires_at 可从 JWT 读取。使用本人获准使用的完整凭据；到期需重新登录。operation_token / oper_token 可选，用于独立积分门户。",
chatgpt: "支持 Codex auth.json 或 access_token、account_id、user_id、expires_at；也可导入官方 OpenAI api_key（sk- 开头）。浏览器授权后粘贴完整 localhost:1455/auth/callback 地址。",
claude: "支持 access_token、account_uuid、expires_at 和可选 refresh_token；也可导入官方 Anthropic api_key（sk-ant- 开头）。浏览器授权后粘贴 code#state 或完整回调地址。",
        trae: "支持 Trae 国内版扁平或 auth/account 嵌套凭据，需包含 access_token、uid、expires_at、machine_id、device_id；国际版尚未接入。",
        workbuddy: "支持已有 WorkBuddy 导出的扁平或 auth/account 嵌套 JSON。导入前会向上游核对身份。",
        kiro: "Kiro 支持 Builder ID 设备授权；也可导入 api_key（ksk_ 开头）或完整 OAuth JSON。region 默认 us-east-1；导入前校验上游模型访问。",
        qoder: "Qoder 凭据需包含 access_token、uid、expires_at、machine_id 和 scope（cn / global）。导入前会向上游核对身份。"
    };
    domState.$("#providerImportHint").textContent = tr(hints[name]);
    document.querySelector('label[for="providerLoginCallback"]').textContent = ["chatgpt", "claude"].includes(name) ? tr("回调地址或授权码（必填）") : tr("回调地址（可选）");
    domState.$("#providerLoginCallback").placeholder = name === "chatgpt" ? tr("粘贴完整 http://localhost:1455/auth/callback?code=…&state=… 地址") : name === "claude" ? tr("粘贴 code#state 或完整 Claude OAuth 回调地址") : name === "trae" ? tr("粘贴 127.0.0.1:18080/authorize?... 完整回调地址（含 loginTraceID）") : tr("完成授权后可直接检查，也可粘贴带 state 的回调地址");
}

export function openProvider(name) { if (!sessionState.adminAuthenticated) {
    showAdminLogin();
    return;
} if (providersState.providerSession && providersState.activeProvider !== name) {
    providerJob(function () { return providerCall("cancel").then(function () { resetProviderLogin(); selectProvider(name); domState.$("#providerLoginDialog").classList.remove("hidden"); }); });
    return;
} selectProvider(name); domState.$("#providerLoginDialog").classList.remove("hidden"); }

export function providerError(e) { domState.$("#providerLoginError").textContent = e.message || String(e); }

export function resetProviderLogin() { clearTeleagentTask(); providersState.providerSession = ""; providersState.providerPreviewID = ""; domState.$("#providerLoginURL").value = ""; domState.$("#providerLoginCallback").value = ""; domState.$("#providerPreview").classList.add("hidden"); domState.$("#providerLoginOpen").classList.add("hidden"); domState.$("#providerLoginOpen").removeAttribute("href"); domState.$("#providerLoginCopy").disabled = true; domState.$("#providerLoginCheck").disabled = true; domState.$("#providerScope").disabled = false; setDeviceInputsLocked(false); domState.$("#providerLoginStatus").textContent = ""; domState.$("#providerLoginError").textContent = ""; domState.$("#providerReplace").checked = false; }

function clearTeleagentTask() {
    stopTeleagentConnector();
    providersState.teleagentTask = null;
    domState.$("#teleagentHelperCommand").value = "";
    domState.$("#teleagentResultFile").value = "";
    ["#teleagentTaskDownload", "#teleagentCommandCopy", "#teleagentResultFile"].forEach(function (id) { domState.$(id).disabled = true; });
}

function downloadText(name, text, type) {
    const url = URL.createObjectURL(new Blob([text], { type }));
    const link = document.createElement("a"); link.href = url; link.download = name; link.click();
    setTimeout(function () { URL.revokeObjectURL(url); }, 1000);
}

function teleagentTaskName() { return "teleagent-login-task-" + providersState.teleagentTask.challenge.slice(0, 8) + ".json"; }

function setTeleagentTask(d) {
    if (typeof d.helper_challenge !== "string" || !/^[A-Za-z0-9_-]{43}$/.test(d.helper_challenge) || !Number.isSafeInteger(d.expires_at)) throw new Error(tr("登录任务格式无效，请重新生成"));
    providersState.teleagentTask = { version: 1, provider: "teleagent", challenge: d.helper_challenge, url: d.url, expires_at: d.expires_at };
    domState.$("#teleagentHelperCommand").value = "node teleagent-login.mjs " + teleagentTaskName();
    ["#teleagentTaskDownload", "#teleagentCommandCopy", "#teleagentResultFile"].forEach(function (id) { domState.$(id).disabled = false; });
}

export function providerJob(fn) { if (providersState.providerBusy)
    return Promise.resolve(); providersState.providerBusy = true; updateAccountCreateBusy(); domState.$("#providerLoginError").textContent = ""; return Promise.resolve().then(fn).catch(providerError).finally(function () { providersState.providerBusy = false; updateAccountCreateBusy(); }); }

export function providerCall(action, extra) { return api("/admin/api/provider-login", { method: "POST", body: Object.assign({ action: action, session_id: providersState.providerSession }, extra || {}) }); }

export function refreshAfterProviderSave() { resetProviderLogin(); domState.$("#providerImportJSON").value = ""; finishAccountCreation(); toast(providerLabel() + tr(" 账号已保存，正在刷新模型目录"), "ok"); return api("/admin/api/models/refresh", { method: "POST" }).catch(function (e) { if (isAbortError(e)) return; toast(tr("账号已保存，模型目录刷新失败：") + e.message, "err"); }).then(refresh); }

export function trustedProviderURL(raw) { try {
    var u = new URL(raw);
    if (providersState.activeProvider === "teleagent") return u.protocol === "https:" && u.hostname === "www.teleai.com.cn" && !u.port && !u.username && !u.password && u.pathname === "/teleai-api/upms/public/auth/authorize";
    return u.protocol === "https:" && !u.username && !u.password && (providersState.activeProvider === "trae" ? ["trae.cn"] : providersState.activeProvider === "chatgpt" ? ["auth.openai.com"] : providersState.activeProvider === "claude" ? ["claude.com"] : providersState.activeProvider === "kiro" ? ["view.awsapps.com", "device.sso.us-east-1.amazonaws.com", "device.sso.eu-central-1.amazonaws.com"] : providersState.activeProvider === "qoder" ? ["qoder.com", "qoder.com.cn", "qoder.cn"] : ["workbuddy.cn", "workbuddy.ai", "codebuddy.cn", "codebuddy.ai", "tencent.com", "qq.com"]).some(function (h) { return u.hostname === h || u.hostname.endsWith("." + h); });
}
catch (e) {
    return false;
} }

function stopTeleagentConnector() {
    providersState.connectorAbort?.abort(); providersState.connectorAbort = null;
}

function showProviderPreview(d) {
    if (!d.ready) { domState.$("#providerLoginStatus").textContent = d.message || tr("尚未完成授权"); return; }
    providersState.providerPreviewID = d.preview_id;
    domState.$("#providerPreviewText").textContent = (d.name || providerLabel() + tr(" 账号")) + " · " + d.uid + " · " + (providersState.activeProvider === "kiro" ? d.scope : d.scope === "global" ? tr("国际版") : tr("国内版")) + (d.existing ? tr(" · 已存在，保存将更新凭据") : tr(" · 新账号"));
    domState.$("#providerPreview").classList.remove("hidden");
    domState.$("#providerLoginStatus").textContent = tr("账号已验证，点击确认保存才会加入账号池。");
    domState.$("#providerLoginCallback").value = "";
}

async function automaticTeleagentLogin(task, session) {
    const controller = new AbortController(); providersState.connectorAbort = controller;
    try {
        domState.$("#providerLoginStatus").textContent = tr("正在打开官方登录窗口，请完成认证。完成后账号预览会自动显示。");
        const input = await connectorLogin(task, controller.signal);
        if (controller.signal.aborted || providersState.providerSession !== session) return;
        await providerJob(async function () {
            const d = await providerCall("check", { input });
            if (!controller.signal.aborted && providersState.providerSession === session) showProviderPreview(d);
        });
    } catch (e) {
        if (!controller.signal.aborted && providersState.providerSession === session) {
            providerError(e); domState.$("#providerLoginStatus").textContent = tr("登录未完成，可重新点击浏览器登录。尚未保存账号。");
        }
    } finally { if (providersState.connectorAbort === controller) providersState.connectorAbort = null; }
}

export function mountProviders() {
cleanup(function () { stopTeleagentConnector(); providersState.teleagentTask = null; });
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
function startProviderAuthorization(automatic = true) {
    return providerJob(async function () {
        if (providersState.activeProvider === "teleagent" && automatic) {
            try { await checkConnector(); } catch (e) { domState.$("#teleagentConnectorSetup").open = true; throw e; }
        }
        stopTeleagentConnector();
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
            throw new Error(tr("授权链接不在所选上游官方域名范围")); providersState.providerSession = d.session_id; domState.$("#providerLoginURL").value = d.url; domState.$("#providerLoginOpen").href = d.url; domState.$("#providerLoginOpen").classList.toggle("hidden", providersState.activeProvider === "teleagent"); if (providersState.activeProvider === "teleagent") setTeleagentTask(d); domState.$("#providerScope").disabled = true; setDeviceInputsLocked(true); domState.$("#providerLoginCopy").disabled = false; domState.$("#providerLoginCheck").disabled = false; domState.$("#providerLoginStatus").textContent = providersState.activeProvider === "teleagent" ? tr("请下载助手和本次任务，在自己的电脑完成官方登录，再选择结果文件并检查；不要直接在普通浏览器打开授权链接。") : providersState.activeProvider === "trae" ? tr("请在另一浏览器完成 Trae 授权，跳转到 127.0.0.1:18080 后复制完整地址并粘贴。该地址无人监听时会显示连接失败，但地址中的授权信息仍可提交；检查后再确认保存。") : providersState.activeProvider === "chatgpt" ? tr("授权后复制完整 localhost:1455/auth/callback 地址并粘贴。若页面显示连接失败，仍可复制地址栏中的回调地址；检查预览后再保存。") : providersState.activeProvider === "claude" ? tr("授权后粘贴 code#state 或完整 Claude OAuth 回调地址，检查预览后再保存。") : tr("链接已生成。请复制到另一浏览器完成授权，再点击检查。"); if (providersState.activeProvider === "teleagent" && automatic) setTimeout(function () { if (providersState.providerSession === d.session_id) automaticTeleagentLogin(providersState.teleagentTask, d.session_id); }, 0); });
    });
}
on("#providerLoginStart", function () { return startProviderAuthorization(true); });
on("#teleagentManualTask", function () { return startProviderAuthorization(false); });
on("#teleagentManualCheck", function () { domState.$("#providerLoginCheck").click(); });
on("#teleagentInstallCopy", function () { return copyText(domState.$("#teleagentInstallCommand").value, tr("安装命令已复制，请在自己的电脑执行一次")); });
on("#teleagentPair", function () { return providerJob(async function () { await pairConnector(domState.$("#teleagentPairKey").value); domState.$("#teleagentPairKey").value = ""; domState.$("#teleagentConnectorSetup").open = false; domState.$("#teleagentConnectorStatus").textContent = tr("本机组件已配对，点击浏览器登录即可开始。"); }); });
on("#teleagentForget", function () { stopTeleagentConnector(); forgetConnector(); domState.$("#teleagentPairKey").value = ""; domState.$("#teleagentConnectorSetup").open = true; domState.$("#teleagentConnectorStatus").textContent = tr("首次使用请先连接本机登录组件"); });
on("#teleagentHelperDownload", function () { downloadText("teleagent-login.mjs", teleagentHelperSource, "text/javascript;charset=utf-8"); });
on("#teleagentTaskDownload", function () {
    if (!providersState.teleagentTask || providersState.teleagentTask.expires_at * 1000 <= Date.now()) { providerError(new Error(tr("登录任务已过期，请重新生成"))); return; }
    downloadText(teleagentTaskName(), JSON.stringify(providersState.teleagentTask, null, 2), "application/json");
});
on("#teleagentCommandCopy", function () { return copyText(domState.$("#teleagentHelperCommand").value, tr("助手命令已复制，请在自己的电脑执行")); });
listen(domState.$("#teleagentResultFile"), "change", function () {
    const file = this.files[0];
    if (!file) return;
    domState.$("#providerLoginCallback").value = "";
    providersState.providerPreviewID = ""; domState.$("#providerPreview").classList.add("hidden");
    return providerJob(async function () {
        try {
            if (!providersState.teleagentTask || providersState.activeProvider !== "teleagent" || file.size > 256 * 1024) throw new Error(tr("请选择本次任务生成的登录结果 JSON（不超过 256 KiB）"));
            const text = await file.text(); let result;
            try { result = JSON.parse(text); } catch { throw new Error(tr("登录结果 JSON 格式无效")); }
            if (result.version !== 1 || result.provider !== "teleagent" || result.challenge !== providersState.teleagentTask.challenge || !result.credentials || typeof result.credentials !== "object" || Array.isArray(result.credentials)) throw new Error(tr("登录结果不属于本次任务，请重新选择"));
            if (providersState.teleagentTask.expires_at * 1000 <= Date.now()) throw new Error(tr("登录任务已过期，请重新生成"));
            domState.$("#providerLoginCallback").value = text;
            providersState.providerPreviewID = ""; domState.$("#providerPreview").classList.add("hidden");
            domState.$("#providerLoginStatus").textContent = tr("已读取登录结果，点击检查授权信息核对账号；尚未保存。");
        } finally { domState.$("#teleagentResultFile").value = ""; }
    });
});
on("#providerLoginCopy", function () { return copyText(domState.$("#providerLoginURL").value, providerLabel() + tr(" 授权链接已复制")); });
on("#providerLoginCheck", function () { return providerJob(function () {
    providersState.providerPreviewID = ""; domState.$("#providerPreview").classList.add("hidden");
    return providerCall("check", { input: domState.$("#providerLoginCallback").value.trim() }).then(showProviderPreview);
}); });
on("#providerLoginComplete", function () { return providerJob(function () { return providerCall("complete", { preview_id: providersState.providerPreviewID, replace_existing: domState.$("#providerReplace").checked, name: domState.$("#providerAccountName").value.trim() }).then(refreshAfterProviderSave); }); });
on("#providerImport", function () { return providerJob(function () { var credentials; try {
    credentials = JSON.parse(domState.$("#providerImportJSON").value);
}
catch (e) {
    throw new Error(tr("请粘贴有效的 ") + providerLabel() + tr(" JSON 凭据"));
} return api("/admin/api/provider-import", { method: "POST", body: { provider: providersState.activeProvider, credentials: credentials, replace_existing: domState.$("#providerImportReplace").checked, name: domState.$("#providerAccountName").value.trim() } }).then(refreshAfterProviderSave); }); });
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
