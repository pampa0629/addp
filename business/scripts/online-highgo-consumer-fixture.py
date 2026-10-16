#!/usr/bin/env python3
"""Own only disposable Hosted HighGo/PostgreSQL sources; never register Engines."""
from __future__ import annotations

import json
import os
from pathlib import Path
import platform
import re
import secrets
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
LABEL = 'highgo-consumer-flow'
HIGHGO = 'addp-highgo-online-disposable'
POSTGRES = 'addp-highgo-pg-online-disposable'
TARGET = 'orders_replace_online'


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


def command(*args, input=None, env=None, allow_failure=False, timeout=180):
    result = subprocess.run(args, input=input, env=env, capture_output=True, text=True, timeout=timeout)
    # Docker/SQL diagnostics may include credentials: never forward raw output.
    require(allow_failure or result.returncode == 0, f'HighGo fixture command failed: {args[0]} {args[1]}')
    return result


def guard():
    require(all(os.environ.get(k) == v for k, v in {
        'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'ADDP_ONLINE_HOST': '1',
        'ADDP_ONLINE_HOSTED': '1', 'ONLINE_SUITE': LABEL, 'POSTGRES_DB': 'addp_online'}.items()), 'dedicated GitHub Hosted Online is required')
    require(platform.system() == 'Linux' and platform.machine() == 'x86_64', 'Linux x86_64 required')
    require(os.environ.get('ADDP_ONLINE_OWNER_MANAGED') != '1', 'conflicting Online profile')


def secret_directory():
    path = Path(os.environ['ADDP_ONLINE_SECRET_DIR'])
    require(path.is_absolute() and path.is_dir() and path.resolve() == path,
            'existing canonical external secret directory required')
    require(not path.is_relative_to(ROOT) and path.stat().st_mode & 0o077 == 0, 'private external secrets required')
    return path


def owned(name):
    result = command('docker', 'container', 'inspect', name, allow_failure=True)
    if result.returncode:
        # Do not mistake an unavailable Docker daemon for an absent container.
        command('docker', 'info')
        return False
    data = json.loads(result.stdout)[0]
    require((data['Config'].get('Labels') or {}).get('com.addp.online-fixture') == LABEL, 'container ownership mismatch')
    return True


def write_private(path, content):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as output:
        output.write(content)


def password():
    return 'Aa1!' + secrets.token_hex(18)


def highgo_image():
    # Consume the Business image pin, rather than inventing a second media route.
    compose = (ROOT / 'business/docker-compose.yml').read_text()
    match = re.search(r'(?m)^  highgo:\n    image: (qiuchenjun/hgdb-see:4\.5\.10\.3@sha256:[0-9a-f]{64})$', compose)
    require(match is not None, 'Business HighGo immutable image pin missing')
    return match[1]


def sql(name, statement, secrets_path, *, admin=False):
    values = json.loads((secrets_path / 'highgo-fixture.json').read_text())
    if name == HIGHGO:
        pw = values['admin' if admin else 'business']
        # stdin carries both the password and SQL; no secret in argv or output.
        args = ['bash', '-c', 'IFS= read -r PGPASSWORD; export PGPASSWORD PGSSLMODE=require; exec psql -X -h 127.0.0.1 -p 5866 -U "$1" -d highgo -v ON_ERROR_STOP=1 -At', '_', 'sysdba' if admin else 'business']
        payload = pw + '\n' + statement
    else:
        args = ['psql', '-X', '-U', 'postgres', '-d', 'highgo_source', '-v', 'ON_ERROR_STOP=1', '-At']
        payload = statement
    return command('docker', 'exec', '-i', name, *args, input=payload, timeout=20).stdout.strip()


def descriptor(directory, name, engine_type, container, port, database, user, pw, sslmode):
    mapping = command('docker', 'port', container, str(port) + '/tcp').stdout.strip()
    require(re.fullmatch(r'127\.0\.0\.1:[0-9]+', mapping), 'source must bind only a random loopback port')
    write_private(directory / name, json.dumps({
        'name': 'Hosted HighGo' if engine_type == 'highgo' else 'Hosted HighGo PostgreSQL source',
        'engine_type': engine_type, 'engine_origin': 'general', 'description': 'Disposable HighGo acceptance',
        'connection_info': {'host': '127.0.0.1', 'port': int(mapping.rsplit(':', 1)[1]),
                            'database': database, 'user': user, 'password': pw, 'sslmode': sslmode}}))


