<script setup lang="ts">
import ThemeToggle from './ThemeToggle.vue';
import AdminHeading from './AdminHeading.vue';
import AdminSessionActions from './AdminSessionActions.vue';
defineProps<{role: 'admin' | 'user'; name: string; open: boolean}>();
defineEmits<{menu: []}>();
</script>
<template>
  <header class="topbar">
    <button id="consoleMenuToggle" class="btn ghost console-menu-toggle" aria-label="打开导航" :aria-expanded="open" aria-controls="consoleSidebar" @click="$emit('menu')"><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="M4 6h16M4 12h16M4 18h16"/></svg></button>
    <AdminHeading v-if="role === 'admin'" />
    <div v-else class="page-head"><slot name="heading" /></div>
    <div class="topbar-actions">
      <ThemeToggle />
      <span class="console-role badge" :title="name || '管理员'">{{role === 'admin' ? '管理员' : name}}</span>
      <AdminSessionActions v-if="role === 'admin'" />
      <slot v-else name="actions" />
    </div>
  </header>
</template>
