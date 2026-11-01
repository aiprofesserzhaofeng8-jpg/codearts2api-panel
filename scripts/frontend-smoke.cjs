const {chromium}=require('playwright');const assert=require('node:assert/strict');const {mock}=require('./frontend-fixture.cjs');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});
 try{
  const context=await browser.newContext({viewport:{width:1500,height:1000}}),page=await context.newPage(),errors=[],requests=[];let rejectState=true;
  page.on('pageerror',error=>errors.push(error.message));page.on('console',message=>{if(message.type()==='error'&&/Content Security|Refused to/.test(message.text()))errors.push(message.text());});
  const base=process.env.CODEARTS_TEST_BASE;
  await context.route(base+'/',async route=>{const response=await route.fetch();const body=(await response.text()).replace(/name="local-panel-session" content="[^"]*"/,'name="local-panel-session" content=""');await route.fulfill({response,body});});
  await mock(context,{authenticated:false,onRequest:async(route,url,request)=>{
   requests.push(url.pathname+url.search);
   if(url.pathname.endsWith('/state')&&rejectState){await route.fulfill({status:401,json:{error:{message:'fixture session expired'}}});return true;}
   if(url.pathname.endsWith('/keys/budget')){const body=request.postDataJSON();assert.equal(body.ref,'fixture-ref');assert.equal(body.budget.max_requests,5);await route.fulfill({json:{ok:true}});return true;}
  }});
  await page.goto(base+'/#accounts');await page.locator('#pw').fill('fixture-password');const rejected=page.waitForResponse(response=>response.url().endsWith('/admin/api/state')&&response.status()===401);await page.locator('#savePw').click();await rejected;await page.waitForFunction(()=>document.querySelector('#adminLoginDialog').open);
  rejectState=false;await page.locator('#pw').fill('fixture-password');await page.locator('#savePw').click();await page.waitForFunction(()=>!document.querySelector('#adminLoginDialog').open&&document.querySelector('#accounts').children.length>0);
  assert.equal(await page.evaluate(()=>localStorage.getItem('codearts2api_pw')),null);
  for(const name of ['groups','overview','models','keys','logs','audit','settings','accounts']){await page.locator('#nav-'+name).click();await page.locator('#tab-'+name+'.on').waitFor();assert.ok((await page.title()).includes('CodeArts2API'));}
  assert.equal(await page.evaluate(()=>window.injected),undefined);assert.equal(await page.locator('#accounts img').count(),0);
  await page.locator('#nav-settings').click();await page.locator('#sForceModel').fill('draft-kept');
  await page.locator('#nav-accounts').click();await page.locator('#refreshCredits').click();await page.waitForTimeout(80);await page.locator('#nav-settings').click();assert.equal(await page.locator('#sForceModel').inputValue(),'draft-kept');
  await page.locator('#nav-keys').click();await page.getByRole('button',{name:'预算',exact:true}).click();await page.locator('#budgetRequests').fill('5');await page.locator('#keyBudgetSave').click();await page.locator('#keyBudgetDialog').waitFor({state:'hidden'});
  await page.locator('#nav-overview').click();await page.locator('#usageDateToggle').click();assert.equal(await page.locator('#usageDatePopover').isVisible(),true);await page.locator('#usageDateToggle').click();assert.equal(await page.locator('#usageDatePopover').isVisible(),false);
  await page.locator('#usagePageSize').selectOption('50');await page.waitForFunction(()=>document.querySelector('#usageLogs').getAttribute('aria-busy')==='false');assert.ok(requests.some(url=>url.includes('/usage?')&&url.includes('page_size=50')));
  await page.locator('#nav-audit').click();await page.locator('#auditNext').click();await page.waitForFunction(()=>document.querySelector('#auditPage').textContent.includes('2'));assert.ok(requests.some(url=>url.includes('/audit?page=2')));
  await page.locator('#themeToggle').click();assert.ok(['dark','light'].includes(await page.locator('html').getAttribute('data-theme')));
  await page.setViewportSize({width:390,height:844});for(const name of ['accounts','groups','overview','models','keys','logs','audit','settings']){await page.locator('#nav-'+name).click();await page.locator('#tab-'+name+'.on').waitFor();}
  assert.deepEqual(errors,[]);console.log('PASS: production Vue pages, expired login, draft retention, key budget, usage/date controls, audit pagination, XSS, strict CSP, theme and mobile.');await context.close();
 }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exit(1);});
