import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdir, mkdtemp, readFile, stat, rm, writeFile} from 'node:fs/promises';
import {join} from 'node:path';
import {tmpdir, platform, homedir} from 'node:os';
import {EventEmitter} from 'node:events';
import {request as httpRequest} from 'node:http';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {validateTask, clientMetadata, extractTokens, isCallback, observeLogin, CDP, saveResult, readJSON, configFilePath, openApplication, installedMetadata, gatewayKeyFromSource, discoveryLocations, discoverClientMetadata, startBridge, panelOrigin, startupEntry, LOGIN_URL, CALLBACK} from './teleagent-login.mjs';
const token = (account = 'fixture', extra = {}) => 'tenant:' + Buffer.from('{"alg":"HS256"}').toString('base64url') + '.' + Buffer.from(JSON.stringify({accountId: account, exp: 2000000000, ...extra})).toString('base64url') + '.fixture-signature';
const task = () => ({version: 1, provider: 'teleagent', challenge: 'A'.repeat(43), url: LOGIN_URL, expires_at: Math.floor(Date.now()/1000)+300});
const meta = {api_key: 'sk-owned-fixture', device_id: 'original-device', install_id: 'original-install', app_version: '2.7.1'};

test('connector pins a safe panel origin and prepares desktop autostart without shell commands',()=>{
  assert.equal(panelOrigin('https://panel.example/'),'https://panel.example');assert.equal(panelOrigin('http://127.0.0.1:10087'),'http://127.0.0.1:10087');
  for(const v of ['http://panel.example','https://u:p@panel.example','https://panel.example/path','https://panel.example/#key','file:///tmp/a'])assert.throws(()=>panelOrigin(v));
  const linux=startupEntry('linux','/usr/bin/node','/home/one user/helper.mjs','/home/one user/settings.json');
  assert.ok(linux.includes('Type=Application'));assert.ok(linux.includes('"/home/one user/helper.mjs"'));assert.ok(!linux.includes('sudo'));
  const win=startupEntry('win32','C:\\Program Files\\nodejs\\node.exe','C:\\User Files\\helper.mjs','C:\\User Files\\connection.json');
  assert.ok(win.includes('WScript.Shell'));assert.ok(!win.includes('cmd.exe'));assert.throws(()=>startupEntry('win32','C:\\%BAD%\\node.exe','a','b'));
});

test('connector rejects wrong origin, key and host; relays only the matching task in memory',async()=>{
  const origin='http://127.0.0.1:10087',secret='K'.repeat(43);let calls=0,finish;
  const bridge=await startBridge({origin,secret,port:0,login:async()=>{calls++;return new Promise(ok=>{finish=ok;});}});
  const base='http://127.0.0.1:'+bridge.port;
  const call=(path,body,headers={})=>fetch(base+path,{method:body===undefined?'GET':'POST',headers:{Origin:origin,'X-Codearts-Connector':secret,'Content-Type':'application/json',...headers},body:body===undefined?undefined:JSON.stringify(body)});
  try{
    assert.equal((await call('/status',undefined,{Origin:'https://evil.example'})).status,403);
    assert.equal((await call('/status',undefined,{'X-Codearts-Connector':'wrong'})).status,401);
    assert.equal(await new Promise((ok,fail)=>{const r=httpRequest(base+'/status',{headers:{Host:'evil.example',Origin:origin,'X-Codearts-Connector':secret}},response=>{response.resume();ok(response.statusCode);});r.on('error',fail);r.end();}),403);
    assert.equal((await call('/status')).status,200);assert.equal(calls,0);
    const preflight=await fetch(base+'/login',{method:'OPTIONS',headers:{Origin:origin,'Access-Control-Request-Method':'POST','Access-Control-Request-Headers':'content-type,x-codearts-connector','Access-Control-Request-Private-Network':'true'}});
    assert.equal(preflight.status,204);assert.equal(preflight.headers.get('Access-Control-Allow-Origin'),origin);assert.equal(preflight.headers.get('Access-Control-Allow-Private-Network'),'true');
    assert.equal((await call('/login',{...task(),url:'https://evil.example'})).status,400);
    assert.equal((await call('/login',task())).status,200);assert.equal(calls,1);
    assert.equal((await call('/login',task())).status,200);assert.equal(calls,1);
    assert.equal((await call('/login',{...task(),challenge:'B'.repeat(43)})).status,409);
    assert.equal((await call('/result',{challenge:'wrong'})).status,410);
    assert.equal((await (await call('/result',{challenge:task().challenge})).json()).status,'waiting');
    finish({...meta,token:token(),operation_token:'must-not-copy'});await new Promise(ok=>setImmediate(ok));
    const response=await call('/result',{challenge:task().challenge}),body=await response.json();
    assert.equal(response.headers.get('cache-control'),'no-store');assert.equal(body.status,'ready');assert.equal(body.result.credentials.token,token());assert.equal(body.result.credentials.operation_token,undefined);
    assert.equal((await call('/cancel',{challenge:task().challenge})).status,200);
    assert.equal((await call('/result',{challenge:task().challenge})).status,410);
    assert.equal((await call('/stop',{})).status,200);
    await bridge.close(); // Shutdown and an owner cleanup may race safely.
  }finally{finish?.({...meta,token:token()});await bridge.close();}
});

