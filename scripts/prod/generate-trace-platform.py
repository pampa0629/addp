"""Project the sole explicit Go trace deployment into container-local mounts."""
from pathlib import Path
import json
import os
import re
import tempfile

ROOT = Path(__file__).resolve().parents[2]

def private_directory(raw):
    p = Path(raw)
    if not p.is_absolute() or not p.is_dir() or p.is_symlink() or p.resolve().is_relative_to(ROOT) or p.stat().st_mode & 0o077:
        raise ValueError('private trace directory required')
    if {f.name for f in p.iterdir()} != {'ca.crt','client.crt','client.key'}:
        raise ValueError('trace client requires exactly three files')
    for f in p.iterdir():
        if f.is_symlink() or not f.is_file() or f.stat().st_nlink != 1 or not 0 < f.stat().st_size <= 262144 or (f.name.endswith('.key') and f.stat().st_mode & 0o077):
            raise ValueError('invalid trace client file')
    return p

def strict_object(pairs):
    result={}
    for key,value in pairs:
        if key in result: raise ValueError('duplicate trace input field')
        result[key]=value
    return result

def render():
    directory = Path(os.environ['ADDP_TRACE_DEPLOYMENT_DIR'])
    if not directory.is_absolute() or directory.is_symlink() or not directory.is_dir() or directory.resolve().is_relative_to(ROOT) or directory.stat().st_mode & 0o077:
        raise ValueError('private trace center directory required')
    raw = os.environ.get('ADDP_TRACE_DEPLOYMENTS','')
    values = json.loads(raw,object_pairs_hook=strict_object) if raw else []
    if not isinstance(values,list) or len(values)>1000: raise ValueError('invalid deployment array')
    known = set(re.findall(r'^  ([a-z][a-z0-9-]+):$',(ROOT/'docker-compose.yml').read_text(),re.M))
    services = {}
    for value in values:
        if not isinstance(value,dict) or set(value)!={'module_name','role','endpoint','tls_dir'}: raise ValueError('invalid deployment')
        module,role = value['module_name'],value['role']
        service = 'gateway' if module=='gateway' and role=='ingress' else module+'-backend' if role=='backend' else ''
        if service not in known or service in services or (service != 'gateway' and not (ROOT/module/'backend/go.mod').is_file()): raise ValueError('unknown or duplicate container deployment')
        from urllib.parse import urlsplit
        endpoint=urlsplit(value['endpoint'])
        if endpoint.scheme!='https' or not endpoint.hostname or endpoint.username or endpoint.path!='/v1/traces' or endpoint.query or endpoint.fragment: raise ValueError('invalid OTLP endpoint')
        p=private_directory(value['tls_dir'])
        projected=dict(value,tls_dir='/etc/addp/traces/source')
        services[service]={'environment':{'ADDP_OBSERVABILITY_TRACES_ENABLED':'true','ADDP_TRACE_DEPLOYMENTS':json.dumps([projected],separators=(',',':'))},'volumes':[{'type':'bind','source':str(p),'target':'/etc/addp/traces/source','read_only':True,'bind':{'create_host_path':False}}]}
    query = os.environ.get('MONITOR_TEMPO_URL','')
    if query:
        from urllib.parse import urlsplit
        endpoint=urlsplit(query)
        if endpoint.scheme!='https' or not endpoint.hostname or endpoint.username or endpoint.path or endpoint.query or endpoint.fragment: raise ValueError('invalid Tempo query origin')
        p=private_directory(os.environ.get('MONITOR_TRACE_TLS_DIR',''))
        row=services.setdefault('monitor-backend',{'environment':{},'volumes':[]})
        row['environment'].update(ADDP_OBSERVABILITY_TRACES_ENABLED='true',MONITOR_TEMPO_URL=query,MONITOR_TRACE_TLS_DIR='/etc/addp/traces/query')
        row['volumes'].append({'type':'bind','source':str(p),'target':'/etc/addp/traces/query','read_only':True,'bind':{'create_host_path':False}})
    fd, name = tempfile.mkstemp(prefix='.platform-traces-',dir=directory)
    try:
        with os.fdopen(fd,'w') as f: json.dump({'services':services},f);f.flush();os.fsync(f.fileno())
        os.chmod(name,0o600);os.replace(name,directory/'platform-traces.json')
    finally: Path(name).unlink(missing_ok=True)

if __name__=='__main__':
    try: render()
    except (ValueError,OSError,KeyError,TypeError): raise SystemExit('trace_platform_deployment_invalid') from None
