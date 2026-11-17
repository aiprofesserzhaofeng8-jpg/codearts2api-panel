// Independent prompt/settings editors; synthetic API only, no upstream calls.
const {chromium}=require('playwright'),assert=require('node:assert/strict'),fs=require('node:fs');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});try{
 for(const locale of ['zh-CN','en-US']){
 const context=await browser.newContext({locale,viewport:{width:1280,height:1000}}),page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
 const names=['codearts','workbuddy','trae','qoder','kiro','chatgpt','claude','teleagent'];
 let config={common:{prompt_mode:'custom',prompt_text:'COMMON',prompt_fallback:false,sanitize:false},platforms:Object.fromEntries(names.map(id=>[id,{prompt_mode:'passthrough',prompt_text:id.toUpperCase(),prompt_fallback:false,sanitize:false,overrides:id==='workbuddy'?{prompt_text:'',prompt_fallback:false,sanitize:false}:id==='chatgpt'?{}:null}]))};
 let fail=true,savedSettings,saves=0,previews=0;
 await context.route('**/setup/status',r=>r.fulfill({json:{ready:true,needs_setup:false}}));
 await context.route('**/auth/session',r=>r.fulfill({json:{authenticated:true,user:{id:'builtin:admin',username:'admin',role:'admin'}}}));
 await context.route('**/admin/api/**',route=>{
  const request=route.request(),path=new URL(request.url()).pathname;let result={ok:true};
  if(path.endsWith('/state'))result={accounts:[],groups:[],keys:[],models:[],settings:{pool:{},model_routing_revision:1},stats:{}};
  if(path.endsWith('/settings')){if(request.method()==='POST'){savedSettings=request.postDataJSON();result={ok:true,settings:savedSettings};}else result={pool:{},model_routing_revision:1};}
  if(path.endsWith('/prompts')){if(request.method()==='POST'){saves++;const body=request.postDataJSON();assert.deepEqual(body.expected,config);if(fail)return route.fulfill({status:409,json:{error:{message:'fixture conflict'}}});config=body.configuration;}result=config;}
  if(path.endsWith('/prompts/preview')){previews++;const b=request.postDataJSON();assert.equal(b.text,'EXTRA');assert.equal(b.mode,'append');const r=JSON.parse(b.request_json);if(b.protocol==='responses')r.instructions+='\n\n'+b.text;else if(b.protocol==='anthropic')r.system=[{type:'text',text:r.system},{type:'text',text:b.text}];else r.messages.splice(1,0,{role:'system',content:b.text});result={request:r,request_json:JSON.stringify(r,null,2)};}
  return route.fulfill({json:result});
 });
 await context.route('**/health',r=>r.fulfill({json:{accounts:0,ready:0,models:0}}));
 await page.goto(process.env.CODEARTS_TEST_BASE+'/#prompts');await page.locator('#promptText').waitFor();
 await page.getByRole('button',{name:'WorkBuddy',exact:true}).click();assert.equal(await page.locator('#promptMode').isDisabled(),true);assert.equal(await page.locator('#promptText').inputValue(),'');
 await page.getByRole('button',{name:'CodeArts',exact:true}).click();assert.equal(await page.locator('#promptMode').inputValue(),'passthrough');assert.equal(await page.locator('#promptText').inputValue(),'CODEARTS');
 await page.locator('.prompt-scope').first().click();await page.locator('#promptMode').selectOption('append');await page.locator('#promptText').fill('EXTRA');
 await page.locator('#nav-settings').click();assert.equal(await page.locator('#tab-settings textarea[id*="Prompt"]').count(),0);await page.locator('#sTimeout').fill('111');await page.locator('#saveSettings').click();await page.locator('#settingsDirty').waitFor({state:'hidden'});assert.equal(savedSettings.common_prompt,undefined);assert.equal(savedSettings.codearts.prompt_mode,undefined);
 await page.locator('#nav-prompts').click();assert.equal(await page.locator('#promptText').inputValue(),'EXTRA');
 for(const protocol of ['chat_completions','responses','anthropic']){await page.locator('#promptProtocol').selectOption(protocol);await page.locator('#previewPrompt').click();await page.waitForFunction(()=>document.querySelector('#promptOutput').value.includes('EXTRA'));assert.ok((await page.locator('#promptOutput').inputValue()).includes('Keep the client rules.'));}
 assert.equal(previews,3);assert.equal(saves,0);
 await page.locator('#savePrompts').click();await page.getByRole('alert').filter({hasText:'fixture conflict'}).waitFor();assert.equal(await page.locator('#promptText').inputValue(),'EXTRA');fail=false;await page.locator('#savePrompts').click();await page.waitForFunction(()=>document.querySelector('#savePrompts').disabled);assert.equal(saves,2);
 assert.equal(config.platforms.codearts.overrides,null);assert.deepEqual(config.platforms.workbuddy.overrides,{prompt_text:'',prompt_fallback:false,sanitize:false});
 await page.reload();await page.waitForFunction(()=>document.querySelector('#promptText')?.value==='EXTRA');
 fs.mkdirSync('artifacts/prompts',{recursive:true});
 for(const theme of ['light','dark']){await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);await page.screenshot({path:`artifacts/prompts/${locale}-${theme}.png`,fullPage:true,animations:'disabled'});}
 await page.setViewportSize({width:360,height:800});await page.waitForFunction(()=>document.querySelector('#consoleSidebar').getBoundingClientRect().right<=0);assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await page.screenshot({path:`artifacts/prompts/${locale}-mobile.png`,fullPage:true,animations:'disabled'});
 assert.deepEqual(errors,[]);await context.close();
 }console.log('PASS: prompts sidebar, independent save, draft retention, inheritance, failed-save retry, three protocol previews, zh/en and mobile.');
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exitCode=1});
