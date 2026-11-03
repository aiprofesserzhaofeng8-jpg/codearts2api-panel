import { afterEach, beforeEach, expect, it, vi } from 'vitest';

function languages(values: string[], primary = values[0] ?? '') {
  Object.defineProperty(navigator, 'languages', {configurable: true, value: values});
  Object.defineProperty(navigator, 'language', {configurable: true, value: primary});
}
beforeEach(() => {
  vi.resetModules(); localStorage.clear(); sessionStorage.clear(); languages(['zh-CN']);
});
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

it('defaults to Chinese when browser languages are absent or unsupported', async () => {
  languages([]);
  const {locale, detectLocale} = await import('./index');
  expect(locale).toBe('zh');
  expect(detectLocale(['fr-FR', 'ja-JP'])).toBe('zh');
  expect(document.documentElement.lang).toBe('zh-CN');
});
it.each(['zh-CN', 'zh-TW', 'zh-Hant', 'ZH_cn'])('recognizes Chinese browser language %s', async language => {
  languages([language]); expect((await import('./index')).locale).toBe('zh');
});
it('uses supported browser languages in preference order, including the primary-language fallback', async () => {
  languages(['de-DE', 'en-GB', 'zh-CN']);
  const i18n = await import('./index');
  expect(i18n.locale).toBe('en');
  expect(i18n.localePreference).toBe('auto');
  expect(i18n.detectLocale(['zh-CN', 'en'])).toBe('zh');
  expect(document.documentElement.lang).toBe('en-US');
  expect(localStorage.getItem(i18n.localeStorageKey)).toBeNull();
  expect(sessionStorage.getItem(i18n.localeStorageKey)).toBeNull();
  vi.resetModules(); languages([], 'en-US');
  expect((await import('./index')).locale).toBe('en');
});
it('keeps an explicit preference across reloads and changing browser environments', async () => {
  const first = await import('./index');
  first.saveLocalePreference('en'); vi.resetModules();
  const second = await import('./index');
  expect(second.locale).toBe('en'); expect(second.localePreference).toBe('en');
  second.saveLocalePreference('zh'); languages(['en-US']); vi.resetModules();
  expect((await import('./index')).locale).toBe('zh');
});
it('does not change the language of a mounted page on storage or browser language events', async () => {
  const i18n = await import('./index');
  localStorage.setItem(i18n.localeStorageKey, 'en'); languages(['en-US']);
  window.dispatchEvent(new Event('languagechange'));
  window.dispatchEvent(new StorageEvent('storage', {key: i18n.localeStorageKey, newValue: 'en'}));
  expect(i18n.locale).toBe('zh'); expect(i18n.t('用户名')).toBe('用户名');
  vi.resetModules(); expect((await import('./index')).locale).toBe('en');
});
it('returns to automatic detection after clearing the manual preference', async () => {
  localStorage.setItem('codearts2api_locale', 'zh'); sessionStorage.setItem('codearts2api_locale', 'zh');
  languages(['en-US']); const i18n = await import('./index');
  expect(i18n.locale).toBe('zh'); i18n.saveLocalePreference('auto'); vi.resetModules();
  const automatic = await import('./index');
  expect(automatic.locale).toBe('en'); expect(automatic.localePreference).toBe('auto');
  expect(localStorage.getItem(i18n.localeStorageKey)).toBeNull();
  expect(sessionStorage.getItem(i18n.localeStorageKey)).toBeNull();
});
it('ignores invalid stored values instead of persisting a detection result', async () => {
  localStorage.setItem('codearts2api_locale', 'undefined'); languages(['en-US']);
  const i18n = await import('./index');
  expect(i18n.locale).toBe('en'); expect(i18n.localePreference).toBe('auto');
  expect(() => i18n.saveLocalePreference('fr' as 'en')).toThrow('Unsupported language');
});
it('can render and save to tab storage when local storage is blocked', async () => {
  vi.stubGlobal('localStorage', {getItem: () => { throw new Error('blocked'); }, setItem: () => { throw new Error('blocked'); }});
  const i18n = await import('./index');
  expect(i18n.locale).toBe('zh'); i18n.saveLocalePreference('en'); vi.resetModules();
  expect((await import('./index')).locale).toBe('en');
});
it('reports failed persistence rather than reloading into an unexpected language', async () => {
  const i18n = await import('./index');
  const blocked = {getItem: () => { throw new Error('blocked'); }, setItem: () => { throw new Error('blocked'); }};
  vi.stubGlobal('localStorage', blocked); vi.stubGlobal('sessionStorage', blocked);
  expect(() => i18n.saveLocalePreference('en')).toThrow('浏览器禁止保存语言偏好');
});
it('does not claim a save succeeded when a read-only stored preference still takes precedence', async () => {
  const i18n = await import('./index');
  vi.stubGlobal('localStorage', {getItem: () => 'zh', setItem: () => { throw new Error('read only'); }});
  expect(() => i18n.saveLocalePreference('en')).toThrow();
});
it('translates exact interface copy and preserves whitespace, identifiers and unknown text', async () => {
  languages(['en-US']); const {t} = await import('./index');
  expect(t('用户名')).toBe('Username'); expect(t('  用户名 ')).toBe('  Username ');
  expect(t('workbuddy/cn/deepseek-v4.1-flash')).toBe('workbuddy/cn/deepseek-v4.1-flash');
  expect(t('Unknown upstream message')).toBe('Unknown upstream message');
  expect(t('toString')).toBe('toString'); expect(t('__proto__')).toBe('__proto__');
});
