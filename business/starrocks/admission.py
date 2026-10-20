#!/usr/bin/env python3
"""Own only the disposable official StarRocks protocol-admission resources."""
from __future__ import annotations

import json
import os
from pathlib import Path
import platform
import secrets
import subprocess
import tempfile
import time
import uuid
import sys
import re

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))
from business.starrocks.config import image
IMAGE = image()
LABEL = 'com.addp.starrocks-admission'


def run(*args, evidence=None, **kwargs):
    result = subprocess.run(args, capture_output=True, text=True, **kwargs)
    if evidence is not None:
        # Only fixed FROM/LABEL and credential-free schema DDL use this.
        private(evidence, {'stdout': result.stdout, 'stderr': result.stderr})
    if result.returncode:
        raise RuntimeError('StarRocks admission command failed: ' + ' '.join(args[:2]))
    return result.stdout.strip()


def directory():
    path = Path(os.environ['ADDP_STARROCKS_ADMISSION_DIR'])
    if not path.is_absolute() or not path.is_dir() or path.resolve() != path or path.is_relative_to(ROOT) or path.stat().st_mode & 0o077:
        raise RuntimeError('canonical private external admission directory required')
    return path


def private(path, value):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, 'w') as output:
        json.dump(value, output)


def inspect(kind, name):
    result = subprocess.run(['docker', kind, 'inspect', name], capture_output=True, text=True)
    if result.returncode:
        run('docker', 'info')
        if 'no such' not in result.stderr.lower() and 'not found' not in result.stderr.lower():
            raise RuntimeError('unknown Docker resource state')
        return None
    return json.loads(result.stdout)[0]


def owned(kind, name, state):
    value = inspect(kind, name)
    labels = (value.get('Config', {}).get('Labels', {}) if kind != 'volume' else value.get('Labels', {})) if value else {}
    if value and (labels or {}).get(LABEL) != state['run_id']:
        raise RuntimeError('refusing foreign StarRocks admission resource')
    return value


def sql(name, statement, *, evidence=None):
    return run('docker', 'exec', '-i', name, 'mysql', '--default-character-set=utf8mb4', '--batch', '--raw', '-h127.0.0.1', '-P9030', '-uroot', input=statement, timeout=30, evidence=evidence)


def backend_ready(rows):
    # A heartbeat can be Alive before the first disk report. StarRocks refuses
    # tablet creation while Backend.disksRef is empty; do not race that report.
    if len(rows) != 1 or rows[0].get('Alive', '').lower() != 'true':
        return False
    for key in ('TotalCapacity', 'AvailCapacity'):
        value = re.fullmatch(r'([0-9]+(?:\.[0-9]+)?)\s+(?:B|KB|MB|GB|TB|PB)', rows[0].get(key, ''))
        if value is None or float(value.group(1)) <= 0:
            return False
    return True


