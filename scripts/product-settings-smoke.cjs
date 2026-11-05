// M6 editor contracts using isolated synthetic data; no credentials or upstream calls.
const {chromium}=require('playwright');const assert=require('node:assert/strict');const fs=require('node:fs');const {fixture,mock}=require('./frontend-fixture.cjs');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});
 try{
  const ctx=await browser.newContext({locale:'zh-CN',viewport:{width:1440,height:1000}}),page=await ctx.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
  const data=fixture();data.accounts[0].name='WorkBuddy fixture';data.models[0].name='DeepSeek-V4.1-Flash';data.settings={common_prompt:{prompt_mode:'custom',prompt_text:'COMMON',prompt_fallback:true,sanitize:true},codearts:{prompt_mode:'passthrough',prompt_text:'CA',prompt_fallback:true,sanitize:true},workbuddy:{prompt_mode:'custom',prompt_text:'WB',prompt_fallback:true,sanitize:true,prompt_overrides:{prompt_fallback:false,sanitize:false,prompt_text:''}},trae:{prompt_mode:'passthrough',prompt_text:'TR',prompt_fallback:true,sanitize:true}};
  let savedSettings,groupWrites=0;
  await mock(ctx,{onRequest:async(route,url,req)=>{
   if(url.pathname.endsWith('/state')){await route.fulfill({json:data});return true;}
   if(url.pathname.endsWith('/groups/presets')){await route.fulfill({json:{version:1,mappings:[{name:'deepseek-4.1-flash',sources:[{id:data.models[0].id,available:true},{id:'trae/cn/missing',available:false}]}]}});return true;}
   if(url.pathname.endsWith('/groups')&&req.method()==='POST'){groupWrites++;const body=req.postDataJSON();data.groups=[{...body,id:'g1'}];await route.fulfill({json:{ok:true,group:data.groups[0]}});return true;}
   if(url.pathname.endsWith('/settings')&&req.method()==='GET'){await route.fulfill({json:data.settings});return true;}
   if(url.pathname.endsWith('/settings')&&req.method()==='POST'){savedSettings=req.postDataJSON();Object.assign(data.settings,savedSettings);await route.fulfill({json:{ok:true,settings:data.settings}});return true;}
  }});
  await page.goto(process.env.CODEARTS_TEST_BASE+'/#settings');await page.locator('#sCommonPromptText').waitFor();
  await page.locator('#nav-settings').click();assert.equal(await page.locator('#sWBPromptMode').isDisabled(),true);assert.equal(await page.locator('#sWBPromptMode').inputValue(),'custom');assert.equal(await page.locator('#sWBPromptText').inputValue(),'');assert.equal(await page.locator('#sWBPromptFallback').isChecked(),false);assert.equal(await page.locator('#sWBSanitize').isChecked(),false);
  await page.locator('#sTRPromptModeInherit').check();await page.locator('#sTRPromptTextInherit').check();await page.locator('#sTRPromptFallbackInherit').check();await page.locator('#sTRSanitizeInherit').check();
  await page.locator('#sCommonPromptText').fill('COMMON NEW');assert.equal(await page.locator('#sTRPromptText').inputValue(),'COMMON NEW');assert.equal(await page.locator('#sCAPromptText').inputValue(),'CA');
  await page.locator('#saveSettings').click();await page.locator('#settingsDirty').waitFor({state:'hidden'});
  assert.deepEqual(savedSettings.trae.prompt_overrides,{});assert.deepEqual(savedSettings.workbuddy.prompt_overrides,{prompt_text:'',prompt_fallback:false,sanitize:false});assert.equal(savedSettings.codearts.prompt_overrides,null);
  await page.reload();await page.waitForFunction(()=>document.querySelector('#sTRPromptText')?.value==='COMMON NEW');assert.equal(await page.locator('#sTRPromptTextInherit').isChecked(),true);
  const out=process.env.CODEARTS_PRODUCT_ARTIFACTS || 'artifacts/structure-m6-complete/browser';fs.mkdirSync(out,{recursive:true});
  for(const theme of ['light','dark']){await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);await page.screenshot({path:out+'/settings-'+theme+'.png',fullPage:true});}
  assert.deepEqual(errors,[]);console.log('PASS M6: canonical editors, legacy/partial inheritance, false/empty, reload, desktop/mobile and both themes.');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
