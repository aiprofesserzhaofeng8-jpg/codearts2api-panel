// Fixtures only: no real account, model or policy writes.
const {chromium}=require('playwright');const assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true,channel:'chrome'});try{
const context=await browser.newContext({viewport:{width:1460,height:1000}}),page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
let policy={context_window:65536,max_output_tokens:8192,reserve_tokens:16384,keep_recent_tokens:12000,trigger_tokens:45000,summary_max_tokens:8192,summary_enabled:true,summary_retry:true,disable_summary_thinking:true,source:'保守推荐；已实测'};let writes=0,fail=false;
await context.route('**/admin/auth/session',r=>r.fulfill({json:{authenticated:true,local_compatibility:false}}));
await context.route('**/admin/api/**',route=>{
 const path=new URL(route.request().url()).pathname;
 if(path.endsWith('/state'))return route.fulfill({json:{accounts:[],keys:[],settings:{pool:{}},stats:{},models:[{id:'deepseek-v4.1-flash',source:'upstream'},{id:'unverified-model',source:'upstream'}]}});
 if(path.endsWith('/model-policies')){
  if(route.request().method()==='POST'){writes++;if(fail)return route.fulfill({status:400,json:{error:{message:'模拟：预留不足'}}});policy=route.request().postDataJSON().policy;}
  return route.fulfill({json:{policies:{'deepseek-v4.1-flash':policy}}});
 }
 if(path.endsWith('/pricing'))return route.fulfill({json:{prices:{}}});
 return route.fulfill({json:{accounts:[]}});
});await context.route('**/health',r=>r.fulfill({json:{ready:0,accounts:0}}));
await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10087')+'/#models');await page.locator('#modelPolicies tr').nth(1).waitFor();
assert.equal(await page.locator('#modelPolicies input').count(),0);
const row=page.locator('#modelPolicies tr').filter({hasText:'deepseek-v4.1-flash'});
await row.getByRole('button',{name:'编辑',exact:true}).click();await page.locator('#policyKeep').fill('8192');await page.locator('#policyCancel').click();assert.equal(writes,0);
await row.getByRole('button',{name:'编辑',exact:true}).click();assert.equal(await page.locator('#policyKeep').inputValue(),'12000');
fail=true;await page.locator('#policySave').click();await page.waitForFunction(()=>document.querySelector('#policyError').textContent.includes('模拟'));assert.equal(await page.locator('#policyDialog').isVisible(),true);
fail=false;await page.locator('#policyKeep').fill('8192');await page.locator('#policyFallback').selectOption('unverified-model');await page.locator('#policySave').click();await page.locator('#policyDialog').waitFor({state:'hidden'});assert.equal(policy.keep_recent_tokens,8192);assert.equal(policy.summary_fallback_model,"unverified-model");
await page.evaluate(()=>document.documentElement.dataset.theme='light');await page.screenshot({path:'artifacts/model-policies-light.png',fullPage:true});
await page.locator('#modelPolicies tr').filter({hasText:'unverified-model'}).getByRole('button',{name:'编辑',exact:true}).click();assert.equal(await page.locator('#policyContext').inputValue(),'');assert.equal(await page.locator('#policySummary').isChecked(),false);
await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await page.screenshot({path:'artifacts/model-policy-mobile.png',fullPage:true});assert.deepEqual(errors,[]);
console.log('PASS: model policies read-only, cancel, validation error, save/retry, unverified model defaults and mobile.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
