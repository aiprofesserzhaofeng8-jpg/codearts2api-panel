import { describe, it, expect, beforeEach } from 'vitest';
import { appendQuota } from '../features/accounts/quota';
import { readPreferences, savePreferences, sortAccounts, runSelected } from '../features/accounts/table';
describe('account quota projection', () => {
  it('caps list windows, keeps exhausted warnings, and exposes everything in detail', () => {
    const info = { available: true, kind: 'windows', windows: [0, 1, 2, 3].map(i => ({ id: String(i), name: 'window'+i, unit: 'percent', utilization: i === 3 ? 100 : i })) };
    const list = document.createElement('div'); appendQuota(list, info, 100, true);
    expect(list.querySelectorAll('progress')).toHaveLength(2);
    expect(list.querySelector('progress')?.value).toBe(100);
    const detail = document.createElement('div'); appendQuota(detail, info, 100);
    expect(detail.querySelectorAll('progress')).toHaveLength(4);
  });
  it('keeps unknown, zero and unlimited distinct; never invents a balance progress bar', () => {
    const el = document.createElement('div');
    appendQuota(el, {available:true,kind:'windows', windows:[{id:'z',name:'zero',unit:'percent',utilization:0},{id:'u',name:'unknown',unit:'percent'},{id:'n',name:'unlimited',unlimited:true,utilization:100}],balance:{amount:'123.4567890123456789',unit:'credits'}}, 100);
    expect(el.querySelectorAll('progress')).toHaveLength(1);
    expect(el.querySelector('progress')?.value).toBe(0);
    expect(el.textContent).toContain('用量未知');expect(el.textContent).toContain('额度未设上限');expect(el.textContent).toContain('123.4567890123456789');
  });
});
describe('table preferences and bulk scope', () => {
  beforeEach(() => localStorage.clear());
  it('persists only optional columns and known sort settings', () => {
    savePreferences({hidden:['credential','quota'],sort:'name'});
    expect(readPreferences()).toEqual({hidden:['credential'],sort:'name'});
    expect(sortAccounts([{name:'b'},{name:'a'}],'name').map(x=>x.name)).toEqual(['a','b']);
  });
  it('runs selected accounts serially, reports failures, and never retries them', async () => {
    let active=0;const calls:string[]=[];const results:unknown[]=[];
    await runSelected([{id:'a'},{id:'b'},{id:'c'}],async a=>{expect(++active).toBe(1);calls.push(a.id);await Promise.resolve();active--;if(a.id==='b')throw new Error('provider failure');return 'ok';},r=>results.push(r));
    expect(calls).toEqual(['a','b','c']);expect(results).toMatchObject([{ok:true},{ok:false,message:'provider failure'},{ok:true}]);
  });
  it('stops after lost authorization rather than submitting the rest', async () => {
    const calls:string[]=[];
    await runSelected([{id:'a'},{id:'b'}],async a=>{calls.push(a.id);throw Object.assign(new Error('expired'),{status:401});},()=>{});
    expect(calls).toEqual(['a']);
  });
});
