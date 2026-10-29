// Real settings API on an isolated instance; never sends model/search requests.
const {chromium}=require('playwright');
const fs=require('node:fs');
const assert=require('node:assert/strict');
(async()=>{
 const base=process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10089';
 const access=JSON.parse(fs.readFileSync(process.argv[2],'utf8'));
 const browser=await chromium.launch({headless:true,channel:'chrome'});
 try {
  const ctx=await browser.newContext({viewport:{width:1440,height:1000}});
  assert.ok((await ctx.request.post(base+'/admin/auth/login',{data:{password:access.admin_password}})).ok());
  const original=await (await ctx.request.get(base+'/admin/api/settings')).json();
  const page=await ctx.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#settings');await page.locator('#sCAPromptMode').waitFor();
  for(const prefix of ['sCA','sTR']) {
   assert.equal(await page.locator('#'+prefix+'PromptMode').inputValue(),'passthrough');
   assert.equal(await page.locator('#'+prefix+'Sanitize').isChecked(),true);
   assert.equal(await page.locator('#'+prefix+'Search').isChecked(),false);
  }
  await page.locator('#sCAPromptMode').selectOption('custom');await page.locator('#sCAPromptText').fill('CODEARTS ONLY');
  await page.locator('#sCASanitize').uncheck();await page.locator('#sCASearchURL').fill('http://127.0.0.1:18080/search');await page.locator('#sCASearch').check();
  await page.locator('#sTRPromptText').fill('TRAE ONLY');await page.locator('#sTRPromptFallback').uncheck();await page.locator('#sTRSearchURL').fill('http://127.0.0.1:18081/search');
  await page.locator('#saveSettings').click();await page.locator('#settingsDirty').waitFor({state:'hidden'});
  await page.reload();await page.waitForFunction(()=>document.querySelector('#sCAPromptText').value==='CODEARTS ONLY');
  const saved=await (await ctx.request.get(base+'/admin/api/settings')).json();
  assert.equal(saved.codearts.prompt_mode,'custom');assert.equal(saved.codearts.sanitize,false);assert.equal(saved.codearts.search_enabled,true);assert.equal(saved.codearts.search_url,'http://127.0.0.1:18080/search');
  assert.equal(saved.trae.prompt_mode,'passthrough');assert.equal(saved.trae.prompt_text,'TRAE ONLY');assert.equal(saved.trae.sanitize,true);assert.equal(saved.trae.prompt_fallback,false);assert.equal(saved.trae.search_enabled,false);assert.equal(saved.trae.search_url,'http://127.0.0.1:18081/search');
  assert.deepEqual(saved.workbuddy,original.workbuddy);
  const invalid=await ctx.request.post(base+'/admin/api/settings',{data:{codearts:{prompt_text:'DO NOT SAVE'},trae:{search_backend:'native'}}});assert.equal(invalid.status(),400);
  const unchanged=await (await ctx.request.get(base+'/admin/api/settings')).json();assert.deepEqual(unchanged.codearts,saved.codearts);
  fs.mkdirSync('artifacts',{recursive:true});
  for(const theme of ['light','dark']) {
   await page.evaluate(value=>document.documentElement.dataset.theme=value,theme);
   const section=page.locator('.settings-group').filter({has:page.getByRole('heading',{name:'CodeArts',exact:true})});
   await section.screenshot({path:'artifacts/provider-settings-'+theme+'.png'});
  }
  await page.setViewportSize({width:390,height:844});
  const section=page.locator('.settings-group').filter({has:page.getByRole('heading',{name:'Trae',exact:true})});
  await section.screenshot({path:'artifacts/provider-settings-mobile.png'});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  assert.ok((await ctx.request.post(base+'/admin/api/settings',{data:{codearts:original.codearts,trae:original.trae,workbuddy:original.workbuddy}})).ok());
  assert.deepEqual(errors,[]);
  console.log('PASS: provider settings isolation, independent switches/URLs, reload, atomic validation, dark/light/mobile; restored defaults. No upstream calls.');
 } finally {await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
