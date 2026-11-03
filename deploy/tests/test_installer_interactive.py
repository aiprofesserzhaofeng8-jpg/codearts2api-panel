"""Exercise curl-pipe prompts through a real controlling PTY, without installing services."""
import argparse
from contextlib import ExitStack
import io
import json
import os
from pathlib import Path
import select
import subprocess
import tempfile
import time
import types
import unittest
from unittest.mock import patch

SOURCE = Path(__file__).resolve().parents[1] / 'install.sh'
module = types.ModuleType('installer_ui')
exec(SOURCE.read_text().split("<<'PY'\n", 1)[1].rsplit('\nPY', 1)[0], module.__dict__)


def options(**overrides):
    values = dict(command='install', system=True, lang=None, interactive=False,
                  non_interactive=False, host=None, port=None, root=Path('/opt/test-panel'))
    return argparse.Namespace(**(values | overrides))


class ConfigurationTest(unittest.TestCase):
    def setUp(self):
        module.LANGUAGE = 'zh'

    def configure(self, replies, **overrides):
        args = options(**overrides)
        output = io.StringIO()
        with patch.object(module, 'open_terminal', return_value=(ExitStack(), io.StringIO(replies), output)):
            module.configure(args)
        return args, output.getvalue()

    def test_defaults_and_english_validation(self):
        args, output = self.configure('\n\n\n\n')
        self.assertEqual((args.host, args.port, module.LANGUAGE), ('127.0.0.1', 10087, 'zh'))
        args, output = self.configure('bad\n2\ninvalid\n0.0.0.0\nabc\n0\n65536\n8089\ny\n')
        self.assertEqual((args.host, args.port, module.LANGUAGE), ('0.0.0.0', 8089, 'en'))
        self.assertIn('Enter 127.0.0.1 or 0.0.0.0', output)
        self.assertIn('Port must be an integer', output)
        self.assertIn('Restrict access until setup', output)

    def test_explicit_values_are_not_prompted_or_changed(self):
        args, output = self.configure('y\n', lang='en', host='127.0.0.1', port=23456)
        self.assertNotIn('Choice', output)
        self.assertNotIn('Listen address [', output)
        self.assertNotIn('Server port (', output)
        self.assertEqual((args.host, args.port), ('127.0.0.1', 23456))

    def test_no_terminal_defaults_and_explicit_interactive_failure(self):
        with patch.object(module, 'open_terminal', return_value=None):
            args = options()
            module.configure(args)
            self.assertEqual((args.host, args.port), ('127.0.0.1', 10087))
            with self.assertRaises(module.InstallError):
                module.configure(options(interactive=True))

    def test_noninteractive_and_readonly_commands_never_open_terminal(self):
        with patch.object(module, 'open_terminal') as terminal:
            for args in (options(non_interactive=True), options(command='check'),
                         options(command='download'), options(command='upgrade')):
                module.configure(args)
            terminal.assert_not_called()

    def test_eof_and_cancel_abort(self):
        for replies in ('', '1\n', '1\n\n\nn\n'):
            with self.subTest(replies=replies), self.assertRaises(module.InstallCancelled):
                self.configure(replies)

    def test_invalid_cli_port_and_existing_directory_fail_before_network(self):
        with tempfile.TemporaryDirectory() as root:
            for argv in (['install', '--root', root],
                         ['install', '--root', root + '/new', '--port', '0'],
                         ['install', '--root', root + '/new', '--port', '65536']):
                with patch.object(module.sys, 'argv', ['installer'] + argv), patch.object(module, 'fetch') as fetch, patch.object(module, 'open_terminal') as terminal:
                    with self.assertRaises(module.InstallError):
                        module.main()
                    fetch.assert_not_called()
                    terminal.assert_not_called()


