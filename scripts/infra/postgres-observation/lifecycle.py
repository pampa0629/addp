"""Deployment-only PostgreSQL observation installation and stateless sources."""
import argparse
import base64
from contextlib import contextmanager
import hashlib
import hmac
import importlib.util
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import sys
import time
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[3]


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


auth = load('postgres_auth_lifecycle', Path(__file__).with_name('authentication.py'))
source = auth.source
inventory = load('postgres_deployment_inventory', ROOT / 'scripts/utils/deployment-inventory.py')
SQL = Path(__file__).with_name('000002_lifecycle.sql')
OWNER = 'io.addp.postgres-metrics.owner'
SCOPE = 'io.addp.postgres-metrics.scope'
group = inventory.metrics_project
ACTIVATION_STAGES = frozenset({'session-check', 'transport-check', 'private-state', 'authentication-read',
    'account-install', 'authentication-update', 'account-enable', 'connection-check',
    'source-files', 'identity-recheck', 'source-start', 'source-health'})



deployment = load('postgres_deployment_transport', ROOT / 'scripts/utils/deployment-docker.py')
Docker = deployment.Docker
LifecycleError = deployment.DeploymentError


def verifier(password):
    if not re.fullmatch('[0-9a-f]{64}', password):
        raise LifecycleError('Invalid private observation credential')
    salt = secrets.token_bytes(16)
    salted = hashlib.pbkdf2_hmac('sha256', password.encode(), salt, 4096)
    stored = hashlib.sha256(hmac.digest(salted, b'Client Key', 'sha256')).digest()
    server = hmac.digest(salted, b'Server Key', 'sha256')
    encoded = lambda value: base64.b64encode(value).decode('ascii')
    return f'SCRAM-SHA-256$4096:{encoded(salt)}${encoded(stored)}:{encoded(server)}'


def install_input(scope, password, enabled):
    if scope not in ('infra', 'business') or type(enabled) is not bool:
        raise LifecycleError('Invalid observation installation intent')
    return (f"\\set observer_scope '{scope}'\n\\set observer_enabled '{str(enabled).lower()}'\n"
            + "\\set observer_verifier '" + verifier(password) + "'\n" + SQL.read_text())


