// Unified account wizard fixtures; no real account or upstream mutations.
const {chromium}=require('playwright');const assert=require('node:assert/strict');const fs=require('node:fs');
(async()=>{
 const browser=await chromium.launch({channel:'chrome',headless:true});
 try{
  const ctx=await browser.newContext({viewport:{width:1280,height:1000}});const page=await ctx.newPage(),errors=[];
  let starts=0,cancels=0,failCancel=true,failImport=true,imports=0;
  page.on('pageerror',e=>errors.push(e.message));
  await ctx.route('**/admin/auth/session',r=>r.fulfill({json:{authenticated:true}}));
  await ctx.route('**/admin/api/**',route=>{
   const req=route.request(),u=new URL(req.url());let out={ok:true};
   if(u.pathname.endsWith('/state'))out={accounts:[],keys:[],models:[],settings:{pool:{}},stats:{}};
   if(u.pathname.endsWith('/provider-login')){
    const b=req.postDataJSON();
    if(b.action==='start'){starts++;assert.equal(b.provider,'workbuddy');out={session_id:'fixture',url:'https://www.workbuddy.cn/login?state=fixture'};}
    if(b.action==='cancel'){cancels++;if(failCancel)return route.fulfill({status:503,json:{error:{message:'模拟取消失败'}}});}
   }
   if(u.pathname.endsWith('/accounts')){
    imports++;const b=req.postDataJSON();assert.equal(b.action,'manual');assert.equal(b.name,'统一入口测试');
    if(failImport)return route.fulfill({status:500,json:{error:{message:'模拟保存失败'}}});
   }
   return route.fulfill({json:out});
  });
  await ctx.route('**/health',r=>r.fulfill({json:{accounts:0,ready:0}}));
  const ready=page.waitForResponse(r=>r.url().endsWith('/admin/api/state'));
  await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10088')+'/#accounts');await ready;
  assert.equal(await page.locator('.card-actions #addAccount').count(),1);
  assert.equal(await page.locator('.card-actions #browserLogin,.card-actions #workbuddyLogin,.card-actions #traeLogin,.card-actions #importLocal').count(),0);
  assert.equal(await page.locator('#manualAccountPanel').isVisible(),false);
  await page.locator('#addAccount').click();await page.locator('#accountCreateName').fill('统一入口测试');
  await page.locator('#traeLogin').click();assert.equal(await page.locator('#traeLogin').getAttribute('aria-pressed'),'true');
  assert.equal(starts,0);assert.equal(await page.locator('#providerDeviceID').isVisible(),false);
  fs.mkdirSync('artifacts',{recursive:true});await page.evaluate(()=>document.documentElement.dataset.theme='light');
  await page.screenshot({path:'artifacts/add-account-platforms.png'});
  await page.locator('#accountCreateNext').click();assert.equal(await page.locator('#providerTraeDevice').isVisible(),true);assert.equal(await page.locator('#providerImportSection').isVisible(),false);
  await page.locator('#providerLoginCallback').fill('PRIVATE_CALLBACK');await page.locator('#accountCreateBack').click();await page.locator('#accountChoose').waitFor({state:'visible'});
  assert.equal(await page.locator('#providerLoginCallback').inputValue(),'');
  await page.locator('#workbuddyLogin').click();await page.locator('#accountCreateNext').click();
  assert.equal(await page.locator('#providerTraeDevice').isVisible(),false);await page.locator('#providerLoginStart').click();await page.waitForFunction(()=>document.querySelector('#providerLoginURL').value.includes('fixture'));
  await page.locator('#accountCreateBack').click();await page.waitForFunction(()=>document.querySelector('#accountCreateError').textContent.includes('模拟取消失败'));
  assert.equal(await page.locator('#accountAuthorize').isVisible(),true);failCancel=false;
  await page.locator('#accountCreateBack').click();await page.locator('#accountChoose').waitFor({state:'visible'});assert.equal(cancels,2);
  await page.locator('#traeLogin').click();await page.locator('#accountMethodImport').click();await page.locator('#accountCreateNext').click();
  assert.equal(await page.locator('#providerImportSection').isVisible(),true);assert.equal(await page.locator('#providerBrowserFields').isVisible(),false);
  await page.locator('#providerImportJSON').fill('PRIVATE_IMPORT');await page.locator('#accountCreateBack').click();await page.locator('#accountChoose').waitFor({state:'visible'});assert.equal(await page.locator('#providerImportJSON').inputValue(),'');
  await page.locator('#browserLogin').click();await page.locator('#accountCreateNext').click();
  assert.equal(await page.locator('#manualAccountPanel').isVisible(),true);assert.equal(await page.locator('#manualName').inputValue(),'统一入口测试');
  await page.locator('#manualAK').fill('fixture-ak');await page.locator('#manualSK').fill('fixture-sk');await page.locator('#addManual').click();
  await page.waitForFunction(()=>document.querySelector('#accountCreateError').textContent.includes('模拟保存失败'));
  assert.equal(await page.locator('#addAccountDialog').evaluate(el=>el.open),true);assert.equal(await page.locator('#manualSK').inputValue(),'fixture-sk');
  failImport=false;await page.locator('#addManual').click();await page.waitForFunction(()=>!document.querySelector('#addAccountDialog').open);assert.equal(imports,2);
  assert.equal(await page.locator('#manualSK').inputValue(),'');
  await page.locator('#addAccount').click();await page.setViewportSize({width:390,height:844});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await page.screenshot({path:'artifacts/add-account-mobile.png'});
  await page.keyboard.press('Escape');await page.waitForFunction(()=>!document.querySelector('#addAccountDialog').open);
  assert.deepEqual(errors,[]);console.log('PASS: one add-account entry, platform/method selection, no automatic authorization, back/cancel failure and retry, credential isolation, import failure retention/success clearing and mobile.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
