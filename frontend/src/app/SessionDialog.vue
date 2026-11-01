<template>
<dialog v-once id="adminLoginDialog" class="modal" aria-labelledby="adminLoginTitle">
  <div v-once id="passwordGroup"><div id="pwPanel">
    <h2 id="adminLoginTitle">管理员登录</h2>
    <p class="hint">登录后管理账号、密钥与用量。会话有效期 8 小时，退出或修改密码后失效。</p>
    <div v-once class="field"><label for="pw">管理密码</label><input id="pw" type="password" autocomplete="current-password" placeholder="输入管理密码"></div>
    <p class="hint" id="adminLoginError" role="alert"></p>
    <button class="btn primary" id="savePw">登录</button>
    <p class="hint">首次启动的密码见服务器 data 目录中的 .access.json；旧部署未设密码时会生成 state.json.admin-recovery.json。也可用 CODEARTS_ADMIN_PASSWORD 重设。</p>
  </div></div>
</dialog>
</template>
