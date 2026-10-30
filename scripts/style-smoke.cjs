// Visual verification with synthetic account/key data; never calls Huawei APIs.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const fs=require('node:fs');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||undefined});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:1000},colorScheme:'dark',hasTouch:true});
  const now=Math.floor(Date.now()/1000);
  const accounts=['主力开发账号','项目测试账号','备用账号','个人体验账号'].map((name,i)=>({
   id:'fixture-'+i,name,source:'browser',enabled:true,paused:i===3,status:'ok',failures:0,
   access_key:'AK***'+i,has_refresh:i!==2,expires_at:now+7200,expires_in:7200,last_used:now-600,
   claimed_today:i<2,pool:{state:['ready','ready','cooling','paused'][i],in_flight:i===0?1:0,max_in_flight:2,cooldown_remaining_sec:i===2?90:0}
  }));
  const settings={pool:{strategy:'least_inflight',max_in_flight:2,sticky_ttl_sec:1800,rate_cooldown_sec:60,breaker_threshold:3,breaker_cooldown_sec:300},default_model:'glm-5.3',allow_credit_models:true,request_timeout_sec:300,max_retries:3,log_keep:500,live_ttl_sec:10,rate_limit_enabled:true,rate_limit_qps:5,rate_limit_burst:10,rate_limit_concurrency:8,auto_refresh:true};
  const models=['GLM-5.3','DeepSeek-V4.1-Flash','OpenPangu-2.0-Pro'].map((name,i)=>({id:name.toLowerCase(),name,credit:['0.79x','0.18x','0.7x'][i],description:'通用编程与智能问答',source:'upstream'}));
  const snapshot={accounts,settings,keys:[{key:'sk-ca-…fixture',value:'sk-ca-visual-fixture-not-a-real-key',name:'本地开发',enabled:true,created_at:now,requests:128,last_used:now}],models,stats:{total_requests:1248,failed_requests:6,total_chars_out:186400,started_at:now-86400}};
  await context.route('**/admin/auth/session',r=>r.fulfill({json:{authenticated:true,local_compatibility:false}}));
