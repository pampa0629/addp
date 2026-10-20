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
SERVICES = {'postgres-metrics', 'postgres-metrics-web'}
FACT = ('{"id":{{json .Id}},"running":{{json .State.Running}},'
        '"labels":{{json .Config.Labels}},"networks":{{json .NetworkSettings.Networks}},'
        '"mounts":{{json .Mounts}}}')


class LifecycleError(ValueError):
    """Closed diagnostic; raw command output is never operator-facing."""


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


class Docker:
    def __init__(self, env):
        self.env = {key: env[key] for key in ('PATH', 'HOME', 'DOCKER_CONFIG', 'DOCKER_CONTEXT',
                    'LANG', 'LC_ALL', 'TMPDIR') if key in env}
        self.context = None
        _, self.info, _ = inventory.local_docker(env=env, reader=self._discovery)

    def _discovery(self, *args):
        value = self.run(list(args), raw=True)
        if args == ('context', 'show'):
            self.context = value.strip()
        return value.strip()

    def run(self, args, *, input=None, timeout=15, check=True, raw=False, extra_env=None):
        prefix = [] if raw else ['--context', self.context]
        environment = dict(self.env, **(extra_env or {}))
        try:
            result = subprocess.run(['docker', *prefix, *args], input=input, capture_output=True,
                                    text=True, timeout=timeout, env=environment)
        except (OSError, subprocess.SubprocessError):
            raise LifecycleError('PostgreSQL source command unavailable or timed out') from None
        if len(result.stdout.encode()) > 1024 * 1024 or len(result.stderr.encode()) > 1024 * 1024:
            raise LifecycleError('PostgreSQL source response exceeds budget')
        if check and result.returncode:
            raise LifecycleError('PostgreSQL source command failed')
        return result.stdout if check else result

    def fact(self, identity):
        value = json.loads(self.run(['inspect', '--format', FACT, identity]))
        # Docker does not promise mount-array ordering between inspections.
        # Preserve every mount property while comparing the unordered facts.
        value['mounts'] = sorted(value['mounts'], key=lambda item: json.dumps(item, sort_keys=True))
        return value


class Database:
    """Owned PG15 transport. No environment, application row or SQL discovery."""
    def __init__(self, docker, project, working_dir, scope, admin, *, maintenance='postgres', authentication=None):
        if (scope not in ('infra', 'business') or not re.fullmatch('[a-zA-Z_][a-zA-Z0-9_]{0,62}', admin or '')
                or not re.fullmatch('[a-zA-Z0-9_]{1,63}', maintenance)):
            raise LifecycleError('Invalid deployment PostgreSQL identity')
        self.docker, self.scope, self.admin, self.maintenance = docker, scope, admin, maintenance
        self.project, self.working_dir = project, str(Path(working_dir).resolve())
        self.authentication = authentication
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
        return self.docker.run(['exec', self.identity, 'sh', '-c',
                'set -eu; test ! -L "$1"; test -f "$1"; test "$(wc -c < "$1")" -le 262144; cat "$1"',
                'observation-hba', self.settings['hba']])

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

    def authenticate(self, password):
        user = f'addp_{self.scope}_observer'
        path = '/tmp/addp-observer-check-' + secrets.token_hex(16)
        self.docker.run(['exec', '-i', self.identity, 'sh', '-c', 'umask 077; cat > "$1"',
                         'observation-password', path], input=f'127.0.0.1:5432:*:{user}:{password}\n')
        try:
            args = ['exec', '-i', '-e', 'PGPASSFILE='+path, '-e', 'PGCONNECT_TIMEOUT=3', self.identity,
                    'psql', '-X', '-q', '-A', '-t', '-w', '-U', user, '-h', '127.0.0.1', '-d']
            value = self.docker.run(args + [self.maintenance], input='SELECT count(*) FROM addp_observation.snapshot();', timeout=8)
            if value.strip() != '1':
                raise LifecycleError('PostgreSQL observation authentication failed')
            other = 'postgres' if self.maintenance != 'postgres' else 'template1'
            result = self.docker.run(args + [other], input='SELECT 1;', timeout=8, check=False)
            if result.returncode == 0 or 'pg_hba.conf rejects connection' not in result.stderr:
                raise LifecycleError('PostgreSQL cross-database authentication boundary failed')
        finally:
            self.docker.run(['exec', self.identity, 'rm', '-f', path])


