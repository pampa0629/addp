"""Prepare private local FTP configuration; never call platform APIs."""
import hashlib
import json
import os
from pathlib import Path
import re
import ssl
import subprocess
import sys

from config import ROOT, server, users
from seed import write_samples


def identity(username, password):
    return hashlib.sha256(json.dumps([username, password], ensure_ascii=False).encode()).hexdigest() + '\n'


def prepare(root, username, password, security):
    if not re.fullmatch(r'[A-Za-z][A-Za-z0-9_]{2,63}', username):
        raise ValueError('FTP_USER must be an explicit ordinary username')
    if len(password) < 12 or any(c in password for c in '\r\n\0'):
        raise ValueError('FTP_PASSWORD requires at least 12 characters without control characters')
    if security not in ('explicit_tls', 'plain'):
        raise ValueError('FTP_SECURITY must be explicit_tls or plain')
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    root.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    root.parent.chmod(0o700)
    root.chmod(0o755)
    fingerprint = identity(username, password)
    marker = root / 'identity.sha256'
    if marker.exists() and marker.read_text() != fingerprint:
        raise ValueError('FTP account differs from existing configuration; implicit rotation is forbidden')
    cert, key = root / 'server.crt', root / 'server.key'
    if cert.exists() != key.exists():
        raise ValueError('FTP TLS certificate and private key must both exist')
    if not cert.exists():
        subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:3072', '-nodes', '-days', '365',
                        '-subj', '/CN=ADDP Business FTP',
                        '-addext', 'subjectAltName=DNS:localhost,IP:127.0.0.1',
                        '-keyout', str(key), '-out', str(cert)],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    # Unprivileged server reads this private bind mount; host parent remains 0700.
    for path, text in ((root / 'sftpgo.json', json.dumps(server(50000, 50009, 1 if security == 'explicit_tls' else 0))),
                       (root / 'users.json', json.dumps(users(username, password))),
                       (marker, fingerprint)):
        path.write_text(text)
        path.chmod(0o644)
    key.chmod(0o644)
    cert.chmod(0o644)


def probe(root, username, password, security):
    import ftplib
    if security == 'explicit_tls':
        client = ftplib.FTP_TLS(context=ssl.create_default_context(cafile=str(root / 'server.crt')), timeout=2)
    else:
        client = ftplib.FTP(timeout=2)
    with client:
        client.connect('127.0.0.1', 2121)
        client.login(username, password)
        if security == 'explicit_tls':
            client.prot_p()
        names = client.nlst('/examples')
        if len(names) < 3:
            raise ValueError('FTP Business samples are incomplete')


if __name__ == '__main__':
    root = ROOT / 'business/.business-state/ftp/config'
    args = (root, os.environ.get('FTP_USER', 'business_reader'), os.environ.get('FTP_PASSWORD', ''),
            os.environ.get('FTP_SECURITY', 'explicit_tls'))
    try:
        {'prepare': prepare, 'probe': probe}[sys.argv[1]](*args)
        if sys.argv[1] == 'prepare':
            write_samples(ROOT / 'business/ftp/data')
    except Exception as exc:
        # FTP server responses and certificate details can contain private facts.
        print('FTP configuration/probe failed: ' + (str(exc) if isinstance(exc, ValueError) else type(exc).__name__), file=sys.stderr)
        sys.exit(1)
