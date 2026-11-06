// @vitest-environment jsdom
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils';
import { beforeEach, afterEach, it, expect, vi } from 'vitest';
import { api, configureClient, disposeClient } from '../api/client';
import { serverState } from '../shared/read-model';
import { compatibilityAliases, parseMigrationReport, reloadUnifiedModels, routingRevision, unifiedModels } from '../shared/unified-models';
import UnifiedModelsPanel from '../features/models/UnifiedModelsPanel.vue';
import GroupsPage from '../features/groups/GroupsPage.vue';
import type { components } from '../api/generated/admin';
const model={id:'um_'+'1'.repeat(32),name:'shared-public-1',enabled:true,revision:1,sources:[{id:'workbuddy/cn/shared-v1',enabled:true},{id:'trae/cn/shared-v1',enabled:true}]};
const json=(data:unknown,status=200)=>Promise.resolve(new Response(JSON.stringify(data),{status}));
let wrapper:VueWrapper|undefined;
beforeEach(()=>{
 configureClient(vi.fn());unifiedModels.value=[];routingRevision.value=0;compatibilityAliases.value={};
 serverState.data={models:model.sources.map(s=>({id:s.id,source:'upstream',name:s.id})),accounts:[],groups:[],keys:[],settings:{},stats:{}} as unknown as components['schemas']['State'];
 HTMLDialogElement.prototype.showModal=function(){this.setAttribute('open','');};HTMLDialogElement.prototype.close=function(){this.removeAttribute('open');};
});
afterEach(()=>{wrapper?.unmount();wrapper=undefined;disposeClient();vi.unstubAllGlobals();serverState.data=null;});
it('rejects mixed pagination revisions without publishing a partial directory',async()=>{
 unifiedModels.value=[model];
 vi.stubGlobal('fetch',vi.fn((path:string)=>json({models:[],routing_revision:path.includes('after=')?2:1,next_after:path.includes('after=')?'':model.id})));
 await expect(reloadUnifiedModels()).rejects.toMatchObject({status:409,reason:'revision_conflict'});
 expect(unifiedModels.value).toEqual([model]);expect(routingRevision.value).toBe(0);
});
it('reads only bounded migration reports and rejects state/credential documents',()=>{
 const report={from_schema:10,to_schema:11,ready:false,definitions:1,group_changes:1,conflicts:[{reason:'model_name_conflict',group_ids:['group'],model_name:'<img src=x>',field_path:'groups.model_mappings',message:'duplicate'}],warnings:[]};
 expect(parseMigrationReport(JSON.stringify(report))).toEqual(report);
 expect(()=>parseMigrationReport(JSON.stringify({version:11,accounts:[],credentials:{}}))).toThrow();
 expect(()=>parseMigrationReport(' '.repeat((1<<20)+1))).toThrow();
});
it('preserves a model draft on CAS failure and serializes a complete source replacement',async()=>{
 const fetch=vi.fn((path:string,options:RequestInit)=> options?.method==='POST'?json({error:{message:'version changed',reason:'revision_conflict',code:409}},409):json({models:[],routing_revision:3,next_after:''}));vi.stubGlobal('fetch',fetch);
 wrapper=mount(UnifiedModelsPanel,{attachTo:document.body});await flushPromises();await wrapper.get('#addUnifiedModel').trigger('click');await flushPromises();
 await wrapper.get('#unifiedModelName').setValue('shared-public-1');
 for(const source of model.sources)await wrapper.get('#unifiedSourceSelect').setValue(source.id);
 await wrapper.get('form').trigger('submit');await flushPromises();
 expect(wrapper.get('#unifiedModelName').element).toHaveProperty('value','shared-public-1');expect(wrapper.get('#unifiedModelDialog').attributes('open')).toBe('');expect(wrapper.get('#unifiedModelError').text()).toContain('草稿已保留');
 const write=fetch.mock.calls.find(([,o])=>o?.method==='POST')!;expect(JSON.parse(write[1].body as string)).toMatchObject({sources:model.sources,client_policy:null,expected_routing_revision:3});
});
it('keeps selected empty grants denied and separates raw rules from unified permissions',async()=>{
 const bodies:unknown[]=[];
 vi.stubGlobal('fetch',vi.fn((path:string,options:RequestInit)=>{
  if(options?.method==='POST'){bodies.push(JSON.parse(options.body as string));return json({ok:true,group:{...bodies[0] as object,id:'g1',revision:1}});}
  return json({models:[model],routing_revision:1,next_after:''});
 }));
 wrapper=mount(GroupsPage,{attachTo:document.body});await wrapper.get('#addGroup').trigger('click');await flushPromises();
 await wrapper.get('#groupName').setValue('fixed');await wrapper.get('[aria-label="授权 shared-public-1"]').setValue(true);
 const sourceBoxes=wrapper.findAll('#groupUnifiedGrants details input[type=checkbox]');for(const box of sourceBoxes)await box.setValue(false);
 await wrapper.get('form').trigger('submit');await flushPromises();
 expect(bodies[0]).not.toHaveProperty('model_mappings');expect(bodies[0]).toMatchObject({raw_model_mode:'qualified_only',raw_models_visible_default:false,unified_models:[{model_id:model.id,visible:true,callable:true,source_mode:'selected',source_ids:[]}]});
});
it('retains server error reasons while preserving numeric HTTP status',async()=>{
 vi.stubGlobal('fetch',vi.fn(()=>json({error:{code:403,message:'denied',reason:'model_permission_denied'}},403)));
 await expect(api('/admin/api/unified-models')).rejects.toMatchObject({status:403,reason:'model_permission_denied',message:'denied'});
});
it('does not close a draft on an unconfirmed successful HTTP response',async()=>{
 vi.stubGlobal('fetch',vi.fn((_path:string,options:RequestInit)=>options?.method==='POST'?json({}):json({models:[],routing_revision:1,next_after:''})));
 wrapper=mount(UnifiedModelsPanel,{attachTo:document.body});await flushPromises();await wrapper.get('#addUnifiedModel').trigger('click');await flushPromises();
 await wrapper.get('#unifiedModelName').setValue('shared-public-1');await wrapper.get('#unifiedSourceSelect').setValue(model.sources[0].id);
 await wrapper.get('form').trigger('submit');await flushPromises();expect(wrapper.get('#unifiedModelError').text()).toContain('保存结果未确认');expect(wrapper.get('#unifiedModelDialog').attributes('open')).toBe('');
});

