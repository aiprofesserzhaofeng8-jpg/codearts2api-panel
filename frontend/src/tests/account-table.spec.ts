import { describe, it, expect, beforeEach } from 'vitest';
import { appendQuota } from '../features/accounts/quota';
import { readPreferences, savePreferences, sortAccounts, paginateAccounts, runSelected } from '../features/accounts/table';
describe('account quota projection', () => {
  it('shows every quota window inline without a separate detail view', () => {
    const info = { available: true, kind: 'windows', windows: [0, 1, 2, 3].map(i => ({ id: String(i), name: 'window'+i, unit: 'percent', utilization: i === 3 ? 100 : i })) };
    const list = document.createElement('div'); appendQuota(list, info, 100);
    expect(list.querySelectorAll('progress')).toHaveLength(4);
    expect(list.querySelectorAll('progress')[3].value).toBe(100);
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
    savePreferences({hidden:['credential','identity','actions'],sort:'name',pageSize:50});
    expect(readPreferences()).toEqual({hidden:['credential'],sort:'name',pageSize:50});
    savePreferences({hidden:[],sort:'default',pageSize:999});
    expect(readPreferences().pageSize).toBe(20);
    expect(sortAccounts([{name:'b'},{name:'a'}],'name').map(x=>x.name)).toEqual(['a','b']);
  });
  it('pages filtered results and clamps a vanished last page after deletion', () => {
    const accounts = Array.from({length:43}, (_, id) => ({id}));
    expect(paginateAccounts(accounts,1)).toMatchObject({page:1,pages:3,from:1,to:20,total:43});
    expect(paginateAccounts(accounts,2).items.map(a=>a.id)).toEqual(Array.from({length:20},(_,i)=>i+20));
    expect(paginateAccounts(accounts,3).items.map(a=>a.id)).toEqual([40,41,42]);
    expect(paginateAccounts(accounts.slice(0,20),3)).toMatchObject({page:1,pages:1,from:1,to:20});
    expect(paginateAccounts([],3)).toMatchObject({page:1,pages:1,from:0,to:0,total:0,items:[]});
    expect(paginateAccounts(accounts,1,10)).toMatchObject({pages:5,to:10});
    const links = paginateAccounts(Array.from({length:10000}),250).links;
    expect(links).toEqual([1,'…',249,250,251,'…',500]);
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
