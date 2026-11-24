"""Drive the installed Pi CLI through a tool call using only loopback fixtures."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import json
import os
import shutil
import subprocess
import tempfile
import threading
import unittest

ROOT = Path(__file__).resolve().parents[1]


@unittest.skipUnless(shutil.which("pi"), "Pi CLI required")
class PiSearchTest(unittest.TestCase):
    def test_complete_tool_round_trip(self):
        paths = []
        failures = []
        class FakeGateway(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_POST(self):
                body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
                paths.append(self.path)
                if self.headers.get('Authorization') != 'Bearer fixture-key':
                    failures.append('incorrect auth')
                if self.path == '/v1/search':
                    if body['query'] != 'term':
                        failures.append('incorrect query')
                    result = {'results': [{'title':'Fixture','url':'https://example.org/source','snippet':'round-trip-marker'}], 'request_id':'r','service_id':'s'}
                    data = json.dumps(result).encode()
                    self.send_response(200)
                    self.send_header('Content-Type','application/json')
                    self.end_headers()
                    self.wfile.write(data)
                    return
                if self.path != '/v1/chat/completions':
                    self.send_error(404)
                    return
                if any(m['role']=='tool' for m in body['messages']):
                    if 'round-trip-marker' not in json.dumps(body['messages']):
                        failures.append('tool result missing')
                    delta, reason = {'role':'assistant','content':'search verified'}, 'stop'
                else:
                    if not any(t['function']['name']=='web_search' for t in body.get('tools', [])):
                        failures.append('tool not registered')
                    delta, reason = {'role':'assistant','tool_calls':[{'index':0,'id':'call_fixture','type':'function','function':{'name':'web_search','arguments':'{"query":"term"}'}}]}, 'tool_calls'
                self.send_response(200)
                self.send_header('Content-Type','text/event-stream')
                self.end_headers()
                for d,r in [(delta,None),({},reason)]:
                    event = {'id':'chatcmpl-fixture','object':'chat.completion.chunk','created':1,'model':'fixture','choices':[{'index':0,'delta':d,'finish_reason':r}]}
                    self.wfile.write(('data: '+json.dumps(event)+'\n\n').encode())
                self.wfile.write(b'data: [DONE]\n\n')

        with tempfile.TemporaryDirectory() as folder, ThreadingHTTPServer(('127.0.0.1',0),FakeGateway) as server:
            thread=threading.Thread(target=server.serve_forever,daemon=True)
            thread.start()
            self.addCleanup(server.shutdown)
            agent=Path(folder)
            models=agent/'models.json'
            models.write_text(json.dumps({'providers':{'codearts-local':{'baseUrl':f'http://127.0.0.1:{server.server_port}/v1','api':'openai-completions','apiKey':'fixture-key','authHeader':True,'models':[{'id':'fixture','contextWindow':131072,'maxTokens':4096}]}}}))
            cli=['python3',str(ROOT/'scripts/connect-client.py'),'pi','--pi-models-file',str(models)]
            env={**os.environ,'PI_CODING_AGENT_DIR':folder}
            def installer(option):
                return subprocess.run(cli+[option],capture_output=True,text=True,cwd=agent,env=env)
            ext=agent/'extensions'/'codearts-web-search.ts'
            for _ in range(2):
                self.assertEqual(installer('--install-web-search').returncode,0)
            result=subprocess.run(['pi','--no-session','--provider','codearts-local','--model','fixture','--tools','web_search','-p','Search for term.'],capture_output=True,cwd=agent,env=env,text=True,timeout=40)
            self.assertEqual(result.returncode,0,result.stderr)
            self.assertIn('search verified',result.stdout)
            self.assertEqual(paths,['/v1/chat/completions','/v1/search','/v1/chat/completions'])
            self.assertEqual(failures,[])
            self.assertEqual(installer('--remove-web-search').returncode,0)
            self.assertFalse(ext.exists())
            other=ext.with_name('other.ts')
            other.write_text('pi.registerTool({name:"web_search"})')
            self.assertNotEqual(installer('--install-web-search').returncode,0)
            self.assertTrue(other.exists())
            server.shutdown()

if __name__=='__main__':
    unittest.main()
