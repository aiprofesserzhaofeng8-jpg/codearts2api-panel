// Isolated fixtures: group editing, key binding and model previews. No credentials or inference.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({channel:'chrome',headless:true});
 try {
  const ctx=await browser.newContext({viewport:{width:1440,height:1000}}),page=await ctx.newPage(),errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  const data={accounts:[
   {id:'wb',name:'WB fixture',provider:'workbuddy',scope:'cn',enabled:true,status:'ok'},
   {id:'trae',name:'Trae fixture',provider:'trae',scope:'cn',enabled:true,status:'ok'}
  ],models:[
   {id:'workbuddy/cn/shared',provider:'workbuddy',scope:'cn',name:'shared'},
   {id:'workbuddy/cn/other',provider:'workbuddy',scope:'cn',name:'other'},
   {id:'trae/cn/shared',provider:'trae',scope:'cn',name:'shared'}
  ],keys:[{ref:'fixture-ref',key:'masked-fixture',name:'Existing key',enabled:true,created_at:1,requests:0}],groups:[],settings:{},stats:{}};
  let failSave=true,failBinding=true,failCreate=true,saves=0,keyCreates=0,releaseCreate,modelRefreshes=0;
  const modelProbes=[];
  await ctx.route('**/admin/auth/session',r=>r.fulfill({json:{authenticated:true}}));
  await ctx.route('**/admin/api/**',route=>{
   const req=route.request(),u=new URL(req.url());let out={ok:true};
   if(u.pathname.endsWith('/state'))out=data;
   if(u.pathname.endsWith('/credits'))out={accounts:[]};
   if(u.pathname.endsWith('/models/refresh')){modelRefreshes++;out={models:data.models};}
   if(u.pathname.endsWith('/models/probe')){
    const body=req.postDataJSON();assert.ok(Array.isArray(body.models)&&body.models.length);
    modelProbes.push(body.models);out={results:body.models.map(model=>({model,ok:true})),total:body.models.length,available:body.models.length,unavailable:0};
   }
   if(u.pathname.endsWith('/groups')){
    if(req.method()==='POST'){
     saves++;if(failSave)return route.fulfill({status:500,json:{error:{message:'fixture save failed'}}});
     const body=req.postDataJSON(),g={...body,id:body.id||'group-fixture'};
     data.groups=data.groups.filter(x=>x.id!==g.id).concat(g);out={ok:true,group:g};
    }else if(req.method()==='DELETE')data.groups=data.groups.filter(g=>g.id!==u.searchParams.get('id'));
    else out={groups:data.groups};
   }
   if(u.pathname.endsWith('/keys/group')){
    if(failBinding)return route.fulfill({status:500,json:{error:{message:'fixture binding failed'}}});
    const body=req.postDataJSON();assert.equal(body.ref,'fixture-ref');data.keys[0].group_id=body.group_id;
   }
   if(u.pathname.endsWith('/keys')&&req.method()==='POST'){
    keyCreates++;
    if(failCreate)return route.fulfill({status:500,json:{error:{message:'fixture key creation failed'}}});
    const body=req.postDataJSON();assert.equal(body.group_id,'group-fixture');
    return new Promise(resolve=>{releaseCreate=()=>{
     data.keys.push({ref:'new-ref',key:'masked-new',name:body.name,enabled:true,group_id:body.group_id});
     route.fulfill({json:{ok:true,key:'fixture-not-a-real-key'}}).then(resolve);
    };});
   }
   return route.fulfill({json:out});
  });
  await ctx.route('**/health',r=>r.fulfill({json:{accounts:2,ready:2}}));
  await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10088')+'/#groups');
  await page.locator('#tab-groups.on').waitFor();
  assert.equal(await page.locator('#pageTitle').textContent(),'账号分组');
  assert.equal(await page.locator('#nav-groups').getAttribute('aria-selected'),'true');
  assert.equal(await page.locator('#tab-keys #addGroup').count(),0);
  await page.reload();await page.locator('#tab-groups.on').waitFor();
  await page.locator('#addGroup').click();
  await page.locator('#groupSave').click();assert.equal(saves,0);
  await page.locator('#groupName').fill('WB only');
  await page.locator('#groupAccounts input[value="wb"]').check();
  assert.equal(await page.locator('#groupModels option').count(),3);
  assert.equal(await page.locator('#groupModels option[value="workbuddy/cn/web-search"]').count(),1);
  assert.equal(await page.locator('#groupModels option[value="workbuddy/global/web-search"]').count(),0);
  assert.equal(await page.locator('#groupModels option[value="trae/cn/web-search"]').count(),0);
  await page.locator('#groupAccounts input[value="trae"]').check();
  assert.equal(await page.locator('#groupModels option[value="trae/cn/web-search"]').count(),1);
  await page.locator('#groupAccounts input[value="trae"]').uncheck();
  assert.equal(await page.locator('#groupModels option[value="trae/cn/web-search"]').count(),0);
  assert.equal(await page.locator('#groupModels').isDisabled(),true);
  await page.locator('#groupAllowlist').check();await page.locator('#groupModels').selectOption(['workbuddy/cn/shared']);
  await page.locator('#groupSave').click();await page.waitForFunction(()=>document.querySelector('#groupError').textContent.includes('fixture save failed'));
  assert.equal(await page.locator('#groupName').inputValue(),'WB only');assert.equal(data.groups.length,0);
  failSave=false;await page.locator('#groupSave').click();await page.locator('#groupDialog').waitFor({state:'hidden'});
  await page.locator('#groups tr').filter({hasText:'WB only'}).waitFor();
  assert.deepEqual(data.groups[0].account_ids,['wb']);assert.deepEqual(data.groups[0].model_ids,['workbuddy/cn/shared']);
  await page.screenshot({path:'artifacts/groups-page-desktop.png'});
  await page.locator('#nav-keys').click();
  assert.equal(await page.locator('#addGroup').isVisible(),false);
  const keyRow=()=>page.locator('#keys tr').filter({hasText:'Existing key'});
  await keyRow().getByRole('button',{name:'分组',exact:true}).click();await page.locator('#keyGroupSelect').selectOption('group-fixture');
  await page.locator('#keyGroupSave').click();await page.waitForFunction(()=>document.querySelector('#keyGroupError').textContent.includes('fixture binding failed'));
  assert.equal(data.keys[0].group_id,undefined);assert.equal(await page.locator('#keyGroupSelect').inputValue(),'group-fixture');
  failBinding=false;await page.locator('#keyGroupSave').click();await page.locator('#keyGroupDialog').waitFor({state:'hidden'});
  await page.waitForFunction(()=>document.querySelector('#keys').textContent.includes('WB only'));
  await page.locator('#nav-groups').click();
  assert.equal(await page.locator('#groups tr').getByRole('button',{name:'删除',exact:true}).isDisabled(),true);
  await page.locator('#nav-keys').click();
  assert.equal(await page.locator('#tab-keys .card-head input, #tab-keys .card-head select').count(),0);
  await page.locator('#addKey').click();await page.locator('#keyName').fill('Canceled draft');await page.locator('#keyCreateCancel').click();
  assert.equal(keyCreates,0);assert.equal(data.keys.length,1);
  await page.locator('#addKey').click();assert.equal(await page.locator('#keyName').inputValue(),'');
  await page.locator('#keyCreateGroup').selectOption('group-fixture');await page.locator('#keyName').fill('New grouped key');
  await page.screenshot({path:'artifacts/key-create-desktop.png'});
  await page.locator('#keyCreateSave').click();await page.waitForFunction(()=>document.querySelector('#keyCreateError').textContent.includes('fixture key creation failed'));
  assert.equal(await page.locator('#keyName').inputValue(),'New grouped key');assert.equal(await page.locator('#keyCreateGroup').inputValue(),'group-fixture');
  assert.equal(data.keys.length,1);
  await page.setViewportSize({width:390,height:844});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  await page.evaluate(()=>{document.querySelector('#keyCreateError').textContent='';document.documentElement.dataset.theme='light';});
  await page.screenshot({path:'artifacts/key-create-mobile-light.png'});
  await page.evaluate(()=>document.documentElement.dataset.theme='dark');
  await page.screenshot({path:'artifacts/key-create-mobile-dark.png'});
  await page.evaluate(()=>document.documentElement.dataset.theme='light');
  await page.setViewportSize({width:1440,height:1000});
  failCreate=false;
  await page.locator('#keyCreateForm').evaluate(form=>{form.requestSubmit();form.requestSubmit();});
  await page.waitForFunction(()=>document.querySelector('#keyCreateSave').textContent==='创建中…');
  assert.equal(await page.locator('#keyName').isDisabled(),true);assert.equal(await page.locator('#keyCreateCancel').isDisabled(),true);
  await page.keyboard.press('Escape');assert.equal(await page.locator('#keyCreateDialog').isVisible(),true);
  for(let attempt=0;!releaseCreate&&attempt<100;attempt++)await new Promise(resolve=>setTimeout(resolve,10));
  assert.equal(typeof releaseCreate,'function');assert.equal(keyCreates,2);releaseCreate();
  await page.locator('#keyCreateDialog').waitFor({state:'hidden'});
  await page.waitForFunction(()=>document.querySelector('#keys').textContent.includes('New grouped key'));
  assert.equal(data.keys[1].group_id,'group-fixture');
  await page.locator('#nav-models').click();await page.locator('#modelsGroupFilter').selectOption('group-fixture');
  assert.equal(await page.locator('#models tr').count(),1);assert.match(await page.locator('#models').textContent(),/workbuddy\/cn\/shared/);
  assert.doesNotMatch(await page.locator('#models').textContent(),/trae\/cn/);
  assert.equal(await page.locator('#modelsCount').textContent(),'1 个');
  await page.locator('#modelSearch').fill('  SHARED  ');assert.equal(await page.locator('#modelsCount').textContent(),'1 个');
  await page.locator('#modelSearch').fill('not-a-model');assert.equal(await page.locator('#modelsCount').textContent(),'0 个');
  await page.locator('#probeAllModels').click();assert.equal(modelProbes.length,0);
  await page.locator('#modelSearch').fill('');
  page.once('dialog',d=>{assert.match(d.message(),/1 个模型/);d.accept();});
  await page.locator('#probeAllModels').click();await page.waitForFunction(()=>document.querySelector('#modelHint').textContent==='可用 1 / 1');
  assert.deepEqual(modelProbes[0],['workbuddy/cn/shared']);
  await page.locator('#modelsGroupFilter').selectOption('');await page.locator('#modelSearch').fill('workbuddy/');
  assert.equal(await page.locator('#modelsCount').textContent(),'2 个');
  page.once('dialog',d=>d.accept());await page.locator('#probeAllModels').click();
  await page.waitForFunction(()=>document.querySelector('#modelHint').textContent==='可用 2 / 2');
  assert.deepEqual(modelProbes[1],['workbuddy/cn/shared','workbuddy/cn/other']);
  await page.locator('#modelSearch').fill('');await page.locator('#refreshModels').click();
  await page.waitForFunction(()=>document.querySelector('#modelsCount').textContent==='3 个');assert.equal(modelRefreshes,1);
  await page.screenshot({path:'artifacts/model-toolbar-desktop-light.png'});
  await page.locator('#themeToggle').click();await page.screenshot({path:'artifacts/model-toolbar-desktop-dark.png'});await page.locator('#themeToggle').click();
  await page.setViewportSize({width:390,height:844});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  const searchBox=await page.locator('#modelSearch').boundingBox(),groupBox=await page.locator('#modelsGroupFilter').boundingBox();
  assert.ok(Math.abs(searchBox.y-groupBox.y)<1,'model filters should share one row on mobile');
  const refreshBox=await page.locator('#refreshModels').boundingBox(),probeBox=await page.locator('#probeAllModels').boundingBox();
  assert.ok(Math.abs(refreshBox.y-probeBox.y)<1,'model actions should share one row on mobile');
  await page.screenshot({path:'artifacts/model-toolbar-mobile-light.png'});
  await page.locator('#themeToggle').click();await page.screenshot({path:'artifacts/model-toolbar-mobile-dark.png'});await page.locator('#themeToggle').click();
  await page.setViewportSize({width:320,height:740});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  await page.setViewportSize({width:1440,height:1000});
  await page.locator('#modelsGroupFilter').selectOption('group-fixture');
  await page.locator('#nav-groups').click();await page.locator('#groups tr').getByRole('button',{name:'编辑',exact:true}).click();
  assert.equal(await page.locator('#groupAllowlist').isChecked(),true);assert.deepEqual(await page.locator('#groupModels').evaluate(e=>Array.from(e.selectedOptions).map(o=>o.value)),['workbuddy/cn/shared']);
  await page.setViewportSize({width:390,height:844});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  await page.screenshot({path:'artifacts/groups-mobile.png'});
  await page.locator('#groupModels').selectOption([]);await page.locator('#groupSave').click();await page.locator('#groupDialog').waitFor({state:'hidden'});
  await page.locator('#nav-models').click();assert.equal(await page.locator('#models').textContent(),'没有符合条件的模型');
  await page.reload();await page.locator('#modelsGroupFilter option[value="group-fixture"]').waitFor({state:'attached'});await page.locator('#modelsGroupFilter').selectOption('group-fixture');
  assert.equal(await page.locator('#models').textContent(),'没有符合条件的模型');
  await page.locator('#nav-keys').click();await keyRow().getByRole('button',{name:'分组',exact:true}).click();
  await page.locator('#keyGroupSelect').selectOption('');await page.locator('#keyGroupSave').click();await page.locator('#keyGroupDialog').waitFor({state:'hidden'});
  assert.equal(data.keys[0].group_id,'');
  // Remove the synthetic second binding before checking group deletion.
  data.keys[1].group_id='';await page.locator('#nav-groups').click();await page.reload();await page.locator('#groups tr').filter({hasText:'WB only'}).waitFor();
  page.once('dialog',d=>d.accept());await page.locator('#groups tr').getByRole('button',{name:'删除',exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('#groups').textContent.includes('尚未创建分组'));
  assert.equal(data.groups.length,0);assert.deepEqual(errors,[]);
  console.log('PASS: groups/key creation, binding guards, model search/group filters/counts/refresh, filtered batch probe payload, empty lists and desktop/mobile themes.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
