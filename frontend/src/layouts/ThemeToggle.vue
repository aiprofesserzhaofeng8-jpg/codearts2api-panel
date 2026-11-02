<script setup lang="ts">
import { ref } from 'vue';
const light = ref(document.documentElement.dataset.theme === 'light');
function toggle() {
  light.value = document.documentElement.dataset.theme !== 'light';
  const theme = light.value ? 'light' : 'dark';
  document.documentElement.dataset.theme = theme;
  try { localStorage.setItem('codearts2api_theme', theme); } catch { /* The theme also works without storage. */ }
}
</script>
<template>
  <button class="btn ghost" id="themeToggle" :aria-label="light ? '切换到深色主题' : '切换到浅色主题'" :title="light ? '切换到深色主题' : '切换到浅色主题'" @click="toggle">
    <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="M20.5 13A8.5 8.5 0 0 1 11 3.5 8.5 8.5 0 1 0 20.5 13Z"/></svg>
    <span id="themeLabel">{{light ? '深色' : '浅色'}}</span>
  </button>
</template>
