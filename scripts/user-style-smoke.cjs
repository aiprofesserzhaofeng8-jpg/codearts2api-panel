// Isolated real authentication. Read fixtures exercise populated visual states;
// users-smoke and consumer-entitlements-smoke cover actual storage mutations.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const fs=require('node:fs');
(async()=>{
 const base=process.env.CODEARTS_TEST_BASE,password=process.env.CODEARTS_TEST_ADMIN_PASSWORD;
 assert(base&&password,'Run with scripts/frontend_e2e.py');
 const out=process.env.CODEARTS_USER_STYLE_ARTIFACTS||'artifacts/user-style/browser';fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});
 try{
  const admin=await browser.newContext({locale:'zh-CN'}),context=await browser.newContext({locale:'zh-CN',viewport:{width:1440,height:1000},permissions:['clipboard-read','clipboard-write']});
  assert.equal((await admin.request.post(base+'/auth/login',{data:{username:'admin',password}})).status(),200);
  let response=await admin.request.post(base+'/admin/api/groups',{data:{name:'Standard',account_ids:[],model_ids:[]}});
  assert.equal(response.status(),200);const group=(await response.json()).group;
  response=await admin.request.post(base+'/admin/api/users',{data:{username:'style-viewer',password:'style-viewer-test-password',group_id:group.id,max_concurrency:20}});
  assert.equal(response.status(),200);
  assert.equal((await context.request.post(base+'/auth/login',{data:{username:'style-viewer',password:'style-viewer-test-password'}})).status(),200);
  const actual=await (await context.request.get(base+'/user/api/profile')).json();
  const now=Math.floor(Date.now()/1000);
  const user={...actual.user,display_name:'Alex Chen',email:'alex.chen@example.test',email_verified:true,wallet:{...actual.user.wallet,enabled:true,balance:9749000000},budget:{max_requests:10000,max_tokens:5000000,max_usd:100},budget_usage:{...actual.user.budget_usage,requests:328,tokens:428600,usd:3.1294}};
  const stats={total_requests:328,failed_requests:2,input_tokens:348600,output_tokens:80000,cached_tokens:124800,reasoning_tokens:12000,usage_requests:328,priced_usd_requests:328,total_latency_ms:501840,latency_samples:328,partial_usage_requests:0,estimated_usd:3.1294};
  const models=['deepseek-4.1-flash','deepseek-4-pro','glm-5.2','kimi-k2.5','minimax-m2.7','qwen3.6-plus'].map(id=>({id,name:id}));
  const logs=models.slice(0,5).map((m,i)=>({time:now-i*1800,model:m.id,status:i===4?429:200,request_id:'visual-'+i,latency_ms:1200+i*97,usage:{known:true,input_tokens:2400+i*100,output_tokens:480},estimated_cost:{amount:.0048+i*.001}}));
  const usage={logs,stats,page:1,pages:1,total:328,retention_limit:10000,analysis:{time_zone:'Asia/Shanghai',bucket_days:1,models:models.slice(0,5).map((m,i)=>({name:m.id,stats:{...stats,total_requests:[140,84,52,32,20][i],input_tokens:100000-i*17000,output_tokens:20000-i*3000,estimated_usd:[1.15,.8,.6,.35,.2294][i]}})),trend:[18,42,35,56,40,73,64].map((n,i)=>({time:now-(6-i)*86400,stats:{...stats,total_requests:n}}))}};
  const rights={wallet:user.wallet,reserved:1200000,subscription:actual.user.subscription,redemptions:[{id:'redemption-visual',kind:'balance',used_at:now-86400,hint:'redeem-••••-8f2a',amount:1000000000}],entries:[{id:'entry-visual',kind:'redeem',time:now-86400,delta:1000000000,balance:9749000000,note:'兑换额度'}],retention_limit:10000};
  await context.route('**/user/api/**',async route=>{
   const path=new URL(route.request().url()).pathname;
   if(route.request().method()!=='GET')return route.continue();
   const fixture=path==='/user/api/profile'?{...actual,user,group_name:'Standard'}:path==='/user/api/keys'?{keys:[{ref:'visual-key',key:'sk-ca-••••8f2a',name:'开发客户端',enabled:true,created_at:now-86400,last_used:now-1800,requests:328,budget:{},budget_usage:{requests:328}}]}:path==='/user/api/models'?{models}:path==='/user/api/usage'?usage:path==='/user/api/entitlements'?rights:path==='/user/api/identity'?{identities:[]}:null;
   if(fixture)return route.fulfill({json:fixture});return route.continue();
  });
  const page=await context.newPage(),errors=[],adminRequests=[];
  page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>{if(new URL(r.url()).pathname.startsWith('/admin/'))adminRequests.push(r.url());});
  page.on('console',m=>{if(m.type()==='error'&&/Content Security|Refused to/.test(m.text()))errors.push(m.text());});
  await page.goto(base+'/user');await page.locator('#userDashboard').waitFor();await page.locator('.portal-welcome').getByText('你好，Alex Chen').waitFor();
  const pages=['dashboard','keys','models','usage','entitlements','identity','profile','password'];
  const checks=[];
  async function navigate(id){if(await page.locator('#consoleMenuToggle').isVisible())await page.locator('#consoleMenuToggle').click();await page.locator('[data-user-tab="'+id+'"]').click();await page.waitForLoadState('networkidle');if(id==='entitlements'){await page.locator('#userEntitlements .portal-stats').waitFor();await page.getByText('redeem-••••-8f2a',{exact:true}).waitFor();}if(id==='identity')await page.locator('#userIdentity .portal-binding').first().waitFor();}
  for(const width of [1440,390]){
   await page.setViewportSize({width,height:width===1440?1000:844});
   for(const theme of ['light','dark']){
    await page.evaluate(value=>document.documentElement.dataset.theme=value,theme);
    for(const id of pages){
     await navigate(id);
     assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,id+' overflow '+width+' '+theme);
     assert.equal(await page.getByText('Console disposed',{exact:true}).count(),0);
     if(id==='profile'&&width===1440){const bounds=await page.locator('.user-main').boundingBox();assert(bounds.width<=950.1,'profile is centered and bounded');}
     if(!['dashboard','profile'].includes(id))assert.equal(await page.locator('.portal-account,.portal-budget').count(),0,'no repeated account hero/budget');
     await page.screenshot({path:out+'/'+id+'-'+theme+'-'+width+'.png',fullPage:true,animations:'disabled'});checks.push({page:id,theme,width,passed:true});
    }
   }
  }
  await page.setViewportSize({width:1440,height:1000});await navigate('models');
  await page.getByRole('searchbox',{name:'搜索模型'}).fill('deepseek');assert.equal(await page.locator('.portal-model-card').count(),2);
  await page.getByRole('button',{name:'复制模型名 deepseek-4.1-flash',exact:true}).click();assert.equal(await page.evaluate(()=>navigator.clipboard.readText()),'deepseek-4.1-flash');
  await page.getByRole('searchbox',{name:'搜索模型'}).fill('no-model-found');await page.getByText('没有匹配的模型',{exact:true}).waitFor();
  await navigate('keys');await page.getByRole('button',{name:'复制 API 地址',exact:true}).click();assert.equal(await page.evaluate(()=>navigator.clipboard.readText()),base+'/v1');
  await navigate('entitlements');await page.getByRole('button',{name:'额度流水',exact:true}).click();await page.getByText('兑换额度',{exact:true}).waitFor();
  await navigate('profile');user.display_name='很长的用户昵称用于验证页面布局与换行不会挤压表单和其他操作按钮';await page.locator('#userRefresh').click();await page.locator('.portal-account h2').getByText(user.display_name,{exact:true}).waitFor();
  for(const id of pages){await page.setViewportSize({width:320,height:740});await navigate(id);assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,id+' 320px overflow');}
  await navigate('dashboard');assert.equal(await page.getByRole('img',{name:'每日请求趋势',exact:true}).count(),1);
  assert.deepEqual(adminRequests,[]);assert.deepEqual(errors,[]);assert.equal((await context.request.get(base+'/admin/api/state')).status(),403);
  fs.writeFileSync(out+'/checks.json',JSON.stringify({visual_states:checks,passed:true,search_and_copy:true,ledger_tab:true,long_name_and_320px_overflow:false,admin_requests:0,page_and_csp_errors:0,real_authentication:true,populated_read_fixtures:true,real_provider_requests:0},null,2)+'\n');
  console.log('PASS 32 populated user visual states, 8 pages at 320px, long names, bounded profile, model search/copy, endpoint copy, ledger tab, role isolation and CSP; read fixtures only, no external calls.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
