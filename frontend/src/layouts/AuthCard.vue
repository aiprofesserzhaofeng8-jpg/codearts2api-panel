<script setup lang="ts">
import ConsoleBrand from './ConsoleBrand.vue';
defineProps<{title: string; description: string}>();
</script>
<template>
  <div class="auth-card">
    <ConsoleBrand />
    <div class="auth-heading"><h1>{{title}}</h1><p class="hint">{{description}}</p></div>
    <slot />
  </div>
</template>
