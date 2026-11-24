#!/usr/bin/env python3
"""Real TeleAgent input/output probes. Requires --live and tiktoken.

Uses only synthetic text. Reads the existing Pi key or prompts without saving it.
Input targets use cl100k_base; upstream usage remains the authoritative count.
Output parameter acceptance and actual output generation are separate evidence.
"""
import argparse
import concurrent.futures
import getpass
import importlib.util
import json
from pathlib import Path
import random
import secrets
import threading
import time
import types

spec = importlib.util.spec_from_file_location('capability_audit', Path(__file__).with_name('model-capability-audit.py'))
audit = importlib.util.module_from_spec(spec)
spec.loader.exec_module(audit)
# Token-by-token SSE can exceed the ordinary 8 MiB short-probe buffer.
audit.LIMIT = 64 * 1024 * 1024
MODELS = ['teleagent-chat-' + n for n in ('flagship', 'flash', 'lite', 'nano', 'pro')]
INPUTS = [256000, 512000, 768000, 1000000]
OUTPUTS = [8192, 32768, 65536, 131072, 262144, 524288, 1048576]


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--live', required=True, action='store_true')
    ap.add_argument('--prompt-key', action='store_true')
    ap.add_argument('--url', default='http://127.0.0.1:10087')
    ap.add_argument('--pi-models', type=Path, default=Path.home()/'.pi/agent/models.json')
    ap.add_argument('--out', type=Path, default=Path('artifacts/teleagent-limits-20261123'))
    ap.add_argument('--timeout', type=int, default=300)
    ap.add_argument('--workers', type=int, default=1)
    ap.add_argument('--phase', choices=['all', 'input', 'output', 'boundary', 'refine', 'output-control'], default='all')
    ap.add_argument('--models', nargs='+', choices=MODELS, default=MODELS)
    ap.add_argument('--resume', action='store_true')
    ap.add_argument('--retry-failed', action='store_true')
    args = ap.parse_args()
    import tiktoken
    enc = tiktoken.get_encoding('cl100k_base')
    key = getpass.getpass('Gateway API key (not saved): ') if args.prompt_key else audit.credentials(args)
    api = audit.API(args.url, key, timeout=args.timeout, max_requests=80, min_interval=1)
    args.out.mkdir(parents=True, exist_ok=True, mode=0o700)
    log = args.out/'results.jsonl'
    existing = [json.loads(l) for l in log.read_text().splitlines()] if args.resume and log.exists() else []
    if not args.resume and log.exists():
        raise SystemExit('Report exists; use --resume or a new --out')
    lock = threading.Lock()
    def record(row):
        row['time'] = audit.now()
        with lock:
            with log.open('a') as f:
                f.write(json.dumps(row, ensure_ascii=False)+'\n')
            log.chmod(0o600)
            existing.append(row)
        print(json.dumps({k: row.get(k) for k in ('model','case','target','status','error','usage','finish','seconds','retrieval')},ensure_ascii=False),flush=True)
    def done(model, case, target):
        rows=[x for x in existing if x.get('model')==model and x.get('case')==case and x.get('target')==target]
        return bool(rows) and not (args.retry_failed and rows[-1].get('error'))
    def request(model, case, target, text, output, expected=None):
        if done(model,case,target):
            return next(x for x in reversed(existing) if x.get('model')==model and x.get('case')==case and x.get('target')==target)
        body={'model':model,'stream':True,'stream_options':{'include_usage':True},'max_tokens':output,'messages':[{'role':'user','content':text}]}
        resp=api.request('/v1/chat/completions',body)
        result,error=audit.evaluated(resp,'chat',True)
        row={'model':model,'case':case,'target':target,'requested_output':output,
             'local_input_tokens':len(enc.encode(text)), 'tokenizer':'cl100k_base',
             **audit.evidence(resp,result), 'error':audit.scrub(error,key) if error else None}
        if result:
            row['local_output_tokens']=len(enc.encode(result['text']))
            row['output_characters']=len(result['text'])
            if expected:row['retrieval']={k:v in result['text'] for k,v in expected.items()}
        record(row)
        return row
    pre=api.request('/v1/models',method='GET')
    audit.save(args.out/'preflight.json',audit.evidence(pre))
    if pre.get('status')!=200:
        raise SystemExit('Gateway preflight failed HTTP '+str(pre.get('status'))+'; no inference sent')
    catalog=json.loads(pre['raw'])
    names={x['id'] for x in catalog.get('data',[])}
    audit.save(args.out/'catalog-summary.json',{'time':audit.now(),'models':[{'id':x['id'],'provider':x.get('provider'),'codearts':x.get('codearts')} for x in catalog.get('data',[]) if x['id'] in MODELS]})
    # Random numeric filler is synthetic and produces a repeatable tokenizer count.
    rng=random.Random(20261123)
    filler=' '.join(str(rng.randrange(100000000,1000000000)) for _ in range(260000))
    tokens=enc.encode(filler)
    def model_run(model):
        if model not in names:
            record({'model':model,'case':'availability','target':0,'status':0,'error':'model absent from authorized catalog'})
            return
        base=request(model,'baseline',0,'Reply exactly READY.',32)
        if base.get('status')!=200 or base.get('error'):
            return
        if args.phase == 'output-control':
            request(model,'output_generation',1024,'Write exactly 128 numbered records. Each record must have its number and these words: alpha bravo charlie delta echo foxtrot golf hotel. Write actual records without ellipses, introduction or conclusion.',1024)
            return
        if args.phase == 'refine':
            probes=[r for r in existing if r.get('model')==model and r.get('case','').startswith(('output_parameter','output_search'))]
            accepted=[r['target'] for r in probes if r.get('status')==200 and not r.get('error')]
            if not accepted:return
            low=max(accepted)
            rejected=[r['target'] for r in probes if r.get('status')==400 and r['target']>low]
            if not rejected:return
            high=min(rejected)
            while high-low>1:
                midpoint=(low+high)//2
                row=request(model,'output_search',midpoint,'Reply exactly READY.',midpoint)
                if row.get('status')==200 and not row.get('error'):low=midpoint
                elif row.get('status')==400:high=midpoint
                else:return
            record({'model':model,'case':'output_parameter_limit','target':low,'status':200,'first_rejected':high,'error':None})
            return
        if args.phase == 'boundary':
            request(model,'output_parameter_boundary',131073,'Reply exactly READY.',131073)
            return
        if args.phase in ('all','input'):
            calibration=request(model,'input_calibration',4096,enc.decode(tokens[:4096])+'\nReply exactly READY.',64)
            consumed=(calibration.get('usage') or {}).get('prompt_tokens',0)
            if calibration.get('status')!=200 or consumed<=0:
                return
            ratio=consumed/4096
            for size in INPUTS:
                expected={n:'MARK_'+secrets.token_hex(10) for n in ('A','B','C')}
                parts=[f'FIELD_{n}={v}\n' for n,v in expected.items()]
                prompt='Read the text and return the values of FIELD_A, FIELD_B and FIELD_C, exactly.\n'
                suffix='\nNow output only FIELD_A, FIELD_B and FIELD_C values.\n'
                overhead=len(enc.encode(prompt+''.join(parts)+suffix))
                n=max(0,int(size/ratio)-overhead)
                text=prompt+parts[0]+enc.decode(tokens[:n//2])+'\n'+parts[1]+enc.decode(tokens[n//2:n])+'\n'+parts[2]+suffix
                row=request(model,'input_calibrated',size,text,2048,expected)
                if row.get('status') in (401,402,403,429):
                    record({'model':model,'case':'input_blocked','target':size,'status':row['status'],'error':'remaining input sizes blocked by auth/quota/rate; resume after recovery'})
                    break
        if args.phase in ('all','output'):
            accepted=[]
            for size in OUTPUTS:
                row=request(model,'output_parameter',size,'Reply exactly READY.',size)
                if row.get('status')==200 and not row.get('error'):accepted.append(size)
                if row.get('status') in (401,402,403,429):return
            if accepted:
                request(model,'output_parameter_boundary',max(accepted)+1,'Reply exactly READY.',max(accepted)+1)
            # Ask for sustained actual output separately from the small acceptance probes.
            for size in (8192,32768):
                if not accepted or size>max(accepted):continue
                text=('Generate a long synthetic test fixture. Output exactly one numbered record per line, starting at 000001 and incrementing by one. Each line must contain the record number followed by the fixed words: alpha bravo charlie delta echo foxtrot golf hotel. Continue until the API token limit stops you. Do not summarize, use ellipses, code fences or explanatory prose. Produce at least 20000 lines; keep writing actual records.')
                request(model,'output_generation',size,text,size)
    with concurrent.futures.ThreadPoolExecutor(max_workers=max(1,min(args.workers,2))) as pool:
        futures=[pool.submit(model_run,m) for m in args.models]
        for f in concurrent.futures.as_completed(futures):f.result()
    audit.save(args.out/'summary.json',{'time':audit.now(),'requests_this_run':api.count,'rows':existing,'note':'Accepted max_tokens is not actual maximum output. cl100k_base counts are reference counts; use upstream usage for actual consumed tokens. Input retrieval samples three positions and does not prove all tokens retained.'})

if __name__=='__main__':main()