def recovery_verification(scope):
    """Fixed catalog-only predicate, evaluated as the existing observer login."""
    if scope not in ('infra', 'business'):
        raise LifecycleError('Invalid observation verification scope')
    body = SQL.read_text().split('$snapshot$')[1]
    body_hash = hashlib.md5(body.encode()).hexdigest()
    return f"""WITH r AS (SELECT * FROM pg_catalog.pg_roles WHERE rolname=current_user),
 o AS (SELECT * FROM pg_catalog.pg_roles WHERE rolname='addp_observation_owner'),
 n AS (SELECT * FROM pg_catalog.pg_namespace WHERE nspname='addp_observation'),
 p AS (SELECT * FROM pg_catalog.pg_proc WHERE oid=pg_catalog.to_regprocedure('addp_observation.snapshot()'))
SELECT current_user='addp_{scope}_observer'
 AND pg_catalog.current_setting('server_version_num')::integer BETWEEN 150000 AND 159999
 AND r.rolcanlogin AND NOT r.rolinherit AND NOT r.rolsuper AND NOT r.rolcreatedb
 AND NOT r.rolcreaterole AND NOT r.rolreplication AND NOT r.rolbypassrls AND r.rolconnlimit=2
 AND pg_catalog.shobj_description(r.oid,'pg_authid')='addp.postgres-observation/v1:{scope}:reader'
 AND r.rolconfig @> ARRAY['default_transaction_read_only=on','statement_timeout=3s',
   'idle_in_transaction_session_timeout=5s','search_path=pg_catalog, pg_temp']
 AND NOT o.rolcanlogin AND o.rolinherit AND NOT o.rolsuper AND NOT o.rolcreatedb
 AND NOT o.rolcreaterole AND NOT o.rolreplication AND NOT o.rolbypassrls
 AND pg_catalog.shobj_description(o.oid,'pg_authid')='addp.postgres-observation/v1:{scope}:owner'
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m WHERE m.member=r.oid OR m.roleid IN(r.oid,o.oid)
   OR (m.member=o.oid AND (m.roleid<>'pg_read_all_stats'::regrole OR m.admin_option)))
 AND EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m WHERE m.member=o.oid AND m.roleid='pg_read_all_stats'::regrole)
 AND n.nspowner=o.oid AND pg_catalog.obj_description(n.oid,'pg_namespace')='addp.postgres-observation/v1:{scope}'
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_class WHERE relnamespace=n.oid)
 AND (SELECT count(*) FROM pg_catalog.pg_proc WHERE pronamespace=n.oid)=1
 AND p.pronamespace=n.oid AND p.proowner=o.oid AND p.pronargs=0 AND p.prosecdef AND p.provolatile='s'
 AND p.prolang=(SELECT oid FROM pg_catalog.pg_language WHERE lanname='plpgsql')
 AND pg_catalog.md5(p.prosrc)='{body_hash}'
 AND p.proconfig @> ARRAY['search_path=pg_catalog, pg_temp','stats_fetch_consistency=snapshot']
 AND cardinality(p.proconfig)=3 AND EXISTS(SELECT 1 FROM unnest(p.proconfig) c WHERE lower(c)='timezone=utc')
 AND p.proallargtypes=ARRAY[1184,1184,25,16,20,20,20,20,20,20,20,23,20,20,20,20]::oid[]
 AND p.proargnames=ARRAY['observed_at','server_started_at','statistics_generation','track_counts_enabled',
 'connections','active_connections','idle_connections','idle_in_transaction_connections','active_waiting_connections',
 'active_lock_waiting_connections','active_io_waiting_connections','max_connections','transactions_committed',
 'transactions_rolled_back','deadlocks','database_bytes']
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.aclexplode(COALESCE(n.nspacl,pg_catalog.acldefault('n',n.nspowner))) a
 WHERE a.grantee<>o.oid AND (a.grantee<>r.oid OR a.privilege_type<>'USAGE' OR a.is_grantable))
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.aclexplode(COALESCE(p.proacl,pg_catalog.acldefault('f',p.proowner))) a
 WHERE a.grantee<>o.oid AND (a.grantee<>r.oid OR a.privilege_type<>'EXECUTE' OR a.is_grantable))
 AND NOT pg_catalog.has_database_privilege(current_user,current_database(),'CREATE')
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_namespace a WHERE a.nspname !~ '^pg_'
 AND a.nspname NOT IN('information_schema','addp_observation') AND pg_catalog.has_schema_privilege(current_user,a.oid,'CREATE'))
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace a ON a.oid=c.relnamespace
 WHERE a.nspname !~ '^pg_' AND a.nspname<>'information_schema' AND CASE WHEN c.relkind='S' THEN
 pg_catalog.has_sequence_privilege(current_user,c.oid,'USAGE,SELECT,UPDATE') ELSE false END)
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace a ON a.oid=c.relnamespace
 WHERE a.nspname !~ '^pg_' AND a.nspname<>'information_schema' AND CASE WHEN c.relkind IN('r','p','v','m','f') THEN
 pg_catalog.has_table_privilege(current_user,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')
 OR pg_catalog.has_any_column_privilege(current_user,c.oid,'SELECT,INSERT,UPDATE,REFERENCES') ELSE false END)
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_proc f JOIN pg_catalog.pg_namespace a ON a.oid=f.pronamespace
 WHERE a.nspname !~ '^pg_' AND a.nspname NOT IN('information_schema','addp_observation')
 AND f.prosecdef AND pg_catalog.has_function_privilege(current_user,f.oid,'EXECUTE'))
FROM r,o,n,p;"""



