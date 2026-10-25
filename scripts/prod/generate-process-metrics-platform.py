"""Project existing opt-in SDK inputs and read-only evidence into Compose."""
import json
import os
from pathlib import Path
import re
import tempfile

ROOT = Path(__file__).resolve().parents[2]


def unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate process deployment field')
        result[key] = value
    return result


def private_directory(raw, *, outside=False):
    value = Path(raw)
    if (not value.is_absolute() or value.is_symlink() or not value.is_dir()
            or value.stat().st_mode & 0o077 or outside and value.resolve().is_relative_to(ROOT)):
        raise ValueError('private deployment directory required')
    return value


def render():
    directory = private_directory(os.environ['ADDP_METRICS_DEPLOYMENT_DIR'], outside=True)
    raw = os.environ.get('ADDP_PROCESS_METRICS_DEPLOYMENTS', '') or '[]'
    if len(raw.encode()) > 256 << 10:
        raise ValueError('process deployment budget')
    deployments = json.loads(raw, object_pairs_hook=unique)
    if not isinstance(deployments, list) or len(deployments) > 64:
        raise ValueError('process deployment list required')
    known = set(re.findall(r'^  ([a-z][a-z0-9-]+):$', (ROOT/'docker-compose.yml').read_text(), re.M))
    services = {}
    seen, endpoints = set(), set()
    fields = {'module_name','role','listen','endpoint','tls_dir'}
    for item in deployments:
        if not isinstance(item,dict) or not fields <= item.keys() or item.keys()-fields-{'cgroup_v2'}:
            raise ValueError('invalid SDK deployment fields')
        if any(not isinstance(item[k],str) or not item[k] or len(item[k])>1024 for k in fields):
            raise ValueError('invalid SDK deployment value')
        module, role = item['module_name'], item['role']
        if not re.fullmatch(r'[a-z][a-z0-9_-]{0,49}',module) or role not in ('backend','worker','scheduler','ingress'):
            raise ValueError('invalid SDK deployment identity')
        service = 'gateway' if (module,role)==('gateway','ingress') else module+'-'+role
        if service not in known or service in seen or item['endpoint'] in endpoints or not (ROOT/module/('go.mod' if module=='gateway' else 'backend/go.mod')).is_file():
            raise ValueError('unknown or duplicate SDK container deployment')
        if 'cgroup_v2' in item and type(item['cgroup_v2']) is not bool:
            raise ValueError('invalid cgroup opt-in')
        tls = private_directory(item['tls_dir'],outside=True)
        if {f.name for f in tls.iterdir()} != {'ca.crt','server.crt','server.key'}:
            raise ValueError('exact SDK TLS files required')
        for f in tls.iterdir():
            if f.is_symlink() or not f.is_file() or f.stat().st_nlink!=1 or not 0<f.stat().st_size<=256<<10 or f.name.endswith('.key') and f.stat().st_mode&0o077:
                raise ValueError('invalid SDK TLS file')
        projected=dict(item,tls_dir='/etc/addp/process-metrics/source')
        row=services.setdefault(service,{'environment':{},'volumes':[]})
        row['environment']['ADDP_PROCESS_METRICS_DEPLOYMENTS']=json.dumps([projected],separators=(',',':'))
        row['volumes'].append({'type':'bind','source':str(tls),'target':'/etc/addp/process-metrics/source','read_only':True,'bind':{'create_host_path':False}})
        seen.add(service);endpoints.add(item['endpoint'])
    fd,name=tempfile.mkstemp(prefix='.platform-process-metrics-',dir=directory)
    try:
        with os.fdopen(fd,'w') as out:
            json.dump({'services':services},out);out.flush();os.fsync(out.fileno())
        os.replace(name,directory/'platform-process-metrics.json')
    finally:
        Path(name).unlink(missing_ok=True)


if __name__=='__main__':
    try:
        render()
    except (ValueError,OSError,KeyError,TypeError,RecursionError):
        raise SystemExit('process_metrics_platform_deployment_invalid') from None
