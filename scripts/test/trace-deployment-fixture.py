"""One-run test credentials; emit only bounded, closed preparation diagnostics."""
from pathlib import Path
import importlib.util
import json
import os
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
STEPS = ('prepare', 'ca_server', 'ca_source', 'ca_query',
         'csr_server', 'sign_server', 'tls_server', 'csr_source', 'sign_source', 'tls_source',
         'csr_query', 'sign_query', 'tls_query', 'publish_ca', 'render', 'finalize')
CATEGORIES = ('none', 'openssl_command_failed', 'timeout', 'filesystem_error', 'renderer_rejected', 'unknown')
SCHEMA = 'addp.trace-credentials/v1'
LIMIT = 2048


def diagnostic(raw, *, failed):
    """Accept one complete ordered receipt; never export input strings or partial facts."""
    unavailable = {'status': 'unavailable'}
    if not isinstance(raw, bytes) or len(raw) > LIMIT:
        return unavailable
    try:
        value = json.loads(raw)
        canonical = json.dumps(value, separators=(',', ':')).encode()
    except (ValueError, UnicodeError, RecursionError):
        return unavailable
    if raw.strip() != canonical:
        return unavailable
    if not isinstance(value, dict) or set(value) != {'schema', 'completed_steps', 'step', 'error_category'}:
        return unavailable
    completed = value['completed_steps']
    if (value['schema'] != SCHEMA or not isinstance(completed, list) or
            len(completed) > len(STEPS) or completed != list(STEPS[:len(completed)]) or
            value['error_category'] not in CATEGORIES or
            failed == (value['error_category'] == 'none')):
        return unavailable
    if value['error_category'] == 'none':
        if completed != list(STEPS) or value['step'] != 'finalize':
            return unavailable
    elif len(completed) == len(STEPS) or value['step'] != STEPS[len(completed)]:
        return unavailable
    return {'status': 'complete', 'completed_steps': list(STEPS[:len(completed)]),
            'step': STEPS[-1] if len(completed) == len(STEPS) else STEPS[len(completed)],
            'error_category': CATEGORIES[CATEGORIES.index(value['error_category'])]}


def openssl(config, *args):
    subprocess.run(['openssl', *map(str, args)], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                   env=dict(os.environ, OPENSSL_CONF=str(config)))


def main(directory):
    completed = []
    step = 'prepare'
    category = 'none'
    code = 0
    try:
        w = Path(directory).resolve()
        center = w / 'deployment'
        center.mkdir(mode=0o700)
        config = w / 'openssl.cnf'
        with open(config, 'x', opener=lambda path, flags: os.open(path, flags, 0o600)) as out:
            out.write('[req]\nprompt = no\ndistinguished_name = dn\nx509_extensions = ca_extensions\n'
                      '[dn]\n[ca_extensions]\nsubjectKeyIdentifier = hash\nauthorityKeyIdentifier = keyid:always\n')
        completed.append('prepare')
        for authority in ['server', 'source', 'query']:
            step = 'ca_' + authority
            openssl(config, 'req', '-config', config, '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1', '-subj', '/CN=trace-test-'+authority,
                    '-addext', 'basicConstraints=critical,CA:TRUE', '-addext', 'keyUsage=critical,keyCertSign,cRLSign', '-keyout', w/(authority+'-ca.key'), '-out', w/(authority+'-ca.crt'))
            completed.append(step)
        for identity, authority, server in [('server','server',True),('source','source',False),('query','query',False)]:
            step = 'csr_' + identity
            dest = center if server else w/(identity+'-tls')
            if not server: dest.mkdir(mode=0o700)
            key = dest / ('server.key' if server else 'client.key')
            crt = dest / ('server.crt' if server else 'client.crt')
            openssl(config, 'req','-config',config,'-new','-newkey','rsa:2048','-nodes','-subj','/CN=trace-test-'+identity,'-keyout',key,'-out',w/(identity+'.csr'))
            completed.append(step)
            step = 'sign_' + identity
            extensions = w/(identity+'.ext')
            extensions.write_text('subjectKeyIdentifier=hash\nauthorityKeyIdentifier=keyid,issuer\n' +
                                  ('subjectAltName=DNS:localhost,DNS:trace-query,DNS:alloy,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n'
                                   if server else 'extendedKeyUsage=clientAuth\n'))
            openssl(config, 'x509','-req','-in',w/(identity+'.csr'),'-CA',w/(authority+'-ca.crt'),'-CAkey',w/(authority+'-ca.key'),'-CAcreateserial','-days','1','-extfile',extensions,'-out',crt)
            completed.append(step)
            step = 'tls_' + identity
            key.chmod(0o600)
            if not server: (dest/'ca.crt').write_bytes((w/'server-ca.crt').read_bytes())
            completed.append(step)
        step = 'publish_ca'
        for authority in ['source','query']: (center/(authority+'-ca.crt')).write_bytes((w/(authority+'-ca.crt')).read_bytes())
        completed.append(step)
        step = 'render'
        spec=importlib.util.spec_from_file_location('trace_renderer',ROOT/'scripts/infra/generate-trace-config.py')
        m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m);m.render(center,True)
        completed.append(step)
        step = 'finalize'
        # Tempo runs as its fixed UID; this non-secret rendered config is readable to it.
        (center/'tempo.yml').chmod(0o644)
        completed.append(step)
    except Exception as error:
        if isinstance(error, subprocess.CalledProcessError):
            category = 'openssl_command_failed'
        elif isinstance(error, (subprocess.TimeoutExpired, TimeoutError)):
            category = 'timeout'
        elif isinstance(error, OSError):
            category = 'filesystem_error'
        elif step == 'render' and isinstance(error, ValueError):
            category = 'renderer_rejected'
        else:
            category = 'unknown'
        code = 1
    print(json.dumps({'schema': SCHEMA, 'completed_steps': completed, 'step': step,
                      'error_category': category}, separators=(',', ':')), flush=True)
    return code


if __name__ == '__main__':
    sys.exit(main(sys.argv[1]))
