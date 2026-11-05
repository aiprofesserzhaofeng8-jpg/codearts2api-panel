#!/usr/bin/env node
// Standalone local login helper. No npm packages, existing browser profiles,
// password handling or public upload endpoint. Metadata discovery reads only
// TeleAgent's device-meta.json and its installed public application resources.
import {spawn} from 'node:child_process';
import {access, chmod, mkdir, mkdtemp, open, readFile, rm, stat, writeFile} from 'node:fs/promises';
import {createServer} from 'node:http';
import {randomBytes, timingSafeEqual} from 'node:crypto';
import {constants} from 'node:fs';
import {homedir, platform, tmpdir} from 'node:os';
import {dirname, join, resolve, delimiter, isAbsolute} from 'node:path';
import {pathToFileURL, fileURLToPath} from 'node:url';
import {setTimeout as delay} from 'node:timers/promises';

export const LOGIN_URL = 'https://www.teleai.com.cn/teleai-api/upms/public/auth/authorize?client_id=Z2veLLEHhZfq3hb5hWIwqJuPajIuasYi&redirect_uri=https%3A%2F%2Fagent.teleai.com.cn%2FsuperCowork%2Fsapi%2Fcowork%2Fauth';
export const CALLBACK = 'https://agent.teleai.com.cn/superCowork/sapi/cowork/auth';
class LoginError extends Error {}
const MAX_FILE = 256 * 1024, MAX_BODY = 1024 * 1024;

export function validateTask(task, now = Date.now()) {
  if (!task || task.version !== 1 || task.provider !== 'teleagent' || task.url !== LOGIN_URL ||
      typeof task.challenge !== 'string' || !/^[A-Za-z0-9_-]{43}$/.test(task.challenge) ||
      !Number.isSafeInteger(task.expires_at) || task.expires_at * 1000 <= now || task.expires_at * 1000 > now + 630000)
    throw new LoginError('登录任务无效或已过期，请在面板重新生成 / Invalid or expired login task');
  return task;
}

export async function readJSON(path) {
  if (typeof path !== 'string' || !path.trim()) throw new LoginError('未填写 JSON 文件路径 / Enter a JSON file path');
  let info;
  try { info = await stat(path); }
  catch (error) {
    if (error.code === 'ENOENT') throw new LoginError('找不到指定的 JSON 文件，请检查完整路径 / JSON file not found; check the full path');
    if (error.code === 'EACCES' || error.code === 'EPERM') throw new LoginError('无法读取指定的 JSON 文件，请检查文件权限 / Cannot read JSON file; check permissions');
    throw error;
  }
  if (!info.isFile()) throw new LoginError('输入的是文件夹或其他非普通文件，请填写具体 JSON 文件的完整路径 / Select a JSON file, not a folder');
  if (info.size > MAX_FILE) throw new LoginError('JSON 文件超过 256 KiB 大小限制 / JSON file exceeds the 256 KiB size limit');
  const bytes = await readFile(path);
  if (bytes.length > MAX_FILE) throw new LoginError('JSON file exceeds size limit');
  try { return JSON.parse(bytes.toString('utf8').replace(/^\uFEFF/, '')); }
  catch { throw new LoginError('JSON 格式无效 / Invalid JSON'); }
}

