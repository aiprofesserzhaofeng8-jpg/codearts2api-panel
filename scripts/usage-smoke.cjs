// Synthetic history only; no upstream requests or production mutations.
const {chromium}=require('playwright');const assert=require('node:assert/strict');const fs=require('node:fs');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chrome'});try{
 const context=await browser.newContext({viewport:{width:1460,height:1100},timezoneId:'Asia/Shanghai'});
 const now=Math.floor(Date.now()/1000), today=Math.floor((Date.now()+8*3600000)/86400000)*86400-8*3600;
 const models=['deepseek-v4.1-flash','glm-5.3','workbuddy/cn/claude-sonnet','trae/cn/gpt-5.3'];
 const logs=Array.from({length:125},(_,i)=>({time:i<73?today+100+i:i<113?today-1000-i:today-40*86400-i,model:models[i%4],provider:i%4===2?'workbuddy':i%4===3?'trae':'codearts',protocol:['responses','chat_completions','messages'][i%3],status:i%17===0?502:200,latency_ms:19240,usage:{known:true,input_tokens:1200+i*10,output_tokens:200+i*5,cached_tokens:1000},estimated_cost:{currency:'USD',amount:.0005}})).sort((a,b)=>b.time-a.time);
 function statsFor(rows){return rows.reduce((s,l)=>{s.total_requests++;s.failed_requests+=l.status>=400?1:0;s.usage_requests++;s.input_tokens+=l.usage.input_tokens;s.output_tokens+=l.usage.output_tokens;s.cached_tokens+=l.usage.cached_tokens;s.latency_samples++;s.total_latency_ms+=l.latency_ms;s.priced_requests++;s.priced_usd_requests++;s.estimated_usd+=l.estimated_cost.amount;return s;},{total_requests:0,failed_requests:0,usage_requests:0,input_tokens:0,output_tokens:0,cached_tokens:0,latency_samples:0,total_latency_ms:0,priced_requests:0,priced_usd_requests:0,estimated_usd:0});}
 function analysisFor(rows){
  const grouped=key=>{const map=new Map();for(const row of rows){const name=row[key];if(!map.has(name))map.set(name,[]);map.get(name).push(row);}return [...map].map(([name,entries])=>({name,stats:statsFor(entries)}));};
  const days=new Map();for(const row of rows){const date=Math.floor((row.time+8*3600)/86400)*86400-8*3600;if(!days.has(date))days.set(date,[]);days.get(date).push(row);}
  const trend=[];if(rows.length){const start=Math.min(...days.keys()),end=Math.max(...days.keys());for(let date=start;date<=end;date+=86400)trend.push({time:date,stats:statsFor(days.get(date)||[])});}
  return {models:grouped('model'),providers:grouped('provider'),protocols:grouped('protocol'),trend,bucket_days:1,time_zone:'Asia/Shanghai'};
 }
 const prices={'deepseek-v4.1-flash':{currency:'USD',input:.3,output:1.2,cached:.006,source:'官方参考价',updated_at:'2026-10-26'}};
 let saves=0,failSave=false,usageCalls=0,failUsage=false;
 await context.route('**/admin/auth/session',r=>r.fulfill({json:{authenticated:true,local_compatibility:false}}));
