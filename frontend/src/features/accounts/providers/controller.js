import { accountTypes, brandName, brandIcon, brandIconStyle, extraPlatforms } from '../brands';
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
import mobileclawExportSource from '../../../../../scripts/mobileclaw-export.mjs?raw';
const officialAPI = Object.fromEntries(['deepseek', 'zhipu', 'kimi', 'minimax', 'gemini', 'opencode', 'commandcode', 'cline', 'typesafe'].map(id => [id, brandName(id)]));
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
    ["#addAccountClose", "#accountCreateCancel", "#accountCreateBack", "#accountCreateNext", "#accountSwitchCancel", "#accountSwitchConfirm"].forEach(function (id) { domState.$(id).disabled = busy; });
    domState.$("#teleagentResultFile").disabled = busy || !providersState.teleagentTask;
}

providersState.accountCreateBrand = 'codearts';
providersState.accountCreateType = 'default';
providersState.expanded = false;
let preparedPath = '';
let pendingSwitch = null;
const sensitiveFields = ['#providerAPIKey', '#providerLoginCallback', '#providerImportJSON', '#loginPasted', '#manualAK', '#manualSK', '#manualSTS', '#sessionJSON', '#providerDeviceJSON', '#providerMachineID', '#providerDeviceID', '#teleagentPairKey'];
function selectedType() { return accountTypes(providersState.accountCreateBrand).find(t => t.id === providersState.accountCreateType); }
function pathKey() { return [providersState.accountCreateBrand, providersState.accountCreateType, providersState.accountCreateMethod].join('/'); }
export function accountPlatformLabel() { return brandName(providersState.accountCreateBrand); }
function renderExpansion() {
    const extra = extraPlatforms.some(b => b.id === providersState.accountCreateBrand);
    domState.$('#accountExtraPlatforms').classList.toggle('hidden', !providersState.expanded);
    domState.$('#accountPlatformsMore').classList.toggle('hidden', providersState.expanded);
    domState.$('#accountPlatformsMore').setAttribute('aria-expanded', String(providersState.expanded));
    const summary = domState.$('#accountSelectedSummary');
    summary.classList.toggle('hidden', !extra || providersState.expanded);
    summary.replaceChildren();
    const tile = document.createElement('span'); tile.className = 'platform-icon'; tile.style.setProperty('--brand-tint', brandIconStyle(providersState.accountCreateBrand)['--brand-tint']);
    const icon = document.createElement('img'); icon.src = brandIcon(providersState.accountCreateBrand); icon.alt = ''; tile.append(icon);
    summary.append(tile, document.createTextNode(tr('已选：') + accountPlatformLabel()));
}
function renderTypes() {
    const container = domState.$('#accountTypes'); container.replaceChildren();
    const types = accountTypes(providersState.accountCreateBrand);
    types.forEach(type => {
        const item = document.createElement(types.length === 1 ? 'p' : 'button');
        item.textContent = tr(type.label);
        if (types.length > 1) {
            item.className = 'account-type' + (type.id === providersState.accountCreateType ? ' selected' : '');
            item.dataset.accountType = type.id; item.setAttribute('role', 'radio');
            item.setAttribute('aria-checked', String(type.id === providersState.accountCreateType));
            item.tabIndex = type.id === providersState.accountCreateType ? 0 : -1;
        } else item.className = 'account-type-summary';
        container.append(item);
    });
}
async function switchChoice(brand, type, method) {
    if (providersState.accountCreateBusy || providersState.providerBusy || codeartsState.loginBusy) return;
    const target = accountTypes(brand).find(t => t.id === type);
    method = target.methods.includes(method) ? method : target.methods[0];
    if ([brand, type, method].join('/') === pathKey()) return;
    if (preparedPath && (providersState.providerSession || codeartsState.loginSid || sensitiveFields.some(id => domState.$(id).value))) {
        pendingSwitch = {brand, type, method}; domState.$('#accountSwitchDialog').showModal(); return;
    }
    applyChoice(brand, type, method);
}
function applyChoice(brand, type, method) {
    clearAccountSensitiveFields(); preparedPath = '';
    providersState.accountCreateBrand = brand; providersState.accountCreateType = type;
    providersState.accountCreatePlatform = selectedType().provider;
    providersState.accountCreateMethod = method;
    if (extraPlatforms.some(b => b.id === brand)) providersState.expanded = true;
    domState.$('#accountCreateError').textContent = '';
    renderAccountChoices();
}
async function releaseAuthorization() {
    stopTeleagentConnector(); stopMobileClawQR();
    if (providersState.providerSession) await providerCall('cancel');
    if (codeartsState.loginSid) await api('/admin/api/login/cancel', {method: 'POST', body: {session_id: codeartsState.loginSid}});
    resetProviderLogin(); stopLogin(); drawLogin();
}
function configureType() {
    const type = selectedType();
    ['geminiImportMode', 'codingImportMode', 'opencodeImportScope', 'commandcodeImportMode'].forEach(id => domState.$('#' + id + 'Field').classList.add('hidden'));
    if (type.field) domState.$('#' + type.field).value = type.value;
    domState.$('#providerAPIKeyField').classList.toggle('hidden', !type.key);
    domState.$('#providerImportJSON').classList.toggle('hidden', !!type.key);
    domState.$('#providerScope').parentElement.classList.toggle('hidden', !['workbuddy', 'trae', 'qoder', 'kiro'].includes(type.provider));
    domState.$('#providerImportTitle').textContent = type.key ? 'API Key' : tr('导入上游凭据 JSON');
    if (['chatgpt', 'claude'].includes(type.provider)) domState.$('#providerImportHint').textContent = type.key ? tr('使用所选平台的官方 API Key。') : tr('导入订阅账号的完整 OAuth 凭据 JSON。API Key 请返回选择 API 类型。');
    if (type.provider === 'vertex') domState.$('#providerImportHint').textContent += ' ' + tr('上游协议：Google 原生 API。');
    if (type.provider === 'gemini' && type.id === 'cli') domState.$('#providerImportHint').textContent = tr('导入 CLI / Code Assist JSON，包含完整 OAuth 凭据及项目。');
}

