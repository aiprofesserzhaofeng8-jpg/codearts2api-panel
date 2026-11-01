<template>
<aside v-once class="sidebar">
    <div v-once class="brand">
      <span class="brand-mark">
        <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3.2 20 7.7v8.6L12 20.8 4 16.3V7.7z"/><path d="M4 7.7 12 12.2l8-4.5"/><path d="M12 12.2v8.6"/></svg>
      </span>
      <span class="brand-text">
        <span class="brand-name">CodeArts2API</span>
      </span>
    </div>

    <nav class="tabs" role="tablist" aria-label="控制台分区">
      <button role="tab" id="nav-accounts" data-tab="accounts" aria-selected="false" aria-controls="tab-accounts">
        <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><circle cx="9" cy="8" r="3.2"/><path d="M3.5 20c0-3.2 2.5-5.6 5.5-5.6s5.5 2.4 5.5 5.6"/><path d="M16 5.4a3.2 3.2 0 0 1 0 6.2"/><path d="M17.5 14.6c2 .6 3.5 2.5 3.5 4.9"/></svg>
        <span>账号池</span><span class="nav-count" id="navAccountCount">0</span>
      </button>
      <button role="tab" id="nav-groups" data-tab="groups" aria-selected="false" aria-controls="tab-groups">
        <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><rect x="9" y="3" width="6" height="5" rx="1"/><rect x="2" y="16" width="6" height="5" rx="1"/><rect x="16" y="16" width="6" height="5" rx="1"/><path d="M12 8v4M5 16v-4h14v4"/></svg>
        <span>账号分组</span>
      </button>
      <button role="tab" id="nav-overview" data-tab="overview" class="on" aria-selected="true" aria-controls="tab-overview">
        <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><rect x="3.5" y="3.5" width="7" height="7" rx="1.6"/><rect x="13.5" y="3.5" width="7" height="7" rx="1.6"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.6"/><rect x="13.5" y="13.5" width="7" height="7" rx="1.6"/></svg>
        <span>用量</span>
      </button>
      <button role="tab" id="nav-models" data-tab="models" aria-selected="false" aria-controls="tab-models">
        <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="m12 3 9 5-9 5-9-5 9-5Z"/><path d="m3 8 9 5 9-5M3 8v9l9 5 9-5V8M12 13v9"/></svg>
        <span>模型</span>
      </button>
      <button role="tab" id="nav-keys" data-tab="keys" aria-selected="false" aria-controls="tab-keys">
        <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><circle cx="8" cy="12" r="3.6"/><path d="M11.6 12H21"/><path d="M17.5 12v3"/><path d="M20.5 12v2"/></svg>
        <span>API 密钥</span>
      </button>
      <button role="tab" id="nav-logs" data-tab="logs" aria-selected="false" aria-controls="tab-logs">
        <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="M4 6h16"/><path d="M4 12h16"/><path d="M4 18h10"/></svg>
        <span>运行日志</span>
      </button>
      <button role="tab" id="nav-audit" data-tab="audit" aria-selected="false" aria-controls="tab-audit">
        <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3 4 6v6c0 4.2 3.3 7.4 8 9 4.7-1.6 8-4.8 8-9V6l-8-3Z"/><path d="m8.5 12 2.5 2.5 4.5-5"/></svg>
        <span>管理审计</span>
      </button>
      <button role="tab" id="nav-settings" data-tab="settings" aria-selected="false" aria-controls="tab-settings">
        <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="M4 8h8"/><path d="M16 8h4"/><path d="M4 16h4"/><path d="M12 16h8"/><circle cx="14" cy="8" r="2.2"/><circle cx="10" cy="16" r="2.2"/></svg>
        <span>配置</span>
      </button>
    </nav>

    <div v-once class="sidebar-foot">
      <span id="health" class="badge"><i class="dot"></i><span>检测中…</span></span>
      <span class="sidebar-note">CodeArts2API Panel <span class="mono">v0.3-dev</span></span>
    </div>
  </aside>
</template>
