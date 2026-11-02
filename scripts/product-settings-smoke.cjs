// M6 editor contracts using isolated synthetic data; no credentials or upstream calls.
const {chromium}=require('playwright');const assert=require('node:assert/strict');const fs=require('node:fs');const {fixture,mock}=require('./frontend-fixture.cjs');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});
 try{
  const ctx=await browser.newContext({viewport:{width:1440,height:1000}}),page=await ctx.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
  const data=fixture();data.accounts[0].name='WorkBuddy fixture';data.models[0].name='DeepSeek-V4.1-Flash';data.settings={common_prompt:{prompt_mode:'custom',prompt_text:'COMMON',prompt_fallback:true,sanitize:true},codearts:{prompt_mode:'passthrough',prompt_text:'CA',prompt_fallback:true,sanitize:true},workbuddy:{prompt_mode:'custom',prompt_text:'WB',prompt_fallback:true,sanitize:true,prompt_overrides:{prompt_fallback:false,sanitize:false,prompt_text:''}},trae:{prompt_mode:'passthrough',prompt_text:'TR',prompt_fallback:true,sanitize:true}};
  let savedSettings,groupWrites=0;
  await mock(ctx,{onRequest:async(route,url,req)=>{
   if(url.pathname.endsWith('/state')){await route.fulfill({json:data});return true;}
   if(url.pathname.endsWith('/groups/presets')){await route.fulfill({json:{version:1,mappings:[{name:'deepseek-4.1-flash',sources:[{id:data.models[0].id,available:true},{id:'trae/cn/missing',available:false}]}]}});return true;}
   if(url.pathname.endsWith('/groups')&&req.method()==='POST'){groupWrites++;const body=req.postDataJSON();data.groups=[{...body,id:'g1'}];await route.fulfill({json:{ok:true,group:data.groups[0]}});return true;}
   if(url.pathname.endsWith('/settings')&&req.method()==='POST'){savedSettings=req.postDataJSON();Object.assign(data.settings,savedSettings);await route.fulfill({json:{ok:true,settings:data.settings}});return true;}
  }});
  await page.goto(process.env.CODEARTS_TEST_BASE+'/#groups');await page.locator('#addGroup').click();await page.locator('#groupName').fill('Unified');await page.locator('#groupAccounts input').check();
  await page.locator('#applyGroupPresets:not([disabled])').waitFor();await page.locator('#applyGroupPresets').click();assert.equal(await page.locator('#groupMappingRows input').inputValue(),'deepseek-4.1-flash');
  assert.deepEqual(await page.locator('#groupMappingRows select').evaluate(el=>Array.from(el.selectedOptions).map(o=>o.value)),[data.models[0].id]);
  assert.equal(await page.locator('#groupDefaultModel').inputValue(),'deepseek-4.1-flash');assert.equal(await page.locator('#groupClientDefaultModel').inputValue(),'deepseek-4.1-flash');
  await page.locator('#applyGroupPresets').click();assert.equal(await page.locator('#groupMappingRows').locator('.group-mapping-row').count(),1);
  await page.locator('#groupMappingRows input').fill('deepseek-4.1-custom');await page.locator('#applyGroupPresets').click();assert.equal(await page.locator('#groupMappingRows input').first().inputValue(),'deepseek-4.1-custom');assert.equal(await page.locator('#groupMappingRows .group-mapping-row').count(),2);
  await page.locator('#groupDefaultModel').selectOption('deepseek-4.1-flash');await page.locator('#groupClientDefaultModel').selectOption('deepseek-4.1-flash');
  await page.locator('#groupDialog details summary').click();await page.getByRole('checkbox',{name:'deepseek-4.1-custom 显示',exact:true}).uncheck();await page.getByRole('checkbox',{name:'deepseek-4.1-custom 允许调用',exact:true}).uncheck();
  await page.locator('#groupSave').click();await page.locator('#groupDialog').waitFor({state:'hidden'});assert.equal(groupWrites,1);assert.deepEqual(data.groups[0].model_rules,[{id:'deepseek-4.1-custom',visible:false,callable:false}]);
  await page.locator('#groups').getByRole('button',{name:'编辑',exact:true}).click();assert.equal(await page.locator('#groupDefaultModel').inputValue(),'deepseek-4.1-flash');await page.locator('#groupCancel').click();
  await page.locator('#nav-settings').click();assert.equal(await page.locator('#sWBPromptMode').isDisabled(),true);assert.equal(await page.locator('#sWBPromptMode').inputValue(),'custom');assert.equal(await page.locator('#sWBPromptText').inputValue(),'');assert.equal(await page.locator('#sWBPromptFallback').isChecked(),false);assert.equal(await page.locator('#sWBSanitize').isChecked(),false);
  await page.locator('#sTRPromptModeInherit').check();await page.locator('#sTRPromptTextInherit').check();await page.locator('#sTRPromptFallbackInherit').check();await page.locator('#sTRSanitizeInherit').check();
  await page.locator('#sCommonPromptText').fill('COMMON NEW');assert.equal(await page.locator('#sTRPromptText').inputValue(),'COMMON NEW');assert.equal(await page.locator('#sCAPromptText').inputValue(),'CA');
  await page.locator('#saveSettings').click();await page.locator('#settingsDirty').waitFor({state:'hidden'});
  assert.deepEqual(savedSettings.trae.prompt_overrides,{});assert.deepEqual(savedSettings.workbuddy.prompt_overrides,{prompt_text:'',prompt_fallback:false,sanitize:false});assert.equal(savedSettings.codearts.prompt_overrides,null);
  await page.reload();await page.waitForFunction(()=>document.querySelector('#sTRPromptText').value==='COMMON NEW');assert.equal(await page.locator('#sTRPromptTextInherit').isChecked(),true);
  const out=process.env.CODEARTS_PRODUCT_ARTIFACTS || 'artifacts/structure-m6-complete/browser';fs.mkdirSync(out,{recursive:true});
  for(const theme of ['light','dark']){await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);await page.screenshot({path:out+'/settings-'+theme+'.png',fullPage:true});}
  await page.locator('#nav-groups').click();await page.locator('#groups').getByRole('button',{name:'编辑',exact:true}).click();await page.screenshot({path:out+'/groups-desktop.png'});
  await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await page.screenshot({path:out+'/groups-mobile.png'});
  assert.deepEqual(errors,[]);console.log('PASS M6: explicit presets, no overwrite/duplicates, defaults, independent flags, legacy/partial inheritance, false/empty, reload, desktop/mobile and both themes.');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
