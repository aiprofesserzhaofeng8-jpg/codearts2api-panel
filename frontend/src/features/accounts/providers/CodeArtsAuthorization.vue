<template>
<section v-once id="loginPanel" class="account-authorize-panel hidden" aria-labelledby="loginTitle">
  <h3 id="loginTitle" class="account-panel-title">码道账号授权</h3>
  <div v-once class="modal-body">
    <div v-once class="login-stepper" aria-label="登录步骤"><span id="loginStep1" class="active">1 获取链接</span><span id="loginStep2">2 提交授权信息</span><span id="loginStep3">3 确认保存</span></div>
    <div v-once id="loginInitial"><h3>准备登录链接</h3><p class="hint">点击下方按钮生成一次性链接。可以复制到其他浏览器或无痕窗口登录，避免沿用当前浏览器的账号。</p><button class="btn primary" id="loginGenerate">生成登录链接</button></div>
    <div v-once id="loginDetails" class="hidden">
      <label class="field" for="loginURL">登录链接<input id="loginURL" readonly data-select-on-click="1"></label>
      <div v-once class="row mt-12"><button class="btn primary" id="loginCopy">复制登录链接</button><a class="btn hidden" id="loginLink" href="#" target="_blank" rel="noopener noreferrer">在当前浏览器打开</a></div>
      <p class="hint">在另一浏览器完成授权后，复制最后的回调地址并粘贴到下方。如果本机已自动收到回调，留空点击「检查授权信息」。此时不会自动添加账号。</p>
      <label class="field mt-14" for="loginPasted">回调 URL / OAuth 授权码 / 完整 session JSON<textarea id="loginPasted" rows="4" spellcheck="false" autocomplete="off" placeholder="粘贴 http://127.0.0.1:端口/oauth/callback?...，或完整的登录凭据 JSON"></textarea></label>
      <div v-once class="row mt-12"><button class="btn primary" id="loginSubmit">提交授权信息</button><button class="btn" id="loginCheck">检查授权信息</button></div>
      <details class="login-advanced mt-14"><summary>远程部署与回调地址</summary><p class="hint">本次回调地址：<code id="loginCallback">—</code></p><label class="field" for="loginTunnel">可选 SSH 转发命令<input id="loginTunnel" readonly></label><button class="btn xs mt-8" id="copyTunnel">复制命令</button></details>
    </div>
    <div v-once id="loginStatus" class="modal-status" role="status" aria-live="polite">先生成链接，再到另一浏览器完成授权。</div>
    <div v-once id="loginPreview" class="login-preview hidden"><h3>确认将要保存的账号</h3><strong id="loginAccountName"></strong><p id="loginAccountInfo" class="hint"></p><label id="loginReplaceLabel" class="check hidden"><input id="loginReplace" type="checkbox">此账号已存在，确认更新它的登录凭据</label></div>
    <p id="loginError" class="err-text hidden" role="alert"></p>
  </div>
  <div v-once class="modal-actions"><button class="btn danger hidden" id="loginCancel">取消本次授权</button><button class="btn hidden" id="loginRestart">重新生成链接</button><button class="btn primary hidden" id="loginComplete">确认完成登录</button></div>
</section>
</template>
