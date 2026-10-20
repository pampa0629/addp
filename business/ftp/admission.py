"""Disposable FTP owner: private credentials, matching ports and labelled cleanup."""
import json
import ftplib
import io
import ssl
import os
from pathlib import Path
import platform
import secrets
import socket
import subprocess
import sys
import time

from config import ROOT, image, server, users
from prepare import identity
from seed import write_samples


def run(*args, **kwargs):
    return subprocess.run(args, check=True, text=True, capture_output=True, **kwargs).stdout.strip()


def write(path, value, mode=0o600):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2))
    path.chmod(mode)


def owner():
    path = Path(os.environ['ADDP_FTP_ADMISSION_DIR']).resolve()
    if not path.is_dir() or path.stat().st_mode & 0o077:
        raise ValueError('admission directory must be private')
    return path


def allocate():
    # Hold all candidate ports until the complete set is available. Docker bind
    # still arbitrates the release/start race; a collision fails this fixture.
    for _ in range(100):
        start = 30000 + secrets.randbelow(25000)
        held = []
        try:
            for port in range(start, start + 11):
                sock = socket.socket()
                held.append(sock)
                sock.bind(('127.0.0.1', port))
            return start
        except OSError:
            pass
        finally:
            for sock in held:
                sock.close()
    raise RuntimeError('no isolated matching passive range available')


def start():
    root = owner()
    if (root / 'state.json').exists():
        raise ValueError('fixture already owns resources')
    name = 'addp-ftp-' + secrets.token_hex(6)
    state = {'name': name, 'label': 'org.addp.ftp.owner=' + name,
             'volumes': [name + '-data', name + '-state']}
    write(root / 'state.json', state)
    port = allocate()
    password = secrets.token_urlsafe(24)
    config = root / 'config'
    config.mkdir(mode=0o755)
    settings = server(port + 1, port + 10, tls_mode=0)
    settings['ftpd']['bindings'][0]['force_passive_ip'] = '203.0.113.123'
    write(config / 'sftpgo.json', settings, 0o644)
    write(config / 'users.json', users('fixture_reader', password), 0o644)
    (config / 'identity.sha256').write_text(identity('fixture_reader', password))
    (config / 'identity.sha256').chmod(0o644)
    run('openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '2',
        '-subj', '/CN=ADDP FTP admission', '-addext', 'subjectAltName=IP:127.0.0.1',
        '-keyout', str(config / 'server.key'), '-out', str(config / 'server.crt'))
    # The bind mount exposes only this private fixture's directory. Unprivileged
    # image UID 1000 must be able to read its TLS files.
    (config / 'server.key').chmod(0o644)
    write_samples(root / 'seed')
    pinned = image()
    run('docker', 'pull', pinned)
    for volume in state['volumes']:
        run('docker', 'volume', 'create', '--label', state['label'], volume)
    args = ['docker', 'run', '-d', '--name', name, '--label', state['label'],
            '--memory', '512m', '-p', f'127.0.0.1:{port}:2121',
            '-p', f'127.0.0.1:{port+1}-{port+10}:{port+1}-{port+10}',
            '-v', f'{config}:/addp/config:ro',
            '-v', f'{ROOT / "business/ftp"}:/addp/ftp:ro',
            '-v', f'{root / "seed"}:/addp/samples:ro',
            '--entrypoint', 'sh',
            '-v', f'{state["volumes"][0]}:/srv/sftpgo',
            '-v', f'{state["volumes"][1]}:/var/lib/sftpgo', pinned, '/addp/ftp/entrypoint.sh']
    state['args'] = args
    write(root / 'state.json', state)
    run(*args)
    run('docker', 'exec', '-u', '0', name, 'sh', '-ec',
        'mkdir -p /srv/sftpgo/data/examples; chown -R 1000:1000 /srv/sftpgo/data')
    run('docker', 'exec', '-u', '0', name, 'sh', '-ec',
        'mkdir -p /srv/sftpgo/data/unsupported; ln -s ../examples/demo.json /srv/sftpgo/data/unsupported/link')
    samples = root / 'samples'
    samples.mkdir()
    (samples / '中文 空格%样例.csv').write_text('id,name,amount\n1,长沙,100.25\n2,株洲,200.50\n')
    (samples / 'demo.json').write_text('[{"id":1,"name":"长沙"},{"id":2,"name":"株洲"}]')
    (samples / 'range.bin').write_bytes(bytes(range(256)) * 32768)
    (samples / '.hidden.txt').write_text('hidden sample')
    run('docker', 'cp', str(samples) + '/.', name + ':/srv/sftpgo/data/examples')
    run('docker', 'exec', '-u', '0', name, 'chown', '-R', '1000:1000', '/srv/sftpgo/data')
    for _ in range(90):
        try:
            with socket.create_connection(('127.0.0.1', port), timeout=1) as sock:
                if sock.recv(256).startswith(b'220'):
                    break
        except OSError:
            pass
        if run('docker', 'inspect', '-f', '{{.State.Running}}', name) != 'true':
            raise RuntimeError('FTP fixture exited before readiness')
        time.sleep(1)
    else:
        raise RuntimeError('FTP fixture readiness timed out')
    tls = ssl.create_default_context(cafile=str(config / 'server.crt'))
    with ftplib.FTP_TLS(context=tls, timeout=5) as probe:
        probe.connect('127.0.0.1', port)
        probe.login('fixture_reader', password)
        probe.prot_p()
        link_facts = probe.sendcmd("MLST /unsupported/link")
        root_facts = probe.sendcmd("MLST /")
        for expected, operation in ((553, lambda: probe.storbinary('STOR /forbidden.txt', io.BytesIO(b'no'))),
                                    (550, lambda: probe.delete('/examples/demo.json'))):
            try:
                operation()
            except ftplib.error_perm as exc:
                if not str(exc).startswith(str(expected)):
                    raise RuntimeError('FTP native permission denial has wrong status')
            else:
                raise RuntimeError('FTP reader unexpectedly has mutation permissions')
    write(root / 'connection.json', {'host': '127.0.0.1', 'port': port,
          'user': 'fixture_reader', 'password': password, 'ca': str(config / 'server.crt')})
    write(root / 'facts.json', {'image': pinned, 'host_arch': platform.machine(), 'host_os': platform.system(),
          'docker_arch': run('docker', 'info', '-f', '{{.Architecture}}'),
          'image_id': run('docker', 'inspect', '-f', '{{.Image}}', name),
          'control_port': port, 'passive_start': port+1, 'passive_end': port+10,
          'tls_mode': 'explicit and plain admission', 'native_read_only': True, 'native_link_mlst': link_facts, 'native_root_mlst': root_facts, 'advertised_pasv_ip': '203.0.113.123'})
    print('Owned FTP fixture ready; credentials remain private')


