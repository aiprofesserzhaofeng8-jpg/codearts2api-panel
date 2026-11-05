// Staged browser helper UI; all provider responses and identities are fixtures.
const {chromium}=require('playwright'),assert=require('node:assert/strict'),fs=require('node:fs/promises');
const url='https://www.teleai.com.cn/teleai-api/upms/public/auth/authorize?client_id=Z2veLLEHhZfq3hb5hWIwqJuPajIuasYi&redirect_uri=https%3A%2F%2Fagent.teleai.com.cn%2FsuperCowork%2Fsapi%2Fcowork%2Fauth';
const file=(challenge)=>({name:'result.json',mimeType:'application/json',buffer:Buffer.from(JSON.stringify({version:1,provider:'teleagent',challenge,credentials:{token:'fixture-secret',api_key:'sk-owned-fixture',device_id:'original-device',install_id:'original-install',app_version:'2.7.1'}}))});
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});try{for(const locale of ['zh-CN','en-US']){
 const ctx=await browser.newContext({locale,viewport:{width:1440,height:1000},acceptDownloads:true}),page=await ctx.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
 let starts=0,checks=0,saves=0,cancels=0,deny=true,challenge,accounts=[];
 await ctx.route('**/auth/session',r=>r.fulfill({json:{authenticated:true,user:{id:'builtin:admin',role:'admin',username:'admin'}}}));
 await ctx.route('**/admin/api/**',r=>{const req=r.request(),path=new URL(req.url()).pathname;let out={ok:true};
  if(path.endsWith('/state'))out={accounts,keys:[],models:[],settings:{pool:{}},stats:{}};
  if(path.endsWith('/provider-login')){const b=req.postDataJSON();
   if(b.action==='start'){starts++;challenge=(starts===1?'A':'B').repeat(43);out={session_id:'fixture-'+starts,url,helper_challenge:challenge,expires_at:Math.floor(Date.now()/1000)+600};}
   if(b.action==='cancel')cancels++;
   if(b.action==='check'){checks++;const result=JSON.parse(b.input);assert.equal(result.challenge,challenge);assert.equal(result.credentials.token,'fixture-secret');if(deny){deny=false;return r.fulfill({status:502,json:{error:{message:'Upstream identity fixture denied'}}});}out={ready:true,name:'Verified TeleAgent',uid:'fi***re',scope:'cn',preview_id:'fixture-preview',has_refresh:false};}
   if(b.action==='complete'){saves++;assert.equal(b.preview_id,'fixture-preview');accounts=[{id:'browser-fixture',provider:'teleagent',scope:'cn',name:'Verified TeleAgent',enabled:true,source:'browser',status:'ok',expires_at:2000000000}];}
  }
  if(path.endsWith('/usage'))out={page:1,pages:1,total:0,stats:{},logs:[]};
  return r.fulfill({json:out});
 });
 await ctx.route('**/health',r=>r.fulfill({json:{accounts:accounts.length,ready:accounts.length,models:5}}));
 await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10088')+'/#accounts');
 await page.locator('#addAccount').click();await page.locator('#teleagentLogin').click();await page.locator('#accountMethodBrowser').click();await page.locator('#accountCreateNext').click();
 assert.equal(await page.locator('#providerTeleAgentHelper').isVisible(),true);assert.equal(await page.locator('#teleagentTaskDownload').isDisabled(),true);
 assert.equal(await page.locator('#providerLoginOpen').isVisible(),false);assert.equal(await page.locator('#providerLoginCallback').isVisible(),false);
 await page.locator('#teleagentFileFallback').evaluate(e=>e.open=true);await page.locator('#teleagentManualTask').click();await page.locator('#teleagentTaskDownload:not([disabled])').waitFor();
 await page.locator('#teleagentConnectorSetup').evaluate(e=>e.open=true);
 const [helper]=await Promise.all([page.waitForEvent('download'),page.locator('#teleagentHelperDownload').click()]);
 assert.equal(helper.suggestedFilename(),'teleagent-login.mjs');const source=await fs.readFile(await helper.path(),'utf8');assert.equal(source,await fs.readFile('scripts/teleagent-login.mjs','utf8'));
 const [taskDownload]=await Promise.all([page.waitForEvent('download'),page.locator('#teleagentTaskDownload').click()]);
 const task=JSON.parse(await fs.readFile(await taskDownload.path(),'utf8'));assert.equal(task.challenge,challenge);assert.equal(task.url,url);assert.equal(task.credentials,undefined);
 assert.ok((await page.locator('#teleagentHelperCommand').inputValue()).includes(taskDownload.suggestedFilename()));
 await page.locator('#teleagentResultFile').setInputFiles(file('wrong'));await page.locator('#providerLoginError').filter({hasText:locale==='zh-CN'?'不属于本次任务':'does not belong'}).waitFor();assert.equal(checks,0);assert.equal(await page.locator('#providerLoginCallback').inputValue(),'');
 await page.locator('#teleagentResultFile').setInputFiles(file(challenge));await page.waitForFunction(()=>document.querySelector('#providerLoginCallback').value.includes('fixture-secret'));assert.equal(checks,0);assert.equal(saves,0);
 await page.locator('#teleagentManualCheck').click();await page.getByText('Upstream identity fixture denied',{exact:true}).waitFor();assert.equal(saves,0);
 await page.locator('#teleagentManualCheck').click();await page.locator('#providerPreview').waitFor({state:'visible'});assert.equal(saves,0);assert.equal(await page.locator('#providerLoginCallback').inputValue(),'');assert.ok(!(await page.locator('#providerPreviewText').textContent()).includes('fixture-secret'));
 await page.locator('#providerLoginComplete').click();await page.locator('#addAccountDialog').waitFor({state:'hidden'});assert.equal(saves,1);assert.equal(await page.locator('#teleagentHelperCommand').inputValue(),'');
 await page.locator('#addAccount').click();await page.locator('#teleagentLogin').click();await page.locator('#accountCreateNext').click();await page.locator('#teleagentFileFallback').evaluate(e=>e.open=true);await page.locator('#teleagentManualTask').click();await page.locator('#teleagentTaskDownload:not([disabled])').waitFor();
 await page.locator('#teleagentResultFile').setInputFiles(file('A'.repeat(43)));await page.locator('#providerLoginError').filter({hasText:locale==='zh-CN'?'不属于本次任务':'does not belong'}).waitFor();assert.equal(checks,2);
 await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await page.screenshot({path:'artifacts/teleagent-browser-login-'+locale+'.png',fullPage:true});
 await page.locator('#accountCreateCancel').click();await page.locator('#addAccountDialog').waitFor({state:'hidden'});assert.equal(cancels,1);assert.equal(await page.locator('#providerLoginCallback').inputValue(),'');assert.deepEqual(errors,[]);await ctx.close();
}console.log('PASS: TeleAgent helper/task downloads, result flow binding, no automatic save, identity denial, retry, preview, confirmation, cancel, secret cleanup, zh/en and mobile.');}finally{await browser.close();}})().catch(e=>{console.error(e.stack);process.exitCode=1;});
