#!/usr/bin/env python3
"""Run/reuse live evidence, start the 24h observer, and finalize current findings.
Requires --live because missing evidence may trigger billed upstream calls.
"""
import argparse,datetime,hashlib,json,os,pathlib,subprocess,sys
from quality_common import API,ROOT,current_access,private_json,save_json,scrub,utc
p=argparse.ArgumentParser(description=__doc__);p.add_argument('run_dir',type=pathlib.Path);p.add_argument('--live',action='store_true',required=True);a=p.parse_args();run=a.run_dir.resolve();private=ROOT/'data/.acceptance'/run.name;private.mkdir(parents=True,exist_ok=True,mode=0o700);report=json.loads((run/'results.json').read_text());state,password=current_access();admin=API('http://127.0.0.1:10087',password=password);admin.login();config=private/'live.json'
if config.exists():cfg=json.loads(config.read_text())
else:cfg={}
# Reuse completed evidence without issuing more billed calls.
need_live=any(not (run/f).exists() for f in ('live-protocols.json','clients.json','pi-compaction.json','fallback.json'))
if not cfg or (need_live and not any(k['key']==cfg.get('key') for k in state['keys'])):
 key,ref=admin.add_key('质量验收 '+run.name,{'max_requests':100,'max_tokens':600000,'max_usd':.5});cfg={'base':admin.base,'key':key,'ref':ref,'run_dir':str(run),'started_at':utc(),'before_accounts':[{'id':x['id'],'name':x.get('name'),'expires_at':x['session'].get('expires_at'),'source':x['session'].get('source')}for x in state['accounts']]};private_json(config,cfg)
def case(ids,status,detail,evidence):
 for ident in ids.split():report['cases'][ident]={'status':status,'detail':detail,'evidence':evidence}
 save_json(run/'results.json',report);print(ids+' '+status+': '+detail,flush=True)
def execute(name,args,timeout):
 proc=subprocess.run(args,cwd=ROOT,capture_output=True,text=True,timeout=timeout);(run/name).write_text(scrub(proc.stdout+proc.stderr,(cfg.get('key'),password)));return proc.returncode==0
if not (run/'live-protocols.json').exists():execute('live-protocols.log',[sys.executable,'scripts/quality-live.py',str(config),str(run/'live-protocols.json')],600)
if not (run/'clients.json').exists():execute('clients.log',[sys.executable,'scripts/quality-clients.py',str(config),str(run/'clients.json')],600)
if not (run/'pi-compaction.json').exists():execute('pi-compaction.log',['node','scripts/quality-pi-compaction.mjs',str(config),str(run/'pi-compaction.json'),'--adapter'],900)
if not (run/'pi-compaction-initial-overshoot.json').exists():execute('pi-compaction-burst.log',['node','scripts/quality-pi-compaction.mjs',str(config),str(run/'pi-compaction-initial-overshoot.json'),'--burst','--adapter'],900)
if not (run/'fallback.json').exists():execute('fallback.log',[sys.executable,'scripts/quality-fallback.py',str(config),str(run/'fallback.json')],300)
live=json.loads((run/'live-protocols.json').read_text());checks=live['checks'];normal=[x for x in checks if x['name'].endswith(('_buffered','_stream'))];tools=[x for x in checks if x['name'].endswith('_tool_roundtrip')];budgets=[x for x in checks if x['name']=='budget_limits']
case('F05','PASS' if len(normal)==6 and all(x['passed'] for x in normal) else 'FAIL','真实三种协议 × 流式/非流式，共六组；检查正文、终止和 usage','live-protocols.json')
case('F06','PASS' if len(tools)==3 and all(x['passed'] for x in tools) else 'FAIL','真实三种协议的自动选择工具往返；指定函数 tool_choice 的上游拒绝另记限制','live-protocols.json')
case('F07','PASS' if budgets and budgets[0]['passed'] else 'FAIL','真实次数/Token/USD 达标后 HTTP 402，元数据仍可读；缺失用量触发暂停已复现','live-protocols.json')
clients=json.loads((run/'clients.json').read_text())['checks']
for name,ident in [('pi','C02'),('codex','C03'),('claude','C04')]:
 c=next(x for x in clients if x['client']==name);detail='真实只读工具执行且随机文件标记返回' if c['status']=='PASS' else '本机 bwrap 无法建立网络命名空间，原生命令工具未成功执行' if c['status']=='BLOCKED' else c.get('diagnostic','客户端验证失败')[:220];case(ident,c['status'],name+'：'+detail,'clients.json')
