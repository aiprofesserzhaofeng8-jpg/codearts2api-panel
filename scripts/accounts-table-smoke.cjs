// Only fixture API responses: no credentials, real rewards or provider traffic.
const {chromium}=require('playwright'),assert=require('node:assert/strict'),fs=require('node:fs');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});try{
for(const locale of ['zh-CN','en-US']){
 const context=await browser.newContext({locale,viewport:{width:1440,height:1000}}),page=await context.newPage(),errors=[],writes=[],rewardCalls=[],creditCalls=[];
 page.on('pageerror',e=>errors.push(e.message));let fail=true, releaseCredit=null, holdCredit=false;
 const accounts=['Alpha','Beta','Gamma'].map((name,i)=>({id:'a'+i,name,provider:i===2?'kiro':'qoder',scope:'cn',source:'browser',has_refresh:true,expires_at:1800000000,expires_in:7200,enabled:true,status:'ok',daily_claim_supported:i!==2,claimed_today:false,pool:{state:'ready',in_flight:0,max_in_flight:2},last_used:1700000000+i}));
 await context.route('**/setup/status',r=>r.fulfill({json:{ready:true,needs_setup:false}}));
 await context.route('**/auth/session',r=>r.fulfill({json:{authenticated:true,user:{id:'admin',username:'admin',role:'admin'}}}));
 await context.route('**/admin/api/**',async r=>{const url=new URL(r.request().url()),path=url.pathname;let value={ok:true};
  if(path.endsWith('/state'))value={accounts,keys:[],models:[],settings:{pool:{}},stats:{}};
  if(path.endsWith('/credits')){if(holdCredit)await new Promise(resolve=>{releaseCredit=resolve;});creditCalls.push(url.searchParams.getAll('id'));value={accounts:accounts.filter(a=>url.searchParams.getAll('id').includes(a.id)).map(a=>({id:a.id,ok:true,credit:{available:true,updated_at:1794500000,kind:'windows',windows:[0,1,2].map(i=>({id:'w'+i,name:'window'+i,unit:'percent',utilization:i===2?100:20}))}}))};}
  if(path.endsWith('/welfare')){const id=url.searchParams.get('id');rewardCalls.push(id);value={ok:true,id,campaigns:[],daily_status:id==='a0'?'claimable':'already_claimed',message:'reward '+id};}
  if(r.request().method()==='POST'){
   const body=r.request().postDataJSON();writes.push({path,...body});await new Promise(resolve=>setTimeout(resolve,60));
   if(path.endsWith('/accounts')){if(body.id==='a1'&&fail)return r.fulfill({status:500,json:{error:{message:'fixture failure'}}});if(body.action==='pause'){const a=accounts.find(a=>a.id===body.id);a.paused=true;a.pool.state='paused';}if(body.action==='remove'){value={ok:true,removed_id:body.id==='a1'?'wrong-id':body.id};if(body.id!=='a1')accounts.splice(accounts.findIndex(a=>a.id===body.id),1);}}
   if(path.endsWith('/claim')||path.endsWith('/checkin'))value={ok:true,results:[{id:body.id,ok:true,message:'done'}]};
   if(path.endsWith('/accounts')&&body.action==='export')value={session_json:'{"fixture":true}',name:'Alpha'};
  }
  return r.fulfill({json:value});
 });
 await context.route('**/health',r=>r.fulfill({json:{accounts:3,ready:3,models:0}}));
 await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10098')+'/#accounts');
 const rows=page.locator('#accounts tr[data-account-id]');await rows.first().locator('progress').first().waitFor({timeout:8000}).catch(async e=>{console.error(errors, (await page.locator('body').innerText()).slice(0,1400));throw e;});
 assert.equal(await rows.first().locator('progress').count(),3);assert.equal(await rows.first().locator('progress').last().getAttribute('value'),'100');
 assert.equal(await page.locator('.account-bulk-bar').isVisible(),false);
 const menu=page.locator('#accountToolsMenu'),toggle=page.locator('#accountToolsToggle');
 await toggle.click();assert.equal(await menu.isVisible(),true);assert.equal(await toggle.getAttribute('aria-expanded'),'true');assert.equal(await page.locator('#accountToolsExport').isDisabled(),true);
 await page.locator('#accountToolsImport').click();assert.equal(await page.locator('#accountMethodImport').getAttribute('aria-pressed'),'true');await page.locator('#accountCreateCancel').click();
 await rows.first().locator('input[type=checkbox]').check();await toggle.click();await page.locator('#accountToolsExport').click();await page.waitForFunction(()=>document.querySelector('#exportJSON').value==='{"fixture":true}');assert.ok((await page.locator('#exportMeta').textContent()).includes(locale==='zh-CN'?'未知':'Unknown'));assert.deepEqual(writes.map(w=>({id:w.id,action:w.action})),[{id:'a0',action:'export'}]);await page.locator('#closeExport').click();await page.locator('#accountClearSelection').click();writes.length=0;
 const welfare=page.locator('#accountWelfare'),summary=welfare.locator(':scope > summary');
 assert.equal(await welfare.getAttribute('open'),null);assert.equal(await page.locator('#loadWelfare').isVisible(),false);
 const layout=()=>page.evaluate(()=>{const wrap=document.querySelector('.account-table-wrap'),table=document.querySelector('.accounts-workspace'),head=document.querySelector('.account-table th'),footer=document.querySelector('.account-table-footer');return {pageHeight:document.documentElement.scrollHeight,viewport:innerHeight,cardHeight:table.getBoundingClientRect().height,wrapHeight:wrap.clientHeight,scrollHeight:wrap.scrollHeight,headerTop:head.getBoundingClientRect().top,footerTop:footer.getBoundingClientRect().top,pageTop:scrollY};});
 const baseline=await layout();assert.ok(baseline.pageHeight<=baseline.viewport+1);
 for(let i=0;i<40;i++)accounts.push({...accounts[0],id:'extra'+i,name:'Extra '+i});
 await page.reload();await page.waitForFunction(()=>document.querySelectorAll('#accounts tr[data-account-id]').length===20);
 assert.ok((await page.locator('#accountFilterCount').textContent()).includes('43'));assert.equal(await page.locator('#accountPrevPage').isDisabled(),true);
 await page.locator('.quota-updated').first().waitFor({state:'visible'});assert.ok(creditCalls.every(ids=>ids.length>0&&ids.length<=20));
 const quotaBefore=await rows.first().locator('[data-column="quota"]').textContent();
 await page.locator('.account-table-wrap').evaluate(el=>{el.scrollTop=300;});
 await page.evaluate(async()=>{await document.fonts.ready;await Promise.all(document.getAnimations().filter(a=>a.effect?.getTiming().iterations!==Infinity).map(a=>a.finished.catch(()=>{})));});
 const stableBefore=await layout();holdCredit=true;await page.locator('#refreshCredits').click();
 await page.waitForTimeout(100);const stableDuring=await layout();
 assert.equal(await rows.first().locator('[data-column="quota"]').textContent(),quotaBefore);
 assert.equal(stableDuring.scrollHeight,stableBefore.scrollHeight);assert.equal(stableDuring.footerTop,stableBefore.footerTop);
 holdCredit=false;releaseCredit();await page.waitForResponse(r=>r.url().includes('/credits?')&&r.url().includes('fresh=1'));
 assert.equal((await layout()).scrollHeight,stableBefore.scrollHeight);
 await page.locator('.account-table-wrap').evaluate(el=>{el.scrollTop=0;});
 await page.locator('#accountSelectAll').check();assert.equal(await rows.locator('input:checked').count(),20);
 await page.locator('#accountNextPage').click();assert.equal(await rows.first().getAttribute('data-account-id'),'extra17');assert.equal(await rows.locator('input:checked').count(),0);assert.equal(await page.locator('.account-bulk-bar').isVisible(),false);
 await rows.nth(0).locator('input[type=checkbox]').check();await rows.nth(1).locator('input[type=checkbox]').check();await page.locator('#accountBulkAction').selectOption('probe');await page.locator('#accountBulkStart').click();assert.equal(await page.locator('#accountBulkTargets li').count(),2);await page.locator('#accountBulkConfirm').click();await page.waitForFunction(()=>!document.querySelector('#accountBulkClose').disabled);assert.deepEqual(writes.map(w=>w.id),['extra17','extra18']);await page.locator('#accountBulkClose').click();writes.length=0;
 await page.locator('#accountNextPage').click();assert.equal(await rows.count(),3);assert.equal(await page.locator('#accountNextPage').isDisabled(),true);
 fs.mkdirSync('artifacts',{recursive:true});await toggle.click();await page.screenshot({path:'artifacts/accounts-tools-'+locale+'.png',fullPage:true});await page.keyboard.press('Escape');assert.equal(await toggle.getAttribute('aria-expanded'),'false');
 await page.locator('#accountSearch').fill('Alpha');assert.equal(await rows.count(),1);assert.equal(await page.locator('#accountPrevPage').isDisabled(),true);assert.equal(await page.locator('#accountNextPage').isDisabled(),true);await page.locator('#accountSearch').fill('');
 await page.locator('#accountPageSize').selectOption('10');assert.equal(await rows.count(),10);await page.reload();await page.waitForFunction(()=>document.querySelectorAll('#accounts tr[data-account-id]').length===10);assert.equal(await page.locator('#accountPageSize').inputValue(),'10');
 await page.locator('#accountPageNumbers button').last().click();assert.equal(await rows.count(),3);
 await page.locator('#accountPageSize').selectOption('50');assert.equal(await rows.count(),43);
 await rows.last().locator('progress').first().waitFor();
 const many=await layout();assert.ok(Math.abs(many.cardHeight-baseline.cardHeight)<2);assert.ok(many.scrollHeight>many.wrapHeight);
 await rows.last().scrollIntoViewIfNeeded();const scrolled=await layout();assert.equal(scrolled.pageTop,0);assert.ok(Math.abs(scrolled.headerTop-many.headerTop)<2);assert.ok(Math.abs(scrolled.footerTop-many.footerTop)<2);
 await summary.focus();await page.keyboard.press('Enter');assert.equal(await page.locator('#loadWelfare').isVisible(),true);
 assert.ok((await layout()).pageHeight<=baseline.viewport+1);assert.deepEqual(rewardCalls,[]);
 fs.mkdirSync('artifacts',{recursive:true});await page.screenshot({path:'artifacts/accounts-layout-'+locale+'-expanded.png',fullPage:true});
 await page.setViewportSize({width:1280,height:720});assert.ok((await layout()).pageHeight<=721);assert.ok((await layout()).wrapHeight>100);
 await summary.click();await page.setViewportSize({width:1440,height:1000});
 await page.locator('#accountSearch').fill('no matching account');assert.equal(await rows.count(),0);assert.ok(Math.abs((await layout()).cardHeight-baseline.cardHeight)<2);
 accounts.splice(3);await page.locator('#accountPageSize').selectOption('20');await page.reload();await page.waitForFunction(()=>document.querySelectorAll('#accounts tr[data-account-id]').length===3);
 assert.ok(!(await rows.first().textContent()).includes('自动续期已就绪'));
 await page.locator('#accountStatusFilter').selectOption('disabled');assert.equal(await rows.count(),0);await page.locator('#accountStatusFilter').selectOption('all');assert.equal(await rows.count(),3);
 assert.equal(await page.locator('#accountDetailDialog').count(),0);
 assert.equal(await rows.first().getByRole('button',{name:locale==='zh-CN'?'额度详情':'Quota details',exact:true}).count(),0);
 await page.locator('#accountSelectAll').check();await page.locator('#accountSearch').fill('Alpha');assert.ok((await page.locator('#accountSelectionCount').textContent()).includes('1'));await page.locator('#accountSearch').fill('');assert.equal(await rows.locator('input:checked').count(),1);
 await page.locator('#accountSort').selectOption('name');await toggle.click();await page.locator('[data-column-option="credential"]').click();assert.equal(await menu.isVisible(),true);assert.equal(await page.locator('[data-column-option="credential"]').getAttribute('aria-checked'),'false');await page.locator('.accounts-workspace > .card-head h2').click();assert.equal(await menu.isVisible(),false);await page.reload();await rows.first().waitFor();assert.equal(await page.locator('#accountSort').inputValue(),'name');assert.equal(await rows.first().locator('[data-column="credential"]').isVisible(),false);
 await page.locator('#accountSelectAll').check();await page.locator('#accountBulkAction').selectOption('pause');await page.locator('#accountBulkStart').click();assert.equal(await page.locator('#accountBulkTargets li').count(),3);assert.equal(writes.length,0);assert.deepEqual(rewardCalls,[]);
 await page.locator('#accountBulkConfirm').click();await page.locator('#accountBulkClose').waitFor();await page.waitForFunction(()=>!document.querySelector('#accountBulkClose').disabled);
 assert.equal(writes.length,3);assert.deepEqual(writes.map(w=>w.id),['a0','a1','a2']);assert.ok((await page.locator('#accountBulkResults').textContent()).includes('fixture failure'));assert.equal(await page.locator('#accountBulkConfirm').isDisabled(),true);await page.locator('#accountBulkClose').click();
 fail=false;writes.length=0;await page.locator('#accountSelectAll').check();await page.locator('#accountBulkAction').selectOption('claim');await page.locator('#accountBulkStart').click();await page.locator('#accountBulkConfirm').click();await page.waitForFunction(()=>!document.querySelector('#accountBulkClose').disabled);assert.deepEqual(writes.map(w=>w.id),['a0']);assert.deepEqual(rewardCalls.filter(Boolean),['a0','a1']);await page.locator('#accountBulkClose').click();
 await page.locator('#accountBulkAction').selectOption('remove');await page.locator('#accountBulkStart').click();await page.locator('#accountBulkClose').click();assert.equal(writes.length,1);
 fs.mkdirSync('artifacts',{recursive:true});await page.screenshot({path:'artifacts/accounts-table-'+locale+'.png',fullPage:true});
 await page.locator('#themeToggle').click();await page.screenshot({path:'artifacts/accounts-table-'+locale+'-dark.png',fullPage:true});await page.locator('#themeToggle').click();
 await page.setViewportSize({width:390,height:844});await page.locator('#consoleSidebar').waitFor({state:'hidden'});await page.locator('#accountSelectAllMobile').check();assert.equal(await rows.locator('input:checked').count(),await rows.count());await page.locator('#accountSelectAllMobile').uncheck();assert.equal(await rows.locator('input:checked').count(),0);assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await summary.click();assert.equal(await page.locator('#loadWelfare').isVisible(),true);await summary.click();await page.screenshot({path:'artifacts/accounts-table-'+locale+'-mobile.png',fullPage:true});await toggle.click();assert.ok(await menu.evaluate(el=>{const r=el.getBoundingClientRect();return r.left>=0&&r.right<=innerWidth&&r.top>=0&&r.bottom<=innerHeight;}));await page.keyboard.press('Escape');await page.setViewportSize({width:1440,height:1000});
 await page.locator('#accountSelectAll').check();await page.locator('#accountBulkStart').click();await page.locator('#accountBulkConfirm').click();await page.waitForFunction(()=>!document.querySelector('#accountBulkClose').disabled);
 assert.equal(writes.filter(w=>w.action==='remove').length,3);assert.ok((await page.locator('#accountBulkResults').textContent()).includes(locale==='zh-CN'?'操作结果未确认':'Operation result not confirmed'));await page.locator('#accountBulkClose').click();assert.equal(await rows.count(),1);
 assert.deepEqual(errors,[]);await context.close();
}
console.log('PASS accounts table: inline quota, fixed viewport, tools menu, pagination, page-scoped selection, persisted columns/sort/page size, serial partial failure, claim eligibility, delete cancellation, Chinese/English/mobile.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
