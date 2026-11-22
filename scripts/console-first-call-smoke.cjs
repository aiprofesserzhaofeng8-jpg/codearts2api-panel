// The browser exercises built assets only. All session and data requests are fixtures.
const { chromium } = require('playwright');
const assert = require('node:assert/strict');
(async () => {
  const browser = await chromium.launch({channel: process.env.CODEARTS_TEST_BROWSER || 'chrome'});
  try {
    for (const locale of ['zh-CN', 'en-US']) {
      const context = await browser.newContext({
        locale,
        viewport:{width:locale==='zh-CN'?1280:360,height:820},
        permissions:['clipboard-read','clipboard-write'],
      });
      const page = await context.newPage();
      let history='records';
      let guideResult = 'models';
      let guideReads = 0;
      const chinese = locale === 'zh-CN';
      const log = {status:429,err:'fixture quota exhausted',provider:'historical-fixture',request_id:'req-123',requested_model:'public-name',model:'resolved-name',protocol:'responses',usage:{},latency_ms:28,time:1700000000};
      await context.route('**/setup/status', r=>r.fulfill({json:{ready:true,needs_setup:false}}));
      await context.route('**/auth/session', r=>r.fulfill({json:{authenticated:true,user:{id:'builtin:admin',role:'admin',username:'admin'}}}));
      await context.route('**/admin/api/**', r=>{
        const url=new URL(r.request().url());
        if(url.pathname.endsWith('/usage') && history!=='records'){
          if(history==='error')return r.fulfill({status:503,json:{error:{message:'history offline'}}});
          return r.fulfill({json:{page:1,pages:1,total:0,retained:history==='empty'?0:3,logs:[],stats:{}}});
        }
        if (url.pathname.endsWith('/keys/models')) {
          guideReads++;
          assert.equal(url.searchParams.get('ref'), 'key-fixture');
          if (guideResult === 'error') {
            return r.fulfill({status:503,json:{error:{message:'fixture guide unavailable'}}});
          }
          return r.fulfill({json:{models:guideResult === 'empty' ? [] : ['public-name']}});
        }
        if(url.pathname.endsWith('/unified-models'))return r.fulfill({json:{routing_revision:1,models:[{id:'m',name:'public-name',enabled:true,sources:[{id:'kimi/cn/source-with-a-long-upstream-model-identifier',enabled:true},{id:'fixture/other',enabled:false}]}]}});
        const data=url.pathname.endsWith('/usage')?{page:1,pages:1,total:1,retained:1,logs:[log],stats:{}}:{accounts:[],models:[],keys:[
          {name:'fixture',group_id:'group-a',ref:'key-fixture',key:'masked',enabled:true,budget_usage:{},budget:{}},
          {name:'paused-fixture',ref:'key-paused',key:'masked',enabled:false,budget_usage:{},budget:{}},
          {name:'legacy',ref:'legacy',key:'masked',enabled:true},
        ],groups:[{id:'group-a',name:'Group A'}],settings:{pool:{}},stats:{}};
        return r.fulfill({json:data});
      });
      await context.route('**/health', r=>r.fulfill({json:{accounts:0,ready:0,models:0}}));
      await page.goto(process.env.CODEARTS_TEST_BASE+'/#accounts');
      await page.locator('#accounts td.empty button').click();
      await page.locator('#addAccountDialog').waitFor();
      await page.keyboard.press('Escape');
      await page.evaluate(()=>location.hash='overview');
      await page.locator('#usageLogs tr').first().waitFor();
      const select=page.locator('#usageProvider');
      assert.equal(await select.locator('option[value="historical-fixture"]').count(),1);
      await Promise.all([page.waitForResponse(r=>new URL(r.url()).searchParams.get('provider')==='kimi'),select.selectOption('kimi')]);
      await Promise.all([page.waitForResponse(r=>new URL(r.url()).searchParams.get('model_mismatch')==='true'),page.locator('#usageMismatch').selectOption('true')]);
      assert.equal(await page.locator('#usageRecordDialog,#usageLogs button').count(),0);
      const notice=page.locator('#usageNoticeAction');
      const state=kind=>page.waitForSelector('#tab-overview[data-result="'+kind+'"]');
      history='error';await page.locator('#reloadUsage').click();await state('error');
      assert.equal(await page.locator('#kpis').isVisible(),false);
      history='filtered';await Promise.all([page.waitForResponse(r=>new URL(r.url()).searchParams.get('provider')==='kimi'),notice.click()]);await state('filtered');
      history='records';await notice.click();await state('records');assert.equal(await select.inputValue(),'');
      history='empty';await page.locator('#reloadUsage').click();await state('empty');
      assert.equal(await page.locator('.usage-charts-grid').isVisible(),false);
      await page.screenshot({path:'/tmp/console-empty-'+locale+'.png'});
      await notice.click();await page.locator('#tab-keys.on').waitFor();
      await page.evaluate(()=>location.hash='models');
      const model=page.locator('#unifiedModels tr').filter({hasText:'public-name'});
      const copy = model.getByRole('button',{name:chinese?'复制 public-name':'Copy public-name',exact:true});
      await copy.waitFor();
      assert.equal(await copy.textContent(), '');
      assert.equal(await copy.getAttribute('title'), chinese?'复制':'Copy');
      assert.match(await copy.evaluate(button => getComputedStyle(button,'::before').maskImage), /copy-.*\.svg/);
      await copy.focus();
      await page.keyboard.press('Enter');
      await page.waitForFunction(async () => await navigator.clipboard.readText() === 'public-name');
      const toggle=model.locator('.source-toggle');
      const list=model.locator('.source-list');
      await toggle.scrollIntoViewIfNeeded();
      const layout=()=>model.locator('td').evaluateAll(cells=>cells.map(cell=>cell.getBoundingClientRect().toJSON()));
      const closedLayout=await layout();
      await toggle.click();
      await list.waitFor();
      assert.ok((await list.innerText()).includes('kimi/cn/source-with-a-long-upstream-model-identifier'));
      assert.deepEqual(await layout(),closedLayout);
      const box=await list.boundingBox();
      assert.ok(box && box.x>=0 && box.x+box.width<=page.viewportSize().width);
      await page.screenshot({path:'/tmp/console-models-'+locale+'.png'});
      await page.keyboard.press('Escape');
      await list.waitFor({state:'hidden'});
      assert.deepEqual(await layout(),closedLayout);
      await page.evaluate(()=>location.hash='keys');
      const useButtons = page.locator('#keys').getByRole('button',{name:chinese?'使用':'Use',exact:true});
      const use = useButtons.first();
      await use.waitFor();
      const headers = page.locator('#tab-keys thead th');
      assert.deepEqual((await headers.allTextContents()).slice(0,3), chinese ? ['名称','API 密钥','分组'] : ['Name','API keys','Group']);
      const firstRow = page.locator('#keys tr').first();
      assert.equal(await firstRow.locator('td').nth(0).textContent(),'fixture');
      assert.equal(await firstRow.locator('td').nth(1).locator('input').getAttribute('type'),'password');
      assert.equal(await firstRow.locator('td').nth(2).textContent(),'Group A');
      assert.ok(await firstRow.locator('td').nth(3).isVisible());
      assert.equal(await use.textContent(), chinese?'使用':'Use');
      assert.equal(await use.getAttribute('title'), chinese?'使用':'Use');
      assert.match(await use.evaluate(button => getComputedStyle(button,'::before').maskImage), /terminal-.*\.svg/);
      assert.equal(await useButtons.nth(1).isDisabled(), true);
      assert.equal(await useButtons.nth(2).isDisabled(), true);
      await page.getByRole('button',{name:chinese?'绑定分组':'Assign group',exact:true}).click();
      await page.locator('#keyGroupDialog[open]').waitFor();
      await page.keyboard.press('Escape');
      await page.locator('#keys').screenshot({path:'/tmp/console-key-actions-'+locale+'.png'});
      await use.click();
      const guide=page.locator('.key-use-dialog[open]');
      for(const [name,sample] of [['cURL','/v1/chat/completions'],['Codex','wire_api'],['Claude Code','ANTHROPIC_BASE_URL']]){
        await guide.getByRole('button',{name,exact:true}).click();
        assert.ok((await guide.locator('pre').innerText()).includes(sample));
        assert.ok((await guide.locator('pre').innerText()).includes('public-name'));
      }
      await guide.getByRole('button',{name:'cURL',exact:true}).click();
      const curlExample = await guide.locator('pre').innerText();
      assert.ok(curlExample.includes('$CODEARTS_API_KEY'));
      await guide.getByRole('button',{name:chinese?'复制命令':'Copy command',exact:true}).click();
      await page.waitForFunction(async example => await navigator.clipboard.readText() === example, curlExample);
      await page.screenshot({path:'/tmp/console-key-guide-'+locale+'.png'});
      await page.keyboard.press('Escape');
      await guide.waitFor({state:'hidden'});
      guideResult = 'error';
      await use.click();
      await guide.getByRole('alert').waitFor();
      assert.equal(await guide.getByRole('alert').textContent(), 'fixture guide unavailable');
      assert.equal(await guide.locator('pre').count(), 0);
      await page.keyboard.press('Escape');
      await guide.waitFor({state:'hidden'});

      guideResult = 'empty';
      const response = page.waitForResponse(r => new URL(r.url()).pathname.endsWith('/keys/models'));
      await use.click();
      await response;
      await guide.getByRole('status').waitFor({state:'hidden'});
      assert.equal(await guide.locator('select, pre').count(), 0);
      assert.equal(await guide.getByRole('alert').count(), 0);
      assert.equal(guideReads, 3);
      await page.evaluate(() => navigator.clipboard.writeText(''));
      await context.close();
    }
    console.log('PASS: onboarding, history filters and empty states, keyboard model copy, key guide and clipboard, guide failure and empty catalog; zh desktop and en mobile');
  } finally { await browser.close(); }
})().catch(error=>{console.error(error);process.exitCode=1;});
