// @vitest-environment jsdom
import { mount, flushPromises } from '@vue/test-utils';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import PromptsPage from './PromptsPage.vue';
import { configureClient, disposeClient } from '../../api/client';
const platforms=['codearts','workbuddy','trae','qoder','kiro','chatgpt','claude','teleagent'];
const fixture=()=>({common:{prompt_mode:'passthrough',prompt_text:'SHARED',prompt_fallback:false,sanitize:false},platforms:Object.fromEntries(platforms.map(id=>[id,{prompt_mode:'custom',prompt_text:'LEGACY',prompt_fallback:false,sanitize:false,overrides:id==='workbuddy'?null:{}}]))});
beforeEach(()=>{configureClient(()=>{});location.hash='#prompts';vi.stubGlobal('fetch',vi.fn(async()=>new Response(JSON.stringify(fixture()))));});
afterEach(()=>{disposeClient();vi.unstubAllGlobals();document.body.innerHTML='';});
it('preserves legacy mode, inherited text and unsaved edits across refresh',async()=>{
 const page=mount(PromptsPage,{attachTo:document.body});await flushPromises();
 expect((page.get('#promptMode').element as HTMLSelectElement).value).toBe('passthrough');
 await page.get('#promptMode').setValue('append');await page.get('#promptText').setValue('EXTRA');
 window.dispatchEvent(new Event('prompts-refresh'));await flushPromises();expect((page.get('#promptText').element as HTMLTextAreaElement).value).toBe('EXTRA');
 const wb=page.findAll('.prompt-scope').find(b=>b.text()==='WorkBuddy')!;await wb.trigger('click');
 expect((page.get('#promptMode').element as HTMLSelectElement).value).toBe('custom');expect((page.get('#promptText').element as HTMLTextAreaElement).value).toBe('LEGACY');
 await page.findAll('.prompt-scope').find(b=>b.text()==='ChatGPT / Codex')!.trigger('click');
 expect((page.get('#promptMode').element as HTMLSelectElement).value).toBe('append');expect(page.get('#promptMode').attributes('disabled')).toBeDefined();
 page.unmount();
});
it('keeps draft on failed save and submits only prompt-owned configuration',async()=>{
 const page=mount(PromptsPage,{attachTo:document.body});await flushPromises();
 await page.get('#promptMode').setValue('append');await page.get('#promptText').setValue('EXTRA');
 vi.mocked(fetch).mockImplementationOnce(async()=>new Response(JSON.stringify({error:{message:'conflict'}}),{status:409}));
 await page.get('#savePrompts').trigger('click');await flushPromises();
 expect(page.text()).toContain('conflict');expect((page.get('#promptText').element as HTMLTextAreaElement).value).toBe('EXTRA');
 const options=vi.mocked(fetch).mock.calls.at(-1)![1]!;const body=JSON.parse(options.body as string);
 expect(Object.keys(body)).toEqual(['expected','configuration']);expect(body.configuration.common.prompt_mode).toBe('append');expect(body.expected.common.prompt_mode).toBe('passthrough');
 page.unmount();
});
