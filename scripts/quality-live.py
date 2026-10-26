#!/usr/bin/env python3
"""Real upstream protocol/tool and budget acceptance, synthetic input only."""
import argparse,json,pathlib,secrets,time
from quality_common import API,ROOT,current_access,save_json,sse,text_chat,text_messages,text_responses,utc
p=argparse.ArgumentParser();p.add_argument('private_config',type=pathlib.Path);p.add_argument('output',type=pathlib.Path);args=p.parse_args();cfg=json.loads(args.private_config.read_text());api=API(cfg['base'],key=cfg['key']);admin=API(cfg['base'],password=current_access()[1]);admin.login();model='deepseek-v4.1-flash';checks=[]
def record(name,fn):
 start=time.monotonic()
 try:detail=fn();item={'name':name,'passed':True,'seconds':round(time.monotonic()-start,3),**detail}
 except Exception as e:item={'name':name,'passed':False,'error':type(e).__name__+': '+str(e)[:500]}
 checks.append(item);save_json(args.output,{'checked_at':utc(),'checks':checks});print(name+': '+str(item['passed']),flush=True)
def complete(protocol,stream):
 marker='OK_'+secrets.token_hex(4);messages=[{'role':'user','content':'Reply with exactly '+marker+'. No explanation.'}]
 if protocol=='chat':path='/v1/chat/completions';body={'model':model,'messages':messages,'max_tokens':128,'thinking':{'type':'disabled'},'stream':stream}
 elif protocol=='responses':path='/v1/responses';body={'model':model,'input':messages,'max_output_tokens':128,'thinking':{'type':'disabled'},'stream':stream,'store':False}
 else:path='/v1/messages';body={'model':model,'messages':messages,'max_tokens':128,'thinking':{'type':'disabled'},'stream':stream}
 code,raw,headers=api.request(path,body);assert code==200,'HTTP '+str(code)
 if not stream:
  out=json.loads(raw);text={'chat':text_chat,'responses':text_responses,'messages':text_messages}[protocol](out);usage=out.get('usage');finish=out['choices'][0].get('finish_reason') if protocol=='chat' else out.get('status') if protocol=='responses' else out.get('stop_reason')
 else:
  events=sse(raw)
  if protocol=='chat':
   text=''.join(c.get('delta',{}).get('content') or '' for e in events for c in e.get('choices',[]));usage=next((e['usage'] for e in reversed(events) if e.get('usage')),None);finish=next((c['finish_reason'] for e in reversed(events) for c in e.get('choices',[]) if c.get('finish_reason')),None);assert raw.rstrip().endswith('data: [DONE]')
  elif protocol=='responses':
   last=next(e for e in reversed(events) if e['type']=='response.completed');out=last['response'];text=text_responses(out);usage=out.get('usage');finish=out['status'];assert [e['sequence_number'] for e in events]==list(range(len(events)))
  else:
   text=''.join(e.get('delta',{}).get('text','') for e in events);usage={};finish=None
   for e in events:
    if e['type']=='message_start':usage.update(e['message'].get('usage',{}))
    if e['type']=='message_delta':usage.update(e.get('usage',{}));finish=e.get('delta',{}).get('stop_reason')
   assert events[-1]['type']=='message_stop'
 assert marker in text and finish in ('stop','completed','end_turn'), 'missing answer or normal terminal state'
 assert usage and any(v>0 for k,v in usage.items() if isinstance(v,int)), 'missing usage'
 return {'status':code,'finish_reason':finish,'usage':usage,'request_id':headers.get('X-Request-Id') or headers.get('X-Request-ID')}
def tool(protocol):
 marker='FILE_'+secrets.token_hex(8);prompt='Call acceptance_read with path probe.txt. After the tool returns, reply with only its exact text.';schema={'type':'object','properties':{'path':{'type':'string'}},'required':['path'],'additionalProperties':False}
 if protocol=='chat':
  path='/v1/chat/completions';body={'model':model,'messages':[{'role':'user','content':prompt}],'tools':[{'type':'function','function':{'name':'acceptance_read','description':'Read the test fixture file.','parameters':schema}}],'tool_choice':{'type':'function','function':{'name':'acceptance_read'}},'max_tokens':256,'thinking':{'type':'disabled'}}
 elif protocol=='responses':
  path='/v1/responses';body={'model':model,'input':prompt,'tools':[{'type':'function','name':'acceptance_read','description':'Read the test fixture file.','parameters':schema}],'tool_choice':{'type':'function','name':'acceptance_read'},'max_output_tokens':256,'thinking':{'type':'disabled'},'store':False}
 else:
  path='/v1/messages';body={'model':model,'messages':[{'role':'user','content':prompt}],'tools':[{'name':'acceptance_read','description':'Read the test fixture file.','input_schema':schema}],'tool_choice':{'type':'tool','name':'acceptance_read'},'max_tokens':256,'thinking':{'type':'disabled'}}
 body.pop("tool_choice",None) # upstream supports auto tool selection; forced selection is separately recorded as rejected.
 first=api.json(path,body)
 if protocol=='chat':
  message=first['choices'][0]['message'];call=message['tool_calls'][0];assert call['function']['name']=='acceptance_read';arguments=json.loads(call['function']['arguments']);body['messages'] += [message,{'role':'tool','tool_call_id':call['id'],'content':marker}]
 elif protocol=='responses':
  call=next(o for o in first['output'] if o['type']=='function_call');assert call['name']=='acceptance_read';arguments=json.loads(call['arguments']);body['input']=[{'role':'user','content':prompt}]+first['output']+[{'type':'function_call_output','call_id':call['call_id'],'output':marker}]
 else:
  call=next(o for o in first['content'] if o['type']=='tool_use');assert call['name']=='acceptance_read';arguments=call['input'];body['messages'] += [{'role':'assistant','content':first['content']},{'role':'user','content':[{'type':'tool_result','tool_use_id':call['id'],'content':marker}]}]
 assert arguments.get('path')=='probe.txt','wrong file arguments';body.pop('tool_choice',None)
 second=api.json(path,body);answer={'chat':text_chat,'responses':text_responses,'messages':text_messages}[protocol](second);assert marker in answer,'tool result was not consumed'
 return {'tool_name':'acceptance_read','tool_roundtrip':True,'usage_first':first.get('usage'),'usage_second':second.get('usage')}
def budgets():
 result=[]
 for kind,budget in [('requests',{'max_requests':1}),('tokens',{'max_tokens':1}),('usd',{'max_usd':0.00000001})]:
  key,ref=admin.add_key('quality-'+kind,budget);local=API(cfg['base'],key=key)
  try:
   a=local.request('/v1/chat/completions',{'model':model,'messages':[{'role':'user','content':'Reply OK.'}],'max_tokens':64,'thinking':{'type':'disabled'}})[0];b=local.request('/v1/chat/completions',{'model':model,'messages':[{'role':'user','content':'Reply OK.'}],'max_tokens':64,'thinking':{'type':'disabled'}})[0];metadata=local.request('/v1/capabilities')[0]
   assert a==200 and b==402 and metadata==200,'budget statuses '+str((a,b,metadata))
   result.append({'kind':kind,'first':a,'next':b,'metadata':metadata})
  finally:admin.delete_key(ref)
 return {'limits':result,'temporary_keys_removed':True}
for protocol in ('chat','responses','messages'):
 for stream in (False,True):record(protocol+('_stream' if stream else '_buffered'),lambda p=protocol,s=stream:complete(p,s))
for protocol in ('chat','responses','messages'):record(protocol+'_tool_roundtrip',lambda p=protocol:tool(p))
record('budget_limits',budgets)
