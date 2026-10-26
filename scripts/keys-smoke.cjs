// Verifies copy/reveal/reload with an existing key. Never logs or screenshots keys.
const {chromium}=require('playwright');
const fs=require('node:fs');
const assert=require('node:assert/strict');
(async()=>{
  const base=process.argv[2]||'http://127.0.0.1:10087';
  const state=JSON.parse(fs.readFileSync('data/state.json','utf8'));
  const expected=state.keys.find(k=>k.enabled)?.key;
  if(!expected)throw new Error('An enabled existing key is required');
  const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||undefined});
  try {
    const context=await browser.newContext({permissions:['clipboard-read','clipboard-write']});
    const access=JSON.parse(fs.readFileSync('data/state.json.access.json','utf8'));
    const login=await context.request.post(base+'/admin/auth/login',{data:{password:process.env.CODEARTS_ADMIN_PASSWORD||access.admin_password}});
    if(!login.ok())throw new Error('Administrator login failed; set CODEARTS_ADMIN_PASSWORD if it has changed');
    const page=await context.newPage();const errors=[];
    page.on('pageerror',e=>errors.push(e.message));
    await page.goto(base+'/#keys');
    await page.locator('#keys .key-value').first().waitFor();
    const findRow=async()=>{
      const inputs=page.locator('#keys .key-value');
      for(let i=0;i<await inputs.count();i++)if(await inputs.nth(i).inputValue()===(expected.slice(0,6)+"..."+expected.slice(-4)))return inputs.nth(i).locator('xpath=ancestor::tr');
      throw new Error('Existing key not found after reload');
    };
    let row=await findRow();
    assert.equal(await row.locator('input').getAttribute('type'),'password');
    await row.getByRole('button',{name:'复制',exact:true}).click();
    await page.waitForFunction(async value=>(await navigator.clipboard.readText())===value,expected);
    const copied=await page.evaluate(()=>navigator.clipboard.readText());
    if(copied!==expected)throw new Error('Clipboard does not contain the full key');
    await row.getByRole('button',{name:'显示',exact:true}).click();
    await row.getByRole('button',{name:'隐藏',exact:true}).waitFor();
    assert.equal(await row.locator('input').getAttribute('type'),'text');if(await row.locator('input').inputValue()!==expected)throw new Error('Reveal did not retrieve full key');
    await row.getByRole('button',{name:'隐藏',exact:true}).click();
    await page.reload();await page.locator('#keys .key-value').first().waitFor();row=await findRow();
    assert.equal(await row.locator('input').getAttribute('type'),'password');
    await row.getByRole('button',{name:'复制',exact:true}).click();
    await page.waitForFunction(async value=>(await navigator.clipboard.readText())===value,expected);
    if(await page.evaluate(()=>navigator.clipboard.readText())!==expected)throw new Error('Copy after reload failed');
    // Exercise legacy clipboard fallback when the modern API rejects access.
    await page.evaluate(async()=>{await navigator.clipboard.writeText('clipboard-fallback-sentinel');navigator.clipboard.writeText=()=>Promise.reject(new Error('fixture clipboard denial'));});
    await row.getByRole('button',{name:'复制',exact:true}).click();
    await page.waitForFunction(async value=>(await navigator.clipboard.readText())===value,expected);
    if(await page.evaluate(()=>navigator.clipboard.readText())!==expected)throw new Error('Fallback copied incorrect value');
    await page.evaluate(async()=>{delete navigator.clipboard.writeText;await navigator.clipboard.writeText('');});
    assert.deepEqual(errors,[]);
    console.log('PASS: existing full-key copy, show/hide, copy after reload, clipboard fallback; no keys logged.');
  } finally {await browser.close();}
})().catch(e=>{console.error(e.message);process.exitCode=1;});
