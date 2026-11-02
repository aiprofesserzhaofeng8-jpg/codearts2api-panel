// M7 browser acceptance uses the actual isolated HTTP/storage implementation.
// No intercepted API responses, real accounts or upstream requests.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const fs=require('node:fs');
(async()=>{
 const base=process.env.CODEARTS_TEST_BASE, password=process.env.CODEARTS_TEST_ADMIN_PASSWORD;
 assert(base&&password,'Run via scripts/frontend_e2e.py');
 const out=process.env.CODEARTS_USERS_ARTIFACTS||'artifacts/structure-m7-complete/browser';fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});
 const errors=[];
 try{
  const admin=await browser.newContext({viewport:{width:1440,height:1000}}),a=await browser.newContext({viewport:{width:1440,height:1000}}),b=await browser.newContext();
  assert.equal((await admin.request.post(base+'/auth/login',{data:{username:'admin',password}})).status(),200);
  let response=await admin.request.post(base+'/admin/api/groups',{data:{name:'M7 consumer group',account_ids:[],model_ids:[]}});
  assert.equal(response.status(),200);const group=(await response.json()).group;
  const ap=await admin.newPage();ap.on('pageerror',e=>errors.push(e.message));await ap.goto(base+'/#users');
  await ap.locator('#addUser').click();await ap.locator('#uUsername').fill('browser-alice');await ap.locator('#uPassword').fill('browser-alice-strong-password');await ap.locator('#uGroup').selectOption(group.id);await ap.locator('#uRequests').fill('5');await ap.locator('#uConcurrency').fill('2');await ap.locator('#userSave').click();await ap.locator('#userDialog').waitFor({state:'hidden'});await ap.locator('#users').getByText('browser-alice',{exact:true}).waitFor();
  response=await admin.request.get(base+'/admin/api/users');const alice=(await response.json()).users.find(u=>u.username==='browser-alice');assert.equal(alice.budget.max_requests,5);assert.equal(alice.max_concurrency,2);assert.equal('password_hash' in alice,false);
  response=await admin.request.post(base+'/admin/api/users',{data:{username:'browser-bob',password:'browser-bob-strong-password',group_id:group.id}});assert.equal(response.status(),200);
  const page=await a.newPage();page.on('pageerror',e=>errors.push(e.message));await page.goto(base+'/user');await page.locator('#loginUsername').fill('browser-alice');await page.locator('#loginPassword').fill('browser-alice-strong-password');await page.locator('#loginForm').getByRole('button',{name:'登录',exact:true}).click();await page.locator('#userDashboard').waitFor();await page.getByRole('button',{name:'我的密钥',exact:true}).click();await page.locator('#userKeys').waitFor();
  assert.equal(await page.locator('#nav-accounts').count(),0);assert.equal(await page.locator('#pw').count(),0);
  await page.getByRole('textbox',{name:'密钥备注',exact:true}).fill('browser client');await page.locator('#userCreateKey').click();await page.locator('#userKeys').getByRole('button',{name:'显示',exact:true}).click();await page.locator('#userKeys input[type="text"][readonly]').waitFor();const input=page.locator('#userKeys input[readonly]');const value=await input.inputValue();assert(value.startsWith('sk-ca-'));assert(value.length>40);
  response=await a.request.get(base+'/user/api/keys');const key=(await response.json()).keys[0];assert.notEqual(key.key,value);
  response=await b.request.post(base+'/auth/login',{data:{username:'browser-bob',password:'browser-bob-strong-password'}});assert.equal(response.status(),200);
  assert.deepEqual((await (await b.request.get(base+'/user/api/keys')).json()).keys,[]);
  assert.equal((await b.request.post(base+'/user/api/keys/reveal',{data:{ref:key.ref}})).status(),404);
  assert.equal((await b.request.delete(base+'/user/api/keys?ref='+key.ref+'&action=delete')).status(),404);
  assert.equal((await a.request.get(base+'/admin/api/state')).status(),403);
  assert.equal((await a.request.post(base+'/user/api/keys',{data:{name:'escape',user_id:'other'}})).status(),400);
  await page.reload();await page.locator('#userKeys').waitFor();assert.equal(await page.locator('#userKeys input[readonly]').getAttribute('type'),'password');
  await page.getByRole('button',{name:'我的用量',exact:true}).click();await page.locator('#userUsage').waitFor();assert((await page.locator('#userUsage').innerText()).includes('0 次请求'));
  await page.getByRole('button',{name:'可用模型',exact:true}).click();await page.locator('#userModels').getByText('暂无显示的模型，请联系管理员检查分组').waitFor();
  await page.getByRole('button',{name:'仪表盘',exact:true}).click();await page.locator('#userDashboard').getByText('当前范围暂无请求记录').waitFor();
  await page.locator('#userUsageRange').selectOption('30');await page.locator('#userRefresh').waitFor({state:'visible'});await page.waitForFunction(()=>!document.querySelector('#userRefresh').disabled);
  for(const theme of ['light','dark']){await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);await page.screenshot({path:out+'/user-dashboard-'+theme+'.png',fullPage:true,animations:'disabled'});}
  await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await page.screenshot({path:out+'/user-dashboard-mobile.png',fullPage:true,animations:'disabled'});await page.setViewportSize({width:1440,height:1000});
  await page.getByRole('button',{name:'个人资料',exact:true}).click();await page.locator('#userDisplayName').fill('浏览器测试用户');
  const png=await page.evaluate(()=>{const c=document.createElement('canvas');c.width=600;c.height=600;const ctx=c.getContext('2d');const d=ctx.createImageData(600,600);let n=7;for(let i=0;i<d.data.length;i+=4){n=(n*1664525+1013904223)>>>0;d.data[i]=n&255;d.data[i+1]=(n>>>8)&255;d.data[i+2]=(n>>>16)&255;d.data[i+3]=255;}ctx.putImageData(d,0,0);return c.toDataURL('image/png');});
  await page.locator('#userAvatarFile').setInputFiles({name:'large-avatar.png',mimeType:'image/png',buffer:Buffer.from(png.split(',')[1],'base64')});
  await page.locator('#userProfileSave').click();await page.getByText('个人资料已保存',{exact:true}).waitFor();
  response=await a.request.get(base+'/user/api/profile');let profile=(await response.json()).user;assert.equal(profile.display_name,'浏览器测试用户');assert.equal(profile.username,'browser-alice');assert.equal(profile.max_concurrency,2);assert(profile.avatar.startsWith('data:image/jpeg;base64,'));assert(Buffer.from(profile.avatar.split(',')[1],'base64').length<=20*1024);
  assert.equal(await page.locator('#userProfile img').evaluate(image=>image.complete&&image.naturalWidth>0),true);
  response=await b.request.get(base+'/user/api/profile');assert.equal((await response.json()).user.display_name,'');
  await page.reload();await page.locator('.user-identity h1').getByText('浏览器测试用户',{exact:true}).waitFor();
  await page.getByRole('button',{name:'个人资料',exact:true}).click();await page.locator('#userProfile img').waitFor();
  assert.equal(await page.locator('.user-tabs .primary').count(),1);
  for(const theme of ['light','dark']){await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);await page.screenshot({path:out+'/user-'+theme+'.png',fullPage:true,animations:'disabled'});}
  await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await page.screenshot({path:out+'/user-mobile.png',fullPage:true,animations:'disabled'});
  await ap.locator('#users').getByRole('button',{name:'编辑',exact:true}).first().click();await ap.screenshot({path:out+'/users-admin-desktop.png',animations:'disabled'});await ap.setViewportSize({width:390,height:844});assert.equal(await ap.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await ap.screenshot({path:out+'/users-admin-mobile.png',animations:'disabled'});await ap.locator('#userDialog').getByRole('button',{name:'取消',exact:true}).click();
  await page.locator('#consoleMenuToggle').click();await page.getByRole('button',{name:'修改密码',exact:true}).first().click();await page.locator('#userOldPassword').fill('browser-alice-strong-password');await page.locator('#userNewPassword').fill('browser-alice-new-password');await page.locator('form').getByRole('button',{name:'修改并重新登录'}).click();await page.locator('#loginForm').waitFor();
  assert.equal((await a.request.get(base+'/user/api/keys')).status(),401);
  response=await a.request.post(base+'/auth/login',{data:{username:'browser-alice',password:'browser-alice-new-password'}});assert.equal(response.status(),200);
  response=await admin.request.post(base+'/admin/api/users',{data:{id:alice.id,username:alice.username,group_id:group.id,enabled:false}});assert.equal(response.status(),200);
  assert.equal((await a.request.get(base+'/user/api/keys')).status(),401);
  assert.equal((await a.request.get(base+'/v1/models',{headers:{Authorization:'Bearer '+value}})).status(),401);
  assert.deepEqual(errors,[]);console.log('PASS consumer HTTP/storage and product UI: profile/avatar compression/persistence, dashboard/range, user concurrency configuration, admin create, user login, own keys, cross-user denial, admin denial, CSRF fields, reload, scoped usage/models, password revocation, disable, desktop/mobile and themes.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
