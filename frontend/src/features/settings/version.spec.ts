// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest';
import { flushPromises } from '@vue/test-utils';
import { configureClient, disposeClient } from '../../api/client';
import { beginLifetime, endLifetime } from '../../shared/lifecycle';
import { mountVersion } from './version';
afterEach(()=>{disposeClient();endLifetime();vi.unstubAllGlobals();document.body.textContent='';});
function mount(){document.body.innerHTML='<button id="checkVersion"></button><span id="runningVersion"></span><p id="versionMessage"></p><a id="releaseLink"></a>';configureClient(()=>{});beginLifetime();mountVersion();}
it('shows actual build and explicitly checks updates without writing settings',async()=>{
 vi.stubGlobal('fetch',vi.fn((url:string)=>Promise.resolve(new Response(JSON.stringify({current:'v0.3.1',latest:url.includes('?')?'v0.4.0':undefined,message:url.includes('?')?'有新版本':'点击检查更新',url:'https://evil.invalid'})))));
 mount();await flushPromises();expect(document.querySelector('#runningVersion')!.textContent).toBe('v0.3.1');
 (document.querySelector('#checkVersion') as HTMLButtonElement).click();await flushPromises();
 expect(document.querySelector('#versionMessage')!.textContent).toContain('v0.4.0');expect((document.querySelector('#releaseLink') as HTMLAnchorElement).href).toBe('https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/releases');
 expect(vi.mocked(fetch).mock.calls.map(x=>x[0])).toEqual(['/admin/api/version','/admin/api/version?check=1']);
});
it('ignores an obsolete response after logout',async()=>{
 let resolve!:(value:Response)=>void;vi.stubGlobal('fetch',vi.fn(()=>new Promise<Response>(done=>resolve=done)));
 mount();const current=document.querySelector('#runningVersion')!;disposeClient();endLifetime();document.body.textContent='';resolve(new Response(JSON.stringify({current:'obsolete',message:'obsolete'})));await flushPromises();expect(current.textContent).toBe('');
});
