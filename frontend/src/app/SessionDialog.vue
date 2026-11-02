<script setup lang="ts">
import AuthCard from '../layouts/AuthCard.vue';
</script>
<template>
<dialog v-once id="adminLoginDialog" class="modal" aria-labelledby="adminLoginTitle">
  <div v-once id="passwordGroup"><div id="pwPanel">
    <AuthCard role="admin" title="管理员登录" description="管理账号、模型、用户与运行配置。">
    <div v-once class="field"><label for="pw">管理密码</label><input id="pw" type="password" autocomplete="current-password" placeholder="输入管理密码"></div>
    <p class="hint" id="adminLoginError" role="alert"></p>
    <button class="btn primary" id="savePw">登录</button>
    <p class="hint">会话有效期 8 小时，退出或修改密码后失效。</p>
    <details class="auth-help"><summary>首次登录或忘记管理密码？</summary><p class="hint">首次密码见服务器 data 目录中的 .access.json；旧部署的恢复文件为 state.json.admin-recovery.json。也可用 CODEARTS_ADMIN_PASSWORD 重设。</p></details>
    </AuthCard>
  </div></div>
</dialog>
</template>
