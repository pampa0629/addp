"""Approved Hosted raster suites: bounded read-only persistence evidence.

Business requests remain ordinary User requests. This observer never supplies
credentials to them and never substitutes SQL for API authorization or Meta.
"""
from __future__ import annotations

import hashlib
import importlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))
BASE = importlib.import_module('scripts.test.execution-retention-readonly-online')
SuiteError = importlib.import_module('scripts.test.manager-internal-artifact-lineage-online').SuiteError
SUITES = {'raster-cut-fill-persistence': 'cut-fill-lineage', 'raster-tpi-persistence': 'tpi-lineage', 'raster-tri-persistence': 'tri-lineage', 'raster-roughness-persistence': 'roughness-lineage', 'raster-surface-area-persistence': 'surface-area-lineage', 'raster-twi-persistence': 'twi-lineage', 'raster-spi-persistence': 'spi-lineage', 'raster-sieve-persistence': 'sieve-lineage', 'raster-focal-percentile-persistence': 'percentile-lineage', 'raster-zonal-percentile-persistence': 'zonal-percentile-lineage'}
RECEIPT = 'raster-lineage-owner.json'
FIELDS = {'schema_version', 'inputs', 'outputs', 'operations'}


def require(ok, message):
    if not ok:
        raise SuiteError(message)


def boundary(env):
    require(all(env.get(k) == os.environ.get(k) for k in ('ONLINE_SUITE','ADDP_ONLINE_TEST_RUN_ID','ADDP_ONLINE_ARTIFACT_DIR','ADDP_ONLINE_SECRET_DIR','GITHUB_RUN_ID','GITHUB_RUN_ATTEMPT')), 'observer environment differs from its actual Hosted process')
    secret = BASE.HOSTED.deployment_secret()
    suite = env.get('ONLINE_SUITE')
    require(suite in SUITES and env.get('ADDP_ONLINE_TEST') == '1'
            and not env.get('ADDP_ONLINE_OWNER_MANAGED') and not env.get('ADDP_EXECUTION_TRACE_LOCAL'),
            'observer only owns approved Hosted raster suites')
    run = env.get('ADDP_ONLINE_TEST_RUN_ID', '')
    prefix = suite.removeprefix('raster-').removesuffix('-persistence')
    require(run == prefix + '-' + env.get('GITHUB_RUN_ID', '') + '-' + env.get('GITHUB_RUN_ATTEMPT', '')
            and re.fullmatch(r'[a-z-]+-\d+-\d+', run), 'observer Run ID differs')
    artifacts = Path(env['ADDP_ONLINE_ARTIFACT_DIR']).resolve(strict=True)
    require(not artifacts.is_relative_to(ROOT) and not artifacts.is_relative_to(secret)
            and not secret.is_relative_to(artifacts), 'observer private/evidence boundaries overlap')
    return secret, suite, run


def inspect():
    try:
        result = subprocess.run(['docker', 'inspect', 'addp-postgres'], check=True,
                                capture_output=True, text=True, timeout=10)
        value, = json.loads(result.stdout)
        return value
    except Exception:
        raise SuiteError('owned PostgreSQL inspection unavailable') from None


def prepare():
    secret, suite, run = boundary(os.environ)
    container = inspect()
    user = BASE.validate_container(container, container.get('Id'), os.environ)
    require(re.fullmatch('[0-9a-f]{64}', container['Id']) and container['State'].get('StartedAt'),
            'observer needs an exact live container identity')
    value = {'schema': 'addp.raster-lineage-owner/v1', 'suite': suite, 'run_id': run,
             'container_id': container['Id'], 'started_at': container['State']['StartedAt'],
             'postgres_user': user}
    path = secret / RECEIPT
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, 'w') as stream:
        json.dump(value, stream, sort_keys=True)


