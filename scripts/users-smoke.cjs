// M7 browser acceptance uses the actual isolated HTTP/storage implementation.
// No intercepted API responses, real accounts or upstream requests.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const fs=require('node:fs');
(async()=>{
 const base=process.env.CODEARTS_TEST_BASE, password=process.env.CODEARTS_TEST_ADMIN_PASSWORD;
 assert(base&&password,'Run via scripts/frontend_e2e.py');
 const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});
 const errors=[];
 try{
  const admin=await browser.newContext({viewport:{width:1440,height:1000}}),a=await browser.newContext({viewport:{width:1440,height:1000}}),b=await browser.newContext();
  assert.equal((await admin.request.post(base+'/admin/auth/login',{data:{password}})).status(),200);
  let response=await admin.request.post(base+'/admin/api/groups',{data:{name:'M7 consumer group',account_ids:[],model_ids:[]}});
  assert.equal(response.status(),200);const group=(await response.json()).group;
  const ap=await admin.newPage();ap.on('pageerror',e=>errors.push(e.message));await ap.goto(base+'/#users');
  await ap.locator('#addUser').click();await ap.locator('#uUsername').fill('browser-alice');await ap.locator('#uPassword').fill('browser-alice-strong-password');await ap.locator('#uGroup').selectOption(group.id);await ap.locator('#uRequests').fill('5');await ap.locator('#userSave').click();await ap.locator('#userDialog').waitFor({state:'hidden'});await ap.locator('#users').getByText('browser-alice',{exact:true}).waitFor();
  response=await admin.request.get(base+'/admin/api/users');const alice=(await response.json()).users.find(u=>u.username==='browser-alice');assert.equal(alice.budget.max_requests,5);assert.equal('password_hash' in alice,false);
  response=await admin.request.post(base+'/admin/api/users',{data:{username:'browser-bob',password:'browser-bob-strong-password',group_id:group.id}});assert.equal(response.status(),200);
  const page=await a.newPage();page.on('pageerror',e=>errors.push(e.message));await page.goto(base+'/user');await page.locator('#userUsername').fill('browser-alice');await page.locator('#userPassword').fill('browser-alice-strong-password');await page.locator('#userLoginForm').getByRole('button',{name:'登录',exact:true}).click();await page.locator('#userKeys').waitFor();
  assert.equal(await page.locator('#nav-accounts').count(),0);assert.equal(await page.locator('#pw').count(),0);
  await page.getByRole('textbox',{name:'密钥备注',exact:true}).fill('browser client');await page.locator('#userCreateKey').click();await page.locator('#userKeys').getByRole('button',{name:'显示',exact:true}).click();await page.locator('#userKeys input[type="text"][readonly]').waitFor();const input=page.locator('#userKeys input[readonly]');const value=await input.inputValue();assert(value.startsWith('sk-ca-'));assert(value.length>40);
  response=await a.request.get(base+'/user/api/keys');const key=(await response.json()).keys[0];assert.notEqual(key.key,value);
  response=await b.request.post(base+'/user/auth/login',{data:{username:'browser-bob',password:'browser-bob-strong-password'}});assert.equal(response.status(),200);
  assert.deepEqual((await (await b.request.get(base+'/user/api/keys')).json()).keys,[]);
  assert.equal((await b.request.post(base+'/user/api/keys/reveal',{data:{ref:key.ref}})).status(),404);
  assert.equal((await b.request.delete(base+'/user/api/keys?ref='+key.ref+'&action=delete')).status(),404);
  assert.equal((await a.request.get(base+'/admin/api/state')).status(),401);
  assert.equal((await a.request.post(base+'/user/api/keys',{data:{name:'escape',user_id:'other'}})).status(),400);
  await page.reload();await page.locator('#userKeys').waitFor();assert.equal(await page.locator('#userKeys input[readonly]').getAttribute('type'),'password');
  await page.getByRole('button',{name:'我的用量',exact:true}).click();await page.locator('#userUsage').waitFor();assert((await page.locator('#userUsage').innerText()).includes('0 次请求'));
  await page.getByRole('button',{name:'可用模型',exact:true}).click();await page.locator('#userModels').getByText('暂无显示的模型，请联系管理员检查分组').waitFor();
  const out=process.env.CODEARTS_USERS_ARTIFACTS||'artifacts/structure-m7-complete/browser';fs.mkdirSync(out,{recursive:true});
  for(const theme of ['light','dark']){await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);await page.screenshot({path:out+'/user-'+theme+'.png',fullPage:true});}
  await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await page.screenshot({path:out+'/user-mobile.png',fullPage:true});
  await ap.locator('#users').getByRole('button',{name:'编辑',exact:true}).first().click();await ap.screenshot({path:out+'/users-admin-desktop.png'});await ap.setViewportSize({width:390,height:844});assert.equal(await ap.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await ap.screenshot({path:out+'/users-admin-mobile.png'});await ap.locator('#userDialog').getByRole('button',{name:'取消',exact:true}).click();
  await page.getByRole('button',{name:'修改密码',exact:true}).first().click();await page.locator('#userOldPassword').fill('browser-alice-strong-password');await page.locator('#userNewPassword').fill('browser-alice-new-password');await page.locator('form').getByRole('button',{name:'修改并重新登录'}).click();await page.locator('#userLoginForm').waitFor();
  assert.equal((await a.request.get(base+'/user/api/keys')).status(),401);
  response=await a.request.post(base+'/user/auth/login',{data:{username:'browser-alice',password:'browser-alice-new-password'}});assert.equal(response.status(),200);
  response=await admin.request.post(base+'/admin/api/users',{data:{id:alice.id,username:alice.username,group_id:group.id,enabled:false}});assert.equal(response.status(),200);
  assert.equal((await a.request.get(base+'/user/api/keys')).status(),401);
  assert.equal((await a.request.get(base+'/v1/models',{headers:{Authorization:'Bearer '+value}})).status(),401);
  assert.deepEqual(errors,[]);console.log('PASS M7 actual HTTP/storage: admin create, user login, own keys, cross-user denial, admin denial, CSRF fields, reload, scoped usage/models, password revocation, disable, desktop/mobile and themes.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