test('connector heartbeat loss cancels the owned browser and discards late results',async()=>{
  let aborted=false;
  const origin='https://panel.example',secret='K'.repeat(43);
  const bridge=await startBridge({origin,secret,port:0,leaseMs:50,login:(_task,signal)=>new Promise(resolve=>signal.addEventListener('abort',()=>{aborted=true;resolve({...meta,token:token()});},{once:true}))});
  const call=(path,body)=>fetch('http://127.0.0.1:'+bridge.port+path,{method:'POST',headers:{Origin:origin,'X-Codearts-Connector':secret,'Content-Type':'application/json'},body:JSON.stringify(body)});
  try{
    await call('/login',task());await new Promise(ok=>setTimeout(ok,150));assert.equal(aborted,true);
    assert.equal((await call('/result',{challenge:task().challenge})).status,410);
  }finally{await bridge.close();}
});

function archiveBytes(files, patch=()=>{}) {
  const tree={files:{}},payload=[];let offset=0;
  for(const [path,text] of Object.entries(files)){
    const parts=path.split('/');let node=tree;
    for(const part of parts.slice(0,-1))node=node.files[part]??= {files:{}};
    const bytes=Buffer.from(text);node.files[parts.at(-1)]={size:bytes.length,offset:String(offset)};payload.push(bytes);offset+=bytes.length;
  }
  patch(tree);
  const json=Buffer.from(JSON.stringify(tree)),header=Buffer.alloc(Math.ceil((json.length+8)/4)*4),size=Buffer.alloc(8);
  size.writeUInt32LE(4,0);size.writeUInt32LE(header.length,4);header.writeUInt32LE(header.length-4,0);header.writeUInt32LE(json.length,4);json.copy(header,8);
  return Buffer.concat([size,header,...payload]);
}
const gatewaySource=(key='sk-gateway-fixture')=>`const host="https://agent.teleai.com.cn/superCowork/sapi";const p={NewApi:{id:"new-api",name:"预置模型",options:{baseURL:host,apiKey:"${key}",headers:h,useSuperAgentAuth:!0}}};const image={newApi:{apiKey:"sk-other-channel"}};`;

test('gateway discovery selects only the signed NewApi provider, never another channel',()=>{
  assert.equal(gatewayKeyFromSource(gatewaySource()),'sk-gateway-fixture');
  assert.equal(gatewayKeyFromSource(gatewaySource().replace('NewApi:','newApi:')),undefined);
  assert.equal(gatewayKeyFromSource(gatewaySource().replace('useSuperAgentAuth:!0','useSuperAgentAuth:false')),undefined);
  assert.equal(gatewayKeyFromSource(gatewaySource().replace('agent.teleai.com.cn','evil.invalid')),undefined);
  assert.throws(()=>gatewayKeyFromSource(gatewaySource()+gatewaySource('sk-conflicting-channel')),/Multiple model gateway/);
});

