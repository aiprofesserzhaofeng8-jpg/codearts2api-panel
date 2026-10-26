// Fixtures only: never edits the real price table or calls a model.
const {chromium}=require('playwright');const assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chrome'});try{
const context=await browser.newContext({viewport:{width:1460,height:1000}});const errors=[];
const prices={'deepseek-v4.1-flash':{currency:'USD',input:.3,output:1.2,cached:.006,source:'DeepSeek 官方高峰参考价',updated_at:'2026-10-25'}};
const stats={total_requests:10,usage_requests:10,priced_requests:8,priced_usd_requests:8,estimated_usd:.25};
let saves=0;
await context.route('**/admin/auth/session',r=>r.fulfill({json:{authenticated:true,local_compatibility:false}}));
await context.route('**/admin/api/**',async route=>{const path=new URL(route.request().url()).pathname;
if(path.endsWith('/state'))return route.fulfill({json:{accounts:[],keys:[],stats,settings:{pool:{}},models:[{id:'deepseek-v4.1-flash',source:'upstream'},{id:'glm-5.3',source:'upstream'},{id:'gpt-4o',source:'alias'}]}});
if(path.endsWith('/pricing')){if(route.request().method()==='POST'){saves++;const b=route.request().postDataJSON();prices[b.model]={...b.price,updated_at:'now'};}return route.fulfill({json:{prices,ok:true}});}
if(path.endsWith('/usage'))return route.fulfill({json:{logs:[],stats,total:10,page:1,page_size:20,pages:1,retained:10,oldest:0,retention_limit:100000}});
if(path.endsWith('/logs'))return route.fulfill({json:{logs:[]}});return route.fulfill({json:{accounts:[]}});});
await context.route('**/health',r=>r.fulfill({json:{accounts:0,ready:0}}));
const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10087')+'/#models');
await page.locator('#pricingRows tr').nth(1).waitFor();
assert.equal(await page.locator('#pricingRows tr').count(),2);
assert.equal(await page.locator('#pricingRows input').count(),0);
const row=page.locator('#pricingRows tr').filter({hasText:'glm-5.3'});
await row.getByRole('button',{name:'编辑',exact:true}).click();await page.locator('#priceEditSave').click();assert.equal(saves,0);
for(const [f,v]of [['Input','3'],['Output','10'],['Cached','0']])await page.locator('#priceEdit'+f).fill(v);
await page.locator('#priceEditSave').click();await page.waitForFunction(()=>document.querySelector('#toast').textContent.includes('价格已保存'));
assert.equal(saves,1);assert.equal(prices['glm-5.3'].cached,0);assert.equal(prices['glm-5.3'].currency,'USD');assert.equal(await page.locator('#pricingRows select').count(),0);
await page.evaluate(()=>document.documentElement.dataset.theme='light');await page.screenshot({path:'artifacts/pricing-table.png',fullPage:true});
await page.locator('#nav-overview').click();await page.locator('.usage-kpi.cost .usage-value').waitFor();assert.equal(await page.locator('.usage-kpi.cost .usage-value').textContent(),'$0.250000');
assert.match(await page.locator('.usage-kpi.cost .usage-sub').textContent(),/8\/10/);
await page.screenshot({path:'artifacts/usage-estimated-cost.png',fullPage:true});
await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
assert.deepEqual(errors,[]);console.log('PASS: price editing, required values, explicit zero, alias exclusion, USD-only totals, coverage and mobile.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
