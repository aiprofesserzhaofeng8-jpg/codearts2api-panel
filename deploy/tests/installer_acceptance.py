#!/usr/bin/env python3
"""Run the exact bootstrap against local release bytes, with real systemd installation."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import sys
import tempfile
import types
from unittest.mock import patch
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
import panelctl as ops
from live_acceptance import port,post,wait

def run(archive):
    source=Path(__file__).resolve().parents[1]/'install.sh'
    installer=types.ModuleType('installer')
    exec(source.read_text().split("<<'PY'\n",1)[1].rsplit('\nPY',1)[0],installer.__dict__)
    with tempfile.TemporaryDirectory(prefix='codearts-bootstrap-') as temp:
        folder=Path(temp);release=folder/'release';meta=ops.unpack(archive,release,'release',ops.sha256(archive));tag=meta['version']
        blobs={archive.name:archive.read_bytes(),'panelctl.py':(release/'panelctl.py').read_bytes()}
        blobs['SHA256SUMS']=''.join(hashlib.sha256(data).hexdigest()+'  '+name+'\n' for name,data in blobs.items()).encode()
        metadata={'tag_name':tag,'assets':[{'name':name,'browser_download_url':installer.BASE+tag+'/'+name} for name in blobs]}
        def fetch(url,target,limit):
            content=json.dumps(metadata).encode() if url.startswith(installer.API) else blobs[url.rsplit('/',1)[1]]
            assert len(content)<=limit;target.write_bytes(content)
        suffix='bootstrap-'+secrets.token_hex(5);unit='codearts2api-'+suffix+'.service'
        unit_path=Path(os.environ.get('XDG_CONFIG_HOME',str(Path.home()/'.config')))/'systemd/user'/unit
        p=port();base=f'http://127.0.0.1:{p}';root=folder/'managed'
        try:
            with patch.object(installer,'fetch',side_effect=fetch),patch.object(sys,'argv',['installer','install','--version',tag,'--root',str(root),'--name',suffix,'--port',str(p)]):installer.main()
            wait(base,'setup_required');email='owner@example.com';password=secrets.token_urlsafe(24)
            post(base,'/setup/install',dict(email=email,password=password,confirm_password=password));wait(base,'running')
            assert post(base,'/auth/login',{'username':'owner@example.com','password':password})['user']['role']=='admin'
            ops.verify_process(unit,(root/'current').resolve()/'codearts2api-panel')
            print(json.dumps({'checks':['same_bootstrap_entry','release_bytes_verified','real_managed_install','initial_setup','admin_login','actual_binary_version'],'network':'local release fixture, not GitHub publication'}))
        finally:
            ops.run(['systemctl','--user','disable','--now',unit],check=False);unit_path.unlink(missing_ok=True);ops.run(['systemctl','--user','daemon-reload'])
if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--archive',type=Path,required=True);run(p.parse_args().archive.resolve())
