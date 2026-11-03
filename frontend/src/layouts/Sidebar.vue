<script setup lang="ts">
import { t as tr } from '../i18n';
</script>
<template>
    <nav v-once id="adminNavigation" class="tabs" role="tablist" :aria-label="tr('控制台分区')">
      <button role="tab" id="nav-accounts" data-tab="accounts" aria-selected="false" aria-controls="tab-accounts">
        <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><circle cx="9" cy="8" r="3.2"/><path d="M3.5 20c0-3.2 2.5-5.6 5.5-5.6s5.5 2.4 5.5 5.6"/><path d="M16 5.4a3.2 3.2 0 0 1 0 6.2"/><path d="M17.5 14.6c2 .6 3.5 2.5 3.5 4.9"/></svg>
        <span>{{tr('账号池')}}</span><span class="nav-count" id="navAccountCount">0</span>
      </button>
      <button role="tab" id="nav-groups" data-tab="groups" aria-selected="false" aria-controls="tab-groups">
        <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><rect x="9" y="3" width="6" height="5" rx="1"/><rect x="2" y="16" width="6" height="5" rx="1"/><rect x="16" y="16" width="6" height="5" rx="1"/><path d="M12 8v4M5 16v-4h14v4"/></svg>
        <span>{{tr('账号分组')}}</span>
      </button>
      <button role="tab" id="nav-users" data-tab="users" aria-selected="false" aria-controls="tab-users">
        <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="7" r="4"/><path d="M4 21v-2a8 8 0 0 1 16 0v2"/></svg><span>{{tr('用户管理')}}</span>
      </button>
      <button role="tab" id="nav-overview" data-tab="overview" class="on" aria-selected="true" aria-controls="tab-overview">
        <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><rect x="3.5" y="3.5" width="7" height="7" rx="1.6"/><rect x="13.5" y="3.5" width="7" height="7" rx="1.6"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.6"/><rect x="13.5" y="13.5" width="7" height="7" rx="1.6"/></svg>
        <span>{{tr('用量')}}</span>
      </button>
      <button role="tab" id="nav-models" data-tab="models" aria-selected="false" aria-controls="tab-models">
        <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="m12 3 9 5-9 5-9-5 9-5Z"/><path d="m3 8 9 5 9-5M3 8v9l9 5 9-5V8M12 13v9"/></svg>
        <span>{{tr('模型')}}</span>
      </button>
      <button role="tab" id="nav-keys" data-tab="keys" aria-selected="false" aria-controls="tab-keys">
        <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><circle cx="8" cy="12" r="3.6"/><path d="M11.6 12H21"/><path d="M17.5 12v3"/><path d="M20.5 12v2"/></svg>
        <span>{{tr('API 密钥')}}</span>
      </button>
      <button role="tab" id="nav-logs" data-tab="logs" aria-selected="false" aria-controls="tab-logs">
        <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="M4 6h16"/><path d="M4 12h16"/><path d="M4 18h10"/></svg>
        <span>{{tr('运行日志')}}</span>
      </button>
      <button role="tab" id="nav-audit" data-tab="audit" aria-selected="false" aria-controls="tab-audit">
        <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3 4 6v6c0 4.2 3.3 7.4 8 9 4.7-1.6 8-4.8 8-9V6l-8-3Z"/><path d="m8.5 12 2.5 2.5 4.5-5"/></svg>
        <span>{{tr('管理审计')}}</span>
      </button>
      <button role="tab" id="nav-settings" data-tab="settings" aria-selected="false" aria-controls="tab-settings">
        <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="M4 8h8"/><path d="M16 8h4"/><path d="M4 16h4"/><path d="M12 16h8"/><circle cx="14" cy="8" r="2.2"/><circle cx="10" cy="16" r="2.2"/></svg>
        <span>{{tr('配置')}}</span>
      </button>
    </nav>
</template>
