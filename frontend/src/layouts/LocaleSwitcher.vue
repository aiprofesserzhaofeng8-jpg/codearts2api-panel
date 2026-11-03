<script setup lang="ts">
// Interaction follows Sub2API's LocaleSwitcher; retain our theme and DOM ownership boundaries.
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue';
import { locale, localePreference, saveLocalePreference, t, type LocalePreference } from '../i18n';
import { pendingWrites } from '../api/client';

const open = ref(false), switching = ref(false), error = ref('');
const root = ref<HTMLElement>(), trigger = ref<HTMLButtonElement>();
const options: {code: LocalePreference; name: string; flag: string}[] = [
  {code: 'auto', name: t('跟随系统'), flag: '◎'},
  {code: 'zh', name: '中文', flag: '🇨🇳'},
  {code: 'en', name: 'English', flag: '🇺🇸'},
];
async function toggle() {
  open.value = !open.value; error.value = '';
  if (open.value) {
    await nextTick();
    root.value?.querySelector<HTMLButtonElement>('[aria-checked="true"]')?.focus();
  }
}
function close(restoreFocus = false) {
  open.value = false;
  if (restoreFocus) trigger.value?.focus();
}
function outside(event: PointerEvent) {
  if (event.target instanceof Node && !root.value?.contains(event.target)) close();
}
function keydown(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); close(true); }
  if (!open.value || !['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return;
  event.preventDefault();
  const items = [...root.value!.querySelectorAll<HTMLButtonElement>('[role="menuitemradio"]')];
  const current = items.indexOf(document.activeElement as HTMLButtonElement);
  const index = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1
    : (current + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length;
  items[index]?.focus();
}
function select(code: LocalePreference) {
  if (switching.value || pendingWrites.value) return;
  if (code === localePreference) { close(true); return; }
  switching.value = true; error.value = '';
  try {
    saveLocalePreference(code);
    // Like Sub2API, initialize all pages in the selected language together.
    window.location.reload();
  } catch (e) { error.value = (e as Error).message; switching.value = false; }
}
onMounted(() => document.addEventListener('pointerdown', outside));
onBeforeUnmount(() => document.removeEventListener('pointerdown', outside));
</script>
<template>
  <div ref="root" class="locale-switcher" @keydown="keydown" @focusout="event => { if (!root?.contains(event.relatedTarget as Node)) close(); }">
    <button ref="trigger" class="btn ghost locale-trigger" type="button" :title="t('选择语言')" :aria-label="t('选择语言')" aria-haspopup="menu" :aria-expanded="open" :disabled="switching || pendingWrites > 0" @click="toggle" @keydown.down.prevent="!open && toggle()">
      <span aria-hidden="true">{{locale === 'zh' ? '🇨🇳' : '🇺🇸'}}</span><span>{{locale === 'zh' ? 'ZH' : 'EN'}}</span>
      <svg class="ico locale-chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg>
    </button>
    <div v-if="open" class="locale-menu" role="menu" :aria-label="t('选择语言')">
      <button v-for="option in options" :key="option.code" type="button" role="menuitemradio" :aria-checked="localePreference === option.code" :lang="option.code === 'auto' ? undefined : option.code" :disabled="switching || pendingWrites > 0" @click="select(option.code)">
        <span aria-hidden="true">{{option.flag}}</span><span>{{option.name}}</span><span v-if="localePreference === option.code" class="locale-check" aria-hidden="true">✓</span>
      </button>
      <p v-if="error" role="alert" class="err-text locale-error">{{error}}</p>
    </div>
  </div>
</template>