def persistence():
    root = owner()
    state = json.loads((root / 'state.json').read_text())
    name = state['name']
    actual = run('docker', 'inspect', '-f', '{{index .Config.Labels "org.addp.ftp.owner"}}', name)
    if actual != name:
        raise RuntimeError('FTP lifecycle ownership mismatch')
    csv_path = '/srv/sftpgo/data/examples/客户 中文%.csv'
    missing = '/srv/sftpgo/data/examples/订单 示例.json'
    run('docker', 'exec', '-u', '1000', name, 'sh', '-ec',
        'printf "%s\\n" "custom-preserved" >> "$1"; rm "$2"', 'sh', csv_path, missing)
    run('docker', 'rm', '-f', name)
    run(*state['args'])
    for _ in range(60):
        if run('docker', 'inspect', '-f', '{{.State.Running}}', name) != 'true':
            raise RuntimeError('FTP lifecycle container exited')
        try:
            connection = json.loads((root / 'connection.json').read_text())
            context = ssl.create_default_context(cafile=connection['ca'])
            with ftplib.FTP_TLS(context=context, timeout=2) as client:
                client.connect(connection['host'], connection['port'])
                client.login(connection['user'], connection['password'])
                client.prot_p()
                client.nlst('/examples')
            break
        except OSError:
            time.sleep(1)
    else:
        raise RuntimeError('FTP lifecycle readiness timed out')
    if not run('docker', 'exec', name, 'cat', csv_path).endswith('custom-preserved'):
        raise RuntimeError('FTP sample overwritten on recreation')
    if run('docker', 'exec', name, 'cat', missing) != (root / 'seed/订单 示例.json').read_text().strip():
        raise RuntimeError('FTP missing sample was not restored')
    write(root / 'business-lifecycle.json', {'recreated': True, 'user_changes_preserved': True,
          'missing_sample_restored': True, 'credentials_and_volumes_preserved': True})


def stop():
    root = owner()
    state_path = root / 'state.json'
    if not state_path.exists():
        write(root / 'cleanup.json', {'residuals': 0})
        return
    state = json.loads(state_path.read_text())
    # Never remove a personal or foreign resource, including on partial startup.
    containers = run('docker', 'ps', '-aq', '--filter', 'label=' + state['label']).split()
    if containers:
        run('docker', 'rm', '-f', *containers)
    volumes = run('docker', 'volume', 'ls', '-q', '--filter', 'label=' + state['label']).split()
    if volumes:
        run('docker', 'volume', 'rm', *volumes)
    remaining = run('docker', 'ps', '-aq', '--filter', 'label=' + state['label']).split()
    remaining += run('docker', 'volume', 'ls', '-q', '--filter', 'label=' + state['label']).split()
    write(root / 'cleanup.json', {'residuals': len(remaining)})
    if remaining:
        raise RuntimeError('FTP owned resources remain')
    state_path.unlink()


if __name__ == '__main__':
    try:
        {'start': start, 'stop': stop, 'persistence': persistence}[sys.argv[1]]()
    except subprocess.CalledProcessError:
        # Bootstrap command output can contain user records; never print it.
        print('FTP fixture command failed; private owner directory retained', file=sys.stderr)
        sys.exit(1)
