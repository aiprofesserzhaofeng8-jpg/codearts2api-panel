// Isolated onboarding acceptance; no real credentials or upstream calls.
const {chromium}=require('playwright'),assert=require('node:assert/strict'),fs=require('node:fs');
const {selectPlatform}=require('./account-onboarding-helpers.cjs');
(async()=>{const browser=await chromium.launch({channel:process.env.CODEARTS_TEST_BROWSER||'chrome',headless:true});try{
for(const locale of ['zh-CN','en-US']){
 const ctx=await browser.newContext({locale,viewport:{width:1280,height:1000}}),page=await ctx.newPage(),errors=[];
 let starts=0,cancels=0,failCancel=true,failImport=true,imports=0,missingConfig=false;
 page.on('pageerror',e=>errors.push(e.message));
 await ctx.route('**/auth/session',r=>r.fulfill({json:{authenticated:true,user:{id:'builtin:admin',username:'admin',role:'admin'}}}));
 await ctx.route('**/admin/api/**',route=>{
  const req=route.request(),u=new URL(req.url());let out={ok:true};
  if(u.pathname.endsWith('/state'))out={accounts:[],keys:[],models:[],settings:{pool:{}},stats:{}};
  if(u.pathname.endsWith('/provider-login')){
   const b=req.postDataJSON();
   if(b.action==='start'){starts++;if(missingConfig)return route.fulfill({status:400,json:{error:{message:'OAuth client not configured'}}});assert.equal(b.provider,'workbuddy');out={session_id:'fixture',url:'https://www.workbuddy.cn/login?state=fixture'};}
   if(b.action==='cancel'){cancels++;if(failCancel)return route.fulfill({status:503,json:{error:{message:'fixture-cancel-failure'}}});}
  }
  if(u.pathname.endsWith('/accounts')){imports++;const b=req.postDataJSON();assert.equal(b.action,'manual');assert.equal(b.name,'统一入口测试');if(failImport)return route.fulfill({status:500,json:{error:{message:'fixture-save-failure'}}});}
  return route.fulfill({json:out});
 });
 await ctx.route('**/health',r=>r.fulfill({json:{accounts:0,ready:0}}));
 const ready=page.waitForResponse(r=>r.url().endsWith('/admin/api/state'));await page.goto(process.env.CODEARTS_TEST_BASE+'/#accounts');await ready;
 await page.locator('#addAccount').click();
 assert.equal(await page.locator('#browserLogin').getAttribute('aria-checked'),'true');
 assert.equal(await page.locator('[data-account-platform]:visible').count(),11);
 assert.deepEqual(await page.locator('[data-account-platform]:visible').evaluateAll(es=>es.map(e=>e.textContent.replace('✓','').trim())),['OpenAI','Claude','Gemini','DeepSeek','Zhipu','Kimi','MiniMax','Grok','CodeArts','WorkBuddy','Trae']);
 assert.equal(await page.locator('#accountCreateName').isVisible(),false);
 const positions=await page.locator('.account-platform:visible').evaluateAll(es=>es.map(e=>({y:e.offsetTop,h:e.offsetHeight})));
 assert.equal(new Set(positions.map(p=>p.y)).size,3);assert.ok(positions.every(p=>p.h===56));
 fs.mkdirSync('artifacts',{recursive:true});
 for(const width of [1280,360])for(const theme of ['light','dark']){
  await page.setViewportSize({width,height:width===360?800:1000});await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  assert.ok(await page.locator('#accountCreateNext').isVisible());
  await page.screenshot({path:`artifacts/onboarding-${locale}-${width}-${theme}.png`});
 }
 await page.setViewportSize({width:1280,height:1000});
 await page.locator('#accountPlatformsMore').click();assert.equal(await page.locator('[data-account-platform]:visible').count(),20);
 assert.equal(await page.locator('#accountPlatformsMore').getAttribute('aria-expanded'),'true');
 const grid=await page.locator('[data-account-platform]:visible').evaluateAll(es=>es.map(e=>({x:e.getBoundingClientRect().x,y:e.getBoundingClientRect().y})));
 assert.equal(new Set(grid.map(p=>p.y)).size,5);
 for(let i=0;i<20;i++){assert.equal(grid[i].x,grid[i%4].x);assert.equal(grid[i].y,grid[Math.floor(i/4)*4].y);}
 assert.equal(await page.locator('.platform-icon img').evaluateAll(es=>es.every(e=>e.complete && e.naturalWidth>0)),true);
 assert.equal(await page.locator('.platform-icon').first().evaluate(e=>getComputedStyle(e).backgroundColor==='rgba(0, 0, 0, 0)'),false);
 assert.equal(await page.locator('#accountPlatformsMore').isVisible(),false);
 assert.equal(await page.evaluate(()=>document.activeElement.id),'antigravityLogin');
 await selectPlatform(page,'qoder');await page.locator('#accountPlatformsLess').click();
 assert.equal(await page.locator('#qoderLogin').isVisible(),false);assert.equal(await page.locator('#accountPlatformsMore').isVisible(),true);assert.equal(await page.locator('#accountPlatformsLess').isVisible(),false);assert.match(await page.locator('#accountSelectedSummary').textContent(),/Qoder/);
 await page.locator('#accountSelectedSummary').click();assert.equal(await page.evaluate(()=>document.activeElement.id),'qoderLogin');
 await page.screenshot({path:`artifacts/onboarding-${locale}-expanded.png`});
 await page.evaluate(()=>document.documentElement.dataset.theme='light');await page.screenshot({path:`artifacts/onboarding-${locale}-expanded-light.png`});await page.evaluate(()=>document.documentElement.dataset.theme='dark');
 await page.setViewportSize({width:360,height:800});await page.locator('#commandcodeLogin').scrollIntoViewIfNeeded();
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
 assert.equal(await page.locator('[data-account-platform]').evaluateAll(es=>es.every(e=>e.scrollWidth<=e.clientWidth)),true);
 await page.screenshot({path:`artifacts/onboarding-${locale}-360-expanded.png`});await page.setViewportSize({width:1280,height:1000});
 await selectPlatform(page,'claude');assert.deepEqual(await page.locator('[data-account-type]').evaluateAll(es=>es.map(e=>e.dataset.accountType)),['oauth','api','bedrock']);
 await selectPlatform(page,'vertex');await page.locator('#accountCreateNext').click();assert.match(await page.locator('#accountAuthorizationPath').textContent(),/Gemini \/ Vertex AI/);assert.equal(await page.locator('#providerImportJSON').isVisible(),true);
 await page.screenshot({path:`artifacts/onboarding-${locale}-vertex.png`});
 await page.locator('#accountCreateBack').click();await selectPlatform(page,'gemini');await page.locator('[data-account-type="api"]').click();await page.locator('#accountCreateNext').click();await page.locator('#providerAPIKey').fill('PRIVATE_API_KEY');
 await page.locator('#accountCreateBack').click();await page.locator('[data-account-type="cli"]').click();await page.locator('#accountSwitchCancel').click();assert.equal(await page.locator('#providerAPIKey').inputValue(),'PRIVATE_API_KEY');
 await page.locator('[data-account-type="cli"]').click();await page.locator('#accountSwitchConfirm').click();await page.locator('#accountSwitchDialog').waitFor({state:'hidden'});assert.equal(await page.locator('#providerAPIKey').inputValue(),'');
 assert.equal(await page.evaluate(()=>JSON.stringify(localStorage).includes('PRIVATE_API_KEY')),false);
 await selectPlatform(page,'trae');await page.locator('#accountMethodBrowser').click();await page.locator('#accountCreateNext').click();
 await page.locator('#providerLoginCallback').fill('PRIVATE_CALLBACK');await page.locator('#accountCreateBack').click();assert.equal(await page.locator('#providerLoginCallback').inputValue(),'PRIVATE_CALLBACK');
 await page.locator('#accountCreateNext').click();assert.equal(await page.locator('#providerLoginCallback').inputValue(),'PRIVATE_CALLBACK');await page.locator('#accountCreateBack').click();
 await selectPlatform(page,'workbuddy');await page.locator('#accountSwitchDialog').waitFor({state:'visible'});await page.locator('#accountSwitchCancel').click();assert.equal(await page.locator('#traeLogin').getAttribute('aria-checked'),'true');
 await selectPlatform(page,'workbuddy');await page.locator('#accountSwitchConfirm').click();await page.locator('#accountSwitchDialog').waitFor({state:'hidden'});assert.equal(await page.locator('#providerLoginCallback').inputValue(),'');assert.equal(starts,0);
 await page.locator('#accountCreateNext').click();await page.locator('#providerLoginStart').click();await page.waitForFunction(()=>document.querySelector('#providerLoginURL').value.includes('fixture'));
 await page.locator('#accountCreateBack').click();assert.equal(cancels,0);
 await selectPlatform(page,'codearts');await page.locator('#accountSwitchConfirm').click();await page.waitForFunction(()=>document.querySelector('#accountCreateError').textContent.includes('fixture-cancel-failure'));assert.equal(await page.locator('#workbuddyLogin').getAttribute('aria-checked'),'true');
 failCancel=false;await selectPlatform(page,'codearts');await page.locator('#accountSwitchConfirm').click();await page.locator('#accountSwitchDialog').waitFor({state:'hidden'});assert.equal(cancels,2);
 await page.locator('#accountMethodImport').click();await page.locator('#accountCreateNext').click();await page.locator('#accountCreateName').fill('统一入口测试');
 await page.locator('#manualAK').fill('fixture-ak');await page.locator('#manualSK').fill('fixture-sk');await page.locator('#addManual').click();await page.waitForFunction(()=>document.querySelector('#accountCreateError').textContent.includes('fixture-save-failure'));
 assert.equal(await page.locator('#manualSK').inputValue(),'fixture-sk');failImport=false;await page.locator('#addManual').click();await page.locator('#addAccountDialog').waitFor({state:'hidden'});assert.equal(imports,2);assert.equal(await page.locator('#manualSK').inputValue(),'');assert.equal(await page.evaluate(()=>document.activeElement.id),'addAccount');
 await page.locator('#addAccount').click();await selectPlatform(page,'gemini');await page.locator('[data-account-type="cli"]').click();await page.locator('#accountMethodBrowser').click();await page.locator('#accountCreateNext').click();missingConfig=true;await page.locator('#providerLoginStart').click();await page.waitForFunction(()=>document.querySelector('#providerLoginError').textContent.includes('OAuth client not configured'));assert.match(await page.locator('#providerLoginError').textContent(),/Client ID/);assert.equal(await page.locator('#providerLoginOpen').isVisible(),false);
 await page.keyboard.press('Escape');await page.locator('#addAccountDialog').waitFor({state:'hidden'});assert.equal(await page.evaluate(()=>document.activeElement.id),'addAccount');
 await page.locator('#addAccount').click();await page.locator('#browserLogin').focus();await page.keyboard.press('ArrowRight');assert.equal(await page.locator('#workbuddyLogin').getAttribute('aria-checked'),'true');await page.keyboard.press('Escape');
 assert.deepEqual(errors,[]);await ctx.close();
}
console.log('PASS onboarding: exact platform order/grid, cloud channels, expansion/focus/keyboard, preserved drafts, confirmed switches, cancel/save retries, OAuth configuration errors; desktop/360px, light/dark, Chinese/English.');
}finally{await browser.close();}})().catch(e=>{console.error(e.stack);process.exitCode=1;});