export function renderAccountChoices() {
    var teleagent = providersState.accountCreatePlatform === 'teleagent';
    var mobileclaw = providersState.accountCreatePlatform === 'mobileclaw';
    renderTypes(); renderExpansion();
    document.querySelectorAll('[data-account-platform]').forEach(b => {
        const selected = b.dataset.accountPlatform === providersState.accountCreateBrand;
        b.classList.toggle('selected', selected); b.setAttribute('aria-checked', String(selected)); b.setAttribute('aria-pressed', String(selected)); b.tabIndex = selected ? 0 : -1;
    });
    document.querySelectorAll('[data-account-method]').forEach(b => {
        const available = selectedType().methods.includes(b.dataset.accountMethod);
        b.disabled = !available; b.classList.toggle('hidden', !available);
        const selected = b.dataset.accountMethod === providersState.accountCreateMethod;
        b.classList.toggle('selected', selected); b.setAttribute('aria-pressed', String(selected));
    });
    domState.$('#accountMethodImport strong').textContent = selectedType().key ? 'API Key' : tr('凭据导入');
    domState.$("#accountBrowserHint").textContent = providersState.accountCreatePlatform === "teleagent" ? tr("通过本机登录助手完成官方网页登录，核对后保存。") : providersState.accountCreatePlatform === "kiro" ? tr("通过 AWS Builder ID 设备授权登录；其他登录方式请导入凭据。") : tr("通过 ") + accountPlatformLabel() + tr(" 官方页面登录并授权");
    domState.$('#accountImportHint').textContent = selectedType().key ? tr('使用对应账号类型的 API Key。') : providersState.accountCreatePlatform === 'vertex' ? tr('导入 Google 服务账号 JSON。') : providersState.accountCreatePlatform === 'bedrock' ? tr('导入 AWS 凭据 JSON。') : mobileclaw ? tr('从官方客户端导出个人免费 Token 凭据。') : teleagent ? tr('在官方客户端登录后，导入完整账号与设备凭据') : providersState.accountCreatePlatform === 'codearts' ? tr('本机客户端、STS / AK/SK 或 session JSON') : tr('从已有客户端导入完整凭据 JSON');
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
    providersState.expanded = extraPlatforms.some(b => b.id === providersState.accountCreateBrand);
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
    domState.$('#accountAuthorizationPath').textContent = accountPlatformLabel() + ' / ' + (selectedType().label === accountPlatformLabel() ? tr(providersState.accountCreateMethod === 'browser' ? '浏览器授权' : '凭据导入') : tr(selectedType().label));
    if (preparedPath === pathKey()) {
        domState.$(providersState.accountCreatePlatform === 'codearts' ? (providersState.accountCreateMethod === 'browser' ? '#loginPanel' : '#manualAccountPanel') : '#providerLoginDialog').classList.remove('hidden');
        return;
    }
    preparedPath = pathKey();
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
        configureType();
        domState.$("#providerAccountName").value = name;
        domState.$("#providerBrowserFields").classList.toggle("hidden", providersState.accountCreateMethod !== "browser");
        domState.$("#providerImportSection").classList.toggle("hidden", providersState.accountCreateMethod !== "import");
        domState.$("#providerImportSection").open = providersState.accountCreateMethod === "import";
    }
    domState.$('.account-create-body').scrollTop = 0;
}

