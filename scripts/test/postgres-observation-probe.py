"""Database-side observation contract in the metrics gate's disposable PG15.

No shared database, personal environment, business contents or raw SQL errors
are used. Passwords/verifiers travel only via private stdin and a private pgpass.
"""
import base64
import hashlib
import hmac
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import time

ROOT = Path(__file__).resolve().parents[2]
SQL = ROOT / 'scripts/infra/postgres-observation/000001_init.sql'
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
        self.pgpass = '/tmp/addp-observation-' + secrets.token_hex(16) + '.pgpass'
        self.peers = []

    def command(self, user='addp'):
        command = self.compose + ['exec', '-T', '-e', 'PGPASSFILE=' + self.pgpass,
                                  '-e', 'PGCONNECT_TIMEOUT=3', 'postgres',
                                  'psql', '-X', '-q', '-A', '-t', '-v', 'ON_ERROR_STOP=1',
                                  '-U', user, '-d', 'addp_test']
        if user != 'addp':
            command += ['-h', '127.0.0.1', '-p', '5432', '-w']
        return command

    def sql(self, sql, user='addp', expected=None):
        # sqlstate-only failures prevent role/password DDL from entering logs.
        result = subprocess.run(self.command(user), input='\\set VERBOSITY sqlstate\n' + sql + '\n',
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
        value = f'127.0.0.1:5432:addp_test:{user}:{password}\n'
        subprocess.run(self.compose + ['exec', '-T', 'postgres', 'sh', '-c',
                       'umask 077; cat > "$1"', 'observation-pgpass', self.pgpass],
                       input=value, capture_output=True, text=True, check=True, timeout=10)

    def install(self, scope, password, expected=None):
        sql = ('\\set observer_scope ' + quoted(scope) + '\n\\set observer_verifier '
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
        user = 'addp_' + scope + '_observer'
        password = secrets.token_hex(32)
        self.install(scope, password)
        self.password(user, password)
        assert self.sql('SELECT current_user;', user) == user
        assert self.sql('SHOW default_transaction_read_only;', user) == 'on'
        first = self.snapshot(user)
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
        for scope in ['infra', 'business']:
            self.run_scope(scope)
        assert self.sql('SELECT count(*) FROM observation_business.private_rows;') == '1'
        self.sql('DROP SCHEMA observation_business CASCADE; DROP ROLE observation_stranger;')
        self.absent()


if __name__ == '__main__':
    probe = Probe()
    try:
        probe.run()
    finally:
        probe.close_peers()
        subprocess.run(probe.compose + ['exec', '-T', 'postgres', 'rm', '-f', probe.pgpass],
                       capture_output=True, check=True, timeout=10)
