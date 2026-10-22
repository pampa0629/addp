"""Disposable FTP owner: private credentials, matching ports and labelled cleanup."""
import json
import hashlib
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
from seed import write_model_samples, write_samples


def run(*args, **kwargs):
    return subprocess.run(args, check=True, text=True, capture_output=True, **kwargs).stdout.strip()


def write(path, value, mode=0o600):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2))
    path.chmod(mode)


def command_failure(exc, action):
    root = owner()
    command = exc.cmd[:2] if isinstance(exc.cmd, (list, tuple)) else ['unknown']
    write(root / 'command-failure.json', {'action': action, 'command': command, 'exit_code': exc.returncode})
    private = root / 'command-failure.stderr'
    private.write_text(exc.stderr or '')
    private.chmod(0o600)


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


def wait_for_greeting(root, state, connection, phase, timeout=90):
    """Wait for the FTP server, not merely Docker's running process.

    Only connect/banner failures are startup conditions. Authentication, TLS and
    data-channel checks run once after this boundary and must fail normally.
    """
    started = time.monotonic()
    report = {'ready': False, 'attempts': []}
    try:
        while (now := time.monotonic()) < started + timeout:
            container = json.loads(run('docker', 'inspect', '-f', '{{json .State}}', state['name']))
            report['container'] = {key: container[key] for key in ('Running', 'Status', 'ExitCode')}
            if not container['Running']:
                raise RuntimeError('FTP fixture exited before its server greeting')
            client = ftplib.FTP(timeout=min(2, started + timeout - now))
            attempt = {'elapsed_seconds': round(now - started, 3)}
            report['attempts'].append(attempt)
            try:
                welcome = client.connect(connection['host'], connection['port'])
                if not welcome.startswith(('220 ', '220-')):
                    raise RuntimeError('FTP fixture returned an invalid ready greeting')
                attempt['outcome'] = '220'
                report['ready'] = True
                return
            except (ConnectionRefusedError, ConnectionResetError, TimeoutError, EOFError) as exc:
                attempt['outcome'] = type(exc).__name__
            except Exception as exc:
                attempt['outcome'] = type(exc).__name__
                raise
            finally:
                client.close()
            time.sleep(min(1, max(0, started + timeout - time.monotonic())))
        raise RuntimeError('FTP server greeting readiness timed out')
    finally:
        # Keep raw server logs private; artifacts include only fixed sender/level
        # counts and startup outcomes, never configuration, usernames or replies.
        try:
            logs = run('docker', 'logs', '--tail', '200', state['name'])
            private_log = root / ('server-' + phase + '.log')
            private_log.write_text(logs)
            private_log.chmod(0o600)
            counts = {key: 0 for key in ('service', 'dataprovider', 'ftpd', 'fatal')}
            for line in logs.splitlines():
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if not isinstance(event, dict):
                    continue
                sender = event.get('sender')
                if sender in ('service', 'dataprovider', 'ftpd'):
                    counts[sender] += 1
                if event.get('level') == 'fatal':
                    counts['fatal'] += 1
            report['server_log_counts'] = counts
        except subprocess.CalledProcessError:
            report['diagnostics_unavailable'] = True
        write(root / ('readiness-' + phase + '.json'), report)


