// Optional browser verification: requires Playwright + Chromium and a running local gateway.
// node scripts/ui-smoke.cjs [base URL] [first-run credentials file]
const { chromium } = require('playwright');
const fs = require('node:fs');
const assert = require('node:assert/strict');

(async () => {
  const base = process.argv[2] || 'http://127.0.0.1:10087';
  const access = JSON.parse(fs.readFileSync(process.argv[3] || 'data/state.json.access.json', 'utf8'));
  const browser = await chromium.launch({ headless: true, channel: process.env.PLAYWRIGHT_CHANNEL || undefined });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    const login=await context.request.post(base+'/admin/auth/login',{data:{password:access.admin_password}});
    assert.ok(login.ok(),'fixture administrator login');
    await page.goto(base);
    await page.locator('#forgetPw').waitFor({state:'visible'});
    assert.equal(await page.locator('#adminLoginDialog').isVisible(), false);
    await page.locator('#nav-settings').click();
    await page.locator('#sPoolStrategy').waitFor({ state: 'visible' });
    const original = await page.locator('#sPoolStrategy').inputValue();
    const save = async status => {
      await Promise.all([
        page.waitForResponse(r => r.url().endsWith('/admin/api/settings') && r.request().method() === 'POST' && r.status() === status),
        page.locator('#saveSettings').click()
      ]);
      if (status === 200) await page.locator('#settingsDirty').waitFor({ state: 'hidden' });
    };
    await page.locator('#sPoolStrategy').selectOption('round_robin');
    await save(200);
    await page.reload();
    await page.locator('#nav-settings').click();
    await page.waitForFunction(() => document.querySelector('#sPoolStrategy').value === 'round_robin');
    await page.locator('#sPoolConcurrency').fill('0');
    await save(400);
    assert.match(await page.locator('#toast').textContent(), /max_in_flight/);
    await page.locator('#sPoolConcurrency').fill('2');
    await page.locator('#sPoolStrategy').selectOption(original);
    await save(200);
    fs.mkdirSync('artifacts', { recursive: true });
    await page.screenshot({ path: 'artifacts/settings-desktop.png', fullPage: true });

    // Account-row rendering/actions use browser fixtures; no account is imported
    // and no upstream balance/model request is sent by this test.
    const response = await context.request.get(base + '/admin/api/state', { headers: { 'X-Admin-Password': access.admin_password } });
    const snapshot = await response.json();
    const account = { id: 'ui-fixture', name: '界面测试账号', source: 'browser', enabled: true, paused: false,
      status: 'ok', failures: 0, has_refresh: true, claimed_today: false,
      pool: { state: 'ready', in_flight: 0, max_in_flight: 2, cooldown_remaining_sec: 0 } };
    const actions = [];
    await page.route('**/admin/api/state', route => route.fulfill({ json: { ...snapshot, accounts: [account] } }));
    await page.route('**/admin/api/credits*', route => route.fulfill({ json: { accounts: [] } }));
    await page.route('**/admin/api/accounts', route => {
      const action = route.request().postDataJSON().action;
      actions.push(action);
      if (action === 'pause' || action === 'resume') {
        account.paused = action === 'pause';
        account.pool.state = account.paused ? 'paused' : 'ready';
      }
      if (action === 'clear-errors') { account.pool.state = 'ready'; account.pool.cooldown_remaining_sec = 0; }
      return route.fulfill({ json: { ok: true } });
    });
    await page.reload();
    await page.locator('#nav-accounts').click();
    await page.locator('#accounts tr').first().getByRole('button',{name:'更多',exact:true}).click();await page.locator('#accountActionMenu').getByRole('button', { name: '暂停选号', exact: true }).click();
    await page.getByText('已暂停选号', { exact: true }).waitFor();
    await page.locator('#accounts tr').first().getByRole('button',{name:'更多',exact:true}).click();await page.locator('#accountActionMenu').getByRole('button', { name: '恢复选号', exact: true }).click();
    await page.locator('#accounts tr[data-state="ready"]').waitFor();
    account.pool.state = 'cooling'; account.pool.cooldown_remaining_sec = 60;
    await page.reload();
    await page.locator('#accounts tr').first().getByRole('button',{name:'更多',exact:true}).click();await page.locator('#accountActionMenu').getByRole('button', { name: '解除冷却 / 清错', exact: true }).click();
    await page.getByRole('button', { name: '解除冷却 / 清错', exact: true }).waitFor({ state: 'hidden' });
    assert.deepEqual(actions, ['pause', 'resume', 'clear-errors']);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({ path: 'artifacts/accounts-mobile.png', fullPage: true });
    assert.deepEqual(errors, []);
    console.log('PASS: admin login, pool settings persistence/validation, pause/resume/recovery actions, desktop/mobile render; no JavaScript errors.');
  } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });
