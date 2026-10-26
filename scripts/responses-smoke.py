#!/usr/bin/env python3
"""Opt-in LIVE checks: makes real, billed model requests using the local state key."""
import datetime
import json
import os
from pathlib import Path
import secrets
import subprocess
import tempfile
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
if os.environ.get('CODEARTS_LIVE_TEST') != '1':
    raise SystemExit('Set CODEARTS_LIVE_TEST=1 to run real upstream requests.')
state = json.loads((ROOT / 'data/state.json').read_text())
key = next(k['key'] for k in state['keys'] if k.get('enabled'))
base = os.environ.get('CODEARTS_TEST_URL', 'http://127.0.0.1:10087/v1')
model = os.environ.get('CODEARTS_TEST_MODEL', 'deepseek-v4.1-flash')
report = {'checked_at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'model': model, 'checks': []}

def request(path, payload):
    req = urllib.request.Request(base + path, data=json.dumps(payload).encode(), headers={
        'Authorization': 'Bearer ' + key, 'Content-Type': 'application/json'})
    with urllib.request.urlopen(req, timeout=120) as response:
        return response.status, response.read().decode()

def text_of(response):
    return ''.join(p.get('text', '') for item in response.get('output', []) if item.get('type') == 'message' for p in item.get('content', []))

for stream in (False, True):
    status, body = request('/responses', {'model': model, 'input': 'Reply with exactly RESPONSES_OK.', 'store': False, 'stream': stream, 'max_output_tokens': 512})
    if stream:
        events = [json.loads(line[6:]) for line in body.splitlines() if line.startswith('data: ') and line != 'data: [DONE]']
        assert events[0]['type'] == 'response.created'
        assert events[-1]['type'] == 'response.completed'
        assert [e['sequence_number'] for e in events] == list(range(len(events)))
        result = events[-1]['response']
    else:
        result = json.loads(body)
    assert status == 200 and result['status'] == 'completed', result.get('status')
    assert 'RESPONSES_OK' in text_of(result), 'unexpected answer'
    report['checks'].append({'name': 'responses_stream' if stream else 'responses_buffered', 'status': status, 'passed': True, 'usage': result.get('usage')})
    print(report['checks'][-1], flush=True)

# A clean Pi config ensures this is truly openai-responses, not the existing
# Chat provider. Only a read tool and a newly generated temporary file are used.
with tempfile.TemporaryDirectory(prefix='pi-codearts-responses-') as directory:
    temp = Path(directory)
    agent = temp / 'agent'
    agent.mkdir(mode=0o700)
    models = {'providers': {'codearts-responses-check': {
        'baseUrl': base, 'api': 'openai-responses', 'apiKey': key,
        'authHeader': True,
        'models': [{'id': model, 'name': 'CodeArts Responses check', 'input': ['text'], 'contextWindow': 32768, 'maxTokens': 4096}]
    }}}
    config = agent / 'models.json'
    config.write_text(json.dumps(models))
    config.chmod(0o600)
    marker = 'READ_OK_' + secrets.token_hex(8)
    (temp / 'probe.txt').write_text(marker + '\n')
    env = os.environ.copy()
    env.update(PI_CODING_AGENT_DIR=str(agent), PI_OFFLINE='1', PI_TELEMETRY='0')
    proc = subprocess.run(['pi', '--provider', 'codearts-responses-check', '--model', model,
        '--mode', 'json', '--print', '--no-session', '--tools', 'read', '--no-extensions', '--no-mcp',
        '--no-skills', '--no-context-files', '--no-prompt-templates', '--system-prompt',
        'Use the read tool when asked. Reply concisely.',
        'Use the read tool to read probe.txt. Reply with only the exact text found in that file.'],
        cwd=temp, env=env, capture_output=True, text=True, timeout=180)
    events = []
    for line in proc.stdout.splitlines():
        try: events.append(json.loads(line))
        except json.JSONDecodeError: pass
    tools = [e for e in events if e.get('type') == 'tool_execution_end']
    assistants = [e.get('message', {}) for e in events if e.get('type') == 'message_end' and e.get('message', {}).get('role') == 'assistant']
    answer = ''.join(p.get('text', '') for a in assistants for p in a.get('content', []) if p.get('type') == 'text')
    passed = proc.returncode == 0 and any(e.get('toolName') == 'read' and not e.get('isError') for e in tools) and marker in answer
    report['checks'].append({'name': 'pi_responses_read_tool_roundtrip', 'passed': passed, 'exit_code': proc.returncode,
        'tool_results': [{'name': e.get('toolName'), 'error': e.get('isError', False)} for e in tools],
        'assistant_stop_reasons': [a.get('stopReason') for a in assistants]})
    print(report['checks'][-1], flush=True)
    if not passed:
        # Print only scrubbed diagnostics, never the config/API key.
        print((proc.stderr + proc.stdout[-6000:]).replace(key, '[redacted]'))
    assert passed, 'Pi Responses tool roundtrip failed'
(ROOT / 'artifacts').mkdir(exist_ok=True)
(ROOT / 'artifacts/responses-live-check.json').write_text(json.dumps(report, indent=2) + '\n')
print('All real Responses checks passed.')