test('ASAR metadata discovery reads the installed version and original device without account files',async()=>{
  const dir=await mkdtemp(join(tmpdir(),'teleagent-autoconfig-test-'));
  try{
    const app=join(dir,'application'),data=join(dir,'data');
    await mkdir(join(app,'resources'),{recursive:true});await mkdir(join(data,'app-auth'),{recursive:true});
    const archive=join(app,'resources/app.asar');
    await writeFile(archive,archiveBytes({'package.json':JSON.stringify({name:'TeleAgent',version:'3.0.0-rc.4',main:'dist-electron/main-loader.cjs'}),'dist-electron/main-loader.cjs':'require("bytenode");require("./main.jsc");','dist-electron/main-runtime-fixture.js':gatewaySource()}));
    await writeFile(join(data,'app-auth/device-meta.json'),JSON.stringify({deviceId:meta.device_id,installId:meta.install_id}));
    const found=await discoverClientMetadata({appPath:app,dataDir:data});
    assert.deepEqual(found.metadata,{...meta,api_key:'sk-gateway-fixture',app_version:'3.0.0-rc.4'});assert.equal(found.metadata.token,undefined);assert.equal(found.metadata.operation_token,undefined);
    assert.equal(found.appPath,archive);assert.equal(found.devicePath,join(data,'app-auth/device-meta.json'));
    assert.deepEqual(await installedMetadata(archive),{api_key:'sk-gateway-fixture',app_version:'3.0.0-rc.4'});
    if(process.getuid?.()!==0){
      const check=spawnSync(process.execPath,[fileURLToPath(new URL('./teleagent-login.mjs',import.meta.url)),'--check','--app',app,'--data-dir',data],{encoding:'utf8',timeout:5000});
      assert.equal(check.status,0,check.stderr);assert.ok(check.stdout.includes('3.0.0-rc.4'));
      for(const secret of ['sk-gateway-fixture',meta.device_id,meta.install_id])assert.ok(!check.stdout.includes(secret));
      assert.ok(!check.stdout.includes('path (required)')); // No JSON prompt or browser login in check mode.
    }
    await rm(join(data,'app-auth/device-meta.json'));
    await assert.rejects(discoverClientMetadata({appPath:app,dataDir:data}),/Start TeleAgent once/);
    await assert.rejects(discoverClientMetadata({appPath:join(dir,'missing-app'),dataDir:data}),/TeleAgent not found/);
  }finally{await rm(dir,{recursive:true,force:true});}
});

test('ASAR discovery rejects malformed archives, offset escapes, unpacked resources and wrong products',async()=>{
  const dir=await mkdtemp(join(tmpdir(),'teleagent-asar-test-')),path=join(dir,'app.asar');
  const files={'package.json':JSON.stringify({name:'TeleAgent',version:'3.0.0',main:'dist-electron/main.js'}),'dist-electron/main.js':gatewaySource()};
  try{
    await writeFile(path,Buffer.alloc(16));await assert.rejects(openApplication(path),/Invalid application archive/);
    await writeFile(path,archiveBytes(files,tree=>{tree.files['package.json'].offset='9999999999999999';}));await assert.rejects(installedMetadata(path),/Cannot read TeleAgent/);
    await writeFile(path,archiveBytes(files,tree=>{tree.files['dist-electron'].files['main.js'].unpacked=true;}));await assert.rejects(installedMetadata(path),/not recognized/);
    await writeFile(path,archiveBytes({...files,'package.json':JSON.stringify({name:'OtherProduct',version:'3.0.0'})}));await assert.rejects(installedMetadata(path),/Invalid TeleAgent/);
    await writeFile(path,archiveBytes(files));const app=await openApplication(path);
    try{await assert.rejects(app.read('../token.json'),/Invalid client resource path/);}finally{await app.close();}
  }finally{await rm(dir,{recursive:true,force:true});}
});

