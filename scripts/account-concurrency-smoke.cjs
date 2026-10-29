// Isolated concurrency-setting fixtures; no real credentials or model requests.
const {chromium}=require('playwright');const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({channel:'chrome',headless:true});
 try{
  const ctx=await browser.newContext({viewport:{width:1460,height:1000}}),page=await ctx.newPage();
  const accounts=['codearts','workbuddy','trae'].map(provider=>({id:provider,name:provider+' fixture',provider,scope:'cn',enabled:true,status:'ok',max_in_flight:0}));
  let saves=0,fail=false;const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await ctx.route('**/admin/auth/session',r=>r.fulfill({json:{authenticated:true}}));
  await ctx.route('**/admin/api/**',route=>{
   const req=route.request(),u=new URL(req.url());let out={ok:true};
   if(u.pathname.endsWith('/state'))out={accounts:accounts.map(a=>({...a,pool:{state:'ready',in_flight:0,max_in_flight:a.max_in_flight||2}})),keys:[],models:[],settings:{pool:{max_in_flight:2}},stats:{}};
   if(u.pathname.endsWith('/credits'))out={accounts:[]};
   if(u.pathname.endsWith('/accounts')){
    const b=req.postDataJSON();assert.equal(b.action,'concurrency');saves++;
    if(fail)return route.fulfill({status:500,json:{error:{message:'模拟状态保存失败'}}});
    const a=accounts.find(a=>a.id===b.id);assert.ok(a);a.max_in_flight=b.max_in_flight;
    out={ok:true,id:a.id,max_in_flight:a.max_in_flight,pool:{max_in_flight:a.max_in_flight||2}};
   }
   return route.fulfill({json:out});
  });
  await ctx.route('**/health',r=>r.fulfill({json:{accounts:3,ready:3}}));
  await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10088')+'/#accounts');await page.locator('#accounts tr[data-state]').first().waitFor();
  async function open(id){await page.locator('#accounts tr').filter({hasText:id+' fixture'}).getByRole('button',{name:'设置',exact:true}).click();await page.locator('#accountActionMenu').getByRole('button',{name:'并发设置',exact:true}).click();await page.locator('#accountConcurrencyDialog').waitFor({state:'visible'});}
  for(const id of ['codearts','workbuddy','trae']){await open(id);assert.match(await page.locator('#accountConcurrencyName').textContent(),new RegExp(id));assert.equal(await page.locator('#accountConcurrencyDefault').isChecked(),true);await page.locator('#accountConcurrencyCancel').click();}
  assert.equal(saves,0);
  await open('workbuddy');await page.locator('#accountConcurrencyDefault').uncheck();await page.locator('#accountConcurrencyLimit').fill('0');await page.locator('#accountConcurrencySave').click();
  await page.waitForFunction(()=>document.querySelector('#accountConcurrencyError').textContent.includes('整数'));assert.equal(saves,0);
  await page.locator('#accountConcurrencyLimit').fill('3');fail=true;await page.locator('#accountConcurrencySave').click();
  await page.waitForFunction(()=>document.querySelector('#accountConcurrencyError').textContent.includes('模拟状态保存失败'));
  assert.equal(await page.locator('#accountConcurrencyLimit').inputValue(),'3');assert.equal(accounts[1].max_in_flight,0);
  fail=false;await page.locator('#accountConcurrencySave').click();await page.locator('#accountConcurrencyDialog').waitFor({state:'hidden'});
  await page.waitForFunction(()=>document.querySelector('#accounts').textContent.includes('在途 0 / 3'));
  assert.equal(accounts[1].max_in_flight,3);assert.equal(accounts[0].max_in_flight,0);assert.equal(accounts[2].max_in_flight,0);
  await page.reload();await page.locator('#accounts tr[data-state]').first().waitFor();await open('workbuddy');
  assert.equal(await page.locator('#accountConcurrencyDefault').isChecked(),false);assert.equal(await page.locator('#accountConcurrencyLimit').inputValue(),'3');
  await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  await page.screenshot({path:'artifacts/account-concurrency-mobile.png'});
  await page.locator('#accountConcurrencyDefault').check();await page.locator('#accountConcurrencySave').click();await page.locator('#accountConcurrencyDialog').waitFor({state:'hidden'});
  assert.equal(accounts[1].max_in_flight,0);assert.deepEqual(errors,[]);
  console.log('PASS: per-account concurrency for all providers, inheritance, validation, cancel, failed-save retention/retry, account isolation, reload and mobile.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
