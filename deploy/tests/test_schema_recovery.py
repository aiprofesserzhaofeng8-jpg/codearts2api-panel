"""Recover each recorded crash boundary without reverting rotated credentials."""
import json
from pathlib import Path
import tempfile
import types
import unittest
from unittest.mock import patch

import test_panelctl
import panelctl as ops


class SchemaRecoveryTest(unittest.TestCase):
    def fixture(self, phase):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        root = Path(temp.name)
        old, new = root/'releases/v1.0.0', root/'releases/v1.0.1'
        for release, schema in ((old, 10), (new, 11)):
            release.mkdir(parents=True)
            (release/'manifest.json').write_text(json.dumps({'version':release.name,'schema':schema}))
        original, candidate, data = root/'data-before-schema11-1', root/'data-schema11-2', root/'data'
        for target, schema, token in ((data,10,'before'),(candidate,11,'latest')):
            target.mkdir()
            (target/'state.json').write_text(json.dumps({'version':schema,'token':token}))
        (root/'current').symlink_to('releases/v1.0.0')
        (root/'installation.json').write_text(json.dumps({'unit':'fixture.service','port':10087}))
        record={'from':10,'to':11,'phase':'prepared','old_release':str(old),'new_release':str(new),
                'original_data':str(original),'candidate_data':str(candidate)}
        if phase >= 1: (root/'current.next').symlink_to('releases/v1.0.1')
        if phase >= 2: data.rename(original)
        if phase >= 3: candidate.rename(data)
        if phase >= 4: (root/'current.next').replace(root/'current')
        if phase >= 5: record['phase']='starting'
        (root/'schema-migration.json').write_text(json.dumps(record))
        service=types.SimpleNamespace(calls=[],running=lambda:False)
        service.stop=lambda:service.calls.append('stop')
        service.start=lambda:service.calls.append('start')
        args=ops.parser().parse_args(['recover-schema','--root',str(root),'--apply'])
        return root,args,service

    def mocks(self, service, health=None):
        from contextlib import ExitStack
        stack=ExitStack()
        stack.enter_context(patch.object(ops,'Service',return_value=service))
        stack.enter_context(patch.object(ops,'check_release',side_effect=lambda p:json.loads((p/'manifest.json').read_text())))
        stack.enter_context(patch.object(ops,'verify_process'))
        stack.enter_context(patch.object(ops,'model_migration_precheck',return_value={}))
        stack.enter_context(patch.object(ops,'wait_health',side_effect=health))
        return stack

    def test_forward_recovery_at_every_published_boundary_and_repeat(self):
        for phase in range(6):
            with self.subTest(phase=phase):
                root,args,service=self.fixture(phase)
                with self.mocks(service):
                    ops.recover_schema(args)
                    self.assertEqual((root/'current').resolve().name,'v1.0.1')
                    self.assertEqual(json.loads((root/'data/state.json').read_text())['token'],'latest')
                    self.assertEqual(json.loads((root/'data-before-schema11-1/state.json').read_text())['token'],'before')
                    self.assertFalse((root/'schema-migration.json').exists())
                    self.assertEqual(service.calls,['stop','start'])
                    ops.recover_schema(args)
                    self.assertEqual(service.calls,['stop','start'])

    def test_inspection_does_not_stop_or_move(self):
        root,args,service=self.fixture(2)
        args.apply=False
        with self.mocks(service): ops.recover_schema(args)
        self.assertFalse((root/'data').exists())
        self.assertEqual(service.calls,[])

    def test_failed_start_preserves_newest_data_and_retries(self):
        root,args,service=self.fixture(5)
        def failed(*_):
            (root/'data/state.json').write_text('{"version":11,"token":"rotated"}')
            raise ops.OpsError('synthetic health failure')
        with self.mocks(service,failed), self.assertRaises(ops.OpsError): ops.recover_schema(args)
        self.assertTrue((root/'schema-migration.json').exists())
        with self.mocks(service): ops.recover_schema(args)
        self.assertEqual(json.loads((root/'data/state.json').read_text())['token'],'rotated')

    def test_invalid_or_ambiguous_paths_fail_closed(self):
        for change in ('outside','link','duplicate','incomplete','wrong-current'):
            with self.subTest(change=change):
                root,args,service=self.fixture(3)
                marker=root/'schema-migration.json'
                record=json.loads(marker.read_text())
                if change=='outside': record['candidate_data']='/tmp/data-schema11-999'
                if change=='link': (root/'data').rename(root/'elsewhere');(root/'data').symlink_to('elsewhere')
                if change=='duplicate':
                    candidate=root/'data-schema11-2';candidate.mkdir();(candidate/'state.json').write_text('{"version":11}')
                if change=='incomplete': (root/'data/.model-migration.pending.json').write_text('{}')
                if change=='wrong-current': (root/'current').unlink();(root/'current').symlink_to('releases/v9.9.9')
                marker.write_text(json.dumps(record))
                with self.mocks(service),self.assertRaises(ops.OpsError): ops.recover_schema(args)
                self.assertTrue(marker.exists())

    def test_unpublished_release_requires_exact_package(self):
        root,args,service=self.fixture(0)
        import shutil
        shutil.rmtree(root/'releases/v1.0.1')
        with self.mocks(service),self.assertRaises(ops.OpsError): ops.recover_schema(args)
        self.assertEqual(service.calls,[])

    def test_recovery_precheck_requires_target_schema_explicitly(self):
        result=types.SimpleNamespace(returncode=0,stdout=json.dumps({'from_schema':11,'to_schema':11,'ready':True,'conflicts':[]}))
        with patch.object(ops.subprocess,'run',return_value=result):
            with self.assertRaises(ops.OpsError):ops.model_migration_precheck(Path('/fixture/panel'),Path('/fixture/state'))
            self.assertTrue(ops.model_migration_precheck(Path('/fixture/panel'),Path('/fixture/state'),source_schema=11)['ready'])

    def test_cli_inspection_creates_no_lock_or_install_directory(self):
        root,_,service=self.fixture(2)
        before=sorted(str(p.relative_to(root)) for p in root.rglob('*'))
        with patch.object(ops.sys,'argv',['panelctl.py','recover-schema','--root',str(root)]):
            self.assertEqual(ops.main(),0)
        self.assertEqual(sorted(str(p.relative_to(root)) for p in root.rglob('*')),before)
        self.assertFalse((root.parent/('.'+root.name+'.panelops.lock')).exists())
        missing=root/'not-an-installation'
        with patch.object(ops.sys,'argv',['panelctl.py','recover-schema','--root',str(missing)]):
            self.assertEqual(ops.main(),1)
        self.assertFalse(missing.exists())


if __name__=='__main__': unittest.main()