def start():
    root = owner()
    if (root / 'state.json').exists():
        raise ValueError('fixture already owns resources')
    name = 'addp-ftp-' + secrets.token_hex(6)
    state = {'name': name, 'label': 'org.addp.ftp.owner=' + name,
             'volumes': [name + '-state']}
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
    data = root / 'data'
    data.mkdir(mode=0o755)
    data.chmod(0o755)
    pinned = image()
    run('docker', 'pull', pinned)
    for volume in state['volumes']:
        run('docker', 'volume', 'create', '--label', state['label'], volume)
    args = ['docker', 'run', '-d', '--name', name, '--label', state['label'],
            '--memory', '512m', '-p', f'127.0.0.1:{port}:2121',
            '-p', f'127.0.0.1:{port+1}-{port+10}:{port+1}-{port+10}',
            '-v', f'{config}:/addp/config:ro',
            '-v', f'{ROOT / "business/ftp/entrypoint.sh"}:/addp/ftp/entrypoint.sh:ro',
            '-v', f'{data}:/srv/sftpgo/data:ro',
            '--entrypoint', 'sh',
            '-v', f'{state["volumes"][0]}:/var/lib/sftpgo', pinned, '/addp/ftp/entrypoint.sh']
    state['args'] = args
    write(root / 'state.json', state)
    run(*args)
    connection = {'host': '127.0.0.1', 'port': port, 'user': 'fixture_reader',
                  'password': password, 'ca': str(config / 'server.crt')}
    write(root / 'connection.json', connection)
    wait_for_greeting(root, state, connection, 'initial')
    # Publish a new host directory only after the FTP server is actually ready.
    # The subsequent native FTP/Provider/Meta checks must see it without restart.
    write_samples(data)
    write(root / 'ftp-model-samples.json', write_model_samples(data))
    padded = data / 'models' / ' whitespace '
    padded.mkdir(mode=0o755)
    for filename in ('TEST.PNG', ' TEST.PNG ', ' ', '资料%2F.png　'):
        target = padded / filename
        target.write_bytes(filename.encode('utf-8'))
        target.chmod(0o644)
    samples = data / 'examples'
    (samples / '中文 空格%样例.csv').write_text('id,name,amount\n1,长沙,100.25\n2,株洲,200.50\n')
    (samples / 'demo.json').write_text('[{"id":1,"name":"长沙"},{"id":2,"name":"株洲"}]')
    (samples / 'range.bin').write_bytes(bytes(range(256)) * 32768)
    (samples / '.hidden.txt').write_text('hidden sample')
    for sample in samples.iterdir():
        sample.chmod(0o644)
    unsupported = data / 'unsupported'
    unsupported.mkdir(mode=0o755)
    unsupported.chmod(0o755)
    (unsupported / 'link').symlink_to('../examples/demo.json')
    (root / 'outside-root.txt').write_text('not part of the FTP account root')
    write(root / 'ftp-engine.json', {'name': 'Hosted FTP', 'engine_type': 'ftp', 'engine_origin': 'general',
          'connection_info': {'host': connection['host'], 'port': connection['port'],
          'user': connection['user'], 'password': connection['password'],
          'security': 'explicit_tls', 'tls_ca_cert': (config / 'server.crt').read_text()},
          'description': 'Disposable FTP file consumer acceptance'})
    write(root / 'ftp-samples.json', [
        {'format': kind, 'full_name': 'examples/' + filename,
         'size': (samples / filename).stat().st_size,
         'sha256': hashlib.sha256((samples / filename).read_bytes()).hexdigest()}
        for kind, filename in [('csv', '客户 中文%.csv'), ('json', '订单 示例.json'), ('parquet', '客户 示例.parquet')]])
    tls = ssl.create_default_context(cafile=str(config / 'server.crt'))
    with ftplib.FTP_TLS(context=tls, timeout=5) as probe:
        probe.connect('127.0.0.1', port)
        probe.login('fixture_reader', password)
        probe.prot_p()
        link_facts = probe.sendcmd("MLST /unsupported/link")
        root_facts = probe.sendcmd("MLST /")
        if any(Path(item).name == 'outside-root.txt' for item in probe.nlst('/')):
            raise RuntimeError('FTP unexpectedly exposed a sibling of the host data root')
        for expected, operation in ((553, lambda: probe.storbinary('STOR /forbidden.txt', io.BytesIO(b'no'))),
                                    (550, lambda: probe.delete('/examples/demo.json'))):
            try:
                operation()
            except ftplib.error_perm as exc:
                if not str(exc).startswith(str(expected)):
                    raise RuntimeError('FTP native permission denial has wrong status')
            else:
                raise RuntimeError('FTP reader unexpectedly has mutation permissions')
    write(root / 'facts.json', {'image': pinned, 'host_arch': platform.machine(), 'host_os': platform.system(),
          'docker_arch': run('docker', 'info', '-f', '{{.Architecture}}'),
          'image_id': run('docker', 'inspect', '-f', '{{.Image}}', name),
          'control_port': port, 'passive_start': port+1, 'passive_end': port+10,
          'tls_mode': 'explicit and plain admission', 'native_read_only': True,
          'host_directory_added_after_ready': True, 'host_root_sibling_not_exposed': True,
          'native_link_mlst': link_facts, 'native_root_mlst': root_facts, 'advertised_pasv_ip': '203.0.113.123'})
    print('Owned FTP fixture ready; credentials remain private')


