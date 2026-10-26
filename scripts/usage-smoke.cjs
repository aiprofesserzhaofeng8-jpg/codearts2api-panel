// Synthetic history only; no upstream requests or production mutations.
const {chromium}=require('playwright');const assert=require('node:assert/strict');const fs=require('node:fs');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chrome'});try{
 const context=await browser.newContext({viewport:{width:1460,height:1100},timezoneId:'Asia/Shanghai'});
 const now=Math.floor(Date.now()/1000), today=Math.floor((Date.now()+8*3600000)/86400000)*86400-8*3600;
 const logs=Array.from({length:125},(_,i)=>({time:i<73?today+100+i:i<113?today-1000-i:today-40*86400-i,model:'deepseek-v4.1-flash',protocol:'responses',status:200,latency_ms:19240,usage:{known:true,input_tokens:1200,output_tokens:200,cached_tokens:1000},estimated_cost:{currency:'USD',amount:.0005}})).sort((a,b)=>b.time-a.time);
 const prices={'deepseek-v4.1-flash':{currency:'USD',input:.3,output:1.2,cached:.006,source:'官方参考价',updated_at:'2026-10-26'}};
 let saves=0,failSave=false,usageCalls=0;
 await context.route('**/admin/auth/session',r=>r.fulfill({json:{authenticated:true,local_compatibility:false}}));
