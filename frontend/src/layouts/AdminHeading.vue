<script setup lang="ts">
import { t as tr } from '../i18n';
</script>
<template>
  <!-- Native routes own the text; reactive shell updates must not patch it. -->
  <div v-once class="page-head"><h1 id="pageTitle">{{tr('概览')}}</h1><p class="page-hint" id="pageHint">{{tr('运行状态与用量')}}</p></div>
</template>