export function clearAccountSensitiveFields() {
    sensitiveFields.forEach(id => { domState.$(id).value = ''; });
    domState.$('#providerImportReplace').checked = false;
    clearTeleagentTask();
    domState.$("#teleagentPairKey").value = "";
}

export function finishAccountCreation() { clearAccountSensitiveFields(); preparedPath = ""; domState.$("#addAccountDialog").close(); domState.$("#addAccount").focus(); }

export async function leaveAccountCreate(close) {
    if (providersState.accountCreateBusy || providersState.providerBusy || codeartsState.loginBusy) return;
    if (!close) { showAccountChoices(); return; }
    providersState.accountCreateBusy = true; updateAccountCreateBusy();
    try { await releaseAuthorization(); finishAccountCreation(); }
    catch (e) { if (!isAbortError(e)) domState.$('#accountCreateError').textContent = tr('取消授权失败：') + e.message; }
    finally { providersState.accountCreateBusy = false; updateAccountCreateBusy(); }
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

export function providerLabel() { return brandName(providersState.activeProvider); }

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
    const apiOnly = !!officialAPI[name];
    domState.$("#codingImportModeField").classList.toggle("hidden", !["kimi", "zhipu", "minimax"].includes(name));
    domState.$("#codingImportMode").value = "payg";
    domState.$("#codingImportMode").options[1].textContent = name === "minimax" ? "M Plan / Subscription" : "Coding Plan / Subscription";
    domState.$("#commandcodeImportModeField").classList.toggle("hidden", name !== "commandcode");
    domState.$("#commandcodeImportMode").value = "subscription";
    domState.$("#opencodeImportScopeField").classList.toggle("hidden", name !== "opencode");
    domState.$("#opencodeImportScope").value = "zen";
    domState.$("#geminiImportModeField").classList.toggle("hidden", name !== "gemini");
    domState.$("#geminiImportMode").value = "api";
    domState.$("#providerAPIKeyField").classList.toggle("hidden", !apiOnly);
    domState.$("#providerImportJSON").classList.toggle("hidden", apiOnly);
    domState.$("#providerAPIKey").value = "";
    domState.$("#mobileclawExportHelper").classList.toggle("hidden", name !== "mobileclaw");
    domState.$("#providerTraeDevice").classList.toggle("hidden", name !== "trae");
    domState.$("#providerTeleAgentHelper").classList.toggle("hidden", name !== "teleagent");
    domState.$("#providerBrowserSteps").textContent = name === "mobileclaw" ? tr("生成二维码或复制登录链接 → 使用手机蜂窝网络确认 → 自动显示账号预览 → 确认保存。无需粘贴回调地址。") : name === "teleagent" ? tr("点击浏览器登录 → 完成官方认证 → 自动显示账号预览 → 确认保存。") : tr("生成链接 → 在另一浏览器授权 → 检查账号预览 → 确认保存。关闭窗口不会自动保存账号。");
    domState.$("#providerLoginCallback").type = name === "trae" || name === "teleagent" ? "password" : "text";
    domState.$("#providerLoginCallback").parentElement.classList.toggle("hidden", ["qoder", "kiro", "teleagent", "mobileclaw"].includes(name));
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
    domState.$("#providerImportTitle").textContent = apiOnly ? providerLabel() + " API Key" : tr("导入 ") + providerLabel() + tr(" 凭据 JSON");
    var scopeSelect = domState.$("#providerScope");
    var scopeValue = scopeSelect.value;
    scopeSelect.textContent = "";
    var scopes = name === "vertex" ? [["global", "Global"], ["us-central1", "us-central1"], ["europe-west4", "europe-west4"]] : name === "bedrock" ? [["us-east-1", "us-east-1"], ["us-west-2", "us-west-2"], ["eu-west-1", "eu-west-1"]] : ["typesafe", "cline", "commandcode"].includes(name) ? [["global", "Global"]] : name === "opencode" ? [["zen", "Zen / Pay as you go"], ["go", "Go / Subscription"]] : name === "grok" ? [["build", "Build OAuth"]] : name === "gemini" ? (providersState.accountCreateMethod === "browser" ? [["cli", "CLI / Code Assist"]] : [["api", "AI Studio API"]]) : ["teleagent", "mobileclaw", ...Object.keys(officialAPI)].includes(name) ? [["cn", tr("国内版")]] : ["chatgpt", "claude", "antigravity"].includes(name) ? [["global", tr("国际版")]] : name === "kiro" ? [["us-east-1", "us-east-1"], ["eu-central-1", "eu-central-1"]] : [["cn", tr("国内版")], ["global", tr("国际版")]];
    scopes.forEach(function (entry) { var option = document.createElement("option"); option.value = entry[0]; option.textContent = entry[1]; option.disabled = name === "trae" && entry[0] === "global"; scopeSelect.appendChild(option); });
    if (scopes.some(function (entry) { return entry[0] === scopeValue && !(name === "trae" && scopeValue === "global"); })) scopeSelect.value = scopeValue;
    const hints = {
grok: "使用 Grok Build 浏览器授权，或导入 access_token、refresh_token、expires_at、scope=build；不支持网页登录 Cookie 或普通 API Key。",
antigravity: "导入包含 access_token、refresh_token、subject、project_id、expires_at、scope=global 的完整凭据；也可使用浏览器授权。",
mobileclaw: "先在官方 MobileClaw 客户端登录，再运行 node mobileclaw-export.mjs，粘贴生成的 mobileclaw-credentials.json。当前仅个人免费 Token 通道；到期请重新导出并勾选覆盖。",
teleagent: "TeleAgent 需要 token、api_key、device_id、install_id、app_version，account_id 与 expires_at 可从 JWT 读取。使用本人获准使用的完整凭据；到期需重新登录。operation_token / oper_token 可选，用于独立积分门户。",
chatgpt: "支持 Codex auth.json 或 access_token、account_id、user_id、expires_at；也可导入官方 OpenAI api_key（sk- 开头）。浏览器授权后粘贴完整 localhost:1455/auth/callback 地址。",
claude: "支持 access_token、account_uuid、expires_at 和可选 refresh_token；也可导入官方 Anthropic api_key（sk-ant- 开头）。浏览器授权后粘贴 code#state 或完整回调地址。",
        trae: "支持 Trae 国内版扁平或 auth/account 嵌套凭据，需包含 access_token、uid、expires_at、machine_id、device_id；国际版尚未接入。",
        workbuddy: "支持已有 WorkBuddy 导出的扁平或 auth/account 嵌套 JSON。导入前会向上游核对身份。",
        kiro: "Kiro 支持 Builder ID 设备授权；也可导入 api_key（ksk_ 开头）或完整 OAuth JSON。region 默认 us-east-1；导入前校验上游模型访问。",
        qoder: "Qoder 凭据需包含 access_token、uid、expires_at、machine_id 和 scope（cn / global）。导入前会向上游核对身份。"
    };
    domState.$("#providerImportHint").textContent = tr(name === "vertex" ? "服务账号 JSON：service_account 内粘贴 Google 服务账号，location 指定区域，model_ids 填写已授权 Gemini 型号。校验会发送一次短推理请求，可能产生费用。" : name === "bedrock" ? "AWS 凭据 JSON：aws_access_key_id、aws_secret_access_key、aws_region；临时凭据另需 aws_session_token 和 expires_at。可用 model_ids 指定已授权 Claude 型号。校验会发送一次短推理请求，可能产生费用。" : name === "typesafe" ? "TypeSafe 使用独立的 /v1/systemone 决策接口，不是聊天模型。使用 API Key 校验模型访问，不发送付费决策请求。" : name === "cline" ? "使用 Cline API Key；账号校验不发送推理请求。按量、ClinePass 和免费模型分别计量，登录成功不代表所有模型有权限。" : name === "commandcode" ? "需要支持外部 API 的 Command Code 套餐或 Provider 按量账号；Go 套餐不适用。校验会发送一次短推理请求，可能消耗额度。" : name === "opencode" ? "Zen 按量与 Go 订阅分别导入。校验会发送一次短推理请求，可能消耗额度；公开目录不代表密钥有效。" : name === "gemini" ? "导入使用 AI Studio API Key；浏览器授权使用 Gemini CLI / Code Assist，两个通道独立。" : apiOnly ? "使用所选模式对应的 API Key。Kimi、Zhipu 个人版及 MiniMax 支持套餐选项；目录不可用时，导入校验会发送一次短请求，可能消耗额度。" : hints[name]);
    document.querySelector('label[for="providerLoginCallback"]').textContent = ["chatgpt", "claude", "gemini", "antigravity", "grok"].includes(name) ? tr("回调地址或授权码（必填）") : tr("回调地址（可选）");
    domState.$("#providerLoginCallback").placeholder = name === "grok" ? tr("粘贴完整 http://127.0.0.1:56121/callback?code=…&state=… 地址") : name === "antigravity" ? tr("粘贴完整 http://localhost:8085/callback?code=…&state=… 地址") : name === "gemini" ? tr("粘贴完整 codeassist.google.com/authcode 回调地址（含 code 和 state）") : name === "chatgpt" ? tr("粘贴完整 http://localhost:1455/auth/callback?code=…&state=… 地址") : name === "claude" ? tr("粘贴 code#state 或完整 Claude OAuth 回调地址") : name === "trae" ? tr("粘贴 127.0.0.1:18080/authorize?... 完整回调地址（含 loginTraceID）") : tr("完成授权后可直接检查，也可粘贴带 state 的回调地址");
}