@unittest.skipUnless(os.name == 'posix', 'controlling-terminal tests require POSIX')
class PipeTerminalTest(unittest.TestCase):
    def run_pipe(self, args, exchanges, fixture=False, no_terminal=False):
        import fcntl
        import pty
        import termios
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            record = root / 'executed.json'
            script = SOURCE.read_text()
            if fixture:
                # Keep the exact entry, parsing, downloads/checksums and subprocess
                # execution. Substitute only GitHub bytes and root prerequisites.
                helper = 'import json,sys\nfrom pathlib import Path\nif "--help" in sys.argv: print("--system")\nelse: Path(' + repr(str(record)) + ').write_text(json.dumps(sys.argv[1:]))\n'
                injection = '''
os.geteuid = lambda: 0
original_is_dir = Path.is_dir
Path.is_dir = lambda p: str(p) == '/run/systemd/system' or original_is_dir(p)
original_which = shutil.which
shutil.which = lambda name: '/fixture/systemctl' if name == 'systemctl' else original_which(name)
fixture_tag = 'v0.3.8'
fixture_name = f'codearts2api-panel-{fixture_tag}-linux-amd64.tar.gz'
platform.machine = lambda: 'x86_64'
fixture_blobs = {fixture_name: b'isolated archive', 'panelctl.py': HELPER.encode()}
fixture_blobs['SHA256SUMS'] = ''.join(hashlib.sha256(b).hexdigest()+'  '+n+'\\n' for n,b in fixture_blobs.items()).encode()
fixture_meta = {'tag_name': fixture_tag, 'assets': [{'name': n, 'browser_download_url': BASE+fixture_tag+'/'+n} for n in fixture_blobs]}
def fetch(url, target, limit):
    target.write_bytes(json.dumps(fixture_meta).encode() if url.startswith(API) else fixture_blobs[url.rsplit('/',1)[1]])
'''.replace('HELPER', repr(helper))
                script = script.replace("if __name__ == '__main__':", injection + "\nif __name__ == '__main__':")
            argv = ['bash', '-s', '--', 'install', '--root', str(root / 'installation')] + args
            if no_terminal:
                result = subprocess.run(argv, input=script, text=True, capture_output=True, start_new_session=True, timeout=15)
                return result.returncode, result.stdout + result.stderr, json.loads(record.read_text()) if record.exists() else None
            master, slave = pty.openpty()
            def attach():
                os.setsid()
                fcntl.ioctl(slave, termios.TIOCSCTTY, 0)
            process = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=slave, stderr=slave, preexec_fn=attach)
            os.close(slave)
            output = b''
            try:
                process.stdin.write(script.encode())
                process.stdin.close()
                deadline = time.monotonic() + 20
                pending = list(exchanges)
                while time.monotonic() < deadline:
                    readable, _, _ = select.select([master], [], [], 0.1)
                    if readable:
                        try:
                            data = os.read(master, 65536)
                        except OSError:
                            break
                        if not data:
                            break
                        output += data
                        if pending and pending[0][0].encode() in output:
                            _, reply = pending.pop(0)
                            os.write(master, reply.encode())
                    if process.poll() is not None and not readable:
                        break
                process.wait(timeout=2)
                self.assertFalse(pending, output.decode(errors='replace'))
                return process.returncode, output.decode(errors='replace'), json.loads(record.read_text()) if record.exists() else None
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait()
                os.close(master)

    def test_piped_system_install_selects_language_host_and_port(self):
        code, output, command = self.run_pipe(['--system'], [
            ('Choice [1]', '2\n'), ('Listen address [127.0.0.1]', '0.0.0.0\n'),
            ('Server port (recommended: 1024-65535) [10087]', '8089\n'), ('Continue installation?', 'y\n')], fixture=True)
        self.assertEqual(code, 0, output)
        self.assertIn('Asset checksums verified', output)
        self.assertEqual(command[command.index('--port') + 1], '8089')
        self.assertEqual(command[command.index('--host') + 1], '0.0.0.0')
        self.assertIn('--system', command)

    def test_exact_pipe_cancel_ctrl_c_and_eof_have_no_network_or_traceback(self):
        for reply in ('n\n', '\x03', '\x04'):
            with self.subTest(reply=reply):
                code, output, command = self.run_pipe(['--interactive', '--lang', 'en', '--port', '10087'], [('Continue installation?', reply)])
                self.assertEqual(code, 130, output)
                self.assertIn('Operation cancelled', output)
                self.assertNotIn('Fetching release', output)
                self.assertNotIn('Traceback', output)
                self.assertIsNone(command)

    def test_no_tty_defaults_and_noninteractive_ignore_available_tty(self):
        for no_terminal, args in ((True, ['--system']), (False, ['--system', '--non-interactive'])):
            code, output, command = self.run_pipe(args, [], fixture=True, no_terminal=no_terminal)
            self.assertEqual(code, 0, output)
            self.assertNotIn('Select language', output)
            self.assertEqual(command[command.index('--port') + 1], '10087')
            self.assertEqual(command[command.index('--host') + 1], '127.0.0.1')

    def test_forced_interaction_without_tty_fails_closed(self):
        code, output, command = self.run_pipe(['--interactive'], [], no_terminal=True)
        self.assertEqual(code, 1, output)
        self.assertNotIn('Fetching release', output)
        self.assertIsNone(command)