class ObservationDatabase:
    """Owned container/network facts and observer-only checks; no admin SQL."""
    def __init__(self, docker, project, working_dir, scope, *, maintenance='postgres'):
        if (scope not in ('infra', 'business')
                or not re.fullmatch('[a-zA-Z0-9_]{1,63}', maintenance)):
            raise LifecycleError('Invalid deployment PostgreSQL identity')
        self.docker, self.scope, self.maintenance = docker, scope, maintenance
        self.project, self.working_dir = project, str(Path(working_dir).resolve())
        identities = docker.run(['ps', '-aq', '--no-trunc', '--filter', 'label=com.docker.compose.project='+project,
                                 '--filter', 'label=com.docker.compose.service=postgres']).split()
        if len(identities) != 1 or not re.fullmatch('[0-9a-f]{64}', identities[0]):
            raise LifecycleError('Owned PostgreSQL is not uniquely available')
        self.identity = identities[0]
        self.facts = docker.fact(self.identity)
        labels = self.facts['labels']
        if (not self.facts['running'] or labels.get('com.docker.compose.project') != project
                or labels.get('com.docker.compose.service') != 'postgres'
                or labels.get('com.docker.compose.project.working_dir') != self.working_dir
                or labels.get('com.docker.compose.oneoff', '').lower() != 'false'):
            raise LifecycleError('PostgreSQL container ownership mismatch')
        if len(self.facts['networks']) != 1:
            raise LifecycleError('PostgreSQL requires one unambiguous deployment network')
        self.network, attachment = next(iter(self.facts['networks'].items()))
        network = json.loads(docker.run(['network', 'inspect', attachment['NetworkID']]))[0]
        if (network['Labels'].get('com.docker.compose.project') != project
                or network['Driver'] != 'bridge' or network['Name'] != self.network):
            raise LifecycleError('PostgreSQL network ownership mismatch')
        self.address = attachment['IPAddress'] or attachment['GlobalIPv6Address']
        self.networks = ['127.0.0.1/32', '::1/128'] + [item['Subnet'] for item in network['IPAM']['Config']]
        source.authentication_configuration('', scope, self.networks, database=maintenance)
        source.configuration(scope, self.address, 5432, '0' * 64, database=maintenance)

    def authenticate(self, password):
        user = f'addp_{self.scope}_observer'
        path = '/tmp/addp-observer-check-' + secrets.token_hex(16)
        self.docker.run(['exec', '-i', self.identity, 'sh', '-c', 'umask 077; cat > "$1"',
                         'observation-password', path], input=f'127.0.0.1:5432:*:{user}:{password}\n')
        try:
            args = ['exec', '-i', '-e', 'PGPASSFILE='+path, '-e', 'PGCONNECT_TIMEOUT=3', self.identity,
                    'psql', '-X', '-q', '-A', '-t', '-w', '-v', 'ON_ERROR_STOP=1', '-U', user, '-h', '127.0.0.1', '-d']
            verified = self.docker.run(args + [self.maintenance], input=recovery_verification(self.scope), timeout=8)
            if verified.strip() != 't':
                raise LifecycleError('PostgreSQL observation installation drift; use the standard deployment entry')
            value = self.docker.run(args + [self.maintenance], input='SELECT count(*) FROM addp_observation.snapshot();', timeout=8)
            if value.strip() != '1':
                raise LifecycleError('PostgreSQL observation authentication failed')
            other = 'postgres' if self.maintenance != 'postgres' else 'template1'
            result = self.docker.run(args + [other], input='SELECT 1;', timeout=8, check=False)
            if result.returncode == 0 or 'pg_hba.conf rejects connection' not in result.stderr:
                raise LifecycleError('PostgreSQL cross-database authentication boundary failed')
        finally:
            self.docker.run(['exec', self.identity, 'rm', '-f', path])


