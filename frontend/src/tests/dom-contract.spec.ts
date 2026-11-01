// @vitest-environment node
import { it, expect } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
const root=fileURLToPath(new URL('../',import.meta.url));
function files(dir:string):string[]{return readdirSync(dir,{withFileTypes:true}).flatMap(entry=>entry.isDirectory()?files(path.join(dir,entry.name)):[path.join(dir,entry.name)]);}
const source=files(root).filter(file=>!file.includes('/tests/')&&!file.endsWith('.spec.ts')&&!file.includes('/generated/'));
const templates=source.filter(file=>file.endsWith('.vue')).map(file=>readFileSync(file,'utf8')).join('\n');
it('keeps every static control reference unique and available across page components', () => {
 const ids=Array.from(templates.matchAll(/\bid="([^"]+)"/g),match=>match[1]!);expect(new Set(ids).size).toBe(ids.length);
 const scripts=source.filter(file=>file.endsWith('.js')).map(file=>readFileSync(file,'utf8')).join('\n');
 for(const match of scripts.matchAll(/(?:\$|on)\("#([^"\s]+)"/g))expect(ids,match[1]).toContain(match[1]);
});
it('keeps all eight tab/ARIA paths and the native rendering ownership boundary', () => {
 for(const tab of ['accounts','groups','overview','models','keys','logs','audit','settings']) {
  expect(templates).toContain('data-tab="'+tab+'"');expect(templates).toContain('id="tab-'+tab+'"');expect(templates).toContain('aria-labelledby="nav-'+tab+'"');
 }
 expect(templates).not.toMatch(/\bv-html\b|\bonclick=/);
 for(const file of source.filter(file=>file.endsWith('.js')))expect(readFileSync(file,'utf8'),file).not.toMatch(/innerHTML|outerHTML|insertAdjacentHTML|document\.write|\beval\(|new Function\(/);
});
it('keeps resource and API paths same-origin', () => {
 for(const match of templates.matchAll(/<(?:script|link|img)\b[^>]*(?:src|href)="([^"]+)"/g))expect(match[1]).not.toMatch(/^(?:https?:)?\/\//);
 for(const file of source.filter(file=>file.endsWith('.js')))for(const match of readFileSync(file,'utf8').matchAll(/\bapi\(\s*"([^"]+)"/g))expect(match[1]).toMatch(/^\/(?!\/)/);
});
it('does not expose feature-private state to another feature', () => {
 for(const file of source.filter(file=>file.includes('/features/')&&file.endsWith('.js'))) {
  const owner=path.relative(root,file).split(path.sep)[1];
  for(const match of readFileSync(file,'utf8').matchAll(/import\s*\{([^}]+)\}\s*from\s*'([^']+)'/g)) {
   const target=path.resolve(path.dirname(file),match[2]!);const parts=path.relative(root,target).split(path.sep);
   if(parts[0]==='features'&&parts[1]!==owner)expect(match[1],file+' -> '+target).not.toMatch(/\b\w+State\b/);
  }
 }
});
