// Account fixtures only. No production credentials, upstream RPC, or model calls.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
async function until(check){const end=Date.now()+5000;while(!check()){assert.ok(Date.now()<end,'fixture condition timed out');await delay(10);}}
(async()=>{
 const browser=await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL||'chrome',headless:true});
 try{
  const ctx=await browser.newContext({viewport:{width:1440,height:1000}}),page=await ctx.newPage(),errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  let accounts=['a','b','c','disabled'].map(id=>({id,name:'WorkBuddy '+id,provider:'workbuddy',scope:'cn',enabled:id!=='disabled',growth_tasks_supported:true,status:'ok',pool:{state:id==='disabled'?'disabled':'ready',in_flight:0,max_in_flight:2}}));
  accounts.push({id:'global',name:'WB global',provider:'workbuddy',scope:'global',enabled:true},{id:'enterprise',name:'WB enterprise',provider:'workbuddy',scope:'cn',enabled:true},{id:'trae',name:'Trae fixture',provider:'trae',enabled:true},{id:'ca',name:'CodeArts fixture',provider:'codearts',enabled:true});
  const tasks={};
  function resetTasks(){for(const id of ['a','b','c','disabled'])tasks[id]=['first','second'].map(code=>({task_code:code,title:'任务 '+code,description:'完成对应操作后领取奖励',credit:100,energy:5,current:1,target:1,claimable:true,claimed:false,locked:false}));}
  resetTasks();
  let readCount=0,activeReads=0,peakReads=0,activeClaims=0,peakClaims=0,claimCalls=[],perAccount={},listDelay=70,claimDelay=70,failRead='',pendingAccount='';
  await ctx.route('**/admin/auth/session',r=>r.fulfill({json:{authenticated:true}}));
  await ctx.route('**/health',r=>r.fulfill({json:{accounts:accounts.length,ready:accounts.length-1}}));
  await ctx.route('**/admin/api/**',async route=>{
   const req=route.request(),u=new URL(req.url());let out={ok:true};
   if(u.pathname.endsWith('/state'))out={accounts,groups:[],keys:[],models:[],settings:{pool:{max_in_flight:2}},stats:{}};
   if(u.pathname.endsWith('/credits'))out={accounts:[]};
   if(u.pathname==='/admin/api/workbuddy/tasks'){
    const id=u.searchParams.get('id');assert.ok(tasks[id],'unsupported account queried');readCount++;activeReads++;peakReads=Math.max(peakReads,activeReads);
    await delay(listDelay);activeReads--;
    if(id===failRead)return route.fulfill({status:502,json:{error:{message:'fixture list failure'}}});
    out={ok:true,id,tasks:tasks[id]};
   }
   if(u.pathname==='/admin/api/workbuddy/tasks/claim'){
    const b=req.postDataJSON();assert.ok(['a','b','c'].includes(b.id),'disabled or unsupported account claimed');
    claimCalls.push(b);activeClaims++;perAccount[b.id]=(perAccount[b.id]||0)+1;assert.equal(perAccount[b.id],1,'same-account claims overlap');peakClaims=Math.max(peakClaims,activeClaims);
    await delay(claimDelay);activeClaims--;perAccount[b.id]--;
    const pending=b.id===pendingAccount;
    if(!pending){const task=tasks[b.id].find(t=>t.task_code===b.task_code);assert.ok(!task.claimed,'duplicate claim');task.claimed=true;task.claimable=false;}
    out={ok:true,id:b.id,tasks:tasks[b.id],state:pending?'pending':'claimed',message:pending?'等待上游确认，请刷新':'奖励已确认'};
   }
   return route.fulfill({json:out});
  });
  const base=process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:19089';
  await page.goto(base+'/#accounts');await page.locator('#accounts tr').first().waitFor();
  assert.equal(await page.locator('#accountGrowth').isVisible(),false);
  for(const provider of ['codearts','trae','']){await page.locator('#accountProvider').selectOption(provider);assert.equal(await page.locator('#accountGrowth').isVisible(),false);}
  await page.locator('#accountProvider').selectOption('workbuddy');assert.equal(await page.locator('#accountGrowth').isVisible(),true);assert.equal(readCount,0);
  const idle=()=>page.waitForFunction(()=>document.querySelector('#growthDialog').open&&!document.querySelector('#growthRefresh').disabled);
  const open=async()=>{await page.locator('#accountGrowth').click();await idle();};
  await open();assert.equal(readCount,4);assert.equal(peakReads,2);assert.equal(await page.locator('#growthAccountSelect option').count(),5);
  assert.equal(await page.locator('#growthTaskList .growth-account').count(),4);
  for(const group of await page.locator('#growthTaskList .growth-account').all())assert.equal(await group.locator('.growth-task').first().isVisible(),true);
  await page.locator('#growthClaimAll').click();await idle();
  assert.equal(claimCalls.length,6);assert.equal(peakClaims,2);assert.equal(await page.locator('#growthClaimAll').isDisabled(),true);
  for(const id of ['a','b','c'])assert.deepEqual(claimCalls.filter(c=>c.id===id).map(c=>c.task_code),['first','second']);
  const beforeScope=readCount;await page.locator('#growthAccountSelect').selectOption('b');await idle();
  assert.equal(readCount,beforeScope+1);assert.equal(await page.locator('#growthTaskList .growth-account').count(),1);assert.equal(await page.locator('#growthAccountName').textContent(),'WorkBuddy b');
  await page.locator('#growthClose').click();
  resetTasks();claimCalls=[];pendingAccount='b';failRead='c';
  await open();assert.match(await page.locator('#growthTaskList [data-account-id="c"]').textContent(),/查询失败/);
  assert.equal(await page.locator('#growthTaskList [data-account-id="c"] .growth-task').count(),0);
  await page.locator('#growthClaimAll').click();await idle();
  assert.equal(claimCalls.length,3);assert.equal(claimCalls.filter(c=>c.id==='b').length,1);assert.equal(claimCalls.filter(c=>c.id==='a').length,2);
  assert.match(await page.locator('#growthTaskList [data-account-id="b"]').textContent(),/待刷新确认/);
  await page.locator('#growthClose').click();
  resetTasks();pendingAccount='';failRead='';await open();claimCalls=[];claimDelay=250;
  await page.locator('#growthClaimAll').click();await until(()=>activeClaims===2);await page.locator('#growthClose').click();await delay(350);
  assert.equal(claimCalls.length,2,'closing should stop queued accounts and later tasks');
  listDelay=250;const beforeClose=readCount;await page.locator('#accountGrowth').click();await until(()=>activeReads===2);await page.locator('#growthClose').click();await delay(350);
  assert.equal(readCount-beforeClose,2,'closing should stop queued account reads');
  listDelay=30;claimDelay=30;await open();
  fs.mkdirSync('artifacts/growth-multi-20261030',{recursive:true});
  for(const theme of ['light','dark']){
   await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);
   for(const viewport of [{width:1440,height:1000},{width:390,height:844}]){
    await page.setViewportSize(viewport);assert.equal(await page.locator('#growthDialog').evaluate(e=>e.scrollWidth>e.clientWidth),false);
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
    await page.screenshot({path:`artifacts/growth-multi-20261030/${theme}-${viewport.width}.png`});
   }
  }
  await page.locator('#growthClose').click();const stopped=readCount;await delay(200);assert.equal(readCount,stopped);
  accounts=accounts.filter(a=>!a.growth_tasks_supported);await page.reload();await page.locator('#accounts tr').first().waitFor();await page.locator('#accountProvider').selectOption('workbuddy');
  assert.equal(await page.locator('#accountGrowth').isVisible(),true);assert.equal(await page.locator('#accountGrowth').isDisabled(),true);
  assert.deepEqual(errors,[]);
  const result={provider_button_visibility:true,multi_account_queries:true,read_concurrency_peak:peakReads,claim_concurrency_peak:peakClaims,same_account_serial:true,disabled_and_unsupported_accounts_excluded:true,account_scope_switch:true,partial_error_isolation:true,pending_stops_same_account:true,close_stops_queued_reads_and_claims:true,no_background_polling:true,empty_supported_accounts_disabled:true,light_dark_desktop_mobile:true,javascript_errors:errors};
  fs.writeFileSync('artifacts/growth-multi-20261030/browser.json',JSON.stringify(result,null,2));console.log(JSON.stringify(result,null,2));
 }finally{await browser.close();}
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