it('previews defaults and applies selected models and explicit group revision in one write',async()=>{
 const {default:DefaultModelsPanel}=await import('../features/models/DefaultModelsPanel.vue');
 const preview={version:1,catalog_revision:'catalog-hash',routing_revision:7,providers:['codearts','workbuddy','trae','qoder','kiro','chatgpt','claude','teleagent'],models:[
  {name:'deepseek-v4.1-flash',sources:model.sources,status:'create',message:'',existing_id:''},
  {name:'blocked-v1',sources:[],status:'conflict',message:'alias conflict',existing_id:''}
 ]};
 serverState.data!.groups=[{id:'g1',name:'Consumers',revision:3}] as components['schemas']['Group'][];
 const fetch=vi.fn((path:string,options:RequestInit)=>{
  if(options?.method==='POST')return json({ok:true,routing_revision:8});
  if(path==='/admin/api/model-defaults')return json(preview);
  return json({models:[model],routing_revision:8,next_after:''});
 });vi.stubGlobal('fetch',fetch);
 wrapper=mount(DefaultModelsPanel,{attachTo:document.body});await wrapper.get('#openDefaultModels').trigger('click');await flushPromises();
 expect(wrapper.get('[aria-label="选择 blocked-v1"]').attributes('disabled')).toBeDefined();
 await wrapper.get('#defaultModelsGroup').setValue('g1');await wrapper.get('#applyDefaultModels').trigger('click');await flushPromises();
 const writes=fetch.mock.calls.filter(([,o])=>o?.method==='POST');expect(writes).toHaveLength(1);
 expect(JSON.parse(writes[0][1].body as string)).toEqual({version:1,catalog_revision:'catalog-hash',expected_routing_revision:7,names:['deepseek-v4.1-flash'],group_id:'g1',expected_group_revision:3});
 expect(wrapper.get('#defaultModelsDialog').attributes('open')).toBeUndefined();expect(wrapper.text()).toContain('默认模型已应用');
});
it('retains selected defaults when the server rejects a stale preview',async()=>{
 const {default:DefaultModelsPanel}=await import('../features/models/DefaultModelsPanel.vue');
 vi.stubGlobal('fetch',vi.fn((_path:string,options:RequestInit)=>options?.method==='POST'?json({error:{message:'目录变化',reason:'revision_conflict'}},409):json({version:1,catalog_revision:'old',routing_revision:1,providers:['qoder'],models:[{name:'qoder-auto',sources:[{id:'qoder/cn/auto',enabled:true}],status:'create',message:'',existing_id:''}]})));
 wrapper=mount(DefaultModelsPanel,{attachTo:document.body});await wrapper.get('#openDefaultModels').trigger('click');await flushPromises();await wrapper.get('#applyDefaultModels').trigger('click');await flushPromises();
 expect(wrapper.get('#defaultModelsError').text()).toContain('目录变化');expect(wrapper.get('#defaultModelsDialog').attributes('open')).toBe('');expect(wrapper.get('[aria-label="选择 qoder-auto"]').element).toHaveProperty('checked',true);
});
it('directs duplicate public names to editing the existing sources without another create',async()=>{
 vi.stubGlobal('fetch',vi.fn((path:string)=>path.endsWith(model.id)?json({model,client_policy:null,routing_revision:1,references:[],source_status:[]}):json({models:[model],routing_revision:1,next_after:''})));
 wrapper=mount(UnifiedModelsPanel,{attachTo:document.body});await flushPromises();await wrapper.get('#addUnifiedModel').trigger('click');await flushPromises();await wrapper.get('#unifiedModelName').setValue(model.name);
 const button=wrapper.findAll('button').find(b=>b.text()==='编辑已有模型并添加来源')!;await button.trigger('click');await flushPromises();expect(wrapper.get('#unifiedModelTitle').text()).toBe('编辑统一模型');expect(wrapper.findAll('#unifiedSourceRows .unified-source')).toHaveLength(2);
});

it('shows one canonical model by default while retaining an explicit legacy-name view',async()=>{
 const old={...model,id:'um_'+'2'.repeat(32),name:'workbuddy-deepseek-v4.1-flash',sources:[model.sources[0]]};
 const canonical={...model,name:'deepseek-v4.1-flash'};
 vi.stubGlobal('fetch',vi.fn(()=>json({models:[old,canonical],routing_revision:1,next_after:'',compatibility_aliases:{[old.id]:canonical.name}})));
 wrapper=mount(UnifiedModelsPanel,{attachTo:document.body});await flushPromises();
 expect(wrapper.findAll('#unifiedModels tr')).toHaveLength(1);expect(wrapper.get('#unifiedModels').text()).toContain(canonical.name);expect(wrapper.get('#unifiedModels').text()).not.toContain(old.name);
 await wrapper.get('.unified-toolbar input[type=checkbox]').setValue(true);
 expect(wrapper.findAll('#unifiedModels tr')).toHaveLength(2);
});
