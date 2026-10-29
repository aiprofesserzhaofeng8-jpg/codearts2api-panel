// Device detection fixtures only; no real credentials or upstream calls.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const os=require('node:os');
const path=require('node:path');
const {execFileSync}=require('node:child_process');
async function chooseAccount(page, id, method='browser') {
  if (await page.locator('#addAccountDialog').evaluate(el=>el.open) && !await page.locator('#accountChoose').isVisible()) await page.locator('#addAccountDialog').waitFor({state:'hidden'});
  if (!await page.locator('#addAccountDialog').evaluate(el=>el.open)) await page.locator('#addAccount').click();
  await page.locator('#'+id).click();
  await page.locator(method==='import'?'#accountMethodImport':'#accountMethodBrowser').click();
  await page.locator('#accountCreateNext').click();
}
(async()=>{
  const browser=await chromium.launch({channel:'chrome',headless:true});
  const fixture=fs.mkdtempSync(path.join(os.tmpdir(),'trae-device-command-'));
  try{
    const ctx=await browser.newContext({viewport:{width:1460,height:1100},permissions:['clipboard-read','clipboard-write']});
    const page=await ctx.newPage(),errors=[];let reads=0;
    const first={client:'Trae CN',source:'/fixture/Trae CN/storage.json',machine_id:'machine-fixture',device_id:'1234567890123456'};
    const second={client:'TRAE SOLO CN',source:'/fixture/TRAE SOLO CN/storage.json',machine_id:'second-machine',device_id:'2222222222222222'};
    page.on('pageerror',e=>errors.push(e.message));
    await ctx.route('**/admin/auth/session',r=>r.fulfill({json:{authenticated:true}}));
    await ctx.route('**/admin/api/**',route=>{
      const u=new URL(route.request().url());let out={ok:true};
      if(u.pathname.endsWith('/state'))out={accounts:[],keys:[],models:[],settings:{pool:{}},stats:{}};
      if(u.pathname.endsWith('/trae-device')){
        reads++;
        if(reads===1)out={devices:[],message:'未找到完整设备信息'};
        else if(reads===2)out={devices:[first],warnings:[]};
        else if(reads===3)out={devices:[first,second],warnings:[]};
        else return route.fulfill({status:503,json:{error:{message:'模拟读取失败'}}});
      }
      if(u.pathname.endsWith('/provider-login')&&route.request().postDataJSON().action==='start')out={session_id:'fixture-flow',url:'https://www.trae.cn/authorization?fixture=1'};
      return route.fulfill({json:out});
    });
    await ctx.route('**/health',r=>r.fulfill({json:{accounts:0,ready:0}}));
    const stateReady=page.waitForResponse(r=>new URL(r.url()).pathname==='/admin/api/state');
    await page.goto((process.env.CODEARTS_TEST_BASE||'http://127.0.0.1:10088')+'/#accounts');await stateReady;
    await chooseAccount(page,'workbuddyLogin');assert.equal(await page.locator('#providerDeviceRead').isVisible(),false);
    await page.locator('#addAccountClose').click();await chooseAccount(page,'traeLogin');
    await page.locator('#providerDeviceCommand').click();
    assert.match(await page.evaluate(()=>navigator.clipboard.readText()),/\$env:APPDATA/);
    await page.locator('#providerDeviceOS').selectOption('unix');await page.locator('#providerDeviceCommand').click();
    const command=await page.evaluate(()=>navigator.clipboard.readText());
    const storage=path.join(fixture,'Trae CN','User','globalStorage');fs.mkdirSync(storage,{recursive:true});
    fs.writeFileSync(path.join(storage,'storage.json'),JSON.stringify({'telemetry.machineId':'machine-fixture','iCubeAuthInfo://icube-dc:1234567890123456':{token:'PRIVATE_CANARY'}}));
    const output=execFileSync('bash',['-c',command],{env:{...process.env,XDG_CONFIG_HOME:fixture},encoding:'utf8'});
    assert.equal(JSON.parse(output)[0].device_id,first.device_id);assert.equal(output.includes('PRIVATE_CANARY'),false);
    await page.locator('#providerDeviceRead').click();await page.waitForFunction(()=>document.querySelector('#providerDeviceStatus').textContent.includes('未找到'));
    assert.equal(await page.locator('#providerDeviceID').inputValue(),'');
    await page.locator('#providerDeviceRead').click();await page.waitForFunction(()=>document.querySelector('#providerDeviceID').value==='1234567890123456');
    assert.match(await page.locator('#providerDeviceStatus').textContent(),/来源/);
    await page.locator('#providerDeviceRead').click();await page.locator('#providerDeviceChoices').waitFor({state:'visible'});
    assert.equal(await page.locator('#providerDeviceID').inputValue(),'');
    await page.locator('#providerDeviceChoice').selectOption('1');assert.equal(await page.locator('#providerMachineID').inputValue(),second.machine_id);
    await page.locator('#providerDeviceRead').click();await page.waitForFunction(()=>document.querySelector('#providerLoginError').textContent.includes('模拟读取失败'));
    await page.locator('#providerDeviceJSON').locator('..').locator('summary').click();
    await page.locator('#providerDeviceJSON').fill('{"machine_id":"machine-fixture","device_id":1234567890123456}');
    await page.locator('#providerDeviceApply').click();await page.waitForFunction(()=>document.querySelector('#providerLoginError').textContent.includes('格式无效'));
    await page.locator('#providerDeviceJSON').fill(output);await page.locator('#providerDeviceApply').click();
    await page.waitForFunction(()=>document.querySelector('#providerDeviceID').value==='1234567890123456');
    assert.equal(await page.locator('#providerDeviceJSON').inputValue(),'');
    await page.locator('#providerLoginStart').click();await page.waitForFunction(()=>document.querySelector('#providerLoginURL').value.includes('fixture=1'));
    assert.equal(await page.locator('#providerDeviceRead').isDisabled(),true);assert.equal(await page.locator('#providerDeviceApply').isDisabled(),true);
    await page.locator('#addAccountClose').click();await chooseAccount(page,'traeLogin');
    await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
    await page.screenshot({path:'artifacts/trae-device-mobile.png'});assert.deepEqual(errors,[]);
    console.log('PASS: OS command copy/execution, no token export, missing/single/multiple device records, source display, explicit selection, JSON paste, request failure and authorization locking.');
  }finally{await browser.close();fs.rmSync(fixture,{recursive:true,force:true});}
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