await context.route('**/admin/api/**',async route=>{const u=new URL(route.request().url()),path=u.pathname;
  if(path.endsWith('/state'))return route.fulfill({json:{accounts:[],keys:[],stats:{total_requests:99999},settings:{pool:{}},models:[{id:'deepseek-v4.1-flash',source:'upstream'},{id:'glm-5.3',source:'upstream'}]}});
  if(path.endsWith('/usage')){
   if(failUsage)return route.fulfill({status:500,json:{error:{message:'fixture history failure'}}});
   assert.equal(u.searchParams.get('time_zone'),'Asia/Shanghai');
   usageCalls++;const start=Number(u.searchParams.get('start')||0),end=Number(u.searchParams.get('end')||0),size=Number(u.searchParams.get('page_size')||20);
   const provider=u.searchParams.get('provider'),selected=logs.filter(l=>(!start||l.time>=start)&&(!end||l.time<end)&&(!provider||l.provider===provider)&&(!u.searchParams.get('model')||l.model===u.searchParams.get('model'))&&(!u.searchParams.get('protocol')||l.protocol===u.searchParams.get('protocol'))&&(!u.searchParams.get('status')||(u.searchParams.get('status')==='error'?l.status>=400:l.status<400))),pages=Math.max(1,Math.ceil(selected.length/size)),page=Math.min(pages,Number(u.searchParams.get('page')||1)),n=selected.length;
   return route.fulfill({json:{logs:selected.slice((page-1)*size,page*size),stats:statsFor(selected),analysis:analysisFor(selected),page,page_size:size,pages,total:n,retained:125,oldest:logs[124].time,retention_limit:100000}});
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
 assert.equal(await page.locator('.usage-charts-grid .usage-chart').count(),4);
 assert.equal(await page.locator('#usageModelsChart .usage-chart-legend-item').count(),4);
 const chartTotal=await page.locator('#usageModelsChart .usage-donut-center strong').textContent();
 const summary=await page.locator('.usage-kpi.tokens .usage-value').textContent();
 await page.getByRole('button',{name:'下一页',exact:true}).click();await ready();assert.match(await page.locator('#usagePageInfo').textContent(),/21–40/);
 assert.equal(await page.locator('.usage-kpi.tokens .usage-value').textContent(),summary);
 assert.equal(await page.locator('#usageModelsChart .usage-donut-center strong').textContent(),chartTotal);
 await page.locator('#usagePageJump').fill('7');await page.locator('#usageJump').click();await ready();assert.equal(await page.locator('#usageLogs tr').count(),5);
 assert.equal(await page.getByRole('button',{name:'下一页',exact:true}).isDisabled(),true);
 await page.locator('#usagePageSize').selectOption('50');await ready();assert.equal(await page.locator('#usageLogs tr').count(),50);assert.match(await page.locator('#usagePageInfo').textContent(),/1–50/);
 await page.locator('#usageDateToggle').click();await page.locator('[data-usage-preset="today"]').click();await ready();assert.equal(await page.locator('.usage-kpi.requests .usage-value').textContent(),'73');assert.match(await page.locator('#usagePageInfo').textContent(),/共 73 条/);
 await page.locator('#usageDateToggle').click();await page.locator('[data-usage-preset="yesterday"]').click();await ready();assert.equal(await page.locator('.usage-kpi.requests .usage-value').textContent(),'40');
 await page.locator('#usageDateToggle').click();await page.locator('#usageStart').fill('2090-01-01');await page.locator('#usageEnd').fill('2090-01-02');await page.locator('#applyUsageDates').click();await ready();assert.equal(await page.locator('.usage-kpi.requests .usage-value').textContent(),'0');assert.match(await page.locator('#usageLogs').textContent(),/暂无/);
 const before=usageCalls;await page.locator('#usageDateToggle').click();await page.locator('#usageStart').fill('2090-02-01');await page.locator('#applyUsageDates').click();assert.equal(usageCalls,before);await page.keyboard.press('Escape');assert.equal(await page.locator('#usageDatePopover').isVisible(),false);
 await page.locator('#usageDateToggle').click();await page.locator('[data-usage-preset="7"]').click();await ready();assert.equal(await page.locator('.usage-kpi.requests .usage-value').textContent(),'113');
 await page.locator('[data-usage-metric="models"]').getByRole('button',{name:'请求',exact:true}).click();assert.equal(await page.locator('#usageModelsChart .usage-donut-center strong').textContent(),'113');
 await page.locator('#usageModelsChart .usage-chart-legend-item').first().focus();assert.match(await page.locator('#usageModelsChart .usage-donut-center strong').textContent(),/%/);
 await page.locator('[data-usage-metric="models"]').getByRole('button',{name:'Token',exact:true}).click();
 await page.locator('#usageTrendChart .usage-trend-hover').first().focus();assert.match(await page.locator('.usage-trend-caption').textContent(),/输入/);
 await page.locator('#usageProvider').selectOption('workbuddy');await ready();assert.equal(await page.locator('#usageProvidersChart .usage-chart-legend-item').count(),1);
 await page.locator('#resetUsageFilters').click();await ready();assert.equal(await page.locator('.usage-kpi.requests .usage-value').textContent(),'125');assert.equal(await page.locator('#usageProvider').inputValue(),'');
 await page.locator('#usageModel').selectOption(models[0]);await ready();assert.equal(await page.locator('#usageModelsChart .usage-chart-legend-item').count(),1);
 await page.locator('#usageProtocol').selectOption('responses');await ready();await page.locator('#usageStatus').selectOption('error');await ready();
 assert.equal(await page.locator('.usage-kpi.requests .usage-value').textContent(),String(logs.filter(l=>l.model===models[0]&&l.protocol==='responses'&&l.status>=400).length));
 await page.locator('#resetUsageFilters').click();await ready();
 await page.locator('#usageDateToggle').click();await page.locator('#usageStart').fill('2000-01-01');await page.locator('.usage-chart-head').nth(1).click();assert.equal(await page.locator('#usageDatePopover').isVisible(),false);await page.locator('#usageDateToggle').click();assert.equal(await page.locator('#usageStart').inputValue(),'');await page.keyboard.press('Escape');
 failUsage=true;await page.locator('#reloadUsage').click();await ready();assert.match(await page.locator('#usageLogs').textContent(),/fixture history failure/);assert.equal(await page.locator('#usageModelsChart svg').count(),0);
 failUsage=false;await page.locator('#reloadUsage').click();await ready();
 await page.locator('#usagePageSize').selectOption('20');await ready();
 if(await page.locator('#toast').isVisible())await page.locator('#toast').click();
 await page.evaluate(()=>{document.documentElement.dataset.theme='light';window.scrollTo(0,0)});fs.mkdirSync('artifacts',{recursive:true});await page.screenshot({path:'artifacts/usage-sub2api-light.png',animations:'disabled'});await page.locator('#usageDateToggle').click();await page.screenshot({path:'artifacts/usage-date-picker.png'});await page.keyboard.press('Escape');await page.screenshot({path:'artifacts/usage-time-pagination-light.png',fullPage:true,animations:'disabled'});
 await page.locator('#nav-models').click();await page.locator('#pricingRows tr').nth(1).waitFor();assert.equal(await page.locator('#pricingRows input').count(),0);
 const glm=page.locator('#pricingRows tr').filter({hasText:'glm-5.3'});
 await glm.getByRole('button',{name:'编辑',exact:true}).click();await page.locator('#priceEditInput').fill('3');await page.locator('#priceEditCancel').click();assert.equal(saves,0);
 await glm.getByRole('button',{name:'编辑',exact:true}).click();assert.equal(await page.locator('#priceEditInput').inputValue(),'');
 await page.locator('#priceEditSave').click();assert.equal(saves,0);
 await page.locator('#priceEditInput').fill('3');await page.locator('#priceEditOutput').fill('10');await page.locator('#priceEditCached').fill('0');
 failSave=true;await page.locator('#priceEditSave').click();await page.waitForFunction(()=>document.querySelector('#priceEditError').textContent.includes('保存失败'));assert.equal(await page.locator('#priceEditDialog').isVisible(),true);
 failSave=false;await page.locator('#priceEditSave').click();await page.locator('#priceEditDialog').waitFor({state:'hidden'});assert.equal(prices['glm-5.3'].cached,0);assert.equal(prices['glm-5.3'].currency,'USD');assert.equal(await page.locator('#pricingRows input').count(),0);
 await glm.getByRole('button',{name:'编辑',exact:true}).click();await page.screenshot({path:'artifacts/pricing-edit-dialog.png',fullPage:true});await page.keyboard.press('Escape');
 await page.locator('#nav-overview').click();await ready();if(await page.locator('#toast').isVisible())await page.locator('#toast').click();await page.setViewportSize({width:390,height:844});await page.evaluate(()=>window.scrollTo(0,0));await page.screenshot({path:'artifacts/usage-sub2api-mobile.png',animations:'disabled'});await page.screenshot({path:'artifacts/usage-time-pagination-mobile.png',fullPage:true,animations:'disabled'});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await page.locator('#usageDateToggle').click();assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await page.keyboard.press('Escape');
 await page.setViewportSize({width:320,height:740});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
 await page.evaluate(()=>document.documentElement.dataset.theme='dark');await page.setViewportSize({width:1460,height:1100});await page.evaluate(()=>window.scrollTo(0,0));await page.screenshot({path:'artifacts/usage-sub2api-dark.png',animations:'disabled'});await page.screenshot({path:'artifacts/usage-time-pagination-dark.png',fullPage:true,animations:'disabled'});
 assert.deepEqual(errors,[]);console.log('PASS: full-range chart summaries across pages, interactive metrics, browser timezone, provider/reset/error recovery, pagination/date ranges, pricing regression and Sub2API layout in light/dark/mobile.');
 }finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