export function openProvider(name) { if (!sessionState.adminAuthenticated) {
    showAdminLogin();
    return;
} if (providersState.providerSession && providersState.activeProvider !== name) {
    providerJob(function () { return providerCall("cancel").then(function () { resetProviderLogin(); selectProvider(name); domState.$("#providerLoginDialog").classList.remove("hidden"); }); });
    return;
} selectProvider(name); domState.$("#providerLoginDialog").classList.remove("hidden"); }

export function providerError(e) { const message = e.message || String(e);
    domState.$("#providerLoginError").textContent = message + (["gemini", "antigravity"].includes(providersState.activeProvider) && /client|配置|configured/i.test(message) ? ' ' + tr('请管理员在部署环境中配置对应 OAuth Client ID 和 Client Secret 后重试。') : ''); }

export function resetProviderLogin() { stopMobileClawQR(); clearTeleagentTask(); providersState.providerSession = ""; providersState.providerPreviewID = ""; domState.$("#providerLoginURL").value = ""; domState.$("#providerLoginCallback").value = ""; domState.$("#providerPreview").classList.add("hidden"); domState.$("#providerLoginOpen").classList.add("hidden"); domState.$("#providerLoginOpen").removeAttribute("href"); domState.$("#providerLoginCopy").disabled = true; domState.$("#providerLoginCheck").disabled = true; domState.$("#providerScope").disabled = false; setDeviceInputsLocked(false); domState.$("#providerLoginStatus").textContent = ""; domState.$("#providerLoginError").textContent = ""; domState.$("#providerReplace").checked = false; }

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
    if (providersState.activeProvider === "mobileclaw") return u.protocol === "https:" && u.host === "www.cmpassport.com" && !u.username && !u.password && !u.hash && u.pathname === "/h5/js/jssdk_yw_auth/auth/scanAuth.html";
    if (providersState.activeProvider === "teleagent") return u.protocol === "https:" && u.hostname === "www.teleai.com.cn" && !u.port && !u.username && !u.password && u.pathname === "/teleai-api/upms/public/auth/authorize";
    return u.protocol === "https:" && !u.username && !u.password && (providersState.activeProvider === "grok" ? ["auth.x.ai"] : providersState.activeProvider === "trae" ? ["trae.cn"] : ["gemini", "antigravity"].includes(providersState.activeProvider) ? ["accounts.google.com"] : providersState.activeProvider === "chatgpt" ? ["auth.openai.com"] : providersState.activeProvider === "claude" ? ["claude.com"] : providersState.activeProvider === "kiro" ? ["view.awsapps.com", "device.sso.us-east-1.amazonaws.com", "device.sso.eu-central-1.amazonaws.com"] : providersState.activeProvider === "qoder" ? ["qoder.com", "qoder.com.cn", "qoder.cn"] : ["workbuddy.cn", "workbuddy.ai", "codebuddy.cn", "codebuddy.ai", "tencent.com", "qq.com"]).some(function (h) { return u.hostname === h || u.hostname.endsWith("." + h); });
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
    domState.$("#providerPreviewText").textContent = (d.name || providerLabel() + tr(" 账号")) + " · " + d.uid + " · " + (["kiro", "gemini", "grok", "opencode"].includes(providersState.activeProvider) ? d.scope : d.scope === "global" ? tr("国际版") : tr("国内版")) + (d.existing ? tr(" · 已存在，保存将更新凭据") : tr(" · 新账号"));
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

