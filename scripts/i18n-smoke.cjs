// Real isolated authentication and storage. No upstream requests or production data.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const fs=require('node:fs');

(async()=>{
 const base=process.env.CODEARTS_TEST_BASE,password=process.env.CODEARTS_TEST_ADMIN_PASSWORD;
 assert(base&&password,'Run via scripts/frontend_e2e.py');
 const output=process.env.CODEARTS_I18N_ARTIFACTS||'artifacts/frontend-e2e/i18n';fs.mkdirSync(output,{recursive:true});
 const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});
 const errors=[];
 const context=async options=>{
  const c=await browser.newContext({viewport:{width:1440,height:1000},...options});
  c.on('page',p=>{
   p.on('pageerror',e=>errors.push(e.message));
   p.on('console',m=>{if(m.type()==='error'&&/Content Security|Refused to/.test(m.text()))errors.push(m.text());});
  });return c;
 };
 const select=async(page,name)=>{
  await page.locator('.locale-trigger').click();
  await Promise.all([page.waitForNavigation({waitUntil:'domcontentloaded'}),page.getByRole('menuitemradio',{name,exact:true}).click()]);
 };
 const language=async(page,value)=>{await page.waitForFunction(value=>document.documentElement.lang===value,value);};
 const mobileMenu=async(page,role)=>{
  for(const width of [390,320]){
   await page.setViewportSize({width,height:844});
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,'English header must fit on mobile');
   const boxes=await page.locator('.topbar-actions').evaluate(node=>[...node.children].map(child=>child.getBoundingClientRect()).filter(box=>box.width&&box.height).map(box=>({left:box.left,right:box.right})));
   for(let i=1;i<boxes.length;i++)assert(boxes[i].left>=boxes[i-1].right-1,'Header actions must not overlap');
   await page.locator('.locale-trigger').click();await page.getByRole('menu').waitFor();
   const box=await page.getByRole('menu').boundingBox();assert(box&&box.x>=0&&box.x+box.width<=width);
   await page.screenshot({path:output+'/'+role+'-menu-en-'+width+'.png',fullPage:true});
   await page.keyboard.press('Escape');
  }
 };
 const login=async(page,username,secret)=>{
  await page.locator('#loginUsername').fill(username);await page.locator('#loginPassword').fill(secret);
  await page.locator('#loginForm button').click();
 };
 try{
  const english=await context({locale:'en-US'}),p=await english.newPage();
  await p.goto(base+'/login');await p.locator('#loginForm').waitFor();await language(p,'en-US');
  assert.equal(await p.locator('.auth-heading h1').innerText(),'Welcome back');
  assert.equal(await p.evaluate(()=>localStorage.getItem('codearts2api_locale')),null);
  await select(p,/中文/);await language(p,'zh-CN');await p.locator('#loginForm').waitFor();
  assert.equal(await p.evaluate(()=>localStorage.getItem('codearts2api_locale')),'zh');
  for(let i=0;i<2;i++){await p.reload();await p.locator('#loginForm').waitFor();await language(p,'zh-CN');}
  await p.evaluate(()=>{window.dispatchEvent(new Event('languagechange'));location.hash='test';});
  assert.equal(await p.locator('.auth-heading h1').innerText(),'欢迎回来');
  await select(p,/English/);await language(p,'en-US');await p.locator('#loginForm').waitFor();
  // The persisted English preference also wins in a new Chinese browser environment.
  const chinese=await context({locale:'zh-CN',storageState:await english.storageState()}),zh=await chinese.newPage();
  await zh.goto(base+'/login');await zh.locator('#loginForm').waitFor();await language(zh,'en-US');
  await select(zh,/System language/);await language(zh,'zh-CN');await zh.locator('#loginForm').waitFor();
  assert.equal(await zh.evaluate(()=>localStorage.getItem('codearts2api_locale')),null);
  assert.equal(await zh.evaluate(()=>sessionStorage.getItem('codearts2api_locale')),null);
  await zh.screenshot({path:output+'/login-zh.png',fullPage:true});await chinese.close();
  const unsupported=await context({locale:'fr-FR'}),fr=await unsupported.newPage();
  await fr.goto(base+'/login');await fr.locator('#loginForm').waitFor();await language(fr,'zh-CN');await unsupported.close();

  await login(p,'admin',password);await p.locator('#nav-accounts').waitFor();
  const headings={accounts:'Account pool',groups:'Account groups',users:'Users',overview:'Usage',models:'Models',keys:'API keys',logs:'Request logs',audit:'Audit logs',settings:'Settings'};
  for(const [tab,title]of Object.entries(headings)){
   await p.locator('#nav-'+tab).click();await p.locator('#tab-'+tab+'.on').waitFor();
   assert.equal(await p.locator('#pageTitle').innerText(),title);assert.equal(await p.locator('#nav-'+tab).getAttribute('aria-selected'),'true');
  }
  await p.locator('#sForceModel').fill('draft-model-do-not-translate');
  await p.locator('#nav-accounts').click();await p.locator('#nav-settings').click();
  assert.equal(await p.locator('#sForceModel').inputValue(),'draft-model-do-not-translate');
  assert.equal((await p.locator('.locale-trigger').innerText()).replace(/\s/g,''),'🇺🇸EN');
  await p.locator('#nav-groups').click();await p.locator('#addGroup').click();
  await p.locator('#groupDialog').waitFor();assert.equal(await p.locator('#groupTitle').innerText(),'Create account group');
  await p.locator('#groupName').fill('中文分组 · i18n-test');await p.locator('#groupSave').click();
  await p.locator('#groupDialog').waitFor({state:'hidden'});await p.getByRole('cell',{name:'中文分组 · i18n-test',exact:true}).waitFor();
  await p.screenshot({path:output+'/admin-en.png',fullPage:true});
  await mobileMenu(p,'admin');await p.setViewportSize({width:1440,height:1000});
  const state=await (await english.request.get(base+'/admin/api/state')).json();
  const group=state.groups.find(g=>g.name==='中文分组 · i18n-test');assert(group);
  const created=await english.request.post(base+'/admin/api/users',{data:{username:'i18n-user',password:'i18n-test-strong-password',group_id:group.id}});
  assert.equal(created.status(),200,await created.text());
  await p.locator('#forgetPw').click();await p.locator('#loginForm').waitFor();await language(p,'en-US');
  await login(p,'i18n-user','i18n-test-strong-password');await p.locator('#userDashboard').waitFor();
  const adminReads=[];p.on('request',r=>{if(new URL(r.url()).pathname.startsWith('/admin/'))adminReads.push(r.url());});
  const userPages={dashboard:'Dashboard',keys:'My keys',models:'Available models',usage:'My usage',entitlements:'Credits and redemption',identity:'Sign-in methods',profile:'Profile',password:'Change password'};
  for(const [id,title]of Object.entries(userPages)){
   await p.locator('nav[aria-label="User navigation"] button').filter({hasText:title}).click();
   await p.waitForFunction(title=>document.querySelector('.topbar h1')?.textContent===title,title);
   if(id!=='dashboard')assert.equal(new URL(p.url()).hash,'#'+id);
  }
  await p.reload();await p.locator('#userNewPassword').waitFor();await language(p,'en-US');
  await p.locator('nav[aria-label="User navigation"] button').filter({hasText:'Profile'}).click();await p.locator('#userProfile').waitFor();
  await p.screenshot({path:output+'/user-en.png',fullPage:true});
  await mobileMenu(p,'user');
  await p.screenshot({path:output+'/user-en-mobile.png',fullPage:true});
  await select(p,/中文/);await language(p,'zh-CN');await p.locator('#userProfile').waitFor();
  assert.equal(await p.locator('.topbar h1').innerText(),'个人资料');
  await p.reload();await p.locator('#userProfile').waitFor();await language(p,'zh-CN');
  assert.deepEqual(adminReads,[]);await english.close();

  const restricted=await context({locale:'en-US'});
  await restricted.addInitScript(()=>{const original=Storage.prototype.setItem;Storage.prototype.setItem=function(key,value){if(this===localStorage)throw new DOMException('Storage blocked');return original.call(this,key,value);};});
  const blocked=await restricted.newPage();await blocked.goto(base+'/login');await blocked.locator('#loginForm').waitFor();
  await select(blocked,/中文/);await language(blocked,'zh-CN');await blocked.locator('#loginForm').waitFor();
  await blocked.reload();await blocked.locator('#loginForm').waitFor();await language(blocked,'zh-CN');
  await restricted.close();
  const denied=await context({locale:'zh-CN'});
  await denied.addInitScript(()=>{Storage.prototype.setItem=function(){throw new DOMException('Storage blocked');};});
  const d=await denied.newPage();await d.goto(base+'/login');await d.locator('#loginForm').waitFor();
  await d.locator('.locale-trigger').click();await d.getByRole('menuitemradio',{name:/English/}).click();
  await d.getByRole('alert').filter({hasText:'浏览器禁止保存语言偏好'}).waitFor();await language(d,'zh-CN');await denied.close();
  assert.deepEqual(errors,[]);
  console.log('PASS: browser detection, Chinese fallback, explicit persistence, automatic mode, admin/user navigation, unchanged user data, mobile menus, storage failures and CSP. No real provider requests.');
 }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exit(1);});
