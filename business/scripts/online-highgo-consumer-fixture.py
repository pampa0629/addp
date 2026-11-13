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
    require(os.environ.get('ADDP_ONLINE_HIGHGO_CASE', 'business') in ('business', 'sql-timeout'), 'unknown HighGo case')


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
    # HighGo rejects its reserved "db" keyword even inside random passwords.
    # This one-to-one alphabet substitution retains all 144 random bits.
    return 'Aa1!' + secrets.token_hex(18).translate(str.maketrans({'b': 'x', 'd': 'y'}))


def highgo_image():
    # Consume the Business image pin, rather than inventing a second media route.
    compose = (ROOT / 'business/docker-compose.yml').read_text()
    match = re.search(r'(?m)^  highgo:\n    image: (qiuchenjun/hgdb-see:4\.5\.10\.3@sha256:[0-9a-f]{64})$', compose)
    require(match is not None, 'Business HighGo immutable image pin missing')
    return match[1]


def startup_diagnostics(values):
    for name in (HIGHGO, POSTGRES):
        evidence = {'container': name}
        try:
            if owned(name):
                data = json.loads(command('docker', 'container', 'inspect', name).stdout)[0]
                state = {key: data['State'].get(key) for key in ('Status', 'ExitCode', 'OOMKilled', 'Error')}
                logs = command('docker', 'logs', '--tail', '80', name, allow_failure=True)
                text = logs.stdout + '\n' + logs.stderr
                for secret in values.values():
                    text = text.replace(secret, '[REDACTED]')
                    state['Error'] = (state['Error'] or '').replace(secret, '[REDACTED]')
                evidence.update(state=state, log_exit_code=logs.returncode, log_tail=text[-8192:])
            else:
                evidence['state'] = {'Status': 'absent'}
        except (RuntimeError, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
            evidence['diagnostic_error'] = type(error).__name__
        print(json.dumps(evidence), file=sys.stderr)


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
    timeout_case = os.environ.get('ADDP_ONLINE_HIGHGO_CASE') == 'sql-timeout'
    if not timeout_case:
        env = dict(os.environ, POSTGRES_PASSWORD=values['postgres'])
        command('docker', 'run', '-d', '--name', POSTGRES, '--label', 'com.addp.online-fixture=' + LABEL,
                '-e', 'POSTGRES_PASSWORD', '-e', 'POSTGRES_DB=highgo_source', '-p', '127.0.0.1::5432',
                'addp-postgres-pgvector:latest', env=env)
    deadline = time.monotonic() + 180
    while True:
        probing = HIGHGO
        try:
            require('HighGo V4.5 Business' in sql(HIGHGO, 'SELECT engine_name FROM addp_business.addp_engine_probe WHERE id=1;', directory), 'HighGo not ready')
            if not timeout_case:
                probing = POSTGRES
                sql(POSTGRES, 'SELECT 1;', directory)
            break
        except RuntimeError:
            if time.monotonic() >= deadline:
                startup_diagnostics(values)
                raise RuntimeError('database startup deadline exceeded: ' + probing) from None
            time.sleep(2)
    version = sql(HIGHGO, 'SELECT version();', directory)
    require('HighGo Database Management System 4.5' in version, 'native HighGo V4.5 required')
    login = json.loads(sql(HIGHGO, "SELECT json_build_object('user',current_user,'ssl',s.ssl,'admin',r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls) FROM pg_stat_ssl s JOIN pg_roles r ON r.rolname=current_user WHERE s.pid=pg_backend_pid();", directory))
    require(login == {'user': 'business', 'ssl': True, 'admin': False}, 'ordinary TLS business login required')
    descriptor(directory, 'highgo-engine.json', 'highgo', HIGHGO, 5866, 'highgo', 'business', values['business'], 'require')
    if timeout_case:
        print('Disposable HighGo V4.5 SQL timeout probe ready')
        return
    sql(POSTGRES, 'CREATE SCHEMA addp_business;\n' + (ROOT / 'business/highgo/init/init.sql').read_text(), directory)
    # The source uses a separate SELECT-only role; bind its password through stdin.
    pw = values['postgres']
    sql(POSTGRES, "\\set reader_password '" + pw + "'\nCREATE USER source_reader PASSWORD :'reader_password';\nGRANT CONNECT ON DATABASE highgo_source TO source_reader;\nGRANT USAGE ON SCHEMA addp_business TO source_reader;\nGRANT SELECT ON ALL TABLES IN SCHEMA addp_business TO source_reader;", directory)
    sql(HIGHGO, f"CREATE TABLE addp_business.{TARGET} (LIKE addp_business.orders INCLUDING ALL);\nINSERT INTO addp_business.{TARGET} VALUES (999,'SENTINEL',1,'old',999.99,TIMESTAMP '2020-01-01',TIMESTAMP '2020-01-01');", directory)
    require(sql(HIGHGO, f"SELECT count(*) FROM addp_business.{TARGET} WHERE order_no='SENTINEL';", directory) == '1', 'replace sentinel initialization failed')
    descriptor(directory, 'highgo-pg-engine.json', 'postgresql', POSTGRES, 5432, 'highgo_source', 'source_reader', pw, 'disable')
    print('Disposable HighGo V4.5 and PostgreSQL sources ready')


def verify():
    directory = secret_directory()
    if os.environ.get('ADDP_ONLINE_HIGHGO_CASE') == 'sql-timeout':
        require(owned(HIGHGO) and not owned(POSTGRES), 'timeout case owns only HighGo')
        probe = sql(HIGHGO, 'SELECT id,engine_name FROM addp_business.addp_engine_probe ORDER BY id;', directory)
        require(probe == '1|HighGo V4.5 Business', 'HighGo timeout probe changed')
        remaining = sql(HIGHGO, "SELECT count(*) FROM pg_stat_activity WHERE datname='highgo' "
                        "AND application_name LIKE 'hg_timeout_%';", directory, admin=True)
        require(remaining == '0', 'HighGo timeout control sessions remain')
        print('HighGo native timeout probe preserved; zero control sessions')
        return
    require(owned(HIGHGO) and owned(POSTGRES), 'owned sources required')
    fields = 'id,order_no,customer_id,order_status,total_amount,ordered_at,updated_at'
    source = sql(POSTGRES, f'SELECT {fields} FROM addp_business.orders ORDER BY id;', directory)
    target = sql(HIGHGO, f'SELECT {fields} FROM addp_business.{TARGET} ORDER BY id;', directory)
    require(source == target and len(source.splitlines()) == 3, 'native seven-field replace comparison failed')
    for table, count in [('customers', 3), ('products', 3), ('orders', 3), ('order_items', 4)]:
        require(sql(HIGHGO, f'SELECT count(*) FROM addp_business.{table};', directory) == str(count), 'Business sample row count changed')
    require(sql(HIGHGO, f'SELECT sum(total_amount) FROM addp_business.{TARGET};', directory) == '6795.50', 'native amount mismatch')
    print('HighGo native replace comparison and Business sample preservation passed')


def timeout_barrier(operation, phase):
    """Hold only this disposable probe; archive native PIDs, never SQL or secrets."""
    require(operation in ('acquire', 'observe', 'release') and phase in ('manager', 'develop'),
            'use barrier acquire|observe|release manager|develop')
    directory = secret_directory()
    require(owned(HIGHGO), 'owned HighGo source required')
    run_id = os.environ.get('ADDP_ONLINE_TEST_RUN_ID', '')
    require(re.fullmatch(r'[A-Za-z0-9_-]{1,64}', run_id) is not None, 'safe run identity required')
    marker = f'hg_timeout_{phase}_{run_id}'[:63]
    journal = directory / f'highgo-{phase}-barrier.json'
    holder = ("(SELECT pid FROM pg_stat_activity WHERE datname='highgo' AND usename='sysdba' "
              f"AND application_name='{marker}' AND state='active' AND wait_event='PgSleep')")
    statement = f"""SELECT json_build_object('holder_id',{holder},'waiters',COALESCE((
        SELECT json_agg(json_build_object('id',pid,'state',state,'wait_event_type',wait_event_type,
            'operation',CASE WHEN query LIKE '%pg_total_relation_size(%' THEN 'table_catalog' ELSE 'table_read' END) ORDER BY pid)
        FROM pg_stat_activity a WHERE datname='highgo' AND usename='business' AND state='active'
          AND wait_event_type='Lock' AND {holder}=ANY(pg_blocking_pids(pid))
          AND EXISTS (SELECT 1 FROM pg_locks l WHERE l.pid=a.pid AND l.locktype='relation'
            AND NOT l.granted AND l.relation='addp_business.addp_engine_probe'::regclass)
          AND (query LIKE '%pg_total_relation_size(%' OR query ~* 'FROM[[:space:]]+("?addp_business"?[.])?"?addp_engine_probe"?([[:space:];]|$)')
        ),'[]'::json));"""

    def snapshot():
        result = json.loads(sql(HIGHGO, statement, directory, admin=True))
        require(isinstance(result, dict) and set(result) == {'holder_id', 'waiters'}
                and (result['holder_id'] is None or type(result['holder_id']) is int and result['holder_id'] > 0)
                and isinstance(result['waiters'], list), 'invalid native barrier snapshot')
        ids = set()
        for row in result['waiters']:
            require(isinstance(row, dict) and set(row) == {'id', 'state', 'wait_event_type', 'operation'}
                    and type(row['id']) is int and row['id'] > 0 and row['id'] not in ids
                    and row['state'] == 'active' and row['wait_event_type'] == 'Lock'
                    and row['operation'] in ('table_catalog', 'table_read'),
                    'invalid native waiter identity')
            ids.add(row['id'])
        return result

    if operation == 'acquire':
        require(not journal.exists(), 'barrier acquire cannot be repeated')
        # Record intent before dispatch: an unknown outcome must never resubmit.
        write_private(journal, json.dumps({'run_id': run_id, 'phase': phase, 'holder_id': None}))
        control = (f"SET application_name='{marker}'; BEGIN; "
                   'LOCK TABLE addp_business.addp_engine_probe IN ACCESS EXCLUSIVE MODE; '
                   'SELECT pg_sleep(240); ROLLBACK;')
        command('docker', 'exec', '-d', HIGHGO, 'bash', '-c',
                'export PGPASSWORD="$POSTGRES_PASSWORD" PGSSLMODE=require; '
                'exec psql -X -h127.0.0.1 -p5866 -Usysdba -dhighgo -v ON_ERROR_STOP=1 -c "$1"',
                'highgo-timeout', control)
        deadline = time.monotonic() + 10
        while (state := snapshot())['holder_id'] is None:
            require(time.monotonic() < deadline, 'native HighGo barrier acquisition deadline exceeded')
            time.sleep(0.1)
        receipt = {'run_id': run_id, 'phase': phase, 'holder_id': state['holder_id']}
        # The intent file is private and exclusive; replace only our own content.
        journal.write_text(json.dumps(receipt))
    else:
        receipt = json.loads(journal.read_text())
        require(receipt.get('run_id') == run_id and receipt.get('phase') == phase,
                'barrier receipt belongs to another run')
        state = snapshot()
        require(type(receipt.get('holder_id')) is int and receipt['holder_id'] > 0
                and state['holder_id'] == receipt['holder_id'], 'owned native HighGo barrier disappeared')
        if operation == 'release':
            # Check the marker again in the termination statement, not just an old PID.
            released = sql(HIGHGO, f"SELECT pg_terminate_backend(pid) FROM pg_stat_activity "
                           f"WHERE pid={receipt['holder_id']} AND application_name='{marker}' "
                           "AND usename='sysdba' AND datname='highgo';", directory, admin=True)
            require(released == 't', 'owned HighGo barrier release was not confirmed')
            deadline = time.monotonic() + 10
            while (state := snapshot())['holder_id'] is not None or state['waiters']:
                require(time.monotonic() < deadline, 'owned HighGo barrier release deadline exceeded')
                time.sleep(0.1)
    return {'run_id': run_id, 'phase': phase, 'stage': operation, 'holder_id': receipt['holder_id'],
            'held': state['holder_id'] == receipt['holder_id'], 'waiters': state['waiters']}


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
    if len(sys.argv) == 4 and sys.argv[1] == 'barrier':
        print(json.dumps(timeout_barrier(sys.argv[2], sys.argv[3]), sort_keys=True))
        return
    require(len(sys.argv) == 2 and sys.argv[1] in ('start', 'verify', 'stop'), 'use start|verify|stop')
    {'start': start, 'verify': verify, 'stop': stop}[sys.argv[1]]()


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        # Avoid a traceback with secret command input/arguments.
        print('HighGo fixture failed: ' + (str(error) if isinstance(error, RuntimeError) else type(error).__name__), file=sys.stderr)
        sys.exit(1)
