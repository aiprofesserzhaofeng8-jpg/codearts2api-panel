<script setup lang="ts">
defineProps<{ searchLabel: string }>();
const query = defineModel<string>({required:true});
</script>
<template>
  <div class="record-toolbar">
    <label class="record-search">
      <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/></svg>
      <input v-model="query" type="search" :placeholder="searchLabel" :aria-label="searchLabel">
    </label>
    <slot />
    <div class="record-command"><slot name="action" /></div>
  </div>
</template>
