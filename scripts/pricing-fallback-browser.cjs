// Invoked only by TestDefaultPricingBrowser against its temporary httptest server.
const {chromium} = require('../frontend/node_modules/playwright');
const assert = require('node:assert/strict');

(async () => {
  const {url, cookieName, cookieValue, screenshot} = JSON.parse(require('node:fs').readFileSync(0,'utf8'));
  const browser = await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL || 'chrome'});
  try {
    const page = await browser.newPage({locale:'zh-CN', viewport:{width:1280,height:800}});
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.context().addCookies([{name:cookieName,value:cookieValue,url}]);
    await page.goto(url+'/#models');
    const edit = page.locator('#editDefaultPrice'), dialog = page.locator('#priceEditDialog');
    const row = page.locator('#pricingRows tr').filter({hasText:'test-model'});
    const read = async () => (await page.request.get(url+'/admin/api/pricing')).json();
    await row.getByText('未配置',{exact:true}).first().waitFor();
    await edit.click();
    assert.equal(await page.locator('#priceEditClear').isVisible(), false);
    for (const [field,value] of Object.entries({Input:'2',Output:'6',Cached:'1'})) {
      await page.locator('#priceEdit'+field).fill(value);
    }
    await page.locator('#priceEditSave').click(); await dialog.waitFor({state:'hidden'});
    await row.getByText(/全站默认价/).waitFor();
    assert.deepEqual((await row.locator('td').allTextContents()).slice(2,5), ['2','6','1']);
    assert.equal((await read()).default_price.output,6);
    await page.reload(); await row.getByText(/全站默认价/).waitFor();
    await edit.click(); assert.equal(await page.locator('#priceEditInput').inputValue(),'2');
    if (screenshot) await dialog.screenshot({path:screenshot});
    await page.route('**/admin/api/pricing/default', route => route.fulfill({
      status:500,json:{error:{message:'测试保存失败'}}
    }),{times:1});
    await page.locator('#priceEditClear').click();
    await page.locator('#priceEditError').getByText(/测试保存失败/).waitFor();
    assert.equal((await read()).default_price.output,6);
    await page.locator('#priceEditClear').click(); await dialog.waitFor({state:'hidden'});
    await row.getByText('未配置',{exact:true}).first().waitFor();
    assert.equal((await read()).default_price,undefined);
    assert.deepEqual(errors,[]);
  } finally {await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
