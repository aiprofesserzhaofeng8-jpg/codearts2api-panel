#!/usr/bin/env python3
"""U6: real packaged binary and offline upgrade/recovery in temporary directories.

Replaces only the systemd transport with a local subprocess. No user service,
real account, real client configuration or remote upstream is accessed.
"""
import argparse
from contextlib import redirect_stdout
import http.cookiejar
import io
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'deploy'))
import panelctl as ops


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


class ProcessService:
    def __init__(self, root, password):
        self.root, self.password, self.process = root, password, None
        self.port = json.loads((root/'installation.json').read_text())['port']

    def running(self):
        return self.process is not None and self.process.poll() is None

    def stop(self):
        if self.running():
            self.process.terminate()
            self.process.wait(timeout=15)

    def start(self):
        binary=(self.root/'current').resolve()/'codearts2api-panel'
        self.process=subprocess.Popen([str(binary),'-host','127.0.0.1','-port',str(self.port),
            '-data',str(self.root/'data/state.json'),'-no-import','-no-refresh'],cwd=self.root,
            env={'PATH':os.environ['PATH'],'CODEARTS_ADMIN_PASSWORD':self.password},
            stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)

    def verify(self, unit, binary, system=False):
        assert self.running(), 'temporary process not running'
        assert Path(f'/proc/{self.process.pid}/exe').resolve()==binary, 'wrong temporary binary'
        assert str(self.root/'data/state.json').encode() in Path(f'/proc/{self.process.pid}/cmdline').read_bytes(), 'wrong state argument'


def request(service, path, key=None, password=None):
    opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    base=f'http://127.0.0.1:{service.port}'
    if password:
        opener.open(urllib.request.Request(base+'/auth/login',data=json.dumps({'username':'admin','password':password}).encode(),headers={'Content-Type':'application/json','Origin':base}),timeout=3).close()
    req=urllib.request.Request(base+path,headers={'Authorization':'Bearer '+key} if key else {})
    with opener.open(req,timeout=3) as response: return json.load(response)


def ready(service):
    for _ in range(100):
        if not service.running(): raise AssertionError('temporary service failed to start')
        try:
            request(service,'/health');return
        except OSError: time.sleep(.05)
    raise AssertionError('temporary service health timed out')


