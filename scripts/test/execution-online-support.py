"""Bounded, private controls shared by the Graph and Cleanup Hosted runners.

This module never starts a deployment. The existing Hosted lifecycle owns all
containers, credentials and final destruction. No business rows are mutated.
"""
from contextlib import contextmanager
from importlib import import_module
import json
import os
from pathlib import Path
import re
import subprocess
import time
import uuid
from urllib.parse import urlsplit

HOSTED = import_module('scripts.test.orchestrator-execution-faults')
HTTP = import_module('scripts.test.security-transfer-protection-online')
Client, SuiteError = HTTP.GatewayClient, HTTP.SuiteError
ROOT = Path(__file__).resolve().parents[2]
OWNERS = ('asset', 'develop', 'graph', 'manager', 'meta', 'model', 'quality', 'service', 'standard', 'transfer')
TERMINAL = {'success', 'failed', 'timeout', 'cancelled'}
CONTRACT = json.loads(Path(__file__).with_name('execution-online-contract.json').read_text())
CONTROL_CONTAINERS = {'graph-backend', 'graph-backend-peer', 'asset-backend'}


def require(ok, message):
    if not ok:
        raise SuiteError(message)


def positive(value):
    require(type(value) is int and value > 0, 'invalid positive fixture identity')
    return value


def identifier(value):
    try:
        valid = isinstance(value, str) and str(uuid.UUID(value)) == value and uuid.UUID(value).int != 0
    except ValueError:
        valid = False
    require(valid, 'invalid execution/request UUID')
    return value


def local_client(url, token, timeout=30):
    parsed = urlsplit(url)
    require(parsed.scheme == 'http' and parsed.hostname == '127.0.0.1' and parsed.port is not None
            and not parsed.username and not parsed.password and not parsed.path and not parsed.query and not parsed.fragment,
            'execution acceptance requires an exact loopback HTTP endpoint')
    return Client(url, token, timeout)


def wait_for(read, predicate, message, timeout=120):
    require(type(timeout) is int and 0 < timeout <= 600, 'invalid scenario wait budget')
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        value = read()
        if predicate(value):
            return value
        time.sleep(.2)
    raise SuiteError(message)


def safe_public(value):
    """Check public responses before using them; never archive the input."""
    forbidden = {'execution_config', 'lease_token', 'lease_owner', 'access_token', 'refresh_token',
                 'authorization', 'password', 'client_secret', 'raw_response', 'target_descriptor'}
    if isinstance(value, dict):
        require(not forbidden.intersection(k.lower() for k in value), 'public response contains private execution fields')
        for child in value.values():
            safe_public(child)
    elif isinstance(value, list):
        for child in value:
            safe_public(child)


def user_identity(client, tenant, permissions):
    value = client.request('GET', '/api/v1/system/auth/context', (200,)).payload
    require(value['principal']['type'] == 'user' and value['context']['type'] == 'tenant'
            and value['context']['tenant_id'] == str(positive(tenant))
            and value['token']['type'] == 'first_party_access_token', 'fixture requires a first-party tenant User')
    roles = value['authorization']['role_assignments']
    require(not {r['role_key'] for r in roles}.intersection(HTTP.FORBIDDEN_ADMIN_ROLES)
            and {p for r in roles for p in r['permissions']} == set(permissions), 'fixture permissions differ from the exact role contract')
    return str(value['principal']['id'])