def read_current(env, identifier, tenant, principal, workflow):
    secret, suite, run = boundary(env)
    require(type(tenant) is int and tenant > 1 and type(principal) is int and principal > 0
            and isinstance(identifier, str) and re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', identifier),
            'observer requires current User/Tenant/execution identities')
    path = secret / RECEIPT
    require(not path.is_symlink() and path.is_file() and path.stat().st_uid == os.getuid()
            and path.stat().st_mode & 0o777 == 0o600, 'observer owner receipt is not private')
    owner = json.loads(path.read_text())
    require(set(owner) == {'schema', 'suite', 'run_id', 'container_id', 'started_at', 'postgres_user'}
            and owner['schema'] == 'addp.raster-lineage-owner/v1' and owner['suite'] == suite
            and owner['run_id'] == run, 'observer receipt differs from current run')
    container = inspect()
    values = dict(v.split('=', 1) for v in container.get('Config', {}).get('Env', []) if '=' in v)
    user = BASE.validate_container(container, owner['container_id'], values)
    require(user == owner['postgres_user'] and container['State']['StartedAt'] == owner['started_at'],
            'observer container restarted or changed')
    require(isinstance(workflow, dict) and len(json.dumps(workflow).encode()) <= 65536, 'workflow evidence exceeds bound')
    # Fixed read-only query; psql quoted variables never become SQL source.
    statement = """BEGIN READ ONLY;
SET LOCAL statement_timeout='3s'; SET LOCAL lock_timeout='1s';
SELECT json_build_object('execution_id',execution_id,'tenant_id',tenant_id,
 'principal_id',actor_principal_id,'triggered_by',triggered_by,'module',module,
 'task_type',task_type,'status',status,
 'workflow_matches',execution_config->'content'->'workflow_definition' = :'workflow'::jsonb,
 'facts',CASE WHEN octet_length((metadata->'lineage_facts')::text)<=32768
 THEN metadata->'lineage_facts' ELSE '{"unavailable_or_oversize":true}'::jsonb END)
FROM common.task_executions WHERE execution_id=:'execution_id'
 AND tenant_id=:'tenant_id'::integer AND actor_principal_id=:'principal_id'::bigint
 AND triggered_by=:'principal_id'::integer AND module='develop' AND task_type='workflow'
 AND status='success' AND created_at>=:'started_at'::timestamptz LIMIT 1;
COMMIT;
"""
    command = ['docker', 'exec', '-i', owner['container_id'], 'psql', '-X', '-qAt',
               '-v', 'ON_ERROR_STOP=1', '-v', 'VERBOSITY=sqlstate', '-U', user, '-d', 'addp_online']
    for key, value in {'execution_id': identifier, 'tenant_id': tenant, 'principal_id': principal,
                       'started_at': owner['started_at'], 'workflow': json.dumps(workflow, sort_keys=True)}.items():
        command.extend(['-v', key + '=' + str(value)])
    try:
        result = subprocess.run(command, input=statement, capture_output=True, text=True, timeout=8)
        require(result.returncode == 0 and 0 < len(result.stdout.encode()) <= 40000,
                'bounded internal lineage read unavailable')
        value = json.loads(result.stdout)
    except Exception:
        raise SuiteError('bounded internal lineage read unavailable') from None
    require(set(value) == {'execution_id', 'tenant_id', 'principal_id', 'triggered_by', 'module',
                           'task_type', 'status', 'workflow_matches', 'facts'}, 'internal evidence envelope differs')
    expected = {'execution_id': identifier, 'tenant_id': tenant, 'principal_id': principal,
                'triggered_by': principal, 'module': 'develop', 'task_type': 'workflow',
                'status': 'success', 'workflow_matches': True}
    require(all(value.get(k) == v and type(value.get(k)) is type(v) for k, v in expected.items()),
            'internal evidence is not the current workflow/User/Tenant')
    return value['facts']


def validate(facts, expected, *, public):
    require(isinstance(facts, dict) and set(facts) == FIELDS and facts['schema_version'] == expected['schema_version'],
            'lineage envelope differs')
    for side in ('inputs', 'outputs'):
        require(isinstance(facts[side], list), 'lineage resources must be arrays')
        refs = {}
        for ref in facts[side]:
            require(isinstance(ref, dict) and isinstance(ref.get('port'), str) and ref['port']
                    and ref['port'] not in refs, 'duplicate or missing lineage port')
            refs[ref['port']] = ref
        require(refs == {r['port']: r for r in expected[side]}, 'role-bound lineage resources differ')
    if public:
        require(facts['operations'] is None, 'public observation must hide internal operations')
        return
    operations = facts['operations']
    require(isinstance(operations, list), 'internal operations must be an array')
    normalized = []
    for raw in operations:
        require(isinstance(raw, dict), 'invalid internal operation')
        op = dict(raw)
        for side in ('input_ports', 'output_ports'):
            ports = op.get(side)
            require(isinstance(ports, list) and all(isinstance(p, str) and p for p in ports)
                    and len(set(ports)) == len(ports), 'duplicate or missing internal operation ports')
            op[side] = sorted(ports)
        normalized.append(op)
    require(normalized == expected['operations'], 'exact internal save/derive operation differs')


