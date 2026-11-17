// Isolated service only. Prompt and search editors save independently.
const {chromium}=require('playwright'),fs=require('node:fs'),assert=require('node:assert/strict');
(async()=>{const base=process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10088',access=JSON.parse(fs.readFileSync(process.argv[2],'utf8'));const browser=await chromium.launch({headless:true,channel:'chrome'});try{
 const ctx=await browser.newContext({locale:'zh-CN',viewport:{width:1440,height:1000}});assert.ok((await ctx.request.post(base+'/auth/login',{data:{username:'admin',password:access.admin_password}})).ok());
 const original=await (await ctx.request.get(base+'/admin/api/prompts')).json();
 try{
 const page=await ctx.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));await page.goto(base+'/#prompts');await page.locator('#promptMode').waitFor();await page.getByRole('button',{name:'WorkBuddy',exact:true}).click();
 for(const name of ['继承公共模式','继承公共文本'])await page.getByLabel(name,{exact:true}).uncheck();
 await page.locator('#promptMode').selectOption('append');await page.locator('#promptText').fill('Fixture custom prompt\n第二行');await page.locator('#savePrompts').click();await page.waitForFunction(()=>document.querySelector('#savePrompts').disabled);
 await page.reload();await page.locator('#promptMode').waitFor();await page.getByRole('button',{name:'WorkBuddy',exact:true}).click();assert.equal(await page.locator('#promptMode').inputValue(),'append');assert.equal(await page.locator('#promptText').inputValue(),'Fixture custom prompt\n第二行');
 await page.locator('#nav-settings').click();await page.locator('#sWBSearch').waitFor();assert.equal(await page.locator('#tab-settings textarea[id*="Prompt"]').count(),0);
 const invalid=await ctx.request.post(base+'/admin/api/settings',{data:{workbuddy:{prompt_mode:'invalid'}}});assert.equal(invalid.status(),400);
 assert.deepEqual(errors,[]);console.log('PASS: independent WorkBuddy prompt editor, append persistence, settings separation and validation.');
 }finally{const current=await (await ctx.request.get(base+'/admin/api/prompts')).json();const restored=await ctx.request.post(base+'/admin/api/prompts',{data:{expected:current,configuration:original}});assert.ok(restored.ok());}
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exitCode=1});