let mobileclawQRTask = null;
function stopMobileClawQR() {
    if (mobileclawQRTask) clearTimeout(mobileclawQRTask.timer);
    mobileclawQRTask = null;
    domState.$('#mobileclawQR')?.classList.add('hidden');
    domState.$('#mobileclawQRImage')?.removeAttribute('src');
}
function startMobileClawQR(d) {
    stopMobileClawQR();
    if (typeof d.qr_code !== 'string' || d.qr_code.length > 180000 || !/^data:image\/png;base64,[A-Za-z0-9+/=]+$/.test(d.qr_code) || !Number.isSafeInteger(d.expires_at) || d.expires_at * 1000 <= Date.now()) throw new Error(tr('二维码响应无效，请重新生成'));
    domState.$('#mobileclawQRImage').src = d.qr_code;
    domState.$('#mobileclawQR').classList.remove('hidden');
    mobileclawQRTask = { session: d.session_id, expires: d.expires_at * 1000, timer: null, checking: false };
    domState.$('#mobileclawQRStatus').textContent = tr('二维码有效期至 ') + new Date(d.expires_at * 1000).toLocaleTimeString();
    mobileclawQRTask.timer = setTimeout(pollMobileClawQR, 2500);
}
async function pollMobileClawQR() {
    const task = mobileclawQRTask;
    if (!task || task.checking || providersState.providerPreviewID) return;
    clearTimeout(task.timer);
    if (Date.now() >= task.expires) { stopMobileClawQR(); domState.$('#providerLoginStatus').textContent = tr('二维码已过期，请重新生成'); return; }
    task.checking = true;
    try {
        const d = await providerCall('check', { session_id: task.session });
        if (mobileclawQRTask !== task || providersState.providerSession !== task.session) return;
        if (d.ready) { stopMobileClawQR(); showProviderPreview(d); }
        else task.timer = setTimeout(pollMobileClawQR, 2500);
    } catch (e) {
        if (mobileclawQRTask !== task) return;
        stopMobileClawQR(); if (!isAbortError(e)) providerError(e);
    } finally { task.checking = false; }
}

