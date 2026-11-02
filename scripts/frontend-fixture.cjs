// Synthetic data only. Shared by production UI and before/after visual acceptance.
const fixture=()=>({today:'2026-11-02',accounts:[{id:'fixture-wb',name:'测试账号 <img src=x onerror=window.injected=true>',provider:'workbuddy',scope:'cn',source:'browser',enabled:true,paused:false,status:'ready',max_in_flight:2,expires_at:1796000000,expires_in:100000,has_refresh:true,failures:0,last_used:1793500000,last_err:'',pool:{state:'ready',in_flight:0},daily_claim_supported:true,growth_tasks_supported:true}],models:[{id:'workbuddy/cn/deepseek-v4.1-flash',name:'DeepSeek <img src=x onerror=window.injected=true>',provider:'workbuddy',scope:'cn',context:128000,max_output:8192,source:'workbuddy',upstream_id:'deepseek-v4.1-flash',description:'Synthetic model'}],keys:[{ref:'fixture-ref',key:'masked-key',name:'测试密钥',enabled:true,group_id:'',created_at:1793500000,last_used:0,requests:0,budget:{max_requests:0,max_tokens:0,max_usd:0},budget_usage:{requests:0,tokens:0,usd:0}}],groups:[],providers:[],provider_errors:{},settings:{pool:{}},stats:{}});
async function mock(context,{authenticated=true,onRequest}={}){
 await context.route('**/auth/**',async route=>{
  const path=new URL(route.request().url()).pathname;
  if(path==='/auth/login')authenticated=true;
  if(path==='/auth/logout')authenticated=false;
  await route.fulfill({json:{authenticated,user:authenticated?{id:'builtin:admin',username:'admin',role:'admin'}:undefined}});
 });
 await context.route('**/admin/**',async route=>{const request=route.request(),url=new URL(request.url());let data={ok:true};
  if(onRequest){const handled=await onRequest(route,url,request);if(handled)return;}
  if(url.pathname.endsWith('/state'))data=fixture();
  if(url.pathname.endsWith('/credits'))data={accounts:[{id:'fixture-wb',ok:true,credit:{available:true,total:100,remaining:80,used:20}}]};
  if(url.pathname.endsWith('/usage'))data={page:1,pages:1,total:0,logs:[],stats:{},analysis:{}};
  if(url.pathname.endsWith('/logs'))data={logs:[]};
  if(url.pathname.endsWith('/audit'))data={total:51,events:[]};
  if(url.pathname.endsWith('/pricing'))data={prices:{}};
  if(url.pathname.endsWith('/model-policies'))data={policies:{}};
  await route.fulfill({json:data});
 });
 await context.route('**/health',route=>route.fulfill({json:{service:'codearts2api-panel',status:'ok',accounts:1,ready:1,models:1}}));
}
module.exports={fixture,mock};
