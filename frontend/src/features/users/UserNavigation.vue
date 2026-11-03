<script setup lang="ts">
import { t as tr } from '../../i18n';

import { userPages, type UserPage } from './navigation';
defineProps<{active: UserPage}>();
defineEmits<{navigate: [page: UserPage]}>();
</script>
<template>
  <nav class="tabs user-tabs" :aria-label="tr('用户功能')">
    <button v-for="page in userPages" :key="page.id" :data-user-tab="page.id" :class="{on: active === page.id, primary: active === page.id}" :aria-pressed="active === page.id" :title="page.name" @click="$emit('navigate', page.id)">
      <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path :d="page.icon" /></svg><span>{{page.name}}</span>
    </button>
  </nav>
</template>
