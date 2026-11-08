"""Render the sole shared Alloy configuration from Monitor's fixed policy.

Default rendering consumes deployment-owned secrets. Explicit development
preparation creates local certificates only; neither operation activates services.
"""
from pathlib import Path
import json
import os
import stat
import tempfile
import argparse
import re
import shlex
import shutil
import subprocess
import time

ROOT = Path(__file__).resolve().parents[2]
FILES = ('server.crt', 'server.key', 'source-ca.crt', 'query-ca.crt')


def prepare_development(directory, producers, otlp_port, query_port):
    """Prepare explicit native-development inputs, leaving activation to the deployer."""
    p = Path(directory)
    if (os.environ.get('ENV') == 'production' or not p.is_absolute() or p.exists() or p.is_symlink()
            or p.resolve().is_relative_to(ROOT) or not p.parent.is_dir()):
        raise ValueError('A new development directory outside the repository is required')
    if not 0 < otlp_port <= 65535 or not 0 < query_port <= 65535 or otlp_port == query_port:
        raise ValueError('Distinct loopback deployment ports are required')
    bindings = []
    for producer in producers:
        match = re.fullmatch(r'([a-z][a-z0-9_-]{0,49}):(backend|ingress)', producer)
        if match is None or match.groups() in bindings:
            raise ValueError('Unique module:backend or module:ingress producers are required')
        bindings.append(match.groups())
    if not bindings:
        raise ValueError('At least one explicit producer is required')
    p.mkdir(mode=0o700)
    deadline = time.monotonic() + 60
    try:
        authority = p/'authorities'; authority.mkdir(mode=0o700)
        center = p/'center'; center.mkdir(mode=0o700)
        sources = p/'sources'; sources.mkdir(mode=0o700)
        config = authority/'openssl.cnf'
        config.write_text('[req]\nprompt = no\ndistinguished_name = dn\n[dn]\n')
        env = dict(os.environ, OPENSSL_CONF=str(config))
        def openssl(*args):
            remaining = deadline-time.monotonic()
            if remaining <= 0:
                raise TimeoutError('Development preparation deadline exceeded')
            subprocess.run(['openssl',*map(str,args)],env=env,check=True,
                           stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=min(10,remaining))
        for name in ('server','source','query'):
            openssl('req','-config',config,'-x509','-newkey','rsa:2048','-nodes','-days','30',
                '-subj','/CN=addp-local-trace-'+name,'-addext','basicConstraints=critical,CA:TRUE',
                '-addext','keyUsage=critical,keyCertSign,cRLSign',
                '-keyout',authority/(name+'.key'),'-out',authority/(name+'.crt'))
        identities = [('center','server',True,center),('query','query',False,p/'query')]
        identities += [(module+'-'+role,'source',False,sources/(module+'-'+role)) for module,role in bindings]
        for name,ca,is_server,dest in identities:
            if not is_server:
                dest.mkdir(mode=0o700)
            prefix = 'server' if is_server else 'client'
            csr = authority/(name+'.csr'); extensions = authority/(name+'.ext')
            extensions.write_text('basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\n'
                'subjectKeyIdentifier=hash\nauthorityKeyIdentifier=keyid,issuer\nextendedKeyUsage='+
                ('serverAuth\nsubjectAltName=DNS:localhost,IP:127.0.0.1\n' if is_server else 'clientAuth\n'))
            openssl('req','-config',config,'-new','-newkey','rsa:2048','-nodes','-subj','/CN=addp-local-trace-'+name,
                '-keyout',dest/(prefix+'.key'),'-out',csr)
            openssl('x509','-req','-in',csr,'-CA',authority/(ca+'.crt'),'-CAkey',authority/(ca+'.key'),
                '-CAcreateserial','-days','30','-extfile',extensions,'-out',dest/(prefix+'.crt'))
            if not is_server:
                (dest/'ca.crt').write_bytes((authority/'server.crt').read_bytes())
        for name in ('source','query'):
            (center/(name+'-ca.crt')).write_bytes((authority/(name+'.crt')).read_bytes())
        for file in p.rglob('*'):
            if file.is_file():
                file.chmod(0o600)
        values = {'ADDP_OBSERVABILITY_TRACES_ENABLED':'true', 'ADDP_TRACE_DEPLOYMENT_DIR':str(center),
            'TRACE_OTLP_PORT':str(otlp_port),'TRACE_QUERY_PORT':str(query_port),
            'ADDP_TRACE_DEPLOYMENTS':json.dumps([{'module_name':module,'role':role,
                'endpoint':f'https://127.0.0.1:{otlp_port}/v1/traces','tls_dir':str(sources/(module+'-'+role))}
                for module,role in bindings],separators=(',',':')),
            'MONITOR_TEMPO_URL':f'https://127.0.0.1:{query_port}','MONITOR_TRACE_TLS_DIR':str(p/'query')}
        with open(p/'deployment.env','x',opener=lambda path,flags:os.open(path,flags,0o600)) as out:
            out.write(''.join(key+'='+shlex.quote(value)+'\n' for key,value in values.items()))
        flag = os.environ.get('ADDP_OBSERVABILITY_LOGS_ENABLED','true')
        if flag not in ('true','false'):
            raise ValueError('Invalid log capability selection')
        render(center,flag == 'true')
        if time.monotonic() >= deadline:
            raise TimeoutError('Development preparation deadline exceeded')
    except BaseException:
        shutil.rmtree(p)
        raise


