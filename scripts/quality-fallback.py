#!/usr/bin/env python3
"""Real primary truncation -> real fallback using a non-refreshing credential snapshot."""
import argparse,copy,datetime,json,os,pathlib,secrets,subprocess,time
from quality_common import API,ROOT,current_access,private_json,save_json,text_chat,utc,wait_ready
p=argparse.ArgumentParser();p.add_argument('config',type=pathlib.Path);p.add_argument('output',type=pathlib.Path);a=p.parse_args();cfg=json.loads(a.config.read_text());state,password=current_access();now=datetime.datetime.now(datetime.timezone.utc);eligible=[x for x in state['accounts'] if x.get('enabled') and datetime.datetime.fromisoformat(x['session']['expires_at'].replace('Z','+00:00'))>now+datetime.timedelta(minutes=10)]
if not eligible:save_json(a.output,{'passed':False,'blocked':'no credential snapshot has more than 10 minutes validity'});raise SystemExit(0)
account=copy.deepcopy(max(eligible,key=lambda x:x['session']['expires_at']));account.update(paused=False,cooldown_until=0,failures=0)
settings=copy.deepcopy(state['settings']);settings.update(auto_refresh=False,write_back_to_ide=False,auto_claim=False,force_model='',allow_anonymous=False,max_retries=1,request_timeout_sec=240)
p1={'context_window':65536,'max_output_tokens':8192,'reserve_tokens':16384,'keep_recent_tokens':12000,'trigger_tokens':45000,'summary_enabled':True,'summary_retry':True,'summary_max_tokens':256,'disable_summary_thinking':True,'source':'acceptance controlled primary output cap','summary_fallback_model':'glm-5.3'}
p2={**p1,'context_window':32768,'max_output_tokens':4096,'reserve_tokens':8192,'keep_recent_tokens':2048,'trigger_tokens':20000,'summary_max_tokens':4096,'disable_summary_thinking':False,'summary_fallback_model':''}
key='sk-ca-'+secrets.token_hex(24);file=a.config.parent/'fallback-state.json';snapshot={'version':2,'accounts':[account],'keys':[{'key':key,'name':'isolated fallback acceptance','enabled':True,'budget':{'max_requests':2,'max_tokens':60000}}],'settings':settings,'model_policies':{'deepseek-v4.1-flash':p1,'glm-5.3':p2},'model_prices':{'glm-5.3':{'currency':'USD','input':1,'output':2,'cached':.1,'source':'Acceptance calculation fixture, not vendor pricing'}}}
private_json(file,snapshot);log=open(a.config.parent/'fallback-server.log','w');server=subprocess.Popen([str(ROOT/'bin/codearts2api-panel'),'-port','10089','-data',str(file),'-no-import'],cwd=ROOT,stdout=log,stderr=subprocess.STDOUT)
try:
 wait_ready('http://127.0.0.1:10089');api=API('http://127.0.0.1:10089',key=key);admin=API(api.base,password=password);admin.login()
 history='\n'.join(f'Completed milestone {i}: implemented account/model isolation, request conversion, audit records, JSON validation and rollback check. Preserve USD billing, secret redaction, ownership boundaries and pending release tasks for module {i}.' for i in range(120))
 body={'model':'deepseek-v4.1-flash','max_tokens':8192,'messages':[{'role':'system','content':'You are a context summarization assistant. Produce a structured checkpoint with Goal, Completed, Constraints, and Next Steps. Include at least 500 words of actionable detail.'},{'role':'user','content':history}]}
 code,raw,_=api.request('/v1/chat/completions',body,timeout=250);entries=admin.json('/admin/api/logs?limit=1')['logs'];entry=entries[-1] if entries else {};attempts=entry.get('attempt_details',[])
 passed=code==200 and bool(text_chat(json.loads(raw))) and len(attempts)==2 and attempts[0].get('finish_reason')=='length' and attempts[1].get('model')=='glm-5.3' and attempts[1].get('finish_reason')=='stop' and attempts[0].get('account')==attempts[1].get('account') and all(x.get('usage',{}).get('known') for x in attempts)
 safe_attempts=[{k:v for k,v in x.items() if k in ('number','model','finish_reason','usage','estimated_cost')} for x in attempts]
 save_json(a.output,{'checked_at':utc(),'passed':passed,'status':code,'attempts':safe_attempts,'same_account':len(attempts)==2 and attempts[0].get('account')==attempts[1].get('account'),'total_usage':entry.get('usage'),'estimated_cost':entry.get('estimated_cost'),'price_note':'GLM uses a declared test calculation price, not vendor price','refresh_disabled':True,'production_policy_changed':False,'error':json.loads(raw).get('error') if code>=400 else None});print('Real fallback passed='+str(passed),flush=True)
finally:
 server.terminate();server.wait(timeout=15);log.close();file.unlink(missing_ok=True)
