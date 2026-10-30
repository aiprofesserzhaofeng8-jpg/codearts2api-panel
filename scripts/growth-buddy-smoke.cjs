// Browser fixtures only. Every API is intercepted; no real accounts or credits.
const {chromium}=require('playwright'),assert=require('node:assert/strict'),fs=require('node:fs');
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
(async()=>{const browser=await chromium.launch({channel:'chrome',headless:true});try{
 const context=await browser.newContext({viewport:{width:1440,height:1000}}),page=await context.newPage(),errors=[];
 page.on('pageerror',error=>errors.push(error.message));
 const accounts=['a','b'].map(id=>({id,name:'WorkBuddy '+id,provider:'workbuddy',scope:'cn',enabled:true,growth_tasks_supported:true,status:'ok',pool:{state:'ready',in_flight:0,max_in_flight:2}}));
 let states={},posts=[],accepts=[],active=0,peak=0,pending='',perAccount={};
 function reset(agreed=false){for(const a of accounts)states[a.id]={buddy:{owned:false,agreed},tasks:['first_buddy','chat_5'].map(code=>({task_code:code,title:code,auto_supported:true,activity_only:true,accept_not_required:code==='first_buddy',action_description:'活动上报、核对协议与领养进度；不调用模型',accept_status:'not_accepted',target:0,current:0,claimed:false,claimable:false,locked:false}))};}
 reset();
 await context.route('**/admin/auth/session',route=>route.fulfill({json:{authenticated:true}}));await context.route('**/health',route=>route.fulfill({json:{accounts:2,ready:2}}));
 await context.route('**/admin/api/**',async route=>{
  const request=route.request(),url=new URL(request.url());let out={ok:true};
  if(url.pathname.endsWith('/state'))out={accounts,models:[],keys:[],groups:[],settings:{},stats:{}};
  if(url.pathname.endsWith('/credits'))out={accounts:[]};
  if(url.pathname==='/admin/api/workbuddy/tasks')out={id:url.searchParams.get('id'),...states[url.searchParams.get('id')]};
  if(url.pathname.endsWith('/tasks/accept')){
   const body=request.postDataJSON();accepts.push(body);assert.deepEqual(body.task_codes,['chat_5']);
   return route.fulfill({status:409,json:{error:{message:'请先执行「领养第一只 Buddy」完成前置条件，再接取此任务'}}});
  }
  if(url.pathname.endsWith('/tasks/run')){
   const body=request.postDataJSON();assert.ok(['a','b'].includes(body.id));const state=states[body.id];posts.push(body);
   active++;peak=Math.max(peak,active);perAccount[body.id]=(perAccount[body.id]||0)+1;assert.equal(perAccount[body.id],1);await delay(60);active--;perAccount[body.id]--;
   const t=state.tasks.find(t=>t.task_code===body.task_code);
   if(body.task_code==='first_buddy'){
    assert.ok(body.agree_buddy||state.buddy.agreed,'unconfirmed agreement submitted');state.buddy.agreed=true;
    if(body.id===pending){t.execution_pending=true;out={state:'pending',message:'活动上报已提交，领养进度待确认',...state};}
    else{state.buddy.owned=true;out={state:'prepared',message:'已确认领养，奖励以任务进度为准',...state};}
   }else{
    assert.equal(body.task_code,'chat_5');assert.equal(state.buddy.owned,true,'dependent task ran before adoption');t.claimed=true;t.accept_status='claimed';t.current=t.target=5;out={state:'claimed',...state};
   }
  }
  return route.fulfill({json:out});
 });
 await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:19092')+'/#accounts');await page.locator('#accounts tr').first().waitFor();await page.locator('#accountProvider').selectOption('workbuddy');
 const idle=()=>page.waitForFunction(()=>!document.querySelector('#growthRefresh').disabled);
 const open=async()=>{await page.locator('#accountGrowth').click();await idle();};
 await open();assert.match(await page.locator('#growthRunAll').getAttribute('title'),/活跃和领养不调用模型/);assert.equal(posts.length,0);assert.equal(await page.locator('#growthBuddyAgree').isChecked(),false);assert.equal(await page.locator('#growthBuddyConsent').isVisible(),true);
 assert.equal(await page.locator('[data-task-code="first_buddy"]').getByRole('button',{name:'接取任务',exact:true}).count(),0);
 assert.doesNotMatch(await page.locator('[data-task-code="first_buddy"]').first().textContent(),/未报名/);
 await page.locator('#growthAcceptAll').click();await idle();assert.equal(accepts.length,2);assert.equal(posts.length,0);
 assert.equal(await page.locator('#growthRunAll').isDisabled(),false);
 for(const id of ['a','b'])assert.equal(await page.locator(`[data-account-id="${id}"] [data-task-code="chat_5"]`).getByRole('button',{name:'执行任务',exact:true}).isDisabled(),false,'enrollment refusal must not block actual execution');
 await page.locator('#growthRunAll').click();assert.equal(posts.length,0);assert.match(await page.locator('#growthError').textContent(),/勾选同意/);
 await page.locator('#growthBuddyAgree').check();await page.locator('#growthRunAll').click();await idle();
 assert.equal(posts.length,4);assert.equal(peak,2);for(const id of ['a','b'])assert.deepEqual(posts.filter(p=>p.id===id).map(p=>p.task_code),['first_buddy','chat_5']);
 assert.equal(await page.locator('#growthRunAll').isDisabled(),true);assert.equal(await page.locator('#growthBuddyConsent').isVisible(),false);assert.match(await page.locator('[data-account-id="a"] [data-task-code="first_buddy"]').textContent(),/已领养/);
 await page.locator('#growthClose').click();reset(true);posts=[];pending='b';await open();assert.equal(await page.locator('#growthBuddyConsent').isVisible(),false);assert.equal(await page.locator('#growthBuddyAgree').isChecked(),false);
 await page.locator('#growthRunAll').click();await idle();assert.equal(posts.length,3);assert.equal(posts.filter(p=>p.id==='b').length,1);assert.match(await page.locator('#growthError').textContent(),/领养进度待确认/);
 pending='';await page.locator('#growthRefresh').click();await idle();await page.locator('#growthRunAll').click();await idle();assert.deepEqual(posts.filter(p=>p.id==='b').map(p=>p.task_code),['first_buddy','first_buddy','chat_5']);
 await page.locator('#growthClose').click();reset();posts=[];await open();
 const dir=process.env.GROWTH_BUDDY_OUTPUT||'artifacts/growth-buddy-20261030';fs.mkdirSync(dir,{recursive:true});
 for(const theme of ['light','dark']){await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);for(const viewport of [{width:1440,height:1000},{width:390,height:844},{width:1200,height:500}]){
  await page.setViewportSize(viewport);assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);assert.equal(await page.locator('#growthDialog').evaluate(e=>e.scrollWidth>e.clientWidth),false);assert.equal(await page.locator('#growthBuddyAgree').isVisible(),true);await page.screenshot({path:`${dir}/buddy-${theme}-${viewport.width}-${viewport.height}.png`});
 }}
 assert.deepEqual(errors,[]);assert.equal(posts.length,0);
 const result={no_enrollment_for_automatic_buddy_task:true,enrollment_failure_does_not_disable_execution:true,explicit_consent_before_execution:true,dependency_order_before_chat:true,confirmed_ownership_allows_dependents:true,reward_not_invented_by_prepared_state:true,account_concurrency_peak:peak,same_account_serial:true,account_failure_isolated:true,pending_onboarding_can_resume:true,existing_agreement_respected:true,consent_reset_on_reopen:true,light_dark_mobile_short_viewport:true,javascript_errors:errors};fs.writeFileSync(dir+'/browser.json',JSON.stringify(result,null,2)+'\n');console.log(JSON.stringify(result,null,2));
}finally{await browser.close();}})().catch(error=>{console.error(error.stack);process.exitCode=1;});
