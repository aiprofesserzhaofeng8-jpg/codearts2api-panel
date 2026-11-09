// Daily reward interaction and post-claim balance refresh; no real account.
const {chromium}=require('playwright'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});try{for(const locale of ['zh-CN','en-US']){
 const ctx=await browser.newContext({locale,viewport:{width:1440,height:1000}}),page=await ctx.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));let claimed=false,claims=0,balanceReads=0;
 const account=()=>({id:'ta-daily',name:'TeleAgent daily fixture',provider:'teleagent',scope:'cn',source:'browser',enabled:true,status:'ok',expires_at:2000000000,daily_claim_supported:true,claimed_today:claimed,last_claim_state:claimed?'claimed':'unknown'});
 await ctx.route('**/auth/session',r=>r.fulfill({json:{authenticated:true,user:{id:'builtin:admin',role:'admin',username:'admin'}}}));
 await ctx.route('**/admin/api/**',r=>{const req=r.request(),path=new URL(req.url()).pathname;let out={ok:true};
  if(path.endsWith('/state'))out={accounts:[account()],models:[],keys:[],stats:{},settings:{pool:{},auto_claim:false}};
  if(path.endsWith('/credits')){balanceReads++;out={accounts:[{id:'ta-daily',ok:true,credit:{kind:'credits',unit:'credits',available:true,windows:[{id:'balance',name:'积分余额',unit:'credits',remaining:claimed?150:50}]}}]};}
  if(path.endsWith('/welfare'))out={ok:true,id:'ta-daily',provider:'teleagent',account:'TeleAgent daily fixture',accounts:[account()],claimable_count:claimed?0:1,campaigns:[{title:'TeleAgent 每日积分',type:'daily_checkin',status:claimed?'already_claimed':'claimable',claimable:!claimed,auto_claimable:!claimed,benefit_amount:100,benefit_unit:'积分'},{title:'邀请共享积分',type:'activity_notice',status:'ENTRY',claimable:false,auto_claimable:false,trigger_event:'须满足真实活动条件'}]};
  if(path.endsWith('/claim')){assert.equal(req.method(),'POST');assert.equal(req.postDataJSON().id,'ta-daily');claims++;claimed=true;out={succeeded:1,total:1,results:[{id:'ta-daily',name:'TeleAgent daily fixture',provider:'teleagent',ok:true,message:'TeleAgent 每日积分已复查确认'}]};}
  if(path.endsWith('/usage'))out={page:1,pages:1,total:0,stats:{},logs:[]};return r.fulfill({json:out});
 });
 await ctx.route('**/health',r=>r.fulfill({json:{accounts:1,ready:1,models:5}}));
 await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10088')+'/#accounts');
 const row=page.locator('#accounts tr[data-state]');await row.locator('[data-quota-window="balance"]').waitFor();assert.ok((await row.textContent()).includes('50 Credits'));assert.equal(claims,0);
 await page.locator('#loadWelfare').click();await page.locator('#welfare').getByText('TeleAgent 每日积分',{exact:true}).waitFor();assert.equal(claims,0);assert.equal(await page.locator('#welfare tr').count(),2);
 const before=balanceReads;
 await row.getByRole('button',{name:locale==='zh-CN'?'更多':'More',exact:true}).click();
 await page.locator('#accountActionMenu').getByRole('button',{name:locale==='zh-CN'?'领取可领奖励':'Claim available rewards',exact:true}).click();
 await page.waitForFunction(()=>document.querySelector('#accounts').textContent.includes('150 Credits'));
 assert.equal(claims,1);assert.ok(balanceReads>before);assert.ok((await row.textContent()).includes(locale==='zh-CN'?'已领':'Claimed'));
 await page.locator('#loadWelfare').click();await page.locator('#welfare').getByText('TeleAgent 每日积分',{exact:true}).waitFor();assert.equal(claims,1);
 await page.locator('#nav-settings').click();assert.equal(await page.locator('#sAutoClaim').isChecked(),false);assert.ok((await page.locator('label[for="sAutoClaim"]').textContent()).includes('TeleAgent'));
 assert.deepEqual(errors,[]);await ctx.close();
}console.log('PASS: TeleAgent daily button, read-only activity list, notices not claimed, post-claim balance refresh, status and opt-in automatic setting, Chinese/English.');}finally{await browser.close();}})().catch(e=>{console.error(e.stack);process.exitCode=1;});
