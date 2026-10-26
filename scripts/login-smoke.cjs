// Live official sign-in entry check. No account authorization or credentials entered.
const {chromium}=require('playwright');const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chrome'});
 let sid,token;const context=await browser.newContext();
 try{
  const page=await context.newPage();await page.goto('http://127.0.0.1:10087/#accounts');
  token=await page.locator('meta[name="local-panel-session"]').getAttribute('content');
  let starts=0;page.on('request',r=>{if(r.url().endsWith('/admin/api/login/start'))starts++;});
  await page.locator('#browserLogin').click();await page.locator('#loginPanel').waitFor({state:'visible'});
  assert.equal(context.pages().length,1);assert.equal(starts,0);
  const response=page.waitForResponse(r=>r.url().endsWith('/admin/api/login/start'));
  await page.locator('#loginGenerate').click();sid=(await (await response).json()).session_id;
  await page.locator('#loginDetails').waitFor({state:'visible'});assert.equal(context.pages().length,1);
  const popup=context.waitForEvent('page');await page.locator('#loginLink').click();const authPage=await popup;
  await authPage.waitForURL(u=>u.hostname==='auth.huaweicloud.com',{timeout:45000});
  await authPage.getByText('欢迎使用华为云',{exact:true}).waitFor({timeout:20000});await authPage.close();
  await page.locator('#loginCancel').click();await page.locator('#loginInitial').waitFor({state:'visible'});sid=null;
  console.log('PASS: local modal first, link generation without navigation, official login opens only on explicit link click, cancel. No real account authorization.');
 }finally{
  if(sid&&token)await context.request.post('http://127.0.0.1:10087/admin/api/login/cancel',{headers:{'X-Local-Panel-Session':token},data:{session_id:sid}});
  await browser.close();
 }
})().catch(e=>{console.error(e.message);process.exitCode=1;});
