<script setup lang="ts">
import { t as tr } from '../i18n';

import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue';
import ConsoleBrand from './ConsoleBrand.vue';
import Sidebar from './Sidebar.vue';
import Topbar from './Topbar.vue';
withDefaults(defineProps<{role?: 'admin' | 'user'; name?: string}>(), {role: 'admin', name: ''});
const media = window.matchMedia('(max-width: 920px)');
const mobile = ref(media.matches), open = ref(false), collapsed = ref(false);
try { collapsed.value = localStorage.getItem('codearts2api_sidebar_collapsed') === 'true'; } catch { /* Storage is optional. */ }
function resize() { mobile.value = media.matches; open.value = false; }
function closeMenu() {
  if (!open.value) return;
  open.value = false;
  void nextTick(() => document.querySelector<HTMLButtonElement>('#consoleMenuToggle')?.focus());
}
function navigate(event: MouseEvent) {
  if ((event.target as Element).closest('nav button, a')) closeMenu();
}
function collapse() {
  collapsed.value = !collapsed.value;
  try { localStorage.setItem('codearts2api_sidebar_collapsed', String(collapsed.value)); } catch { /* Storage is optional. */ }
}
watch(open, value => document.body.classList.toggle('console-menu-open', value), {flush: 'sync'});
watch(open, value => { if (value) void nextTick(() => document.querySelector<HTMLButtonElement>('.console-mobile-close')?.focus()); });
onMounted(() => media.addEventListener('change', resize));
onUnmounted(() => { media.removeEventListener('change', resize); document.body.classList.remove('console-menu-open'); });
</script>
<template>
  <div class="app console-layout" :class="{'sidebar-collapsed': collapsed, 'sidebar-open': open}" :data-console-role="role" @keydown.esc="closeMenu">
    <a class="skip" href="#main">{{tr('跳到主要内容')}}</a>
    <aside id="consoleSidebar" class="sidebar" :inert="mobile && !open" :aria-label="tr('侧栏导航')" @click="navigate">
      <ConsoleBrand />
      <button class="btn ghost console-mobile-close" :aria-label="tr('关闭导航')" @click="closeMenu">{{tr('关闭')}}</button>
      <div class="sidebar-scroll">
        <p class="sidebar-section-label">{{role === 'admin' ? tr('管理空间') : tr('个人空间')}}</p>
        <slot name="navigation"><Sidebar /></slot>
      </div>
      <div class="sidebar-foot">
        <slot name="status"><span v-once id="health" class="badge"><i class="dot"></i><span>{{tr('检测中…')}}</span></span></slot>
        <button class="btn ghost console-collapse" :aria-label="collapsed ? tr('展开侧栏') : tr('收起侧栏')" :aria-expanded="!collapsed" aria-controls="consoleSidebar" @click="collapse"><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path :d="collapsed ? 'm9 5 7 7-7 7' : 'm15 5-7 7 7 7'"/></svg><span>{{collapsed ? tr('展开侧栏') : tr('收起侧栏')}}</span></button>
      </div>
    </aside>
    <button v-if="open" class="console-backdrop" :aria-label="tr('关闭侧栏')" tabindex="-1" @click="closeMenu"></button>
    <div class="main" :inert="mobile && open">
      <Topbar :role="role" :name="name" :open="open" @menu="open = !open">
        <template #heading><slot name="heading"></slot></template>
        <template #identity><slot name="identity"></slot></template>
        <template #actions><slot name="actions"></slot></template>
      </Topbar>
      <main id="main" class="console-content"><slot /></main>
    </div>
  </div>
</template>