def persistence():
    root = owner()
    state = json.loads((root / 'state.json').read_text())
    name = state['name']
    actual = run('docker', 'inspect', '-f', '{{index .Config.Labels "org.addp.ftp.owner"}}', name)
    if actual != name:
        raise RuntimeError('FTP lifecycle ownership mismatch')
    data = root / 'data'
    csv_path = data / 'examples/客户 中文%.csv'
    missing = data / 'examples/订单 示例.json'
    original_json = missing.read_bytes()
    with csv_path.open('a') as stream:
        stream.write('custom-preserved\n')
    missing.unlink()
    added = data / '新增 目录%/嵌套'
    added.mkdir(parents=True)
    for directory in (added, added.parent):
        directory.chmod(0o755)
    added_file = added / 'host-added.csv'
    added_file.write_bytes(b'id,name\n11,host-added\n')
    added_file.chmod(0o644)
    connection = json.loads((root / 'connection.json').read_text())
    context = ssl.create_default_context(cafile=connection['ca'])
    with ftplib.FTP_TLS(context=context, timeout=2) as client:
        client.connect(connection['host'], connection['port'])
        client.login(connection['user'], connection['password'])
        client.prot_p()
        contents = io.BytesIO()
        client.retrbinary('RETR /新增 目录%/嵌套/host-added.csv', contents.write)
        if contents.getvalue() != added_file.read_bytes():
            raise RuntimeError('FTP did not expose the live host directory')
    run('docker', 'rm', '-f', name)
    # The same host initialization used by standard startup restores only missing
    # examples. The container never copies or writes files in the read-only root.
    write_samples(data)
    run(*state['args'])
    wait_for_greeting(root, state, connection, 'recreated', timeout=60)
    context = ssl.create_default_context(cafile=connection['ca'])
    with ftplib.FTP_TLS(context=context, timeout=2) as client:
        client.connect(connection['host'], connection['port'])
        client.login(connection['user'], connection['password'])
        client.prot_p()
        client.nlst('/examples')
        contents = io.BytesIO()
        client.retrbinary('RETR /examples/客户 中文%.csv', contents.write)
        if not contents.getvalue().endswith(b'custom-preserved\n'):
            raise RuntimeError('FTP sample overwritten on recreation')
        contents = io.BytesIO()
        client.retrbinary('RETR /examples/订单 示例.json', contents.write)
        if contents.getvalue() != original_json:
            raise RuntimeError('FTP missing sample was not restored')
        contents = io.BytesIO()
        client.retrbinary('RETR /新增 目录%/嵌套/host-added.csv', contents.write)
        if contents.getvalue() != added_file.read_bytes():
            raise RuntimeError('FTP user directory lost on recreation')
    if not csv_path.read_text().endswith('custom-preserved\n'):
        raise RuntimeError('FTP sample overwritten on recreation')
    if missing.read_bytes() != original_json:
        raise RuntimeError('FTP missing sample was not restored')
    write(root / 'business-lifecycle.json', {'recreated': True, 'user_changes_preserved': True,
          'missing_sample_restored': True, 'credentials_and_state_preserved': True,
          'live_host_directory_visible': True, 'host_directory_preserved': True})


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
    except subprocess.CalledProcessError as exc:
        # Bootstrap command output can contain user records; never publish it.
        command_failure(exc, sys.argv[1])
        print('FTP fixture command failed; private owner directory retained', file=sys.stderr)
        sys.exit(1)