export function configFilePath(input) {
  const path = typeof input === 'string' ? input.trim().replace(/^(["'])(.*)\1$/, '$2').trim() : '';
  if (!path) throw new LoginError('未填写客户端配置 JSON 路径。需要先准备含 api_key、device_id、install_id、app_version 的本人客户端配置文件，再重新运行助手。 / A client configuration JSON file path is required');
  return resolve(path.startsWith('~/') || path.startsWith('~\\') ? join(homedir(), path.slice(2)) : path);
}

export function clientMetadata(input) {
  const o = input?.auth ?? input;
  const value = (...keys) => keys.map(k => o?.[k]).find(v => typeof v === 'string' && v.trim())?.trim() ?? '';
  const result = {api_key: value('api_key', 'apiKey', 'gateway_key'), device_id: value('device_id', 'deviceId'),
    install_id: value('install_id', 'installId'), app_version: value('app_version', 'appVersion')};
  if (!/^sk-[^\s\x00-\x1f\x7f]{1,4093}$/.test(result.api_key) ||
      !/^[A-Za-z0-9_.:-]{1,256}$/.test(result.device_id) || !/^[A-Za-z0-9_.:-]{1,256}$/.test(result.install_id) ||
      result.app_version.length > 64 || !/^\d+\.\d+\.\d+(?:[-.][A-Za-z0-9.-]+)?$/.test(result.app_version))
    throw new LoginError('客户端配置缺少有效 api_key、device_id、install_id、app_version / Missing client configuration fields');
  // Never copy an earlier account token, expiry, identity or portal cookies.
  return result;
}

// Bound reads to the ASAR index and selected JavaScript resources. Never
// execute client code, unpack the whole application or touch account tokens.
async function readExact(file,size,position) {
  const bytes=Buffer.alloc(size);let offset=0;
  while(offset<size){
    const read=await file.read(bytes,offset,size-offset,position+offset);
    if(!read.bytesRead)throw new LoginError('客户端资源不完整 / Incomplete client resource');
    offset+=read.bytesRead;
  }
  return bytes;
}
export async function openApplication(archive) {
  const file = await open(archive, 'r');
  try {
    const info = await file.stat();
    if (!info.isFile() || info.size<16) throw new LoginError('客户端安装包索引无效 / Invalid application archive');
    const prefix=await readExact(file,16,0);
    const headerSize = prefix.readUInt32LE(4), payloadSize = prefix.readUInt32LE(8), jsonSize = prefix.readUInt32LE(12);
    if (prefix.readUInt32LE(0) !== 4 || headerSize !== payloadSize + 4 || jsonSize < 2 || jsonSize > 16*1024*1024 || jsonSize+8 > headerSize || headerSize-jsonSize > 12 || 8+headerSize > info.size)
      throw new LoginError('客户端安装包索引无效 / Invalid application archive');
    const header=await readExact(file,jsonSize,16);
    let tree; try { tree=JSON.parse(header.toString('utf8')); } catch { throw new LoginError('客户端安装包索引无效 / Invalid application archive'); }
    const entry = path => {
      if (typeof path !== 'string' || path.split('/').some(p=>!p || p==='.' || p==='..')) throw new LoginError('客户端资源路径无效 / Invalid client resource path');
      let item=tree;
      for(const part of path.split('/')) {
        if (!item?.files || !Object.hasOwn(item.files,part)) return undefined;
        item=item.files[part];
      }
      return item;
    };
    return {close:()=>file.close(),
      scripts:()=>Object.entries(entry('dist-electron')?.files ?? {}).filter(([name,item])=>/^main(?:[-.][A-Za-z0-9_.-]+)?\.(?:c?js|mjs)$/.test(name) && item && !item.files && !item.unpacked && !item.link).map(([name])=>'dist-electron/'+name).slice(0,12),
      async read(path, max=24*1024*1024) {
        const item=entry(path);
        if (!item || item.files || item.unpacked || item.link || !Number.isSafeInteger(item.size) || item.size<0 || item.size>max || typeof item.offset!=='string' || !/^\d{1,16}$/.test(item.offset)) throw new LoginError('客户端资源不可读取或过大 / Client resource unavailable or too large');
        const start=8+headerSize+Number(item.offset);
        if (!Number.isSafeInteger(start) || !Number.isSafeInteger(start+item.size) || start<8+headerSize || start+item.size>info.size) throw new LoginError('客户端资源越界 / Client resource out of bounds');
        const bytes=await readExact(file,item.size,start);
        return bytes.toString('utf8');
      }};
  } catch(error) { await file.close(); throw error; }
}

export function gatewayKeyFromSource(source) {
  if (typeof source!=='string' || !source.includes('https://agent.teleai.com.cn') || !source.includes('/superCowork/sapi')) return undefined;
  const keys=new Set();
  // Match only the NewApi provider's options. Other channels (image, search,
  // telemetry) may have different keys and must not be selected by proximity.
  for(const match of source.matchAll(/["']?NewApi["']?\s*:\s*\{[^{}]{0,1024}\boptions\s*:\s*\{([^{}]{1,8192})\}/g)) {
    const options=match[1];
    if (!/\buseSuperAgentAuth\s*:\s*(?:!0|true)\b/.test(options)) continue;
    const key=options.match(/\bapiKey\s*:\s*["'](sk-[A-Za-z0-9_-]{1,4093})["']/)?.[1];
    if(key)keys.add(key);
  }
  if(keys.size>1)throw new LoginError('客户端存在多个模型网关配置，请用 --config 指定完整配置 / Multiple model gateway configurations');
  return [...keys][0];
}

export async function installedMetadata(archive) {
  const app=await openApplication(archive);
  try {
    let pkg;try{pkg=JSON.parse(await app.read('package.json',MAX_FILE));}catch{throw new LoginError('无法读取 TeleAgent 客户端版本 / Cannot read TeleAgent application metadata');}
    if(typeof pkg.name!=='string' || pkg.name.toLowerCase()!=='teleagent' || typeof pkg.version!=='string' || !/^\d+\.\d+\.\d+(?:[-.][A-Za-z0-9.-]+)?$/.test(pkg.version) || pkg.version.length>64)
      throw new LoginError('所选安装包不是有效的 TeleAgent 客户端 / Invalid TeleAgent installation');
    const scripts=app.scripts();
    if(typeof pkg.main==='string' && /\.(?:c?js|mjs)$/.test(pkg.main) && !scripts.includes(pkg.main))scripts.unshift(pkg.main);
    const keys=new Set();let size=0;
    for(const name of scripts.slice(0,12)){
      let text;try{text=await app.read(name);}catch{continue;}
      size+=Buffer.byteLength(text);if(size>64*1024*1024)break;
      const key=gatewayKeyFromSource(text);if(key)keys.add(key);
    }
    if(keys.size!==1)throw new LoginError('未能识别此版本的模型网关参数，请更新助手或用 --config 指定完整配置 / Model gateway metadata was not recognized');
    return {api_key:[...keys][0],app_version:pkg.version};
  } finally {await app.close();}
}

export function discoveryLocations({home=homedir(),os=platform(),env=process.env,appPath,dataDir}={}) {
  const absolute=items=>[...new Set(items.filter(p=>typeof p==='string' && isAbsolute(p)))];
  const envJoin=(key,...parts)=>env[key] && isAbsolute(env[key]) ? join(env[key],...parts) : undefined;
  const apps=appPath ? [resolve(appPath)] : os==='darwin' ? [
    '/Applications/TeleAgent.app/Contents/Resources/app.asar',join(home,'Applications/TeleAgent.app/Contents/Resources/app.asar')
  ] : os==='win32' ? ['PROGRAMFILES','PROGRAMFILES(X86)','LOCALAPPDATA'].flatMap(key=>[
    envJoin(key,'TeleAgent/resources/app.asar'),envJoin(key,'Programs/TeleAgent/resources/app.asar')
  ]) : ['/opt/TeleAgent/resources/app.asar','/opt/teleagent/resources/app.asar','/usr/share/TeleAgent/resources/app.asar','/usr/share/teleagent/resources/app.asar'];
  const data=dataDir ? [join(resolve(dataDir),'app-auth/device-meta.json')] : [
    join(env.XDG_DATA_HOME && isAbsolute(env.XDG_DATA_HOME) ? env.XDG_DATA_HOME : join(home,'.local/share'),'TeleAgent/app-auth/device-meta.json'),
    envJoin('APPDATA','TeleAgent/app-auth/device-meta.json'),join(home,'.config/TeleAgent/app-auth/device-meta.json'),join(home,'Library/Application Support/TeleAgent/app-auth/device-meta.json')
  ];
  return {apps:absolute(apps),devices:absolute(data)};
}

export async function discoverClientMetadata(options={}) {
  const locations=discoveryLocations(options),apps=[],devices=[];
  let appError;
  for(let path of locations.apps){
    try {
      if((await stat(path)).isDirectory())path=path.endsWith('.app') ? join(path,'Contents/Resources/app.asar') : join(path,'resources/app.asar');
      const metadata=await installedMetadata(path);
      if(!apps.some(a=>a.metadata.api_key===metadata.api_key && a.metadata.app_version===metadata.app_version))apps.push({path,metadata});
    }catch(e){if(e.code!=='ENOENT')appError=e;}
  }
  if(apps.length>1)throw new LoginError('发现多个不同版本的 TeleAgent，请用 --app 指定要使用的客户端目录 / Multiple TeleAgent installations; specify --app');
  if(!apps.length)throw appError instanceof LoginError ? appError : new LoginError('未找到 TeleAgent 官方客户端，请先安装，或用 --app 指定安装目录 / TeleAgent not found; install it or specify --app');
  let deviceError;
  for(const path of locations.devices){
    try{
      const data=await readJSON(path),device_id=data.device_id??data.deviceId,install_id=data.install_id??data.installId;
      if(typeof device_id!=='string' || typeof install_id!=='string' || !/^[A-Za-z0-9_.:-]{1,256}$/.test(device_id) || !/^[A-Za-z0-9_.:-]{1,256}$/.test(install_id))throw new LoginError('TeleAgent 设备信息不完整，请先启动官方客户端 / Incomplete device metadata; start TeleAgent first');
      if(!devices.some(d=>d.metadata.device_id===device_id && d.metadata.install_id===install_id))devices.push({path,metadata:{device_id,install_id}});
    }catch(e){if(e.code!=='ENOENT' && !String(e.message).includes('file not found'))deviceError=e;}
  }
  if(devices.length>1)throw new LoginError('发现多组 TeleAgent 设备信息，请用 --data-dir 指定客户端数据目录 / Multiple device records; specify --data-dir');
  if(!devices.length)throw deviceError instanceof LoginError ? deviceError : new LoginError('未找到 TeleAgent 设备信息，请先启动一次官方客户端，或用 --data-dir 指定数据目录 / Start TeleAgent once or specify --data-dir');
  return {metadata:clientMetadata({...apps[0].metadata,...devices[0].metadata}),appPath:apps[0].path,devicePath:devices[0].path};
}

function accountToken(token, now) {
  if (typeof token !== 'string' || token.length > 32768 || !/^(?:[A-Za-z0-9_-]+:)?[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(token)) return false;
  try {
    const jwt = token.includes(':') ? token.slice(token.indexOf(':') + 1) : token;
    const claims = JSON.parse(Buffer.from(jwt.split('.')[1], 'base64url').toString('utf8'));
    // This is candidate selection, never signature/identity verification. The
    // authenticated panel must call user_info before allowing confirmation.
    return Boolean(claims.accountId ?? claims.account_id ?? claims.uid) && Number.isSafeInteger(claims.exp) && claims.exp * 1000 > now;
  } catch { return false; }
}

export function extractTokens(text, now = Date.now()) {
  if (typeof text !== 'string' || Buffer.byteLength(text) > MAX_BODY) return [];
  const values = new Set();
  for (const m of text.matchAll(/(?:[?&#]token=|["']token["']\s*:\s*["'])([^\s"'<>\\]+)/g)) {
    try { const token = decodeURIComponent(m[1]); if (accountToken(token, now)) values.add(token); } catch { /* malformed encoding */ }
  }
  // JSON replies and HTML embedded JSON may contain the credential directly.
  // Preserve tenant prefixes rather than silently stripping them.
  for (const m of text.matchAll(/(?<![A-Za-z0-9_:-])(?:[A-Za-z0-9_-]+:)?eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/g))
    if (accountToken(m[0], now)) values.add(m[0]);
  return [...values];
}

export function isCallback(url) {
  try { const u = new URL(url); return u.origin + u.pathname === CALLBACK && !u.username && !u.password; }
  catch { return false; }
}

export class CDP {
  constructor(socket) {
    this.socket = socket; this.next = 0; this.pending = new Map(); this.listeners = new Set();
    socket.addEventListener('message', event => {
      if (typeof event.data !== 'string' || event.data.length > MAX_BODY * 3) return;
      let m; try { m = JSON.parse(event.data); } catch { return; }
      if (m.id !== undefined) {
        const slot = this.pending.get(m.id); if (!slot) return;
        clearTimeout(slot.timer); this.pending.delete(m.id);
        if (m.error) slot.reject(new LoginError('浏览器命令失败 / Browser command failed'));
        else slot.resolve(m.result);
      } else if (typeof m.method === 'string') for (const listener of this.listeners) listener(m.method, m.params ?? {});
    });
    socket.addEventListener('close', () => this.close());
    socket.addEventListener('error', () => this.close());
  }
  static async open(url, port) {
    const u = new URL(url);
    if (u.protocol !== 'ws:' || u.hostname !== '127.0.0.1' || Number(u.port) !== port || u.username || u.password)
      throw new LoginError('无效的本机浏览器连接 / Invalid local browser connection');
    const ws = new WebSocket(url);
    try {
      await new Promise((ok, fail) => {
        const timer = setTimeout(() => fail(new LoginError('浏览器连接超时 / Browser connection timeout')), 10000);
        ws.addEventListener('open', () => { clearTimeout(timer); ok(); }, {once: true});
        ws.addEventListener('error', () => { clearTimeout(timer); fail(new LoginError('浏览器连接失败 / Browser connection failed')); }, {once: true});
      });
      return new CDP(ws);
    } catch (e) { ws.close(); throw e; }
  }
  send(method, params = {}, timeoutMs = 10000) {
    return new Promise((resolveCommand, reject) => {
      const id = ++this.next;
      const timer = setTimeout(() => { this.pending.delete(id); reject(new LoginError('浏览器命令超时 / Browser command timeout')); }, timeoutMs);
      this.pending.set(id, {resolve: resolveCommand, reject, timer});
      try { this.socket.send(JSON.stringify({id, method, params})); }
      catch { clearTimeout(timer); this.pending.delete(id); reject(new LoginError('浏览器连接已关闭 / Browser connection closed')); }
    });
  }
  close() {
    for (const slot of this.pending.values()) { clearTimeout(slot.timer); slot.reject(new LoginError('浏览器连接已关闭 / Browser connection closed')); }
    this.pending.clear(); this.listeners.clear();
    if (this.socket.readyState < 2) this.socket.close();
  }
}

// Only the official callback response is inspected. UPMS replies, analytics,
// scripts, other tabs and pre-existing browser profiles are outside this scope.
export function observeLogin(cdp, accept) {
  const bodies = new Set(); let active = true;
  const inspect = text => { if (active) for (const token of extractTokens(text)) accept(token); };
  const location = headers => Object.entries(headers ?? {}).find(([key]) => key.toLowerCase() === 'location')?.[1];
  const listener = (method, p) => {
    if (!active) return;
    if (method === 'Network.requestWillBeSent' && isCallback(p.redirectResponse?.url)) inspect(location(p.redirectResponse.headers));
    if (method === 'Network.responseReceived' && isCallback(p.response?.url) && p.response.status >= 200 && p.response.status < 400) {
      inspect(location(p.response.headers));
      if (['Document', 'XHR', 'Fetch'].includes(p.type) && bodies.size < 64 && Number(p.response.encodedDataLength ?? 0) <= MAX_BODY) bodies.add(p.requestId);
    }
    if (method === 'Network.loadingFailed') bodies.delete(p.requestId);
    if (method === 'Network.loadingFinished' && bodies.delete(p.requestId) && Number(p.encodedDataLength ?? 0) <= MAX_BODY) {
      cdp.send('Network.getResponseBody', {requestId: p.requestId}).then(r => {
        if (typeof r.body !== 'string' || r.body.length > MAX_BODY * 2) return;
        inspect(r.base64Encoded ? Buffer.from(r.body, 'base64').toString('utf8') : r.body);
      }).catch(() => {});
    }
    if (['Page.frameRequestedNavigation', 'Page.frameScheduledNavigation'].includes(method)) {
      try { const u = new URL(p.url); if (u.protocol === 'teleai-cowork:' && u.hostname === 'auth') inspect(p.url); } catch { /* not a deep link */ }
    }
  };
  cdp.listeners.add(listener);
  return () => { active = false; bodies.clear(); cdp.listeners.delete(listener); };
}

async function browserPath(explicit) {
  const windows = platform() === 'win32';
  const names = windows ? [] : ['google-chrome', 'google-chrome-stable', 'chromium', 'chromium-browser', 'microsoft-edge'];
  const candidates = explicit ? [resolve(explicit)] : [
    ...(windows ? [process.env.PROGRAMFILES, process.env['PROGRAMFILES(X86)'], process.env.LOCALAPPDATA].filter(Boolean)
      .flatMap(root => [join(root, 'Google/Chrome/Application/chrome.exe'), join(root, 'Microsoft/Edge/Application/msedge.exe')]) : []),
    ...(platform() === 'darwin' ? ['/Applications/Google Chrome.app/Contents/MacOS/Google Chrome', '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge', join(homedir(), 'Applications/Google Chrome.app/Contents/MacOS/Google Chrome')] : []),
    ...String(process.env.PATH ?? '').split(delimiter).filter(Boolean).flatMap(root => names.map(name => join(root, name)))];
  for (const candidate of candidates) {
    try { if ((await stat(candidate)).isFile()) { await access(candidate, windows ? constants.F_OK : constants.X_OK); return resolve(candidate); } } catch { /* next known executable */ }
  }
  throw new LoginError('未找到 Chrome / Chromium / Edge，可用 --browser 指定路径 / Browser not found');
}

export async function captureLogin(task, executable, signal) {
  const profile = await mkdtemp(join(tmpdir(), 'codearts-teleagent-login-'));
  await chmod(profile, 0o700);
  let child, cdp; let exited = false, failed = false;
  try {
    child = spawn(executable, ['--remote-debugging-address=127.0.0.1', '--remote-debugging-port=0', `--user-data-dir=${profile}`,
      '--no-first-run', '--no-default-browser-check', '--disable-sync', '--disable-background-networking', '--disable-extensions', 'about:blank'], {stdio: 'ignore'});
    child.on('exit', () => { exited = true; }); child.on('error', () => { failed = true; });
    const started = Date.now(); let port;
    while (!port) {
      if (signal?.aborted || exited || failed) throw new LoginError('浏览器启动已取消或失败 / Browser launch canceled or failed');
      if (Date.now() - started > 20000) throw new LoginError('浏览器调试连接启动超时 / Browser startup timed out');
      try {
        const text = await readFile(join(profile, 'DevToolsActivePort'), 'utf8');
        const value = Number(text.split('\n')[0]); if (Number.isInteger(value) && value > 0 && value < 65536) port = value;
      } catch { /* fresh private profile not ready yet */ }
      if (!port) await delay(100);
    }
    const reply = await fetch(`http://127.0.0.1:${port}/json/list`, {signal: AbortSignal.timeout(5000), redirect: 'error'});
    if (!reply.ok) throw new LoginError('无法连接本次登录窗口 / Cannot connect to login window');
    const pages = await reply.json();
    const page = Array.isArray(pages) ? pages.find(p => p.type === 'page' && p.url === 'about:blank') : undefined;
    if (!page) throw new LoginError('找不到本次登录窗口 / Login window not found');
    cdp = await CDP.open(page.webSocketDebuggerUrl, port);
    return await capturePageLogin(cdp, task, signal, () => exited || failed || cdp.socket.readyState !== 1);
  } finally {
    // Browser.close exits this newly spawned instance and its child processes
    // on Windows as well as POSIX. Process signals are a bounded fallback.
    if (cdp?.socket.readyState === 1) await cdp.send('Browser.close', {}, 750).catch(() => {});
    cdp?.close();
    if (child?.pid && !exited) {
      child.kill();
      for (let i = 0; i < 20 && !exited; i++) await delay(100);
      if (!exited) { child.kill('SIGKILL'); await delay(200); }
    }
    await rm(profile, {recursive: true, force: true, maxRetries: 5, retryDelay: 200}).catch(() => {
      console.error('请手动清理临时登录目录 / Remove the temporary login folder: ' + profile);
    });
  }
}

// The same page-level path is exercised with a real Chromium CDP connection
// and intercepted fixture responses in the isolated browser acceptance suite.
export async function capturePageLogin(cdp, task, signal, closed = () => false) {
  validateTask(task);
  const tokens = new Set(), stop = observeLogin(cdp, token => tokens.add(token));
  try {
    await cdp.send('Network.enable', {maxTotalBufferSize: 4 * MAX_BODY, maxResourceBufferSize: MAX_BODY});
    await cdp.send('Page.enable');
    await cdp.send('Page.navigate', {url: task.url});
    console.log('请在新窗口完成官方登录；密码和验证码仅在官方页面填写。\nComplete the official login in the new window.');
    while (!tokens.size) {
      if (signal?.aborted || closed()) throw new LoginError('登录已取消或窗口已关闭 / Login canceled or window closed');
      if (Date.now() >= task.expires_at * 1000) throw new LoginError('登录任务已过期，请重新生成 / Login task expired');
      await delay(150);
    }
    await delay(150);
    if (signal?.aborted || closed() || Date.now() >= task.expires_at * 1000) throw new LoginError('登录已取消或过期 / Login canceled or expired');
    if (tokens.size !== 1) throw new LoginError('登录结果存在多个身份，请重新登录 / Multiple login identities');
    return [...tokens][0];
  } finally { stop(); }
}

export async function saveResult(path, task, metadata, token) {
  validateTask(task);
  if (!accountToken(token, Date.now())) throw new LoginError('登录结果无效 / Invalid account token');
  const credentials = {...clientMetadata(metadata), token};
  // Exclusive creation and private POSIX mode. Never print the credential.
  await writeFile(path, JSON.stringify({version: 1, provider: 'teleagent', challenge: task.challenge, credentials}), {flag: 'wx', mode: 0o600});
}

export const BRIDGE_PORT = 18765;
export function panelOrigin(value) {
  let u;try{u=new URL(value);}catch{throw new LoginError('面板地址无效 / Invalid panel origin');}
  if (u.username || u.password || u.search || u.hash || u.pathname!=='/' ||
      !(u.protocol==='https:' || u.protocol==='http:' && ['127.0.0.1','localhost','[::1]'].includes(u.hostname)))
    throw new LoginError('面板地址须为 HTTPS 或本机 HTTP origin / Use an HTTPS or loopback HTTP panel origin');
  return u.origin;
}
const equalSecret=(a,b)=>typeof a==='string' && typeof b==='string' && Buffer.byteLength(a)===Buffer.byteLength(b) && timingSafeEqual(Buffer.from(a),Buffer.from(b));

// The browser relays the result to its already authenticated panel session.
// This service has no panel administrator cookie, public callback or save API.
export async function startBridge({origin,secret,port=BRIDGE_PORT,login,leaseMs=20000}) {
  origin=panelOrigin(origin);
  if(!/^[A-Za-z0-9_-]{43}$/.test(secret))throw new LoginError('连接码无效 / Invalid connector key');
  let pending=null,worker=null,closing=false,closeBridge;
  const discard=()=>{if(pending){pending.controller.abort();pending.result=null;pending=null;}};
  const json=(res,status,body)=>{res.writeHead(status,{'Content-Type':'application/json','Cache-Control':'no-store','X-Content-Type-Options':'nosniff'});res.end(JSON.stringify(body));};
  const server=createServer(async(req,res)=>{
    // Exact host rejects DNS rebinding; exact Origin rejects arbitrary web pages.
    if(req.headers.host!==`127.0.0.1:${server.address()?.port}` || req.headers.origin!==origin){json(res,403,{error:'Origin or host rejected'});return;}
    res.setHeader('Access-Control-Allow-Origin',origin);res.setHeader('Vary','Origin');
    if(req.method==='OPTIONS'){
      if(!['GET','POST'].includes(req.headers['access-control-request-method']) ||
        String(req.headers['access-control-request-headers']??'').split(',').filter(Boolean).some(h=>!['content-type','x-codearts-connector'].includes(h.trim().toLowerCase()))){json(res,403,{error:'Preflight rejected'});return;}
      res.setHeader('Access-Control-Allow-Methods','GET, POST');res.setHeader('Access-Control-Allow-Headers','Content-Type, X-Codearts-Connector');
      res.setHeader('Access-Control-Allow-Private-Network','true');res.writeHead(204);res.end();return;
    }
    if(!equalSecret(req.headers['x-codearts-connector'],secret)){json(res,401,{error:'Connector pairing required'});return;}
    if(closing){json(res,503,{error:'Connector closing'});return;}
    if(req.method==='GET' && req.url==='/status'){json(res,200,{ok:true,protocol:1});return;}
    if(req.method!=='POST' || !['/login','/result','/cancel','/stop'].includes(req.url)){json(res,404,{error:'Unknown connector operation'});return;}
    if(String(req.headers['content-type']??'').split(';')[0]!=='application/json'){json(res,415,{error:'JSON required'});return;}
    let body;
    try{
      const chunks=[];let size=0;
      for await(const chunk of req){size+=chunk.length;if(size>4096){json(res,413,{error:'Request too large'});return;}chunks.push(chunk);}
      body=JSON.parse(Buffer.concat(chunks).toString('utf8'));
      if(!body || typeof body!=='object' || Array.isArray(body))throw new Error();
    }catch{if(!res.headersSent)json(res,400,{error:'Invalid request'});return;}
    if(req.url==='/stop'){json(res,200,{ok:true});setImmediate(()=>closeBridge());return;}
    if(req.url==='/login'){
      let task;try{task=validateTask(body);}catch{json(res,400,{error:'Invalid or expired task'});return;}
      if(pending){json(res,pending.task.challenge===task.challenge?200:409,{ok:pending.task.challenge===task.challenge,status:'busy'});return;}
      if(worker){json(res,409,{error:'Previous browser is closing; retry'});return;}
      const flow={task,controller:new AbortController(),touched:Date.now(),state:'waiting',result:null,error:null};pending=flow;
      worker=Promise.resolve().then(()=>login(task,flow.controller.signal)).then(credentials=>{
        if(pending!==flow || flow.controller.signal.aborted || task.expires_at*1000<=Date.now())return;
        const metadata=clientMetadata(credentials);
        if(!accountToken(credentials.token,Date.now()))throw new LoginError('授权结果无效，请重新登录 / Invalid authorization result');
        flow.result={version:1,provider:'teleagent',challenge:task.challenge,credentials:{...metadata,token:credentials.token}};flow.state='ready';
      }).catch(error=>{
        if(pending===flow && !flow.controller.signal.aborted){flow.state='error';flow.error=error instanceof LoginError?error.message:'无法完成官方登录，请检查客户端、浏览器和网络 / Official login failed';}
      }).finally(()=>{worker=null;});
      json(res,200,{ok:true,status:'waiting'});return;
    }
    if(!pending || !equalSecret(body.challenge,pending.task.challenge)){json(res,410,{error:'Login task expired or canceled'});return;}
    if(req.url==='/cancel'){discard();json(res,200,{ok:true});return;}
    if(pending.task.expires_at*1000<=Date.now()){discard();json(res,410,{error:'Login task expired'});return;}
    pending.touched=Date.now();
    json(res,200,{status:pending.state,...(pending.result?{result:pending.result}:{}),...(pending.error?{error:pending.error}:{})});
  });
  server.requestTimeout=5000;server.headersTimeout=5000;server.maxConnections=8;
  await new Promise((ok,fail)=>{server.once('error',fail);server.listen(port,'127.0.0.1',ok);});
  const timer=setInterval(()=>{if(pending && (Date.now()-pending.touched>leaseMs || pending.task.expires_at*1000<=Date.now()))discard();},Math.min(1000,leaseMs));timer.unref();
  let closed;
  closeBridge=()=>closed??=(async()=>{closing=true;clearInterval(timer);discard();server.closeAllConnections();await new Promise(done=>server.close(done));await worker;})();
  return {port:server.address().port,close:closeBridge};
}

async function runBridge(config,options={}) {
  const bridge=await startBridge({origin:config.origin,secret:config.secret,login:async(task,signal)=>{
    const found=await discoverClientMetadata({appPath:options['--app'],dataDir:options['--data-dir']});
    const executable=await browserPath(options['--browser']);
    const token=await captureLogin(task,executable,signal);
    return {...found.metadata,token};
  }});
  console.log('登录连接组件已就绪 / Login connector ready: '+config.origin);
  const close=()=>{bridge.close().finally(()=>process.exit(0));};
  process.once('SIGINT',close);process.once('SIGTERM',close);
  return bridge;
}

export function startupEntry(os,node,script,settings) {
  const args=[node,script,'--serve','--settings',settings];
  if(args.some(s=>/[\r\n\0]/.test(s)))throw new LoginError('安装路径无效 / Invalid installation path');
  if(os==='linux'){
    const quote=s=>'"'+s.replace(/[\\"`$]/g,ch=>'\\'+ch).replace(/%/g,'%%')+'"';
    return '[Desktop Entry]\nType=Application\nName=CodeArts2API Login Connector\nExec='+args.map(quote).join(' ')+'\nTerminal=false\nX-GNOME-Autostart-enabled=true\n';
  }
  if(os==='win32'){
    if(args.some(s=>/["%]/.test(s)))throw new LoginError('安装路径含不支持的字符 / Unsupported installation path');
    // WScript starts the process without a console and without a command shell.
    const command=args.map(s=>'"'+s+'"').join(' ').replaceAll('"','""');
    return 'Set shell = CreateObject("WScript.Shell")\r\nshell.Run "'+command+'", 0, False\r\n';
  }
  throw new LoginError('此系统暂不支持自动启动安装，请使用 --serve --origin / Use --serve --origin on this OS');
}

async function installConnector(origin) {
  origin=panelOrigin(origin);
  const os=platform();if(!['linux','win32'].includes(os))throw new LoginError('此系统请使用 --serve --origin / Use --serve --origin on this OS');
  const root=join(homedir(),'.codearts2api-login'),script=join(root,'teleagent-login.mjs'),settings=join(root,'connection.json');
  await mkdir(root,{recursive:true,mode:0o700});
  let config;
  try{config=await readJSON(settings);if(config.origin!==origin)throw new LoginError('组件已连接其他面板，请先停用原连接 / Connector belongs to a different panel');}
  catch(error){if(!String(error.message).includes('file not found'))throw error;config={origin,secret:randomBytes(32).toString('base64url')};await writeFile(settings,JSON.stringify(config),{flag:'wx',mode:0o600});}
  const content=await readFile(fileURLToPath(import.meta.url));await writeFile(script,content,{mode:0o600});
  const startup=os==='linux'?join(homedir(),'.config/autostart/codearts2api-login.desktop'):join(process.env.APPDATA??'','Microsoft/Windows/Start Menu/Programs/Startup/CodeArts2API Login.vbs');
  if(os==='win32' && !isAbsolute(process.env.APPDATA??''))throw new LoginError('缺少用户启动目录 / User startup folder unavailable');
  await mkdir(dirname(startup),{recursive:true});await writeFile(startup,startupEntry(os,process.execPath,script,settings),{mode:0o600});
  // An existing connector may already be serving this pairing. Do not spawn a
  // second browser controller or kill a process merely for occupying the port.
  const ready=async()=>{
    try{const r=await fetch(`http://127.0.0.1:${BRIDGE_PORT}/status`,{headers:{Origin:origin,'X-Codearts-Connector':config.secret},signal:AbortSignal.timeout(1000)});return r.ok && (await r.json()).protocol===1;}catch{return false;}
  };
  if(await ready()){
    await fetch(`http://127.0.0.1:${BRIDGE_PORT}/stop`,{method:'POST',headers:{Origin:origin,'X-Codearts-Connector':config.secret,'Content-Type':'application/json'},body:'{}',signal:AbortSignal.timeout(2000)});
    for(let i=0;i<30 && await ready();i++)await delay(100);
    if(await ready())throw new LoginError('旧组件尚未退出，请稍后重试 / Previous connector has not exited');
  }
  {
    const log=await open(join(root,'connector.log'),'a',0o600);
    try{const child=spawn(process.execPath,[script,'--serve','--settings',settings],{detached:true,windowsHide:true,stdio:['ignore',log.fd,log.fd]});child.on('error',()=>{});child.unref();}finally{await log.close();}
    let up=false;for(let i=0;i<20;i++){if(await ready()){up=true;break;}await delay(200);}
    if(!up)throw new LoginError('组件未启动，可能端口已占用。请检查本机连接组件 / Connector did not start; check local port');
  }
  console.log('安装完成，桌面登录时自动启动。\nInstalled; starts automatically with your desktop session.\n面板 / Panel: '+origin+'\n首次配对连接码 / Pairing key: '+config.secret+'\n只在此面板的组件设置中填写一次。请勿发送到聊天。 / Enter once in this panel connector settings; do not share.');
}

async function uninstallConnector() {
  const root=join(homedir(),'.codearts2api-login'),config=await readJSON(join(root,'connection.json'));
  try{
    const r=await fetch(`http://127.0.0.1:${BRIDGE_PORT}/stop`,{method:'POST',headers:{Origin:panelOrigin(config.origin),'X-Codearts-Connector':config.secret,'Content-Type':'application/json'},body:'{}',signal:AbortSignal.timeout(2000)});
    if(!r.ok)throw new LoginError('运行中的组件不属于此连接，未删除 / Running connector does not match this installation');
  }catch(error){if(error instanceof LoginError)throw error;/* Not running. */}
  const startup=platform()==='linux'?join(homedir(),'.config/autostart/codearts2api-login.desktop'):platform()==='win32' && isAbsolute(process.env.APPDATA??'')?join(process.env.APPDATA,'Microsoft/Windows/Start Menu/Programs/Startup/CodeArts2API Login.vbs'):null;
  if(startup){
    let entry;try{entry=await readFile(startup,'utf8');}catch(e){if(e.code!=='ENOENT')throw e;}
    if(entry && entry===startupEntry(platform(),process.execPath,join(root,'teleagent-login.mjs'),join(root,'connection.json')))await rm(startup);
    else if(entry)throw new LoginError('自动启动文件已改变，请手工检查 / Startup entry changed; inspect it manually');
  }
  // Delete only our named component files, never the official client or panel.
  for(const name of ['connection.json','teleagent-login.mjs','connector.log'])await rm(join(root,name),{force:true});
  console.log('登录组件已卸载。面板中的账号不受影响。 / Login connector removed; panel accounts are preserved.');
}

async function main(args) {
  if (args.includes('--help') || !args.length) {
    console.log('node teleagent-login.mjs --install --origin https://YOUR-PANEL\nnode teleagent-login.mjs --serve --origin https://YOUR-PANEL\nnode teleagent-login.mjs TASK.json [--app PATH] [--data-dir PATH] [--config CLIENT.json] [--browser PATH] [--output RESULT.json]\nnode teleagent-login.mjs --check [--app PATH] [--data-dir PATH]\nNode.js 22.15+ / Chrome, Chromium or Edge. Run on your desktop without sudo.\n默认自动读取本机 TeleAgent 的网关参数、版本和设备信息，无需准备 JSON；先安装并启动一次官方客户端。'); return;
  }
  const [major, minor] = process.versions.node.split('.').map(Number);
  if (major < 22 || major === 22 && minor < 15 || typeof WebSocket !== 'function') throw new LoginError('需要 Node.js 22.15 或更新版本 / Node.js 22.15+ required');
  if (process.getuid?.() === 0) throw new LoginError('请用桌面用户运行，不要使用 sudo / Run as your desktop user without sudo');
  if(args[0]==='--uninstall'){if(args.length!==1)throw new LoginError('无效参数 / Invalid arguments');await uninstallConnector();return;}
  if(['--install','--serve'].includes(args[0])){
    const options={};
    for(let i=1;i<args.length;i+=2){if(!['--origin','--settings','--app','--data-dir','--browser'].includes(args[i]) || !args[i+1] || options[args[i]])throw new LoginError('组件参数无效 / Invalid connector arguments');options[args[i]]=args[i+1];}
    if(args[0]==='--install'){await installConnector(options['--origin']);return;}
    const config=options['--settings']?await readJSON(configFilePath(options['--settings'])):{origin:panelOrigin(options['--origin']),secret:randomBytes(32).toString('base64url')};
    await runBridge(config,options);
    if(!options['--settings'])console.log('首次配对连接码 / Pairing key: '+config.secret);
    return;
  }
  const options = {}, checking=args[0]==='--check', taskPath=checking ? null : resolve(args[0]);
  for (let i = 1; i < args.length; i += 2) {
    if (!['--app', '--data-dir', '--config', '--browser', '--output'].includes(args[i]) || !args[i + 1] || options[args[i]]) throw new LoginError('无效参数，使用 --help / Invalid arguments');
    options[args[i]] = args[i + 1];
  }
  const task = checking ? null : validateTask(await readJSON(taskPath));
  let metadata;
  if(options['--config']) {
    metadata=clientMetadata(await readJSON(configFilePath(options['--config'])));
    console.log('已读取指定客户端配置 / Client configuration loaded');
  } else {
    const discovered=await discoverClientMetadata({appPath:options['--app'],dataDir:options['--data-dir']});
    metadata=discovered.metadata;
    console.log('已自动读取 TeleAgent '+metadata.app_version+' 的网关和设备信息。\nTeleAgent metadata detected: '+metadata.app_version+'\n客户端 / Application: '+discovered.appPath+'\n设备文件 / Device metadata: '+discovered.devicePath);
  }
  if(checking)return;
  const executable = await browserPath(options['--browser']);
  const output = resolve(options['--output'] ?? join(dirname(taskPath), 'teleagent-login-result.json'));
  try { await access(output); throw new LoginError('结果文件已存在，请移动或删除后重试 / Result file already exists'); }
  catch (e) { if (e.code !== 'ENOENT') throw e; }
  const controller = new AbortController(); const cancel = () => controller.abort();
  process.once('SIGINT', cancel); process.once('SIGTERM', cancel);
  try {
    const token = await captureLogin(task, executable, controller.signal);
    if (controller.signal.aborted) throw new LoginError('登录已取消 / Login canceled');
    await saveResult(output, task, metadata, token);
    console.log(`结果已保存 / Result saved: ${output}\n回到面板选择此文件，检查账号后确认保存。完成后删除结果文件。\nSelect this file in the panel, check the account, then confirm. Delete the file afterwards.`);
  } finally { process.off('SIGINT', cancel); process.off('SIGTERM', cancel); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href)
  main(process.argv.slice(2)).catch(e => { console.error(e instanceof LoginError ? e.message : '无法完成登录，请检查文件路径、浏览器及网络 / Login failed; check files, browser and network'); process.exitCode = 1; });
