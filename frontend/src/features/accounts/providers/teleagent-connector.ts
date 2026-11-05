import {t} from '../../../i18n';

const endpoint = 'http://127.0.0.1:18765';
const storageKey = 'codearts2api_teleagent_connector';
type LoginTask = {version:number;provider:string;challenge:string;url:string;expires_at:number};

export function connectorKey(): string {
  try { const key=localStorage.getItem(storageKey) ?? ''; return /^[A-Za-z0-9_-]{43}$/.test(key) ? key : ''; }
  catch { return ''; }
}
export function forgetConnector(): void { try {localStorage.removeItem(storageKey);} catch { /* read below verifies removal */ }
  if(connectorKey())throw new Error(t('无法清除本机连接，请检查浏览器存储权限')); }

async function request(path:string, key:string, body?:unknown, signal?:AbortSignal):Promise<any> {
  let response:Response;
  try {
    response=await fetch(endpoint+path,{method:body===undefined?'GET':'POST',mode:'cors',credentials:'omit',cache:'no-store',redirect:'error',
      headers:{'X-Codearts-Connector':key,...(body===undefined?{}:{'Content-Type':'application/json'})},
      body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.any([AbortSignal.timeout(5000),...(signal?[signal]:[])])});
  } catch(error) {
    if(signal?.aborted)throw error;
    throw new Error(t('本机登录组件未连接。请启动组件，并在浏览器询问时允许访问本地网络。'));
  }
  if(response.status===401)throw new Error(t('连接码无效，请重新配对本机组件'));
  if(!response.ok)throw new Error(t('本机组件拒绝请求，请检查面板地址、任务有效期或等待上一登录窗口关闭'));
  return response.json();
}
export async function pairConnector(raw:string):Promise<void> {
  const key=raw.trim();
  if(!/^[A-Za-z0-9_-]{43}$/.test(key))throw new Error(t('请填写安装组件时显示的连接码'));
  const reply=await request('/status',key);
  if(reply.protocol!==1)throw new Error(t('本机组件版本不兼容，请更新组件'));
  try {localStorage.setItem(storageKey,key);}catch{throw new Error(t('无法保存连接，请允许此站点使用浏览器存储'));}
}
export async function checkConnector():Promise<void> {
  const key=connectorKey();if(!key)throw new Error(t('首次使用请先连接本机登录组件'));
  const reply=await request('/status',key);if(reply.protocol!==1)throw new Error(t('本机组件版本不兼容，请更新组件'));
}
function wait(signal:AbortSignal):Promise<void> {
  return new Promise((resolve,reject)=>{
    if(signal.aborted){reject(new DOMException('Canceled','AbortError'));return;}
    const abort=()=>{clearTimeout(timer);reject(new DOMException('Canceled','AbortError'));};
    const timer=setTimeout(()=>{signal.removeEventListener('abort',abort);resolve();},1000);
    signal.addEventListener('abort',abort,{once:true});
  });
}
export async function connectorLogin(task:LoginTask,signal:AbortSignal):Promise<string> {
  const key=connectorKey();if(!key)throw new Error(t('首次使用请先连接本机登录组件'));
  try {
    await request('/login',key,task,signal);
    while(!signal.aborted && Date.now()<task.expires_at*1000) {
      const reply=await request('/result',key,{challenge:task.challenge},signal);
      if(reply.status==='error')throw new Error(reply.error || t('官方登录未完成，请重试'));
      if(reply.status==='ready') {
        const result=reply.result;
        if(result?.version!==1 || result.provider!=='teleagent' || result.challenge!==task.challenge || !result.credentials)throw new Error(t('登录结果不属于本次任务，请重新选择'));
        const text=JSON.stringify(result);if(new TextEncoder().encode(text).length>256*1024)throw new Error(t('登录结果过长'));
        return text;
      }
      await wait(signal);
    }
    throw new Error(t('登录任务已过期，请重新生成'));
  } finally { await request('/cancel',key,{challenge:task.challenge}).catch(()=>{}); }
}