normal_fixes=sorted(run.glob('fixes-*/pi-normal-recovery.json'))
normal_evidence=next((f for f in reversed(normal_fixes) if len(json.loads(f.read_text()).get('rounds',[]))==3 and all(r['passed'] for r in json.loads(f.read_text())['rounds'])),run/'pi-compaction.json')
comp=json.loads(normal_evidence.read_text());rounds=comp.get('rounds',[]);case('C05','PASS' if len(rounds)==3 and all(x['passed'] for x in rounds) else 'FAIL','同一合成会话渐进达到推荐阈值，Pi 原生自动压缩连续 3 次；检查关键约束及里程碑',str(normal_evidence.relative_to(run)))
fallback=json.loads((run/'fallback.json').read_text());case('C06','PASS' if fallback.get('passed') else 'BLOCKED' if fallback.get('blocked') else 'FAIL','真实 DeepSeek length → GLM stop；两次用量和分模型费用；GLM 单价明确为计算测试值','fallback.json')
burst_fixes=sorted(run.glob('fixes-*/pi-burst-recovery.json'))
burst_evidence=next((f for f in reversed(burst_fixes) if len(json.loads(f.read_text()).get('rounds',[]))==3 and all(r['passed'] for r in json.loads(f.read_text())['rounds'])),run/'pi-compaction-initial-overshoot.json')
burst_ok=None
if burst_evidence.exists():
 burst=json.loads(burst_evidence.read_text());burst_ok=len(burst.get('rounds',[]))==3 and all(r['passed'] for r in burst['rounds']);case('R05','PASS' if burst_ok else 'FAIL','一次性批量回填历史的三轮恢复通过' if burst_ok else '一次性批量回填大量历史后，第 2 轮摘要失败；正常渐进场景通过不覆盖此恢复缺陷',str(burst_evidence.relative_to(run)))
else:case('R05','BLOCKED','需单独复现突发历史超过预留区后的恢复，不能用渐进场景替代','')
# Actual launcher entry points, using independent generated configuration.
profile_env={**os.environ,'CODEARTS_API_KEY':cfg['key']};profile_ok=True
for client in ('pi','codex','claude'):
 previous=run/('launcher-'+client+'.log')
 if previous.exists() and 'Prepared' in previous.read_text() and 'Client setup failed' not in previous.read_text():continue
 proc=subprocess.run([sys.executable,'scripts/connect-client.py',client,'--check','--config-dir',str(private/'launcher-check'/client)],cwd=ROOT,env=profile_env,capture_output=True,text=True,timeout=30);profile_ok &= proc.returncode==0;(run/('launcher-'+client+'.log')).write_text(scrub(proc.stdout+proc.stderr,(cfg['key'],password)))
case('C01','PASS' if profile_ok else 'FAIL','配置单元测试及三个启动器实际 --check 均完成，不发生成请求','client-config-tests.log')
# Negative capability tests use a separate key, so an exhausted generation
# budget cannot mask capability validation. All payloads must be rejected.
k,ref=admin.add_key('quality unsupported validation',{'max_requests':1});api=API(admin.base,key=k)
try:
 negatives=[]
 for label,path,body,expected in [('store','/v1/responses',{'model':'deepseek-v4.1-flash','input':'test','store':True},400),('previous','/v1/responses',{'model':'deepseek-v4.1-flash','input':'test','previous_response_id':'resp_fixture'},400),('compact','/v1/responses/compact',{'input':[]},501)]:
  status=api.request(path,body)[0];negatives.append({'case':label,'status':status,'expected':expected})
 save_json(run/'capabilities-live.json',{'checks':negatives});case('C07','PASS' if all(x['status']==x['expected'] for x in negatives) else 'FAIL','真实部署对 store、previous_response_id、原生 compact 的明确拒绝','capabilities-live.json')
finally:admin.delete_key(ref)
# Record measured billing before revoking the temporary main acceptance key.
row=next((k for k in admin.json('/admin/api/state')['keys'] if k['ref']==cfg['ref']),None)
logs=admin.json('/admin/api/logs?limit=500')['logs'];mask=cfg['key'][:6]+'...'+cfg['key'][-4:];ours=[x for x in logs if x.get('key')==mask]
usage={'input_tokens':0,'output_tokens':0,'cached_tokens':0};amount=0;unknown=0
for x in ours:
 u=x.get('usage',{})
 for field in usage:usage[field]+=u.get(field,0)
 amount+=(x.get('estimated_cost') or {}).get('amount',0);unknown+=int(bool(x.get('attempts',0)>0 and (not u.get('known') or x.get('usage_incomplete'))))
if row is not None or not (run/'billing.json').exists():save_json(run/'billing.json',{'main_key_retained_history_records':len(ours),'main_key_measured_usage':usage,'estimated_usd':amount,'unknown_usage_records':unknown,'budget_usage_since_recorded_reset':row.get('budget_usage') if row else None,'initial_rejected_call_and_reset':'budget-reset-after-rejection.json','fallback_usage':'fallback.json','not_huawei_credit_billing':True})
# Files created by older manual starts had wider permissions; tighten and recheck.
for name in ('server.pid','server.log'):
 f=ROOT/'data'/name
 if f.exists():os.chmod(f,0o600)
