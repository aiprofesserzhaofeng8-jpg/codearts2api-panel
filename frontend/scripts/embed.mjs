import { cp, mkdir, rm, readFile, writeFile, readdir } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
const source = fileURLToPath(new URL('../dist/', import.meta.url));
const target = fileURLToPath(new URL('../../backend/internal/webui/dist/', import.meta.url));
const hash = createHash('sha256');
async function scan(dir) { for (const file of (await readdir(dir, {withFileTypes:true})).sort((a,b)=>a.name.localeCompare(b.name))) {
  if (file.isDirectory()) await scan(dir+'/'+file.name); else { hash.update(file.name); hash.update(await readFile(dir+'/'+file.name)); }
} }
await scan(source);
const id=hash.digest('hex').slice(0,20);
const index=await readFile(source+'/index.html','utf8');
await writeFile(source+'/index.html',index.replace('</head>',`<meta name="frontend-build" content="${id}">\n</head>`));
await writeFile(source+'/build.json', JSON.stringify({id},null,2)+'\n');
await rm(target,{recursive:true,force:true}); await mkdir(target,{recursive:true}); await cp(source,target,{recursive:true});
