// @vitest-environment jsdom
import { mount, flushPromises } from '@vue/test-utils';
import { beforeEach, afterEach, it, expect, vi } from 'vitest';
import App from './ConsoleApplication.vue';
import { expireSession } from './authentication';
const fixture={accounts:[],keys:[],models:[],groups:[],providers:[],settings:{pool:{}},stats:{}};
beforeEach(()=>{
  expireSession();
  vi.stubGlobal('matchMedia',vi.fn(()=>({matches:false,addEventListener:vi.fn(),removeEventListener:vi.fn()})));
  HTMLDialogElement.prototype.showModal=function(){this.setAttribute('open','');};
  HTMLDialogElement.prototype.close=function(){this.removeAttribute('open');this.dispatchEvent(new Event('close'));};
  location.hash='accounts';
  vi.stubGlobal('fetch',vi.fn((path:string)=>Promise.resolve(new Response(JSON.stringify(
    path==='/auth/session'?{authenticated:true,user:{id:'builtin:admin',username:'admin',role:'admin'}}:path==='/admin/api/state'?fixture:path.startsWith('/admin/api/usage')?{page:1,pages:1,total:0,logs:[],stats:{}}:{}
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

it('loads login options without cancelling the child request or exposing an internal lifecycle error',async()=>{
 vi.stubGlobal('fetch',vi.fn((path:string)=>Promise.resolve(new Response(JSON.stringify(path==='/auth/session'?{authenticated:false}:{email:true,github:false,linuxdo:false})))));
 const wrapper=mount(App,{attachTo:document.body});await flushPromises();
 expect(wrapper.find('#loginForm').exists()).toBe(true);
 expect(wrapper.text()).toContain('忘记密码');
 expect(wrapper.text()).not.toContain('Console disposed');
 expect(vi.mocked(fetch).mock.calls.map(call=>call[0])).toEqual(['/auth/session','/auth/options']);
 wrapper.unmount();
});