def run(args):
    archive=args.archive.resolve();old_binary=args.old_binary.resolve()
    args.output.mkdir(parents=True,exist_ok=True)
    evidence={'kind':'U6_isolated_package_acceptance','real_systemd':False,'real_process':True,'real_upstream':False,'checks':[]}
    with tempfile.TemporaryDirectory(prefix='codearts-u6-package-') as folder:
        temp=Path(folder);root=temp/'managed';(root/'releases/v0.3.18').mkdir(parents=True)
        old=root/'releases/v0.3.18'
        shutil.copy2(old_binary,old/'codearts2api-panel')
        (old/'manifest.json').write_text(json.dumps({'version':'v0.3.18','schema':10,'os':'linux','arch':'amd64'}))
        (root/'current').symlink_to('releases/v0.3.18');(root/'data').mkdir();(root/'backups').mkdir()
        (root/'installation.json').write_text(json.dumps({'unit':'u6-isolated.service','port':free_port(),'scope':'user'}))
        key='sk-u6-'+secrets.token_hex(20);password=secrets.token_urlsafe(24)
        legacy={'version':10,'accounts':[],'keys':[{'key':key,'enabled':True,'name':'synthetic','budget':{'max_requests':100},'budget_usage':{'requests':2,'tokens':74}}],
                'groups':[{'id':'g-u6','name':'fixture','account_ids':[],'allowlist_enabled':False,'model_ids':[],
                    'default_model':'shared-model-1','client_default_model':'shared-model-1',
                    'model_mappings':[{'name':'shared-model-1','sources':['workbuddy/cn/shared-v1','trae/cn/shared-v1']}]}],
                'settings':{'auto_refresh':False,'auto_claim':False,'rate_limit_enabled':False},'stats':{},'logs':[]}
        (root/'data/state.json').write_text(json.dumps(legacy));os.chmod(root/'data/state.json',0o600)
        service=ProcessService(root,password)
        try:
            service.start();ready(service);service.stop()
            old_state=(root/'data/state.json').read_bytes()
            # Real read-only CLI report and conflict diagnostics must not write input.
            staging=temp/'package';ops.unpack(archive,staging,'release',args.sha256)
            meta=ops.check_release(staging);assert meta['schema']==11
            assert (staging/'panelctl.py').read_bytes()==(ROOT/'deploy/panelctl.py').read_bytes(), 'package deployment tool differs from tested source'
            assert (staging/'UNIFIED-MODELS.md').is_file()
            binary=staging/'codearts2api-panel'
            report=ops.model_migration_precheck(binary,root/'data/state.json')
            assert report['ready'] and report['definitions']==1
            assert (root/'data/state.json').read_bytes()==old_state
            conflict=json.loads(old_state);other={**conflict['groups'][0],'id':'g-conflict','model_mappings':[{'name':'shared-model-1','sources':['workbuddy/cn/other-v1']}]}
            conflict['groups'].append(other);conflict_path=temp/'conflict.json';conflict_path.write_text(json.dumps(conflict))
            result=subprocess.run([str(binary),'-check-state-migration',str(conflict_path)],capture_output=True,text=True,timeout=10)
            assert result.returncode==2 and not json.loads(result.stdout)['ready']
            invalid=subprocess.run([str(binary),'-migrate-state-dir',str(root/'data')],capture_output=True,timeout=10)
            assert invalid.returncode==1 and '迁移参数组合无效' in invalid.stderr.decode()
            evidence['checks']+=['package digest and executable version','read-only migration precheck','conflict exit 2','incomplete argument exit 1 with diagnostic']
            service.start();ready(service)
            upgrade=ops.parser().parse_args(['upgrade','--root',str(root),'--archive',str(archive),'--sha256',args.sha256,'--migrate-schema'])
            with patch.object(ops,'Service',return_value=service),patch.object(ops,'verify_process',side_effect=service.verify):
                with redirect_stdout(io.StringIO()): ops.upgrade(upgrade)
            ready(service)
            definitions=request(service,'/admin/api/unified-models',password=password)
            assert len(definitions['models'])==1
            model=definitions['models'][0];assert model['name']=='shared-model-1' and len(model['sources'])==2
            state=json.loads((root/'data/state.json').read_text())
            assert state['version']==11 and state['keys'][0]['budget_usage']['tokens']==74
            assert len(state['groups'][0]['unified_models'])==1
            evidence['checks']+=['actual schema 10 to 11 managed upgrade','key budget preserved','fixed group grants preserved','real process identity and schema 11 health']
            service.stop();service.start();ready(service)
            again=request(service,'/admin/api/unified-models',password=password)
            assert again['models'][0]['id']==model['id']
            service.stop()
            migrated=(root/'data/state.json').read_bytes()
            # Baseline schema-10 executable rejects schema 11 and does not write.
            rejected=subprocess.run([str(old_binary),'-data',str(root/'data/state.json'),'-no-import','-no-refresh','-host','127.0.0.1','-port',str(free_port())],env={'PATH':os.environ['PATH']},capture_output=True,timeout=10)
            assert rejected.returncode!=0 and (root/'data/state.json').read_bytes()==migrated
            evidence['checks']+=['restart retains definition identity','actual old binary rejects schema 11 unchanged']
            # Rehearse every data/release switch boundary with the real candidate.
            for phase in range(7):
                case=temp/f'crash-{phase}';(case/'releases').mkdir(parents=True)
                shutil.copytree(old,case/'releases/v0.3.18')
                dest=case/'releases'/meta['version']
                if phase>0: shutil.copytree(staging,dest)
                (case/'current').symlink_to('releases/v0.3.18')
                (case/'installation.json').write_text(json.dumps({'unit':'u6-isolated.service','port':free_port(),'scope':'user'}))
                data=case/'data';original=case/'data-before-schema11-1';candidate=case/'data-schema11-2'
                data.mkdir();(data/'state.json').write_bytes(old_state)
                ops.model_migration_copy(binary,data,candidate)
                record={'from':10,'to':11,'phase':'prepared','old_release':str(case/'releases/v0.3.18'),'new_release':str(dest),'original_data':str(original),'candidate_data':str(candidate)}
                if phase>=2:(case/'current.next').symlink_to(dest.relative_to(case))
                if phase>=3:data.rename(original)
                if phase>=4:candidate.rename(data)
                if phase>=5:(case/'current.next').replace(case/'current')
                if phase>=6:record['phase']='starting'
                (case/'schema-migration.json').write_text(json.dumps(record))
                recovery=ProcessService(case,password)
                recover_args=ops.parser().parse_args(['recover-schema','--root',str(case),'--apply','--archive',str(archive),'--sha256',args.sha256])
                try:
                    with patch.object(ops,'Service',return_value=recovery),patch.object(ops,'verify_process',side_effect=recovery.verify):
                        with redirect_stdout(io.StringIO()):ops.recover_schema(recover_args);ops.recover_schema(recover_args)
                    ready(recovery)
                    assert request(recovery,'/admin/api/unified-models',password=password)['models'][0]['name']=='shared-model-1'
                    assert (original/'state.json').read_bytes()==old_state
                finally:recovery.stop()
            evidence['checks']+=['7 crash boundaries recovered forward using real binary','repeated recovery is a no-op','pre-upgrade snapshot preserved']
            # Explicit offline restore only into a new isolated directory.
            backup=next((root/'backups').glob('before-*.tar.gz'));restored=temp/'restored'
            ops.unpack(backup,restored,'backup');assert ops.state_schema(restored/'state.json')==10
            evidence['checks'].append('offline backup restore to new directory')
            evidence.update(passed=True,package_version=meta['version'],schema=11,package_sha256=ops.sha256(archive),old_binary_sha256=ops.sha256(old_binary),
                process_flags=['loopback','temporary state','no-import','no-refresh'],deployed_to_existing_service=False)
        finally:service.stop()
    (args.output/'package-results.json').write_text(json.dumps(evidence,indent=2)+'\n')
    print(json.dumps({'passed':True,'checks':len(evidence['checks']),'package_version':evidence['package_version']}))


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--archive',type=Path,required=True);parser.add_argument('--sha256',required=True)
    parser.add_argument('--old-binary',type=Path,required=True);parser.add_argument('--output',type=Path,required=True)
    run(parser.parse_args())
