// Real loopback connector and production panel UI. Login results and upstream
// account verification are fixtures; no downloads or result files are needed.
const {chromium}=require('playwright'),assert=require('node:assert/strict');
(async()=>{
 const {startBridge}=await import('./teleagent-login.mjs');
 const base=process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10088',secret='K'.repeat(43);
 const token='tenant:'+Buffer.from('{"alg":"HS256"}').toString('base64url')+'.'+Buffer.from(JSON.stringify({accountId:'fixture-user',exp:2000000000})).toString('base64url')+'.fixture-signature';
 let pendingMode=false,canceled=0,captures=0;
 let bridge;
 const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});
 try{for(const [locale,visit] of [['zh-CN',base],['en-US',base],['en-US','https://panel.fixture.invalid']]){
 bridge=await startBridge({origin:new URL(visit).origin,secret,port:0,login:async(task,signal)=>{
  captures++;
  if(pendingMode)await new Promise((ok,fail)=>signal.addEventListener('abort',()=>{canceled++;fail(new Error('Fixture canceled'));},{once:true}));
  return {token,api_key:'sk-connector-fixture',device_id:'fixture-device',install_id:'fixture-install',app_version:'3.0.0'};
 }});

  pendingMode=false;const ctx=await browser.newContext({locale,viewport:{width:1280,height:900}}),page=await ctx.newPage();let downloaded=0,checks=0,saves=0,starts=0,accounts=[],errors=[];
  // Leave any real local connector alone. Only this test browser forwards its
  // fixed client URL to the ephemeral synthetic connector.
  await ctx.route('http://127.0.0.1:18765/**',async route=>{
    const request=route.request();
    const response=await route.fetch({url:'http://127.0.0.1:'+bridge.port+new URL(request.url()).pathname,headers:{...request.headers(),host:'127.0.0.1:'+bridge.port}});
    await route.fulfill({response});
  });
  if(visit!==base){
    await ctx.route(visit+'/**',async r=>{const response=await fetch(base+new URL(r.request().url()).pathname);await r.fulfill({status:response.status,headers:Object.fromEntries(response.headers),body:Buffer.from(await response.arrayBuffer())});});
    await ctx.grantPermissions(['local-network-access'],{origin:visit});
  }
  page.on('download',()=>downloaded++);page.on('pageerror',e=>errors.push(e.message));
  await ctx.route('**/auth/session',r=>r.fulfill({json:{authenticated:true,user:{id:'builtin:admin',role:'admin',username:'admin'}}}));
  await ctx.route('**/admin/api/**',r=>{const req=r.request(),path=new URL(req.url()).pathname;let out={ok:true};
   if(path.endsWith('/state'))out={accounts,keys:[],models:[],settings:{pool:{}},stats:{}};
   if(path.endsWith('/provider-login')){const b=req.postDataJSON();
    if(b.action==='start'){starts++;out={session_id:'connector-'+starts,url:'https://www.teleai.com.cn/teleai-api/upms/public/auth/authorize?client_id=Z2veLLEHhZfq3hb5hWIwqJuPajIuasYi&redirect_uri=https%3A%2F%2Fagent.teleai.com.cn%2FsuperCowork%2Fsapi%2Fcowork%2Fauth',helper_challenge:(starts===1?'A':'B').repeat(43),expires_at:Math.floor(Date.now()/1000)+600};}
    if(b.action==='check'){checks++;const input=JSON.parse(b.input);assert.equal(input.credentials.token,token);assert.equal(input.challenge,(starts===1?'A':'B').repeat(43));out={ready:true,name:'Connector verified',uid:'fi***re',scope:'cn',preview_id:'verified-preview'};}
    if(b.action==='complete'){saves++;assert.equal(b.preview_id,'verified-preview');accounts=[{id:'connector-account',provider:'teleagent',name:'Connector verified',enabled:true,scope:'cn',source:'browser',status:'ok'}];}
   }
   return r.fulfill({json:out});
  });
  await ctx.route('**/health',r=>r.fulfill({json:{accounts:accounts.length,ready:accounts.length,models:0}}));
  await page.goto(visit+'/#accounts');await page.locator('#addAccount').click();await page.locator('#teleagentLogin').click();await page.locator('#accountMethodBrowser').click();await page.locator('#accountCreateNext').click();
  assert.equal(await page.locator('#teleagentResultFile').isVisible(),false);assert.equal(await page.locator('#teleagentTaskDownload').isVisible(),false);
  await page.locator('#providerLoginStart').click();await page.locator('#providerLoginError').filter({hasText:locale==='zh-CN'?'首次使用':'before first use'}).waitFor();assert.equal(starts,0);
  await page.locator('#teleagentPairKey').fill('wrong');await page.locator('#teleagentPair').click();assert.equal(starts,0);
  await page.locator('#teleagentPairKey').fill(secret);await page.locator('#teleagentPair').click();await page.waitForFunction(()=>!document.querySelector('#teleagentConnectorSetup').open);
  assert.equal(await page.locator('#teleagentPairKey').inputValue(),'');
  await page.locator('#providerLoginStart').click();await page.locator('#providerPreview').waitFor({state:'visible',timeout:20000});assert.equal(checks,1);assert.equal(saves,0);assert.equal(downloaded,0);
  await page.locator('#providerLoginComplete').click();await page.locator('#addAccountDialog').waitFor({state:'hidden'});assert.equal(saves,1);
  // A later login uses the same saved pairing and cancels the owned browser.
  pendingMode=true;const before=canceled;
  await page.locator('#addAccount').click();await page.locator('#teleagentLogin').click();await page.locator('#accountCreateNext').click();assert.equal(await page.locator('#teleagentPairKey').isVisible(),false);
  await page.locator('#providerLoginStart').click();await page.locator('#providerLoginStatus').filter({hasText:locale==='zh-CN'?'正在打开':'Opening'}).waitFor();
  await page.waitForTimeout(300);await page.locator('#accountCreateCancel').click();await page.locator('#addAccountDialog').waitFor({state:'hidden'});
  for(let i=0;i<30 && canceled===before;i++)await page.waitForTimeout(100);assert.equal(canceled,before+1);assert.equal(checks,1);assert.equal(saves,1);assert.equal(downloaded,0);assert.deepEqual(errors,[]);
  await ctx.close();await bridge.close();bridge=null;
 }assert.equal(captures,6);console.log('PASS: real loopback pairing, automatic login result and preview, confirmation-only persistence, paired second login and cancel; zh/en, HTTP and HTTPS panel origins, zero downloads/uploads.');
 }finally{await browser.close();await bridge?.close();}
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