test('metadata discovery uses bounded known locations and ignores empty environment roots',()=>{
  const home=homedir(),locations=discoveryLocations({home,env:{},os:'linux'});
  assert.ok(locations.apps.includes('/opt/TeleAgent/resources/app.asar'));
  assert.ok(locations.devices.includes(join(home,'.local/share/TeleAgent/app-auth/device-meta.json')));
  assert.ok(locations.devices.every(p=>!p.includes('users/')&&!p.endsWith('token.json')));
  const explicit=discoveryLocations({home,env:{APPDATA:''},appPath:'/explicit/client',dataDir:'/explicit/data'});
  assert.equal(explicit.apps.length,1);assert.equal(explicit.devices.length,1);
});

test('task accepts only the fixed official login and a fresh bounded session', () => {
  validateTask(task());
  for (const change of [{url: 'https://evil.invalid/'}, {url: LOGIN_URL+'&redirect_uri=http://127.0.0.1/'}, {expires_at: 1}, {expires_at: 9999999999}, {challenge: 'x'}, {provider: 'other'}, {version: 2}]) assert.throws(()=>validateTask({...task(), ...change}));
});
test('client metadata does not copy an existing token, identity or portal cookie', () => {
  assert.deepEqual(clientMetadata({auth: {...meta, token: 'old-secret', account_id: 'previous-user', operation_token: 'portal-secret'}}), meta);
  assert.deepEqual(clientMetadata({apiKey: meta.api_key, deviceId: meta.device_id, installId: meta.install_id, appVersion: meta.app_version}), meta);
  for (const change of [{api_key: 'sk-'}, {device_id: ''}, {install_id: 'bad\nvalue'}, {app_version: '2.7.\n1'}, {app_version: ''}]) assert.throws(()=>clientMetadata({...meta, ...change}));
});
test('empty configuration paths and folders never produce a misleading size error', async () => {
  for (const value of ['', '  ', '""', "''", '"  "']) assert.throws(()=>configFilePath(value), /client configuration JSON file path is required/);
  assert.equal(configFilePath('"~/client config.json"'), join(homedir(), 'client config.json'));
  const dir=await mkdtemp(join(tmpdir(),'teleagent-path-test-'));
  try {
    await assert.rejects(readJSON(''), /Enter a JSON file path/);
    await assert.rejects(readJSON(dir), /not a folder/);
    await assert.rejects(readJSON(join(dir,'missing.json')), /file not found/);
    await writeFile(join(dir,'large.json'),' '.repeat(256*1024+1));
    await assert.rejects(readJSON(join(dir,'large.json')), /256 KiB size limit/);
  } finally { await rm(dir,{recursive:true,force:true}); }
});
test('token candidates preserve tenant prefix and reject UPMS, tracking and stale tokens', () => {
  assert.deepEqual(extractTokens('teleai-cowork://auth?token='+encodeURIComponent(token())), [token()]);
  assert.deepEqual(extractTokens(JSON.stringify({token: token()})), [token()]);
  assert.deepEqual(extractTokens('teleai-cowork://auth?token=0123456789abcdef'), []);
  assert.deepEqual(extractTokens(token(undefined, {accountId: undefined, tokenType: 'login.v3.account'})), []);
  assert.deepEqual(extractTokens(token('fixture', {exp: 1})), []);
});
test('callback matching rejects deceptive hosts and paths', () => {
  assert.equal(isCallback(CALLBACK+'?code=fixture'), true);
  for (const url of [CALLBACK+'/extra', CALLBACK.replace('agent.teleai.com.cn', 'agent.teleai.com.cn.evil.invalid'), CALLBACK.replace('https:', 'http:'), CALLBACK.replace('agent.', 'user@agent.')]) assert.equal(isCallback(url), false);
});
test('response observation is restricted and handles redirects, body encoding and cleanup', async () => {
  const cdp = {listeners: new Set(), calls: 0, async send() { this.calls++; return {body: Buffer.from(JSON.stringify({token: token()})).toString('base64'), base64Encoded: true}; }};
  const captured = [], stop = observeLogin(cdp, value => captured.push(value));
  const event = (method, params) => { for (const f of cdp.listeners) f(method, params); };
  for (const url of ['https://www.teleai.com.cn/upms/auth', 'https://track-babel.teleai.com.cn/report', CALLBACK.replace('teleai.com.cn', 'teleai.com.cn.evil.invalid')]) {
    event('Network.responseReceived', {requestId: 'bad', type: 'Document', response: {url, status: 200, headers: {Location: 'teleai-cowork://auth?token='+token()}}});
    event('Network.loadingFinished', {requestId: 'bad'});
  }
  event('Network.responseReceived', {requestId: 'script', type: 'Script', response: {url: CALLBACK, status: 200}});
  event('Network.loadingFinished', {requestId: 'script'});
  assert.equal(cdp.calls, 0); assert.equal(captured.length, 0);
  event('Network.requestWillBeSent', {redirectResponse: {url: CALLBACK+'?code=fixture', headers: {location: 'teleai-cowork://auth?token='+encodeURIComponent(token())}}});
  assert.deepEqual(captured, [token()]);
  event('Network.responseReceived', {requestId: 'good', type: 'Document', response: {url: CALLBACK, status: 200}});
  event('Network.loadingFinished', {requestId: 'good'});
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(cdp.calls, 1); assert.deepEqual(captured, [token(), token()]);
  event('Page.frameRequestedNavigation', {url: 'teleai-cowork://auth?token='+encodeURIComponent(token())});
  assert.equal(captured.length, 3);
  stop(); event('Page.frameRequestedNavigation', {url: 'teleai-cowork://auth?token='+token()}); assert.equal(captured.length, 3);
});
test('canceling observation ignores in-flight bodies and oversized responses', async () => {
  let complete; const cdp = {listeners: new Set(), send() { return new Promise(resolve => { complete = resolve; }); }};
  const captured = [], stop = observeLogin(cdp, t => captured.push(t));
  const event = (m,p) => { for (const f of cdp.listeners) f(m,p); };
  event('Network.responseReceived', {requestId:'x', type:'Fetch', response:{url:CALLBACK,status:200}});
  event('Network.loadingFinished', {requestId:'x'}); stop(); complete({body: token()});
  await new Promise(resolve => setImmediate(resolve)); assert.equal(captured.length, 0);
  assert.deepEqual(extractTokens('x'.repeat(1024*1024+1)+token()), []);
});
test('CDP multiplexes replies and events, then rejects outstanding work on close', async () => {
  class Socket extends EventEmitter { readyState = 1; addEventListener(...args) { this.on(...args); } send(text) { this.sent = JSON.parse(text); } close() { this.readyState = 3; this.emit('close'); } }
  const socket = new Socket(), cdp = new CDP(socket); let event;
  cdp.listeners.add((method, p) => { event = [method, p]; });
  const reply = cdp.send('Network.enable'); socket.emit('message', {data: JSON.stringify({id:socket.sent.id,result:{ok:true}})});
  assert.deepEqual(await reply, {ok:true}); socket.emit('message',{data:JSON.stringify({method:'Page.test',params:{x:1}})}); assert.deepEqual(event,['Page.test',{x:1}]);
  const pending = cdp.send('Page.navigate'); cdp.close(); await assert.rejects(pending); assert.equal(cdp.pending.size,0);
  await assert.rejects(CDP.open('ws://evil.invalid:1234/devtools/page/x', 1234));
});
test('result uses private exclusive storage and contains only this login identity', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'teleagent-helper-test-'));
  try {
    const path = join(dir, 'result.json'); await saveResult(path, task(), {...meta, token:'old-token', operation_token:'old-cookie'}, token());
    const saved = JSON.parse(await readFile(path,'utf8')); assert.equal(saved.credentials.token,token()); assert.equal(saved.credentials.operation_token,undefined); assert.equal(saved.challenge,task().challenge);
    if (platform() !== 'win32') assert.equal((await stat(path)).mode & 0o777, 0o600);
    await assert.rejects(saveResult(path, task(), meta, token()));
    assert.equal((await readJSON(path)).provider, 'teleagent');
    await writeFile(join(dir,'big.json'), ' '.repeat(256*1024+1)); await assert.rejects(readJSON(join(dir,'big.json')));
  } finally { await rm(dir,{recursive:true,force:true}); }
});
