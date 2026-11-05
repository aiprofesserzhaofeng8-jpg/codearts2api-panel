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
it('clears a password draft when the administrator explicitly reloads settings',async()=>{
 vi.stubGlobal('confirm',vi.fn(()=>true));const wrapper=mount(App,{attachTo:document.body});await flushPromises();
 const password=document.querySelector<HTMLInputElement>('#sPw')!;password.value='discarded-fixture-password';password.dispatchEvent(new Event('input',{bubbles:true}));
 document.querySelector<HTMLButtonElement>('#reloadSettings')!.click();await flushPromises();
 expect(password.value).toBe('');expect(document.querySelector('#settingsDirty')!.classList.contains('hidden')).toBe(true);wrapper.unmount();
});

it('loads login options without cancelling the child request or exposing an internal lifecycle error',async()=>{
 vi.stubGlobal('fetch',vi.fn((path:string)=>Promise.resolve(new Response(JSON.stringify(path==='/auth/session'?{authenticated:false}:{email:true,github:false,linuxdo:false})))));
 const wrapper=mount(App,{attachTo:document.body});await flushPromises();
 expect(wrapper.find('#loginForm').exists()).toBe(true);
 expect(wrapper.text()).toContain('忘记密码');
 expect(wrapper.text()).not.toContain('Console disposed');
 expect(vi.mocked(fetch).mock.calls.map(call=>call[0])).toEqual(['/setup/status','/auth/session','/auth/options']);
 wrapper.unmount();
});

it('shows setup without loading authentication or any management data',async()=>{
 vi.stubGlobal('fetch',vi.fn(()=>Promise.resolve(new Response(JSON.stringify({needs_setup:true,ready:false,admin_username:'admin'})))));
 const wrapper=mount(App,{attachTo:document.body});await flushPromises();
 expect(wrapper.find('#setupForm').exists()).toBe(true);
 expect(wrapper.find('#setupEmail').attributes('type')).toBe('email');
 expect(wrapper.find('#setupToken').exists()).toBe(false);
 expect(wrapper.find('#loginForm').exists()).toBe(false);
 expect(location.pathname).toBe('/setup');
 expect(vi.mocked(fetch).mock.calls.map(call=>call[0])).toEqual(['/setup/status']);
 wrapper.unmount();
});

it('reconciles a lost installation reply without submitting the administrator twice',async()=>{
 let installed=false, submissions=0;
 vi.stubGlobal('fetch',vi.fn((path:string,options?:RequestInit)=>{
  if(path==='/setup/install'&&options?.method==='POST'){
   expect(JSON.parse(options.body as string)).toEqual({email:'owner@example.com',password:'owner-password-here',confirm_password:'owner-password-here'});
   installed=true;submissions++;return Promise.reject(new TypeError('Connection closed'));
  }
  return Promise.resolve(new Response(JSON.stringify({needs_setup:!installed,ready:installed,admin_username:'admin'})));
 }));
 const wrapper=mount(App,{attachTo:document.body});await flushPromises();
 await wrapper.find('#setupEmail').setValue('owner@example.com');
 await wrapper.find('#setupPassword').setValue('owner-password-here');
 await wrapper.find('#setupConfirm').setValue('different-password');
 await wrapper.find('#setupForm').trigger('submit');
 expect(wrapper.get('[role="alert"]').text()).toBe('两次输入的密码不一致');
 expect(submissions).toBe(0);
 await wrapper.find('#setupConfirm').setValue('owner-password-here');
 await wrapper.find('#setupForm').trigger('submit');
 const finish=wrapper.findAll('button').find(button=>button.text()==='完成安装')!;
 await finish.trigger('click');await flushPromises();
 expect(wrapper.get('#setupCompleteTitle').text()).toBe('安装完成');
 expect(wrapper.text()).not.toContain('Connection closed');
 expect(wrapper.find('#setupPassword').exists()).toBe(false);
 expect(submissions).toBe(1);
 expect(wrapper.findAll('button').find(button=>button.text()==='前往登录')!.attributes('disabled')).toBeUndefined();
 wrapper.unmount();
});