def state_directory(env, scope):
    value = env.get('ADDP_POSTGRES_METRICS_STATE_DIR')
    parent = Path(value) if value else Path.home().resolve() / '.local/state/addp' / hashlib.sha256(str(ROOT).encode()).hexdigest()[:16] / 'postgres-metrics'
    if not parent.is_absolute() or '..' in parent.parts or parent.resolve().is_relative_to(ROOT):
        raise LifecycleError('PostgreSQL metrics state requires a repository-external absolute directory')
    return parent / scope


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
        self.project = project or 'addp-postgres-metrics-' + scope

    def source_containers(self):
        identities = self.docker.run(['ps', '-aq', '--filter', 'label=com.docker.compose.project='+self.project]).split()
        selected = []
        for identity in identities:
            fact = self.docker.fact(identity)
            labels = fact['labels']
            if labels.get('com.docker.compose.service') not in SERVICES:
                continue
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

    def up(self, env):
        with auth.locked(self.state):
            self.stop_sources()
            database = self.database_factory()
            with database.locked():
                original = replacement = None
                installed = False
                try:
                    database.no_sessions()
                    database.disable()
                    tls, ip, port = transport(env)
                    owner = {'schema': 'addp.postgres-metrics/v1', 'workspace': str(ROOT), 'scope': self.scope,
                             'source_key': {'docker_engine_id': self.docker.info['id'], 'project': database.project, 'service': 'postgres'}}
                    owner_file = self.state / 'owner.json'
                    if os.path.lexists(owner_file):
                        if json.loads(auth._read(owner_file, 0o600)[0]) != owner:
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
                    original = database.read_hba()
                    replacement = source.authentication_configuration(original, self.scope, database.networks, database=database.maintenance)
                    database.sql(install_input(self.scope, password, False))
                    database.write_hba(original, replacement)
                    database.no_sessions()
                    database.sql(install_input(self.scope, password, True))
                    installed = True
                    database.authenticate(password)
                    runtime = self.state / 'runtime'
                    auth._directory(runtime, 0o755)
                    for name in ('source', 'web'):
                        auth._directory(runtime / name, 0o755)
                    file_write(runtime / 'source/sql-exporter.yml', json.dumps(source.configuration(
                        self.scope, database.address, 5432, password, database=database.maintenance)), 0o644)
                    for name in ('ca.crt', 'server.crt', 'server.key'):
                        file_write(runtime / 'web' / name, (tls / name).read_bytes().decode('utf-8'), 0o644)
                    labels = {OWNER: str(ROOT), SCOPE: self.scope}
                    template = str(ROOT / 'scripts/infra/postgres-metrics.yml')
                    compose = {'services': {
                        'postgres-metrics': {'extends': {'file': template, 'service': 'postgres-metrics'},
                            'labels': labels, 'networks': ['database'],
                            'ports': [{'host_ip': ip, 'published': str(port), 'target': 9443}]},
                        'postgres-metrics-web': {'extends': {'file': template, 'service': 'postgres-metrics-web'}, 'labels': labels}},
                        'networks': {'database': {'external': True, 'name': database.network}}}
                    compose_file = self.state / 'compose.json'
                    file_write(compose_file, json.dumps(compose), 0o600)
                    if self.docker.fact(database.identity) != database.facts:
                        raise LifecycleError('PostgreSQL source facts changed during installation')
                    uid, gid = (os.getuid(), os.getgid()) if os.getuid() else (65534, 65534)
                    self.docker.run(['compose', '--env-file', '/dev/null', '-p', self.project, '-f', str(compose_file),
                                     'up', '-d', '--force-recreate', '--no-deps', 'postgres-metrics', 'postgres-metrics-web'],
                                    timeout=180, extra_env={'ADDP_POSTGRES_METRICS_USER': f'{uid}:{gid}',
                                    'ADDP_POSTGRES_METRICS_SOURCE_DIR': str(runtime/'source'),
                                    'ADDP_POSTGRES_METRICS_WEB_DIR': str(runtime/'web')})
                    # A running source is not evidence of a successful scrape.
                    time.sleep(.5)
                    sources = self.docker.run(['ps', '-q', '--filter', 'label=com.docker.compose.project='+self.project,
                                               '--filter', 'label='+OWNER+'='+str(ROOT), '--filter', 'label='+SCOPE+'='+self.scope]).split()
                    if len(sources) != 2 or any(not self.docker.fact(identity)['running'] for identity in sources):
                        raise LifecycleError('PostgreSQL source containers did not stay running')
                except Exception:
                    self.stop_sources()
                    database.disable()
                    if not installed and original is not None and replacement is not None and database.read_hba() == replacement:
                        database.write_hba(replacement, original)
                    raise


def run(action, scope, env):
    flag = env.get('ADDP_POSTGRES_METRICS_ENABLED', 'false')
    docker = Docker(env)
    project, working = inventory.SCOPES[scope]
    state = state_directory(env, scope)

    def database():
        return Database(docker, project, working, scope, env.get('POSTGRES_USER', 'business' if scope == 'business' else ''),
                        authentication=auth.state_directory(env) if scope == 'business' else None)

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
    parser.add_argument('action', choices=['up', 'down'])
    parser.add_argument('--scope', choices=['infra', 'business'], required=True)
    args = parser.parse_args()
    try:
        run(args.action, args.scope, os.environ)
    except (ValueError, OSError, KeyError, TypeError):
        print('PostgreSQL metrics unavailable: deployment ownership, authentication or source configuration failed; core services remain independent', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
