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
import selectors
import secrets
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[2]
SQL = ROOT / 'scripts/infra/postgres-observation/000002_lifecycle.sql'
FIELDS = {
    'observed_at', 'server_started_at', 'statistics_generation', 'track_counts_enabled',
    'connections', 'active_connections', 'idle_connections', 'idle_in_transaction_connections',
    'active_waiting_connections', 'active_lock_waiting_connections', 'active_io_waiting_connections',
    'max_connections', 'transactions_committed', 'transactions_rolled_back', 'deadlocks', 'database_bytes',
}

# No external commands, subshells or child processes: reads, timed waits and
# filename expansion all execute in this one Bash process in the owned PG image.
DIRECTORY_PROCESS_SCRIPT = r'''
set -eu
exec 2>/dev/null
a=$1; b=$2
first=$a; second=$b
if [[ "$3" == b ]]; then first=$b; second=$a; fi
sample() {
    local phase=$1 av bv value uptime rest
    if IFS= read -r value < "$a"; then av=$value; else av=unreadable; fi
    if IFS= read -r value < "$b"; then bv=$value; else bv=unreadable; fi
    case "$av" in before|after-version|unreadable) ;; *) av=other ;; esac
    case "$bv" in before|after-version|unreadable) ;; *) bv=other ;; esac
    IFS=' ' read -r uptime rest < /proc/uptime
    printf '%s\t%s\t%s\t%s\t%s\n' "$phase" "$$" "$uptime" "$av" "$bv"
}
enumerate() {
    local target=$1 entry found=0
    local entries=("${target%/*}"/*)
    for entry in "${entries[@]}"; do
        if [[ "$entry" == "$target" ]]; then found=1; fi
    done
    [[ "$found" == 1 ]]
}
sample baseline
IFS= read -r -t 2 signal
[[ "$signal" == published ]]
sample published
if IFS= read -r -t 1 signal; then exit 75; fi
sample control_before_enum
enumerate "$first"
sample enumerate_first
if IFS= read -r -t 1 signal; then exit 75; fi
sample control_before_enum_b
enumerate "$second"
sample enumerate_second
'''


def directory_process_observations(data):
    phases = ['baseline','published','control_before_enum','enumerate_first','control_before_enum_b','enumerate_second']
    lines = data.decode('ascii').splitlines()
    assert len(lines)==len(phases), 'Owned directory process output incomplete'
    rows, pids, times = [], set(), []
    for phase,line in zip(phases,lines):
        fields = line.split('\t')
        assert len(fields)==5 and fields[0]==phase, 'Owned directory process phase mismatch'
        _,pid,clock,a,b = fields
        assert re.fullmatch(r'[1-9][0-9]{0,9}',pid) and re.fullmatch(r'[0-9]{1,12}\.[0-9]{1,6}',clock), 'Owned directory process clock or identity invalid'
        assert a in ('before','after-version','unreadable','other') and b in ('before','after-version','unreadable','other'), 'Owned directory process state invalid'
        pids.add(pid);times.append(float(clock))
        rows.append({'phase':phase,'elapsed_ms':round((times[-1]-times[0])*1000,2),'a':a,'b':b})
    assert len(pids)==1 and times==sorted(times) and times[-1]-times[0]<=8, 'Owned directory process identity or monotonic order changed'
    assert round((times[2]-times[1])*1000)>=990 and round((times[4]-times[3])*1000)>=990, 'Owned directory no-enumeration interval incomplete'
    assert rows[0]['a']==rows[0]['b']=='before', 'Owned directory process baseline differs'
    return {'single_process':True,'samples':rows}


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


