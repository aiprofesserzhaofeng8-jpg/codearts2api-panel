import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import types
import unittest
from unittest.mock import patch

SOURCE = Path(__file__).resolve().parents[1] / 'install.sh'
module = types.ModuleType('installer')
exec(SOURCE.read_text().split("<<'PY'\n", 1)[1].rsplit('\nPY', 1)[0], module.__dict__)

class InstallerTest(unittest.TestCase):
    def test_url_version_and_duplicate_validation(self):
        tag = 'v0.3.1'
        url = module.BASE + tag + '/panelctl.py'
        self.assertEqual(module.assets({'assets':[{'name':'panelctl.py','browser_download_url':url}]},tag,['panelctl.py']),{'panelctl.py':url})
        for items in ([], [{'name':'panelctl.py','browser_download_url':'https://evil.invalid/x'}], [{'name':'panelctl.py','browser_download_url':url}]*2):
            with self.assertRaises(module.InstallError):
                module.assets({'assets':items},tag,['panelctl.py'])
        for value in ('v1.2.3-preview', '../main', 'latest', 'v1.2.3/evil'):
            with self.assertRaises(module.InstallError): module.version(value)

    def test_install_checks_all_bytes_before_execution(self):
        tag='v0.3.1'; name=f'codearts2api-panel-{tag}-linux-amd64.tar.gz'
        bodies={name:b'binary archive', 'panelctl.py':b'trusted helper'}
        meta={'tag_name':tag,'assets':[{'name':n,'browser_download_url':module.BASE+tag+'/'+n} for n in [name,'panelctl.py','SHA256SUMS']]}
        sums=''.join(hashlib.sha256(b).hexdigest()+'  '+n+'\n' for n,b in bodies.items()).encode()
        def fetch(url,target,limit):
            target.write_bytes(json.dumps(meta).encode() if url.startswith(module.API) else sums if url.endswith('/SHA256SUMS') else bodies[url.rsplit('/',1)[1]])
        with patch.object(module,'fetch',side_effect=fetch), patch.object(module.platform,'machine',return_value='x86_64'), patch.object(module.platform,'system',return_value='Linux'), patch.object(module.sys,'argv',['installer','install','--root','/tmp/new-instance']), patch.object(module.subprocess,'run') as run:
            module.main(); self.assertEqual(run.call_count,1)
            run.reset_mock(); bodies['panelctl.py']=b'tampered'
            with self.assertRaises(module.InstallError): module.main()
            run.assert_not_called()

    def test_checksums_reject_duplicates_and_traversal(self):
        with tempfile.TemporaryDirectory() as temp:
            p=Path(temp)/'SHA256SUMS'
            for data in [('0'*64+'  x\n')*2,'0'*64+'  ../x\n','garbage']:
                p.write_text(data)
                with self.assertRaises(module.InstallError): module.checksums(p)

    def test_shell_help(self):
        result=subprocess.run(['sh',str(SOURCE),'--help'],capture_output=True)
        self.assertEqual(result.returncode,0)
        self.assertIn(b'upgrade',result.stdout)