class Database(ObservationDatabase):
    """Explicit installer only; automatic recovery never constructs this class."""
    def __init__(self, docker, project, working_dir, scope, admin, *, maintenance='postgres', authentication=None):
        if not re.fullmatch('[a-zA-Z_][a-zA-Z0-9_]{0,62}', admin or ''):
            raise LifecycleError('Invalid deployment PostgreSQL administrator')
        self.admin, self.authentication = admin, authentication
        super().__init__(docker, project, working_dir, scope, maintenance=maintenance)
        self.settings = json.loads(self.sql("""SELECT json_build_object(
 'version', current_setting('server_version_num')::integer,
 'data', current_setting('data_directory'), 'hba', current_setting('hba_file'),
 'superuser', (SELECT rolsuper FROM pg_catalog.pg_roles WHERE rolname=current_user));"""))
        if not self.settings['superuser'] or not 150000 <= self.settings['version'] <= 159999:
            raise LifecycleError('Observation installation requires deployment administrator and PostgreSQL 15')
        data = self.settings['data']
        if not re.fullmatch('/[a-zA-Z0-9_/-]+', data) or '..' in Path(data).parts:
            raise LifecycleError('Unsupported PostgreSQL data directory')
        if authentication is None:
            if self.settings['hba'] != data + '/pg_hba.conf':
                raise LifecycleError('PostgreSQL authentication path is not deployment-owned')
        else:
            expected = Path(authentication).resolve() / 'runtime'
            if (self.settings['hba'] != '/etc/addp-postgres-auth/pg_hba.conf'
                    or not any(item['Type'] == 'bind' and item['Destination'] == '/etc/addp-postgres-auth'
                               and Path(item['Source']).resolve() == expected and item['RW'] is False for item in self.facts['mounts'])):
                raise LifecycleError('Business authentication directory mount requires standard deployment')

    def sql(self, value):
        return self.docker.run(['exec', '-i', '-e',
            'PGOPTIONS=-c statement_timeout=5000 -c lock_timeout=2000 -c log_statement=none -c log_min_duration_statement=-1 -c log_min_error_statement=panic',
            self.identity, 'psql', '-X', '-q', '-A', '-t', '-w', '-v', 'ON_ERROR_STOP=1',
            '-h', '/var/run/postgresql', '-U', self.admin, '-d', self.maintenance],
            input='\\set VERBOSITY sqlstate\n' + value, timeout=20).strip()

    def no_sessions(self):
        if self.sql("SELECT count(*) FROM pg_catalog.pg_stat_activity WHERE usename IN ('addp_infra_observer','addp_business_observer');") != '0':
            raise LifecycleError('Existing observation sessions prevent activation; no sessions terminated')

    def disable(self):
        # Disabling is not installation: no password, grant or function changes.
        self.sql(f"""DO $disable$ DECLARE r record; BEGIN
 PERFORM pg_catalog.pg_advisory_xact_lock(1728330291,1);
 SELECT oid, rolsuper, rolcreatedb, rolcreaterole, rolreplication, rolbypassrls INTO r
 FROM pg_catalog.pg_roles WHERE rolname='addp_{self.scope}_observer';
 IF FOUND THEN
   IF r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls
      OR pg_catalog.shobj_description(r.oid,'pg_authid') IS DISTINCT FROM 'addp.postgres-observation/v1:{self.scope}:reader' THEN
     RAISE EXCEPTION 'observation identity conflict';
   END IF;
   ALTER ROLE addp_{self.scope}_observer NOLOGIN;
 END IF; END $disable$;""")

    @contextmanager
    def locked(self):
        token = secrets.token_hex(16)
        lock = self.settings['data'] + '/.addp-observation-control'
        self.docker.run(['exec', '-i', self.identity, 'sh', '-c',
                         'set -eu; umask 077; mkdir "$1"; printf %s "$2" > "$1/owner"',
                         'observation-lock', lock, token])
        try:
            yield
        finally:
            self.docker.run(['exec', self.identity, 'sh', '-c',
                'set -eu; test "$(cat "$1/owner")" = "$2"; rm "$1/owner"; rmdir "$1"',
                'observation-unlock', lock, token])

    def read_hba(self):
        result = self.docker.run(['exec', '-e', 'LC_ALL=C', self.identity, 'sh', '-c', '''set -eu
test ! -L "$1" || exit 41
if test -e "$1"; then test -f "$1" || exit 42; fi
head -c 262145 "$1" || exit 45''', 'observation-hba', self.settings['hba']], check=False)
        if result.returncode:
            reason = {41: 'symbolic link', 42: 'not a regular file',
                      45: 'content read failed'}.get(result.returncode, 'container command failed')
            if result.returncode == 45 and 'No such file' in result.stderr:
                reason = 'not visible'
            raise LifecycleError('PostgreSQL authentication read rejected: ' + reason)
        if len(result.stdout.encode('utf-8')) > source.HBA_LIMIT:
            raise LifecycleError('PostgreSQL authentication read rejected: size budget exceeded')
        return result.stdout

    def write_hba(self, expected, value):
        if self.authentication is not None:
            auth.replace(self.authentication, expected, value)
        else:
            # All product writers hold the database lifecycle lock. Compare the
            # input again before rename; never claim CAS against outside writers.
            self.docker.run(['exec', '-i', self.identity, 'sh', '-c', '''set -eu
target="$1"
test ! -L "$target" && test -f "$target"
test "$(sha256sum "$target" | cut -d ' ' -f 1)" = "$2"
temporary=$(mktemp "$target.addp.XXXXXX")
trap 'rm -f "$temporary"' EXIT
cat > "$temporary"
chown --reference="$target" "$temporary"
chmod --reference="$target" "$temporary"
test "$(sha256sum "$target" | cut -d ' ' -f 1)" = "$2"
mv "$temporary" "$target"
trap - EXIT''', 'observation-hba', self.settings['hba'], auth.digest(expected)], input=value)
        if self.read_hba() != value or self.sql('SELECT count(*) FROM pg_catalog.pg_hba_file_rules WHERE error IS NOT NULL;') != '0':
            raise LifecycleError('PostgreSQL authentication syntax validation failed')
        before = self.sql('SELECT pg_catalog.pg_conf_load_time();')
        if self.sql('SELECT pg_catalog.pg_reload_conf();') != 't':
            raise LifecycleError('PostgreSQL authentication reload failed')
        deadline = time.monotonic() + 3
        while self.sql('SELECT pg_catalog.pg_conf_load_time();') == before:
            if time.monotonic() >= deadline:
                raise LifecycleError('PostgreSQL authentication reload timed out')
            time.sleep(.05)



