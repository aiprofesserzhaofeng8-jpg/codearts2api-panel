<script setup lang="ts">
import UserIcon from './UserIcon.vue';
withDefaults(defineProps<{label:string; value:string; note?:string; icon:string; tone?:string}>(), {tone:'accent',note:''});
</script>
<template>
  <div class="portal-stat">
    <div class="portal-stat-top"><span>{{label}}</span><span class="portal-stat-icon" :class="'tone-'+tone"><UserIcon :name="icon" /></span></div>
    <strong>{{value}}</strong><p v-if="note">{{note}}</p>
  </div>
</template>
