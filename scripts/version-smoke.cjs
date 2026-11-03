// Isolated browser acceptance. Credentials arrive over stdin, never command arguments.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
(async()=>{
 let raw='';for await(const chunk of process.stdin)raw+=chunk;
 const {base,password,output,version,latest,updateState}=JSON.parse(raw);
 assert.match(version,/^v[0-9]+\.[0-9]+\.[0-9]+$/);
 const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});
 try {
  const context=await browser.newContext({locale:'zh-CN',viewport:{width:1360,height:900}});
  const response=await context.request.post(base+'/auth/login',{headers:{Origin:base},data:{username:'admin',password}});
  assert.equal(response.status(),200);
  const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#settings');await page.locator('#runningVersion').filter({hasText:version}).waitFor();
  await page.locator('#checkVersion').click();
  await page.waitForFunction(()=>!document.querySelector('#checkVersion').disabled);
  const text=await page.locator('#versionMessage').innerText();
  assert.ok(/正式版|检查更新/.test(text));
  if(updateState){
   const checked=await context.request.get(base+'/admin/api/version?check=1');
   assert.equal(checked.status(),200);
   const status=await checked.json();
   assert.equal(status.current,version);
   assert.equal(status.latest,latest);
   assert.equal(status.state,updateState);
  }
  assert.equal(await page.locator('#settingsDirty').isVisible(),false);
  assert.deepEqual(errors,[]);
  await page.screenshot({path:output,fullPage:false});
  console.log(JSON.stringify({current:await page.locator('#runningVersion').innerText(),update:text,settingsUnchanged:true,browserErrors:errors}));
 }finally{await browser.close()}
})().catch(error=>{console.error(error);process.exitCode=1});
