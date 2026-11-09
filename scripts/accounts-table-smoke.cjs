// Only fixture API responses: no credentials, real rewards or provider traffic.
const {chromium}=require('playwright'),assert=require('node:assert/strict'),fs=require('node:fs');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});try{
for(const locale of ['zh-CN','en-US']){
 const context=await browser.newContext({locale,viewport:{width:1440,height:1000}}),page=await context.newPage(),errors=[],writes=[],rewardCalls=[];
 page.on('pageerror',e=>errors.push(e.message));let fail=true;
 const accounts=['Alpha','Beta','Gamma'].map((name,i)=>({id:'a'+i,name,provider:i===2?'kiro':'qoder',scope:'cn',source:'browser',has_refresh:true,expires_at:1800000000,expires_in:7200,enabled:true,status:'ok',daily_claim_supported:i!==2,claimed_today:false,pool:{state:'ready',in_flight:0,max_in_flight:2},last_used:1700000000+i}));
 await context.route('**/setup/status',r=>r.fulfill({json:{ready:true,needs_setup:false}}));
 await context.route('**/auth/session',r=>r.fulfill({json:{authenticated:true,user:{id:'admin',username:'admin',role:'admin'}}}));
 await context.route('**/admin/api/**',async r=>{const url=new URL(r.request().url()),path=url.pathname;let value={ok:true};
  if(path.endsWith('/state'))value={accounts,keys:[],models:[],settings:{pool:{}},stats:{}};
  if(path.endsWith('/credits'))value={accounts:accounts.filter(a=>!url.searchParams.get('id')||a.id===url.searchParams.get('id')).map(a=>({id:a.id,ok:true,credit:{available:true,kind:'windows',windows:[0,1,2].map(i=>({id:'w'+i,name:'window'+i,unit:'percent',utilization:i===2?100:20}))}}))};
  if(path.endsWith('/welfare')){const id=url.searchParams.get('id');rewardCalls.push(id);value={ok:true,id,campaigns:[],daily_status:id==='a0'?'claimable':'already_claimed',message:'reward '+id};}
  if(r.request().method()==='POST'){
   const body=r.request().postDataJSON();writes.push({path,...body});await new Promise(resolve=>setTimeout(resolve,60));
   if(path.endsWith('/accounts')){if(body.id==='a1'&&fail)return r.fulfill({status:500,json:{error:{message:'fixture failure'}}});if(body.action==='pause'){const a=accounts.find(a=>a.id===body.id);a.paused=true;a.pool.state='paused';}if(body.action==='remove'){value={ok:true,removed_id:body.id==='a1'?'wrong-id':body.id};if(body.id!=='a1')accounts.splice(accounts.findIndex(a=>a.id===body.id),1);}}
   if(path.endsWith('/claim')||path.endsWith('/checkin'))value={ok:true,results:[{id:body.id,ok:true,message:'done'}]};
  }
  return r.fulfill({json:value});
 });
 await context.route('**/health',r=>r.fulfill({json:{accounts:3,ready:3,models:0}}));
 await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10098')+'/#accounts');
 const rows=page.locator('#accounts tr[data-account-id]');await rows.first().locator('progress').first().waitFor({timeout:8000}).catch(async e=>{console.error(errors, (await page.locator('body').innerText()).slice(0,1400));throw e;});
 assert.equal(await rows.first().locator('progress').count(),2);assert.equal(await rows.first().locator('progress').first().getAttribute('value'),'100');
 assert.ok(!(await rows.first().textContent()).includes('自动续期已就绪'));
 await rows.first().locator('button').filter({hasText:locale==='zh-CN'?'额度详情':'Quota details'}).click();assert.equal(await page.locator('#accountDetailBody progress').count(),3);await page.locator('#accountDetailClose').click();
 await page.locator('#accountSelectAll').check();await page.locator('#accountSearch').fill('Alpha');assert.ok((await page.locator('#accountSelectionCount').textContent()).includes('1'));await page.locator('#accountSearch').fill('');assert.equal(await rows.locator('input:checked').count(),1);
 await page.locator('#accountSort').selectOption('name');await page.locator('.account-columns summary').click();await page.locator('#accountColumnOptions input').first().uncheck();await page.reload();await rows.first().waitFor();assert.equal(await page.locator('#accountSort').inputValue(),'name');assert.equal(await rows.first().locator('[data-column="credential"]').isVisible(),false);
 await page.locator('#accountSelectAll').check();await page.locator('#accountBulkAction').selectOption('pause');await page.locator('#accountBulkStart').click();assert.equal(await page.locator('#accountBulkTargets li').count(),3);assert.equal(writes.length,0);assert.deepEqual(rewardCalls,[]);
 await page.locator('#accountBulkConfirm').click();await page.locator('#accountBulkClose').waitFor();await page.waitForFunction(()=>!document.querySelector('#accountBulkClose').disabled);
 assert.equal(writes.length,3);assert.deepEqual(writes.map(w=>w.id),['a0','a1','a2']);assert.ok((await page.locator('#accountBulkResults').textContent()).includes('fixture failure'));assert.equal(await page.locator('#accountBulkConfirm').isDisabled(),true);await page.locator('#accountBulkClose').click();
 fail=false;writes.length=0;await page.locator('#accountSelectAll').check();await page.locator('#accountBulkAction').selectOption('claim');await page.locator('#accountBulkStart').click();await page.locator('#accountBulkConfirm').click();await page.waitForFunction(()=>!document.querySelector('#accountBulkClose').disabled);assert.deepEqual(writes.map(w=>w.id),['a0']);assert.deepEqual(rewardCalls.filter(Boolean),['a0','a1']);await page.locator('#accountBulkClose').click();
 await page.locator('#accountBulkAction').selectOption('remove');await page.locator('#accountBulkStart').click();await page.locator('#accountBulkClose').click();assert.equal(writes.length,1);
 fs.mkdirSync('artifacts',{recursive:true});await page.screenshot({path:'artifacts/accounts-table-'+locale+'.png',fullPage:true});
 await page.setViewportSize({width:390,height:844});await page.locator('#consoleSidebar').waitFor({state:'hidden'});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await page.screenshot({path:'artifacts/accounts-table-'+locale+'-mobile.png',fullPage:true});await page.setViewportSize({width:1440,height:1000});
 await page.locator('#accountBulkStart').click();await page.locator('#accountBulkConfirm').click();await page.waitForFunction(()=>!document.querySelector('#accountBulkClose').disabled);
 assert.equal(writes.filter(w=>w.action==='remove').length,3);assert.ok((await page.locator('#accountBulkResults').textContent()).includes(locale==='zh-CN'?'操作结果未确认':'Operation result not confirmed'));await page.locator('#accountBulkClose').click();assert.equal(await rows.count(),1);
 assert.deepEqual(errors,[]);await context.close();
}
console.log('PASS accounts table: compact/full quota, safe labels, filter scope, persisted columns/sort, serial partial failure, claim eligibility, delete cancellation, Chinese/English/mobile.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
