// @vitest-environment jsdom
import { mount, flushPromises } from '@vue/test-utils';
import { beforeEach, afterEach, it, expect, vi } from 'vitest';
import App from '../App.vue';
const fixture={accounts:[],keys:[],models:[],groups:[],providers:[],settings:{pool:{}},stats:{}};
beforeEach(()=>{
  HTMLDialogElement.prototype.showModal=function(){this.setAttribute('open','');};
  HTMLDialogElement.prototype.close=function(){this.removeAttribute('open');this.dispatchEvent(new Event('close'));};
  location.hash='accounts';
  vi.stubGlobal('fetch',vi.fn((path:string)=>Promise.resolve(new Response(JSON.stringify(
    path==='/admin/auth/session'?{authenticated:true}:path==='/admin/api/state'?fixture:path.startsWith('/admin/api/usage')?{page:1,pages:1,total:0,logs:[],stats:{}}:{}
  )))));
});
afterEach(()=>{vi.unstubAllGlobals();vi.useRealTimers();document.body.textContent='';});
it('mounts nine independent pages, preserves unsaved settings on refresh, and disposes polling',async()=>{
 const wrapper=mount(App,{attachTo:document.body});await flushPromises();
 expect(document.querySelectorAll('section[role="tabpanel"]')).toHaveLength(9);
 const input=document.querySelector<HTMLInputElement>('#sForceModel')!;expect(input).toBeTruthy();
 input.value='draft-do-not-overwrite';input.dispatchEvent(new Event('input',{bubbles:true}));
 const { refresh }=await import('./snapshot');await refresh();expect(input.value).toBe('draft-do-not-overwrite');
 vi.useFakeTimers();wrapper.unmount();const calls=vi.mocked(fetch).mock.calls.length;await vi.advanceTimersByTimeAsync(60000);expect(vi.mocked(fetch).mock.calls.length).toBe(calls);
});
it('ignores an obsolete usage query after the app is removed',async()=>{
 const wrapper=mount(App,{attachTo:document.body});await flushPromises();
 let resolve!: (response:Response)=>void;vi.stubGlobal('fetch',vi.fn(()=>new Promise<Response>(done=>{resolve=done;})));
 const { refreshUsageLogs }=await import('../features/usage/controller');const pending=refreshUsageLogs();wrapper.unmount();resolve(new Response('{}'));await expect(pending).resolves.toBeUndefined();
});
