// @vitest-environment jsdom
import { beforeEach, afterEach, describe, it, expect, vi } from 'vitest';
import { api, ApiError, configureClient, cancelPageQueries, disposeClient } from './client';
const response = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });
beforeEach(() => { document.head.innerHTML = '<meta name="local-panel-session" content="fixture">'; configureClient(vi.fn()); });
afterEach(() => { disposeClient(); vi.unstubAllGlobals(); });
describe('same-origin management client', () => {
  it('uses only cookie authentication and preserves absent/null/[] in command bodies', async () => {
    const fetch = vi.fn().mockImplementation(()=>Promise.resolve(response({ok:true}))); vi.stubGlobal('fetch',fetch);
    for (const body of [{id:'x'}, {id:'x',model_mappings:null}, {id:'x',model_mappings:[]}]) await api('/admin/api/groups',{method:'POST',body});
    expect(fetch.mock.calls.map(call=>JSON.parse(call[1].body))).toEqual([{id:'x'},{id:'x',model_mappings:null},{id:'x',model_mappings:[]}]);
    expect(fetch.mock.calls[0]![1]).toMatchObject({credentials:'same-origin',headers:{'Content-Type':'application/json'}});
    expect(fetch.mock.calls[0]![1].headers).not.toHaveProperty('X-Local-Panel-Session');
  });
  it('rejects cross-origin URLs without sending credentials', async () => {
    const fetch=vi.fn();vi.stubGlobal('fetch',fetch);
    for(const url of ['https://example.com','//example.com','/\\example.com']) await expect(api(url)).rejects.toThrow('same-origin');
    expect(fetch).not.toHaveBeenCalled();
  });
  it('preserves HTTP status/text and invokes expired session handling only for admin reads', async () => {
    const expired=vi.fn();configureClient(expired);vi.stubGlobal('fetch',vi.fn().mockImplementation(()=>Promise.resolve(new Response('expired',{status:401}))));
    await expect(api('/admin/api/state')).rejects.toMatchObject({message:'expired',status:401});expect(expired).toHaveBeenCalledTimes(1);
    await expect(api('/health')).rejects.toBeInstanceOf(ApiError);expect(expired).toHaveBeenCalledTimes(1);
  });
  it('cancels replaceable reads while leaving writes and global reads running', async () => {
    const signals: Array<AbortSignal|undefined>=[];
    vi.stubGlobal('fetch',vi.fn((_path,options)=>{signals.push(options.signal);return Promise.resolve(response({}));}));
    await api('/admin/api/usage?page=1');
    const read=api('/admin/api/usage?page=2');const write=api('/admin/api/workbuddy/tasks/run',{method:'POST',body:{account_id:'fixture'}});const state=api('/admin/api/state');
    cancelPageQueries();expect(signals[1]?.aborted).toBe(true);expect(signals[2]).toBeUndefined();expect(signals[3]?.aborted).toBe(false);
    await Promise.allSettled([read,write,state]);
  });
  it('aborts all outstanding reads on disposal without aborting a submitted write', async () => {
    const signals: Array<AbortSignal|undefined>=[];
    vi.stubGlobal('fetch',vi.fn((_path,options)=>{signals.push(options.signal);return Promise.resolve(response({}));}));
    const work=[api('/admin/api/state'),api('/admin/api/groups',{method:'POST',body:{}})];disposeClient();
    expect(signals[0]?.aborted).toBe(true);expect(signals[1]).toBeUndefined();await Promise.allSettled(work);
  });
});
