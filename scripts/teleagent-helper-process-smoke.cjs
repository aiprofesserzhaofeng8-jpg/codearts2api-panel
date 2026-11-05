// Exercise the actual helper-owned browser process and private profile cleanup.
// DNS for the only navigated host is disabled; no official network is needed.
const assert=require('node:assert/strict'),{chromium}=require('playwright'),fs=require('node:fs/promises'),{join}=require('node:path'),{tmpdir}=require('node:os');
(async()=>{
 if(process.platform!=='linux'){console.log('SKIP: POSIX process lifecycle acceptance runs on Linux.');return;}
 const {captureLogin,LOGIN_URL}=await import('./teleagent-login.mjs');
 const dir=await fs.mkdtemp(join(tmpdir(),'teleagent-process-test-'));
 const quote=s=>"'"+s.replaceAll("'","'\\''")+"'";
 try{
  const executable=process.env.CODEARTS_TEST_BROWSER==='chromium'?chromium.executablePath():'/opt/google/chrome/chrome';
  const wrapper=join(dir,'browser.sh'),argsFile=join(dir,'args'),pidFile=join(dir,'pid');
  await fs.writeFile(wrapper,'#!/bin/sh\nprintf \'%s\\n\' "$@" > '+quote(argsFile)+'\nprintf \'%s\\n\' "$$" > '+quote(pidFile)+'\nexec '+quote(executable)+' --headless=new --disable-gpu '+quote('--host-resolver-rules=MAP www.teleai.com.cn ~NOTFOUND')+' "$@"\n',{mode:0o700});
  const task={version:1,provider:'teleagent',challenge:'A'.repeat(43),url:LOGIN_URL,expires_at:Math.floor(Date.now()/1000)+30},controller=new AbortController();
  const timer=setTimeout(()=>controller.abort(),1500);
  try{await assert.rejects(captureLogin(task,wrapper,controller.signal),/cancel/);}finally{clearTimeout(timer);}
  const args=(await fs.readFile(argsFile,'utf8')).split('\n'),profile=args.find(a=>a.startsWith('--user-data-dir=')).slice('--user-data-dir='.length),pid=Number(await fs.readFile(pidFile,'utf8'));
  assert.ok(args.includes('--remote-debugging-port=0'));assert.ok(args.includes('--remote-debugging-address=127.0.0.1'));assert.ok(!args.includes('--no-sandbox'));
  await assert.rejects(fs.stat(profile),{code:'ENOENT'});assert.throws(()=>process.kill(pid,0),{code:'ESRCH'});
  console.log('PASS: actual helper browser launch, ephemeral debugger port, cancellation, browser process exit and temporary profile deletion; official host DNS blocked.');
 }finally{await fs.rm(dir,{recursive:true,force:true});}
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