class OwnedDeployment:
    """Pin Docker IDs, not reusable names; recover paused containers on exit."""
    def __init__(self, *, recovering=False):
        self.secret = HOSTED.deployment_secret()
        self.run_id = os.environ.get('ADDP_ONLINE_TEST_RUN_ID', '')
        require(bool(re.fullmatch(r'[A-Za-z0-9_-]{1,100}', self.run_id)), 'invalid Hosted run identity')
        self.pins, self.paused = {}, set()
        path = self.secret / 'execution-paused-containers.json'
        if path.exists() or path.is_symlink():
            require(not path.is_symlink() and path.is_file() and path.stat().st_mode & 0o777 == 0o600
                    and path.stat().st_size <= 8192, 'invalid private pause recovery record')
            record = json.loads(path.read_text())
            require(type(record) is dict and set(record) == {'run_id', 'pins'} and record['run_id'] == self.run_id
                    and type(record['pins']) is dict and len(record['pins']) <= 14, 'pause recovery record belongs to another run')
            require(recovering or not record['pins'], 'prior paused containers must be recovered before another scenario')
            for name, pin in record['pins'].items():
                require(name in CONTROL_CONTAINERS and type(pin) is list and len(pin) == 3
                        and all(type(v) is str and v for v in pin), 'invalid persisted container pin')
                self.pins[name] = tuple(pin)
                self.paused.add(name)

    def command(self, args, *, include_stderr=False, **kwargs):
        require(HOSTED.deployment_secret() == self.secret, 'Hosted deployment identity changed')
        result = subprocess.run(args, capture_output=True, text=True, timeout=15, **kwargs)
        require(result.returncode == 0, 'private Hosted control failed')
        return result.stdout + (result.stderr if include_stderr else '')

    def inspect(self, name):
        require(name in {x + '-backend' for x in OWNERS} | {'graph-backend-peer', 'system-backend',
                    'monitor-backend', 'addp-graph-online-review-extractor', 'addp-postgres'}, 'container is outside execution acceptance')
        rows = json.loads(self.command(['docker', 'inspect', name]))
        require(isinstance(rows, list) and len(rows) == 1, 'ambiguous container identity')
        row = rows[0]
        labels = row['Config'].get('Labels') or {}
        environment = dict(v.split('=', 1) for v in row['Config'].get('Env', []) if '=' in v)
        require(row['State']['Running'] and row['Name'] == '/' + name
                and labels.get('com.docker.compose.project.working_dir') == str(ROOT), 'container is not owned by this checkout')
        if name == 'addp-postgres':
            require(labels.get('com.docker.compose.service') == 'postgres' and environment.get('POSTGRES_DB') == 'addp_online'
                    and all(environment.get(k) and environment[k] == os.environ.get(k) for k in ('POSTGRES_USER', 'POSTGRES_PASSWORD')),
                    'probe must use this Hosted disposable PostgreSQL')
        else:
            require(environment.get('ADDP_ONLINE_TEST_RUN_ID') == self.run_id
                    and environment.get('ADDP_ONLINE_HOSTED') == '1', 'container belongs to another acceptance run')
        pin = (row['Id'], row['Image'], row['State']['StartedAt'])
        require(name not in self.pins or self.pins[name] == pin, 'pinned container was replaced')
        self.pins[name] = pin
        return row

    def pause(self, name):
        require(name in CONTROL_CONTAINERS, 'container is outside the approved pause scope')
        row = self.inspect(name)
        require(not row['State'].get('Paused'), 'container was already paused outside this runner')
        # Persist intent before the external action, including uncertain returns.
        self.paused.add(name)
        self.persist()
        self.command(['docker', 'pause', row['Id']])
        require(self.inspect(name)['State'].get('Paused') is True, 'container pause was not observed')

    def resume(self, name):
        require(name in self.paused, 'resume is outside this runner pause set')
        row = self.inspect(name)
        if row['State'].get('Paused'):
            self.command(['docker', 'unpause', row['Id']])
        require(self.inspect(name)['State'].get('Paused') is False, 'container remains paused')
        self.paused.remove(name)
        self.persist()

    def crash_and_start_graph_peer(self):
        name = 'graph-backend-peer'
        row = self.inspect(name)
        require(name not in self.paused and row['State'].get('Health', {}).get('Status') == 'healthy',
                'Graph peer must be healthy and not paused before crash')
        require(row.get('HostConfig', {}).get('RestartPolicy', {}).get('Name') == 'no',
                'Graph crash requires an explicitly disabled automatic restart policy')
        old = self.pins[name]
        try:
            self.command(['docker', 'kill', '--signal=KILL', old[0]])
            stopped = json.loads(self.command(['docker', 'inspect', old[0]]))[0]
            require(stopped['Id'] == old[0] and stopped['Image'] == old[1]
                    and stopped['State']['Running'] is False and stopped['State']['ExitCode'] == 137,
                    'owned Graph peer crash was not observed')
        finally:
            # The command addresses the pinned immutable ID, even if a name is
            # reassigned. No compose up/restart or global service action occurs.
            self.command(['docker', 'start', old[0]])
        del self.pins[name]
        current = self.inspect(name)
        require(current['Id'] == old[0] and current['Image'] == old[1] and current['State']['StartedAt'] != old[2],
                'Graph restart changed the physical runtime or failed to replace the process')
        wait_for(lambda: self.inspect(name), lambda x: x['State'].get('Health', {}).get('Status') == 'healthy',
                 'restarted Graph peer did not become ready', timeout=120)

    def persist(self):
        HOSTED.write_json(self.secret / 'execution-paused-containers.json',
                          {'run_id': self.run_id, 'pins': {name: self.pins[name] for name in sorted(self.paused)}})

    def close(self):
        failed = False
        for name in list(self.paused):
            try:
                self.resume(name)
            except Exception:
                failed = True
        require(not failed, 'owned paused container recovery failed; lifecycle destruction remains required')

    def read(self, query):
        row = self.inspect('addp-postgres')
        user = os.environ['POSTGRES_USER']
        sql = "BEGIN READ ONLY; SET LOCAL statement_timeout='5s'; " + query + '; COMMIT;'
        output = self.command(['docker', 'exec', '-i', row['Id'], 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1',
                               '-U', user, '-d', 'addp_online'], input=sql)
        return json.loads(output)

    def execution(self, tenant, execution_id, module, task_type):
        require(module in OWNERS or module == 'system', 'invalid execution Owner')
        require(task_type in {'kg_build', 'cleanup', 'cleanup_executor'}, 'invalid execution type')
        query = f"""SELECT row_to_json(e) FROM (
 SELECT execution_id, status, attempt, started_at, completed_at, execution_time_ms,
        created_at, updated_at, parent_execution_id, module, task_type,
        error_details->>'code' AS error_code,
        md5((to_jsonb(t) - 'lease_token' - 'execution_config')::text) AS state_digest,
        lease_expires_at < clock_timestamp() AS expired,
        (SELECT count(*) FROM common.execution_events v WHERE v.execution_id = t.execution_id) AS event_count
 FROM common.task_executions t WHERE tenant_id={positive(tenant)} AND execution_id='{identifier(execution_id)}'
 AND module='{module}' AND task_type='{task_type}') e"""
        value = self.read(query)
        require(isinstance(value, dict) and value.get('execution_id') == execution_id, 'scoped execution was not found')
        return value

    def graph_operations(self, tenant, graph_id, execution_id):
        query = f"""SELECT COALESCE(json_agg(o ORDER BY o.id), '[]'::json) FROM (
 SELECT id, state, kind, attempt, evidence_digest <> '' AS known_evidence,
        late_evidence_digest <> '' AS late_evidence
 FROM graph.graph_operations WHERE tenant_id={positive(tenant)} AND graph_id={positive(graph_id)}
 AND execution_id='{identifier(execution_id)}') o"""
        return self.read(query)

    @contextmanager
    def paused_owner(self, name):
        self.pause(name)
        try:
            yield
        except Exception as primary:
            try:
                self.resume(name)
            except Exception as cleanup:
                raise ExceptionGroup('scenario and owned resume failed', [primary, cleanup]) from None
            raise
        else:
            self.resume(name)


if __name__ == '__main__':
    import sys
    require(sys.argv[1:] == ['resume'], 'only owned pause recovery is exposed')
    OwnedDeployment(recovering=True).close()
