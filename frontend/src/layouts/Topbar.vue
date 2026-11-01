<template>
<header v-once class="topbar">
      <div v-once class="page-head">
        <h1 id="pageTitle">概览</h1>
        <p class="page-hint" id="pageHint">运行状态与用量</p>
      </div>
      <div v-once class="topbar-actions">
        <button class="btn ghost" id="themeToggle" aria-label="切换明暗主题" title="切换明暗主题">
          <svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="M20.5 13A8.5 8.5 0 0 1 11 3.5 8.5 8.5 0 1 0 20.5 13Z"/></svg>
          <span id="themeLabel">浅色</span>
        </button>
        <span id="localPanelStatus" class="badge ok hidden">本机已解锁</span>
        <button class="btn ghost hidden" id="forgetPw">退出登录</button>
      </div>
    </header>
</template>
