import english from './locales/en.json';

export type Locale = 'zh' | 'en';
export type LocalePreference = Locale | 'auto';
export const localeStorageKey = 'codearts2api_locale';
export const defaultLocale: Locale = 'zh';

export function isLocale(value: unknown): value is Locale {
  return value === 'zh' || value === 'en';
}

export function detectLocale(languages: readonly string[]): Locale {
  for (const language of languages) {
    const code = language.toLowerCase().split(/[-_]/)[0];
    if (isLocale(code)) return code;
  }
  return defaultLocale;
}

function readPreference(storage: Storage): Locale | undefined {
  try {
    const value = storage.getItem(localeStorageKey);
    return isLocale(value) ? value : undefined;
  } catch { return undefined; }
}

function savedPreference(): Locale | undefined {
  // Accessing the storage property itself can throw in restricted browsers.
  try { const value = readPreference(localStorage); if (value) return value; } catch { /* Try tab storage. */ }
  try { return readPreference(sessionStorage); } catch { return undefined; }
}

export const localePreference: LocalePreference = savedPreference() ?? 'auto';
export const locale: Locale = localePreference === 'auto'
  ? detectLocale([...(navigator.languages ?? []), navigator.language].filter(Boolean))
  : localePreference;
export const dateLocale = locale === 'zh' ? 'zh-CN' : 'en-US';

/** Translate interface copy only. Never translate API identifiers, credentials or user input. */
export function t(source: string): string {
  if (locale === 'zh') return source;
  const key = source.trim();
  const value = Object.hasOwn(english, key) ? (english as Record<string, string>)[key] : undefined;
  return value === undefined ? source : source.replace(key, () => value);
}

/** Save before reloading, so legacy DOM controllers and Vue use the same locale. */
export function saveLocalePreference(preference: LocalePreference): void {
  if (preference !== 'auto' && !isLocale(preference)) throw new Error('Unsupported language');
  let saved = false;
  for (const getStorage of [() => localStorage, () => sessionStorage]) {
    try {
      const storage = getStorage();
      if (preference === 'auto') storage.removeItem(localeStorageKey);
      else storage.setItem(localeStorageKey, preference);
      saved = true;
    } catch { /* Tab storage keeps the choice across reloads if local storage is unavailable. */ }
  }
  if (!saved || (savedPreference() ?? 'auto') !== preference) {
    throw new Error(t('浏览器禁止保存语言偏好，请允许站点存储后重试。'));
  }
}

document.documentElement.lang = dateLocale;
document.title = t('CodeArts2API Panel · 多上游账号网关');
