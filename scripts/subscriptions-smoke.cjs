// Isolated UI contracts: all provider/admin traffic is intercepted.
const {chromium}=require('playwright'),assert=require('node:assert/strict'),fs=require('node:fs');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});try{
 for(const locale of ['zh-CN','en-US']){
  const ctx=await browser.newContext({locale,viewport:{width:1280,height:1000}}),page=await ctx.newPage(),errors=[];
  page.on('pageerror',e=>errors.push(e.message));let accounts=[],active='',starts=0,saves=0,checks=0,imports=0,savedSettings;
  const settings={pool:{},common_prompt:{prompt_mode:'passthrough',prompt_text:'COMMON',prompt_fallback:false,sanitize:false},chatgpt:{prompt_mode:'passthrough',prompt_overrides:{},search_enabled:false},claude:{prompt_mode:'passthrough',prompt_overrides:{},search_enabled:false}};
  await ctx.route('**/auth/session',r=>r.fulfill({json:{authenticated:true,user:{id:'builtin:admin',username:'admin',role:'admin'}}}));
  await ctx.route('**/admin/api/**',route=>{const request=route.request(),url=new URL(request.url());let out={ok:true};
   if(url.pathname.endsWith('/state'))out={accounts,keys:[],models:[],settings,stats:{}};
   if(url.pathname.endsWith('/credits'))out={accounts:accounts.map(a=>({id:a.id,ok:true,credit:{available:false}}))};
   if(url.pathname.endsWith('/provider-login')){const b=request.postDataJSON();
    if(b.action==='start'){active=b.provider;starts++;assert.equal(b.scope,'global');out={session_id:active+'-flow',url:active==='chatgpt'?'https://auth.openai.com/oauth/authorize?state=fixture':'https://claude.com/cai/oauth/authorize?state=fixture',scope:'global'};}
    if(b.action==='check'){checks++;assert.match(b.input,/fixture/);out={ready:true,name:active+' fixture',uid:'masked',scope:'global',preview_id:active+'-preview',existing:false};}
    if(b.action==='complete'){saves++;assert.equal(b.preview_id,active+'-preview');accounts.push({id:active,name:active+' fixture',provider:active,scope:'global',enabled:true,non_expiring:true,status:'ok',source:'browser'});}
   }
   if(url.pathname.endsWith('/provider-import')){imports++;const b=request.postDataJSON();assert.ok(['chatgpt','claude'].includes(b.provider));assert.equal(b.credentials.api_key,b.provider==='chatgpt'?'sk-fixture':'sk-ant-fixture');}
   if(url.pathname.endsWith('/usage'))out={page:1,pages:1,total:0,retained:0,retention_limit:100000,stats:{},logs:[]};
   if(url.pathname.endsWith('/settings')&&request.method()==='POST')savedSettings=request.postDataJSON();return route.fulfill({json:out});
  });
  await ctx.route('**/health',r=>r.fulfill({json:{accounts:accounts.length,ready:accounts.length,models:0}}));
  const ready=page.waitForResponse(r=>r.url().endsWith('/admin/api/state'));await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10088')+'/#accounts');await ready;
  for(const name of ['chatgpt','claude']){
   const beforeStarts=starts,beforeSaves=saves;await page.locator('#addAccount').click();await page.locator('#'+name+'Login').click();assert.equal(starts,beforeStarts);
   await page.locator('#accountMethodBrowser').click();await page.locator('#accountCreateNext').click();assert.deepEqual(await page.locator('#providerScope option').evaluateAll(o=>o.map(x=>x.value)),['global']);
   assert.equal(await page.locator('#providerTraeDevice').isVisible(),false);assert.equal(await page.locator('#providerLoginCallback').isVisible(),true);
   await page.locator('#providerLoginStart').click();await page.waitForFunction(()=>document.querySelector('#providerLoginURL').value.includes('state=fixture'));assert.equal(ctx.pages().length,1);assert.equal(saves,beforeSaves);
   await page.locator('#providerLoginCallback').fill(name==='chatgpt'?'http://localhost:1455/auth/callback?code=fixture&state=fixture':'fixture#fixture');await page.locator('#providerLoginCheck').click();await page.locator('#providerPreview').waitFor({state:'visible'});assert.equal(saves,beforeSaves);
   await page.locator('#providerLoginComplete').click();await page.locator('#addAccountDialog').waitFor({state:'hidden'});await page.locator('#accountProvider').selectOption(name);assert.equal(await page.locator('#accounts tr[data-state]').count(),1);
   await page.locator('#nav-overview').click();await Promise.all([page.waitForResponse(r=>r.url().includes('/usage?')&&r.url().includes('provider='+name)),page.locator('#usageProvider').selectOption(name)]);
   await page.locator('#nav-settings').click();const prefix=name==='chatgpt'?'sCG':'sCL';assert.equal(await page.locator('#'+prefix+'PromptTextInherit').isChecked(),true);assert.equal(await page.locator('#'+prefix+'PromptText').inputValue(),'COMMON');
   await page.locator('#'+prefix+'PromptTextInherit').uncheck();await page.locator('#'+prefix+'PromptText').fill(name+' CUSTOM');await Promise.all([page.waitForResponse(r=>r.url().endsWith('/settings')&&r.request().method()==='POST'),page.locator('#saveSettings').click()]);assert.equal(savedSettings[name].prompt_overrides.prompt_text,name+' CUSTOM');assert.equal(savedSettings[name].search_enabled,false);
   await page.locator('#nav-accounts').click();await page.locator('#addAccount').click();await page.locator('#'+name+'Login').click();await page.locator('#accountMethodImport').click();await page.locator('#accountCreateNext').click();assert.equal(await page.locator('#providerImportSection').isVisible(),true);
   await page.locator('#providerImportJSON').fill(JSON.stringify({api_key:name==='chatgpt'?'sk-fixture':'sk-ant-fixture'}));await page.locator('#providerImport').click();await page.locator('#addAccountDialog').waitFor({state:'hidden'});
  }
  await page.locator('#addAccount').click();await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);assert.ok(await page.locator('#chatgptLogin').isVisible());assert.ok(await page.locator('#claudeLogin').isVisible());
  fs.mkdirSync('artifacts',{recursive:true});await page.screenshot({path:'artifacts/subscriptions-'+locale+'-mobile.png'});assert.equal(starts,2);assert.equal(saves,2);assert.equal(checks,2);assert.equal(imports,2);assert.deepEqual(errors,[]);await ctx.close();
 }
 console.log('PASS: ChatGPT / Claude, Chinese / English, global-only scopes, manual callback, preview then save, key imports, filters, prompt overrides and mobile layout.');
}finally{await browser.close();}})().catch(e=>{console.error(e.stack);process.exitCode=1;});
