const {selectPlatform}=require('./account-onboarding-helpers.cjs');
// Browser-only fixtures. No Google authorization, account tokens or production state.
const {chromium}=require('playwright'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});try{
for(const locale of ['zh-CN','en-US']){
 const ctx=await browser.newContext({locale,viewport:{width:1280,height:1000}}),page=await ctx.newPage(),errors=[];
 let starts=0,checks=0,saves=0,imports=0,fail=true;
 page.on('pageerror',e=>errors.push(e.message));
 await ctx.route('**/auth/session',r=>r.fulfill({json:{authenticated:true,user:{id:'builtin:admin',username:'admin',role:'admin'}}}));
 await ctx.route('**/admin/api/**',r=>{const path=new URL(r.request().url()).pathname;let out={ok:true};
 if(path.endsWith('/state'))out={accounts:[],keys:[],models:[],settings:{pool:{}},stats:{}};
 if(path.endsWith('/provider-login')){const b=r.request().postDataJSON();
  if(b.action==='start'){starts++;assert.equal(b.provider,'gemini');assert.equal(b.scope,'cli');out={session_id:'fixture-login',url:'https://accounts.google.com/o/oauth2/v2/auth?state=fixture-state',scope:'cli'};}
  if(b.action==='check'){checks++;assert.ok(b.input.includes('codeassist.google.com/authcode'));out={ready:true,preview_id:'fixture-preview',name:'Gemini fixture',uid:'fixture-user',scope:'cli',existing:false};}
  if(b.action==='complete'){saves++;assert.equal(b.preview_id,'fixture-preview');if(fail)return r.fulfill({status:500,json:{error:{message:'fixture-save-failure'}}});out={ok:true};}
 }
 if(path.endsWith('/provider-import')){imports++;const b=r.request().postDataJSON();assert.equal(b.provider,'gemini');assert.equal(b.credentials.scope,'cli');assert.equal(b.credentials.project_id,'fixture-project');}
 return r.fulfill({json:out});});
 await ctx.route('**/health',r=>r.fulfill({json:{accounts:0,ready:0}}));
 const ready=page.waitForResponse(r=>r.url().endsWith('/admin/api/state'));await page.goto(process.env.CODEARTS_TEST_BASE+'/#accounts');await ready;
 await page.locator('#addAccount').click();await selectPlatform(page,'gemini');await page.locator('[data-account-type="cli"]').click();await page.locator('[data-account-method="browser"]').click();await page.locator('#accountCreateNext').click();
 assert.equal(await page.locator('#providerScope').inputValue(),'cli');assert.equal(await page.locator('#providerImportSection').isVisible(),false);
 await page.locator('#providerLoginStart').click();await page.locator('#providerLoginOpen').waitFor({state:'visible'});
 assert.ok((await page.locator('#providerLoginOpen').getAttribute('href')).startsWith('https://accounts.google.com/'));
 await page.locator('#providerLoginCallback').fill('https://codeassist.google.com/authcode?code=fixture-code&state=fixture-state');await page.locator('#providerLoginCheck').click();await page.locator('#providerPreview').waitFor({state:'visible'});
 assert.equal(await page.locator('#providerLoginCallback').inputValue(),'');assert.ok((await page.locator('#providerPreviewText').textContent()).includes('cli'));
 await page.locator('#providerLoginComplete').click();await page.waitForFunction(()=>document.querySelector('#providerLoginError').textContent.includes('fixture-save-failure'));
 assert.equal(await page.locator('#addAccountDialog').isVisible(),true);fail=false;await page.locator('#providerLoginComplete').click();await page.locator('#addAccountDialog').waitFor({state:'hidden'});
 await page.locator('#addAccount').click();await selectPlatform(page,'gemini');await page.locator('[data-account-type="cli"]').click();await page.locator('[data-account-method="import"]').click();await page.locator('#accountCreateNext').click();
 assert.equal(await page.locator('#providerAPIKey').isVisible(),false);
 await page.locator('#providerImportJSON').fill(JSON.stringify({scope:'cli',project_id:'fixture-project',access_token:'fake-token'}));await page.locator('#providerImport').click();await page.locator('#addAccountDialog').waitFor({state:'hidden'});
 assert.equal(starts,1);assert.equal(checks,1);assert.equal(saves,2);assert.equal(imports,1);assert.deepEqual(errors,[]);await ctx.close();
}
console.log('PASS Gemini CLI browser authorization/preview/save retry, API versus CLI import, Chinese/English');
}finally{await browser.close();}})().catch(e=>{console.error(e.stack);process.exitCode=1;});