def start():
    path = directory()
    architecture = {'arm64': 'arm64', 'aarch64': 'arm64', 'x86_64': 'amd64', 'amd64': 'amd64'}.get(platform.machine().lower())
    daemon = run('docker', 'info', '--format', '{{.OSType}}/{{.Architecture}}')
    if architecture is None or daemon not in ('linux/' + architecture, 'linux/' + ('aarch64' if architecture == 'arm64' else 'x86_64')):
        raise RuntimeError('native matching Linux Docker architecture required')
    state = {'run_id': uuid.uuid4().hex}
    name = 'addp-starrocks-' + state['run_id']
    state.update(name=name, image=name + ':local', volumes=[name + '-fe', name + '-be'])
    private(path / 'state.json', state)
    for kind, resource in [('container', name), ('image', state['image']), *[('volume', v) for v in state['volumes']]]:
        if inspect(kind, resource) is not None:
            raise RuntimeError('refusing preexisting admission resource')
    with tempfile.TemporaryDirectory(prefix='addp-starrocks-admission-build-') as temporary:
        Path(temporary, 'Dockerfile').write_text(f'FROM {IMAGE}\nLABEL {LABEL}="{state["run_id"]}"\n')
        run('docker', 'build', '-t', state['image'], temporary, timeout=900, evidence=path / 'image-build.json')
    image = owned('image', state['image'], state)
    if not image or image.get('Os') != 'linux' or image.get('Architecture') != architecture:
        raise RuntimeError('official image must match the native daemon architecture')
    for volume in state['volumes']:
        run('docker', 'volume', 'create', '--label', LABEL + '=' + state['run_id'], volume)
    run('docker', 'run', '-d', '--name', name, '--label', LABEL + '=' + state['run_id'], '--memory=4g', '--shm-size=256m',
        '--mount', 'type=volume,source=' + state['volumes'][0] + ',target=/data/deploy/starrocks/fe/meta',
        '--mount', 'type=volume,source=' + state['volumes'][1] + ',target=/data/deploy/starrocks/be/storage',
        '-p', '127.0.0.1::9030', state['image'])
    deadline = time.monotonic() + 300
    while time.monotonic() < deadline:
        value = owned('container', name, state)
        if not value or not value['State']['Running']:
            raise RuntimeError('StarRocks official entrypoint exited')
        try:
            lines = sql(name, 'SHOW BACKENDS;').splitlines()
            rows = [dict(zip(lines[0].split('\t'), row.split('\t'))) for row in lines[1:]] if lines else []
            if backend_ready(rows):
                break
        except RuntimeError:
            pass
        time.sleep(2)
    else:
        raise RuntimeError('StarRocks FE/BE readiness deadline exceeded')
    private(path / 'backend-readiness.json', {'backends': rows})
    sql(name, """CREATE DATABASE addp_acceptance;
CREATE TABLE addp_acceptance.source(name VARCHAR(128),id BIGINT,amount DECIMAL(38,18),optional_text VARCHAR(128))
PRIMARY KEY(name,id) DISTRIBUTED BY HASH(name) BUCKETS 1 PROPERTIES('replication_num'='1');
INSERT INTO addp_acceptance.source VALUES ('正式接入中文',9007199254740993,12345678901234567890.123456789012345678,NULL),('保留尾空格  ',2,-0.000000000000000001,'');
CREATE TABLE addp_acceptance.write_probe(id BIGINT) DUPLICATE KEY(id) DISTRIBUTED BY HASH(id) BUCKETS 1 PROPERTIES('replication_num'='1');
CREATE VIEW addp_acceptance.visible_view AS SELECT name,id FROM addp_acceptance.source;
""", evidence=path / 'schema-initialization.json')
    password = 'Sr9@' + secrets.token_hex(12)
    sql(name, f"""CREATE USER fixture_reader IDENTIFIED BY '{password}';
GRANT SELECT ON TABLE addp_acceptance.source TO USER fixture_reader;
GRANT SELECT ON VIEW addp_acceptance.visible_view TO USER fixture_reader;
""")
    value = owned('container', name, state)
    mapping = value['NetworkSettings']['Ports']['9030/tcp']
    if len(mapping) != 1 or mapping[0]['HostIp'] != '127.0.0.1':
        raise RuntimeError('random loopback-only SQL binding required')
    private(path / 'connection.json', {'host': '127.0.0.1', 'port': int(mapping[0]['HostPort']), 'reader_password': password})
    private(path / 'facts.json', {'architecture': architecture, 'image': IMAGE, 'image_id': value['Image'], 'server_version': sql(name, 'SELECT current_version();')})
    print('STARROCKS_ADMISSION_READY architecture=' + architecture, flush=True)


def stop():
    path = directory()
    file = path / 'state.json'
    if not file.exists():
        return
    state = json.loads(file.read_text())
    resources = []
    if 'business_name' in state:
        resources += [('container', state['business_name'], ('rm', '-fv')), *[('volume', v, ('volume', 'rm')) for v in state['business_volumes']]]
    resources += [('container', state['name'], ('rm', '-fv')), *[('volume', v, ('volume', 'rm')) for v in state['volumes']], ('image', state['image'], ('image', 'rm'))]
    for kind, name, command in resources:
        if owned(kind, name, state) is not None:
            run('docker', *command, name, timeout=90)
        if inspect(kind, name) is not None:
            raise RuntimeError('StarRocks admission residual: ' + kind)
    private(path / 'cleanup.json', {'result': 'passed', 'remaining_resources': 0})
    (path / 'starrocks-engine.json').unlink(missing_ok=True)
    file.unlink()
    print('STARROCKS_ADMISSION_CLEANUP_PASS remaining_resources=0', flush=True)