def start():
    directory = secret_directory()
    require(not owned(HIGHGO) and not owned(POSTGRES), 'refusing existing fixture containers')
    values = {key: password() for key in ('admin', 'business', 'postgres')}
    write_private(directory / 'highgo-fixture.json', json.dumps(values))
    envfile = directory / 'highgo-docker.env'
    write_private(envfile, '\n'.join([
        'POSTGRES_PASSWORD=' + values['admin'], 'HIGHGO_APP_PASSWORD=' + values['business'],
        'HIGHGO_USER=business', 'POSTGRES_USER=sysdba', 'POSTGRES_DB=highgo', 'PGPORT=5866', 'TZ=UTC']) + '\n')
    command('docker', 'run', '-d', '--name', HIGHGO, '--platform', 'linux/amd64',
            '--label', 'com.addp.online-fixture=' + LABEL, '--env-file', str(envfile),
            '-p', '127.0.0.1::5866', '--mount', f'type=bind,source={ROOT}/business/highgo,target=/addp/highgo,readonly',
            '--mount', f'type=bind,source={ROOT}/business/highgo/init,target=/docker-entrypoint-initdb.d,readonly',
            '--entrypoint', 'bash', highgo_image(), '/addp/highgo/entrypoint.sh',
            'postgres', '-p', '5866', '-c', 'listen_addresses=*', '-c', 'ssl=on', '-c', 'password_encryption=sm3', timeout=600)
    env = dict(os.environ, POSTGRES_PASSWORD=values['postgres'])
    command('docker', 'run', '-d', '--name', POSTGRES, '--label', 'com.addp.online-fixture=' + LABEL,
            '-e', 'POSTGRES_PASSWORD', '-e', 'POSTGRES_DB=highgo_source', '-p', '127.0.0.1::5432',
            'addp-postgres-pgvector:latest', env=env)
    deadline = time.monotonic() + 180
    while True:
        try:
            require('HighGo V4.5 Business' in sql(HIGHGO, 'SELECT engine_name FROM addp_business.addp_engine_probe WHERE id=1;', directory), 'HighGo not ready')
            sql(POSTGRES, 'SELECT 1;', directory)
            break
        except RuntimeError:
            require(time.monotonic() < deadline, 'database startup deadline exceeded')
            time.sleep(2)
    version = sql(HIGHGO, 'SELECT version();', directory)
    require('HighGo Database Management System 4.5' in version, 'native HighGo V4.5 required')
    login = json.loads(sql(HIGHGO, "SELECT json_build_object('user',current_user,'ssl',s.ssl,'admin',r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls) FROM pg_stat_ssl s JOIN pg_roles r ON r.rolname=current_user WHERE s.pid=pg_backend_pid();", directory))
    require(login == {'user': 'business', 'ssl': True, 'admin': False}, 'ordinary TLS business login required')
    sql(POSTGRES, 'CREATE SCHEMA addp_business;\n' + (ROOT / 'business/highgo/init/init.sql').read_text(), directory)
    # The source uses a separate SELECT-only role; bind its password through stdin.
    pw = values['postgres']
    sql(POSTGRES, "\\set reader_password '" + pw + "'\nCREATE USER source_reader PASSWORD :'reader_password';\nGRANT CONNECT ON DATABASE highgo_source TO source_reader;\nGRANT USAGE ON SCHEMA addp_business TO source_reader;\nGRANT SELECT ON ALL TABLES IN SCHEMA addp_business TO source_reader;", directory)
    sql(HIGHGO, f"CREATE TABLE addp_business.{TARGET} (LIKE addp_business.orders INCLUDING ALL);\nINSERT INTO addp_business.{TARGET} VALUES (999,'SENTINEL',1,'old',999.99,TIMESTAMP '2020-01-01',TIMESTAMP '2020-01-01');", directory)
    require(sql(HIGHGO, f"SELECT count(*) FROM addp_business.{TARGET} WHERE order_no='SENTINEL';", directory) == '1', 'replace sentinel initialization failed')
    descriptor(directory, 'highgo-engine.json', 'highgo', HIGHGO, 5866, 'highgo', 'business', values['business'], 'require')
    descriptor(directory, 'highgo-pg-engine.json', 'postgresql', POSTGRES, 5432, 'highgo_source', 'source_reader', pw, 'disable')
    print('Disposable HighGo V4.5 and PostgreSQL sources ready')


def verify():
    directory = secret_directory()
    require(owned(HIGHGO) and owned(POSTGRES), 'owned sources required')
    fields = 'id,order_no,customer_id,order_status,total_amount,ordered_at,updated_at'
    source = sql(POSTGRES, f'SELECT {fields} FROM addp_business.orders ORDER BY id;', directory)
    target = sql(HIGHGO, f'SELECT {fields} FROM addp_business.{TARGET} ORDER BY id;', directory)
    require(source == target and len(source.splitlines()) == 3, 'native seven-field replace comparison failed')
    for table, count in [('customers', 3), ('products', 3), ('orders', 3), ('order_items', 4)]:
        require(sql(HIGHGO, f'SELECT count(*) FROM addp_business.{table};', directory) == str(count), 'Business sample row count changed')
    require(sql(HIGHGO, f'SELECT sum(total_amount) FROM addp_business.{TARGET};', directory) == '6795.50', 'native amount mismatch')
    print('HighGo native replace comparison and Business sample preservation passed')


def stop():
    errors = []
    for name in (HIGHGO, POSTGRES):
        try:
            if owned(name):
                state = json.loads(command('docker', 'container', 'inspect', name).stdout)[0]
                volumes = [mount['Name'] for mount in state.get('Mounts', []) if mount.get('Type') == 'volume']
                command('docker', 'rm', '-fv', name)
                for volume in volumes:
                    gone = command('docker', 'volume', 'inspect', volume, allow_failure=True)
                    require(gone.returncode != 0, 'fixture volume remains after cleanup')
                    command('docker', 'info')
            require(not owned(name), 'fixture container remains after cleanup')
        except (RuntimeError, subprocess.SubprocessError):
            errors.append(name)
    require(not errors, 'fixture cleanup failed: ' + ', '.join(errors))
    print('HighGo fixture cleanup: zero residual containers and anonymous volumes')


def main():
    guard()
    require(len(sys.argv) == 2 and sys.argv[1] in ('start', 'verify', 'stop'), 'use start|verify|stop')
    {'start': start, 'verify': verify, 'stop': stop}[sys.argv[1]]()


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        # Avoid a traceback with secret command input/arguments.
        print('HighGo fixture failed: ' + (str(error) if isinstance(error, RuntimeError) else type(error).__name__), file=sys.stderr)
        sys.exit(1)
