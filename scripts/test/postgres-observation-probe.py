"""Database-side observation contract in the metrics gate's disposable PG15.

No shared database, personal environment, business contents or raw SQL errors
are used. Passwords/verifiers travel only via private stdin and a private pgpass.
"""
import base64
import hashlib
import hmac
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import ssl
import subprocess
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
SQL = ROOT / 'scripts/infra/postgres-observation/000002_lifecycle.sql'
FIELDS = {
    'observed_at', 'server_started_at', 'statistics_generation', 'track_counts_enabled',
    'connections', 'active_connections', 'idle_connections', 'idle_in_transaction_connections',
    'active_waiting_connections', 'active_lock_waiting_connections', 'active_io_waiting_connections',
    'max_connections', 'transactions_committed', 'transactions_rolled_back', 'deadlocks', 'database_bytes',
}


def verifier(password):
    salt = secrets.token_bytes(16)
    salted = hashlib.pbkdf2_hmac('sha256', password.encode(), salt, 4096)
    client = hmac.digest(salted, b'Client Key', 'sha256')
    stored = hashlib.sha256(client).digest()
    server = hmac.digest(salted, b'Server Key', 'sha256')
    encoded = lambda value: base64.b64encode(value).decode('ascii')
    return f'SCRAM-SHA-256$4096:{encoded(salt)}${encoded(stored)}:{encoded(server)}'


def quoted(value):
    # psql variable input is trusted deployment material, never a command arg.
    return "'" + value.replace('\\', '\\\\').replace("'", "\\'") + "'"


