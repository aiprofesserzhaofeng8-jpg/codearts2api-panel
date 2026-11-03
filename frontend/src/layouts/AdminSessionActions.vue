<script setup lang="ts">
import { t as tr } from '../i18n';
</script>
<template>
  <!-- Native session controls own visibility and events. -->
  <div v-once class="actions"><button class="btn ghost hidden" id="forgetPw">{{tr('退出登录')}}</button></div>
</template>
