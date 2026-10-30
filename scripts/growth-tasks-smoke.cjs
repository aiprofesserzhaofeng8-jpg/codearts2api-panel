// Browser fixtures only: no real accounts, task execution, or reward claims.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const fs=require('node:fs');
(async()=>{
 const browser=await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL||'chrome',headless:true});
 try{
  const ctx=await browser.newContext({viewport:{width:1440,height:1000}}),page=await ctx.newPage();
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  const accounts=[
   {id:'wb-a',name:'WorkBuddy A',provider:'workbuddy',growth_tasks_supported:true},
   {id:'wb-b',name:'WorkBuddy B',provider:'workbuddy',growth_tasks_supported:true},
   {id:'trae',name:'Trae fixture',provider:'trae'},
   {id:'codearts',name:'CodeArts fixture',provider:'codearts'}
  ].map(a=>({...a,scope:'cn',enabled:true,status:'ok',pool:{state:'ready',in_flight:0,max_in_flight:2}}));
  const task=(code,extra={})=>({task_code:code,title:code,description:'根据任务说明完成对应操作',credit:100,energy:5,current:1,target:1,claimable:true,claimed:false,locked:false,...extra});
  const tasks={'wb-a':[task('ready',{title:'完成对话'}),task('incomplete',{current:1,target:5,claimable:false}),task('locked',{locked:true,claimable:false}),task('claimed',{claimed:true,claimable:false}),task('text',{title:'<img src=x onerror=alert(1)>',description:'很长的操作说明'.repeat(15),claimable:false})], 'wb-b':[task('account-b',{title:'B 账号任务',claimable:false})]};
  let reads=0,claims=0,pending=false,failRead=false,held=null,holdA=false;
  await ctx.route('**/admin/auth/session',r=>r.fulfill({json:{authenticated:true}}));
  await ctx.route('**/admin/api/**',async route=>{
   const req=route.request(),u=new URL(req.url());let out={ok:true};
   if(u.pathname.endsWith('/state'))out={accounts,keys:[],groups:[],models:[],settings:{pool:{max_in_flight:2}},stats:{}};
   if(u.pathname.endsWith('/credits'))out={accounts:[]};
   if(u.pathname==='/admin/api/workbuddy/tasks'){
    reads++;const id=u.searchParams.get('id');assert.ok(tasks[id]);
    if(holdA&&id==='wb-a')await new Promise(resolve=>{held=resolve;});
    if(failRead)return route.fulfill({status:502,json:{error:{message:'任务查询暂不可用'}}});
    out={ok:true,id,tasks:tasks[id]};
   }
   if(u.pathname==='/admin/api/workbuddy/tasks/claim'){
    claims++;const b=req.postDataJSON();assert.equal(b.id,'wb-a');assert.equal(b.task_code,'ready');
    if(!pending){tasks['wb-a'][0].claimed=true;tasks['wb-a'][0].claimable=false;}
    out={ok:true,id:b.id,tasks:tasks[b.id],state:pending?'pending':'claimed',message:pending?'领取已提交，等待上游确认':'奖励已确认',...(pending?{}:{balance:{available:true,remaining:150,total:150,used:0}})};
   }
   return route.fulfill({json:out});
  });
  await ctx.route('**/health',r=>r.fulfill({json:{accounts:4,ready:4}}));
  await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:19088')+'/#accounts');
  await page.locator('#accounts tr[data-state]').first().waitFor();assert.equal(reads,0);
  async function menu(name){await page.locator('#accounts tr').filter({hasText:name}).getByRole('button',{name:'更多',exact:true}).click();}
  async function open(name){await menu(name);await page.locator('#accountActionMenu').getByRole('button',{name:'成长任务',exact:true}).click();await page.locator('#growthDialog').waitFor({state:'visible'});}
  for(const name of ['Trae fixture','CodeArts fixture']){await menu(name);assert.equal(await page.locator('#accountActionMenu').getByRole('button',{name:'成长任务',exact:true}).count(),0);await page.keyboard.press('Escape');}
  await open('WorkBuddy A');await page.locator('#growthDialog .growth-task').first().waitFor();
  assert.equal(reads,1);assert.equal(claims,0);assert.equal(await page.locator('#growthDialog img').count(),0);
  assert.equal(await page.locator('#growthDialog .growth-task button:enabled').count(),1);
  await page.locator('#growthClaimAll').click();await page.waitForFunction(()=>document.querySelector('#growthTaskList [data-task-code="ready"]').textContent.includes('已领取'));
  assert.equal(claims,1);assert.equal(await page.locator('#growthClaimAll').isDisabled(),true);
  assert.match(await page.locator('#accounts').textContent(),/150/);
  await page.locator('#growthClose').click();
  tasks['wb-a'][0].claimed=false;tasks['wb-a'][0].claimable=true;pending=true;
  await open('WorkBuddy A');await page.locator('#growthClaimAll').waitFor({state:'visible'});
  await page.waitForFunction(()=>!document.querySelector('#growthClaimAll').disabled);await page.locator('#growthClaimAll').click();
  await page.waitForFunction(()=>document.querySelector('#growthError').textContent.includes('等待上游确认'));
  assert.equal(claims,2);assert.equal(await page.locator('#growthClaimAll').isDisabled(),true);
  assert.match(await page.locator('#growthTaskList [data-task-code="ready"]').textContent(),/待刷新确认/);
  await page.locator('#growthRefresh').click();await page.waitForFunction(()=>!document.querySelector('#growthClaimAll').disabled);
  failRead=true;await page.locator('#growthRefresh').click();await page.waitForFunction(()=>document.querySelector('#growthError').textContent.includes('查询失败'));
  assert.equal(await page.locator('#growthDialog .growth-task').count(),0);assert.equal(await page.locator('#growthClaimAll').isDisabled(),true);
  failRead=false;await page.locator('#growthClose').click();
  holdA=true;await open('WorkBuddy A');while(!held)await new Promise(r=>setTimeout(r,20));
  await page.locator('#growthClose').click();await open('WorkBuddy B');await page.getByText('B 账号任务',{exact:true}).waitFor();
  held();holdA=false;await page.waitForTimeout(150);
  assert.equal(await page.locator('#growthAccountName').textContent(),'WorkBuddy B');assert.equal(await page.locator('#growthDialog .growth-task').count(),1);
  await page.locator('#growthClose').click();await open('WorkBuddy A');await page.locator('#growthDialog .growth-task').first().waitFor();
  fs.mkdirSync('artifacts/growth-tasks-20261030',{recursive:true});
  for(const light of [false,true]){
   await page.evaluate(light=>document.documentElement.dataset.theme=light?'light':'dark',light);
   await page.setViewportSize({width:1440,height:1000});await page.screenshot({path:`artifacts/growth-tasks-20261030/tasks-${light?'light':'dark'}-desktop.png`});
   await page.setViewportSize({width:390,height:844});
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
   assert.equal(await page.locator('#growthDialog').evaluate(e=>e.scrollWidth>e.clientWidth),false);
   await page.screenshot({path:`artifacts/growth-tasks-20261030/tasks-${light?'light':'dark'}-mobile.png`});
  }
  await page.locator('#growthClose').click();const stopped=reads;await page.waitForTimeout(200);assert.equal(reads,stopped);
  assert.deepEqual(errors,[]);
  console.log('PASS: provider isolation; explicit reads; eligible-only/bulk claims; confirmed balance; pending protection; query failure; late-response account isolation; text escaping; dark/light desktop/mobile; no background polling.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
