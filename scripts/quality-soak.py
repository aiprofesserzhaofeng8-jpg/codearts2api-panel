#!/usr/bin/env python3
"""24h acceptance observer: 1/min health + 1/30min tiny billed completion."""
import argparse,datetime,json,os,pathlib,statistics,subprocess,sys,time
from quality_common import API,ROOT,current_access,save_json,text_chat,utc
p=argparse.ArgumentParser();p.add_argument('config',type=pathlib.Path);p.add_argument('--hours',type=float,default=24);a=p.parse_args();
if a.hours<24:raise SystemExit('Formal acceptance requires at least 24 hours')
cfg=json.loads(a.config.read_text());run=pathlib.Path(cfg['run_dir']);out=run/'soak.json';api=API(cfg['base'],key=cfg['key'])
if out.exists():state=json.loads(out.read_text())
else:state={'started_at':utc(),'start_epoch':time.time(),'duration_hours':a.hours,'samples':0,'health_ok':0,'restart_count':0,'generations':[],'refresh_observed':False,'status':'PENDING','summary':'24 小时观察已启动','rss_samples':[]}
end=state['start_epoch']+a.hours*3600;last_generation=max([g['epoch'] for g in state['generations']],default=0)
before={x['id']:x for x in cfg['before_accounts']}
while True:
 now=time.time();
 if state.get('last_sample_epoch'):state['maximum_sample_gap_sec']=max(state.get('maximum_sample_gap_sec',0),now-state['last_sample_epoch'])
 state['last_sample_epoch']=now
 sample={'time':utc(),'epoch':now}
 try:
  code,raw,_=api.request('/ping',timeout=5);healthy=code==200 and raw=='pong'
 except OSError:healthy=False
 state['samples']+=1;state['health_ok']+=int(healthy);sample['healthy']=healthy
 try:
  pid=int(subprocess.check_output(['systemctl','--user','show','codearts2api-panel','--property=MainPID','--value'],text=True).strip());status=pathlib.Path('/proc')/str(pid)/'status';fields={line.split(':')[0]:line.split(':',1)[1].strip() for line in status.read_text().splitlines() if ':' in line};rss=int(fields['VmRSS'].split()[0])*1024;sample.update(pid=pid,rss_bytes=rss,threads=int(fields['Threads']))
  if state.get('last_pid') and state['last_pid']!=pid:state['restart_count']+=1
  state['last_pid']=pid;state['rss_samples'].append({'epoch':now,'rss_bytes':rss})
 except (OSError,ValueError,KeyError,subprocess.SubprocessError):sample['process_sample_error']=True
 if now-last_generation>=1800 and now<end:
  generation={'epoch':now,'at':utc()}
  try:
   code,raw,headers=api.request('/v1/chat/completions',{'model':'deepseek-v4.1-flash','messages':[{'role':'user','content':'Reply with exactly SOAK_OK.'}],'thinking':{'type':'disabled'},'max_tokens':64},timeout=120)
   result=json.loads(raw);generation.update(status=code,passed=code==200 and 'SOAK_OK' in text_chat(result) and result['choices'][0]['finish_reason']=='stop',usage=result.get('usage'),request_id=headers.get('X-Request-Id') or headers.get('X-Request-ID'))
  except Exception as e:generation.update(passed=False,error=type(e).__name__)
  state['generations'].append(generation);last_generation=now
 # Read immutable file snapshots only; never open the live Store for writing.
 try:
  saved=json.loads((ROOT/'data/state.json').read_text());changed=[]
  for acct in saved['accounts']:
   old=before.get(acct['id']);sess=acct.get('session',{})
   if old and sess.get('source')=='refresh' and sess.get('expires_at')!=old.get('expires_at'):
    success=any(l.get('status',500)<400 and l.get('account')==acct.get('name') and l.get('time',0)>=datetime.datetime.fromisoformat(cfg['started_at']).timestamp() for l in saved.get('logs',[]))
    if success:changed.append({'account_index':list(before).index(acct['id'])+1,'expires_at':sess.get('expires_at'),'source':'refresh','successful_generation_after_test_start':True})
  if changed:state['refresh_observed']=True;state['refresh_evidence']=changed
 except (OSError,ValueError,KeyError):pass
 with (run/'soak-samples.jsonl').open('a') as f:f.write(json.dumps(sample)+'\n')
 elapsed=max(0,now-state['start_epoch']);rate=state['health_ok']/state['samples'];state.update(updated_at=utc(),elapsed_hours=round(elapsed/3600,4),health_success_rate=rate,completed_generations=len(state['generations']))
 if now>=end:
  baseline=[x['rss_bytes'] for x in state['rss_samples'] if state['start_epoch']+600<=x['epoch']<state['start_epoch']+4200];last=[x['rss_bytes'] for x in state['rss_samples'] if x['epoch']>=end-3600];memory_ok=bool(baseline and last) and statistics.median(last)<=statistics.median(baseline)*2+32*1024*1024
  state['memory_ok']=memory_ok;state['status']='PASS' if rate>=.999 and state['restart_count']==0 and state.get('maximum_sample_gap_sec',0)<=120 and state['samples']>=a.hours*60*.99 and memory_ok and all(g['passed'] for g in state['generations']) else 'FAIL';state['summary']=f'满 {a.hours}h；探活 {rate:.3%}；非预期重启 {state["restart_count"]}；生成 {len(state["generations"])} 次；内存门槛={memory_ok}'
 else:
  state['summary']=f'已观察 {elapsed/3600:.2f}/{a.hours}h；探活 {rate:.3%}；生成 {len(state["generations"])} 次；真实续期={state["refresh_observed"]}'
  if state['restart_count'] or any(not g['passed'] for g in state['generations']):state['status']='FAIL'
 save_json(out,state);subprocess.run([sys.executable,str(ROOT/'scripts/render-quality-report.py'),str(run)],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 print(state['summary'],flush=True)
 if now>=end:
  try:
   admin=API(cfg['base'],password=current_access()[1]);admin.login();admin.delete_key(cfg['ref']);state['temporary_key_removed']=True;save_json(out,state)
  except Exception:state['temporary_key_cleanup']='pending administrator availability';save_json(out,state)
  break
 time.sleep(min(60,max(.1,end-time.time())))