class Probe:
    def __init__(self):
        project = os.environ['METRICS_T2_PROJECT']
        file = Path(os.environ['METRICS_T2_COMPOSE']).resolve()
        if (not re.fullmatch(r'addp-metrics-t2-[0-9a-f]{32}', project)
                or file != ROOT / 'scripts/test/docker-compose.monitor-metrics-t2.yml'):
            raise RuntimeError('observation probe requires the owned metrics gate')
        self.compose = ['docker', 'compose', '--env-file', '/dev/null', '-p', project, '-f', str(file)]
        identity = subprocess.run(self.compose + ['ps', '-q', 'postgres'], capture_output=True,
                                  text=True, check=True, timeout=10).stdout.strip()
        if not re.fullmatch(r'[0-9a-f]{64}', identity):
            raise RuntimeError('owned observation database is not available')
        labels = json.loads(subprocess.run(['docker', 'inspect', '--format', '{{json .Config.Labels}}', identity],
                            capture_output=True, text=True, check=True, timeout=10).stdout)
        if labels.get('com.docker.compose.project') != project or labels.get('com.docker.compose.service') != 'postgres':
            raise RuntimeError('observation database ownership mismatch')
        self.project, self.postgres_identity = project, identity
        spec = importlib.util.spec_from_file_location('addp_postgresql_source', SQL.with_name('source.py'))
        self.source = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.source)
        self.auth_directory = None
        self.authentication = None
        if os.environ.get('METRICS_T2_DIRECTORY_AUTH') == 'true':
            self.auth_directory = Path(os.environ['METRICS_T2_WORK']).resolve() / 'postgres-auth'
            spec = importlib.util.spec_from_file_location('addp_postgresql_auth_directory', SQL.with_name('authentication.py'))
            self.authentication = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(self.authentication)
        self.hba_original = self.hba_current = None
        self.pgpass = '/tmp/addp-observation-' + secrets.token_hex(16) + '.pgpass'
        self.peers = []

    def command(self, user='addp', *, database='addp_test', socket=False, physical=False):
        if database not in ('addp_test', 'postgres'):
            raise ValueError('observation authentication probe database is outside the owned fixture')
        command = self.compose + ['exec', '-T', '-e', 'PGPASSFILE=' + self.pgpass,
                                  '-e', 'PGCONNECT_TIMEOUT=3', 'postgres',
                                  'psql', '-X', '-q', '-A', '-t', '-v', 'ON_ERROR_STOP=1',
                                  '-U', user, '-d', database]
        if user != 'addp' and not socket:
            command += ['-h', '127.0.0.1', '-p', '5432', '-w']
        if physical:
            command += ['-d', 'replication=true dbname=replication']
        return command

    def sql(self, sql, user='addp', expected=None, *, database='addp_test'):
        # sqlstate-only failures prevent role/password DDL from entering logs.
        result = subprocess.run(self.command(user, database=database), input='\\set VERBOSITY sqlstate\n' + sql + '\n',
                                capture_output=True, text=True, timeout=15)
        if expected is not None:
            if result.returncode == 0 or expected not in result.stderr:
                state = re.search(r'(?:ERROR|FATAL):\s+([A-Z0-9]{5})(?:\s|$)', result.stderr)
                raise AssertionError('expected observation SQLSTATE ' + expected + ', actual '
                                     + (state.group(1) if state else 'unclassified'))
            return None
        if result.returncode:
            state = re.search(r'ERROR:\s+([A-Z0-9]{5})', result.stderr)
            raise AssertionError('observation SQL failed: ' + (state.group(1) if state else 'unknown'))
        return result.stdout.strip()

    def password(self, user, password):
        value = ''.join(f'127.0.0.1:5432:{database}:{user}:{password}\n' for database in ('addp_test', 'postgres'))
        subprocess.run(self.compose + ['exec', '-T', 'postgres', 'sh', '-c',
                       'umask 077; cat > "$1"', 'observation-pgpass', self.pgpass],
                       input=value, capture_output=True, text=True, check=True, timeout=10)

    def read_hba(self):
        if self.auth_directory is not None:
            path = '/etc/addp-postgres-auth/pg_hba.conf'
            assert self.sql('SHOW hba_file;') == path
            return subprocess.run(self.compose + ['exec', '-T', 'postgres', 'cat', path],
                                  capture_output=True, text=True, check=True, timeout=10).stdout
        assert self.sql('SHOW hba_file;') == '/var/lib/postgresql/data/pg_hba.conf'
        return subprocess.run(self.compose + ['exec', '-T', 'postgres', 'sh', '-c',
                              'test ! -L "$PGDATA/pg_hba.conf" && test -f "$PGDATA/pg_hba.conf" && cat "$PGDATA/pg_hba.conf"'],
                              capture_output=True, text=True, check=True, timeout=10).stdout

    def write_hba(self, expected, replacement):
        # This is strictly an owned T2 fixture adapter, not a product file-write
        # API. Preserve owner/mode and reject changed input before atomic rename.
        digest = hashlib.sha256(expected.encode()).hexdigest()
        path = '/etc/addp-postgres-auth/pg_hba.conf' if self.auth_directory is not None else '/var/lib/postgresql/data/pg_hba.conf'
        metadata_command = self.compose + ['exec', '-T', 'postgres', 'stat', '-c', '%u:%g:%a', path]
        metadata = subprocess.run(metadata_command, capture_output=True, text=True, check=True, timeout=10).stdout
        script = '''set -eu
target="$PGDATA/pg_hba.conf"
test ! -L "$target" && test -f "$target"
test "$(sha256sum "$target" | cut -d ' ' -f 1)" = "$1"
temporary=$(mktemp "$target.addp.XXXXXX")
trap 'rm -f "$temporary"' EXIT
cat > "$temporary"
chown --reference="$target" "$temporary"
chmod --reference="$target" "$temporary"
test "$(sha256sum "$target" | cut -d ' ' -f 1)" = "$1"
mv "$temporary" "$target"
trap - EXIT
'''
        if self.auth_directory is not None:
            self.authentication.replace(self.auth_directory, expected, replacement)
        else:
            subprocess.run(self.compose + ['exec', '-T', 'postgres', 'sh', '-c', script, 'owned-observation-hba', digest],
                           input=replacement, capture_output=True, text=True, check=True, timeout=10)
        assert self.read_hba() == replacement
        assert subprocess.run(metadata_command, capture_output=True, text=True, check=True, timeout=10).stdout == metadata
        assert self.sql('SELECT count(*) FROM pg_hba_file_rules WHERE error IS NOT NULL;') == '0'
        before = self.sql('SELECT pg_conf_load_time();')
        assert self.sql('SELECT pg_reload_conf();') == 't'
        deadline = time.monotonic() + 3
        while self.sql('SELECT pg_conf_load_time();') == before:
            if time.monotonic() >= deadline:
                raise AssertionError('owned observation authentication reload timed out')
            time.sleep(.05)

    def install_authentication(self, scope, user):
        networks = json.loads(subprocess.run(['docker', 'inspect', '--format', '{{json .NetworkSettings.Networks}}', self.postgres_identity],
                              capture_output=True, text=True, check=True, timeout=10).stdout)
        assert set(networks) == {self.project + '_default'}
        network_id = networks[self.project + '_default']['NetworkID']
        assert re.fullmatch(r'[0-9a-f]{64}', network_id)
        network = json.loads(subprocess.run(['docker', 'network', 'inspect', network_id],
                             capture_output=True, text=True, check=True, timeout=10).stdout)[0]
        assert network['Labels']['com.docker.compose.project'] == self.project
        allowed = ['127.0.0.1/32'] + [item['Subnet'] for item in network['IPAM']['Config']]
        self.hba_original = self.hba_current = self.read_hba()
        replacement = self.source.authentication_configuration(self.hba_original, scope, allowed, database='addp_test')
        try:
            self.write_hba(self.hba_original + '# stale input', replacement)
        except (subprocess.CalledProcessError, ValueError):
            pass
        else:
            raise AssertionError('owned authentication writer accepted changed input')
        assert self.read_hba() == self.hba_original
        # A reserved database word must remain literal in the real HBA parser.
        # No "all" database is created, and no business object is accessed.
        literal = self.source.authentication_configuration(self.hba_original, scope, allowed, database='all')
        self.write_hba(self.hba_current, literal)
        self.hba_current = literal
        self.assert_authentication_rejected(user, database='postgres')
        self.write_hba(self.hba_current, self.hba_original)
        self.hba_current = self.hba_original
        self.write_hba(self.hba_current, replacement)
        self.hba_current = replacement
        assert self.source.authentication_configuration(replacement, scope, allowed, database='addp_test') == replacement
        for database, socket in [('postgres', False), ('addp_test', True)]:
            self.assert_authentication_rejected(user, database=database, socket=socket)
        for socket in (False, True):
            self.assert_authentication_rejected(user, database='addp_test', socket=socket, physical=True)
        assert self.sql('SELECT 1;', database='postgres') == '1'
        self.snapshot(user)
        print('PostgreSQL authentication T2: ' + scope + ' maintenance SCRAM allowed, other database/socket rejected, admin preserved', flush=True)

    def assert_authentication_rejected(self, user, *, database, socket=False, physical=False):
        result = subprocess.run(self.command(user, database=database, socket=socket, physical=physical), input='SELECT 1;\n',
                                capture_output=True, text=True, timeout=10)
        rejection = 'pg_hba.conf rejects replication connection' if physical else 'pg_hba.conf rejects connection'
        assert result.returncode != 0 and rejection in result.stderr, 'observer escaped the maintenance database authentication boundary'

    def restore_authentication(self):
        if self.hba_original is not None:
            self.write_hba(self.hba_current, self.hba_original)
            self.hba_original = self.hba_current = None

    def install(self, scope, password, expected=None):
        sql = ('\\set observer_scope ' + quoted(scope) + '\n\\set observer_enabled true\n\\set observer_verifier '
               + quoted(verifier(password)) + '\n' + SQL.read_text())
        return self.sql(sql, expected=expected)

    def snapshot(self, user):
        result = json.loads(self.sql('SELECT row_to_json(s) FROM addp_observation.snapshot() s;', user))
        assert set(result) == FIELDS, 'snapshot must contain only the fixed aggregate fields'
        assert result['track_counts_enabled'] is True
        assert re.fullmatch(r'[0-9a-f]{32}', result['statistics_generation'])
        for name in FIELDS - {'observed_at', 'server_started_at', 'statistics_generation', 'track_counts_enabled'}:
            assert isinstance(result[name], int) and result[name] >= 0, 'invalid aggregate value'
        assert result['database_bytes'] > 0
        return result

    def absent(self):
        assert self.sql("SELECT count(*) FROM pg_roles WHERE rolname IN ('addp_observation_owner', 'addp_infra_observer', 'addp_business_observer');") == '0'
        assert self.sql("SELECT count(*) FROM pg_namespace WHERE nspname = 'addp_observation';") == '0'

    def peer(self, transaction=False):
        process = subprocess.Popen(self.command(), stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                   stderr=subprocess.DEVNULL, text=True)
        self.peers.append(process)
        process.stdin.write(('BEGIN;\n' if transaction else '') + 'SELECT pg_backend_pid();\n')
        process.stdin.flush()
        # A bounded server statement and the enclosing standard gate own this
        # local fixture; no arbitrary shell or personal process is controlled.
        pid = int(process.stdout.readline().strip())
        return process, pid

    def close_peers(self):
        for process in self.peers:
            if process.poll() is None:
                try:
                    process.stdin.write('\\q\n')
                    process.stdin.flush()
                except BrokenPipeError:
                    pass
            try:
                process.communicate(timeout=10)
            except subprocess.TimeoutExpired:
                process.terminate()
                process.communicate(timeout=5)
        self.peers.clear()

    def run_scope(self, scope):
        try:
            self.run_scope_case(scope)
        finally:
            self.restore_authentication()

    def run_scope_case(self, scope):
        user = 'addp_' + scope + '_observer'
        password = secrets.token_hex(32)
        self.install(scope, password)
        self.password(user, password)
        assert self.sql('SELECT current_user;', user) == user
        assert self.sql('SHOW default_transaction_read_only;', user) == 'on'
        first = self.snapshot(user)
        # PUBLIC CONNECT permits this before admission even though the fixed
        # function was installed only in addp_test. Read no table or SQL text.
        assert self.sql('SELECT current_user;', user, database='postgres') == user
        self.install_authentication(scope, user)
        identity = self.sql("SELECT 'addp_observation.snapshot()'::regprocedure::oid;")
        self.install(scope, password)
        assert self.sql("SELECT 'addp_observation.snapshot()'::regprocedure::oid;") == identity
        self.snapshot(user)
        self.sql('SET ROLE addp_observation_owner;', user, '42501')
        self.sql('SET ROLE pg_read_all_stats;', user, '42501')
        self.sql('SELECT rolpassword FROM pg_authid;', user, '42501')
        self.sql('SELECT secret FROM observation_business.private_rows;', user, '42501')
        self.sql("SET default_transaction_read_only = off; INSERT INTO observation_business.private_rows VALUES ('forbidden');", user, '42501')
        self.sql('SELECT * FROM addp_observation.snapshot(1);', user, '42883')
        self.sql('SET ROLE observation_stranger; SELECT * FROM addp_observation.snapshot();', expected='42501')
        assert self.sql("SELECT count(*) FROM pg_auth_members m JOIN pg_roles r ON r.oid=m.member WHERE r.rolname='" + user + "';") == '0'
        poisoned = json.loads(self.sql('''SET default_transaction_read_only = off;
CREATE TEMP TABLE pg_stat_activity (backend_type text, pid integer, state text, wait_event_type text);
INSERT INTO pg_temp.pg_stat_activity VALUES ('client backend', -1, 'active', 'Lock');
SET search_path = pg_temp, public;
SELECT row_to_json(s) FROM addp_observation.snapshot() s;''', user))
        assert poisoned['connections'] == 0, 'temporary relations must not shadow catalog reads'

        self.peer()
        self.peer(transaction=True)
        active, active_pid = self.peer()
        active.stdin.write("SELECT pg_sleep(8) /* observation-business-SQL-must-remain-private */;\n")
        active.stdin.flush()
        deadline = time.monotonic() + 6
        while time.monotonic() < deadline:
            sample = self.snapshot(user)
            if sample['active_waiting_connections'] == 1:
                break
            time.sleep(.1)
        else:
            raise AssertionError('real active wait was not observed')
        assert sample['connections'] == 3
        assert sample['active_connections'] == 1
        assert sample['idle_connections'] == 1
        assert sample['idle_in_transaction_connections'] == 1
        assert sample['active_waiting_connections'] == 1, 'idle ClientRead must not be counted as pressure'
        assert sample['active_lock_waiting_connections'] == 0
        assert sample['active_io_waiting_connections'] == 0
        assert self.sql(f"SELECT query = '<insufficient privilege>' FROM pg_stat_activity WHERE pid={active_pid};", user) == 't'
        assert self.sql(f'SELECT pg_cancel_backend({active_pid});', user, '42501') is None
        self.close_peers()

        locker, _ = self.peer(transaction=True)
        locker.stdin.write('LOCK TABLE observation_business.private_rows IN ACCESS EXCLUSIVE MODE;\n\\echo locked\n')
        locker.stdin.flush()
        assert locker.stdout.readline().strip() == 'locked'
        waiter, _ = self.peer()
        waiter.stdin.write("SET statement_timeout = '5s'; SELECT count(*) FROM observation_business.private_rows;\n")
        waiter.stdin.flush()
        deadline = time.monotonic() + 4
        while time.monotonic() < deadline:
            sample = self.snapshot(user)
            if sample['active_lock_waiting_connections'] == 1:
                break
            time.sleep(.1)
        else:
            raise AssertionError('real lock wait was not observed')
        assert sample['active_waiting_connections'] == 1
        self.close_peers()

        # Independent catalog oracle shares a statistics snapshot with the
        # function call, so counter changes between separate sessions cannot
        # turn an imprecise comparison into a false pass.
        oracle = json.loads(self.sql('''BEGIN;
SET LOCAL stats_fetch_consistency = 'snapshot';
SELECT json_build_object('sample', row_to_json(s), 'commits', c.commits,
 'rollbacks', c.rollbacks, 'deadlocks', c.deadlocks, 'bytes', c.bytes)
FROM addp_observation.snapshot() s CROSS JOIN (
 SELECT sum(t.xact_commit) AS commits, sum(t.xact_rollback) AS rollbacks,
 sum(t.deadlocks) AS deadlocks, sum(pg_database_size(d.oid)) AS bytes
 FROM pg_database d JOIN pg_stat_database t ON t.datid = d.oid WHERE NOT d.datistemplate
) c;
ROLLBACK;'''))
        for field, expected in [('transactions_committed', 'commits'), ('transactions_rolled_back', 'rollbacks'),
                                ('deadlocks', 'deadlocks'), ('database_bytes', 'bytes')]:
            assert oracle['sample'][field] == oracle[expected], 'aggregate differs from independent catalog oracle'
        disabled = json.loads(self.sql('''BEGIN; SET LOCAL track_counts = off;
SELECT row_to_json(s) FROM addp_observation.snapshot() s; ROLLBACK;'''))
        assert disabled['track_counts_enabled'] is False
        assert all(disabled[name] is None for name in ['transactions_committed', 'transactions_rolled_back', 'deadlocks'])
        self.sql('SELECT pg_stat_reset();')
        assert self.snapshot(user)['statistics_generation'] != first['statistics_generation']

        replacement = secrets.token_hex(32)
        self.install(scope, replacement)
        self.sql('SELECT 1;', user, 'password authentication failed')
        self.password(user, replacement)
        self.snapshot(user)
        if os.environ.get('METRICS_T2_POSTGRES_SOURCE_ENABLED') == 'true':
            self.source_transport(scope, user, replacement)
        self.install('business' if scope == 'infra' else 'infra', secrets.token_hex(32), 'P0001')
        assert self.sql("SELECT count(*) FROM pg_roles WHERE rolname='addp_" + ('business' if scope == 'infra' else 'infra') + "_observer';") == '0'
        self.sql(f'GRANT pg_read_all_stats TO {user};')
        self.install(scope, replacement, 'P0001')
        self.sql(f'REVOKE pg_read_all_stats FROM {user};')
        self.sql('GRANT EXECUTE ON FUNCTION addp_observation.snapshot() TO PUBLIC;')
        self.install(scope, replacement, 'P0001')
        self.sql('REVOKE EXECUTE ON FUNCTION addp_observation.snapshot() FROM PUBLIC;')
        self.snapshot(user)
        self.sql(f'DROP SCHEMA addp_observation CASCADE; REVOKE CONNECT ON DATABASE addp_test FROM {user}; DROP ROLE {user}; DROP ROLE addp_observation_owner;')
        self.absent()
        print('PostgreSQL observation T2: ' + scope + ' password, aggregates, privilege denials, idempotence, rotation and ownership passed', flush=True)

    def source_transport(self, scope, user, password):
        work = Path(os.environ['METRICS_T2_WORK']).resolve()
        directory = Path(os.environ['ADDP_POSTGRES_METRICS_SOURCE_DIR'])
        web = Path(os.environ['ADDP_POSTGRES_METRICS_WEB_DIR'])
        assert directory.resolve() == work / 'postgres-source' and web.resolve() == work / 'postgres-web'
        for path in [directory, web]:
            path.mkdir(mode=0o700, exist_ok=True)
        config = self.source.configuration(scope, 'postgres', 5432, password, database='addp_test')
        config_file = directory / 'sql-exporter.yml'
        config_file.write_text(json.dumps(config))
        config_file.chmod(0o600)
        for name, original in [('server.crt', work / 'source-server.crt'), ('server.key', work / 'source-server.key'),
                               ('ca.crt', work / 'deployment/source-ca.crt')]:
            path = web / name
            path.write_bytes(original.read_bytes())
            path.chmod(0o600)
        if os.getuid() == 0:
            for path in [directory, *directory.iterdir(), web, *web.iterdir()]:
                os.chown(path, 65534, 65534)
        deployed = subprocess.run(self.compose + ['up', '-d', '--force-recreate', 'postgres-metrics', 'postgres-metrics-web'],
                                  capture_output=True, text=True, timeout=180)
        if deployed.returncode:
            raise AssertionError('owned PostgreSQL source deployment failed')
        port = subprocess.run(self.compose + ['port', 'postgres-metrics', '9443'], capture_output=True,
                              check=True, text=True, timeout=10).stdout.strip().rsplit(':', 1)[1]
        origin = 'https://localhost:' + port
        context = ssl.create_default_context(cafile=str(web / 'ca.crt'))
        context.load_cert_chain(str(work / 'deployment/collector.crt'), str(work / 'deployment/collector.key'))

        def get(path='/metrics', *, tls=context, method='GET', expected=200):
            request = urllib.request.Request(origin + path, method=method)
            try:
                with urllib.request.urlopen(request, context=tls, timeout=6) as response:
                    status, body = response.status, response.read(65537).decode('utf-8')
            except urllib.error.HTTPError as error:
                status, body = error.code, error.read(65537).decode('utf-8')
            assert status == expected and len(body.encode()) <= 65536, f'unexpected source HTTP status {status}, expected {expected}, or size'
            assert password not in body and 'postgresql://' not in body, 'source response exposed credentials'
            return body

        deadline = time.monotonic() + 25
        while True:
            try:
                body = get()
                break
            except (urllib.error.URLError, AssertionError):
                if time.monotonic() >= deadline:
                    raise AssertionError('owned PostgreSQL mTLS source did not become ready') from None
                time.sleep(.2)
        expected = {item['metric_name'] for item in config['collectors'][0]['metrics']}
        rows = {}
        for line in body.splitlines():
            if line and not line.startswith('#'):
                match = re.fullmatch(r'(\w+)(?:\{([^}]*)\})? (\S+)', line)
                assert match and match[1] in expected, 'source emitted an unregistered metric'
                assert match[1] not in rows, 'source emitted duplicate metric series'
                rows[match[1]] = (match[2], float(match[3]))
        assert set(rows) == expected
        observed = rows['addp_postgresql_sample_time_seconds'][1]
        assert 0 <= time.time() - observed < 10
        assert rows['addp_postgresql_connections'][1] == 0
        assert rows['addp_postgresql_database_bytes'][1] > 0
        for name in expected:
            if name.endswith('_total'):
                assert re.fullmatch(r'counter_epoch="[0-9a-f]{32}"', rows[name][0])
        get('/config', expected=404)
        get('/sql_exporter_metrics', expected=404)
        get('/reload', method='POST', expected=404)
        get('/metrics?target=forbidden', expected=400)
        get(method='POST', expected=405)
        get(tls=ssl.create_default_context(cafile=str(web / 'ca.crt')), expected=400)
        try:
            urllib.request.urlopen(origin.replace('https://', 'http://') + '/metrics', timeout=3)
        except urllib.error.HTTPError as error:
            assert error.code == 400 and b'addp_postgresql_' not in error.read(65537)
        else:
            raise AssertionError('plaintext source request was accepted')
        self.sql(f'REVOKE EXECUTE ON FUNCTION addp_observation.snapshot() FROM {user};')
        failed = get()
        assert not any(re.search(r'^' + name + r'(?:\{| )', failed, re.M) for name in expected), 'failed query returned old samples'
        errors = re.findall(r'^scrape_errors_total(?:\{[^}]*\})? ([\d.e+]+)$', failed, re.M)
        assert errors and sum(map(float, errors)) >= 1, 'failed SQL query must expose an explicit error counter'
        failed_again = get()
        assert not any(re.search(r'^' + name + r'(?:\{| )', failed_again, re.M) for name in expected), 'repeat failure returned old samples'
        next_errors = re.findall(r'^scrape_errors_total(?:\{[^}]*\})? ([\d.e+]+)$', failed_again, re.M)
        assert sum(map(float, next_errors)) > sum(map(float, errors)), 'repeat SQL failure must not be cached or hidden'
        self.sql(f'GRANT EXECUTE ON FUNCTION addp_observation.snapshot() TO {user};')
        recovered = get()
        match = re.search(r'^addp_postgresql_sample_time_seconds ([\d.e+]+)$', recovered, re.M)
        assert match and float(match[1]) > observed, 'source recovery must produce a new observation'
        self.sql(f'ALTER ROLE {user} SET track_counts = off;')
        disabled = get()
        assert re.search(r'^addp_postgresql_track_counts_enabled 0$', disabled, re.M)
        assert not any(re.search(r'^' + name + r'(?:\{| )', disabled, re.M) for name in expected if name.endswith('_total')), 'disabled statistics must not become valid zero counters'
        self.sql(f'ALTER ROLE {user} RESET track_counts;')
        assert re.search(r'^addp_postgresql_track_counts_enabled 1$', get(), re.M)
        logs = subprocess.run(self.compose + ['logs', '--no-color', 'postgres-metrics', 'postgres-metrics-web'],
                              capture_output=True, text=True, check=True, timeout=10).stdout
        assert password not in logs, 'source logs exposed credentials'
        print('PostgreSQL source T2: ' + scope + ' fixed metrics, mTLS, restricted HTTP, failure and fresh recovery passed', flush=True)

    def run(self):
        self.absent()
        self.sql('''CREATE SCHEMA observation_business;
CREATE TABLE observation_business.private_rows (secret text);
INSERT INTO observation_business.private_rows VALUES ('business-data-must-remain-private');
CREATE ROLE observation_stranger NOLOGIN;''')
        # This login is intentionally unrelated and has no observation grants.
        # Use a private pgpass only when its negative connection is exercised.
        self.sql('CREATE ROLE addp_observation_owner NOLOGIN;')
        self.install('infra', secrets.token_hex(32), 'P0001')
        self.sql('DROP ROLE addp_observation_owner;')
        self.absent()
        self.sql('CREATE SCHEMA addp_observation;')
        self.install('infra', secrets.token_hex(32), 'P0001')
        assert self.sql("SELECT nspowner = (SELECT oid FROM pg_roles WHERE rolname = current_user) FROM pg_namespace WHERE nspname='addp_observation';") == 't'
        self.sql('DROP SCHEMA addp_observation;')
        self.absent()
        self.sql('ALTER DEFAULT PRIVILEGES GRANT EXECUTE ON FUNCTIONS TO observation_stranger;')
        self.install('infra', secrets.token_hex(32), 'P0001')
        self.absent()
        self.sql('ALTER DEFAULT PRIVILEGES REVOKE EXECUTE ON FUNCTIONS FROM observation_stranger;')
        self.sql('GRANT SELECT ON observation_business.private_rows TO PUBLIC;')
        self.install('infra', secrets.token_hex(32), 'P0001')
        self.absent()
        assert self.sql("SELECT has_table_privilege('observation_stranger', 'observation_business.private_rows', 'SELECT');") == 't'
        self.sql('REVOKE SELECT ON observation_business.private_rows FROM PUBLIC;')
        # Use SET ROLE in the controlled admin transport for the unrelated-user
        # rejection; actual observer authentication always uses TCP SCRAM.
        if self.auth_directory is not None:
            original = self.read_hba()
            attempted = subprocess.run(self.compose + ['exec', '-T', 'postgres', 'sh', '-c',
                                       'printf forbidden >> /etc/addp-postgres-auth/pg_hba.conf'],
                                      capture_output=True, text=True, timeout=10)
            assert attempted.returncode != 0 and self.read_hba() == original, 'container authentication mount must be read-only'
        for scope in (['business'] if self.auth_directory is not None else ['infra', 'business']):
            self.run_scope(scope)
        assert self.sql('SELECT count(*) FROM observation_business.private_rows;') == '1'
        self.sql('DROP SCHEMA observation_business CASCADE; DROP ROLE observation_stranger;')
        self.absent()
        if self.auth_directory is not None:
            print('PostgreSQL directory authentication T2: read-only mount, host atomic update, real reload and complete restore passed', flush=True)


if __name__ == '__main__':
    probe = Probe()
    try:
        probe.run()
    finally:
        probe.close_peers()
        subprocess.run(probe.compose + ['exec', '-T', 'postgres', 'rm', '-f', probe.pgpass],
                       capture_output=True, check=True, timeout=10)