export function mountProviders() {
cleanup(function () { stopMobileClawQR(); stopTeleagentConnector(); providersState.teleagentTask = null; });
on("#addAccount", openAccountCreate);
document.querySelectorAll('[data-account-platform]').forEach(button => listen(button, 'click', () => { if (button.dataset.accountPlatform !== providersState.accountCreateBrand) return switchChoice(button.dataset.accountPlatform, accountTypes(button.dataset.accountPlatform)[0].id, providersState.accountCreateMethod); }));
listen(domState.$('#accountTypes'), 'click', e => { const button = e.target.closest('[data-account-type]'); if (button) { switchChoice(providersState.accountCreateBrand, button.dataset.accountType, providersState.accountCreateMethod); if (!pendingSwitch) domState.$('#accountTypes [aria-checked="true"]')?.focus(); } });
document.querySelectorAll('[data-account-method]').forEach(button => listen(button, 'click', () => switchChoice(providersState.accountCreateBrand, providersState.accountCreateType, button.dataset.accountMethod)));
listen(domState.$('#accountChoose'), 'keydown', e => {
    if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End'].includes(e.key)) return;
    const selector = e.target.matches('[data-account-platform]') ? '[data-account-platform]' : e.target.matches('[data-account-type]') ? '[data-account-type]' : null;
    if (!selector) return;
    e.preventDefault(); const items = [...domState.$('#accountChoose').querySelectorAll(selector)].filter(el => el.getClientRects().length);
    const i = items.indexOf(e.target); const next = e.key === 'Home' ? 0 : e.key === 'End' ? items.length - 1 : (i + (['ArrowLeft', 'ArrowUp'].includes(e.key) ? -1 : 1) + items.length) % items.length;
    items[next].focus(); items[next].click();
    if (selector === '[data-account-type]') domState.$('#accountTypes [aria-checked="true"]')?.focus();
});
on('#accountPlatformsMore', () => { providersState.expanded = true; renderExpansion(); domState.$('#accountExtraPlatforms [aria-checked="true"], #accountExtraPlatforms [data-account-platform]')?.focus(); });
on('#accountPlatformsLess', () => { providersState.expanded = false; renderExpansion(); domState.$('#accountPlatformsMore').focus(); });
on('#accountSelectedSummary', () => { providersState.expanded = true; renderExpansion(); const selected = domState.$('#accountExtraPlatforms [aria-checked="true"]'); selected?.focus(); selected?.scrollIntoView({block: 'nearest'}); });
on('#accountSwitchCancel', () => { pendingSwitch = null; domState.$('#accountSwitchDialog').close(); });
listen(domState.$('#accountSwitchDialog'), 'cancel', e => { if (providersState.accountCreateBusy) { e.preventDefault(); return; } pendingSwitch = null; });
on('#accountSwitchConfirm', async () => {
    const target = pendingSwitch; if (!target) return;
    providersState.accountCreateBusy = true; updateAccountCreateBusy();
    try { await releaseAuthorization(); applyChoice(target.brand, target.type, target.method); pendingSwitch = null; domState.$('#accountSwitchDialog').close(); }
    catch (e) { domState.$('#accountSwitchDialog').close(); domState.$('#accountCreateError').textContent = tr('取消授权失败：') + e.message; }
    finally { providersState.accountCreateBusy = false; updateAccountCreateBusy(); }
});
listen(domState.$('#accountCreateName'), 'input', () => { ['#providerAccountName', '#manualName', '#sessionName'].forEach(id => { domState.$(id).value = domState.$('#accountCreateName').value; }); });
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
            throw new Error(tr("授权链接不在所选上游官方域名范围")); providersState.providerSession = d.session_id; if (providersState.activeProvider === "mobileclaw") startMobileClawQR(d); domState.$("#providerLoginURL").value = d.url; domState.$("#providerLoginOpen").href = d.url; domState.$("#providerLoginOpen").classList.toggle("hidden", providersState.activeProvider === "teleagent"); if (providersState.activeProvider === "teleagent") setTeleagentTask(d); domState.$("#providerScope").disabled = true; setDeviceInputsLocked(true); domState.$("#providerLoginCopy").disabled = false; domState.$("#providerLoginCheck").disabled = false; domState.$("#providerLoginStatus").textContent = providersState.activeProvider === "mobileclaw" ? tr("请用手机扫码或打开登录链接，关闭 Wi-Fi 后确认授权。面板会自动检查，核对账号后再保存。") : providersState.activeProvider === "teleagent" ? tr("请下载助手和本次任务，在自己的电脑完成官方登录，再选择结果文件并检查；不要直接在普通浏览器打开授权链接。") : providersState.activeProvider === "trae" ? tr("请在另一浏览器完成 Trae 授权，跳转到 127.0.0.1:18080 后复制完整地址并粘贴。该地址无人监听时会显示连接失败，但地址中的授权信息仍可提交；检查后再确认保存。") : ["gemini", "antigravity"].includes(providersState.activeProvider) ? tr("完成 Google 授权后粘贴包含 code 和 state 的完整回调地址，检查项目与账号后再保存。") : providersState.activeProvider === "chatgpt" ? tr("授权后复制完整 localhost:1455/auth/callback 地址并粘贴。若页面显示连接失败，仍可复制地址栏中的回调地址；检查预览后再保存。") : providersState.activeProvider === "claude" ? tr("授权后粘贴 code#state 或完整 Claude OAuth 回调地址，检查预览后再保存。") : tr("链接已生成。请复制到另一浏览器完成授权，再点击检查。"); if (providersState.activeProvider === "teleagent" && automatic) setTimeout(function () { if (providersState.providerSession === d.session_id) automaticTeleagentLogin(providersState.teleagentTask, d.session_id); }, 0); });
    });
}
on("#providerLoginStart", function () { return startProviderAuthorization(true); });
on("#teleagentManualTask", function () { return startProviderAuthorization(false); });
on("#teleagentManualCheck", function () { domState.$("#providerLoginCheck").click(); });
on("#teleagentInstallCopy", function () { return copyText(domState.$("#teleagentInstallCommand").value, tr("安装命令已复制，请在自己的电脑执行一次")); });
on("#teleagentPair", function () { return providerJob(async function () { await pairConnector(domState.$("#teleagentPairKey").value); domState.$("#teleagentPairKey").value = ""; domState.$("#teleagentConnectorSetup").open = false; domState.$("#teleagentConnectorStatus").textContent = tr("本机组件已配对，点击浏览器登录即可开始。"); }); });
on("#teleagentForget", function () { stopTeleagentConnector(); forgetConnector(); domState.$("#teleagentPairKey").value = ""; domState.$("#teleagentConnectorSetup").open = true; domState.$("#teleagentConnectorStatus").textContent = tr("首次使用请先连接本机登录组件"); });
on("#mobileclawExportHelper", function () { downloadText("mobileclaw-export.mjs", mobileclawExportSource, "text/javascript;charset=utf-8"); });
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
on("#providerLoginCheck", function () { if (providersState.activeProvider === "mobileclaw") return pollMobileClawQR(); return providerJob(function () {
    providersState.providerPreviewID = ""; domState.$("#providerPreview").classList.add("hidden");
    return providerCall("check", { input: domState.$("#providerLoginCallback").value.trim() }).then(showProviderPreview);
}); });
on("#providerLoginComplete", function () { return providerJob(function () { return providerCall("complete", { preview_id: providersState.providerPreviewID, replace_existing: domState.$("#providerReplace").checked, name: domState.$("#providerAccountName").value.trim() }).then(refreshAfterProviderSave); }); });
listen(domState.$("#geminiImportMode"), "change", function () {
    var cli = domState.$("#geminiImportMode").value === "cli";
    domState.$("#providerAPIKeyField").classList.toggle("hidden", cli);
    domState.$("#providerImportJSON").classList.toggle("hidden", !cli);
    domState.$("#providerAPIKey").value = "";
    domState.$("#providerImportJSON").value = "";
});
on("#providerImport", function () { return providerJob(function () { var credentials; try {
    credentials = selectedType().key && ["chatgpt", "claude"].includes(providersState.activeProvider) ? {api_key: domState.$("#providerAPIKey").value.trim(), scope: "global"} : providersState.activeProvider === "typesafe" ? {api_key:domState.$("#providerAPIKey").value.trim(),scope:"global"} : officialAPI[providersState.activeProvider] && !(providersState.activeProvider === "gemini" && domState.$("#geminiImportMode").value === "cli") ? {api_key: domState.$("#providerAPIKey").value.trim(), scope:["kimi", "zhipu", "minimax"].includes(providersState.activeProvider) && domState.$("#codingImportMode").value === "coding" ? "coding-cn" : ["typesafe", "cline", "commandcode"].includes(providersState.activeProvider) ? "global" : providersState.activeProvider === "opencode" ? domState.$("#opencodeImportScope").value : providersState.activeProvider === "gemini" ? "api" : "cn", account_mode: ["kimi", "zhipu", "minimax"].includes(providersState.activeProvider) ? domState.$("#codingImportMode").value : providersState.activeProvider === "cline" ? "api" : providersState.activeProvider === "commandcode" ? domState.$("#commandcodeImportMode").value : providersState.activeProvider === "opencode" && domState.$("#opencodeImportScope").value === "go" ? "subscription" : "payg"} : JSON.parse(domState.$("#providerImportJSON").value);
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
