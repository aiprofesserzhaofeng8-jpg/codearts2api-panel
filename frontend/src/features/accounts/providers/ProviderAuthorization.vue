<script setup lang="ts">
import { t as tr } from '../../../i18n';
</script>
<template>
<section v-once id="providerLoginDialog" class="account-authorize-panel hidden" aria-labelledby="providerLoginTitle">
  <h3 id="providerLoginTitle" class="account-panel-title">{{tr('账号授权')}}</h3>
  <div v-once id="providerBrowserFields">
  <p class="hint" id="providerBrowserSteps">{{tr('生成链接 → 在另一浏览器授权 → 检查账号预览 → 确认保存。关闭窗口不会自动保存账号。')}}</p>
  <div v-once class="field"><label for="providerScope">{{tr('账号地区')}}</label><select id="providerScope"><option value="cn">{{tr('国内版')}}</option><option value="global">{{tr('国际版')}}</option></select></div>
  <div v-once id="providerTraeDevice" class="hidden">
    <p class="hint">{{tr('填写已登录 Trae 官方客户端的设备信息，授权后随账号保存，用于后续签到。')}}</p>
    <div v-once class="row"><button class="btn" id="providerDeviceRead">{{tr('读取本机设备信息')}}</button><select id="providerDeviceOS" :aria-label="tr('读取命令的操作系统')"><option value="windows">Windows PowerShell</option><option value="unix">{{tr('macOS / Linux 终端')}}</option></select><button class="btn" id="providerDeviceCommand">{{tr('复制读取命令')}}</button></div>
    <p class="hint">{{tr('本机指面板运行的电脑。其他电脑请复制命令到对应电脑执行；读取到客户端记录后可填入，是否能签到仍以上游结果为准。')}}</p>
    <div v-once id="providerDeviceChoices" class="field hidden"><label for="providerDeviceChoice">{{tr('选择客户端设备')}}</label><select id="providerDeviceChoice"></select></div>
    <p id="providerDeviceStatus" class="hint" role="status"></p>
    <details><summary>{{tr('粘贴命令输出并填入')}}</summary><textarea id="providerDeviceJSON" :aria-label="tr('设备信息 JSON')" :placeholder="tr('粘贴包含 machine_id 和 device_id 的 JSON')"></textarea><button class="btn" id="providerDeviceApply">{{tr('填入设备信息')}}</button></details>
    <div v-once class="field"><label for="providerMachineID">{{tr('机器标识（machine_id）')}}</label><input id="providerMachineID" autocomplete="off" spellcheck="false" maxlength="256" :placeholder="tr('粘贴官方客户端的机器标识')"></div>
    <div v-once class="field"><label for="providerDeviceID">{{tr('设备标识（device_id）')}}</label><input id="providerDeviceID" autocomplete="off" spellcheck="false" inputmode="numeric" maxlength="32" :placeholder="tr('粘贴官方客户端的数字设备号')"></div>
  </div>
  <div v-once class="row"><button class="btn primary" id="providerLoginStart">{{tr('生成登录链接')}}</button><button class="btn" id="providerLoginCopy" disabled>{{tr('复制登录链接')}}</button><a class="btn hidden" id="providerLoginOpen" target="_blank" rel="noopener noreferrer">{{tr('在浏览器打开')}}</a></div>
  <div v-once class="field"><label for="providerLoginURL">{{tr('授权链接')}}</label><input id="providerLoginURL" readonly></div>
  <div v-once id="providerTeleAgentHelper" class="hidden">
    <p class="hint">{{tr('点击浏览器登录，在官方页面完成认证后，账号预览会自动返回。核对后确认保存即可，无需上传文件。')}}</p>
    <p id="teleagentConnectorStatus" class="hint" role="status"></p>
    <details id="teleagentConnectorSetup"><summary>{{tr('首次连接 / 组件设置')}}</summary>
    <p class="hint">{{tr('首次下载组件并执行安装命令，再填写终端显示的连接码。以后组件随桌面登录自动启动；需要 Node.js 22.15+、Chrome / Edge 和已初始化的 TeleAgent 官方客户端。')}}</p>
    <button class="btn" id="teleagentHelperDownload">{{tr('下载登录组件')}}</button>
    <div class="field"><label for="teleagentInstallCommand">{{tr('首次安装命令')}}</label><input id="teleagentInstallCommand" readonly><button class="btn" id="teleagentInstallCopy">{{tr('复制安装命令')}}</button></div>
    <div class="field"><label for="teleagentPairKey">{{tr('连接码（仅首次配对）')}}</label><input id="teleagentPairKey" type="password" autocomplete="off" spellcheck="false" maxlength="43"></div>
    <div class="row"><button class="btn primary" id="teleagentPair">{{tr('连接本机组件')}}</button><button class="btn" id="teleagentForget">{{tr('解除本机配对')}}</button></div>
    </details>
    <details id="teleagentFileFallback"><summary>{{tr('高级：手动文件交接')}}</summary>
    <p class="hint">{{tr('仅在组件连接不可用时使用；正常浏览器登录无需下载任务或上传结果。')}}</p>
    <div class="row"><button class="btn" id="teleagentManualTask">{{tr('生成手动任务')}}</button><button class="btn" id="teleagentTaskDownload" disabled>{{tr('下载本次登录任务')}}</button></div>
    <div class="field"><label for="teleagentHelperCommand">{{tr('在下载目录执行')}}</label><input id="teleagentHelperCommand" readonly><button class="btn" id="teleagentCommandCopy" disabled>{{tr('复制助手命令')}}</button></div>
    <div class="field"><label for="teleagentResultFile">{{tr('选择助手生成的登录结果')}}</label><input id="teleagentResultFile" type="file" accept=".json,application/json" disabled></div>
    <button class="btn" id="teleagentManualCheck">{{tr('检查授权信息')}}</button>
    <p class="hint">{{tr('登录结果包含凭据，仅选择后提交到当前面板。检查身份并确认保存后，请删除本机结果文件；不要上传到聊天或 GitHub。账号到期需重新登录。')}}</p>
    </details>
  </div>
  <div v-once class="field"><label for="providerLoginCallback">{{tr('回调地址（可选）')}}</label><input id="providerLoginCallback" autocomplete="off" spellcheck="false" :placeholder="tr('完成授权后按提示提交回调地址')"></div>
  <button class="btn" id="providerLoginCheck" disabled>{{tr('检查授权信息')}}</button>
  <div v-once id="providerPreview" class="hidden"><p id="providerPreviewText"></p><label><input id="providerReplace" type="checkbox">{{tr('若账号已存在，确认更新它的凭据')}}</label><div class="field"><label for="providerAccountName">{{tr('账号备注（可选）')}}</label><input id="providerAccountName"></div><button class="btn primary" id="providerLoginComplete">{{tr('确认保存账号')}}</button></div>
  </div>
  <details id="providerImportSection"><summary id="providerImportTitle">{{tr('导入上游凭据 JSON')}}</summary><p class="hint" id="providerImportHint">{{tr('支持扁平或 auth/account 嵌套 JSON，导入前向上游核对身份。')}}</p><textarea id="providerImportJSON" :aria-label="tr('上游凭据 JSON')" :placeholder="tr('粘贴完整上游凭据 JSON')"></textarea><label><input id="providerImportReplace" type="checkbox">{{tr('确认覆盖已有同地区账号的凭据')}}</label><div class="row"><button class="btn" id="providerImport">{{tr('校验并导入')}}</button></div></details>
  <p id="providerLoginStatus" class="hint" role="status"></p><p id="providerLoginError" class="hint" role="alert"></p>
</section>
</template>
