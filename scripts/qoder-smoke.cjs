// UI fixtures only. Never authorize or import a real Qoder account.
const {chromium} = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');

(async () => {
  const browser = await chromium.launch({headless:true, channel:process.env.CODEARTS_TEST_BROWSER || 'chrome'});
  try {
    const ctx = await browser.newContext({locale:'zh-CN', viewport:{width:1280,height:1000}});
    const page = await ctx.newPage(), errors = [];
    let accounts = [], starts = 0, saves = 0, checks = 0, filters = [], savedSettings;
    page.on('pageerror', error => errors.push(error.message));
    const settings = {pool:{}, qoder:{prompt_mode:'passthrough',prompt_overrides:{},search_enabled:false}, common_prompt:{prompt_mode:'passthrough',prompt_text:'COMMON',prompt_fallback:true,sanitize:true}};
    await ctx.route('**/auth/session', r => r.fulfill({json:{authenticated:true,user:{id:'builtin:admin',username:'admin',role:'admin'}}}));
    await ctx.route('**/admin/api/**', route => {
      const req = route.request(), url = new URL(req.url()); let out = {ok:true};
      if (url.pathname.endsWith('/state')) out = {accounts,keys:[],models:[],settings,stats:{}};
      if (url.pathname.endsWith('/credits')) out = {accounts:accounts.map(a => ({id:a.id,ok:true,credit:{available:false,reason:'fixture'}}))};
      if (url.pathname.endsWith('/provider-login')) {
        const body = req.postDataJSON();
        if (body.action === 'start') {
          starts++; assert.equal(body.provider,'qoder'); assert.equal(body.scope,'global');
          out = {session_id:'qoder-flow',url:'https://qoder.com/device/selectAccounts?nonce=fixture',scope:'global'};
        }
        if (body.action === 'check') {
          checks++; assert.equal(body.input,'');
          out = {ready:true,name:'Qoder fixture',uid:'masked',scope:'global',preview_id:'qoder-preview',existing:false};
        }
        if (body.action === 'complete') {
          saves++; assert.equal(body.preview_id,'qoder-preview');
          accounts = [{id:'qd',name:'Qoder fixture',provider:'qoder',scope:'global',enabled:true,status:'ok',source:'browser',daily_claim_supported:false}];
        }
      }
      if (url.pathname.endsWith('/usage')) {
        filters.push(url.searchParams.get('provider'));
        out = {page:1,pages:1,total:0,retained:0,retention_limit:100000,stats:{},logs:[]};
      }
      if (url.pathname.endsWith('/settings') && req.method() === 'GET') out=settings;
      if (url.pathname.endsWith('/settings') && req.method() === 'POST') savedSettings = req.postDataJSON();
      return route.fulfill({json:out});
    });
    await ctx.route('**/health', r => r.fulfill({json:{accounts:accounts.length,ready:accounts.length,models:0}}));
    const ready = page.waitForResponse(r => r.url().endsWith('/admin/api/state'));
    await page.goto((process.env.CODEARTS_TEST_BASE || 'http://127.0.0.1:10088')+'/#accounts'); await ready;
    await page.locator('#addAccount').click(); await page.locator('#qoderLogin').click();
    assert.equal(starts,0);
    await page.locator('#accountCreateNext').click();
    assert.equal(await page.locator('#providerTraeDevice').isVisible(),false);
    assert.equal(await page.locator('#providerLoginCallback').isVisible(),false);
    await page.locator('#providerScope').selectOption('global');
    await page.locator('#providerLoginStart').click();
    await page.waitForFunction(() => document.querySelector('#providerLoginURL').value.includes('qoder.com'));
    assert.equal(await page.locator('#providerLoginOpen').getAttribute('href'),'https://qoder.com/device/selectAccounts?nonce=fixture');
    assert.equal(ctx.pages().length,1);
    await page.locator('#providerLoginCheck').click(); await page.locator('#providerPreview').waitFor({state:'visible'});
    assert.equal(saves,0); assert.equal(checks,1);
    await page.locator('#providerLoginComplete').click(); await page.locator('#addAccountDialog').waitFor({state:'hidden'});
    await page.locator('#accountProvider').selectOption('qoder');
    assert.equal(await page.locator('#accounts tr[data-state]').count(),1);
    assert.match(await page.locator('#accounts').textContent(),/Qoder.*国际/);
    assert.equal(await page.locator('#accounts').getByRole('button',{name:'每日签到',exact:true}).count(),0);
    await page.locator('#nav-overview').click();
    await Promise.all([page.waitForResponse(r => r.url().includes('/usage?') && r.url().includes('provider=qoder')),page.locator('#usageProvider').selectOption('qoder')]);
    assert.ok(filters.includes('qoder'));
    await page.locator('#nav-settings').click();
    await page.locator('#sQDPromptText').waitFor({state:'visible'});
    assert.equal(await page.locator('#sQDPromptTextInherit').isChecked(),true);
    assert.equal(await page.locator('#sQDPromptText').inputValue(),'COMMON');
    await page.locator('#sQDPromptTextInherit').uncheck(); await page.locator('#sQDPromptText').fill('QODER CUSTOM');
    await Promise.all([page.waitForResponse(r => r.url().endsWith('/settings') && r.request().method()==='POST'),page.locator('#saveSettings').click()]);
    assert.equal(savedSettings.qoder.prompt_overrides.prompt_text,'QODER CUSTOM');
    assert.equal(savedSettings.qoder.search_enabled,false);
    await page.locator('#nav-accounts').click(); await page.locator('#addAccount').click();
    await page.setViewportSize({width:390,height:844});
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth),false);
    assert.ok(await page.locator('#qoderLogin').isVisible());
    fs.mkdirSync('artifacts',{recursive:true}); await page.screenshot({path:'artifacts/qoder-mobile.png'});
    assert.deepEqual(errors,[]);
    console.log('PASS: Qoder staged login, CN/global selector, explicit save, account/usage filters, prompt inheritance/override and mobile layout.');
  } finally { await browser.close(); }
})().catch(error => {console.error(error.stack);process.exitCode=1;});
