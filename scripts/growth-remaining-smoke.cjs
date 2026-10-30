// All APIs intercepted: verify the remaining-task UI without upstream actions.
const {chromium}=require('playwright'),assert=require('node:assert/strict'),fs=require('node:fs');
(async()=>{const browser=await chromium.launch({channel:'chrome',headless:true});try{
 const context=await browser.newContext(),page=await context.newPage(),errors=[],posts=[];
 page.on('pageerror',e=>errors.push(e.message));
 const account={id:'fixture',provider:'workbuddy',scope:'cn',enabled:true,name:'WorkBuddy',growth_tasks_supported:true,pool:{state:'ready'}};
 const tasks=[{task_code:'expert_5',target:5,auto_supported:true,action_description:'真实对话，会消耗积分'},{task_code:'template_5',target:5,auto_supported:true,activity_only:true},{task_code:'wb_wechat_oa_subscribe_task',target:1,manual_instruction:'需要本人关注公众号满 24 小时'},{task_code:'Library_read',target:1,auto_supported:true,activity_only:true}].map(t=>({...t,current:0,accept_status:'accepted',claimed:false,claimable:false,title:t.task_code}));
 await context.route('**/admin/auth/session',r=>r.fulfill({json:{authenticated:true}}));
 await context.route('**/health',r=>r.fulfill({json:{accounts:1,ready:1}}));
 await context.route('**/admin/api/**',async route=>{
  const url=new URL(route.request().url());let result={ok:true};
  if(url.pathname.endsWith('/state'))result={accounts:[account],settings:{},models:[],keys:[],groups:[],stats:{}};
  if(url.pathname.endsWith('/credits'))result={accounts:[]};
  if(url.pathname==='/admin/api/workbuddy/tasks')result={tasks};
  if(url.pathname.endsWith('/tasks/run')){const body=route.request().postDataJSON();posts.push(body.task_code);const t=tasks.find(t=>t.task_code===body.task_code);assert.ok(t.auto_supported);t.claimed=true;t.current=t.target;t.accept_status='claimed';result={state:'claimed',tasks};}
  await route.fulfill({json:result});
 });
 await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:19092')+'/#accounts');await page.locator('#accounts tr').first().waitFor();await page.locator('#accountProvider').selectOption('workbuddy');await page.locator('#accountGrowth').click();await page.waitForFunction(()=>!document.querySelector('#growthRefresh').disabled);
 const manual=page.locator('[data-task-code="wb_wechat_oa_subscribe_task"]');assert.match(await manual.textContent(),/本人关注公众号满 24 小时/);assert.equal(await manual.getByRole('button',{name:'执行任务',exact:true}).count(),0);
 await page.locator('#growthRunAll').click();await page.waitForFunction(()=>!document.querySelector('#growthRefresh').disabled);
 assert.deepEqual(posts,['template_5','Library_read','expert_5']);assert.deepEqual(errors,[]);
 const result={events_before_billable_experts:true,manual_follow_not_faked:true,existing_growth_dialog_preserved:true,javascript_errors:errors};
 const dir=process.env.GROWTH_REMAINING_OUTPUT||'artifacts/growth-remaining-20261031';fs.mkdirSync(dir,{recursive:true});fs.writeFileSync(dir+'/browser-remaining.json',JSON.stringify(result,null,2)+'\n');console.log(JSON.stringify(result,null,2));
}finally{await browser.close();}})().catch(e=>{console.error(e.stack);process.exitCode=1;});
