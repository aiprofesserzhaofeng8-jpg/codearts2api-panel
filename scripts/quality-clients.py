#!/usr/bin/env python3
import argparse,importlib.util,json,os,pathlib,secrets,subprocess,sys
from quality_common import API,ROOT,save_json,scrub,utc
p=argparse.ArgumentParser();p.add_argument('config',type=pathlib.Path);p.add_argument('output',type=pathlib.Path);p.add_argument('--with-pi-adapter',action='store_true');p.add_argument('--clients',nargs='+',choices=['pi','codex','claude'],default=['pi','codex','claude']);a=p.parse_args();a.config=a.config.resolve();a.output=a.output.resolve();cfg=json.loads(a.config.read_text());api=API(cfg['base'],key=cfg['key']);profile=api.json('/v1/client-profile?model=deepseek-v4.1-flash')
spec=importlib.util.spec_from_file_location('connect',ROOT/'scripts/connect-client.py');connect=importlib.util.module_from_spec(spec);spec.loader.exec_module(connect)
checks=[x for x in json.loads(a.output.read_text()).get('checks',[]) if x['client'] not in a.clients] if a.output.exists() else []
for client in a.clients:
 work=a.config.parent/('client-'+client);work.mkdir(mode=0o700,exist_ok=True);marker='READ_OK_'+secrets.token_hex(8);(work/'probe.txt').write_text(marker+'\n')
 env={**os.environ,'CODEARTS_API_KEY':cfg['key'],'PI_OFFLINE':'1','PI_TELEMETRY':'0'}
 command=connect.build_launch(client,cfg['base'],profile,work/'config',env)
 prompt='Use the available read or command tool to read only probe.txt in the current directory. Reply with the exact text found in that file. Do not guess its contents.'
 if client=='pi':command+=['--mode','json','--print','--no-session','--tools','read','--no-extensions','--no-mcp','--no-skills','--no-context-files','--no-prompt-templates','--thinking','off','--system-prompt','Read the requested fixture using the read tool. Answer briefly.',prompt]
 elif client=='codex':command+=['exec','--ignore-user-config','--skip-git-repo-check','--ephemeral','--sandbox','read-only','--json',prompt]
 else:
  env.pop('CLAUDECODE',None);env['ANTHROPIC_API_KEY']=cfg['key'];env.pop('ANTHROPIC_AUTH_TOKEN',None)
  command+=['--bare','--print','--no-session-persistence','--output-format','stream-json','--verbose','--tools','Read','--allowedTools','Read','--strict-mcp-config','--mcp-config','{"mcpServers":{}}','--system-prompt','Use Read to read only probe.txt in the current directory. Return its exact text.',prompt]
 if client=='pi' and a.with_pi_adapter:command.remove('--no-extensions')
 try:
  proc=subprocess.run(command,cwd=work,env=env,capture_output=True,text=True,timeout=180);events=[]
  for line in proc.stdout.splitlines():
   try:events.append(json.loads(line))
   except ValueError:pass
  if client=='pi':tool=any(e.get('type')=='tool_execution_end' and e.get('toolName')=='read' and not e.get('isError') for e in events)
  elif client=='claude':tool=any(e.get('type')=='user' and any(c.get('type')=='tool_result' and not c.get('is_error') for c in e.get('message',{}).get('content',[]) if isinstance(c,dict)) for e in events)
  else:tool=any(e.get('type')=='item.completed' and e.get('item',{}).get('type')=='command_execution' and e.get('item',{}).get('exit_code')==0 for e in events)
  passed=proc.returncode==0 and tool and marker in proc.stdout
  blocked=not passed and any(t in (proc.stdout+proc.stderr).lower() for t in ('bwrap','namespace','permission denied','notapproved','approval required','sandbox restriction'))
  detail={'client':client,'pi_adapter_enabled':bool(client=='pi' and a.with_pi_adapter),'status':'PASS' if passed else 'BLOCKED' if blocked else 'FAIL','exit_code':proc.returncode,'actual_tool_execution':tool,'marker_returned':marker in proc.stdout,'event_count':len(events)}
  if not passed:detail['diagnostic']=scrub((proc.stderr+'\n'+proc.stdout)[-2500:],(cfg['key'],))
 except subprocess.TimeoutExpired:detail={'client':client,'pi_adapter_enabled':bool(client=='pi' and a.with_pi_adapter),'status':'FAIL','diagnostic':'client exceeded 180s request deadline'}
 checks.append(detail);save_json(a.output,{'checked_at':utc(),'checks':checks});print(client+' '+detail['status'],flush=True)
