import json
from pathlib import Path
import sys
import tempfile
import types
import subprocess
import unittest
from unittest.mock import patch
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
import panelctl as ops

class MigrationFailureTest(unittest.TestCase):
    def test_prelaunch_failure_restarts_source_but_started_candidate_never_does(self):
        for after_launch in (False,True):
            with self.subTest(after_launch=after_launch), tempfile.TemporaryDirectory() as temp:
                base=Path(temp);source=base/'source';source.mkdir();(source/'state.json').write_text('{"version":10}')
                root=base/'managed';events=[]
                class Service:
                    def __init__(self,unit): self.unit=unit
                    def stop(self): events.append(('stop',self.unit))
                    def start(self): events.append(('start',self.unit))
                old=Service('source.service')
                args=types.SimpleNamespace(root=str(root),data_dir=str(source),source_unit='source.service',port=10087)
                def install(args):
                    root.mkdir();(root/'data').mkdir();(root/'backups').mkdir();(root/'current').mkdir()
                    (root/'installation.json').write_text('{"unit":"target.service"}')
                def unpack(*args):
                    (root/'data').mkdir();(root/'data/newest').write_text('new-generation')
                def backup(*args):
                    if not after_launch: raise ops.OpsError('backup failed')
                with patch.object(ops,'source_process',return_value=(old,'1')),patch.object(ops,'install',side_effect=install),patch.object(ops,'Service',Service),patch.object(ops,'run',return_value=types.SimpleNamespace(stdout='enabled')),patch.object(ops,'no_writer'),patch.object(ops,'check_release',return_value={'schema':10}),patch.object(ops,'backup',side_effect=backup),patch.object(ops,'unpack',side_effect=unpack),patch.object(ops,'wait_health',side_effect=ops.OpsError('not ready')):
                    with self.assertRaises(ops.OpsError): ops.migrate(args)
                if after_launch:
                    self.assertNotIn(('start','source.service'),events)
                    self.assertIn(('stop','target.service'),events)
                    self.assertEqual((root/'data/newest').read_text(),'new-generation')
                else:
                    self.assertIn(('start','source.service'),events)
                    self.assertNotIn(('start','target.service'),events)

class DirectoryDescriptorTest(unittest.TestCase):
    def test_directory_handle_is_not_a_writer_but_open_state_file_is(self):
        with tempfile.TemporaryDirectory() as temp:
            directory=Path(temp);state=directory/'state.json';state.write_text('{}')
            for target,blocked in ((directory,False),(state,True)):
                child=subprocess.Popen([sys.executable,'-c','import os,sys; fd=os.open(sys.argv[1],os.O_RDONLY); print("ready",flush=True); sys.stdin.read()',str(target)],stdin=subprocess.PIPE,stdout=subprocess.PIPE,text=True)
                try:
                    self.assertEqual(child.stdout.readline().strip(),'ready')
                    if blocked:
                        with self.assertRaises(ops.OpsError):ops.no_writer(directory)
                    else:ops.no_writer(directory)
                finally:
                    child.communicate('',timeout=5)
