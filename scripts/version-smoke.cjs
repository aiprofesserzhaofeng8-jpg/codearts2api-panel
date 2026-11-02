// Isolated browser acceptance. Credentials arrive over stdin, never command arguments.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
(async()=>{
 let raw='';for await(const chunk of process.stdin)raw+=chunk;
 const {base,password,output}=JSON.parse(raw);
 const browser=await chromium.launch({headless:true,channel:'chrome'});
 try {
  const context=await browser.newContext({viewport:{width:1360,height:900}});
  const response=await context.request.post(base+'/auth/login',{headers:{Origin:base},data:{username:'admin',password}});
  assert.equal(response.status(),200);
  const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#settings');await page.locator('#runningVersion').filter({hasText:'v0.3.1'}).waitFor();
  await page.locator('#checkVersion').click();
  await page.waitForFunction(()=>!document.querySelector('#checkVersion').disabled);
  const text=await page.locator('#versionMessage').innerText();
  assert.ok(/正式版|检查更新/.test(text));
  assert.equal(await page.locator('#settingsDirty').isVisible(),false);
  assert.deepEqual(errors,[]);
  await page.screenshot({path:output,fullPage:false});
  console.log(JSON.stringify({current:await page.locator('#runningVersion').innerText(),update:text,settingsUnchanged:true,browserErrors:errors}));
 }finally{await browser.close()}
})().catch(error=>{console.error(error);process.exitCode=1});
