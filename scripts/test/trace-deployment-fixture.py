"""One-run test credentials. No personal deployment is modified."""
from pathlib import Path
import importlib.util
import os
import subprocess
import sys

root = Path(__file__).resolve().parents[2]
w = Path(sys.argv[1]).resolve()
center = w / 'deployment'
center.mkdir(mode=0o700)
def openssl(*args):
    subprocess.run(['openssl', *map(str, args)], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
for authority in ['server', 'source', 'query']:
    openssl('req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1', '-subj', '/CN=trace-test-'+authority,
            '-addext', 'basicConstraints=critical,CA:TRUE', '-addext', 'keyUsage=critical,keyCertSign,cRLSign', '-keyout', w/(authority+'-ca.key'), '-out', w/(authority+'-ca.crt'))
for identity, authority, server in [('server','server',True),('source','source',False),('query','query',False)]:
    dest = center if server else w/(identity+'-tls')
    if not server: dest.mkdir(mode=0o700)
    key = dest / ('server.key' if server else 'client.key')
    crt = dest / ('server.crt' if server else 'client.crt')
    openssl('req','-new','-newkey','rsa:2048','-nodes','-subj','/CN=trace-test-'+identity,'-keyout',key,'-out',w/(identity+'.csr'))
    extensions = w/(identity+'.ext')
    extensions.write_text('subjectAltName=DNS:localhost,DNS:trace-query,DNS:alloy,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n' if server else 'extendedKeyUsage=clientAuth\n')
    openssl('x509','-req','-in',w/(identity+'.csr'),'-CA',w/(authority+'-ca.crt'),'-CAkey',w/(authority+'-ca.key'),'-CAcreateserial','-days','1','-extfile',extensions,'-out',crt)
    key.chmod(0o600)
    if not server: (dest/'ca.crt').write_bytes((w/'server-ca.crt').read_bytes())
for authority in ['source','query']: (center/(authority+'-ca.crt')).write_bytes((w/(authority+'-ca.crt')).read_bytes())
spec=importlib.util.spec_from_file_location('trace_renderer',root/'scripts/infra/generate-trace-config.py')
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m);m.render(center,True)
# Tempo runs as its fixed UID; this non-secret rendered config is readable to it.
(center/'tempo.yml').chmod(0o644)