def closed(value, expected):
    """Archive contract fields only; unknown values, credentials and responses never escape."""
    allowed_values = {str(v) for side in ('inputs', 'outputs') for ref in expected[side] for v in ref.values()}
    allowed_values |= {expected['schema_version'], 'derive', *[op['operator'] for op in expected['operations']]}
    def safe(v):
        if isinstance(v, str) and v not in allowed_values:
            return {'unexpected_string_sha256': hashlib.sha256(v.encode()).hexdigest()}
        return v
    def fields(item, allowed):
        if not isinstance(item, dict):
            return {'invalid_type': type(item).__name__}
        result = {'unrecognized_fields': sorted(set(item) - set(allowed))}
        for k in allowed:
            if k not in item:
                continue
            v = item[k]
            valid = isinstance(v, str) or v is None or (k.endswith('_ports') and isinstance(v, list)
                                                       and all(isinstance(p, str) for p in v))
            result[k] = ([safe(p) for p in v] if isinstance(v, list) else safe(v)) if valid else {'invalid_type': type(v).__name__}
        return result
    if not isinstance(value, dict):
        return {'invalid_type': type(value).__name__}
    result = fields(value, ('schema_version',))
    result['unrecognized_fields'] = sorted(set(value) - FIELDS)
    for side, allowed in [('inputs', ('port', 'locator')), ('outputs', ('port', 'locator', 'write_mode')),
                          ('operations', ('kind', 'operator', 'input_ports', 'output_ports'))]:
        entries = value.get(side)
        result[side] = [fields(v, allowed) for v in entries] if isinstance(entries, list) else {'invalid_type': type(entries).__name__}
    return result


def collect(env, suite, identifier, name, expected, execution, tenant, principal, workflow, reader=read_current):
    require(suite in SUITES and env.get('ONLINE_SUITE') == suite and re.fullmatch(r'[a-z0-9.-]+', name), 'invalid owned lineage case')
    metadata = execution.get('metadata')
    public = metadata.get('lineage_facts') if isinstance(metadata, dict) else None
    internal = None
    error = None
    try:
        internal = reader(env, identifier, tenant, principal, workflow)
    except Exception as failure:
        error = type(failure).__name__
    directory = Path(env['ADDP_ONLINE_ARTIFACT_DIR']) / SUITES[suite]
    require(not directory.is_symlink(), 'lineage archive must not be symlink')
    directory.mkdir(parents=True, exist_ok=True)
    path = directory / (name + '.json')
    value = {'schema_version': 'addp.raster-lineage-evidence/v2', 'case': name, 'execution_id': identifier,
             'tenant_id': tenant, 'principal_id': principal, 'run_id': env['ADDP_ONLINE_TEST_RUN_ID'],
             'evidence_source': 'isolated_postgresql_read_only', 'expected_internal': expected,
             'actual_public': closed(public, expected), 'actual_internal': closed(internal, expected), 'read_error': error}
    with path.open('xb') as stream:
        stream.write((json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + '\n').encode())
    receipt = {'path': SUITES[suite] + '/' + path.name, 'size_bytes': path.stat().st_size,
               'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}
    require(error is None, 'internal lineage read failed; closed rejection evidence archived')
    validate(public, expected, public=True)
    validate(internal, expected, public=False)
    return receipt


def verify_archive(env, suite, cases):
    directory = Path(env['ADDP_ONLINE_ARTIFACT_DIR']) / SUITES[suite]
    require(not directory.is_symlink() and directory.is_dir()
            and {p.name for p in directory.iterdir()} == {c['name'] + '.json' for c in cases},
            'lineage archive inventory differs')
    for case in cases:
        proof = case['lineage_evidence']
        path = directory / (case['name'] + '.json')
        require(proof.get('path') == SUITES[suite] + '/' + path.name
                and type(proof.get('size_bytes')) is int and proof['size_bytes'] > 0
                and not path.is_symlink() and path.is_file() and path.stat().st_size == proof['size_bytes']
                and hashlib.sha256(path.read_bytes()).hexdigest() == proof.get('sha256'),
                'frozen lineage evidence changed')


if __name__ == '__main__':
    try:
        require(sys.argv[1:] == ['prepare'], 'observer only exposes the fixed prepare action')
        prepare()
    except Exception:
        print('Raster lineage owner preparation failed', file=sys.stderr)
        raise SystemExit(1)
