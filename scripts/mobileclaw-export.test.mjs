import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
const helper=new URL('./mobileclaw-export.mjs',import.meta.url);
test('export excludes refresh material, preserves source and refuses overwrite/expired credentials',()=>{
 const dir=fs.mkdtempSync(path.join(os.tmpdir(),'mobileclaw-export-test-'));
 try{
  const config=path.join(dir,'config.json'),out=path.join(dir,'credentials.json');
  const payload=Buffer.from(JSON.stringify({exp:Math.floor(Date.now()/1000)+3600})).toString('base64url');
  const token='eyJ.'+payload+'.fixture';
  const original=JSON.stringify({token,userId:'user',userName:'fixture',deviceId:'device',workspaceMode:'personal',refreshToken:'private-refresh',systemFingerprint:'private-fingerprint'});
  fs.writeFileSync(config,original);
  const run=()=>spawnSync(process.execPath,[helper.pathname,'--config',config,'--output',out],{encoding:'utf8'});
  const first=run();assert.equal(first.status,0);assert.equal(fs.readFileSync(config,'utf8'),original);
  assert.equal(fs.readFileSync(out,'utf8').includes('private-'),false);
  assert.equal(first.stdout.includes(token),false);
  if(process.platform!=='win32')assert.equal(fs.statSync(out).mode&0o777,0o600);
  const exported=fs.readFileSync(out,'utf8');assert.notEqual(run().status,0);assert.equal(fs.readFileSync(out,'utf8'),exported);
  fs.unlinkSync(out);fs.writeFileSync(config,original.replace(payload,Buffer.from('{"exp":1}').toString('base64url')));
  assert.notEqual(run().status,0);assert.equal(fs.existsSync(out),false);
 }finally{fs.rmSync(dir,{recursive:true,force:true});}
});
