#!/usr/bin/env python3
"""Load the verified Ubuntu bwrap userns profile. Default is inspection only.
No sysctl changes and no Codex permission/sandbox bypass flags are used.
"""
import argparse,hashlib,json,os,pathlib,pwd,shutil,subprocess,sys,tempfile,time
ROOT=pathlib.Path(__file__).resolve().parents[1]
p=argparse.ArgumentParser(description=__doc__);p.add_argument('--apply',action='store_true');p.add_argument('--retest',action='store_true',help='After kernel load, rerun native enforcement and Codex read-tool acceptance as the original user');p.add_argument('--profile-source',type=pathlib.Path);a=p.parse_args();manifest=ROOT/'docs/acceptance/CODEX-SANDBOX-FIX.json';metadata=json.loads(manifest.read_text());source=(a.profile_source or ROOT/metadata['profile']).resolve();destination=pathlib.Path('/etc/apparmor.d/bwrap-userns-restrict');parser=shutil.which('apparmor_parser')
if not source.is_file():raise SystemExit('Official profile not found. Install apparmor-profiles and apparmor-utils, then pass --profile-source /usr/share/apparmor/extra-profiles/bwrap-userns-restrict')
digest=hashlib.sha256(source.read_bytes()).hexdigest()
if a.profile_source is None and digest!=metadata['sha256']:raise SystemExit('Staged profile checksum mismatch; refusing to load')
if not parser:raise SystemExit('apparmor_parser is missing; install the distribution apparmor package')
syntax=subprocess.run([parser,'--skip-kernel-load','--skip-read-cache',str(source)],capture_output=True,text=True)
if syntax.returncode:raise SystemExit('Official profile syntax check failed: '+syntax.stderr[-800:])
print('Source:',source);print('SHA256:',digest);print('Destination:',destination);print('Scope: /usr/bin/bwrap and its restricted child profile; global userns restriction stays enabled.')
if not a.apply:print('Inspection only. To apply, run this script with sudo and --apply.');raise SystemExit(0)
if os.geteuid()!=0:raise SystemExit('Loading an AppArmor profile requires root. Use sudo python3 scripts/install-codex-sandbox.py --apply --retest')
backup=None
if destination.exists():
 backup=pathlib.Path('/var/backups')/(destination.name+'.'+time.strftime('%Y%m%dT%H%M%S'));shutil.copy2(destination,backup);os.chmod(backup,0o600)
fd,temp=tempfile.mkstemp(prefix='.codearts-bwrap-',dir=destination.parent)
with os.fdopen(fd,'wb') as f:f.write(source.read_bytes())
os.chmod(temp,0o644);os.replace(temp,destination)
loaded=subprocess.run([parser,'-r',str(destination)],capture_output=True,text=True)
if loaded.returncode:
 if backup:shutil.copy2(backup,destination)
 else:destination.unlink(missing_ok=True)
 raise SystemExit('Profile load failed; configuration file restored. '+loaded.stderr[-1000:])
print('Official AppArmor profile loaded. No reboot is required.')
if a.retest:
 user=os.environ.get('SUDO_USER')
 if not user or user=='root':raise SystemExit('Profile loaded. Run the preflight and retest as your ordinary desktop user.')
 user_info=pwd.getpwnam(user)
 user_path=str(pathlib.Path(user_info.pw_dir)/'.local/bin')+os.pathsep+os.environ.get('PATH','/usr/bin:/bin')
 run_as_user=['runuser','-u',user,'--','/usr/bin/env','PATH='+user_path]
 report=ROOT/metadata['preflight_report'];report.parent.mkdir(parents=True,exist_ok=True)
 command=[*run_as_user,'/usr/bin/python3',str(ROOT/'scripts/codex-sandbox-preflight.py'),'--output',str(report)]
 if subprocess.run(command,cwd=ROOT).returncode:raise SystemExit('Profile loaded, but native enforcement preflight still fails; no model request was sent.')
 raise SystemExit(subprocess.run([*run_as_user,'/usr/bin/python3',str(ROOT/'scripts/retest-codex-acceptance.py'),'--run-dir',str(ROOT/metadata['acceptance_run'])],cwd=ROOT).returncode)