def persistence():
    """Verify the actual Business entrypoint on separate, owned persistent volumes."""
    path = directory()
    state = json.loads((path / 'state.json').read_text())
    if 'business_name' in state or not owned('container', state['name'], state) or not owned('image', state['image'], state):
        raise RuntimeError('fresh owned StarRocks fixture required')
    state.update(business_name=state['name'] + '-business', business_volumes=[state['name'] + '-business-fe', state['name'] + '-business-be'])
    for kind, name in [('container', state['business_name']), *[('volume', v) for v in state['business_volumes']]]:
        if inspect(kind, name) is not None:
            raise RuntimeError('refusing preexisting Business lifecycle resource')
    # Keep the cleanup ownership record durable before creating any resource.
    private(path / 'state.next.json', state)
    (path / 'state.next.json').replace(path / 'state.json')
    run('docker', 'stop', state['name'], timeout=90)
    for volume in state['business_volumes']:
        run('docker', 'volume', 'create', '--label', LABEL + '=' + state['run_id'], volume)
    administrator, reader = "Sr9@'\\ " + secrets.token_hex(12), "Read8@'\\ " + secrets.token_hex(12)
    environment = dict(os.environ, STARROCKS_PASSWORD=administrator, STARROCKS_APP_PASSWORD=reader, STARROCKS_USER='business_reader')

    def launch():
        run('docker', 'run', '-d', '--name', state['business_name'], '--hostname', state['business_name'],
            '--label', LABEL + '=' + state['run_id'], '--memory=4g', '--shm-size=256m',
            '-p', '127.0.0.1::9030',
            '--env', 'STARROCKS_PASSWORD', '--env', 'STARROCKS_APP_PASSWORD', '--env', 'STARROCKS_USER',
            '--mount', 'type=volume,source=' + state['business_volumes'][0] + ',target=/data/deploy/starrocks/fe/meta',
            '--mount', 'type=volume,source=' + state['business_volumes'][1] + ',target=/data/deploy/starrocks/be/storage',
            '--mount', 'type=bind,source=' + str(ROOT / 'business/starrocks') + ',target=/addp/starrocks,readonly',
            '--health-cmd', 'bash /addp/starrocks/entrypoint.sh --health', '--health-interval=3s', '--health-timeout=10s',
            '--health-retries=90', '--entrypoint', 'bash', state['image'], '/addp/starrocks/entrypoint.sh', env=environment)
        deadline = time.monotonic() + 360
        while time.monotonic() < deadline:
            value = owned('container', state['business_name'], state)
            if not value or not value['State']['Running']:
                raise RuntimeError('Business StarRocks initialization exited')
            if value['State'].get('Health', {}).get('Status') == 'healthy':
                return
            time.sleep(2)
        raise RuntimeError('Business StarRocks lifecycle readiness timed out')

    def business_sql(statement, ordinary=False, denied=False):
        user, password = ('business_reader', reader) if ordinary else ('root', administrator)
        result = subprocess.run(['docker', 'exec', '-i', '-e', 'MYSQL_PWD', state['business_name'], 'mysql',
                                 '--default-character-set=utf8mb4', '--batch', '--raw', '-h127.0.0.1', '-P9030', '-u' + user],
                                input=statement, env=dict(os.environ, MYSQL_PWD=password), capture_output=True, text=True, timeout=30)
        if denied:
            if result.returncode == 0 or '5203 (42000)' not in result.stderr:
                raise RuntimeError('Business reader was not denied by native privileges')
        elif result.returncode:
            raise RuntimeError('Business lifecycle SQL failed')
        return result.stdout.strip()

    def backend_id():
        lines = business_sql('SHOW BACKENDS;').splitlines()
        rows = [dict(zip(lines[0].split('\t'), row.split('\t'))) for row in lines[1:]] if lines else []
        if len(rows) != 1 or rows[0].get('Alive') != 'true' or not rows[0].get('BackendId'):
            raise RuntimeError('Business native Backend identity unavailable')
        return rows[0]['BackendId']

    def demo_facts():
        counts = business_sql('SELECT (SELECT COUNT(*) FROM business.customers),'
                              '(SELECT COUNT(*) FROM business.products),(SELECT COUNT(*) FROM business.orders);', ordinary=True).splitlines()
        joined = business_sql('SELECT COUNT(*),SUM(o.order_amount),'
                              'SUM(CASE WHEN o.order_amount=o.quantity*o.unit_price THEN 1 ELSE 0 END) '
                              'FROM business.orders o JOIN business.customers c ON o.customer_id=c.customer_id '
                              'JOIN business.products p ON o.product_id=p.product_id;', ordinary=True).splitlines()
        typed = business_sql("SELECT (SELECT COUNT(*) FROM business.customers WHERE vip=TRUE),"
                             "(SELECT COUNT(*) FROM business.products WHERE active=TRUE),"
                             "(SELECT COUNT(*) FROM business.orders WHERE remark IS NULL),"
                             "(SELECT COUNT(*) FROM business.orders WHERE remark=''),"
                             "(SELECT MIN(joined_date) FROM business.customers),"
                             "(SELECT MAX(order_time) FROM business.orders);", ordinary=True).splitlines()
        if counts[-1:] != ['5\t6\t10'] or joined[-1:] != ['10\t39179.40\t10'] or typed[-1:] != ['3\t5\t1\t1\t2026-01-15\t2026-10-10 10:00:00']:
            raise RuntimeError('Business demonstration rows, references, amounts or native types differ')

    try:
        launch()
        original_id = backend_id()
        precise = business_sql('SELECT id,amount FROM business.source WHERE id=9007199254740993;', ordinary=True).splitlines()
        if precise[-1:] != ['9007199254740993\t12345678901234567890.123456789012345678']:
            raise RuntimeError('Business initialization lost bigint or Decimal precision')
        demo_facts()
        business_sql("INSERT INTO business.source VALUES ('ungranted',3,1,NULL);", ordinary=True, denied=True)
        business_sql("UPDATE business.source SET optional_text='persistent-owned-marker' WHERE id=2;")
        business_sql("UPDATE business.customers SET customer_name='示范客户修改保留' WHERE customer_id=1001;")
        business_sql('DELETE FROM business.products WHERE product_id=2006;')
        run('docker', 'rm', '-f', state['business_name'], timeout=90)
        launch()
        if backend_id() != original_id:
            raise RuntimeError('Business Backend identity changed across container recreation')
        marker = business_sql('SELECT optional_text FROM business.source WHERE id=2;', ordinary=True).splitlines()
        count = business_sql('SELECT COUNT(*) FROM business.source;', ordinary=True).splitlines()
        if marker[-1:] != ['persistent-owned-marker'] or count[-1:] != ['2']:
            raise RuntimeError('Business restart overwrote source data or duplicated examples')
        demo_facts()
        customer = business_sql('SELECT customer_name FROM business.customers WHERE customer_id=1001;', ordinary=True).splitlines()
        if customer[-1:] != ['示范客户修改保留']:
            raise RuntimeError('Business restart overwrote a demonstration customer change')
        # Restore only this synthetic fixture's modified sample for T4 readers.
        business_sql("UPDATE business.source SET optional_text='' WHERE id=2;")
        business_sql("UPDATE business.customers SET customer_name='湘江环境监测中心' WHERE customer_id=1001;")
        value = owned('container', state['business_name'], state)
        mapping = value['NetworkSettings']['Ports']['9030/tcp']
        if len(mapping) != 1 or mapping[0]['HostIp'] != '127.0.0.1':
            raise RuntimeError('Business fixture requires a random loopback-only port')
        private(path / 'starrocks-engine.json', {'name': 'Hosted StarRocks read-only', 'engine_type': 'starrocks',
                'engine_origin': 'general', 'description': 'Disposable SELECT-only Business consumer',
                'connection_info': {'host': '127.0.0.1', 'port': int(mapping[0]['HostPort']), 'database': 'business',
                                    'user': 'business_reader', 'password': reader}})
        private(path / 'business-lifecycle.json', {'result': 'passed', 'backend_id_preserved': True,
                'user_change_preserved': True, 'example_rows': 2, 'reader_write_denied': True,
                'precise_bigint_decimal': True,
                'demo_rows': {'customers': 5, 'products': 6, 'orders': 10},
                'demo_join_and_amounts': True, 'demo_types': True,
                'demo_user_change_preserved': True, 'missing_demo_row_restored': True,
                'initialization': 'Business official-entrypoint wrapper', 'platform_t4_verified': False})
        print('STARROCKS_BUSINESS_PERSISTENCE_PASS', flush=True)
    except (RuntimeError, OSError, ValueError, KeyError, IndexError):
        value = owned('container', state['business_name'], state)
        output = ''
        if value:
            result = subprocess.run(['docker', 'logs', state['business_name']], capture_output=True, text=True)
            output = result.stdout + result.stderr
        private(path / 'business-launch.json', business_failure_evidence(output, value.get('State', {}) if value else {}))
        raise


def business_failure_evidence(log, state):
    # Never archive arbitrary server/SQL diagnostics: they can quote only part
    # of a password, which whole-secret string replacement would not sanitize.
    phases = {'fe-ready', 'admin-ready', 'backend-ready', 'schema-ready', 'reader-ready'}
    return {'result': 'failed', 'running': bool(state.get('Running')),
            'exit_code': state.get('ExitCode'), 'oom_killed': bool(state.get('OOMKilled')),
            'native_error_codes': sorted(set(re.findall(r'ERROR (\d{4}) \([A-Z0-9]{5}\)', log))),
            'phases': [line.split()[-1] for line in log.splitlines()
                       if line.startswith('ADDP_STARROCKS_PHASE ') and line.split()[-1] in phases]}


if __name__ == '__main__':
    import sys
    try:
        {'start': start, 'stop': stop, 'persistence': persistence}[sys.argv[1]]()
    except (RuntimeError, OSError, ValueError, KeyError, IndexError) as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
