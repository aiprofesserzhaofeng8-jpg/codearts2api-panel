"""Shared acceptance helpers. Reports never contain request credentials."""
import datetime, hashlib, http.cookiejar, json, os, pathlib, re, time, urllib.error, urllib.request
os.umask(0o077)
ROOT=pathlib.Path(__file__).resolve().parents[1]
def utc(): return datetime.datetime.now(datetime.timezone.utc).isoformat()
def private_json(path,value):
 path=pathlib.Path(path);path.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
 temp=path.with_suffix(path.suffix+'.tmp');fd=os.open(temp,os.O_WRONLY|os.O_CREAT|os.O_TRUNC,0o600)
 with os.fdopen(fd,'w') as f: json.dump(value,f,ensure_ascii=False,indent=2);f.write('\n')
 os.chmod(temp,0o600);os.replace(temp,path)
def save_json(path,value):
 path=pathlib.Path(path);path.parent.mkdir(parents=True,exist_ok=True)
 temp=path.with_suffix(path.suffix+'.tmp');temp.write_text(json.dumps(value,ensure_ascii=False,indent=2)+'\n');os.replace(temp,path)
def scrub(text,secrets=()):
 for secret in secrets:
  if secret and len(secret)>6:text=text.replace(secret,'[REDACTED]')
 return re.sub(r'sk-ca-[a-zA-Z0-9]{12,}','[REDACTED_API_KEY]',text)
class API:
 def __init__(self,base,key=None,password=None):
  self.base=base.rstrip('/');self.key=key;self.password=password;self.jar=http.cookiejar.CookieJar();self.op=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))
 def request(self,path,data=None,method=None,headers=None,timeout=180):
  h={'Content-Type':'application/json',**(headers or {})}
  if self.key:h['Authorization']='Bearer '+self.key
  req=urllib.request.Request(self.base+path,None if data is None else json.dumps(data).encode(),headers=h,method=method)
  try:
   with self.op.open(req,timeout=timeout) as r:return r.status,r.read().decode(),dict(r.headers)
  except urllib.error.HTTPError as e:return e.code,e.read().decode(),dict(e.headers)
 def json(self,path,data=None,method=None):
  code,raw,_=self.request(path,data,method)
  if code>=400:raise RuntimeError('HTTP %d at %s: %s'%(code,path,scrub(raw,(self.key,self.password))[:350]))
  return json.loads(raw)
 def login(self):return self.json('/admin/auth/login',{'password':self.password})
 def add_key(self,name,budget):
  k=self.json('/admin/api/keys',{'name':name})['key'];ref='key_'+hashlib.sha256(k.encode()).hexdigest()[:24]
  self.json('/admin/api/keys/budget',{'ref':ref,'budget':budget});return k,ref
 def delete_key(self,ref):return self.json('/admin/api/keys?key='+ref,method='DELETE')
def current_access():
 s=json.loads((ROOT/'data/state.json').read_text());a=json.loads((ROOT/'data/state.json.access.json').read_text())
 return s,a['admin_password']
def wait_ready(base):
 for _ in range(50):
  try:
   if API(base).request('/ping',timeout=2)[1]=='pong':return
  except OSError:pass
  time.sleep(.2)
 raise RuntimeError('isolated fixture did not become ready')
def text_chat(obj):return ''.join(c.get('message',{}).get('content') or '' for c in obj.get('choices',[]))
def text_responses(obj):return ''.join(p.get('text','') for o in obj.get('output',[]) for p in o.get('content',[]) if p.get('type')=='output_text')
def text_messages(obj):return ''.join(p.get('text','') for p in obj.get('content',[]) if p.get('type')=='text')
def sse(raw):return [json.loads(l[6:]) for l in raw.splitlines() if l.startswith('data: ') and l!='data: [DONE]']
