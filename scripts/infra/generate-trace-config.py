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
from datetime import datetime, timezone
import secrets

ROOT = Path(__file__).resolve().parents[2]
FILES = ('server.crt', 'server.key', 'source-ca.crt', 'query-ca.crt')


def openssl(config, deadline, *args, capture=False):
    remaining = deadline-time.monotonic()
    if remaining <= 0:
        raise TimeoutError('Development preparation deadline exceeded')
    return subprocess.run(['openssl', *map(str, args)],
        env=dict(os.environ, OPENSSL_CONF=str(config)), check=True,
        stdout=subprocess.PIPE if capture else subprocess.DEVNULL,
        stderr=subprocess.DEVNULL, timeout=min(10, remaining)).stdout


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
        def run(*args):
            return openssl(config, deadline, *args)
        for name in ('server','source','query'):
            run('req','-config',config,'-x509','-newkey','rsa:2048','-nodes','-days','30',
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
            run('req','-config',config,'-new','-newkey','rsa:2048','-nodes','-subj','/CN=addp-local-trace-'+name,
                '-keyout',dest/(prefix+'.key'),'-out',csr)
            run('x509','-req','-in',csr,'-CA',authority/(ca+'.crt'),'-CAkey',authority/(ca+'.key'),
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


def prepare_development_producer(directory, producer):
    """Append the approved Manager source to existing local preparation inputs."""
    p = Path(directory)
    deadline = time.monotonic()+60
    if os.environ.get('ENV') == 'production' or producer != 'manager:backend':
        raise ValueError('Only the approved development Manager source can be appended')
    for d in (p, p/'authorities', p/'center', p/'sources'):
        if not d.is_absolute() or d.is_symlink() or not d.is_dir() or d.resolve().is_relative_to(ROOT) or d.stat().st_mode & 0o077:
            raise ValueError('Private development preparation directories required')
    manifest = p/'deployment.env'
    for f in (manifest, p/'authorities/source.crt', p/'authorities/source.key',
              p/'authorities/server.crt', p/'center/source-ca.crt'):
        if f.is_symlink() or not f.is_file() or f.stat().st_nlink != 1 or not 0 < f.stat().st_size <= 256*1024 or f.stat().st_mode & 0o077:
            raise ValueError('Invalid private development preparation material')
    original = manifest.read_bytes()
    rows = original.decode().splitlines(keepends=True)
    values = {}
    for line in rows:
        tokens = shlex.split(line)
        if len(tokens) != 1 or '=' not in tokens[0]:
            raise ValueError('Invalid development deployment file')
        key, value = tokens[0].split('=', 1)
        if key in values or not line.startswith(key+'='):
            raise ValueError('Duplicate development deployment field')
        values[key] = value
    if set(values) != {'ADDP_OBSERVABILITY_TRACES_ENABLED', 'ADDP_TRACE_DEPLOYMENT_DIR',
            'TRACE_OTLP_PORT', 'TRACE_QUERY_PORT', 'ADDP_TRACE_DEPLOYMENTS', 'MONITOR_TEMPO_URL', 'MONITOR_TRACE_TLS_DIR'}:
        raise ValueError('Unexpected development deployment fields')
    otlp, query = int(values['TRACE_OTLP_PORT']), int(values['TRACE_QUERY_PORT'])
    endpoint = f'https://127.0.0.1:{otlp}/v1/traces'
    if (values['ADDP_OBSERVABILITY_TRACES_ENABLED'] != 'true' or not 0 < otlp <= 65535
            or not 0 < query <= 65535 or otlp == query
            or values['ADDP_TRACE_DEPLOYMENT_DIR'] != str(p/'center')
            or values['MONITOR_TRACE_TLS_DIR'] != str(p/'query')
            or values['MONITOR_TEMPO_URL'] != f'https://127.0.0.1:{query}'):
        raise ValueError('Development deployment identity mismatch')
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError('Duplicate source field')
            result[key] = value
        return result
    deployments = json.loads(values['ADDP_TRACE_DEPLOYMENTS'], object_pairs_hook=unique)
    if not isinstance(deployments, list) or not 0 < len(deployments) < 1000:
        raise ValueError('Invalid source array')
    seen = set()
    for row in deployments:
        if not isinstance(row, dict) or set(row) != {'module_name','role','endpoint','tls_dir'} or any(not isinstance(v,str) for v in row.values()):
            raise ValueError('Invalid source declaration')
        module, role = row['module_name'], row['role']
        if (not re.fullmatch(r'[a-z][a-z0-9_-]{0,49}', module) or role not in ('backend','ingress')
                or (module,role) in seen or row['endpoint'] != endpoint
                or row['tls_dir'] != str(p/'sources'/(module+'-'+role))):
            raise ValueError('Source identity mismatch')
        seen.add((module,role))
        tls = Path(row['tls_dir'])
        if tls.is_symlink() or not tls.is_dir() or tls.stat().st_mode & 0o077 or {f.name for f in tls.iterdir()} != {'ca.crt','client.crt','client.key'}:
            raise ValueError('Invalid existing source directory')
        for f in tls.iterdir():
            if f.is_symlink() or not f.is_file() or f.stat().st_nlink != 1 or not 0 < f.stat().st_size <= 256*1024 or f.stat().st_mode & 0o077:
                raise ValueError('Invalid existing source file')
        if (tls/'ca.crt').read_bytes() != (p/'authorities/server.crt').read_bytes():
            raise ValueError('Existing source server trust mismatch')
    target = p/'sources/manager-backend'
    if ('manager','backend') in seen or target.exists() or target.is_symlink():
        raise ValueError('Manager source already exists')
    if (p/'authorities/source.crt').read_bytes() != (p/'center/source-ca.crt').read_bytes():
        raise ValueError('Source CA does not match current center trust')
    with tempfile.TemporaryDirectory(prefix='.producer-', dir=p) as temporary:
        scratch = Path(temporary)
        config = scratch/'openssl.cnf'
        config.write_text('[req]\nprompt = no\ndistinguished_name = dn\n[dn]\n'); config.chmod(0o600)
        end = openssl(config, deadline, 'x509', '-in', p/'authorities/source.crt', '-noout', '-enddate', '-checkend', '86400', capture=True)
        expires = datetime.strptime(end.decode().splitlines()[0], 'notAfter=%b %d %H:%M:%S %Y GMT').replace(tzinfo=timezone.utc)
        days = min(30, int((expires-datetime.now(timezone.utc)).total_seconds()//86400))
        if days < 1:
            raise ValueError('Source CA expires too soon')
        target.mkdir(mode=0o700)
        try:
            csr, extensions = scratch/'client.csr', scratch/'client.ext'
            extensions.write_text('basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\n'
                                  'subjectKeyIdentifier=hash\nauthorityKeyIdentifier=keyid,issuer\nextendedKeyUsage=clientAuth\n')
            extensions.chmod(0o600)
            openssl(config, deadline, 'req', '-config', config, '-new', '-newkey', 'rsa:2048', '-nodes',
                    '-subj', '/CN=addp-local-trace-manager-backend', '-keyout', target/'client.key', '-out', csr)
            (target/'client.key').chmod(0o600); csr.chmod(0o600)
            openssl(config, deadline, 'x509', '-req', '-in', csr, '-CA', p/'authorities/source.crt',
                    '-CAkey', p/'authorities/source.key', '-set_serial', str(secrets.randbits(127)+1),
                    '-days', str(days), '-extfile', extensions, '-out', target/'client.crt')
            (target/'client.crt').chmod(0o600)
            (target/'ca.crt').write_bytes((p/'authorities/server.crt').read_bytes()); (target/'ca.crt').chmod(0o600)
            openssl(config, deadline, 'verify', '-CAfile', p/'center/source-ca.crt', '-purpose', 'sslclient', target/'client.crt')
            deployments.append({'module_name':'manager','role':'backend','endpoint':endpoint,'tls_dir':str(target)})
            declaration = json.dumps(deployments, separators=(',',':'))
            if len(declaration.encode()) > 256*1024:
                raise ValueError('Source declaration exceeds SDK input budget')
            output = scratch/'deployment.env'
            output.write_text(''.join('ADDP_TRACE_DEPLOYMENTS='+shlex.quote(declaration)+'\n' if line.startswith('ADDP_TRACE_DEPLOYMENTS=') else line for line in rows))
            output.chmod(0o600)
            if manifest.read_bytes() != original or time.monotonic() >= deadline:
                raise ValueError('Development preparation changed or expired')
            os.replace(output, manifest)
        except BaseException:
            shutil.rmtree(target)
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
    source = commands.add_parser('prepare-development-producer', help='Append the approved Manager source; never activate')
    source.add_argument('--directory',required=True)
    source.add_argument('--producer',required=True,choices=['manager:backend'])
    args = parser.parse_args()
    try:
        if args.action == 'prepare-development':
            prepare_development(args.directory,args.producer,args.otlp_port,args.query_port)
            print('Development Trace deployment inputs prepared; no environment or service was activated')
        elif args.action == 'prepare-development-producer':
            prepare_development_producer(args.directory,args.producer)
            print('Development Manager Trace inputs appended; no environment or service was activated')
        else:
            flag = os.environ.get('ADDP_OBSERVABILITY_LOGS_ENABLED', 'true')
            if flag not in ('true', 'false'):
                raise ValueError('Invalid log capability selection')
            render(os.environ.get('ADDP_TRACE_DEPLOYMENT_DIR', ''), flag == 'true')
    except (ValueError, OSError, KeyError, TimeoutError, subprocess.SubprocessError):
        raise SystemExit('trace_deployment_invalid') from None