def state_directory(env, scope):
    value = env.get('ADDP_POSTGRES_METRICS_STATE_DIR')
    parent = Path(value) if value else Path.home().resolve() / '.local/state/addp' / hashlib.sha256(str(ROOT).encode()).hexdigest()[:16] / 'postgres-metrics'
    if not parent.is_absolute() or '..' in parent.parts or parent.resolve().is_relative_to(ROOT):
        raise LifecycleError('PostgreSQL metrics state requires a repository-external absolute directory')
    return parent / scope


def read_json(path, mode=0o600):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise LifecycleError('Duplicate PostgreSQL source state field')
            result[key] = value
        return result
    return json.loads(auth._read(path, mode)[0], object_pairs_hook=unique)


def file_write(path, value, mode):
    previous = auth._read(path, mode)[0] if os.path.lexists(path) else None
    auth._atomic(path, value, mode, previous=previous)


def transport(env):
    listen = env.get('ADDP_POSTGRES_METRICS_LISTEN', '')
    match = re.fullmatch(r'(\[[0-9a-f:]+\]|[0-9.]+):([1-9][0-9]{0,4})', listen)
    if not match:
        raise LifecycleError('PostgreSQL source requires an explicit IP and port')
    ip = ipaddress.ip_address(match[1].strip('[]'))
    port = int(match[2])
    canonical = f'[{ip}]:{port}' if ip.version == 6 else f'{ip}:{port}'
    if (listen != canonical or port > 65535 or ip.is_unspecified or ip.is_multicast or ip.is_link_local
            or not any(ip.version == network.version and ip in network for network in source.HBA_NETWORKS)):
        raise LifecycleError('PostgreSQL source publication is outside the local private boundary')
    directory = Path(env.get('ADDP_POSTGRES_METRICS_TLS_DIR', ''))
    names = {'ca.crt', 'server.crt', 'server.key'}
    if (not directory.is_absolute() or directory.resolve().is_relative_to(ROOT) or directory.is_symlink()
            or not directory.is_dir() or {path.name for path in directory.iterdir()} != names):
        raise LifecycleError('PostgreSQL source TLS requires an external three-file directory')
    for name in names:
        path = directory / name
        if path.is_symlink() or not path.is_file() or not 0 < path.stat().st_size <= source.HBA_LIMIT:
            raise LifecycleError('Invalid PostgreSQL source certificate material')
    try:
        subprocess.run(['openssl', 'verify', '-CAfile', str(directory/'ca.crt'), '-purpose', 'sslserver',
                        '-verify_ip', str(ip), str(directory/'server.crt')], check=True, capture_output=True, timeout=5)
        public = subprocess.run(['openssl', 'x509', '-in', str(directory/'server.crt'), '-pubkey', '-noout'],
                                check=True, capture_output=True, timeout=5).stdout
        private = subprocess.run(['openssl', 'pkey', '-in', str(directory/'server.key'), '-passin', 'pass:', '-pubout'],
                                 check=True, capture_output=True, timeout=5).stdout
        if public != private:
            raise LifecycleError('PostgreSQL source certificate key mismatch')
    except (OSError, subprocess.SubprocessError):
        raise LifecycleError('PostgreSQL source certificate verification failed') from None
    return directory, str(ip), port


