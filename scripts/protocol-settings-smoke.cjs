// Isolated protocol configuration acceptance: all API calls are intercepted.
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const {selectPlatform}=require('./account-onboarding-helpers.cjs');
(async()=>{const browser=await chromium.launch({channel:'chrome',headless:true});try{
for(const locale of ['zh-CN','en-US']){
 const context=await browser.newContext({locale,viewport:{width:1100,height:900}}),page=await context.newPage(),errors=[];let fail=true,saves=0;
 page.on('pageerror',e=>errors.push(e.message));
 const account={id:'fixture',name:'Kimi fixture',provider:'kimi',scope:'cn',enabled:true,paused:false,source:'manual',status:'ready',api_protocol:'chat_completions',api_protocol_options:['adaptive','chat_completions','responses','anthropic'],daily_claim_supported:false};
 await context.route('**/setup/status',r=>r.fulfill({json:{needs_setup:false,ready:true}}));
 await context.route('**/auth/session',r=>r.fulfill({json:{authenticated:true,user:{id:'builtin:admin',username:'admin',role:'admin'}}}));
 await context.route('**/admin/api/**',route=>{
 const req=route.request(),path=new URL(req.url()).pathname;let out={ok:true};
 if(path.endsWith('/state'))out={accounts:[account],keys:[],models:[],settings:{pool:{}},stats:{}};
 if(path.endsWith('/accounts') && req.method()==='POST'){const body=req.postDataJSON();assert.equal(body.action,'protocol');assert.equal(body.id,'fixture');assert.equal(body.api_protocol,'adaptive');saves++;if(fail)return route.fulfill({status:500,json:{error:{message:'fixture save failed'}}});account.api_protocol=body.api_protocol;out={ok:true,id:body.id,api_protocol:body.api_protocol};}
 return route.fulfill({json:out});
 });
 await context.route('**/health',r=>r.fulfill({json:{}}));
 const ready=page.waitForResponse(r=>r.url().endsWith('/admin/api/state'));await page.goto(process.env.CODEARTS_TEST_BASE+'/#accounts');await ready;
 await page.locator('#accounts .account-actions button').filter({has:page.locator('svg')}).last().click();
 await page.locator('#accountActionMenu button').filter({hasText:locale==='zh-CN'?'API 协议':'API protocol'}).click();
 assert.equal(await page.locator('#accountProtocol').inputValue(),'chat_completions');await page.locator('#accountProtocol').selectOption('adaptive');await page.locator('#accountProtocolSave').click();await page.locator('#accountProtocolError').filter({hasText:'fixture save failed'}).waitFor();assert.equal(await page.locator('#accountProtocol').inputValue(),'adaptive');fail=false;await page.locator('#accountProtocolSave').click();await page.locator('#accountProtocolDialog').waitFor({state:'hidden'});assert.equal(saves,2);
 await page.locator('#addAccount').click();await selectPlatform(page,'kimi');await page.locator('#accountCreateNext').click();assert.equal(await page.locator('#providerProtocol').inputValue(),'adaptive');assert.equal(await page.locator('#providerProtocolField').isVisible(),true);
 await page.screenshot({path:`artifacts/protocol-settings-${locale}.png`});
 await page.setViewportSize({width:360,height:800});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await page.screenshot({path:`artifacts/protocol-settings-${locale}-mobile.png`});
 assert.deepEqual(errors,[]);await context.close();
}console.log('Protocol settings: defaults, edit, failed save/retry, Chinese/English passed');
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exit(1)});
