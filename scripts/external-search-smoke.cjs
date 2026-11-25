// Exercise the built panel against the isolated gateway created by frontend_e2e.py.
const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
(async () => {
  const browser = await chromium.launch({channel:process.env.CODEARTS_TEST_BROWSER || 'chrome'});
  const base = process.env.CODEARTS_TEST_BASE;
  try {
    for (const [locale,width] of [['zh-CN',1280],['en-US',390]]) {
      const ctx = await browser.newContext({locale, viewport:{width,height:850}});
      const login = await ctx.request.post(base+'/auth/login',{data:{username:'admin',password:process.env.CODEARTS_TEST_ADMIN_PASSWORD}});
      assert.equal(login.status(),200);
      const page=await ctx.newPage();
      const errors=[];
      page.on('pageerror',err=>errors.push(err.message));
      await page.goto(base+'/#accounts');
      if (width < 640) await page.locator('#consoleMenuToggle').click();
      await page.locator('#adminNavigation button[data-tab="settings"]').click();
      const area=page.locator('#externalSearch'), dialog=area.locator('dialog');
      await area.locator('.card-head button').last().click();
      fs.mkdirSync('artifacts/search',{recursive:true});
      await dialog.screenshot({path:'artifacts/search/editor-'+locale+'.png'});
      await dialog.locator('input').first().fill('Research '+locale);
      await dialog.locator('input[type=password]').fill('fixture-search-secret');
      await dialog.locator('button.primary').click();
      const row=area.locator('tbody tr').filter({hasText:'Research '+locale});
      await row.waitFor();
      if (width < 640) await page.locator('#consoleMenuToggle').click();
      await page.locator('#adminNavigation button[data-tab="accounts"]').click();
      const reloaded=page.waitForResponse(r=>r.url().endsWith('/admin/api/search-services') && r.request().method()==='GET');
      if (width < 640) await page.locator('#consoleMenuToggle').click();
      await page.locator('#adminNavigation button[data-tab="settings"]').click();
      assert.equal((await reloaded).status(),200);
      await row.waitFor();

      await row.locator('td').last().locator('button').nth(2).click();
      await row.locator('td').nth(3).locator('button:enabled').click();
      const data=await (await ctx.request.get(base+'/admin/api/search-services')).json();
      const chosen=data.services.find(s=>s.name==='Research '+locale);
      assert.ok(chosen?.enabled && chosen.api_key_set && chosen.id===data.default_service_id);
      assert.ok(!JSON.stringify(data).includes('fixture-search-secret'));
      await row.locator('td').last().locator('button').nth(1).click();
      assert.equal(await dialog.locator('input[type=password]').inputValue(),'');
      await dialog.locator('button[type=button]').click();
      await area.scrollIntoViewIfNeeded();
      await area.screenshot({path:'artifacts/search/settings-'+locale+'.png'});
      assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
      await page.goto(base+'/#groups');
      await page.locator('#tab-groups button.primary').first().click();
      const group=page.locator('#groupDialog');
      await group.locator('#groupName').fill('Search '+locale);
      await group.getByLabel('Research '+locale,{exact:false}).check();
      await group.locator('.group-search').screenshot({path:'artifacts/search/grants-'+locale+'.png'});
      await group.locator('button[type=submit],button.primary').last().click();
      await group.waitFor({state:'hidden'});
      const groups=await (await ctx.request.get(base+'/admin/api/groups')).json();
      assert.deepEqual(groups.groups.find(g=>g.name==='Search '+locale).search_service_ids,[chosen.id]);
      await page.goto(base+'/#settings');
      page.once('dialog',d=>d.accept());
      await row.locator('td').last().locator('button').last().click();
      await row.waitFor({state:'detached'});
      assert.deepEqual(errors,[]);
      await ctx.close();
    }
  } finally { await browser.close(); }
})().catch(e=>{console.error(e);process.exitCode=1});