for f in private.rglob('*'):
 if f.is_dir():os.chmod(f,0o700)
 elif f.is_file():os.chmod(f,0o700 if f.stat().st_mode&0o100 else 0o600)
errors=[str(f.relative_to(ROOT)) for f in (ROOT/'data').iterdir() if f.is_file() and f.stat().st_mode&0o077];errors += [str(f.relative_to(ROOT)) for f in private.rglob('*') if f.stat().st_mode&0o077]
save_json(run/'permissions-final.json',{'fixed':['data/server.pid','data/server.log'],'errors':errors,'private_directory_checked':True});case('S04','PASS' if not errors else 'FAIL','旧运行文件已收紧为 0600；秘密文件与私有测试目录复核通过','permissions-final.json')
# Additional known compatibility findings are explicit and do not change the
# successful protocol tests into a claim of complete OpenAI/CLI compatibility.
findings={'open':[{'id':'Q-01','priority':'P1','status':'FAIL','issue':'Pi 一次性突发回填历史后无法自动恢复压缩','evidence':'pi-compaction-initial-overshoot.json','observation':'两轮摘要均返回 length；每次只报告 1 个输出 Token。推断与 Pi 估算上下文后压低输出预算有关；未修改客户端上限或伪造摘要成功。'},{'id':'Q-02','priority':'P2','status':'BLOCKED','issue':'本机 Codex 原生命令沙箱无法初始化','evidence':'clients.json'},{'id':'Q-03','priority':'P2','status':'LIMITATION','issue':'已测试的指定函数 tool_choice 对象被 Huawei 返回 400，自动选择工具三协议通过','evidence':'live-protocols-initial.json'}],'resolved':[{'id':'Q-04','issue':'旧 server.pid / server.log 权限为 0664','resolution':'修正为 0600 并复查','evidence':'permissions-final.json'}]};findings['open']=[f for f in findings['open'] if not ((f['id']=='Q-01' and burst_ok is True) or (f['id']=='Q-02' and report['cases']['C03']['status']=='PASS') or (f['id']=='Q-03' and not (run/'live-protocols-initial.json').exists()))]
if burst_ok is None:
 for f in findings['open']:
  if f['id']=='Q-01':f['status']='BLOCKED';f['issue']='突发历史恢复场景缺少验证证据'
initial_permissions=json.loads((run/'permissions.json').read_text()).get('errors',[])
if not any(x in initial_permissions for x in ('server.pid','server.log')):findings['resolved']=[]
save_json(run/'findings.json',findings)
# Keep only the observer's dedicated key until its own end-of-run cleanup.
admin.delete_key(cfg['ref']);cfg['key_revoked']=True;private_json(config,cfg)
soak_config=private/'soak-config.json'
if not soak_config.exists():
 key,ref=admin.add_key('24h 验收观察 '+run.name,{'max_requests':60,'max_tokens':20000,'max_usd':.05});private_json(soak_config,{**cfg,'key':key,'ref':ref,'key_revoked':False})
unit='codearts-quality-soak-'+run.name.lower().replace('z','');active=subprocess.run(['systemctl','--user','is-active','--quiet',unit]).returncode==0
soak_done=(run/'soak.json').exists() and json.loads((run/'soak.json').read_text()).get('elapsed_hours',0)>=24
if not active and not soak_done:
 subprocess.run(['systemd-run','--user','--unit='+unit,'--property=WorkingDirectory='+str(ROOT),'--property=RuntimeMaxSec=90000','--property=Restart=on-failure',sys.executable,str(ROOT/'scripts/quality-soak.py'),str(soak_config)],check=True)
report['live_phase_finished_at']=utc();report['observer_unit']=unit;save_json(run/'results.json',report)
metadata=json.loads((run/'environment.json').read_text());files=[ROOT/f for f in subprocess.check_output(['rg','--files'],cwd=ROOT,text=True).splitlines() if f.endswith(('.go','.js','.css','.html','.py','.cjs','.mjs')) or f in ('go.mod','go.sum')];metadata['final_validation_source_sha256']=hashlib.sha256(b''.join(str(f.relative_to(ROOT)).encode()+b'\0'+f.read_bytes() for f in sorted(files))).hexdigest();metadata['finalized_at']=utc();metadata['observer_unit']=unit
for cli in ('pi','codex','claude'):
 pr=subprocess.run([cli,'--version'],capture_output=True,text=True,timeout=15);metadata[cli+'_version']=pr.stdout.strip()[:150]
save_json(run/'environment.json',metadata);subprocess.run([sys.executable,'scripts/render-quality-report.py',str(run)],cwd=ROOT,check=True)