def check_source_admission(work, port):
    web = work / 'postgres-web'
    binary = work / 'postgres-admission.test'
    environment = dict(os.environ, GOWORK='off', GOMAXPROCS='2')
    print('PostgreSQL admission T2: build test binary (preparation limit 180s)', flush=True)
    subprocess.run(['go', 'test', '-c', './internal/metricsdiscovery', '-o', str(binary)],
                   cwd=ROOT / 'monitor/backend', env=environment, check=True, timeout=180)
    print('PostgreSQL admission T2: execute prepared test (Go limit 30s, process limit 45s)', flush=True)
    subprocess.run([str(binary), '-test.run=^TestIntegrationMetricsPostgreSQLAdmission$',
                    '-test.count=1', '-test.timeout=30s', '-test.v'],
                   cwd=ROOT / 'monitor/backend/internal/metricsdiscovery',
                   env=dict(environment, ADDP_POSTGRES_ADMISSION_INTEGRATION='1',
                            ADDP_POSTGRES_ADMISSION_ENDPOINT='https://127.0.0.1:' + port + '/metrics',
                            ADDP_POSTGRES_ADMISSION_CA=str(web / 'ca.crt'),
                            ADDP_POSTGRES_ADMISSION_CERT=str(work / 'admission.crt'),
                            ADDP_POSTGRES_ADMISSION_KEY=str(work / 'admission.key')),
                   check=True, timeout=45)


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
                                     + (state.group(1) if state else 'unclassified '+json.dumps(self.sql_failure_diagnostic(result))))
            return None
        if result.returncode:
            state = re.search(r'ERROR:\s+([A-Z0-9]{5})', result.stderr)
            raise AssertionError('observation SQL failed: ' + (state.group(1) if state else 'unknown'))
        return result.stdout.strip()

    def sql_failure_diagnostic(self, result):
        """Closed facts only; never echo SQL, connection errors or credentials."""
        facts = {'return_code': result.returncode, 'stderr_characters': len(result.stderr), 'database': 'unavailable'}
        try:
            state = subprocess.run(['docker', 'inspect', '--format',
                '{"running":{{json .State.Running}},"oom":{{json .State.OOMKilled}},"exit":{{json .State.ExitCode}}}',
                self.postgres_identity], capture_output=True, text=True, check=True, timeout=5)
            state = json.loads(state.stdout)
            if not isinstance(state, dict):
                return facts
            if (set(state)=={'running','oom','exit'} and type(state['running']) is bool
                    and type(state['oom']) is bool and type(state['exit']) is int):
                facts['database'] = state
            if state.get('running') is True:
                memory = subprocess.run(self.compose + ['exec', '-T', 'postgres', 'cat', '/sys/fs/cgroup/memory.events'],
                    capture_output=True, text=True, check=True, timeout=3)
                facts['memory'] = {key:int(value) for key,value in re.findall(r'(?m)^(oom|oom_kill|oom_group_kill) ([0-9]+)$', memory.stdout)}
        except (OSError, ValueError, subprocess.SubprocessError):
            pass
        return facts

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
        # Publication and consumer validation are separate facts. Cleanup must
        # compare against the published version even when readback fails.
        self.hba_current = replacement
        try:
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
        except BaseException as error:
            print('PostgreSQL HBA publication diagnostic '+json.dumps(self.hba_publication_diagnostic(error, replacement)),
                  file=sys.stderr, flush=True)
            raise

    def hba_publication_diagnostic(self, error, replacement):
        def kind(error):
            message = getattr(error, 'stderr', '') or ''
            return ('not_found' if 'No such file' in message else 'permission_denied' if 'Permission denied' in message
                    else 'unclassified')
        facts = {'read_error_kind': kind(error), 'return_code': getattr(error, 'returncode', None)}
        if self.auth_directory is None:
            return facts
        try:
            hba = self.auth_directory/'runtime/pg_hba.conf'
            value, _ = self.authentication._read(hba, 0o644)
            facts['host'] = {'matches_replacement': value==replacement, 'bytes': hba.stat().st_size, 'mode': '644'}
            mounts = subprocess.run(['docker','inspect','--format','{{json .Mounts}}',self.postgres_identity],
                capture_output=True,text=True,check=True,timeout=5)
            mounts = [m for m in json.loads(mounts.stdout) if m['Destination']=='/etc/addp-postgres-auth']
            facts['mount'] = {'unique': len(mounts)==1, 'readonly_bind': len(mounts)==1 and mounts[0]['Type']=='bind' and mounts[0]['RW'] is False,
                'source_matches': len(mounts)==1 and Path(mounts[0]['Source']).resolve()==self.auth_directory/'runtime'}
            result = subprocess.run(self.compose+['exec','-T','postgres','stat','-c','%a:%s:%u:%g','/etc/addp-postgres-auth/pg_hba.conf'],
                capture_output=True,text=True,timeout=5)
            facts['container'] = {'return_code':result.returncode,'error_kind':kind(result),
                'metadata':result.stdout.strip() if re.fullmatch(r'[0-7]{3,4}:[0-9]+:[0-9]+:[0-9]+\n?',result.stdout) else 'unavailable'}
        except (OSError,ValueError,KeyError,TypeError,subprocess.SubprocessError):
            facts['diagnostic_unavailable'] = True
        return facts

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
        self.assert_authentication_rejected(user, database='postgres')
        self.write_hba(self.hba_current, self.hba_original)
        self.write_hba(self.hba_current, replacement)
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
        except BaseException as error:
            try:
                self.restore_authentication()
            except BaseException as cleanup:
                error.add_note('Owned PostgreSQL HBA cleanup failed: '+type(cleanup).__name__)
            raise
        else:
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
        check_source_admission(work, port)
        get(tls=ssl.create_default_context(cafile=str(web / 'ca.crt')), expected=403)
        untrusted = ssl.create_default_context(cafile=str(web / 'ca.crt'))
        untrusted.load_cert_chain(str(work / 'untrusted.crt'), str(work / 'untrusted.key'))
        get(tls=untrusted, expected=403)
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
        if scope == 'infra' and self.auth_directory is None:
            self.professional_queries(work, user)
        logs = subprocess.run(self.compose + ['logs', '--no-color', 'postgres-metrics', 'postgres-metrics-web'],
                              capture_output=True, text=True, check=True, timeout=10).stdout
        assert password not in logs, 'source logs exposed credentials'
        print('PostgreSQL source T2: ' + scope + ' fixed metrics, mTLS, restricted HTTP, failure and fresh recovery passed', flush=True)

    def publish_query_discovery(self, text):
        # Nginx must observe the same atomic rename. Host-side replacement over
        # a Desktop bind mount can leave its Linux view pointing at an absent
        # inode. Publish through this owned fixture as the directory's owner.
        result = subprocess.run(self.compose + ['exec', '-T', '--user', f'{os.getuid()}:{os.getgid()}',
            'metrics-source', 'sh', '-ec',
            'umask 022; pending=/source/discovery.pending; trap \'rm -f "$pending"\' EXIT; '
            'cat > "$pending"; chmod 644 "$pending"; mv -f "$pending" /source/discovery.json; cat /source/discovery.json'],
            input=text, capture_output=True, text=True, check=True, timeout=10)
        if result.stdout != text:
            raise AssertionError('owned query discovery publication differs from intent')

    def professional_queries(self, work, user):
        """Actual PG -> exporter -> sole Prometheus SD -> Monitor query formulas."""
        cid = subprocess.check_output(self.compose + ['ps', '-q', 'postgres'], text=True).strip()
        facts = json.loads(subprocess.check_output(['docker', 'inspect', '--format',
            '{"container_id":{{json .Id}},"started_at":{{json .State.StartedAt}}}', cid], text=True))
        if '.' in facts['started_at']:
            seconds, fraction = facts['started_at'][:-1].split('.')
            fraction = fraction.rstrip('0')
            facts['started_at'] = seconds + ('.' + fraction if fraction else '') + 'Z'
        deployment = str(uuid.uuid4())
        record = {'deployment_id': deployment, 'instance': 'postgres-metrics:9443', 'source': {
            'state': 'available', 'endpoint': 'https://postgres-metrics:9443/metrics', **facts}}
        record_file = work / 'postgres-query-record.json'
        record_file.write_text(json.dumps(record))
        discovery = work / 'source/discovery.json'
        original = discovery.read_text()

        port = subprocess.check_output(self.compose + ['port', 'prometheus', '9090'], text=True).strip().rsplit(':', 1)[1]
        base = 'https://localhost:' + port
        environment = dict(os.environ, GOWORK='off', GOMAXPROCS='2', ADDP_POSTGRES_QUERY_INTEGRATION='1',
            MONITOR_PROMETHEUS_URL=base, MONITOR_PROMETHEUS_CA_FILE=str(work / 'tls/ca.crt'),
            MONITOR_PROMETHEUS_CLIENT_CERT_FILE=str(work / 'query-tls/client.crt'),
            MONITOR_PROMETHEUS_CLIENT_KEY_FILE=str(work / 'query-tls/client.key'),
            ADDP_POSTGRES_QUERY_RECORD=str(record_file))

        def check(mode):
            subprocess.run(['go', 'test', './internal/resourcequery', '-run', '^TestIntegrationMetricsPostgreSQLSource$',
                            '-count=1', '-timeout=180s', '-v'], cwd=ROOT / 'monitor/backend',
                           env=dict(environment, ADDP_POSTGRES_QUERY_EXPECT=mode), check=True, timeout=210)

        center_tls = ssl.create_default_context(cafile=str(work / 'tls/ca.crt'))
        center_tls.load_cert_chain(str(work / 'tls/health.crt'), str(work / 'tls/health.key'))
        try:
            self.publish_query_discovery(json.dumps([{'targets': ['postgres-metrics:9443'], 'labels': {
                '__scheme__': 'https', '__metrics_path__': '/metrics', 'addp_monitor_kind': 'engine_resources',
                'addp_source': 'postgresql', 'addp_deployment_id': deployment,
                'addp_database_container_id': facts['container_id'],
                'addp_database_container_started_at': facts['started_at']}}]))
            check('healthy')
            self.sql('SELECT pg_catalog.pg_stat_reset();')
            check('reset')
            self.sql(f'REVOKE EXECUTE ON FUNCTION addp_observation.snapshot() FROM {user};')
            try:
                check('sql-failed')
            finally:
                self.sql(f'GRANT EXECUTE ON FUNCTION addp_observation.snapshot() TO {user};')
            check('recovered')
            print('PostgreSQL query T2: twelve native metrics, current-container trends, summary, reset gaps and SQL failure/recovery passed', flush=True)
        finally:
            self.publish_query_discovery(original)
            deadline = time.monotonic() + 45
            while True:
                with urllib.request.urlopen(base + '/api/v1/targets', context=center_tls, timeout=5) as response:
                    data = json.load(response)
                if not any(target['labels'].get('addp_deployment_id') == deployment for target in data['data']['activeTargets']):
                    break
                if time.monotonic() >= deadline:
                    raise AssertionError('owned PostgreSQL target did not leave discovery')
                time.sleep(1)
            record_file.unlink(missing_ok=True)

    def directory_publication_diagnostic(self):
        """Counterbalanced observations; diagnostic success never releases HBA."""
        return {'trials':[self.directory_process_trial(('a','b'),'a'),
                          self.directory_process_trial(('c','d'),'b')]}

    def directory_process_trial(self, sides, enumerate_first):
        runtime = self.auth_directory/'runtime'
        paths = [runtime/('publication-process-'+side)/'item' for side in sides]
        deadline = time.monotonic()+8
        process = subprocess.Popen(self.compose+['exec','-T','postgres','bash','--noprofile','--norc','-c',
            DIRECTORY_PROCESS_SCRIPT,'owned-directory-observation',
            *['/etc/addp-postgres-auth/publication-process-'+side+'/item' for side in sides],enumerate_first],
            stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL)
        data = bytearray()
        with selectors.DefaultSelector() as selector:
            selector.register(process.stdout,selectors.EVENT_READ)
            def receive():
                remaining = deadline-time.monotonic()
                assert remaining>0 and selector.select(remaining), 'Owned directory process deadline reached'
                chunk = os.read(process.stdout.fileno(),4096)
                assert len(data)+len(chunk)<=8192, 'Owned directory process output exceeds budget'
                data.extend(chunk)
                return bool(chunk)
            try:
                while b'\n' not in data:
                    assert receive(), 'Owned directory process ended before baseline'
                # Validate the baseline before changing either test-owned file.
                baseline = bytes(data).split(b'\n',1)[0].decode('ascii').split('\t')
                assert len(baseline)==5 and baseline[0]=='baseline' and baseline[3:]==['before','before'], 'Owned directory baseline unavailable'
                for path in paths:
                    self.authentication._atomic(path,'after-version\n',0o644,previous='before\n')
                    assert self.authentication._read(path,0o644)[0]=='after-version\n'
                process.stdin.write(b'published\n');process.stdin.flush()
                while receive():
                    pass
                remaining = deadline-time.monotonic()
                assert remaining>0 and process.wait(timeout=remaining)==0, 'Owned directory process failed'
                return dict(directory_process_observations(bytes(data)),enumerated_first=enumerate_first)
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait(timeout=2)
                process.stdin.close();process.stdout.close()

    def check_directory_publication(self):
        target = self.auth_directory/'runtime/publication-probe'
        before, after = 'before\n', 'after-version\n'
        with self.authentication.locked(self.auth_directory):
            try:
                hba = self.auth_directory/'runtime/pg_hba.conf'
                original = self.authentication._read(hba,0o644)[0]
                assert self.read_hba()==original, 'HBA initially differs across the owned mount'
                assert self.authentication._read(target,0o644)[0]==before
                inode = target.stat().st_ino
                command = self.compose+['exec','-T','postgres','head','-c','64','/etc/addp-postgres-auth/publication-probe']
                first = subprocess.run(command,capture_output=True,text=True,timeout=5)
                assert first.returncode==0 and first.stdout==before, 'Owned control file initially unavailable'
                self.authentication._atomic(target,after,0o644,previous=before)
                result = subprocess.run(command,capture_output=True,text=True,timeout=5)
                facts = {'host_inode_changed':target.stat().st_ino!=inode,
                    'host_matches_replacement':self.authentication._read(target,0o644)[0]==after,
                    'container_read_ok':result.returncode==0,'container_return_code':result.returncode,
                    'container_matches_replacement':result.returncode==0 and result.stdout==after,
                    'hba_host_unchanged':self.authentication._read(hba,0o644)[0]==original}
                try:
                    facts['hba_container_unchanged']=self.read_hba()==original
                except (AssertionError,subprocess.SubprocessError):
                    facts['hba_container_unchanged']=False
                print('PostgreSQL directory publication '+json.dumps(facts),flush=True)
                if not facts['container_matches_replacement']:
                    try:
                        diagnostic = self.directory_publication_diagnostic()
                    except (OSError,ValueError,AssertionError,subprocess.SubprocessError) as error:
                        diagnostic = {'unavailable':type(error).__name__}
                    print('PostgreSQL directory comparison '+json.dumps(diagnostic),flush=True)
                assert all(value is True for key,value in facts.items() if key!='container_return_code'), \
                    'Owned directory does not present an atomic host replacement; HBA acceptance not reached'
            except BaseException as error:
                try:
                    target.unlink(missing_ok=True)
                    for side in ('a','b','c','d'):
                        directory = target.parent/('publication-process-'+side)
                        (directory/'item').unlink(missing_ok=True)
                        if directory.exists():directory.rmdir()
                except BaseException as cleanup:
                    error.add_note('Owned publication cleanup failed: '+type(cleanup).__name__)
                raise
            else:
                target.unlink()
                for side in ('a','b','c','d'):
                    directory = target.parent/('publication-process-'+side)
                    (directory/'item').unlink(missing_ok=True)
                    if directory.exists():directory.rmdir()

    def run(self):
        if self.auth_directory is not None:
            self.check_directory_publication()
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
