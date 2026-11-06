// Real application APIs and synthetic in-process providers. Never use a live service.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});
 const out=process.env.CODEARTS_U5_ARTIFACTS||'artifacts/unified-models-u5';fs.mkdirSync(out,{recursive:true});
 try{
  const context=await browser.newContext({locale:'zh-CN',viewport:{width:1440,height:1000}}),page=await context.newPage(),errors=[],writes=[];
  page.on('pageerror',e=>errors.push(e.message));
  context.on('request',r=>{if(['POST','PUT','DELETE'].includes(r.method())&&r.url().includes('/admin/api/'))writes.push({path:new URL(r.url()).pathname,method:r.method(),body:r.postDataJSON()});});
  const base=process.env.CODEARTS_TEST_BASE;
  assert.ok(base?.startsWith('http://127.0.0.1:'));
  const login=await context.request.post(base+'/auth/login',{data:{username:'admin',password:process.env.CODEARTS_TEST_ADMIN_PASSWORD}});assert.equal(login.status(),200,await login.text());
  await page.goto(base+'/#models');await page.locator('#addUnifiedModel:enabled').waitFor();
  await page.locator('#addUnifiedModel').click();await page.locator('#unifiedModelDialog').waitFor();
  await page.locator('#unifiedModelName').fill('shared-public-1');
  await page.locator('#unifiedSourceSelect').selectOption('workbuddy/cn/shared-v1');
  await page.locator('#unifiedSourceSelect').selectOption('trae/cn/shared-v1');
  assert.equal(await page.locator('#unifiedSourceRows .unified-source').count(),2);
  await page.locator('#unifiedModelSave').click();await page.locator('#unifiedModelDialog').waitFor({state:'hidden'});
  const modelRow=()=>page.locator('#unifiedModels tr').filter({hasText:'shared-public-1'});
  const editModel=async()=>{await modelRow().getByRole('button',{name:'编辑',exact:true}).click();await page.locator('#unifiedModelDialog').waitFor();};
  await modelRow().waitFor();await editModel();
  assert.equal(await page.locator('#unifiedModelName').getAttribute('readonly'),'');
  const probeResponse=page.waitForResponse(r=>new URL(r.url()).pathname==='/admin/api/models/probe');
  page.once('dialog',d=>d.accept());await page.locator('#unifiedSourceRows .unified-source').first().getByRole('button',{name:'测活',exact:true}).click();
  const probed=await probeResponse;const probePayload=await probed.json();assert.equal(probed.status(),200,JSON.stringify(probePayload));assert.equal(probePayload.available,1,JSON.stringify(probePayload));
  await page.getByText('文本测活成功',{exact:false}).waitFor();
  await page.locator('#unifiedModelDialog').getByRole('button',{name:'取消',exact:true}).click();
  await page.locator('#nav-groups').click();await page.locator('#addGroup').click();await page.locator('#groupName').fill('Unified browser group');
  for(const box of await page.locator('#groupAccounts input').all())await box.check();
  await page.getByRole('checkbox',{name:'授权 shared-public-1',exact:true}).check();
  await page.locator('#groupDefaultModel').selectOption('shared-public-1');await page.locator('#groupClientDefaultModel').selectOption('shared-public-1');
  await page.locator('#groupSave').click();await page.locator('#groupDialog').waitFor({state:'hidden'});
  const groupWrite=writes.find(w=>w.path==='/admin/api/groups'&&w.method==='POST');assert.ok(groupWrite);assert.equal('model_mappings' in groupWrite.body,false);assert.equal(groupWrite.body.unified_models[0].source_mode,'selected');assert.equal(groupWrite.body.unified_models[0].source_ids.length,2);
  await page.locator('#nav-models').click();await editModel();
  const trae=page.locator('#unifiedSourceRows .unified-source').filter({hasText:'trae/cn/shared-v1'});await trae.getByRole('checkbox').uncheck();
  await page.locator('#unifiedModelSave').click();await page.locator('#unifiedModelDialog').waitFor({state:'hidden'});
  await page.reload();await editModel();assert.equal(await page.locator('#unifiedSourceRows .unified-source').filter({hasText:'trae/cn/shared-v1'}).getByRole('checkbox').isChecked(),false);
  // A concurrent writer changes the routing version after this editor opened.
  let list=await (await context.request.get(base+'/admin/api/unified-models')).json(),m=list.models[0];
  const other=await context.request.put(base+'/admin/api/unified-models/'+m.id,{data:{name:m.name,enabled:false,sources:m.sources,client_policy:null,expected_revision:m.revision,expected_routing_revision:list.routing_revision}});assert.equal(other.status(),200,await other.text());
  await page.locator('#unifiedModelSave').click();await page.locator('#unifiedModelError').filter({hasText:'草稿已保留'}).waitFor();assert.equal(await page.locator('#unifiedModelDialog').isVisible(),true);assert.equal(await page.locator('#unifiedModelEnabled').isChecked(),true);
  page.once('dialog',d=>d.accept());await page.locator('#unifiedModelDialog .modal-actions').getByRole('button',{name:'重新读取',exact:true}).click();await page.waitForFunction(()=>!document.querySelector('#unifiedModelEnabled').checked);
  await page.locator('#unifiedModelEnabled').check();await page.locator('#unifiedModelSave').click();await page.locator('#unifiedModelDialog').waitFor({state:'hidden'});
  // A missing directory entry stays bound and can be saved without data loss.
  await context.request.post(base+'/fixture/remove-source');await page.reload();await editModel();await page.getByText('目录缺失，配置保留',{exact:true}).waitFor();
  assert.equal(await page.locator('#unifiedSourceRows .unified-source').count(),2);await page.locator('#unifiedModelSave').click();await page.locator('#unifiedModelDialog').waitFor({state:'hidden'});
  // A forbidden write retains the draft without converting it to a successful save.
  await editModel();
  await context.route('**/admin/api/unified-models/*',route=>route.request().method()==='PUT'?route.fulfill({status:403,json:{error:{code:403,type:'permission_error',reason:'model_permission_denied',message:'synthetic permission denied'}}}):route.continue());
  await page.locator('#unifiedModelSave').click();await page.locator('#unifiedModelError').filter({hasText:'synthetic permission denied'}).waitFor();assert.equal(await page.locator('#unifiedModelDialog').isVisible(),true);await context.unroute('**/admin/api/unified-models/*');
  await page.screenshot({animations:'disabled',path:path.join(out,'models-desktop-light.png')});
  for(const [width,height] of [[390,844],[320,740]]){await page.setViewportSize({width,height});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);assert.equal(await page.locator('#unifiedModelDialog').evaluate(e=>e.scrollWidth>e.clientWidth+1),false);await page.screenshot({path:path.join(out,`models-${width}.png`)});}
  await page.locator('#unifiedModelDialog').getByRole('button',{name:'取消',exact:true}).click();
  await page.setViewportSize({width:1440,height:1000});await page.locator('#nav-groups').click();await page.locator('#groups tr').getByRole('button',{name:'编辑',exact:true}).click();await page.locator('#groupDialog').waitFor();assert.equal(await page.locator('#groupClientDefaultModel').inputValue(),'shared-public-1');
  await page.locator('#groupName').fill('Retained browser draft');
  let group=(await(await context.request.get(base+'/admin/api/groups')).json()).groups[0];
  const changedGroup=await context.request.post(base+'/admin/api/groups',{data:{...group,name:'Other editor',expected_revision:group.revision}});assert.equal(changedGroup.status(),200,await changedGroup.text());
  await page.locator('#groupSave').click();await page.locator('#groupError').filter({hasText:'草稿已保留'}).waitFor();assert.equal(await page.locator('#groupName').inputValue(),'Retained browser draft');
  page.once('dialog',d=>d.accept());await page.locator('#groupDialog .modal-actions').getByRole('button',{name:'重新读取',exact:true}).click();await page.waitForFunction(()=>document.querySelector('#groupName').value==='Other editor');
  await page.screenshot({animations:'disabled',path:path.join(out,'groups-desktop-light.png')});await page.evaluate(()=>document.documentElement.dataset.theme='dark');await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await page.screenshot({animations:'disabled',path:path.join(out,'groups-mobile-dark.png')});
  assert.equal(await page.locator('#applyGroupPresets').count(),0);assert.equal(await page.locator('#addGroupMapping').count(),0);
  await page.locator('#groupCancel').click();
  await page.setViewportSize({width:1440,height:1000});await page.evaluate(()=>document.documentElement.dataset.theme='light');
  await page.locator('#nav-models').click();await page.locator('#reloadModelPolicies').click();
  await page.locator('#modelPolicies tr').filter({hasText:'workbuddy/cn/shared-v1'}).getByRole('button',{name:'编辑',exact:true}).click();await page.locator('#policyKeep').fill('8192');await page.locator('#policySave').click();await page.locator('#policyDialog').waitFor({state:'hidden'});
  await page.locator('#nav-settings').click();await page.locator('#sPw').fill('discarded-synthetic-password');page.once('dialog',d=>d.accept());await page.locator('#reloadSettings').click();await page.waitForFunction(()=>document.querySelector('#sPw').value==='');await page.locator('#sTimeout').fill('75');await page.locator('#saveSettings').click();await page.locator('#settingsDirty').waitFor({state:'hidden'});
  const settings=await(await context.request.get(base+'/admin/api/settings')).json();assert.equal(settings.request_timeout_sec,75);
  // Empty catalog blocks new bindings while existing model definitions remain visible.
  const state=await(await context.request.get(base+'/admin/api/state')).json();
  await context.route('**/admin/api/state',r=>r.fulfill({json:{...state,models:[]}}));await page.goto(base+'/#models');await page.reload();await page.locator('#addUnifiedModel:enabled').click();await page.getByText('原始目录为空，请先添加账号并刷新模型清单。',{exact:true}).waitFor();assert.equal(await page.locator('#unifiedSourceSelect option').count(),1);
  await page.locator('#unifiedModelDialog').getByRole('button',{name:'取消',exact:true}).click();await context.unroute('**/admin/api/state');await page.reload();
  // Migration reports are read locally and rendered as text.
  await page.setViewportSize({width:1440,height:1000});await page.locator('#nav-models').click();await page.locator('.unified-help summary').first().click();
  await page.locator('#migrationReport').setInputFiles({name:'migration-report.json',mimeType:'application/json',buffer:Buffer.from(JSON.stringify({from_schema:10,to_schema:11,ready:false,definitions:1,group_changes:1,conflicts:[{reason:'model_name_conflict',group_ids:['g1'],model_name:'<img src=x onerror=window.injected=true>',field_path:'groups.model_mappings',message:'synthetic duplicate name'}],warnings:[]}))});
  await page.locator('#migrationReportResult').filter({hasText:'model_name_conflict'}).waitFor();assert.equal(await page.locator('#migrationReportResult img').count(),0);
  // Default catalog uses real management APIs and survives page refresh.
  await page.locator('#openDefaultModels').click();await page.locator('#defaultModelsDialog').waitFor();
  await page.getByRole('checkbox',{name:'选择 deepseek-v4.1-flash',exact:true}).waitFor();
  await page.setViewportSize({width:390,height:844});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  await page.screenshot({animations:'disabled',path:path.join(out,'defaults-mobile.png')});
  await page.setViewportSize({width:1440,height:1000});
  await page.screenshot({animations:'disabled',path:path.join(out,'defaults-desktop.png')});
  const defaultWrite=page.waitForResponse(r=>new URL(r.url()).pathname==='/admin/api/model-defaults'&&r.request().method()==='POST');
  await page.locator('#applyDefaultModels').click();assert.equal((await defaultWrite).status(),200);
  await page.locator('#defaultModelsDialog').waitFor({state:'hidden'});await page.reload();
  await page.locator('#unifiedModels tr').filter({hasText:'deepseek-v4.1-flash'}).waitFor();
  await page.locator('#openDefaultModels').click();await page.locator('#defaultModelsDialog').waitFor();
  assert.ok(await page.locator('#defaultModelsDialog').getByText('已配置',{exact:true}).count()>0);
  await page.locator('#defaultModelsDialog').getByRole('button',{name:'关闭',exact:true}).click();
  assert.deepEqual(errors,[]);
  fs.writeFileSync(path.join(out,'browser-results.json'),JSON.stringify({passed:true,real_management_api:true,real_upstream:false,checks:['default preview all current platforms','default batch application and reload','idempotent default preview','default mobile layout','create two sources','probe text only','grant selected sources','client default','disable source','refresh persistence','model and group CAS conflict with retained drafts','missing catalog retained','403 retained draft','desktop/mobile/theme','raw policy CAS','settings CAS','empty source catalog','local migration report safe text'],writes:writes.map(w=>({path:w.path,method:w.method,has_revision:!!w.body?.expected_revision,has_routing_revision:!!w.body?.expected_routing_revision}))},null,2));
  console.log('PASS U5 real management API browser acceptance');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
