<script setup lang="ts">
import { t as tr } from './i18n';

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
import UsersPage from './features/users/UsersPage.vue';
import GrowthDialog from './features/jobs/GrowthDialog.vue';
import ConsoleLayout from './layouts/ConsoleLayout.vue';
let dispose: (() => void) | undefined;
onMounted(() => { dispose = startConsole(); });
onUnmounted(() => dispose?.());
</script>
<template>
  <GrowthDialog />
  <ConsoleLayout>
    <UsagePage />
    <ModelsPage />
    <AccountsPage />
    <GroupsPage />
    <UsersPage />
    <KeysPage />
    <LogsPage />
    <AuditPage />
    <SettingsPage />
  </ConsoleLayout>
  <div class="toast" id="toast" role="status" aria-live="polite" :title="tr('点击关闭')"></div>
</template>
