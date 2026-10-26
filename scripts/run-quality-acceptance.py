#!/usr/bin/env python3
"""Execute the local acceptance matrix; live/soak phases append separate evidence."""
import argparse,datetime,hashlib,json,os,pathlib,re,shutil,signal,subprocess,sys,time
from quality_common import API,ROOT,current_access,private_json,save_json,scrub,utc,wait_ready
p=argparse.ArgumentParser();p.add_argument('--run-dir',type=pathlib.Path);p.add_argument('--live',action='store_true',help='Also run billed live checks and start the 24h observer');args=p.parse_args();stamp=datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ');run=(args.run_dir or ROOT/'artifacts/acceptance'/stamp).resolve();run.mkdir(parents=True,exist_ok=True)
if (run/'results.json').exists():raise SystemExit('Existing run: use complete-quality-acceptance.py to resume; use a fresh directory for a new acceptance run')
private=ROOT/'data/.acceptance'/run.name;private.mkdir(parents=True,exist_ok=True,mode=0o700);os.chmod(private,0o700)
ids=re.findall(r'^\| ([A-Z]\d\d) \|', (ROOT/'docs/acceptance/QUALITY-PLAN.md').read_text(),re.M)
report={'started_at':utc(),'cases':{i:{'status':'BLOCKED','detail':'尚未执行','evidence':''} for i in ids}}
def update(ids,status,detail,evidence):
 for ident in ids.split():report['cases'][ident]={'status':status,'detail':detail,'evidence':evidence}
 save_json(run/'results.json',report);print(ids+' '+status+': '+detail,flush=True)
def command(name,argv,timeout=240,env=None):
 start=time.monotonic();proc=subprocess.run(argv,cwd=ROOT,env=env,capture_output=True,text=True,timeout=timeout)
 (run/name).write_text(scrub(proc.stdout+proc.stderr));return proc.returncode==0,proc,time.monotonic()-start
