#!/usr/bin/env python3
"""Native sandbox + real Codex read-tool acceptance; one dedicated limited key."""
import argparse,json,pathlib,subprocess,sys,tempfile
from quality_common import API,ROOT,current_access,private_json,save_json,scrub,utc
p=argparse.ArgumentParser(description=__doc__);p.add_argument('--run-dir',type=pathlib.Path,required=True);a=p.parse_args();run=a.run_dir.resolve()
probe=subprocess.run([sys.executable,str(ROOT/'scripts/codex-sandbox-preflight.py'),'--output',str(run/'codex-sandbox-after.json')],cwd=ROOT,capture_output=True,text=True)
if probe.returncode:print('Native sandbox enforcement still blocked; no billed model test was run.');raise SystemExit(1)
admin=API('http://127.0.0.1:10087',password=current_access()[1]);admin.login();key,ref=admin.add_key('Codex 沙箱修复验收',{'max_requests':20,'max_tokens':80000,'max_usd':.1})
try:
 with tempfile.TemporaryDirectory(prefix='codex-retest-',dir=ROOT/'data/.acceptance') as directory:
  cfg=pathlib.Path(directory)/'live.json';private_json(cfg,{'base':admin.base,'key':key})
  output=run/'clients.json';old=run/'clients-before-sandbox-fix.json'
  if output.exists() and not old.exists():old.write_bytes(output.read_bytes())
  proc=subprocess.run([sys.executable,str(ROOT/'scripts/quality-clients.py'),str(cfg),str(output),'--clients','codex'],cwd=ROOT,capture_output=True,text=True,timeout=240);(run/'codex-retest.log').write_text(scrub(proc.stdout+proc.stderr,(key,)))
 c=next(x for x in json.loads(output.read_text())['checks'] if x['client']=='codex');r=json.loads((run/'results.json').read_text());r['cases']['C03']={'status':c['status'],'detail':'原生只读沙箱权限验证及真实 Codex 读文件工具闭环通过' if c['status']=='PASS' else '沙箱配置后仍未通过真实工具闭环，详见诊断','evidence':'clients.json'};save_json(run/'results.json',r)
 if c['status']=='PASS':
  f=json.loads((run/'findings.json').read_text());resolved=[x for x in f['open'] if x['id']=='Q-02'];f['open']=[x for x in f['open'] if x['id']!='Q-02'];f['resolved'] += [{**x,'status':'RESOLVED','resolution':'加载官方 Ubuntu bwrap profile，原生隔离验证及真实工具闭环通过','evidence':'codex-sandbox-after.json'}for x in resolved];save_json(run/'findings.json',f)
 subprocess.run([sys.executable,str(ROOT/'scripts/render-quality-report.py'),str(run)],cwd=ROOT,check=True)
 print('Codex acceptance:',c['status']);raise SystemExit(0 if c['status']=='PASS' else 1)
finally:admin.delete_key(ref)