class SourceLifecycle:
    def __init__(self, docker, scope, state, database_factory, *, project=None):
        if scope not in ('infra', 'business'):
            raise LifecycleError('Unsupported PostgreSQL observation scope')
        self.docker, self.scope, self.state, self.database_factory = docker, scope, Path(state), database_factory
        self.project = project or group.PROJECT
        self.exporter = group.service('postgres', scope)
        self.web = self.exporter+'-web'
        self.services = {self.exporter, self.web}

    def source_containers(self):
        identities = group.container_ids(self.docker.run,self.project,self.services)
        selected = []
        for identity in identities:
            fact = self.docker.fact(identity)
            labels = fact['labels']
            if labels.get('com.docker.compose.service') not in self.services:
                raise LifecycleError('Unexpected PostgreSQL source service')
            if (labels.get(OWNER) != str(ROOT) or labels.get(SCOPE) != self.scope
                    or labels.get('com.docker.compose.project') != self.project
                    or labels.get('com.docker.compose.oneoff', '').lower() != 'false'):
                raise LifecycleError('Refusing foreign PostgreSQL source containers')
            selected.append((labels['com.docker.compose.service'], identity))
        return sorted(selected, reverse=True)

    def stop_sources(self):
        selected = self.source_containers()
        for _, identity in selected:
            self.docker.run(['rm', '-f', identity], timeout=20)
        return bool(selected)

    def down(self):
        if not self.source_containers() and not (self.state / 'owner.json').exists():
            return
        with auth.locked(self.state):
            try:
                database = self.database_factory()
            except Exception:
                self.stop_sources()
                raise
            with database.locked():
                self.stop_sources()
                database.disable()
                database.no_sessions()

    def source_document(self, database, ip, port, *, started_at=None):
        labels = {OWNER: str(ROOT), SCOPE: self.scope,
                  'io.addp.postgres-metrics.database-id': database.identity,
                  'io.addp.postgres-metrics.database-started-at': started_at or database.facts['started_at']}
        template = str(ROOT / 'scripts/infra/postgres-metrics.yml')
        compose = {'name': self.project, 'services': {
            self.exporter: {'extends': {'file': template, 'service': 'postgres-metrics'},
                'labels': labels, 'networks': ['database'],
                'ports': [{'host_ip': ip, 'published': str(port), 'target': 9443}]},
            self.web: {'extends': {'file': template, 'service': 'postgres-metrics-web'},
                'network_mode':'service:'+self.exporter,'depends_on':[self.exporter], 'labels': labels}},
            'networks': {'database': {'external': True, 'name': database.network}}}
        return compose


    def start_sources(self, database, tls, ip, port, password):
        stage = 'source-files'
        try:
            runtime = self.state / 'runtime'
            auth._directory(runtime, 0o755)
            for name in ('source', 'web'):
                auth._directory(runtime / name, 0o755)
            file_write(runtime / 'source/sql-exporter.yml', json.dumps(source.configuration(
                self.scope, database.address, 5432, password, database=database.maintenance)), 0o644)
            for name in ('ca.crt', 'server.crt', 'server.key'):
                file_write(runtime / 'web' / name, (tls / name).read_bytes().decode('utf-8'), 0o644)
            compose = self.source_document(database, ip, port)
            compose_file = self.state / 'compose.json'
            file_write(compose_file, json.dumps(compose), 0o600)
            stage = 'identity-recheck'
            if self.docker.fact(database.identity) != database.facts:
                raise LifecycleError('PostgreSQL source facts changed during installation')
            uid, gid = (os.getuid(), os.getgid()) if os.getuid() else (65534, 65534)
            stage = 'source-start'
            self.docker.run(['compose', '--env-file', '/dev/null', '-p', self.project, '-f', str(compose_file),
                             'up', '-d', '--force-recreate', '--no-deps', self.exporter, self.web],
                            timeout=180, extra_env={'COMPOSE_IGNORE_ORPHANS':'true','ADDP_POSTGRES_METRICS_USER': f'{uid}:{gid}',
                            'ADDP_POSTGRES_METRICS_SOURCE_DIR': str(runtime/'source'),
                            'ADDP_POSTGRES_METRICS_WEB_DIR': str(runtime/'web')})
            # A running source is not evidence of a successful scrape.
            stage = 'source-health'
            time.sleep(.5)
            sources = self.docker.run(['ps', '-q', '--filter', 'label=com.docker.compose.project='+self.project,
                                       '--filter', 'label='+OWNER+'='+str(ROOT), '--filter', 'label='+SCOPE+'='+self.scope]).split()
            if len(sources) != 2 or any(not self.docker.fact(identity)['running'] for identity in sources):
                raise LifecycleError('PostgreSQL source containers did not stay running')
        except Exception as error:
            error.observation_stage = stage
            raise

    def recover(self, env):
        """Recover a previously installed source, without installer capabilities."""
        with auth.locked(self.state):
            database = self.database_factory()
            expected_owner = {'schema': 'addp.postgres-metrics/v1', 'workspace': str(ROOT), 'scope': self.scope,
                              'source_key': {'docker_engine_id': self.docker.info['id'], 'project': database.project, 'service': 'postgres'}}
            if read_json(self.state / 'owner.json') != expected_owner:
                raise LifecycleError('PostgreSQL recovery ownership mismatch; use the standard deployment entry')
            tls, ip, port = transport(env)
            document = read_json(self.state / 'compose.json')
            if (not isinstance(document, dict) or document.get('name') != self.project
                    or not isinstance(document.get('services'), dict) or set(document['services']) != self.services):
                error = LifecycleError('PostgreSQL source layout changed; explicitly apply the standard up entry')
                error.source_recovery_reason = 'source-layout-changed'
                raise error
            previous = document['services'][self.exporter]['labels']
            old_start = previous['io.addp.postgres-metrics.database-started-at']
            if (previous.get('io.addp.postgres-metrics.database-id') != database.identity
                    or not isinstance(old_start, str) or not re.fullmatch(r'\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z', old_start)
                    or document != self.source_document(database, ip, port, started_at=old_start)):
                raise LifecycleError('PostgreSQL recovery declaration changed; use the standard deployment entry')
            password = auth._read(self.state / 'password', 0o600)[0]
            if not re.fullmatch('[0-9a-f]{64}', password):
                raise LifecycleError('Invalid private observation credential')
            runtime = self.state / 'runtime'
            for directory in (runtime, runtime/'source', runtime/'web'):
                if not directory.is_dir():
                    raise LifecycleError('PostgreSQL recovery installation is incomplete')
                auth._directory(directory, 0o755)
            installed = read_json(runtime / 'source/sql-exporter.yml', 0o644)
            old_host = urlsplit(installed['target']['data_source_name']).hostname
            # Only a prior canonical fixed configuration can authorize reuse.
            ipaddress.ip_address(old_host)
            if installed != source.configuration(self.scope, old_host, 5432, password, database=database.maintenance):
                raise LifecycleError('PostgreSQL recovery source configuration drift')
            for name in ('ca.crt', 'server.crt', 'server.key'):
                if auth._read(runtime/'web'/name, 0o644)[0] != (tls/name).read_text():
                    raise LifecycleError('PostgreSQL recovery TLS changed; use the standard deployment entry')
            database.authenticate(password)
            selected = self.source_containers()
            facts = {service: self.docker.fact(identity) for service, identity in selected}
            if len(facts) != len(selected):
                raise LifecycleError('Duplicate PostgreSQL recovery sources')
            for fact in facts.values():
                if any(fact['labels'].get(key) != value for key, value in previous.items()):
                    raise LifecycleError('PostgreSQL recovery source ownership drift')
            healthy = (set(facts) == self.services and all(fact['running'] for fact in facts.values())
                       and old_start == database.facts['started_at'] and old_host == database.address
                       and facts[self.web]['network_mode'] == 'container:'+facts[self.exporter]['id']
                       and facts[self.exporter]['network_mode'] == database.network
                       and facts[self.exporter]['ports'] == {'9443/tcp': [{'HostIp': ip, 'HostPort': str(port)}]}
                       and not facts[self.web]['ports'])
            if self.docker.fact(database.identity) != database.facts:
                raise LifecycleError('PostgreSQL recovery database facts changed')
            if healthy:
                return 'healthy; source unchanged'
            # All prior files, observer checks and immutable container identities
            # were validated before removing any source. Never start a database.
            try:
                for _, identity in selected:
                    self.docker.run(['rm', '-f', identity], timeout=20)
                self.start_sources(database, tls, ip, port, password)
                database.authenticate(password)
                if self.docker.fact(database.identity) != database.facts:
                    raise LifecycleError('PostgreSQL recovery database facts changed')
            except Exception as error:
                try:
                    self.stop_sources()
                except Exception:
                    error.observation_cleanup_failed = True
                raise
            return 'source recovered; successful scraping requires Monitor verification'

    def up(self, env):
        with auth.locked(self.state):
            if self.project == group.PROJECT:
                self.source_containers()
                group.retire(self.docker, ROOT, 'postgres', self.scope)
            self.stop_sources()
            database = self.database_factory()
            with database.locked():
                original = replacement = None
                installed = False
                stage = 'session-check'
                try:
                    database.no_sessions()
                    database.disable()
                    stage = 'transport-check'
                    tls, ip, port = transport(env)
                    stage = 'private-state'
                    owner = {'schema': 'addp.postgres-metrics/v1', 'workspace': str(ROOT), 'scope': self.scope,
                             'source_key': {'docker_engine_id': self.docker.info['id'], 'project': database.project, 'service': 'postgres'}}
                    owner_file = self.state / 'owner.json'
                    if os.path.lexists(owner_file):
                        if read_json(owner_file) != owner:
                            raise LifecycleError('PostgreSQL source state ownership mismatch')
                    else:
                        if any(path.name != 'control.lock' for path in self.state.iterdir()):
                            raise LifecycleError('Unowned PostgreSQL source state')
                        file_write(owner_file, json.dumps(owner), 0o600)
                    password_file = self.state / 'password'
                    if not os.path.lexists(password_file):
                        if database.sql(f"SELECT count(*) FROM pg_catalog.pg_roles WHERE rolname='addp_{self.scope}_observer';") != '0':
                            raise LifecycleError('Existing observation role has no private credential; automatic reset refused')
                        file_write(password_file, secrets.token_hex(32), 0o600)
                    password = auth._read(password_file, 0o600)[0]
                    verifier(password)
                    stage = 'authentication-read'
                    original = database.read_hba()
                    replacement = source.authentication_configuration(original, self.scope, database.networks, database=database.maintenance)
                    stage = 'account-install'
                    database.sql(install_input(self.scope, password, False))
                    stage = 'authentication-update'
                    database.write_hba(original, replacement)
                    database.no_sessions()
                    stage = 'account-enable'
                    database.sql(install_input(self.scope, password, True))
                    installed = True
                    stage = 'connection-check'
                    database.authenticate(password)
                    stage = 'source-files'
                    self.start_sources(database, tls, ip, port, password)
                except Exception as error:
                    # Retain the original failure for callers; publish only a
                    # closed stage, never raw SQL, HBA, paths or subprocess output.
                    error.observation_stage = getattr(error, 'observation_stage', stage)
                    try:
                        self.stop_sources()
                        database.disable()
                        if not installed and original is not None and replacement is not None and database.read_hba() == replacement:
                            database.write_hba(replacement, original)
                    except Exception:
                        error.observation_cleanup_failed = True
                    raise


