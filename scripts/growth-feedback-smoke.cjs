// Isolated browser regression: no real credentials, models or reward requests.
const {chromium}=require('playwright'),assert=require('node:assert/strict'),fs=require('node:fs');
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
(async()=>{
 const browser=await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL||'chrome',headless:true});
 try{
  const context=await browser.newContext({viewport:{width:1200,height:765}}),page=await context.newPage(),errors=[];
  page.on('pageerror',error=>errors.push(error.message));
  const account={id:'fixture',name:'WorkBuddy fixture',provider:'workbuddy',scope:'cn',enabled:true,growth_tasks_supported:true,status:'ok',pool:{state:'ready',in_flight:0,max_in_flight:2}};
  const task=(code,extra={})=>({task_code:code,title:code,description:'请在官方应用按任务说明完成操作。'.repeat(4),manual_instruction:'请按说明在官方应用完成',current:0,target:0,accept_status:'not_accepted',claimable:false,claimed:false,locked:false,...extra});
  const tasks=Array.from({length:17},(_,i)=>task('manual_'+i));
  tasks.splice(13,0,task('first_buddy',{title:'领养第一只 Buddy',manual_instruction:'请在官方应用阅读并同意领养协议，再领养 Buddy'}));
  tasks.push(task('chat_5',{title:'完成五次对话',auto_supported:true,action_description:'真实对话会消耗积分'}));
  let reads=0,accepts=0,runs=0;
  await context.route('**/admin/auth/session',route=>route.fulfill({json:{authenticated:true}}));
  await context.route('**/health',route=>route.fulfill({json:{accounts:1,ready:1}}));
  await context.route('**/admin/api/**',async route=>{
   const request=route.request(),path=new URL(request.url()).pathname;
   if(path.endsWith('/state'))return route.fulfill({json:{accounts:[account],models:[],keys:[],groups:[],settings:{},stats:{}}});
   if(path.endsWith('/credits'))return route.fulfill({json:{accounts:[]}});
   if(path==='/admin/api/workbuddy/tasks'){reads++;return route.fulfill({json:{id:account.id,tasks}});}
   if(path.endsWith('/tasks/accept')){
    accepts++;assert.ok(request.postDataJSON().task_codes.includes('manual_0'));
    // A batch can partially succeed even when its envelope is returned as 409.
    tasks[0].accept_status='accepted';tasks[0].target=1;
    await delay(100);
    return route.fulfill({status:409,json:{error:{message:'请先在官方应用完成「领养第一只 Buddy」，再接取此任务'}}});
   }
   if(path.endsWith('/tasks/run')){
    runs++;assert.equal(request.postDataJSON().task_code,'chat_5');await delay(150);
    return route.fulfill({status:409,json:{error:{message:'请先在官方应用完成「领养第一只 Buddy」，再接取此任务'}}});
   }
   if(path.endsWith('/tasks/claim'))assert.fail('No eligible reward may be claimed');
   return route.fulfill({json:{ok:true}});
  });
  await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:19091')+'/#accounts');
  await page.locator('#accounts tr[data-state]').first().waitFor();
  await page.locator('#accountProvider').selectOption('workbuddy');await page.locator('#accountGrowth').click();
  const idle=()=>page.waitForFunction(()=>!document.querySelector('#growthRefresh').disabled);
  await idle();assert.equal(reads,1);assert.equal(accepts+runs,0);
  await page.locator('[data-task-code="chat_5"]').scrollIntoViewIfNeeded();
  await page.locator('#growthRunAll').click();await idle();assert.equal(runs,1);
  async function visibleError(){
   const visible=await page.locator('#growthError').evaluate(element=>{
    const r=element.getBoundingClientRect(),dialog=element.closest('dialog').getBoundingClientRect();
    const center=document.elementFromPoint(r.x+r.width/2,r.y+r.height/2);
    return r.top>=Math.max(0,dialog.top)&&r.bottom<=Math.min(innerHeight,dialog.bottom)&&!!center&&(element===center||element.contains(center));
   });
   assert.equal(visible,true,'operation error scrolled out of view or covered by footer');
  }
  await visibleError();assert.match(await page.locator('#growthError').textContent(),/领养第一只 Buddy/);
  assert.match(await page.locator('[data-task-code="chat_5"]').textContent(),/操作未完成/);
  await page.locator('#growthRefresh').click();await idle();
  assert.match(await page.locator('#growthNotice').textContent(),/刷新完成/);
  assert.match(await page.locator('#growthRunAll').textContent(),/对话任务/);
  assert.equal(await page.locator('.growth-task').first().getAttribute('data-task-code'),'first_buddy');
  assert.equal(await page.locator('#growthClaimAll').isDisabled(),true);
  assert.match(await page.locator('#growthClaimAll').getAttribute('title'),/没有可领取/);
  const before=reads;await page.locator('#growthAcceptAll').click();await idle();
  assert.equal(accepts,1);assert.equal(reads,before+1,'batch error must read back partial upstream results');
  const accepted=page.locator('[data-task-code="manual_0"]');
  assert.equal(await accepted.getByRole('button',{name:'接取任务',exact:true}).count(),0,'confirmed enrollment still looks unaccepted');
  assert.doesNotMatch(await accepted.textContent(),/待刷新确认|操作未完成/);
  assert.match(await page.locator('#growthNotice').textContent(),/已确认 1 项/);
  const output=process.env.GROWTH_FEEDBACK_OUTPUT||'artifacts/growth-feedback-20261030';fs.mkdirSync(output,{recursive:true});
  for(const theme of ['light','dark']){
   await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);
   for(const viewport of [{width:1200,height:765},{width:390,height:844},{width:1200,height:500}]){
    await page.setViewportSize(viewport);await page.locator('[data-task-code="chat_5"]').scrollIntoViewIfNeeded();
    await visibleError();assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
    assert.equal(await page.locator('#growthDialog').evaluate(element=>element.scrollWidth>element.clientWidth),false);
    await page.screenshot({path:`${output}/${theme}-${viewport.width}-${viewport.height}.png`});
   }
  }
  assert.deepEqual(errors,[]);
  const result={error_visible_after_long_list_scroll:true,refresh_result_visible:true,partial_enrollment_read_back:true,confirmed_enrollment_not_blocked:true,first_buddy_guidance_first:true,dialog_task_scope_explicit:true,no_ineligible_claims:true,light_dark_desktop_mobile_short_viewport:true,javascript_errors:errors};
  fs.writeFileSync(`${output}/browser.json`,JSON.stringify(result,null,2)+'\n');console.log(JSON.stringify(result,null,2));
 }finally{await browser.close();}
})().catch(error=>{console.error(error.stack);process.exitCode=1;});
