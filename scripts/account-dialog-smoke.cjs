// UI fixtures only. Every admin request is intercepted, so real accounts are untouched.
const {chromium}=require('playwright');const fs=require('node:fs');const assert=require('node:assert/strict');
async function chooseAccount(page, id, method='browser') {
  if (await page.locator('#addAccountDialog').evaluate(el=>el.open) && !await page.locator('#accountChoose').isVisible()) await page.locator('#addAccountDialog').waitFor({state:'hidden'});
  if (!await page.locator('#addAccountDialog').evaluate(el=>el.open)) await page.locator('#addAccount').click();
  await page.locator('#'+id).click();
  await page.locator(method==='import'?'#accountMethodImport':'#accountMethodBrowser').click();
  await page.locator('#accountCreateNext').click();
}
(async()=>{
 const browser=await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL||'chrome',headless:true});
 try{
  const context=await browser.newContext({viewport:{width:1280,height:1000},permissions:['clipboard-read','clipboard-write']});
  let accounts=[{id:'fixture-existing',name:'待删除的测试账号',enabled:true,status:'ok',source:'browser',pool:{state:'ready',in_flight:0,max_in_flight:2},has_refresh:false,model_health:{'fixture-model':{failures:1,cooldown_until:Math.floor(Date.now()/1000)+60,updated_at:Math.floor(Date.now()/1000),last_error:'测试模型限流'}}}];
  let verified=false,deleteFailure=true;const counts={start:0,poll:0,callback:0,complete:0,remove:0};
  await context.route('**/admin/auth/session',r=>r.fulfill({json:{authenticated:true,local_compatibility:false}}));
await context.route('**/admin/api/**',async route=>{
   const path=new URL(route.request().url()).pathname;let data={ok:true};
   if(path.endsWith('/state'))data={accounts,keys:[],models:[],settings:{pool:{strategy:'least_inflight'}},stats:{}};
   if(path.endsWith('/credits'))data={accounts:[]};
   if(path.endsWith('/login/start')){counts.start++;verified=false;data={session_id:'fixture-login',auth_url:'https://codearts.huaweicloud.com/portal/authorize?fixture=1',callback:'http://127.0.0.1:12345/oauth/callback',expires_in:300};}
   if(path.endsWith('/login/callback')){counts.callback++;verified=true;}
   if(path.endsWith('/login/poll')){counts.poll++;data=verified?{status:'confirmed',ready:true,saved:false,preview_id:'fixture-preview',account_name:'待添加的测试账号',domain_id:'fixture-domain',has_refresh:false,expires_at:Math.floor(Date.now()/1000)+3600,existing:false}:{status:'pending',message:'请在另一浏览器完成登录'};}
   if(path.endsWith('/login/complete')){counts.complete++;assert.equal(route.request().postDataJSON().preview_id,'fixture-preview');accounts.push({id:'fixture-added',name:'待添加的测试账号',enabled:true,status:'ok',source:'browser',pool:{state:'ready',in_flight:0,max_in_flight:2}});data={ok:true,saved:true,account_id:'fixture-added'};}
   if(path.endsWith('/accounts')){
    const body=route.request().postDataJSON();assert.equal(body.action,'remove');counts.remove++;
    if(deleteFailure)return route.fulfill({status:500,json:{error:{message:'测试：状态文件无法写入'}}});
    accounts=accounts.filter(a=>a.id!==body.id);data={ok:true,removed_id:body.id};
   }
   return route.fulfill({json:data});
  });
  await context.route('**/health',r=>r.fulfill({json:{accounts:accounts.length,ready:accounts.length,models:0}}));
  const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
  page.on('dialog',()=>{errors.push('Unexpected native confirmation dialog');});
  await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10087')+'/#accounts');await page.locator('#accounts tr[data-state]').first().waitFor();
  await page.locator('#accounts tr').first().getByRole('button',{name:'设置',exact:true}).click();await page.locator('#accountActionMenu').getByRole('button',{name:'模型健康',exact:true}).click();await page.locator('#modelHealthRows tr').filter({hasText:'fixture-model'}).waitFor();assert.match(await page.locator('#modelHealthRows').textContent(),/冷却/);await page.locator('#modelHealthClose').click();
  await chooseAccount(page,'browserLogin');assert.equal(await page.locator('#addAccountDialog').evaluate(el=>el.open),true);
  assert.equal(context.pages().length,1);assert.equal(counts.start,0);
  await page.locator('#loginGenerate').click();await page.locator('#loginDetails').waitFor({state:'visible'});
  assert.equal(context.pages().length,1);assert.equal(counts.poll,0);assert.equal(counts.complete,0);
  await page.locator('#loginCopy').click();assert.match(await page.evaluate(()=>navigator.clipboard.readText()),/^https:\/\/codearts.huaweicloud.com/);
  fs.mkdirSync('artifacts',{recursive:true});await page.screenshot({path:'artifacts/codearts-login-dialog.png',animations:'disabled'});
  await page.reload();await chooseAccount(page,'browserLogin');await page.locator('#loginDetails').waitFor({state:'visible'});assert.equal(counts.start,1);
  await page.locator('#loginPasted').fill('http://127.0.0.1:12345/oauth/callback?code=fixture-code');await page.locator('#loginSubmit').click();
  await page.locator('#loginPreview').waitFor({state:'visible'});assert.equal(counts.complete,0);assert.equal(accounts.length,1);
  assert.equal(await page.locator('#loginPasted').inputValue(),'');
  // Browser callback may already be confirmed when its URL is pasted again.
  await page.locator('#loginPasted').fill('http://127.0.0.1:12345/oauth/callback?code=fixture-code');await page.locator('#loginSubmit').click();
  await page.locator('#loginPreview').waitFor({state:'visible'});assert.equal(counts.complete,0);assert.equal(await page.locator('#loginError').isVisible(),false);
  await page.screenshot({path:'artifacts/codearts-login-preview.png',animations:'disabled'});
  await page.locator('#loginComplete').click();await page.waitForFunction(()=>!document.getElementById('addAccountDialog').open);
  await page.waitForFunction(()=>document.querySelectorAll('#accounts tr[data-state]').length===2);assert.equal(counts.complete,1);
  let row=page.locator('#accounts tr').filter({hasText:'待删除的测试账号'});
  await row.getByRole('button',{name:'更多',exact:true}).click();await page.locator('#accountActionMenu').getByRole('button',{name:'删除',exact:true}).click();await page.locator('#deleteAccountDialog').waitFor({state:'visible'});
  assert.equal(counts.remove,0);await page.locator('#deleteAccountCancel').click();assert.equal(counts.remove,0);
  await row.getByRole('button',{name:'更多',exact:true}).click();await page.locator('#accountActionMenu').getByRole('button',{name:'删除',exact:true}).click();await page.locator('#deleteAccountConfirm').click();
  await page.locator('#deleteAccountError').waitFor({state:'visible'});assert.equal(accounts.length,2);
  deleteFailure=false;
  await page.screenshot({path:'artifacts/codearts-delete-dialog.png',animations:'disabled'});
  await page.locator('#deleteAccountConfirm').click();await page.waitForFunction(()=>!document.getElementById('deleteAccountDialog').open);
  await page.waitForFunction(()=>document.querySelectorAll('#accounts tr[data-state]').length===1);
  await page.reload();await page.locator('#accounts tr[data-state]').first().waitFor();assert.equal(await page.locator('#accounts tr').filter({hasText:'待删除的测试账号'}).count(),0);
  await page.setViewportSize({width:390,height:844});await chooseAccount(page,'browserLogin');
  await page.screenshot({path:'artifacts/codearts-login-dialog-mobile.png',animations:'disabled'});
  assert.equal(context.pages().length,1);assert.deepEqual(errors,[]);
  console.log('PASS: native page modal, no auto-navigation or auto-save, link copy/resume, callback preview then explicit save; delete cancel, error/retry, remove and refresh. Synthetic accounts only.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e.message);process.exitCode=1;});
