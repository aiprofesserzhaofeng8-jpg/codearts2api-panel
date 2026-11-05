// UI fixtures only. Never authorize or import a real Kiro account.
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
    const settings = {pool:{}, kiro:{prompt_mode:'passthrough',prompt_overrides:{},search_enabled:false}, common_prompt:{prompt_mode:'passthrough',prompt_text:'COMMON',prompt_fallback:true,sanitize:true}};
    await ctx.route('**/auth/session', r => r.fulfill({json:{authenticated:true,user:{id:'builtin:admin',username:'admin',role:'admin'}}}));
    await ctx.route('**/admin/api/**', route => {
      const req = route.request(), url = new URL(req.url()); let out = {ok:true};
      if (url.pathname.endsWith('/state')) out = {accounts,keys:[],models:[],settings,stats:{}};
      if (url.pathname.endsWith('/credits')) out = {accounts:accounts.map(a => ({id:a.id,ok:true,credit:{available:false,reason:'fixture'}}))};
      if (url.pathname.endsWith('/provider-login')) {
        const body = req.postDataJSON();
        if (body.action === 'start') {
          starts++; assert.equal(body.provider,'kiro'); assert.equal(body.scope,'eu-central-1');
          out = {session_id:'kiro-flow',url:'https://view.awsapps.com/start/#/device?user_code=FIXTURE',scope:'eu-central-1'};
        }
        if (body.action === 'check') {
          checks++; assert.equal(body.input,'');
          out = {ready:true,name:'Kiro fixture',uid:'masked',scope:'eu-central-1',preview_id:'kiro-preview',existing:false};
        }
        if (body.action === 'complete') {
          saves++; assert.equal(body.preview_id,'kiro-preview');
          accounts = [{id:'qd',name:'Kiro fixture',provider:'kiro',scope:'eu-central-1',enabled:true,non_expiring:true,status:'ok',source:'browser',daily_claim_supported:false}];
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
    await page.locator('#addAccount').click(); await page.locator('#kiroLogin').click();
    assert.equal(starts,0);
    await page.locator('#accountCreateNext').click();
    assert.equal(await page.locator('#providerTraeDevice').isVisible(),false);
    assert.equal(await page.locator('#providerLoginCallback').isVisible(),false);
    await page.locator('#providerScope').selectOption('eu-central-1');
    await page.locator('#providerLoginStart').click();
    await page.waitForFunction(() => document.querySelector('#providerLoginURL').value.includes('view.awsapps.com'));
    assert.equal(await page.locator('#providerLoginOpen').getAttribute('href'),'https://view.awsapps.com/start/#/device?user_code=FIXTURE');
    assert.equal(ctx.pages().length,1);
    await page.locator('#providerLoginCheck').click(); await page.locator('#providerPreview').waitFor({state:'visible'});
    assert.equal(saves,0); assert.equal(checks,1);
    await page.locator('#providerLoginComplete').click(); await page.locator('#addAccountDialog').waitFor({state:'hidden'});
    await page.locator('#accountProvider').selectOption('kiro');
    assert.equal(await page.locator('#accounts tr[data-state]').count(),1);
    assert.match(await page.locator('#accounts').textContent(),/Kiro.*eu-central-1/);
    assert.equal(await page.locator('#accounts').getByRole('button',{name:'每日签到',exact:true}).count(),0);
    await page.locator('#nav-overview').click();
    await Promise.all([page.waitForResponse(r => r.url().includes('/usage?') && r.url().includes('provider=kiro')),page.locator('#usageProvider').selectOption('kiro')]);
    assert.ok(filters.includes('kiro'));
    await page.locator('#nav-settings').click();
    await page.locator('#sKIPromptText').waitFor({state:'visible'});
    assert.equal(await page.locator('#sKIPromptTextInherit').isChecked(),true);
    assert.equal(await page.locator('#sKIPromptText').inputValue(),'COMMON');
    await page.locator('#sKIPromptTextInherit').uncheck(); await page.locator('#sKIPromptText').fill('KIRO CUSTOM');
    await Promise.all([page.waitForResponse(r => r.url().endsWith('/settings') && r.request().method()==='POST'),page.locator('#saveSettings').click()]);
    assert.equal(savedSettings.kiro.prompt_overrides.prompt_text,'KIRO CUSTOM');
    assert.equal(savedSettings.kiro.search_enabled,false);
    await page.locator('#nav-accounts').click(); await page.locator('#addAccount').click();
    await page.setViewportSize({width:390,height:844});
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth),false);
    assert.ok(await page.locator('#kiroLogin').isVisible());
    await page.locator('#qoderLogin').click(); await page.locator('#accountCreateNext').click();
    assert.deepEqual(await page.locator('#providerScope option').evaluateAll(options => options.map(o => o.value)), ['cn','global']);
    fs.mkdirSync('artifacts',{recursive:true}); await page.screenshot({path:'artifacts/kiro-mobile.png'});
    assert.deepEqual(errors,[]);
    console.log('PASS: Kiro staged login, AWS region selector, explicit save, account/usage filters, prompt inheritance/override and mobile layout.');
  } finally { await browser.close(); }
})().catch(error => {console.error(error.stack);process.exitCode=1;});
