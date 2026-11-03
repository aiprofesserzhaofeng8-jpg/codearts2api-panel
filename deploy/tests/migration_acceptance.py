#!/usr/bin/env python3
"""Real disposable source-service migration; never reads production data."""
import argparse
import hashlib
import time
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import tempfile
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
import panelctl as ops
from live_acceptance import port, post, wait

def run(archive):
    with tempfile.TemporaryDirectory(prefix='codearts-migrate-') as temp:
        root=Path(temp); release=root/'release'; data=root/'source-data';data.mkdir(mode=0o700)
        ops.unpack(archive,release,'release',ops.sha256(archive))
        suffix=secrets.token_hex(5);old='codearts-source-'+suffix+'.service';new='codearts2api-migrated-'+suffix+'.service'
        units=Path(os.environ.get('XDG_CONFIG_HOME',str(Path.home()/'.config')))/'systemd/user';old_path=units/old;new_path=units/new
        app_port=port();base=f'http://127.0.0.1:{app_port}';binary=release/'codearts2api-panel'
        old_path.write_text(f'[Unit]\nDescription=Isolated migration fixture\n[Service]\nExecStart={binary} -host 127.0.0.1 -port {app_port} -data {data}/state.json -no-import -no-refresh\n[Install]\nWantedBy=default.target\n')
        try:
            ops.run(['systemctl','--user','daemon-reload']);ops.run(['systemctl','--user','enable','--now',old]);wait(base,'setup_required')
            password=secrets.token_urlsafe(24);email='owner@example.com'
            post(base,'/setup/install',{'email':email,'password':password,'confirm_password':password});wait(base,'running')
            ops.Service(unit=old).stop()
            snapshot=json.loads((data/'state.json').read_text())
            entry={'sequence':snapshot.get('usage_sequence',0)+1,'key_hash':hashlib.sha256(snapshot['keys'][0]['key'].encode()).hexdigest(),
                   'entry':{'request_id':'migration-settlement','time':int(time.time()),'model':'synthetic','status':200,'attempts':1,'output_len':2,
                            'usage':{'known':True,'input_tokens':12,'output_tokens':4},'estimated_cost':{'currency':'USD','amount':0.125}},'out_chars':2}
            journal=data/'state.json.usage.jsonl'
            journal.write_text(json.dumps(entry)+'\n');journal.chmod(0o600)
            ops.Service(unit=old).start();wait(base,'running')
            # Non-state file confirms recursive migration rather than copying just state.json.
            (data/'private').mkdir();(data/'private/fixture').write_text('synthetic latest credential')
            args=['migrate','--root',str(root/'managed'),'--archive',str(archive),'--sha256',ops.sha256(archive),'--name','migrated-'+suffix,'--port',str(app_port),'--source-unit',old,'--source-binary',str(binary),'--data-dir',str(data),'--no-refresh']
            subprocess.run([sys.executable,str(release/'panelctl.py'),*args],check=True)
            wait(base,'running')
            assert post(base,'/auth/login',{'username':'owner@example.com','password':password})['user']['role']=='admin'
            assert (root/'managed/data/private/fixture').read_text()=='synthetic latest credential'
            assert not ops.Service(unit=old).running()
            assert ops.run(['systemctl','--user','is-enabled',old],check=False).stdout.strip()=='disabled'
            assert '-no-refresh' in new_path.read_text()
            assert len(list((root/'managed/backups').glob('*.tar.gz')))==1
            ops.Service(unit=new).stop()
            migrated=json.loads((root/'managed/data/state.json').read_text())
            assert migrated['usage_sequence']==entry['sequence']
            assert migrated['stats']['total_requests']==snapshot['stats']['total_requests']+1
            assert migrated['stats']['estimated_usd']==snapshot['stats']['estimated_usd']+0.125
            assert [k['key'] for k in migrated['keys']]==[k['key'] for k in snapshot['keys']]
            print(json.dumps({'checks':['real_source_migration','old_password_preserved','recursive_private_data_preserved','source_stopped_and_disabled','no_refresh_preserved','full_backup_retained','ledger_settled_once','api_keys_preserved']}))
        finally:
            for unit in (old,new):ops.run(['systemctl','--user','disable','--now',unit],check=False)
            old_path.unlink(missing_ok=True);new_path.unlink(missing_ok=True);ops.run(['systemctl','--user','daemon-reload'])
if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--archive',type=Path,required=True);run(p.parse_args().archive.resolve())
