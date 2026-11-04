import type { components } from '../../api/generated/admin';
import { dateLocale, locale, t as tr } from '../../i18n';

type Snapshot = components['schemas']['AccountQuota'];
type Window = components['schemas']['QuotaWindow'];
const number = new Intl.NumberFormat(dateLocale, { maximumFractionDigits: 2 });
const date = new Intl.DateTimeFormat(dateLocale, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false });
const durationUnits = Object.fromEntries(['day', 'hour', 'minute'].map(unit => [unit, new Intl.NumberFormat(dateLocale, { style: 'unit', unit, unitDisplay: 'narrow' })]));
function amount(value: number): string { return value > 0 && value < 0.01 ? '<0.01' : number.format(value); }
const known = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v) && v >= 0;
function node(tag: string, className: string, text = ''): HTMLElement {
  const el = document.createElement(tag); el.className = className; el.textContent = text; return el;
}
function windowName(w: Window): string {
  const seconds = w.window_seconds;
  if (!known(seconds) || seconds === 0) return tr(w.name);
  const prefix = w.name.includes(' · ') ? w.name.split(' · ').slice(0, -1).join(' · ') : '';
  let label = tr(prefix ? w.name.split(' · ').at(-1) || w.name : w.name);
  if (seconds % 86400 === 0) label = number.format(seconds / 86400) + tr(' 天窗口');
  else if (seconds % 3600 === 0) label = number.format(seconds / 3600) + tr(' 小时窗口');
  else if (seconds % 60 === 0) label = number.format(seconds / 60) + tr(' 分钟窗口');
  return prefix ? tr(prefix) + ' · ' + label : label;
}
function timeText(timestamp: number): string {
  const at = new Date(timestamp * 1000);
  return Number.isFinite(at.getTime()) ? date.format(at) : '';
}
function resetText(timestamp: number, now: number): string {
  const absolute = timeText(timestamp); if (!absolute) return '';
  const left = Math.ceil(timestamp - now);
  if (left <= 0) return tr('重置 ') + absolute + ' · ' + tr('已到重置时间，请刷新');
  const days = Math.floor(left / 86400), hours = Math.floor((left % 86400) / 3600), minutes = Math.floor((left % 3600) / 60);
  const duration = days ? durationUnits.day.format(days) + ' ' + durationUnits.hour.format(hours) : hours ? durationUnits.hour.format(hours) + ' ' + durationUnits.minute.format(minutes) : durationUnits.minute.format(Math.max(1, minutes));
  return tr('重置 ') + absolute + ' · ' + duration + (locale === 'en' ? ' ' : '') + tr('后');
}
function unitLabel(unit?: string): string { return unit === 'credits' ? 'Credits' : unit === 'usd' ? 'USD' : unit || ''; }
function renderWindow(w: Window, now: number): HTMLElement {
  const row = node('div', 'account-quota-window'); row.dataset.quotaWindow = w.id;
  row.append(node('span', 'quota-window-name', windowName(w)));
  let text: string;
  if (w.unlimited) text = tr('额度未设上限');
  else if (w.unit === 'percent' && known(w.utilization)) text = tr('已用 ') + amount(w.utilization) + '% · ' + tr('剩余 ') + amount(Math.max(0, 100 - w.utilization)) + '%';
  else {
    const unit = unitLabel(w.unit);
    if (known(w.remaining)) text = tr('剩余 ') + amount(w.remaining) + (known(w.total) ? ' / ' + amount(w.total) : '') + (unit ? ' ' + unit : '');
    else if (known(w.used)) text = tr('已用 ') + amount(w.used) + (known(w.total) ? ' / ' + amount(w.total) : '') + (unit ? ' ' + unit : '');
    else if (known(w.total)) text = tr('总额 ') + amount(w.total) + (unit ? ' ' + unit : '');
    else text = tr('用量未知');
  }
  row.append(node('span', 'mono quota-window-value', text));
  if (known(w.utilization) && !w.unlimited) {
    const bar = node('div', 'bar credit-bar quota-window-bar');
    if (w.utilization >= 95) bar.classList.add('err'); else if (w.utilization >= 80) bar.classList.add('warn');
    const progress = document.createElement('progress'); progress.max = 100; progress.value = Math.min(100, w.utilization);
    progress.setAttribute('aria-label', windowName(w) + ' · ' + tr('已用 ') + amount(w.utilization) + '%');
    bar.append(progress); row.append(bar);
  }
  const details: string[] = [];
  if (w.unit !== 'percent' && known(w.used) && known(w.remaining)) details.push(tr('已用 ') + amount(w.used));
  if (known(w.resets_at) && w.resets_at > 0) details.push(resetText(w.resets_at, now));
  if (known(w.expires_at) && w.expires_at > 0) details.push(tr('到期 ') + timeText(w.expires_at));
  if (details.length) row.append(node('span', 'sub', details.filter(Boolean).join(' · ')));
  return row;
}

// Append nodes inside the legacy controller's owned subtree. No Vue renderer
// shares these nodes; labels, balances and upstream strings use textContent.
export function appendQuota(parent: HTMLElement, info: Snapshot, now = Date.now() / 1000): boolean {
  if (info.kind === 'unsupported') {
    parent.append(node('span', 'muted', tr('暂不支持查询')));
    if (info.reason) parent.append(node('span', 'sub quota-query-error', tr(info.reason)));
    return true;
  }
  if (info.kind !== 'credits' && info.kind !== 'windows') return false;
  if (!info.available) {
    parent.append(node('span', 'muted', tr('用量未返回')));
    if (info.reason) parent.append(node('span', 'sub quota-query-error', tr(info.reason)));
    return true;
  }
  const windows = Array.isArray(info.windows) ? info.windows : [];
  if (!windows.length && !info.balance) return false;
  parent.classList.add('account-quota');
  if (info.package_name) parent.append(node('span', 'sub quota-plan-name', info.package_name));
  windows.forEach(w => parent.append(renderWindow(w, now)));
  if (info.balance) {
    const balance = node('div', 'account-quota-window quota-balance'); balance.append(node('span', 'quota-window-name', tr('额外 Credits')));
    const amount = info.balance.unlimited ? tr('额度未设上限') : typeof info.balance.amount === 'string' ? info.balance.amount + ' ' + unitLabel(info.balance.unit) : tr('余额未知');
    balance.append(node('span', 'mono quota-window-value', amount)); parent.append(balance);
  }
  if (known(info.updated_at) && info.updated_at > 0) parent.append(node('span', 'sub quota-updated', tr('更新于 ') + timeText(info.updated_at)));
  return true;
}
