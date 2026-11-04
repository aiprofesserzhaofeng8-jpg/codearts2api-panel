import { describe, expect, it } from 'vitest';
import { appendQuota } from './quota';
import { t as tr } from '../../i18n';

type Quota = Parameters<typeof appendQuota>[1];
const now = 1794000000;
function render(info: Quota) { const el = document.createElement('div'); expect(appendQuota(el, info, now)).toBe(true); return el; }
describe('provider quota presentation', () => {
  it('keeps legacy credit rendering with its controller', () => {
    const el = document.createElement('div'); expect(appendQuota(el, { available: true, total: 100, used: 1, remaining: 99 })).toBe(false); expect(el.childElementCount).toBe(0);
  });
  it('preserves known zero, real duration, reset time and independent windows', () => {
    const el = render({ available: true, kind: 'windows', windows: [
      { id: 'primary', name: '7 天窗口', unit: 'percent', utilization: 0, window_seconds: 604800, resets_at: now + 3600 },
      { id: 'secondary', name: '5 小时窗口', unit: 'percent', utilization: 80.25, window_seconds: 18000, resets_at: now + 7200 },
    ] });
    expect(el.querySelectorAll('[data-quota-window]')).toHaveLength(2); expect(el.querySelectorAll('progress')).toHaveLength(2);
    expect(el.querySelector('progress')?.value).toBe(0); expect(el.textContent).toContain('80.25%'); expect(el.textContent).toContain(tr('重置 '));
  });
  it('does not invent zero or a total for partial metrics', () => {
    const el = render({ available: true, kind: 'credits', windows: [{ id: 'plan', name: '套餐额度', unit: 'credits', remaining: 7.5 }] });
    expect(el.textContent).toContain('7.5 Credits'); expect(el.textContent).not.toContain('/ 0'); expect(el.querySelector('progress')).toBeNull();
  });
  it('keeps over-limit values and asks for refresh after reset time', () => {
    const el = render({ available: true, kind: 'windows', windows: [{ id: 'week', name: '7 天窗口', unit: 'percent', utilization: 121, resets_at: now - 1 }] });
    expect(el.textContent).toContain('121%'); expect(el.textContent).toContain(tr('已到重置时间，请刷新')); expect(el.querySelector('progress')?.value).toBe(100);
  });
  it('preserves decimal balances and distinguishes unlimited from a balance', () => {
    const el = render({ available: true, kind: 'windows', balance: { amount: '9007199254740993.125', unit: 'credits' }, windows: [{ id: 'org', name: '组织共享额度', unit: 'credits', unlimited: true }] });
    expect(el.textContent).toContain('9007199254740993.125'); expect(el.textContent).toContain(tr('额度未设上限')); expect(el.textContent).not.toContain('-1');
  });
  it('shows unsupported and unknown states without zero balances', () => {
    const unsupported = render({ available: false, kind: 'unsupported', reason: 'API key fixture' }); expect(unsupported.textContent).toContain(tr('暂不支持查询')); expect(unsupported.textContent).toContain('API key fixture');
    const unknown = render({ available: false, kind: 'windows', reason: 'missing upstream data' }); expect(unknown.textContent).toContain(tr('用量未返回')); expect(unknown.textContent).not.toContain('0%');
  });
  it('does not round small positive credit usage into zero', () => {
    const el = render({ available: true, kind: 'credits', windows: [{ id: 'plan', name: '套餐额度', unit: 'credits', used: 0.001 }] });
    expect(el.textContent).toContain('<0.01');
  });
  it('treats upstream labels as text', () => {
    const el = render({ available: true, kind: 'credits', package_name: '<img src=x onerror=alert(1)>', windows: [{ id: 'plan', name: '<script>alert(1)</script>', unit: 'credits', used: 1 }] });
    expect(el.querySelector('img,script')).toBeNull(); expect(el.textContent).toContain('<img');
  });
});
