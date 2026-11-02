// Actual fresh-install HTTP, storage and authentication. No upstream requests.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const fs=require('node:fs');
(async()=>{
 const base=process.env.CODEARTS_TEST_BASE,password=process.env.CODEARTS_TEST_ADMIN_PASSWORD,token=process.env.CODEARTS_TEST_SETUP_TOKEN,out=process.env.CODEARTS_SETUP_ARTIFACTS;
 assert(base&&password&&token&&out,'Run with scripts/setup_e2e.py');fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:1000}}),page=await context.newPage();
  const errors=[],initialRequests=[];
  page.on('pageerror',e=>errors.push(e.message));
  page.on('console',m=>{if(m.type()==='error'&&/Content Security|Refused to/.test(m.text()))errors.push(m.text());});
  page.on('request',r=>initialRequests.push(new URL(r.url()).pathname));
  await page.goto(base+'/');await page.locator('#setupForm').waitFor();
  assert.equal(new URL(page.url()).pathname,'/setup');
  assert.equal(await page.locator('#setupUsername').inputValue(),'admin');
  assert(await page.locator('#setupUsername').evaluate(n=>n.readOnly));
  for(const width of [1440,390,320]){
   await page.setViewportSize({width,height:width===1440?1000:844});
   for(const theme of ['light','dark']){
    await page.evaluate(value=>document.documentElement.dataset.theme=value,theme);
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,'setup overflow');
    await page.screenshot({path:out+'/setup-'+theme+'-'+width+'.png',fullPage:true,animations:'disabled'});
   }
  }
  assert.equal(initialRequests.some(path=>/^\/(auth|admin|user|v1)\//.test(path)),false,'installer fetched normal application data');
  assert.equal((await context.request.post(base+'/v1/chat/completions',{data:{}})).status(),503);
  assert.equal((await context.request.post(base+'/auth/login',{data:{username:'admin',password}})).status(),503);
  const data={setup_token:token,password,confirm_password:password};
  assert.equal((await context.request.post(base+'/setup/install',{data:{...data,setup_token:'wrong'}})).status(),403);
  assert.equal((await context.request.post(base+'/setup/install',{data,headers:{Origin:'https://evil.example'}})).status(),403);
  await page.setViewportSize({width:1440,height:1000});
  await page.locator('#setupToken').fill(token);await page.locator('#setupPassword').fill(password);await page.locator('#setupConfirm').fill(password+'-mismatch');
  await page.getByRole('button',{name:'下一步',exact:true}).click();await page.getByRole('alert').getByText('两次输入的密码不一致',{exact:true}).waitFor();
  assert.equal((await (await context.request.get(base+'/setup/status')).json()).needs_setup,true);
  await page.locator('#setupConfirm').fill(password);await page.getByRole('button',{name:'下一步',exact:true}).click();
  await page.locator('#setupReviewTitle').waitFor();assert.equal((await page.locator('.setup-review').innerText()).includes(password),false);
  await page.screenshot({path:out+'/confirm-desktop.png',fullPage:true,animations:'disabled'});
  await page.getByRole('button',{name:'完成安装',exact:true}).click();await page.getByRole('button',{name:'前往登录',exact:true}).waitFor();
  await page.waitForFunction(()=>!document.querySelector('.setup-complete button').disabled);
  await page.screenshot({path:out+'/complete-desktop.png',fullPage:true,animations:'disabled'});
  assert.equal((await context.request.post(base+'/setup/install',{data})).status(),403);
  assert.equal((await context.request.get(base+'/v1/models')).status(),401,'initial API key must prevent anonymous local access');
  assert.equal((await context.request.get(base+'/auth/session')).status(),200);
  assert.equal((await (await context.request.get(base+'/auth/session')).json()).authenticated,false,'setup must not create a login session');
  await page.getByRole('button',{name:'前往登录',exact:true}).click();await page.locator('#loginForm').waitFor();
  assert.equal(await page.getByRole('link',{name:/注册/}).count(),0);
  await page.locator('#loginUsername').fill('admin');await page.locator('#loginPassword').fill(password);await page.locator('#loginForm').getByRole('button',{name:'登录',exact:true}).click();await page.locator('#nav-accounts').waitFor();
  let response=await context.request.post(base+'/admin/api/groups',{data:{name:'安装验收分组',account_ids:[],model_ids:[]}});assert.equal(response.status(),200);const group=(await response.json()).group;
  await page.locator('#nav-users').click();await page.locator('#addUser').click();
  await page.locator('#uUsername').fill('setup-consumer');await page.locator('#uPassword').fill('setup-consumer-password');await page.locator('#uGroup').selectOption(group.id);await page.locator('#userSave').click();await page.locator('#userDialog').waitFor({state:'hidden'});
  await page.locator('#users').getByText('setup-consumer',{exact:true}).waitFor();
  const consumer=await browser.newContext(),up=await consumer.newPage();
  await up.goto(base+'/login');await up.locator('#loginForm').waitFor();
  await up.locator('#loginUsername').fill('setup-consumer');await up.locator('#loginPassword').fill('setup-consumer-password');await up.locator('#loginForm').getByRole('button',{name:'登录',exact:true}).click();await up.locator('#userDashboard').waitFor();
  assert.equal((await consumer.request.get(base+'/admin/api/state')).status(),403);
  assert.equal((await consumer.request.post(base+'/admin/api/users',{data:{username:'attacker',role:'admin'}})).status(),403);
  assert.equal((await consumer.request.post(base+'/setup/install',{data})).status(),403);
  await up.goto(base+'/setup');await up.locator('#userDashboard').waitFor();assert.equal(new URL(up.url()).pathname,'/user');
  assert.deepEqual(errors,[]);
  fs.writeFileSync(out+'/checks.json',JSON.stringify({passed:true,visual_states:8,breakpoints:[1440,390,320],themes:['light','dark'],owner_token_csrf_and_repeat_install_denied:true,password_confirmation:true,real_admin_creation_and_consumer_login:true,no_public_registration:true,setup_does_not_issue_session:true,anonymous_inference_denied:true,page_and_csp_errors:0,real_provider_requests:0},null,2)+'\n');
  console.log('PASS fresh wizard, owner proof, CSRF, password confirmation, install handoff, administrator login, admin-created consumer, role isolation, reinstallation denial, desktop/mobile/themes and strict CSP.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
