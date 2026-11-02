<script setup lang="ts">
import ConsoleBrand from './ConsoleBrand.vue';
defineProps<{role: 'admin' | 'user'; title: string; description: string}>();
</script>
<template>
  <div class="auth-card">
    <ConsoleBrand />
    <div class="auth-heading"><span class="badge">{{role === 'admin' ? '管理空间' : '个人空间'}}</span><h1 :id="role === 'admin' ? 'adminLoginTitle' : undefined">{{title}}</h1><p class="hint">{{description}}</p></div>
    <slot />
    <p class="auth-switch"><a :href="role === 'admin' ? '/user' : '/'">{{role === 'admin' ? '普通用户登录' : '管理员登录' }} <span aria-hidden="true">→</span></a></p>
  </div>
</template>
