// Real isolated HTTP/auth/storage; no intercepted API responses or upstream calls.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const fs=require('node:fs');
(async()=>{
 const base=process.env.CODEARTS_TEST_BASE, password=process.env.CODEARTS_TEST_ADMIN_PASSWORD;
 assert(base&&password,'Run via scripts/frontend_e2e.py');
 const out=process.env.CODEARTS_LAYOUT_ARTIFACTS||'artifacts/unified-console/browser';fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});
 const errors=[];
 try{
  const admin=await browser.newContext({viewport:{width:1440,height:1000}});
  const consumer=await browser.newContext({viewport:{width:1440,height:1000}});
  const ap=await admin.newPage(),page=await consumer.newPage();
  for(const p of [ap,page]){
   p.on('pageerror',e=>errors.push(e.message));
   p.on('console',m=>{if(m.type()==='error'&&/Content Security|Refused to/.test(m.text()))errors.push(m.text());});
  }
  await ap.goto(base+'/');await ap.locator('#loginForm').waitFor();
  assert.equal(await ap.locator('.auth-card .brand-name').innerText(),'CodeArts2API');
  await ap.waitForLoadState('networkidle');assert.equal(await ap.getByText('Console disposed',{exact:true}).count(),0);
  await ap.locator('#loginUsername').fill('admin');await ap.locator('#loginPassword').fill(password);await ap.locator('#loginForm button').click();await ap.locator('#nav-accounts').waitFor();
  await ap.locator('#nav-models').click();await ap.locator('#tab-models.on').waitFor();
  // Vue shell updates must not overwrite native navigation, headings or feature drafts.
  const title=await ap.locator('#pageTitle').innerText();
  await ap.getByRole('button',{name:'收起侧栏',exact:true}).click();
  await ap.locator('#themeToggle').click();
  assert.equal(await ap.locator('#pageTitle').innerText(),title);
  assert.equal(await ap.locator('#nav-models').getAttribute('aria-selected'),'true');
  await ap.getByRole('button',{name:'展开侧栏',exact:true}).click();
  assert.equal(await ap.locator('[data-console-role="admin"] .sidebar').count(),1);
  await ap.screenshot({path:out+'/admin-desktop.png',fullPage:true,animations:'disabled'});
  let response=await admin.request.post(base+'/admin/api/groups',{data:{name:'Unified console group',account_ids:[],model_ids:[]}});
  assert.equal(response.status(),200);const group=(await response.json()).group;
  response=await admin.request.post(base+'/admin/api/users',{data:{username:'unified-console-user',password:'unified-console-strong-password',group_id:group.id}});
  assert.equal(response.status(),200);
  const adminRequests=[];page.on('request',r=>{if(new URL(r.url()).pathname.startsWith('/admin/'))adminRequests.push(r.url());});
  await page.goto(base+'/user#keys');await page.locator('#loginForm').waitFor();
  assert.equal(await page.locator('.auth-card .brand-name').innerText(),'CodeArts2API');
  await page.locator('#loginUsername').fill('unified-console-user');await page.locator('#loginPassword').fill('unified-console-strong-password');
  await page.locator('#loginForm').getByRole('button',{name:'登录',exact:true}).click();await page.locator('#userKeys').waitFor();
  assert.equal(await page.locator('[data-console-role="user"] .sidebar').count(),1);
  assert.equal(await page.locator('#nav-accounts,#pw,#health').count(),0);
  assert.equal((await consumer.request.get(base+'/admin/api/state')).status(),403);
  assert.equal((await admin.request.get(base+'/user/api/profile')).status(),403);
  await page.locator('#themeToggle').click();const theme=await page.locator('html').getAttribute('data-theme');
  await page.reload();await page.locator('#userKeys').waitFor();assert.equal(await page.locator('html').getAttribute('data-theme'),theme);
  await page.getByRole('button',{name:'我的用量',exact:true}).click();await page.locator('#userUsage').waitFor();
  await page.goBack();await page.locator('#userKeys').waitFor();assert.equal(new URL(page.url()).hash,'#keys');
  await page.getByRole('button',{name:'收起侧栏',exact:true}).click();
  await page.reload();await page.locator('.sidebar-collapsed').waitFor();await page.locator('#userKeys').waitFor();
  await page.getByRole('button',{name:'展开侧栏',exact:true}).click();
  for(const theme of ['light','dark']){
   if(await page.locator('html').getAttribute('data-theme')!==theme)await page.locator('#themeToggle').click();
   await page.screenshot({path:out+'/user-'+theme+'.png',fullPage:true,animations:'disabled'});
  }
  await page.setViewportSize({width:390,height:844});await page.locator('#consoleMenuToggle').click();
  await page.locator('.sidebar-open').waitFor();
  assert.equal(await page.locator('.main').evaluate(node=>node.inert),true);
  await page.screenshot({path:out+'/user-mobile-navigation.png',fullPage:true,animations:'disabled'});
  await page.keyboard.press('Escape');await page.locator('#consoleMenuToggle').waitFor({state:'visible'});
  await page.waitForFunction(()=>document.activeElement?.id==='consoleMenuToggle');
  for(const name of ['仪表盘','我的密钥','可用模型','我的用量','额度与兑换','登录方式','个人资料','修改密码']){
   await page.locator('#consoleMenuToggle').click();await page.getByRole('navigation',{name:'用户功能',exact:true}).getByRole('button',{name,exact:true}).click();
   await page.locator('.console-layout:not(.sidebar-open)').waitFor();
   assert.equal(await page.locator('.topbar h1').innerText(),name);
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  }
  await page.screenshot({path:out+'/user-mobile.png',fullPage:true,animations:'disabled'});
  await page.setViewportSize({width:320,height:740});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  assert.deepEqual(adminRequests,[],'user workspace must never fetch admin state');
  await page.setViewportSize({width:1440,height:1000});
  assert.equal(await page.getByRole('navigation',{name:'切换空间'}).count(),0);
  await page.goto(base+'/#accounts');await page.locator('[data-console-role="user"]').waitFor();
  assert.equal(new URL(page.url()).pathname,'/user');assert.equal(await page.locator('#nav-accounts').count(),0);
  await page.locator('#userLogout').click();await page.locator('#loginForm').waitFor();
  assert.equal(await page.getByRole('link',{name:/管理员登录|普通用户登录/}).count(),0);
  await page.locator('#loginUsername').fill('admin');await page.locator('#loginPassword').fill(password);await page.locator('#loginForm button').click();await page.locator('#nav-accounts').waitFor();
  const cookies=(await consumer.cookies()).filter(c=>c.name==='codearts_session');assert.equal(cookies.length,1);assert.equal(cookies[0].path,'/');assert(cookies[0].httpOnly);
  for(const path of ['/admin/auth/login','/user/auth/login'])assert.equal((await consumer.request.post(base+path,{data:{password}})).status(),404);
  await ap.setViewportSize({width:390,height:844});
  await ap.locator('#consoleMenuToggle').click();await ap.locator('#nav-users').click();await ap.locator('#tab-users.on').waitFor();
  assert.equal(await ap.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  await ap.screenshot({path:out+'/admin-mobile.png',fullPage:true,animations:'disabled'});
  assert.deepEqual(errors,[]);
  console.log('PASS shared shell/auth, native DOM preservation, role-specific menus, unified session/server-assigned roles, retired endpoints blocked, zero admin fetches from user workspace, theme/collapse persistence, deep links/reload/back, mobile navigation/Escape/focus, 320px overflow and strict CSP.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
