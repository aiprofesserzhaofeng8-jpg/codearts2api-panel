// Dedicated fixture instance only. Never run this against the user's state.
const {chromium}=require('playwright');const assert=require('node:assert/strict');const fs=require('node:fs');
(async()=>{const base=process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10088';if(!base.endsWith(':10088'))throw Error('Use isolated port 10088');
const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chrome'});try{
const context=await browser.newContext({viewport:{width:1460,height:1000},permissions:['clipboard-read','clipboard-write']});const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
fs.mkdirSync('artifacts',{recursive:true});await page.goto(base+'/#keys');await page.locator('#adminLoginDialog').waitFor({state:'visible'});
assert.equal((await context.request.get(base+'/admin/api/state')).status(),401);
await page.locator('#pw').fill('wrong');await page.locator('#savePw').click();await page.waitForFunction(()=>document.querySelector('#adminLoginError').textContent.includes('不正确'));
await page.locator('#pw').fill('feature-ui-test');await page.locator('#savePw').click();await page.locator('#adminLoginDialog').waitFor({state:'hidden'});await page.locator('#keys tr').first().waitFor();
assert.equal(await page.locator('#pw').inputValue(),'');assert.equal(await page.evaluate(()=>sessionStorage.getItem('codearts2api_pw')),null);
const cookies=await context.cookies();assert.ok(cookies.some(c=>c.name==='codearts_admin_session'&&c.httpOnly&&c.sameSite==='Strict'));
await page.locator('#keys tr').first().getByRole('button',{name:'预算',exact:true}).click();await page.locator('#budgetRequests').fill('2');await page.locator('#budgetTokens').fill('10000');await page.locator('#budgetUSD').fill('0.05');await page.locator('#keyBudgetSave').click();await page.locator('#keyBudgetDialog').waitFor({state:'hidden'});
await page.locator('#keys tr').first().getByRole('button',{name:'预算',exact:true}).click();assert.equal(await page.locator('#budgetRequests').inputValue(),'2');assert.equal(await page.locator('#budgetTokens').inputValue(),'10000');assert.equal(await page.locator('#budgetUSD').inputValue(),'0.05');await page.screenshot({path:'artifacts/key-budget-dialog.png'});await page.locator('#keyBudgetClose').click();
await page.locator('#nav-audit').click();await page.locator('#auditRows tr').filter({hasText:'/admin/api/keys/budget'}).first().waitFor();await page.screenshot({path:'artifacts/admin-audit.png'});
await page.reload();await page.locator('#forgetPw').waitFor({state:'visible'});assert.equal(await page.locator('#adminLoginDialog').isVisible(),false);
await page.locator('#forgetPw').click();await page.locator('#adminLoginDialog').waitFor({state:'visible'});assert.equal((await context.request.get(base+'/admin/api/state')).status(),401);
await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await page.screenshot({path:'artifacts/admin-login-mobile.png'});
assert.deepEqual(errors,[]);console.log('PASS: mandatory login, wrong-password error, HttpOnly session, no stored password, budget persistence, audit, reload and logout, mobile.');
}finally{await browser.close();}})().catch(e=>{console.error(e.message);process.exitCode=1;});
