import { t as tr } from '../../i18n';
import { clearStoredPw } from '../../app/session';
import { on } from '../../components/controls';
import { isAbortError } from '../../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { domState, toast } from '../../shared/dom';
import { api } from '../../api/client';
import { adminLogin } from '../../app/session';
import { listen } from '../../shared/lifecycle';
import { refresh } from '../../shared/refresh';
const settingsState = {};
settingsState.settingsDirty = false;

settingsState.providerPolicyFields = [{ key: "codearts", prefix: "sCA" }, { key: "workbuddy", prefix: "sWB" }, { key: "trae", prefix: "sTR" }, { key: "qoder", prefix: "sQD" }, { key: "kiro", prefix: "sKI" }, { key: "chatgpt", prefix: "sCG" }, { key: "claude", prefix: "sCL" }, { key: "teleagent", prefix: "sTA" }];

function readSearchPolicy(key) {
 const def=settingsState.providerPolicyFields.find(item=>item.key===key), field=name=>document.getElementById(def.prefix+name);
 return {search_enabled:field('Search').checked,search_scope:key==='workbuddy'?field('SearchScope').value:'cn',search_backend:key==='workbuddy'?'native':'searxng',search_url:key==='workbuddy'?'':field('SearchURL').value.trim()};
}

export function renderSettings(s) {
    // 用户正在编辑设置时不要用服务端值覆盖 —— 否则在别的页签点一下
    // 「探活」触发的 refresh() 会静默丢掉未保存的改动。
    domState.$("#sPw").placeholder = s.admin_password_set ? tr("已设置，留空则不修改") : tr("未设置");
    if (settingsState.settingsDirty)
        return;
    settingsState.routingRevision = s.model_routing_revision;
    settingsState.providerPolicyFields.forEach(function (def) {
        if (!["codearts", "workbuddy", "trae"].includes(def.key)) return;
        var config = s[def.key] || {}, field = function (name) { return document.getElementById(def.prefix + name); };
        field("Search").checked = def.key === "workbuddy" ? config.search_enabled !== false : !!config.search_enabled;
        if (def.key === "workbuddy")
            field("SearchScope").value = config.search_scope || "cn";
        else
            field("SearchURL").value = config.search_url || "";
    });
    var p = s.pool || {};
    domState.$("#sPoolStrategy").value = p.strategy || "least_inflight";
    domState.$("#sPoolConcurrency").value = p.max_in_flight === undefined ? 2 : p.max_in_flight;
    domState.$("#sPoolSticky").value = p.sticky_ttl_sec === undefined ? 1800 : p.sticky_ttl_sec;
    domState.$("#sPoolCooldown").value = p.rate_cooldown_sec === undefined ? 60 : p.rate_cooldown_sec;
    domState.$("#sPoolThreshold").value = p.breaker_threshold === undefined ? 3 : p.breaker_threshold;
    domState.$("#sPoolBreaker").value = p.breaker_cooldown_sec === undefined ? 300 : p.breaker_cooldown_sec;
    domState.$("#sDefaultModel").value = s.default_model || "";
    domState.$("#sForceModel").value = s.force_model || "";
    domState.$("#sTimeout").value = s.request_timeout_sec || "";
    domState.$("#sRetries").value = s.max_retries || "";
    domState.$("#sLogKeep").value = s.log_keep || "";
    domState.$("#sLiveTTL").value = s.live_ttl_sec || "";
    domState.$("#sCors").value = s.cors_origin || "";
    domState.$("#sUserData").value = s.user_data_dir || "";
    domState.$("#sLoginPort").value = s.login_port || 0;
    domState.$("#sAllowCredit").checked = !!s.allow_credit_models;
    domState.$("#sAllowAnonymous").checked = !!s.allow_anonymous;
    domState.$("#sRateLimit").checked = !!s.rate_limit_enabled;
    domState.$("#sRateQPS").value = s.rate_limit_qps || "";
    domState.$("#sRateBurst").value = s.rate_limit_burst || "";
    domState.$("#sRateConc").value = (s.rate_limit_concurrency === undefined ? "" : s.rate_limit_concurrency);
    domState.$("#sAutoClaim").checked = !!s.auto_claim;
    domState.$("#sClaimHour").value = (s.claim_after_hour === undefined ? 0 : s.claim_after_hour);
    domState.$("#sClaimMinute").value = (s.claim_after_minute === undefined ? 5 : s.claim_after_minute);
    domState.$("#sAutoRefresh").checked = !!s.auto_refresh;
    domState.$("#sWriteBack").checked = !!s.write_back_to_ide;
    domState.$("#sPluginName").value = s.plugin_name || "";
    domState.$("#sPluginVersion").value = s.plugin_version || "";
    domState.$("#sIDEName").value = s.ide_name || "";
    domState.$("#sIDEVersion").value = s.ide_version || "";
    domState.$("#sAliases").value = JSON.stringify(s.model_aliases || {}, null, 2);
}

