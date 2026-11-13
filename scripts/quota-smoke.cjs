// Fixture-only UI checks; never reads real account credentials or provider quotas.
const {chromium}=require('playwright'),assert=require('node:assert/strict'),fs=require('node:fs');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});try{
 for(const locale of ['zh-CN','en-US']){
  const ctx=await browser.newContext({locale,viewport:{width:1500,height:1100}}),page=await ctx.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));let failed=false,requests=0;
  const now=Math.floor(Date.now()/1000);
  const accounts=[['qoder','Qoder quota fixture'],['kiro','Kiro quota fixture'],['chatgpt','GPT quota fixture'],['claude','Claude quota fixture'],['chatgpt','API key fixture'],['workbuddy','Legacy quota fixture'],['kiro','Failure quota fixture']].map(([provider,name],i)=>({id:'a'+i,provider,name,scope:provider==='kiro'?'us-east-1':provider==='qoder'||provider==='workbuddy'?'cn':'global',enabled:true,non_expiring:true,status:'ok'}));
  const metrics=[
   {available:true,kind:'credits',windows:[{id:'plan',name:'套餐额度',unit:'credits',total:300,used:100,remaining:200,utilization:100/3},{id:'addon',name:'附加额度',unit:'credits',total:50,used:2.5,remaining:47.5,utilization:5}]},
   {available:true,kind:'credits',package_name:'KIRO PRO',updated_at:now,windows:[{id:'plan',name:'套餐额度',unit:'credits',total:500,used:12.5,remaining:487.5,utilization:2.5,resets_at:now+86400}]},
   {available:true,kind:'windows',updated_at:now,balance:{amount:'12.345678901234567890',unit:'credits'},windows:[{id:'short',name:'5 小时窗口',window_seconds:18000,unit:'percent',utilization:80.25,resets_at:now+3600},{id:'week',name:'7 天窗口',window_seconds:604800,unit:'percent',utilization:35,resets_at:now+86400}]},
   {available:true,kind:'windows',windows:[{id:'short',name:'5 小时窗口',window_seconds:18000,unit:'percent',utilization:0,resets_at:now+7200},{id:'week',name:'7 天窗口',window_seconds:604800,unit:'percent',utilization:100,resets_at:now-1}]},
   {available:false,kind:'unsupported',reason:'OpenAI API Key 接入未提供订阅窗口查询，请在官方 API 控制台查看用量与账单'},
   {available:true,total:100,used:20,remaining:80}
  ];
  await ctx.route('**/setup/status',r=>r.fulfill({json:{ready:true,needs_setup:false}}));
 await ctx.route('**/auth/session',r=>r.fulfill({json:{authenticated:true,user:{id:'builtin:admin',username:'admin',role:'admin'}}}));
  await ctx.route('**/admin/api/**',r=>{const path=new URL(r.request().url()).pathname;let value={ok:true};
   if(path.endsWith('/state'))value={accounts,keys:[],models:[],settings:{pool:{}},stats:{}};
   if(path.endsWith('/credits')){requests++;value={accounts:accounts.map((a,i)=>i===6||failed&&i===2?{id:a.id,ok:false,error:i===6?'Kiro HTTP 403 fixture':'ChatGPT HTTP 429 fixture'}:{id:a.id,ok:true,credit:metrics[i]})};}
   return r.fulfill({json:value});
  });
  await ctx.route('**/health',r=>r.fulfill({json:{accounts:accounts.length,ready:accounts.length,models:0}}));
  await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10088')+'/#accounts');
  const row=name=>page.locator('#accounts tr[data-state]').filter({hasText:name});await row('GPT quota fixture').locator('[data-quota-window]').first().waitFor();
  assert.ok((await row('Qoder quota fixture').textContent()).includes('47.5'));
  assert.ok((await row('Kiro quota fixture').textContent()).includes('487.5'));assert.ok((await row('Kiro quota fixture').textContent()).includes('KIRO PRO'));
  assert.equal(await row('GPT quota fixture').locator('progress').count(),2);assert.equal(await row('GPT quota fixture').locator('progress').first().getAttribute('value'),'80.25');
  assert.ok((await row('GPT quota fixture').textContent()).includes('12.345678901234567890'));assert.equal(await page.locator('#accountDetailDialog').count(),0);
  assert.equal(await row('Claude quota fixture').locator('progress').first().getAttribute('value'),'0');assert.equal(await row('Legacy quota fixture').locator('progress').first().getAttribute('value'),'20');
  assert.ok((await row('Claude quota fixture').textContent()).includes(locale==='zh-CN'?'已到重置时间，请刷新':'Reset time reached'));
  assert.equal(await row('API key fixture').locator('progress').count(),0);assert.ok((await row('API key fixture').textContent()).includes(locale==='zh-CN'?'暂不支持查询':'Query not supported'));
  assert.ok((await row('Failure quota fixture').textContent()).includes('HTTP 403'));assert.ok((await row('Legacy quota fixture').textContent()).includes('80 / 100'));
  failed=true;await page.locator('#refreshCredits').click();await row('GPT quota fixture').getByText('ChatGPT HTTP 429 fixture',{exact:true}).waitFor();assert.equal(await row('GPT quota fixture').locator('progress').count(),0);
  failed=false;await page.locator('#refreshCredits').click();await row('GPT quota fixture').locator('[data-quota-window]').first().waitFor();assert.ok(requests>=3);
  fs.mkdirSync('artifacts',{recursive:true});await page.screenshot({path:'artifacts/quota-'+locale+'-desktop.png',fullPage:true});
  await page.setViewportSize({width:390,height:844});await page.locator('#consoleSidebar').waitFor({state:'hidden'});
  await row('GPT quota fixture').locator('[data-quota-window]').first().scrollIntoViewIfNeeded();
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  const bounds=await row('GPT quota fixture').locator('[data-quota-window]').first().boundingBox();assert.ok(bounds&&bounds.x>=0&&bounds.x+bounds.width<=390);
  await page.screenshot({path:'artifacts/quota-'+locale+'-mobile.png'});
  assert.deepEqual(errors,[]);await ctx.close();
 }
 console.log('PASS: provider credits and subscription windows, exact balances, zero vs unknown, reset times, query failures, refresh, legacy rendering, English/Chinese and mobile.');
}finally{await browser.close();}})().catch(e=>{console.error(e.stack);process.exitCode=1;});
