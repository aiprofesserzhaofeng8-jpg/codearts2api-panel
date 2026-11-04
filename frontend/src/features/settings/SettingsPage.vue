<script setup lang="ts">
import { t as tr } from '../../i18n';
</script>
<template>
<section v-once id="tab-settings" role="tabpanel" aria-labelledby="nav-settings">
        <div v-once class="card">
          <div class="card-head"><div><h2>{{tr('版本与更新')}}</h2><p class="card-sub">{{tr('当前运行版本 ')}}<strong id="runningVersion">{{tr('读取中')}}</strong></p></div><button class="btn" id="checkVersion">{{tr('检查更新')}}</button></div>
          <div class="card-body"><p id="versionMessage" role="status" aria-live="polite"></p><a id="releaseLink" href="https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/releases" target="_blank" rel="noopener noreferrer">{{tr('查看发布说明')}}</a><p class="hint">{{tr('更新检查缓存一分钟。升级请使用服务器上的安装工具；现有账号和配置由部署工具备份保留。')}}</p></div>
        </div>
        <div v-once class="card"><div class="card-head"><h2>{{tr('公共提示词默认值')}}</h2></div><div class="card-body">
<div class="field"><label for="sCommonPromptMode">{{tr('系统提示词')}}</label><select id="sCommonPromptMode"><option value="passthrough">{{tr('透传客户端提示词')}}</option><option value="custom">{{tr('自定义（替换 system / developer）')}}</option></select></div>
<div class="field"><label for="sCommonPromptText">{{tr('自定义提示词')}}</label><textarea id="sCommonPromptText" maxlength="65536" rows="4"></textarea></div>
<label class="check"><input id="sCommonPromptFallback" type="checkbox">{{tr('透传遇明确拦截时重试一次')}}</label>
<label class="check"><input id="sCommonSanitize" type="checkbox">{{tr('仅清洗系统提示词中的客户端模板')}}</label>
<p class="hint">{{tr('平台勾选“继承公共值”的项目才会采用这里的设置；搜索仍由各平台独立配置。')}}</p>
</div></div>
<div v-once class="card">
          <div v-once class="card-head">
            <div v-once>
              <h2>{{tr('运行设置')}}</h2>
              <p class="card-sub">{{tr('改动点「保存设置」后生效；保存前刷新页面不会覆盖未保存的编辑')}}</p>
            </div>
            <span id="settingsDirty" class="badge warn hidden">{{tr('有未保存的改动')}}</span>
          </div>

          <div v-once class="settings-group first">
            <h3>{{tr('模型与请求')}}</h3>
            <div v-once class="field-grid">
              <div v-once class="field">
                <label for="sDefaultModel">{{tr('默认模型')}}</label>
                <input id="sDefaultModel" :placeholder="tr('客户端未指定时使用')">
              </div>
              <div v-once class="field">
                <label for="sForceModel">{{tr('强制模型')}}</label>
                <input id="sForceModel" :placeholder="tr('留空=按客户端请求')">
                <span class="field-hint">{{tr('填了则一律覆盖客户端请求。')}}</span>
              </div>
              <div v-once class="field">
                <label for="sTimeout">{{tr('请求超时（秒）')}}</label>
                <input id="sTimeout" type="number" min="1">
              </div>
              <div v-once class="field">
                <label for="sRetries">{{tr('最大重试次数')}}</label>
                <input id="sRetries" type="number" min="1">
              </div>
            </div>
          </div>

          <div v-once class="settings-group">
            <h3>{{tr('账号池调度')}}</h3>
            <p class="group-hint">{{tr('会话优先复用同一账号；账号暂停、冷却或并发已满时自动改选。修改后立即生效。')}}</p>
            <div v-once class="field-grid">
              <div v-once class="field"><label for="sPoolStrategy">{{tr('选号策略')}}</label><select id="sPoolStrategy"><option value="least_inflight">{{tr('最少在途请求')}}</option><option value="round_robin">{{tr('轮询')}}</option><option value="sequential_exhaust">{{tr('顺序耗尽')}}</option></select></div>
              <div v-once class="field"><label for="sPoolConcurrency">{{tr('单账号默认并发上限')}}</label><input id="sPoolConcurrency" type="number" min="1" max="32"></div>
              <div v-once class="field"><label for="sPoolSticky">{{tr('会话绑定时长（秒）')}}</label><input id="sPoolSticky" type="number" min="0" max="86400"><span class="field-hint">{{tr('0 表示关闭会话粘性。')}}</span></div>
              <div v-once class="field"><label for="sPoolCooldown">{{tr('限流冷却（秒）')}}</label><input id="sPoolCooldown" type="number" min="0" max="86400"></div>
              <div v-once class="field"><label for="sPoolThreshold">{{tr('连续失败熔断次数')}}</label><input id="sPoolThreshold" type="number" min="1" max="100"></div>
              <div v-once class="field"><label for="sPoolBreaker">{{tr('熔断恢复等待（秒）')}}</label><input id="sPoolBreaker" type="number" min="1" max="86400"></div>
            </div>
          </div>
          <div v-once class="settings-group">
            <h3>{{tr('存储与刷新')}}</h3>
            <div v-once class="field-grid">
              <div v-once class="field">
                <label for="sLogKeep">{{tr('日志保留条数')}}</label>
                <input id="sLogKeep" type="number" min="1">
              </div>
              <div v-once class="field">
                <label for="sLiveTTL">{{tr('IDE 会话重读间隔（秒）')}}</label>
                <input id="sLiveTTL" type="number" min="1">
              </div>
              <div v-once class="field">
                <label for="sUserData">{{tr('CodeArts Agent User Data 目录')}}</label>
                <input id="sUserData" placeholder="%APPDATA%\codearts-agent">
                <span class="field-hint">{{tr('留空自动探测。')}}</span>
              </div>
              <div v-once class="field">
                <label for="sLoginPort">{{tr('浏览器登录回调端口')}}</label>
                <input id="sLoginPort" type="number" min="0" max="65535" placeholder="0">
                <span class="field-hint">{{tr('0=随机；远端部署建议固定，如 64361。')}}</span>
              </div>
            </div>
          </div>

          <div v-once class="settings-group">
            <h3>{{tr('网络')}}</h3>
            <div v-once class="field-grid">
              <div v-once class="field">
                <label for="sCors">{{tr('CORS 允许来源')}}</label>
                <input id="sCors" placeholder="http://192.168.1.10:8080">
                <span class="field-hint">{{tr('逗号分隔；留空=仅同源。')}}</span>
              </div>
            </div>
          </div>

          <div v-once class="settings-group">
            <h3>{{tr('限流')}}</h3>
            <label class="check" for="sRateLimit">
              <input type="checkbox" id="sRateLimit">
              <span>{{tr('启用按密钥限流 ')}}<span class="check-hint">{{tr('只做自我保护：防止失控客户端（循环重试、Agent 死循环）烧光账号积分，')}}<strong>{{tr('不')}}</strong>{{tr('承诺密钥之间的公平配额。超限返回 ')}}<code>429</code> + <code>Retry-After</code>{{tr('，被拦下的请求不消耗额度。')}}</span>
              </span>
            </label>
            <div v-once class="field-grid">
              <div v-once class="field">
                <label for="sRateQPS">{{tr('每密钥 QPS')}}</label>
                <input id="sRateQPS" type="number" min="1">
                <span class="field-hint">{{tr('长期允许的请求速率。')}}</span>
              </div>
              <div v-once class="field">
                <label for="sRateBurst">{{tr('突发上限')}}</label>
                <input id="sRateBurst" type="number" min="1">
                <span class="field-hint">{{tr('瞬时允许的请求数。')}}</span>
              </div>
              <div v-once class="field">
                <label for="sRateConc">{{tr('并发上限')}}</label>
                <input id="sRateConc" type="number" min="0">
                <span class="field-hint">{{tr('同一密钥的在飞请求数；0 = 不限。')}}</span>
              </div>
            </div>
          </div>

          <div v-once class="settings-group">
            <h3>{{tr('计费开关')}}</h3>
            <label class="check" for="sAllowCredit">
              <input type="checkbox" id="sAllowCredit">
              <span>{{tr('允许真实消耗积分 ')}}<span class="check-hint">{{tr('免费权益通道已下线，所有模型都会计费。关闭它等于拒绝一切 ')}}<code>/v1/*</code>{{tr(' 模型调用（403），只在临时停服/排障时使用。')}}</span>
              </span>
            </label>
          </div>

          <div v-once class="settings-group">
            <h3>{{tr('访问控制')}}</h3>
            <label class="check" for="sAllowAnonymous">
              <input type="checkbox" id="sAllowAnonymous">
              <span>{{tr('未签发密钥时允许匿名调用 ')}}<code>/v1/*</code>
                <span class="check-hint">{{tr('默认只允许本机直连。若网关监听 0.0.0.0（Docker 默认）或被反向代理，勾选它等于把账号额度开放给任何能访问该端口的人 —— 更好的做法是在「密钥」页签发一个 API 密钥。')}}</span>
              </span>
            </label>
          </div>

          <div v-once class="settings-group">
            <h3>{{tr('活动福利自动领取')}}</h3>
            <label class="check" for="sAutoClaim">
              <input type="checkbox" id="sAutoClaim">
              <span>{{tr('每天自动领取码道福利及 WorkBuddy / Trae 每日签到')}}</span>
            </label>
            <div v-once class="row mt-12">
              <span class="field-label">{{tr('每日领取时刻')}}</span>
              <input class="w-hour" id="sClaimHour" type="number" min="0" max="23" :aria-label="tr('领取小时')">
              <span class="sep muted">:</span>
              <input class="w-hour" id="sClaimMinute" type="number" min="0" max="59" :aria-label="tr('领取分钟')">
            </div>
            <p class="hint">{{tr(' 运营活动的权益按自然日刷新，所以默认 00:05 领。WorkBuddy / Trae 按 UTC+8 日期去重，失败后等待 30 分钟再试；本地记录和上游复查共同避免重复提交。')}}<br>{{tr(' 仅接入每日签到及码道已支持的活动；邀请、成长任务与学生认证须满足真实条件，不代为伪造任务。 ')}}</p>
          </div>

          <div v-once class="settings-group">
            <h3>{{tr('凭据续期')}}</h3>
            <label class="check" for="sAutoRefresh">
              <input type="checkbox" id="sAutoRefresh">
              <span>{{tr('过期时自行用 refresh_token 续期')}}</span>
            </label>
            <label class="check mt-12" for="sWriteBack">
              <input type="checkbox" id="sWriteBack">
              <span>{{tr('续期后回写 state.vscdb（让 IDE 复用）')}}</span>
            </label>
            <p class="hint">{{tr(' IDE 运行时它会自己续期，网关会跟随读取；只有 IDE 关闭且凭据过期时才会用到这里的续期能力。')}}<br>{{tr(' 回写仅在长度完全一致时做原地字节覆写，并在写入前备份 ')}}<code>state.vscdb.codearts2api.bak</code>。
            </p>
          </div>

          <div v-once class="settings-group">
            <h3>{{tr('上游身份头')}}</h3>
            <p class="group-hint">{{tr('留空使用与 CodeArts Agent 26.9.902 对齐的缺省值。')}}</p>
            <div v-once class="field-grid">
              <div v-once class="field">
                <label for="sPluginName">plugin-name</label>
                <input id="sPluginName">
              </div>
              <div v-once class="field">
                <label for="sPluginVersion">plugin-version</label>
                <input id="sPluginVersion">
              </div>
              <div v-once class="field">
                <label for="sIDEName">ide-name</label>
                <input id="sIDEName">
              </div>
              <div v-once class="field">
                <label for="sIDEVersion">ide-version</label>
                <input id="sIDEVersion">
              </div>
            </div>
          </div>

          <div v-once class="settings-group">
            <h3>CodeArts</h3>
            <label class="check prompt-inherit"><input id="sCAPromptModeInherit" type="checkbox">{{tr('继承公共值 · 模式')}}</label>
<div v-once class="field"><label for="sCAPromptMode">{{tr('系统提示词')}}</label><select id="sCAPromptMode"><option value="passthrough">{{tr('透传客户端提示词')}}</option><option value="custom">{{tr('自定义（替换客户端 system / developer）')}}</option></select></div>
            <label class="check prompt-inherit"><input id="sCAPromptTextInherit" type="checkbox">{{tr('继承公共值 · 提示词')}}</label>
<div v-once class="field"><label for="sCAPromptText">{{tr('自定义提示词（留空使用内置工程助手提示词）')}}</label><textarea id="sCAPromptText" maxlength="65536" rows="5" :placeholder="tr('输入系统提示词')"></textarea></div>
            <div v-once class="row">
              <label class="check prompt-inherit"><input id="sCAPromptFallbackInherit" type="checkbox">{{tr('继承公共值 · 拦截恢复')}}</label>
<label class="check"><input id="sCAPromptFallback" type="checkbox">{{tr('透传遇明确内容策略拦截时，使用中性提示词重试一次')}}</label>
              <label class="check prompt-inherit"><input id="sCASanitizeInherit" type="checkbox">{{tr('继承公共值 · 清洗')}}</label>
<label class="check"><input id="sCASanitize" type="checkbox">{{tr('仅清洗系统提示词中的客户端模板')}}</label>
            </div>
            <div v-once class="field"><label for="sCASearchURL">{{tr('独立搜索后端 · SearXNG')}}</label><input id="sCASearchURL" type="url" maxlength="2048" placeholder="http://127.0.0.1:8080/search"></div>
            <p class="group-hint">{{tr('使用启用 JSON 输出的 SearXNG 服务；不使用 CodeArts 原生搜索或其他平台账号。')}}</p>
            <label class="check"><input id="sCASearch" type="checkbox">{{tr('启用此平台的联网搜索')}}</label>
          </div>

          <div v-once class="settings-group">
            <h3>WorkBuddy</h3>
            <div v-once class="field-grid cols-2">
              <label class="check prompt-inherit"><input id="sWBPromptModeInherit" type="checkbox">{{tr('继承公共值 · 模式')}}</label>
<div v-once class="field"><label for="sWBPromptMode">{{tr('系统提示词')}}</label><select id="sWBPromptMode"><option value="custom">{{tr('自定义（替换客户端 system / developer）')}}</option><option value="passthrough">{{tr('透传客户端提示词')}}</option></select></div>
              <div v-once class="field"><label for="sWBSearchScope">{{tr('默认搜索地区')}}</label><select id="sWBSearchScope"><option value="cn">{{tr('国内')}}</option><option value="global">{{tr('国际')}}</option></select></div>
            </div>
            <label class="check prompt-inherit"><input id="sWBPromptTextInherit" type="checkbox">{{tr('继承公共值 · 提示词')}}</label>
<div v-once class="field"><label for="sWBPromptText">{{tr('自定义提示词（留空使用内置工程助手提示词）')}}</label><textarea id="sWBPromptText" maxlength="65536" rows="5" :placeholder="tr('输入系统提示词')"></textarea></div>
            <div v-once class="row">
              <label class="check"><input id="sWBSearch" type="checkbox">{{tr('启用联网搜索')}}</label>
              <label class="check prompt-inherit"><input id="sWBPromptFallbackInherit" type="checkbox">{{tr('继承公共值 · 拦截恢复')}}</label>
<label class="check"><input id="sWBPromptFallback" type="checkbox">{{tr('透传遇内容策略拦截时，使用中性提示词重试一次')}}</label>
              <label class="check prompt-inherit"><input id="sWBSanitizeInherit" type="checkbox">{{tr('继承公共值 · 清洗')}}</label>
<label class="check"><input id="sWBSanitize" type="checkbox">{{tr('仅清洗系统提示词中的客户端模板')}}</label>
            </div>
          </div>

          <div v-once class="settings-group">
            <h3>Trae</h3>
            <label class="check prompt-inherit"><input id="sTRPromptModeInherit" type="checkbox">{{tr('继承公共值 · 模式')}}</label>
<div v-once class="field"><label for="sTRPromptMode">{{tr('系统提示词')}}</label><select id="sTRPromptMode"><option value="passthrough">{{tr('透传客户端提示词')}}</option><option value="custom">{{tr('自定义（替换客户端 system / developer）')}}</option></select></div>
            <label class="check prompt-inherit"><input id="sTRPromptTextInherit" type="checkbox">{{tr('继承公共值 · 提示词')}}</label>
<div v-once class="field"><label for="sTRPromptText">{{tr('自定义提示词（留空使用内置工程助手提示词）')}}</label><textarea id="sTRPromptText" maxlength="65536" rows="5" :placeholder="tr('输入系统提示词')"></textarea></div>
            <div v-once class="row">
              <label class="check prompt-inherit"><input id="sTRPromptFallbackInherit" type="checkbox">{{tr('继承公共值 · 拦截恢复')}}</label>
<label class="check"><input id="sTRPromptFallback" type="checkbox">{{tr('透传遇明确内容策略拦截时，使用中性提示词重试一次')}}</label>
              <label class="check prompt-inherit"><input id="sTRSanitizeInherit" type="checkbox">{{tr('继承公共值 · 清洗')}}</label>
<label class="check"><input id="sTRSanitize" type="checkbox">{{tr('仅清洗系统提示词中的客户端模板')}}</label>
            </div>
            <div v-once class="field"><label for="sTRSearchURL">{{tr('独立搜索后端 · SearXNG')}}</label><input id="sTRSearchURL" type="url" maxlength="2048" placeholder="http://127.0.0.1:8080/search"></div>
            <p class="group-hint">{{tr('使用启用 JSON 输出的 SearXNG 服务；不使用 Trae 原生搜索或其他平台账号。')}}</p>
            <label class="check"><input id="sTRSearch" type="checkbox">{{tr('启用此平台的联网搜索')}}</label>
          </div>

          <div v-once class="settings-group">
            <h3>Qoder</h3>
            <label class="check prompt-inherit"><input id="sQDPromptModeInherit" type="checkbox">{{tr('继承公共值 · 模式')}}</label>
<div v-once class="field"><label for="sQDPromptMode">{{tr('系统提示词')}}</label><select id="sQDPromptMode"><option value="passthrough">{{tr('透传客户端提示词')}}</option><option value="custom">{{tr('自定义（替换客户端 system / developer）')}}</option></select></div>
            <label class="check prompt-inherit"><input id="sQDPromptTextInherit" type="checkbox">{{tr('继承公共值 · 提示词')}}</label>
<div v-once class="field"><label for="sQDPromptText">{{tr('自定义提示词（留空使用内置工程助手提示词）')}}</label><textarea id="sQDPromptText" maxlength="65536" rows="5" :placeholder="tr('输入系统提示词')"></textarea></div>
            <div v-once class="row">
              <label class="check prompt-inherit"><input id="sQDPromptFallbackInherit" type="checkbox">{{tr('继承公共值 · 拦截恢复')}}</label>
<label class="check"><input id="sQDPromptFallback" type="checkbox">{{tr('透传遇明确内容策略拦截时，使用中性提示词重试一次')}}</label>
              <label class="check prompt-inherit"><input id="sQDSanitizeInherit" type="checkbox">{{tr('继承公共值 · 清洗')}}</label>
<label class="check"><input id="sQDSanitize" type="checkbox">{{tr('仅清洗系统提示词中的客户端模板')}}</label>
            </div>
          </div>

          <div v-once class="settings-group">
            <h3>Kiro</h3>
            <label class="check prompt-inherit"><input id="sKIPromptModeInherit" type="checkbox">{{tr('继承公共值 · 模式')}}</label>
<div v-once class="field"><label for="sKIPromptMode">{{tr('系统提示词')}}</label><select id="sKIPromptMode"><option value="passthrough">{{tr('透传客户端提示词')}}</option><option value="custom">{{tr('自定义（替换客户端 system / developer）')}}</option></select></div>
            <label class="check prompt-inherit"><input id="sKIPromptTextInherit" type="checkbox">{{tr('继承公共值 · 提示词')}}</label>
<div v-once class="field"><label for="sKIPromptText">{{tr('自定义提示词（留空使用内置工程助手提示词）')}}</label><textarea id="sKIPromptText" maxlength="65536" rows="5" :placeholder="tr('输入系统提示词')"></textarea></div>
            <div v-once class="row">
              <label class="check prompt-inherit"><input id="sKIPromptFallbackInherit" type="checkbox">{{tr('继承公共值 · 拦截恢复')}}</label>
<label class="check"><input id="sKIPromptFallback" type="checkbox">{{tr('透传遇明确内容策略拦截时，使用中性提示词重试一次')}}</label>
              <label class="check prompt-inherit"><input id="sKISanitizeInherit" type="checkbox">{{tr('继承公共值 · 清洗')}}</label>
<label class="check"><input id="sKISanitize" type="checkbox">{{tr('仅清洗系统提示词中的客户端模板')}}</label>
            </div>
          </div>
          <div v-once class="settings-group">
            <h3>ChatGPT / Codex</h3>
            <label class="check prompt-inherit"><input id="sCGPromptModeInherit" type="checkbox">{{tr('继承公共值 · 模式')}}</label>
<div v-once class="field"><label for="sCGPromptMode">{{tr('系统提示词')}}</label><select id="sCGPromptMode"><option value="passthrough">{{tr('透传客户端提示词')}}</option><option value="custom">{{tr('自定义（替换客户端 system / developer）')}}</option></select></div>
            <label class="check prompt-inherit"><input id="sCGPromptTextInherit" type="checkbox">{{tr('继承公共值 · 提示词')}}</label>
<div v-once class="field"><label for="sCGPromptText">{{tr('自定义提示词（留空使用内置工程助手提示词）')}}</label><textarea id="sCGPromptText" maxlength="65536" rows="5" :placeholder="tr('输入系统提示词')"></textarea></div>
            <div v-once class="row">
              <label class="check prompt-inherit"><input id="sCGPromptFallbackInherit" type="checkbox">{{tr('继承公共值 · 拦截恢复')}}</label>
<label class="check"><input id="sCGPromptFallback" type="checkbox">{{tr('透传遇明确内容策略拦截时，使用中性提示词重试一次')}}</label>
              <label class="check prompt-inherit"><input id="sCGSanitizeInherit" type="checkbox">{{tr('继承公共值 · 清洗')}}</label>
<label class="check"><input id="sCGSanitize" type="checkbox">{{tr('仅清洗系统提示词中的客户端模板')}}</label>
            </div>
          </div>
          <div v-once class="settings-group">
            <h3>Claude</h3>
            <label class="check prompt-inherit"><input id="sCLPromptModeInherit" type="checkbox">{{tr('继承公共值 · 模式')}}</label>
<div v-once class="field"><label for="sCLPromptMode">{{tr('系统提示词')}}</label><select id="sCLPromptMode"><option value="passthrough">{{tr('透传客户端提示词')}}</option><option value="custom">{{tr('自定义（替换客户端 system / developer）')}}</option></select></div>
            <label class="check prompt-inherit"><input id="sCLPromptTextInherit" type="checkbox">{{tr('继承公共值 · 提示词')}}</label>
<div v-once class="field"><label for="sCLPromptText">{{tr('自定义提示词（留空使用内置工程助手提示词）')}}</label><textarea id="sCLPromptText" maxlength="65536" rows="5" :placeholder="tr('输入系统提示词')"></textarea></div>
            <div v-once class="row">
              <label class="check prompt-inherit"><input id="sCLPromptFallbackInherit" type="checkbox">{{tr('继承公共值 · 拦截恢复')}}</label>
<label class="check"><input id="sCLPromptFallback" type="checkbox">{{tr('透传遇明确内容策略拦截时，使用中性提示词重试一次')}}</label>
              <label class="check prompt-inherit"><input id="sCLSanitizeInherit" type="checkbox">{{tr('继承公共值 · 清洗')}}</label>
<label class="check"><input id="sCLSanitize" type="checkbox">{{tr('仅清洗系统提示词中的客户端模板')}}</label>
            </div>
          </div>
          <div v-once class="settings-group">
            <h3>TeleAgent</h3>
            <label class="check prompt-inherit"><input id="sTAPromptModeInherit" type="checkbox">{{tr('继承公共值 · 模式')}}</label>
<div v-once class="field"><label for="sTAPromptMode">{{tr('系统提示词')}}</label><select id="sTAPromptMode"><option value="passthrough">{{tr('透传客户端提示词')}}</option><option value="custom">{{tr('自定义（替换客户端 system / developer）')}}</option></select></div>
            <label class="check prompt-inherit"><input id="sTAPromptTextInherit" type="checkbox">{{tr('继承公共值 · 提示词')}}</label>
<div v-once class="field"><label for="sTAPromptText">{{tr('自定义提示词（留空使用内置工程助手提示词）')}}</label><textarea id="sTAPromptText" maxlength="65536" rows="5" :placeholder="tr('输入系统提示词')"></textarea></div>
            <div v-once class="row">
              <label class="check prompt-inherit"><input id="sTAPromptFallbackInherit" type="checkbox">{{tr('继承公共值 · 拦截恢复')}}</label>
<label class="check"><input id="sTAPromptFallback" type="checkbox">{{tr('透传遇明确内容策略拦截时，使用中性提示词重试一次')}}</label>
              <label class="check prompt-inherit"><input id="sTASanitizeInherit" type="checkbox">{{tr('继承公共值 · 清洗')}}</label>
<label class="check"><input id="sTASanitize" type="checkbox">{{tr('仅清洗系统提示词中的客户端模板')}}</label>
            </div>
          </div>

          <div v-once class="settings-group">
            <h3>{{tr('模型别名')}}</h3>
            <p class="group-hint">{{tr('JSON 对象，例如 ')}}<code>{"my-model":"glm-5.3"}</code>{{tr('。别名只改变名称，不改变实际调用的模型。')}}</p>
            <textarea id="sAliases" spellcheck="false" :aria-label="tr('模型别名 JSON')"></textarea>
          </div>

          <div v-once class="settings-group">
            <h3>{{tr('管理密码')}}</h3>
            <p class="group-hint">{{tr('留空=不修改；设置后管理接口必须携带。')}}</p>
            <div v-once class="row">
              <input class="w-280" id="sPw" type="password" :placeholder="tr('不修改请留空')" autocomplete="new-password"
                     :aria-label="tr('新的管理密码')">
            </div>
            <p class="hint">{{tr(' 清除后管理接口退回「仅本机直连」兜底；若网关监听 ')}}<code>0.0.0.0</code>{{tr(' 或被反向代理， 控制台将无法访问。 ')}}</p>
          </div>

          <div v-once class="row between mt-14">
            <button class="btn primary" id="saveSettings">{{tr('保存设置')}}</button>
            <button class="btn danger" id="clearStats">{{tr('重置统计')}}</button>
          </div>
        </div>
      </section>
</template>