export function markSettingsDirty() {
    if (settingsState.settingsDirty)
        return;
    settingsState.settingsDirty = true;
    domState.$("#settingsDirty").classList.remove("hidden");
}

export function clearSettingsDirty() {
    settingsState.settingsDirty = false;
    domState.$("#settingsDirty").classList.add("hidden");
}

export function loadSettings() {
    return api('/admin/api/settings').then(renderSettings).catch(function(e){if(!isAbortError(e))toast(tr('加载失败：')+e.message,'err');});
}
function lockSettings(busy) {
    if(!document.querySelector('#tab-settings'))return;
    document.querySelectorAll('#tab-settings input,#tab-settings select,#tab-settings textarea,#tab-settings button').forEach(el=>el.disabled=busy);

}

export function mountSettings() {
on('#reloadSettings',function(){
    if(settingsState.settingsDirty&&!confirm(tr('重新读取将丢弃当前草稿，是否继续？')))return;
    lockSettings(true);
    return api('/admin/api/settings').then(function(s){clearSettingsDirty();domState.$('#sPw').value='';renderSettings(s);}).catch(function(e){if(!isAbortError(e))toast(tr('加载失败：')+e.message,'err');}).finally(()=>lockSettings(false));
});
on("#saveSettings", function () {
    var aliases;
    try {
        aliases = JSON.parse(domState.$("#sAliases").value || "{}");
    }
    catch {
        toast(tr("模型别名不是合法 JSON"), "err");
        return;
    }
    var body = {
        expected_routing_revision: settingsState.routingRevision,
        codearts: readSearchPolicy("codearts"), workbuddy: readSearchPolicy("workbuddy"), trae: readSearchPolicy("trae"),
        pool: {
            strategy: domState.$("#sPoolStrategy").value,
            max_in_flight: Number(domState.$("#sPoolConcurrency").value),
            sticky_ttl_sec: Number(domState.$("#sPoolSticky").value),
            rate_cooldown_sec: Number(domState.$("#sPoolCooldown").value),
            breaker_threshold: Number(domState.$("#sPoolThreshold").value),
            breaker_cooldown_sec: Number(domState.$("#sPoolBreaker").value)
        },
        model_aliases: aliases,
        default_model: domState.$("#sDefaultModel").value.trim(),
        force_model: domState.$("#sForceModel").value.trim(),
        request_timeout_sec: parseInt(domState.$("#sTimeout").value, 10) || 0,
        max_retries: parseInt(domState.$("#sRetries").value, 10) || 0,
        log_keep: parseInt(domState.$("#sLogKeep").value, 10) || 0,
        live_ttl_sec: parseInt(domState.$("#sLiveTTL").value, 10) || 0,
        cors_origin: domState.$("#sCors").value.trim(),
        user_data_dir: domState.$("#sUserData").value.trim(),
        login_port: parseInt(domState.$("#sLoginPort").value, 10) || 0,
        allow_credit_models: domState.$("#sAllowCredit").checked,
        allow_anonymous: domState.$("#sAllowAnonymous").checked,
        rate_limit_enabled: domState.$("#sRateLimit").checked,
        rate_limit_qps: parseInt(domState.$("#sRateQPS").value, 10) || 0,
        rate_limit_burst: parseInt(domState.$("#sRateBurst").value, 10) || 0,
        rate_limit_concurrency: parseInt(domState.$("#sRateConc").value, 10) || 0,
        auto_claim: domState.$("#sAutoClaim").checked,
        claim_after_hour: parseInt(domState.$("#sClaimHour").value, 10) || 0,
        claim_after_minute: parseInt(domState.$("#sClaimMinute").value, 10) || 0,
        auto_refresh: domState.$("#sAutoRefresh").checked,
        write_back_to_ide: domState.$("#sWriteBack").checked,
        plugin_name: domState.$("#sPluginName").value.trim(),
        plugin_version: domState.$("#sPluginVersion").value.trim(),
        ide_name: domState.$("#sIDEName").value.trim(),
        ide_version: domState.$("#sIDEVersion").value.trim()
    };
    var np = domState.$("#sPw").value;
    if (np !== "")
        body.admin_password = np;
    lockSettings(true);
    return api("/admin/api/settings", { method: "POST", body: body }).then(function (r) {
        domState.$("#sPw").value = "";
        toast(tr("设置已保存"), "ok");
        clearSettingsDirty();
        renderSettings(r.settings || {});
        if (np !== "")
            return adminLogin(np.trim()).then(refresh);
        return refresh();
    }).catch(function (e) { if (isAbortError(e)) return; toast(tr("保存失败：") + e.message + (e.status===409||e.status===428 ? ' · '+tr('草稿已保留，请重新读取后核对；不会自动覆盖新配置。') : ''), "err"); }).finally(()=>lockSettings(false));
});
// 设置表单一旦被编辑就标记为脏，refresh() 不再覆盖它。
["input", "change"].forEach(function (evt) {
    listen(domState.$("#tab-settings"), evt, markSettingsDirty);
});

}