await context.route('**/admin/api/**',async route=>{const u=new URL(route.request().url()),path=u.pathname;
  if(path.endsWith('/state'))return route.fulfill({json:{accounts:[],keys:[],stats:{total_requests:99999},settings:{pool:{}},models:[{id:'deepseek-v4.1-flash',source:'upstream'},{id:'glm-5.3',source:'upstream'}]}});
  if(path.endsWith('/usage')){
   usageCalls++;const start=Number(u.searchParams.get('start')||0),end=Number(u.searchParams.get('end')||0),size=Number(u.searchParams.get('page_size')||20);
   const selected=logs.filter(l=>(!start||l.time>=start)&&(!end||l.time<end)),pages=Math.max(1,Math.ceil(selected.length/size)),page=Math.min(pages,Number(u.searchParams.get('page')||1)),n=selected.length;
   return route.fulfill({json:{logs:selected.slice((page-1)*size,page*size),stats:{total_requests:n,usage_requests:n,input_tokens:n*1200,output_tokens:n*200,cached_tokens:n*1000,latency_samples:n,total_latency_ms:n*19240,priced_requests:n,priced_usd_requests:n,estimated_usd:n*.0005},page,page_size:size,pages,total:n,retained:125,oldest:logs[124].time,retention_limit:100000}});
  }
  if(path.endsWith('/pricing')){if(route.request().method()==='POST'){saves++;if(failSave)return route.fulfill({status:500,json:{error:{message:'模拟保存失败'}}});const b=route.request().postDataJSON();prices[b.model]={...b.price,updated_at:'now'};}return route.fulfill({json:{prices}});}
  return route.fulfill({json:{accounts:[],logs:[],campaigns:[]}});
 });
 await context.route('**/health',r=>r.fulfill({json:{accounts:0,ready:0}}));
 const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10087')+'/#overview');
 const ready=()=>page.waitForFunction(()=>document.querySelector('#usageLogs').getAttribute('aria-busy')==='false');await ready();
 assert.equal(await page.locator('.usage-kpi.requests .usage-value').textContent(),'125');
 assert.equal(await page.locator('#usageLogs tr').count(),20);
 const summary=await page.locator('.usage-kpi.tokens .usage-value').textContent();
 await page.getByRole('button',{name:'下一页',exact:true}).click();await ready();assert.match(await page.locator('#usagePageInfo').textContent(),/21–40/);
 assert.equal(await page.locator('.usage-kpi.tokens .usage-value').textContent(),summary);
 await page.locator('#usagePageJump').fill('7');await page.locator('#usageJump').click();await ready();assert.equal(await page.locator('#usageLogs tr').count(),5);
 assert.equal(await page.getByRole('button',{name:'下一页',exact:true}).isDisabled(),true);
 await page.locator('#usagePageSize').selectOption('50');await ready();assert.equal(await page.locator('#usageLogs tr').count(),50);assert.match(await page.locator('#usagePageInfo').textContent(),/1–50/);
 await page.locator('#usagePreset').selectOption('today');await ready();assert.equal(await page.locator('.usage-kpi.requests .usage-value').textContent(),'73');assert.match(await page.locator('#usagePageInfo').textContent(),/共 73 条/);
 await page.locator('#usagePreset').selectOption('yesterday');await ready();assert.equal(await page.locator('.usage-kpi.requests .usage-value').textContent(),'40');
 await page.locator('#usageStart').fill('2090-01-01');await page.locator('#usageEnd').fill('2090-01-02');await page.locator('#applyUsageDates').click();await ready();assert.equal(await page.locator('.usage-kpi.requests .usage-value').textContent(),'0');assert.match(await page.locator('#usageLogs').textContent(),/暂无/);
 const before=usageCalls;await page.locator('#usageStart').fill('2090-02-01');await page.locator('#applyUsageDates').click();assert.equal(usageCalls,before);
 await page.locator('#usagePreset').selectOption('7');await ready();assert.equal(await page.locator('.usage-kpi.requests .usage-value').textContent(),'113');
 await page.evaluate(()=>document.documentElement.dataset.theme='light');fs.mkdirSync('artifacts',{recursive:true});await page.screenshot({path:'artifacts/usage-time-pagination-light.png',fullPage:true});
 await page.locator('#nav-models').click();await page.locator('#pricingRows tr').nth(1).waitFor();assert.equal(await page.locator('#pricingRows input').count(),0);
 const glm=page.locator('#pricingRows tr').filter({hasText:'glm-5.3'});
 await glm.getByRole('button',{name:'编辑',exact:true}).click();await page.locator('#priceEditInput').fill('3');await page.locator('#priceEditCancel').click();assert.equal(saves,0);
 await glm.getByRole('button',{name:'编辑',exact:true}).click();assert.equal(await page.locator('#priceEditInput').inputValue(),'');
 await page.locator('#priceEditSave').click();assert.equal(saves,0);
 await page.locator('#priceEditInput').fill('3');await page.locator('#priceEditOutput').fill('10');await page.locator('#priceEditCached').fill('0');
 failSave=true;await page.locator('#priceEditSave').click();await page.waitForFunction(()=>document.querySelector('#priceEditError').textContent.includes('保存失败'));assert.equal(await page.locator('#priceEditDialog').isVisible(),true);
 failSave=false;await page.locator('#priceEditSave').click();await page.locator('#priceEditDialog').waitFor({state:'hidden'});assert.equal(prices['glm-5.3'].cached,0);assert.equal(prices['glm-5.3'].currency,'USD');assert.equal(await page.locator('#pricingRows input').count(),0);
 await glm.getByRole('button',{name:'编辑',exact:true}).click();await page.screenshot({path:'artifacts/pricing-edit-dialog.png',fullPage:true});await page.keyboard.press('Escape');
 await page.locator('#nav-overview').click();await ready();await page.setViewportSize({width:390,height:844});await page.screenshot({path:'artifacts/usage-time-pagination-mobile.png',fullPage:true});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
 await page.evaluate(()=>document.documentElement.dataset.theme='dark');await page.setViewportSize({width:1460,height:1100});await page.screenshot({path:'artifacts/usage-time-pagination-dark.png',fullPage:true});
 assert.deepEqual(errors,[]);console.log('PASS: server pagination, page size/jump, date presets/custom/empty/invalid ranges, full-range stats, USD read-only prices, cancel/save/failure/retry, light/dark/mobile.');
 }finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