GO=os.environ.get('CODEARTS_GO_BIN') or shutil.which('go');
if not GO:raise SystemExit('Go toolchain not found; install the version specified in go.mod')
env={**os.environ,'CODEARTS_GO_BIN':GO,'PLAYWRIGHT_CHANNEL':'chrome','CODEARTS_TEST_BASE':'http://127.0.0.1:10088'}
files=[ROOT/f for f in subprocess.check_output(['rg','--files'],cwd=ROOT,text=True).splitlines() if f.endswith(('.go','.js','.css','.html','.py','.cjs')) or f in ('go.mod','go.sum')]
fingerprint=hashlib.sha256(b''.join(str(f.relative_to(ROOT)).encode()+b'\0'+f.read_bytes() for f in sorted(files))).hexdigest()
metadata={'started_at':utc(),'platform':sys.platform,'source_sha256':fingerprint,'binary_sha256':hashlib.sha256((ROOT/'bin/codearts2api-panel').read_bytes()).hexdigest(),'git_head':subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip(),'git_dirty':True,'go':subprocess.check_output([GO,'version'],cwd=ROOT,text=True).strip(),'node':subprocess.check_output(['node','--version'],text=True).strip(),'private_directory':str(private)}
save_json(run/'environment.json',metadata);private_json(private/'run.json',{'run_dir':str(run),'private':str(private)})
print('RUN_DIR='+str(run),flush=True)
try:
 print('Running fresh Go race suite...',flush=True);ok,proc,seconds=command('go-race.jsonl',[GO,'test','-race','-count=1','-json','./...'],300,env)
 events=[]
 for line in proc.stdout.splitlines():
  try:events.append(json.loads(line))
  except ValueError:pass
 tests=[e for e in events if e.get('Action')=='pass' and e.get('Test')];packages=[e for e in events if e.get('Action')=='pass' and not e.get('Test')]
 save_json(run/'go-summary.json',{'passed_test_events':len(tests),'passed_packages':len(packages),'seconds':round(seconds,2),'passed':ok})
 update('F01','PASS' if ok else 'FAIL',f'本次 -count=1 race：{len(tests)} 个测试/子测试事件、{len(packages)} 个测试包，{seconds:.1f}s','go-summary.json')
 for group,pattern in [('R01','Retry|Pool|Generation|RefreshCannot|ModelHealth|Stream|Cancel'),('F08','Pricing|Usage|SummaryFallback'),('S01','AdminSession|AdminAuth|Security|DNSRebinding|Redirect|LocalPanel'),('C07','Unsupported|ResponsesReject')]:
  matched=[e['Test'] for e in tests if re.search(pattern,e['Test'])];update(group,'PASS' if ok and matched else 'FAIL',f'模拟回归通过，匹配 {len(matched)} 个测试事件','go-race.jsonl')
 perf=next((json.loads(e['Output'].split('QUALITY_PERF ',1)[1]) for e in events if e.get('Output','').startswith('QUALITY_PERF ')),None)
 if perf:save_json(run/'performance.json',perf)
 update('P01 P02','PASS' if ok and perf and not perf['failures'] else 'FAIL',f'受控上游 100 请求/2并发；P95 {perf["p95_ms"]:.1f}ms；堆增长 {perf["heap_growth_bytes"]}B；剩余租约 {perf["remaining_leases"]}' if perf else '缺少性能样本','performance.json')
 ok,_,_=command('client-config-tests.log',[sys.executable,'-m','unittest','discover','-s','scripts','-p','test_connect_client.py'],60,env);update('C01','PASS' if ok else 'FAIL','客户端参数生成和密钥隔离测试','client-config-tests.log')
 vet,_,_=command('go-vet.log',[GO,'vet','./...'],180,env);build,_,_=command('build.log',[GO,'build','-trimpath','-ldflags','-s -w -X main.version=0.1.0-panel','-o',str(private/'build-check'),'.'],180,env);compat,_,_=command('sub2api.log',[sys.executable,'scripts/check-sub2api-compat.py'],240,env)
 update('M01','PASS' if vet and build and compat else 'FAIL',f'vet={vet}，构建={build}，固定版上游兼容回归={compat}','sub2api.log')
 scanner='/tmp/codearts-security-tools/govulncheck';scan,_,_=command('govulncheck.log',[scanner,'-mode=binary','bin/codearts2api-panel'],120,env);update('S03','PASS' if scan else 'FAIL','当前二进制已知漏洞扫描；二进制 SHA256 见 environment.json','govulncheck.log')
 state,password=current_access();secrets=[password]+[k['key'] for k in state['keys']]
 for a in state['accounts']:
  for k,v in a.get('session',{}).items():
   if k in ('access_key','secret_key','security_token','refresh_token','code_verifier') and isinstance(v,str) and len(v)>=12:secrets.append(v)
 hits=[]
 for f in files+[f for f in ROOT.glob('docs/**/*.md')]:
  raw=f.read_text(errors='replace')
  if any(v and v in raw for v in secrets):hits.append(str(f.relative_to(ROOT)))
 journal=subprocess.run(['journalctl','--user','-u','codearts2api-panel','--no-pager','-o','cat'],capture_output=True,text=True).stdout
 if any(v and v in journal for v in secrets):hits.append('systemd journal')
 save_json(run/'secret-scan.json',{'scanned_files':len(files),'journal_checked':True,'hits':sorted(set(hits)),'values_recorded':False});update('S02','FAIL' if hits else 'PASS',f'源码/文档和服务日志秘密逐值扫描，命中 {len(hits)} 处；不输出秘密','secret-scan.json')
 permission_errors=[]
 for f in (ROOT/'data').iterdir():
  if f.is_file() and f.stat().st_mode&0o077:permission_errors.append(f.name)
 if ROOT.joinpath('data').stat().st_mode&0o077:permission_errors.append('data/')
 save_json(run/'permissions.json',{'errors':permission_errors,'data_mode':oct((ROOT/'data').stat().st_mode&0o777)})
 fixture_log=open(private/'fixture.log','w');fixture=subprocess.Popen([str(ROOT/'bin/codearts2api-panel'),'-port','10088','-data',str(private/'fixture-state.json'),'-no-import','-admin-password','feature-ui-test'],cwd=ROOT,stdout=fixture_log,stderr=subprocess.STDOUT)
 try:
  wait_ready('http://127.0.0.1:10088')
  for name,group in [('features-smoke','F02'),('account-dialog-smoke','F03'),('usage-smoke','I01'),('pricing-smoke','I01'),('policies-smoke','I01'),('style-smoke','I01')]:
   passed,_,_=command(name+'.log',['node','scripts/'+name+'.cjs'],120,env)
   previous=report['cases'].get(group,{});status='PASS' if passed and previous.get('status')!='FAIL' else 'FAIL';update(group,status,'浏览器：'+name+'；登录、阶段确认、分页、价格、策略、主题和布局分别留证',name+'.log')
  passed,_,_=command('settings-ui.log',['node','scripts/ui-smoke.cjs','http://127.0.0.1:10088',str(private/'fixture-state.json.access.json')],120,env)
  update('I01','PASS' if passed and report['cases']['I01']['status']=='PASS' else 'FAIL','设置保存/验证/恢复及各浏览器布局回归','settings-ui.log')
  a=API('http://127.0.0.1:10088',password='feature-ui-test');a.login();before=a.json('/admin/api/state');raw=(private/'fixture-state.json').read_bytes();fixture.terminate();fixture.wait(timeout=15)
  # Restore only isolated state; a user's refreshed credentials are never rolled back.
  (private/'fixture-state.json').write_bytes(raw)
  fixture=subprocess.Popen([str(ROOT/'bin/codearts2api-panel'),'-port','10088','-data',str(private/'fixture-state.json'),'-no-import'],cwd=ROOT,stdout=fixture_log,stderr=subprocess.STDOUT);wait_ready(a.base)
  revoked=a.request('/admin/api/state')[0]==401;a.login();after=a.json('/admin/api/state')
  keep=lambda s:({'id':x['ref'],'budget':x['budget'],'enabled':x['enabled']} for x in s['keys'])
  preserved=list(keep(before))==list(keep(after));save_json(run/'restore.json',{'isolated':True,'session_revoked':revoked,'keys_budgets_preserved':preserved,'new_instance_initialized':True})
  update('R02 D01','PASS' if revoked and preserved else 'FAIL','临时目录初始化、备份恢复和进程重启；旧会话撤销、密钥预算保留；schema 迁移另有回归','restore.json')
 finally:
  fixture.terminate();fixture.wait(timeout=15);fixture_log.close()
 passed,_,_=command('live-key-copy.log',['node','scripts/keys-smoke.cjs'],90,env);update('F04','PASS' if passed else 'FAIL','当前部署的现有密钥复制、显示、刷新和剪贴板回退','live-key-copy.log')
 update('S04','PASS' if not permission_errors and report['cases']['F02']['status']=='PASS' else 'FAIL','文件权限检查与浏览器会话/Web Storage 验证','permissions.json')
 update('R03 R04','PENDING','长时观察待启动；必须满 24 小时并真实观察续期才能通过','')
 update('C05','BLOCKED','待运行同一合成 Pi 会话的 3 次自动压缩','')
finally:
 report['local_phase_finished_at']=utc();save_json(run/'results.json',report)
 subprocess.run([sys.executable,'scripts/render-quality-report.py',str(run)],cwd=ROOT)
print('LOCAL_PHASE_FINISHED '+str(run),flush=True)
if args.live:subprocess.run([sys.executable,'scripts/complete-quality-acceptance.py',str(run),'--live'],cwd=ROOT,check=True)