await context.route('**/admin/api/**',route=>{
   const path=new URL(route.request().url()).pathname;
   if(path.endsWith('/state'))return route.fulfill({json:snapshot});
   if(path.endsWith('/credits'))return route.fulfill({json:{accounts:accounts.map((a,i)=>({id:a.id,ok:true,credit:{available:true,total:10000,remaining:[8240,4670,1260,6000][i],used:[1760,5330,8740,4000][i],status:'active',reset_date:'2026-11-01'}}))}});
   if(path.endsWith('/logs'))return route.fulfill({json:{logs:[
    {time:now,model:'glm-5.3',stream:true,status:200,first_token_ms:123,latency_ms:1240,attempt_details:[{number:1,duration_ms:1200,status:200}],prompt_len:45,output_len:126,account:'界面测试账号',key:'fixture'},
    {time:now,model:'glm-5.3',stream:false,status:200,latency_ms:1240,prompt_len:45,output_len:126,account:'旧日志测试',key:'fixture'}
   ]}});
   if(path.endsWith('/settings'))return route.fulfill({json:{ok:true,settings}});
   return route.fulfill({json:{campaigns:[],accounts:[],ok:true}});
  });
  await context.route('**/health',r=>r.fulfill({json:{ready:2,accounts:4,models:3}}));
  const page=await context.newPage();const errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10087')+'/');
  await page.locator('#accounts tr').nth(3).waitFor();
  await page.waitForFunction(()=>document.querySelectorAll('.credit-bar progress').length===4);
  assert.equal(await page.locator('#nav-accounts').getAttribute('aria-selected'),'true');
  assert.equal(await page.locator('#poolStats .pool-stat').count(),6);
  assert.equal(await page.locator('.credit-bar progress').first().getAttribute('style'),null);
  fs.mkdirSync('artifacts',{recursive:true});
  const firstRow=page.locator('#accounts tr').first(), menu=page.locator('#accountActionMenu');
  assert.equal(await firstRow.locator('.actions-cell button').count(),4);
  for(const name of ['设置','更多']){
   const trigger=firstRow.getByRole('button',{name,exact:true});
   for(let attempt=0;attempt<2;attempt++){
    await trigger.click();assert.equal(await menu.isVisible(),true);
    assert.equal(await trigger.getAttribute('aria-expanded'),'true');
    await trigger.click();assert.equal(await menu.isVisible(),false,'second click must close '+name);
    assert.equal(await trigger.getAttribute('aria-expanded'),'false');
   }
  }
  for(const key of ['Enter','Space']){
   const trigger=firstRow.getByRole('button',{name:'设置',exact:true});
   await trigger.focus();await page.keyboard.press(key);assert.equal(await menu.isVisible(),true);
   await trigger.focus();await page.keyboard.press(key);assert.equal(await menu.isVisible(),false);
  }
  await firstRow.getByRole('button',{name:'设置',exact:true}).click();
  await firstRow.getByRole('button',{name:'更多',exact:true}).click();
  assert.equal(await firstRow.getByRole('button',{name:'设置',exact:true}).getAttribute('aria-expanded'),'false');
  assert.equal(await menu.getByRole('button',{name:'删除',exact:true}).isVisible(),true);
  await page.locator('#accounts tr').nth(1).getByRole('button',{name:'设置',exact:true}).click();
  assert.match(await menu.getAttribute('aria-label'),/项目测试账号/);
  await page.keyboard.press('Escape');

  await firstRow.getByRole('button',{name:'设置',exact:true}).click();
  assert.deepEqual(await menu.getByRole('button').allTextContents(),['并发设置','模型健康']);
  await page.keyboard.press('ArrowDown');assert.equal(await menu.getByRole('button',{name:'模型健康',exact:true}).evaluate(el=>el===document.activeElement),true);
  await page.keyboard.press('Escape');assert.equal(await menu.isVisible(),false);
  assert.equal(await firstRow.getByRole('button',{name:'设置',exact:true}).evaluate(el=>el===document.activeElement),true);
  await firstRow.getByRole('button',{name:'更多',exact:true}).click();
  assert.equal(await menu.getByRole('button',{name:'删除',exact:true}).isVisible(),true);
  const menuBox=await menu.boundingBox();assert.ok(menuBox.x>=0 && menuBox.x+menuBox.width<=1440 && menuBox.y>=0 && menuBox.y+menuBox.height<=1000,'desktop menu stays inside viewport');
  await page.screenshot({path:'artifacts/account-actions-menu-dark.png',animations:'disabled'});
  await page.locator('#pageTitle').click();assert.equal(await menu.isVisible(),false);
  await page.screenshot({animations:'disabled',path:'artifacts/panel-accounts-dark.png',fullPage:true});
  await page.locator('[data-account-filter="cooling"]').click();
  assert.equal(await page.locator('#accounts tr[data-state]').count(),1);
  assert.equal(await page.locator('#accounts tr').getAttribute('data-state'),'cooling');
  await page.locator('[data-account-filter="all"]').click();
  await page.locator('#accountSearch').fill('主力');
  assert.equal(await page.locator('#accounts tr[data-state]').count(),1);
  await page.locator('#accountSearch').fill('不存在');
  await page.getByText('没有符合条件的账号，请更换筛选条件。',{exact:true}).waitFor();
  await page.locator('#accountSearch').fill('');
  await page.locator('#themeToggle').click();
  assert.equal(await page.locator('html').getAttribute('data-theme'),'light');
  await page.reload();await page.locator('#accounts tr').nth(3).waitFor();
  assert.equal(await page.locator('html').getAttribute('data-theme'),'light');
  await page.waitForFunction(()=>document.querySelectorAll('.credit-bar progress').length===4);
  await page.screenshot({animations:'disabled',path:'artifacts/panel-accounts-light.png',fullPage:true});
  for(const tab of ['overview','keys','logs','settings']){
   await page.locator('#nav-'+tab).click();
   assert.equal(await page.locator('#tab-'+tab).isVisible(),true);
   if(tab==='logs'){
    await page.locator('#logs tr').nth(1).waitFor();
    assert.equal(await page.locator('#logs tr').first().locator('td').nth(4).textContent(),'123ms / 1240ms');
    assert.match(await page.locator('#logs tr').first().locator('td').nth(4).getAttribute('title'),/1200ms.*200/);
    assert.equal(await page.locator('#logs tr').nth(1).locator('td').nth(4).textContent(),'— / 1240ms');
   }
   await page.evaluate(()=>window.scrollTo(0,0));
   await page.screenshot({animations:'disabled',path:'artifacts/panel-'+tab+'-light.png',fullPage:tab!=='settings'});
  }
  await page.locator('#nav-accounts').click();
  await page.setViewportSize({width:390,height:844});
  await page.evaluate(()=>window.scrollTo(0,0));
  await page.screenshot({animations:'disabled',path:'artifacts/panel-accounts-mobile.png',fullPage:true});
  const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>window.innerWidth);
  assert.equal(overflow,false,'mobile page should not overflow horizontally');
  await page.locator('#accounts tr').first().getByRole('button',{name:'更多',exact:true}).waitFor({state:'visible'});
  for(const name of ['设置','更多']){
   const trigger=firstRow.getByRole('button',{name,exact:true});
   await trigger.tap();assert.equal(await menu.isVisible(),true);
   await trigger.tap();assert.equal(await menu.isVisible(),false,'second tap must close '+name);
   assert.equal(await trigger.getAttribute('aria-expanded'),'false');
  }

  await firstRow.getByRole('button',{name:'更多',exact:true}).click();
  const mobileMenu=await menu.boundingBox();assert.ok(mobileMenu.x>=0&&mobileMenu.x+mobileMenu.width<=390&&mobileMenu.y>=0&&mobileMenu.y+mobileMenu.height<=844,'mobile menu stays inside viewport');
  await page.screenshot({path:'artifacts/account-actions-menu-mobile.png',animations:'disabled'});
  await page.keyboard.press('Escape');
  await page.setViewportSize({width:320,height:740});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  await page.setViewportSize({width:390,height:844});
  await page.locator('#nav-keys').click();
  assert.equal(await page.locator('#keys').getByRole('button',{name:'复制',exact:true}).isVisible(),true);
  await page.screenshot({animations:'disabled',path:'artifacts/panel-keys-mobile.png',fullPage:true});
  await page.locator('#themeToggle').click();
  assert.equal(await page.locator('html').getAttribute('data-theme'),'dark');
  assert.deepEqual(errors,[]);
  console.log('PASS: desktop dark/light, theme persistence, account search/filter, mobile account cards, key actions, all tabs; synthetic data only.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e.message);process.exitCode=1;});
