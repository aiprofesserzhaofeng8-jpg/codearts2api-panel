const {selectPlatform}=require('./account-onboarding-helpers.cjs');
// Isolated official API wizard: no external provider calls or production state.
const {chromium}=require('playwright'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});try{
 for(const locale of ['zh-CN','en-US']){
 const ctx=await browser.newContext({locale,viewport:{width:1280,height:1000}}),page=await ctx.newPage(),errors=[];
 let fail=true,imports=0,logins=0,coding=false;
 page.on('pageerror',e=>errors.push(e.message));
 await ctx.route('**/auth/session',r=>r.fulfill({json:{authenticated:true,user:{id:'builtin:admin',username:'admin',role:'admin'}}}));
 await ctx.route('**/admin/api/**',r=>{const path=new URL(r.request().url()).pathname;let out={ok:true};
 if(path.endsWith('/state'))out={accounts:[],keys:[],models:[],settings:{pool:{}},stats:{}};
 if(path.endsWith('/provider-login'))logins++;
 if(path.endsWith('/provider-import')){imports++;const b=r.request().postDataJSON();assert.ok(['deepseek','zhipu','kimi','minimax','gemini'].includes(b.provider));assert.deepEqual(b.credentials,{api_key:'fixture-key',scope:coding?'coding-cn':b.provider==='gemini'?'api':'cn',account_mode:coding?'coding':'payg'});if(fail)return r.fulfill({status:500,json:{error:{message:'fixture-save-failure'}}});}
 return r.fulfill({json:out});});
 await ctx.route('**/health',r=>r.fulfill({json:{accounts:0,ready:0}}));
 const ready=page.waitForResponse(r=>r.url().endsWith('/admin/api/state'));await page.goto(process.env.CODEARTS_TEST_BASE+'/#accounts');await ready;
 for(const [p,mode] of [['deepseek','payg'],['zhipu','payg'],['kimi','payg'],['minimax','payg'],['gemini','payg'],['kimi','coding'],['zhipu','coding'],['minimax','coding']]){
 coding=mode==='coding';
 await page.locator('#addAccount').click();await selectPlatform(page,p);
 assert.equal(await page.locator('[data-account-method="browser"]').isVisible(),false);
 if(p==='gemini')await page.locator('[data-account-method="import"]').click();
 if(['kimi','zhipu','minimax'].includes(p))await page.locator(`[data-account-type="${mode}"]`).click();
 await page.locator('#accountCreateNext').click();
 assert.equal(await page.locator('#providerBrowserFields').isVisible(),false);
 assert.equal(await page.locator('#providerImportJSON').isVisible(),false);
 assert.equal(await page.locator('#providerAPIKey').getAttribute('type'),'password');
 assert.equal(await page.locator('#codingImportModeField').isVisible(),false);

 await page.locator('#providerAPIKey').fill('fixture-key');fail=true;await page.locator('#providerImport').click();
 await page.waitForFunction(()=>document.querySelector('#providerLoginError').textContent.includes('fixture-save-failure'));
 assert.equal(await page.locator('#addAccountDialog').isVisible(),true);assert.equal(await page.locator('#providerAPIKey').inputValue(),'fixture-key');
 fail=false;await page.locator('#providerImport').click();await page.locator('#addAccountDialog').waitFor({state:'hidden'});
 assert.equal(await page.locator('#providerAPIKey').inputValue(),'');
 }
 assert.equal(imports,16);assert.equal(logins,0);assert.deepEqual(errors,[]);await ctx.close();
 }
 console.log('PASS five official API platforms and three Coding modes, no OAuth, key input, failed-save retry, secret clearing, Chinese/English');
}finally{await browser.close();}})().catch(e=>{console.error(e.stack);process.exitCode=1;});