def render(directory, logs):
    p = Path(directory)
    if not p.is_absolute() or p.is_symlink() or not p.is_dir() or p.resolve().is_relative_to(ROOT):
        raise ValueError('Trace deployment requires a private absolute directory outside the repository')
    if p.stat().st_mode & 0o077:
        raise ValueError('Trace deployment directory must be owner-only')
    for name in FILES:
        f = p / name
        if f.is_symlink() or not f.is_file() or not 0 < f.stat().st_size <= 256 * 1024 or f.stat().st_nlink != 1:
            raise ValueError('Invalid trace certificate file')
        if name.endswith('.key') and stat.S_IMODE(f.stat().st_mode) & 0o077:
            raise ValueError('Trace key must be owner-only')
    policy = json.loads((ROOT / 'monitor/backend/internal/tracequery/policy.json').read_text())
    if (policy.get('version') != 2 or set(policy) != {'version', 'decision_wait_seconds', 'pending_traces',
            'normal_percent', 'slow_ms', 'retained_spans_per_second', 'retention_hours'}
            or any(type(v) is not int or v <= 0 for v in policy.values()) or policy['normal_percent'] > 100):
        raise ValueError('Unpublished trace policy definition')
    text = (ROOT / 'scripts/infra/traces.alloy').read_text()
    for k, v in dict(DECISION_WAIT=policy['decision_wait_seconds'], PENDING_TRACES=policy['pending_traces'],
                     RETAINED_SPANS=policy['retained_spans_per_second'], SLOW_MS=policy['slow_ms'],
                     NORMAL_PERCENT=policy['normal_percent']).items():
        text = text.replace('@' + k + '@', str(v))
    if logs:
        text = (ROOT / 'scripts/infra/runtime-logs.alloy').read_text() + '\n' + text
    text = '// addp.trace.policy/v2; definition version 2, deployment intent only\n' + text
    fd, temporary = tempfile.mkstemp(prefix='.alloy-', dir=p)
    try:
        with os.fdopen(fd, 'w') as out:
            out.write(text)
            out.flush()
            os.fsync(out.fileno())
        os.chmod(temporary, 0o600)
        os.replace(temporary, p / 'runtime.alloy')
    finally:
        Path(temporary).unlink(missing_ok=True)
    tempo = (ROOT / 'scripts/infra/tempo.yml').read_text().replace('@RETENTION_HOURS@', str(policy['retention_hours'])).replace('@INGESTION_SLACK@', str(policy['decision_wait_seconds'] + 180))
    fd, temporary = tempfile.mkstemp(prefix='.tempo-', dir=p)
    try:
        with os.fdopen(fd, 'w') as out:
            out.write(tempo)
            out.flush()
            os.fsync(out.fileno())
        os.chmod(temporary, 0o644)
        os.replace(temporary, p / 'tempo.yml')
    finally:
        Path(temporary).unlink(missing_ok=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='action')
    development = commands.add_parser('prepare-development', help='Prepare new local-only deployment inputs; never activate')
    development.add_argument('--directory',required=True)
    development.add_argument('--producer',action='append',required=True)
    development.add_argument('--otlp-port',type=int,required=True)
    development.add_argument('--query-port',type=int,required=True)
    args = parser.parse_args()
    try:
        if args.action == 'prepare-development':
            prepare_development(args.directory,args.producer,args.otlp_port,args.query_port)
            print('Development Trace deployment inputs prepared; no environment or service was activated')
        else:
            flag = os.environ.get('ADDP_OBSERVABILITY_LOGS_ENABLED', 'true')
            if flag not in ('true', 'false'):
                raise ValueError('Invalid log capability selection')
            render(os.environ.get('ADDP_TRACE_DEPLOYMENT_DIR', ''), flag == 'true')
    except (ValueError, OSError, KeyError, TimeoutError, subprocess.SubprocessError):
        raise SystemExit('trace_deployment_invalid') from None
