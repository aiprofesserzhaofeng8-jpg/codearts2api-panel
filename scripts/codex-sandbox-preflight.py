#!/usr/bin/env python3
"""Probe native read-only sandbox enforcement without any model/API request."""
import argparse,json,pathlib,secrets,socket,subprocess,tempfile
p=argparse.ArgumentParser(description=__doc__);p.add_argument('--output',type=pathlib.Path);a=p.parse_args();checks={}
with tempfile.TemporaryDirectory(prefix='codearts-codex-sandbox-') as directory:
 root=pathlib.Path(directory);probe=root/'probe.txt';marker=secrets.token_hex(8);probe.write_text(marker);forbidden=root/'must-not-exist'
 def execute(args):
  try:return subprocess.run(['codex','sandbox','-c','sandbox_mode="read-only"','--',*args],cwd=root,capture_output=True,text=True,timeout=15)
  except (OSError,subprocess.TimeoutExpired):return None
 read=execute(['/bin/cat',str(probe)]);checks['read_allowed']=read is not None and read.returncode==0 and read.stdout.strip()==marker
 if checks['read_allowed']:
  write=execute(['/usr/bin/python3','-c','import pathlib,sys;pathlib.Path(sys.argv[1]).write_text("forbidden")',str(forbidden)]);checks['write_blocked']=write is not None and write.returncode!=0 and not forbidden.exists()
  python=execute(['/usr/bin/python3','-c','print("PYTHON_OK")']);checks['python_exec_allowed']=python is not None and python.returncode==0 and python.stdout.strip()=='PYTHON_OK'
  with socket.socket() as listener:
   listener.bind(('127.0.0.1',0));listener.listen(1);port=listener.getsockname()[1]
   with socket.create_connection(('127.0.0.1',port),timeout=2):pass
   net=execute(['/usr/bin/python3','-c','import socket,sys;socket.create_connection(("127.0.0.1",int(sys.argv[1])),timeout=1)',str(port)])
   checks['network_blocked']=net is not None and net.returncode!=0 and checks['python_exec_allowed']
 report={'model_requests':0,'checks':checks,'passed':len(checks)==4 and all(checks.values())}
 if not checks['read_allowed']:report['diagnostic']=(read.stderr[-1200:] if read is not None else 'sandbox executable unavailable or probe timeout')
if a.output:a.output.write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
print(json.dumps(report,ensure_ascii=False));raise SystemExit(0 if report['passed'] else 1)
