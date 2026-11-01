<script setup lang="ts">
import { onMounted, onUnmounted } from "vue";
import { startConsole } from "./app/bootstrap";
import AccountsPage from './features/accounts/AccountsPage.vue';
import GroupsPage from './features/groups/GroupsPage.vue';
import KeysPage from './features/keys/KeysPage.vue';
import UsagePage from './features/usage/UsagePage.vue';
import ModelsPage from './features/models/ModelsPage.vue';
import LogsPage from './features/logs/LogsPage.vue';
import AuditPage from './features/logs/AuditPage.vue';
import SettingsPage from './features/settings/SettingsPage.vue';
import GrowthDialog from './features/jobs/GrowthDialog.vue';
import SessionDialog from './app/SessionDialog.vue';
import Sidebar from './layouts/Sidebar.vue';
import Topbar from './layouts/Topbar.vue';
let dispose: (() => void) | undefined;
onMounted(() => { dispose = startConsole(); });
onUnmounted(() => dispose?.());
</script>
<template>
<a class="skip" href="#main">跳到主要内容</a>

<SessionDialog />






<GrowthDialog />



<div class="app">
  <Sidebar />

  <div class="main">
    <Topbar />





    <main id="main">
      <UsagePage />

      <ModelsPage />

      <AccountsPage />

      <GroupsPage />

      <KeysPage />

      <LogsPage />

      <AuditPage />
      <SettingsPage />
    </main>
  </div>
</div>




<div class="toast" id="toast" role="status" aria-live="polite" title="点击关闭"></div>


</template>
