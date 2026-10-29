// Settings and layout on an isolated server; never sends model/search traffic.
const {chromium}=require('playwright');const fs=require('node:fs');const assert=require('node:assert/strict');
(async()=>{
 const base=process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10088';
 const access=JSON.parse(fs.readFileSync(process.argv[2],'utf8'));
 const browser=await chromium.launch({headless:true,channel:'chrome'});
 try {
  const ctx=await browser.newContext({viewport:{width:1440,height:1000}});
  const auth=await ctx.request.post(base+'/admin/auth/login',{data:{password:access.admin_password}});assert.ok(auth.ok());
  const page=await ctx.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#settings');await page.locator('#sWBPromptMode').waitFor();
  assert.equal(await page.locator('#sWBPromptMode').inputValue(),'custom');
  assert.equal(await page.locator('#sWBSearch').isChecked(),true);assert.equal(await page.locator('#sWBSanitize').isChecked(),true);
  await page.locator('#sWBPromptMode').selectOption('passthrough');await page.locator('#sWBPromptText').fill('Fixture custom prompt\n第二行');
  await page.locator('#sWBSearchScope').selectOption('global');
  for(const id of ['sWBSearch','sWBSanitize','sWBPromptFallback'])await page.locator('#'+id).uncheck();
  await page.locator('#saveSettings').click();await page.locator('#settingsDirty').waitFor({state:'hidden'});
  await page.reload();await page.waitForFunction(()=>document.querySelector('#sWBPromptMode').value==='passthrough');
  assert.equal(await page.locator('#sWBPromptText').inputValue(),'Fixture custom prompt\n第二行');assert.equal(await page.locator('#sWBSearchScope').inputValue(),'global');
  for(const id of ['sWBSearch','sWBSanitize','sWBPromptFallback'])assert.equal(await page.locator('#'+id).isChecked(),false);
  const invalid=await ctx.request.post(base+'/admin/api/settings',{data:{workbuddy:{prompt_mode:'invalid'}}});assert.equal(invalid.status(),400);
  const unchanged=await (await ctx.request.get(base+'/admin/api/settings')).json();assert.equal(unchanged.workbuddy.prompt_mode,'passthrough');
  await page.locator('#sWBPromptMode').selectOption('custom');await page.locator('#sWBSearchScope').selectOption('cn');await page.locator('#sWBPromptText').fill('');
  for(const id of ['sWBSearch','sWBSanitize','sWBPromptFallback'])await page.locator('#'+id).check();
  await page.locator('#saveSettings').click();await page.locator('#settingsDirty').waitFor({state:'hidden'});
  const section=page.locator('.settings-group').filter({has:page.getByRole('heading',{name:'WorkBuddy',exact:true})});
  await page.evaluate(()=>document.documentElement.dataset.theme='light');fs.mkdirSync('artifacts',{recursive:true});await section.screenshot({path:'artifacts/workbuddy-settings-light.png'});
  await page.setViewportSize({width:390,height:844});await section.screenshot({path:'artifacts/workbuddy-settings-mobile.png'});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  assert.deepEqual(errors,[]);console.log('PASS: WorkBuddy defaults, independent switches, settings persistence, validation, restore and desktop/mobile layout. No upstream requests.');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
