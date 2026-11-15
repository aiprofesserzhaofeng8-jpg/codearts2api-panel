#!/usr/bin/env node
// Read only the selected official desktop config. Never copy refresh tokens or fingerprints.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import crypto from 'node:crypto';
const args=process.argv.slice(2);
if(args.includes('--help')) { console.log('node mobileclaw-export.mjs --config PATH --app-asar PATH --output PATH\nDefaults locate the Linux desktop installation. Output is private and never overwrites an existing file.');process.exit(0); }
function option(name,fallback){const i=args.indexOf(name);if(i<0)return fallback;if(!args[i+1]||args[i+1].startsWith('--'))throw new Error('Missing '+name);return args[i+1];}
function main(){
 const configPath=option('--config',path.join(os.homedir(),'.config/com.cmss.mobileclaw-desktop/config.json'));
 const asarPath=option('--app-asar','/opt/MobileClaw/resources/app.asar');
 const output=option('--output',path.resolve('mobileclaw-credentials.json'));
 if(fs.statSync(configPath).size>256*1024)throw new Error('Configuration exceeds size limit');
 const d=JSON.parse(fs.readFileSync(configPath,'utf8'));
 if(d.workspaceMode&&d.workspaceMode!=='personal')throw new Error('Switch the official client to the personal workspace first');
 if(!d.token||!d.userId||!d.deviceId)throw new Error('Sign in and bind your cloud account in the official client first');
 let token=d.token;
 if(!token.startsWith('eyJ')){
  const fd=fs.openSync(asarPath,'r');let source;
  try{const head=Buffer.alloc(16);fs.readSync(fd,head,0,16,0);const size=head.readUInt32LE(12);if(size>8*1024*1024)throw new Error('Invalid archive header');const header=Buffer.alloc(size);fs.readSync(fd,header,0,size,16);let entry=JSON.parse(header.toString());for(const part of ['electron','dist','main.cjs'])entry=entry.files[part];if(!entry||entry.size>4*1024*1024)throw new Error('Unsupported desktop archive');const buf=Buffer.alloc(entry.size);fs.readSync(fd,buf,0,buf.length,8+head.readUInt32LE(4)+Number(entry.offset));source=buf.toString();}finally{fs.closeSync(fd);}
  const salt=source.match(/(?:const|var) CRYPTO_SALT = "([^"]+)"/);
  const length=source.match(/(?:const|var) IV_LEN = (\d+)/);
  if(!salt||!length||typeof d.systemFingerprint!=='string')throw new Error('Unsupported desktop credential encryption');
  const raw=Buffer.from(token,'base64'),n=Number(length[1]);
  const key=crypto.createHash('sha256').update(d.systemFingerprint).update(salt[1]).digest();
  const decrypt=crypto.createDecipheriv('aes-256-gcm',key,raw.subarray(0,n));decrypt.setAuthTag(raw.subarray(-16));
  token=Buffer.concat([decrypt.update(raw.subarray(n,-16)),decrypt.final()]).toString();
 }
 const claims=JSON.parse(Buffer.from(token.split('.')[1],'base64url'));
 if(!Number.isFinite(claims.exp)||claims.exp<=Date.now()/1000)throw new Error('Desktop access token expired; reopen the official client and export again');
 const credentials={token,userId:String(d.userId),userName:d.userName||'MobileClaw',deviceId:d.deviceId,workspaceMode:'personal'};
 fs.writeFileSync(output,JSON.stringify(credentials,null,2)+'\n',{flag:'wx',mode:0o600});
 console.log('Exported private credentials to '+output+'\nImport this JSON under MobileClaw in the panel; do not share it. Desktop configuration was not modified.');
}
try{main()}catch{console.error('MobileClaw export failed. Check --help, official-client login, file paths and whether output already exists. No credential values are printed.');process.exitCode=1;}
