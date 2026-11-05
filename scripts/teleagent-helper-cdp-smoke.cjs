// Real Chromium + the helper's actual CDP observer; upstream traffic is routed
// to local fixture bodies, never to a real login or account endpoint.
const {chromium}=require('playwright'),assert=require('node:assert/strict');
(async()=>{const {capturePageLogin,LOGIN_URL,CALLBACK}=await import('./teleagent-login.mjs');
 const browser=await chromium.launch({headless:true,channel:process.env.CODEARTS_TEST_BROWSER||'chrome'});
 try{const ctx=await browser.newContext(),page=await ctx.newPage();
  const token='tenant:'+Buffer.from('{"alg":"HS256"}').toString('base64url')+'.'+Buffer.from(JSON.stringify({accountId:'fixture-user',exp:2000000000})).toString('base64url')+'.fixture-signature';
  let mode='body',requests=0;
  await ctx.route('**/*',r=>{requests++;const url=r.request().url();
   if(url===LOGIN_URL)return r.fulfill({contentType:'text/html',body:'<script>location.href='+JSON.stringify(CALLBACK+'?code=one-use-fixture')+'</script>'});
   if(url.startsWith(CALLBACK)){
    if(mode==='redirect')return r.fulfill({status:302,headers:{location:'teleai-cowork://auth?token='+encodeURIComponent(token)}});
    if(mode==='pending')return r.fulfill({contentType:'text/html',body:'<html><body>Fixture login is pending</body></html>'});
    return r.fulfill({contentType:'text/html',body:'<html><body><script type="application/json">'+JSON.stringify({token})+'</script>Fixture login</body></html>'});
   }
   return r.abort();
  });
  const session=await ctx.newCDPSession(page),listeners=new Set(),methods=['Network.requestWillBeSent','Network.responseReceived','Network.loadingFinished','Network.loadingFailed','Page.frameRequestedNavigation','Page.frameScheduledNavigation'];
  for(const method of methods)session.on(method,p=>{for(const f of listeners)f(method,p)});
  const cdp={listeners,send:(m,p)=>session.send(m,p)};
  const task=()=>({version:1,provider:'teleagent',challenge:'A'.repeat(43),url:LOGIN_URL,expires_at:Math.floor(Date.now()/1000)+30});
  assert.equal(await capturePageLogin(cdp,task()),token);assert.equal(listeners.size,0);
  await page.goto('about:blank');mode='redirect';assert.equal(await capturePageLogin(cdp,task()),token);assert.equal(listeners.size,0);
  await page.goto('about:blank');mode='pending';const controller=new AbortController();const pending=capturePageLogin(cdp,task(),controller.signal);setTimeout(()=>controller.abort(),350);await assert.rejects(pending,/cancel/);assert.equal(listeners.size,0);
  assert.ok(requests>=6);await ctx.close();console.log('PASS: actual Chromium CDP captures the official callback body and redirect, retains tenant prefix and cleans up on cancellation; all upstream responses intercepted.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
