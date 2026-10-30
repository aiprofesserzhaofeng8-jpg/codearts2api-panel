// Isolated UI fixtures only: all management APIs intercepted; no upstream calls.
const {chromium}=require('playwright'),assert=require('node:assert/strict'),fs=require('node:fs');
const delay=ms=>new Promise(r=>setTimeout(r,ms));
(async()=>{const browser=await chromium.launch({channel:'chrome',headless:true});try{
 const ctx=await browser.newContext({viewport:{width:1440,height:1000}}),page=await ctx.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
 const accounts=['a','b','c','disabled'].map(id=>({id,name:'WorkBuddy '+id,provider:'workbuddy',scope:'cn',enabled:id!=='disabled',growth_tasks_supported:true,status:'ok',pool:{state:id==='disabled'?'disabled':'ready',in_flight:0,max_in_flight:2}}));
 let tasks={},posts=[],active=0,peak=0,perAccount={},pending='',prerequisite='',requestDelay=50,aborted=0;
 function reset(accepted=false){for(const a of accounts)tasks[a.id]=['Model_chat_GLM5.2','chat_5','automation_1'].map(code=>({task_code:code,title:code,description:'任务说明',auto_supported:code!=='automation_1',action_description:code==='automation_1'?'':'真实对话会消耗积分，最多 5 次，每次最多 32 Token',manual_instruction:code==='automation_1'?'请在官方应用真实创建定时任务':'',accept_status:accepted?'accepted':'not_accepted',target:code==='chat_5'?5:1,current:0,claimed:false,claimable:false}));}
 reset();
 await ctx.route('**/admin/auth/session',r=>r.fulfill({json:{authenticated:true}}));await ctx.route('**/health',r=>r.fulfill({json:{accounts:4,ready:3}}));
 page.on('requestfailed',r=>{if(new URL(r.url()).pathname.endsWith('/tasks/run'))aborted++;});
 await ctx.route('**/admin/api/**',async route=>{
  const req=route.request(),url=new URL(req.url());let out={ok:true};
  if(url.pathname.endsWith('/state'))out={accounts,models:[],keys:[],groups:[],settings:{},stats:{}};
  if(url.pathname.endsWith('/credits'))out={accounts:[]};
  if(url.pathname==='/admin/api/workbuddy/tasks')out={id:url.searchParams.get('id'),tasks:tasks[url.searchParams.get('id')]};
  if(['/admin/api/workbuddy/tasks/accept','/admin/api/workbuddy/tasks/run'].includes(url.pathname)){
   const b=req.postDataJSON(),kind=url.pathname.endsWith('/accept')?'accept':'run';assert.ok(['a','b','c'].includes(b.id),'disabled account mutated');posts.push({kind,...b});
   active++;peak=Math.max(peak,active);perAccount[b.id]=(perAccount[b.id]||0)+1;assert.equal(perAccount[b.id],1,'same account operations overlap');await delay(requestDelay);active--;perAccount[b.id]--;
   if(kind==='accept'){
    assert.deepEqual([...b.task_codes].sort(),['Model_chat_GLM5.2','automation_1','chat_5'].sort());for(const t of tasks[b.id])t.accept_status='accepted';out={state:'accepted',tasks:tasks[b.id]};
   }else{
    assert.ok(['Model_chat_GLM5.2','chat_5'].includes(b.task_code),'manual task executed');if(b.id===prerequisite)return route.fulfill({status:409,json:{error:{message:'请先在官方应用完成「领养第一只 Buddy」，再接取此任务'}}});const t=tasks[b.id].find(t=>t.task_code===b.task_code),p=b.id===pending;
    t.execution_pending=p;if(!p){t.current=t.target;t.claimed=true;t.accept_status='claimed';}
    out={state:p?'pending':'claimed',message:p?'真实对话已执行，进度尚未确认；不会重复调用':'奖励已确认',tasks:tasks[b.id],completed_calls:1};
   }
  }
  try{await route.fulfill({json:out});}catch(e){if(!req.failure())throw e;}
 });
 await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:19090')+'/#accounts');await page.locator('#accounts tr').first().waitFor();await page.locator('#accountProvider').selectOption('workbuddy');
 const idle=()=>page.waitForFunction(()=>document.querySelector('#growthDialog').open&&!document.querySelector('#growthRefresh').disabled);
 const open=async()=>{await page.locator('#accountGrowth').click();await idle();};
 await open();assert.equal(posts.length,0,'opening executes tasks');assert.equal(await page.locator('#growthRunAll').isDisabled(),false);
 for(const el of await page.locator('[data-task-code="automation_1"]').all()){assert.equal(await el.getByRole('button',{name:'执行任务',exact:true}).count(),0);assert.match(await el.textContent(),/官方应用真实创建/);}
 await page.locator('#growthAcceptAll').click();await idle();assert.equal(posts.length,3);assert.equal(posts.filter(p=>p.kind==='run').length,0);assert.equal(peak,2);assert.equal(await page.locator('#growthAcceptAll').isDisabled(),true);
 await page.locator('#growthRunAll').click();await idle();assert.equal(posts.filter(p=>p.kind==='run').length,6);
 for(const id of ['a','b','c'])assert.deepEqual(posts.filter(p=>p.kind==='run'&&p.id===id).map(p=>p.task_code),['Model_chat_GLM5.2','chat_5']);
 assert.equal(await page.locator('#growthRunAll').isDisabled(),true);await page.locator('#growthClose').click();
 reset(true);posts=[];pending='b';await open();await page.locator('#growthRunAll').click();await idle();
 assert.equal(posts.filter(p=>p.id==='b').length,1);assert.equal(posts.length,5);assert.match(await page.locator('[data-account-id="b"]').textContent(),/不会重复调用/);
 assert.equal(await page.locator('[data-account-id="b"] [data-task-code="Model_chat_GLM5.2"]').getByRole('button',{name:'执行任务',exact:true}).isDisabled(),true);
 await page.locator('#growthClose').click();reset(true);posts=[];pending='';prerequisite='b';await open();await page.locator('#growthRunAll').click();await idle();assert.equal(posts.filter(p=>p.id==='b').length,1);assert.match(await page.locator('[data-account-id="b"]').textContent(),/领养第一只 Buddy/);await page.locator('#growthClose').click();reset(true);posts=[];prerequisite='';requestDelay=300;await open();await page.locator('#growthRunAll').click();
 await page.waitForFunction(()=>document.querySelector('#growthTaskList').textContent.includes('执行中'));await page.locator('#growthClose').click();await delay(450);assert.equal(posts.length,2,'closing started queued tasks');assert.equal(aborted,2,'run requests not canceled');
 requestDelay=30;await open();
 for(const theme of ['light','dark']){await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);for(const viewport of [{width:1440,height:1000},{width:390,height:844}]){
  await page.setViewportSize(viewport);assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);assert.equal(await page.locator('#growthDialog').evaluate(e=>e.scrollWidth>e.clientWidth),false);
  await page.screenshot({path:`artifacts/growth-actions-20261030/${theme}-${viewport.width}.png`});
 }}
 await page.locator('#growthClose').click();assert.deepEqual(errors,[]);
 const result={explicit_actions_only:true,bulk_enrollment_no_model_calls:true,real_action_allowlist:true,manual_instructions_visible:true,disabled_accounts_not_mutated:true,account_concurrency_peak:peak,same_account_serial:true,model_task_before_chat_deficit:true,pending_stops_account_queue:true,prerequisite_error_visible:true,pending_task_disabled:true,close_aborts_run_requests:true,close_stops_queued_tasks:true,light_dark_desktop_mobile:true,javascript_errors:errors};
 fs.writeFileSync('artifacts/growth-actions-20261030/browser.json',JSON.stringify(result,null,2)+'\n');console.log(JSON.stringify(result,null,2));
}finally{await browser.close();}})().catch(e=>{console.error(e.stack);process.exitCode=1;});