def run(action, scope, env):
    flag = env.get('ADDP_POSTGRES_METRICS_ENABLED', 'false')
    state = state_directory(env, scope)
    if action == 'recover':
        if flag == 'false':
            print('PostgreSQL metrics '+scope+': recovery not selected')
            return
        if flag != 'true':
            raise LifecycleError('ADDP_POSTGRES_METRICS_ENABLED must be true or false')
        if any(path.is_symlink() for path in (state, *state.parents)):
            raise LifecycleError('Linked PostgreSQL recovery state')
        if not state.exists() or not any(state.iterdir()):
            print('PostgreSQL metrics '+scope+': not installed; recovery skipped')
            return
    docker = Docker(env)
    project, working = inventory.SCOPES[scope]

    def database():
        return Database(docker, project, working, scope, env.get('POSTGRES_USER', 'business' if scope == 'business' else ''),
                        authentication=auth.state_directory(env) if scope == 'business' else None)

    if action == 'recover':
        manager = SourceLifecycle(docker, scope, state, lambda: ObservationDatabase(docker, project, working, scope))
        print('PostgreSQL metrics '+scope+': '+manager.recover(env))
        return
    manager = SourceLifecycle(docker, scope, state, database)
    if action == 'down' or flag == 'false':
        manager.down()
        print('PostgreSQL metrics '+scope+': Disabled; source stopped, owned account disabled when deployed')
    elif flag == 'true':
        try:
            manager.up(env)
        except Exception:
            manager.stop_sources()
            raise
        print('PostgreSQL metrics '+scope+': source deployed; successful scraping requires Monitor verification')
    else:
        manager.down()
        raise LifecycleError('ADDP_POSTGRES_METRICS_ENABLED must be true or false')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['up', 'down', 'recover'])
    parser.add_argument('--scope', choices=['infra', 'business'], required=True)
    args = parser.parse_args()
    try:
        run(args.action, args.scope, os.environ)
    except (ValueError, OSError, KeyError, TypeError) as error:
        reason = str(error) if isinstance(error, (LifecycleError, auth.AuthenticationError)) else 'deployment ownership, authentication or source configuration failed'
        stage = getattr(error, 'observation_stage', None)
        detail = ' [stage=' + stage + ']' if stage in ACTIVATION_STAGES else ''
        if getattr(error, 'source_recovery_reason', '') == 'source-layout-changed':
            detail += ' [reason=source-layout-changed]'
        if isinstance(error, OSError) and type(error.errno) is int:
            detail += ' [errno=' + str(error.errno) + ']'
        if getattr(error, 'observation_cleanup_failed', False) is True:
            detail += ' [cleanup=failed]'
        print('PostgreSQL metrics unavailable' + detail + ': ' + reason + '; core services remain independent', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
