"""Hosted MySQL Develop file approvals, the real supervisor and an owned NFS export."""
import importlib
import json
import os
from pathlib import Path
import sys
import time
import uuid

from scripts.utils.online_nfs_fixture import NFSExport
from scripts.utils.hosted_mysql_platform import SuiteError, command, login, mysql_read, owned_container, require

topology = importlib.import_module('scripts.test.vector-topology-persistence-online')
registration = importlib.import_module('scripts.test.online-engine-registration')
raster, fixture, support = topology.raster, topology.fixture, topology.support
PROFILE = fixture.network_profile(fixture.NORMALIZE_CASE)
ACTOR_PERMISSIONS = {
    'develop.task.read', 'develop.task.execute', 'develop.data_read.execute', 'develop.data_write.execute',
    'system.execution_authorization.create', 'meta.catalog.read', 'manager.data_item.read',
}
WRONG_OWNER_TARGET = 'wrong-owner.gpkg'
UNAPPROVED_TARGET = 'not-approved.gpkg'


def develop_process_identity(container):
    # The launcher may use a different UID for the node-wide log receiver.
    # NFS AUTH_SYS uses the Go server's real UID/GID, read from /proc here.
    raw = command(['docker', 'exec', container, 'cat', '/proc/1/status'])
    facts = {key: value.split() for key, sep, value in (line.partition(':') for line in raw.splitlines()) if sep}
    expected = ['10001'] * 4
    require(facts['Name'] == ['server'] and facts['Uid'] == expected and facts['Gid'] == expected
            and not facts['Groups'] and int(facts['CapEff'][0], 16) == 0 and facts['NoNewPrivs'] == ['1'],
            'formal Compose Develop PID 1 must have the deployed non-root identity and privilege restrictions')
    return dict(pid=1, uid=int(facts['Uid'][0]), gid=int(facts['Gid'][0]),
                supplementary_groups=[], effective_capabilities=0, no_new_privileges=True)


def file_snapshot(mysql, tenant, execution):
    require(str(uuid.UUID(execution)) == execution, 'invalid Develop execution identity')
    query = f"""
SELECT e.status, e.attempt, e.lease_token IS NULL, e.started_at IS NULL, f.admission_state,
       COALESCE(JSON_EXTRACT(f.admission_json, '$.approval_ids'), JSON_ARRAY()),
       COALESCE(f.runtime_execution_id, ''), COALESCE(e.execution_authorization_id, 0),
       COALESCE(JSON_UNQUOTE(JSON_EXTRACT(e.metadata, '$.result.runtime_status.status')), '-')
FROM common.task_executions e JOIN develop.file_executions f
  ON e.execution_id=f.execution_id AND e.tenant_id=f.tenant_id
WHERE e.execution_id='{execution}' AND e.tenant_id={int(tenant)} AND e.module='develop' AND e.trigger_type='manual';
"""
    raw = mysql_read(mysql, query)
    status, attempt, no_lease, unstarted, admission, approvals, runtime_id, authorization_id, runtime_status = raw.split('\t')
    return dict(status=status, attempt=int(attempt), lease_absent=no_lease == '1', unstarted=unstarted == '1',
                admission_state=admission, admission_approval_ids=json.loads(approvals),
                runtime_execution_id=runtime_id or None, authorization_id=authorization_id, runtime_status=runtime_status)


def verify_pending(snapshot, authorization):
    require(snapshot == dict(status='pending', attempt=0, lease_absent=True, unstarted=True,
                             admission_state='pending', admission_approval_ids=[], runtime_execution_id=None,
                             authorization_id=authorization, runtime_status='-'),
            'a partially approved execution did not remain unclaimed')


def verify_one_approval(first, second, execution, approval_id, binding):
    require(first['state'] == 'approved' and first['approval']['approval_id'] == approval_id
            and second['state'] == 'awaiting_approval' and 'approval' not in second
            and all((view['authorization_id'], view['scope_digest']) == binding and view['execution_id'] == execution
                    for view in (first, second)), 'expected precisely the first target approval on the original scope')


def verify_worker_result(snapshot, status, receipt_ids):
    require(snapshot['status'] == status and snapshot['attempt'] == 1 and not snapshot['unstarted']
            and snapshot['lease_absent'] and snapshot['admission_state'] == 'ready'
            and snapshot['admission_approval_ids'] == receipt_ids and snapshot['runtime_execution_id']
            and snapshot['runtime_status'] == 'success',
            'the same execution did not finish after one real Worker claim with the exact approvals')


def physical(image, root, action):
    path = '/fixture'
    mount = f'{root}:{path}'
    uid = f'{os.getuid()}:{os.getgid()}'
    if action == 'verify':
        mount += ':ro'
        uid = '0:0'  # Read the Runner's physical files without touching their service ownership.
    source = '''
import importlib
import json
from pathlib import Path
import sys
fixture = importlib.import_module('business.scripts.online-vector-topology-fixture')
case = fixture.NORMALIZE_CASE
profile = fixture.network_profile(case)
root = Path('/fixture')
source = root / profile['source']
if sys.argv[1] == 'seed':
    if source.exists() or list((root / 'outputs').iterdir()):
        raise RuntimeError('refusing existing NFS source or outputs')
    fixture.seed_normalize(source, case)
    source.chmod(0o644)
    result = {'source': fixture.digest(source)}
else:
    if sorted(p.name for p in (root / 'outputs').iterdir()) != sorted(profile['targets']):
        raise RuntimeError('NFS contains a missing, unapproved or extra physical output')
    result = {'source': fixture.verify_normalize(source, 'source', case), 'outputs': {
        name: {**fixture.verify_normalize(root / 'outputs' / name, port, case),
               'uid': (root / 'outputs' / name).stat().st_uid}
        for name, port in zip(profile['targets'], profile['ports'])}}
print(json.dumps(result))
'''
    return json.loads(command(['docker', 'run', '--rm', '-i', '--network', 'none', '--user', uid,
                               '--entrypoint', 'python', '-v', mount, '-v', f'{Path.cwd()}:/repo:ro',
                               '-w', '/repo', image, '-B', '-', action], input=source))


def run(nfs, secret, run_id, artifacts):
    project = os.environ['ADDP_DEPLOYMENT_PROJECT']
    infra = os.environ['ADDP_MYSQL_FIXTURE_PROJECT']
    network = os.environ['ADDP_DEPLOYMENT_NETWORK']
    develop = owned_container(os.environ['ADDP_TEST_DEVELOP_CONTAINER'], project, 'develop-backend', network)
    mysql = owned_container(os.environ['ADDP_TEST_MYSQL_CONTAINER'], infra, 'mysql', network)['Id']
    runtime = owned_container(os.environ['ADDP_TEST_GEOPYTHON_CONTAINER'], infra, 'geopython-workflow-engine', network)
    require(runtime['Config']['Cmd'] == ['sh', 'container_entrypoint.sh']
            and runtime['Config']['Image'] == 'localhost:5001/addp-geopython-workflow-engine:hosted-' + os.environ['GITHUB_SHA']
            and not any(runtime['HostConfig']['PortBindings'] or {}) and not runtime['Mounts'],
            'GeoPython must use the built product entry, with no NFS mount or public port')
    business = develop_process_identity(develop['Id'])
    uid, gid = business['uid'], business['gid']

    export = nfs.lifecycle('start', secret, run_id, compose_bridge=(network, infra))
    root = nfs.owned_root(secret, run_id)
    staging = root / '.addp-publication'
    outputs = root / 'outputs'
    initial = physical(runtime['Image'], root, 'seed')
    command(['sudo', 'chown', f'{uid}:{gid}', str(outputs)])
    command(['sudo', 'chmod', '0755', str(outputs)])
    evidence = {'result': 'incomplete', 'source_commit': os.environ['GITHUB_SHA'], 'provider': 'mysql',
                'develop_process': business, 'geopython_image_id': runtime['Image'],
                'nfs_export': export, 'initial_physical_source': initial['source'], 'stages': []}
    report = artifacts / 'develop-file-publication.json'

    def record(value):
        evidence['stages'].append(value)
        report.write_text(json.dumps(evidence, indent=2) + '\n')

    record({'stage': 'fixture_ready'})
    facts = json.loads(Path(os.environ['ADDP_TEST_MYSQL_DEVELOP_FILE_FACTS']).read_text())
    tenant = facts['tenant_id']
    actor, approver, registrar = [login(f'develop-file-{role.lower()}', os.environ[f'ADDP_TEST_MYSQL_DEVELOP_{role}_PASSWORD'],
                                      facts[f'develop-file-{role.lower()}'], tenant)
                                  for role in ('ACTOR', 'APPROVER', 'REGISTRAR')]
    identity = raster.validate_identity(actor, tenant, required_permissions=ACTOR_PERMISSIONS)
    grants = registration.ExactFileReadGrants(approver, actor, tenant, scan_preparation=True, approval_preparation=True)
    registrar_context = registrar.request('GET', '/api/v1/system/auth/context', (200,)).payload
    roles = registrar_context['authorization']['role_assignments']
    require([role['role_key'] for role in roles] == ['tenant.infrastructure_administrator']
            and {'system.engine.create', 'system.engine.execute'} <= set(roles[0]['permissions']),
            'the separate engine provisioner requires the standard infrastructure role')
    descriptor = registration.load_descriptor(secret / 'develop-file-nfs-engine.json')
    engine = registration.register_engine(registrar.base_url, registrar.token, descriptor)
    registrar.token = ''
    record({'stage': 'source_registered', 'identity': identity, 'registrar_role': roles[0]['role_key'], 'engine_id': engine})
    source_name = PROFILE['source']
    scope = f'addp://engine/{engine}/path/{source_name}?type=file'

    try:
        basic = support.wait_for_meta_scan(approver, engine, time.monotonic() + 150,
                                          depth='basic', scope={'targets': [scope]})
        record({'stage': 'source_basic_scanned', 'basic_scan': basic})
        grants.grant(engine, source_name)
        deep = support.wait_for_meta_scan(approver, engine, time.monotonic() + 150,
                                         scope={'targets': [scope]})
        item = support.find_fixture_item(actor, engine, source_name, 'Develop file source')
        source_locator = f'{scope}&item_id={item["id"]}'
        record(dict(stage='source_granted', deep_scan=deep, source_item_id=item['id']))

        deadline = time.monotonic() + 120
        while True:
            available = actor.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload
            candidates = [value for value in available if value.get('engine_type') == 'geopython_workflow'
                          and value.get('connection_status') == 'online']
            if len(candidates) == 1:
                runtime_id = raster.positive(candidates[0]['id'], 'GeoPython Runtime')
                break
            require(len(candidates) == 0 and time.monotonic() < deadline, 'expected one online GeoPython Runtime')
            time.sleep(2)
        run_approval_scenario(actor, approver, grants, mysql, tenant, engine, runtime_id, uid, gid,
                              source_locator, staging, outputs, runtime['Image'], initial, record)
    except BaseException:
        try:
            record({'stage': 'source_grant_cleanup', **grants.cleanup()})
        except Exception as error:
            record({'stage': 'source_grant_cleanup_failed', 'type': type(error).__name__})
        raise
    else:
        record({'stage': 'source_grant_cleanup', **grants.cleanup()})
    evidence['result'] = 'passed'
    record({'stage': 'complete'})


def run_approval_scenario(actor, approver, grants, mysql, tenant, engine, runtime_id, uid, gid,
                          source, staging, outputs, image, initial, record):
    parent = f'addp://engine/{engine}/path/outputs?type=directory'
    approver_binding = {}

    def approve(execution, name, index):
        def observe(entry):
            if entry['stage'] == 'approval_intent':
                context = entry['context']
                binding = (context['authorization_id'], context['scope_digest'])
                require(approver_binding.setdefault(execution, binding) == binding, 'target approval changed the original authorization')
            record(entry)
        return registration.approve_geopackage_save(
            approver, execution, tenant, grants.recipient, grants.target(engine, f'outputs/{name}', 'nfs'),
            on_evidence=observe, target_index=index)

    def wait_execution(execution, expected):
        def record_terminal(value):
            if value.get('status') in support.TERMINAL_STATUSES:
                record({'stage': 'worker_terminal', 'execution_id': execution, 'status': value['status'],
                        'error_details': value.get('error_details')})
        return raster.wait_execution(actor, 'develop', execution, 180, expected=expected, on_observation=record_terminal)

    wrong_uid = 65533 if uid != 65533 else 65532
    command(['sudo', 'chown', f'{wrong_uid}:{gid}', str(staging)])
    require(staging.stat().st_uid == wrong_uid and staging.stat().st_mode & 0o7777 == 0o700, 'wrong-owner control is invalid')
    definition = topology.normalize_definition(source, parent)
    definition['tasks'].pop()  # A distinct single target for the wrong-owner execution.
    definition['tasks'][-1]['params']['target_name'] = WRONG_OWNER_TARGET
    wrong = raster.submit(actor, runtime_id, definition)
    record({'stage': 'wrong_owner_submitted', 'staging_uid': wrong_uid, 'staging_mode': '0700', 'execution_id': wrong})
    wrong_approval = approve(wrong, WRONG_OWNER_TARGET, 0)
    failed = wait_execution(wrong, 'failed')
    failed_row = file_snapshot(mysql, tenant, wrong)
    verify_worker_result(failed_row, 'failed', [wrong_approval['approval_id']])
    require(failed.get('error_details', {}).get('code') == 'develop.workflow.execution_failed'
            and not failed.get('outputs') and not list(outputs.iterdir())
            and not command(['sudo', 'find', str(staging), '-mindepth', '1', '-maxdepth', '1', '-print', '-quit']),
            'wrong-owner execution left a public file or private staging residue')
    record({'stage': 'wrong_owner_failed', 'execution_id': wrong, 'worker': failed_row,
            'error_details': failed.get('error_details'), 'physical_outputs': []})

    command(['sudo', 'chown', f'{uid}:{gid}', str(staging)])
    require((staging.stat().st_uid, staging.stat().st_gid, staging.stat().st_mode & 0o7777) == (uid, gid, 0o700),
            'positive execution requires exact Develop owner and mode 0700')
    execution = raster.submit(actor, runtime_id, topology.normalize_definition(source, parent))
    require(execution != wrong, 'wrong-owner and positive cases must use separate executions')
    record({'stage': 'dual_target_submitted', 'execution_id': execution})
    first_name, second_name = PROFILE['targets']
    first = approve(execution, first_name, 0)
    first_view = approver.request('GET', f'/api/v1/system/execution-authorizations/develop-file-workflows/{execution}/create-approval-context?target_index=0', (200,)).payload
    second_view = approver.request('GET', f'/api/v1/system/execution-authorizations/develop-file-workflows/{execution}/create-approval-context?target_index=1', (200,)).payload
    authorization, scope_digest = approver_binding[execution]
    verify_one_approval(first_view, second_view, execution, first['approval_id'], (authorization, scope_digest))
    rejected = approver.request(
        'POST', f'/api/v1/system/execution-authorizations/{authorization}/develop-geopackage-create-approvals',
        (403,), {'target_index': len(PROFILE['targets']), 'scope_digest': scope_digest,
                 'expires_at': second_view['authorization_expires_at']})
    require(rejected.payload.get('error_code') == 'permission_denied', 'wrong out-of-scope approval rejection')
    record({'stage': 'outside_frozen_targets_rejected', 'execution_id': execution, 'http_status': rejected.status,
            'target_index': len(PROFILE['targets']), 'unapproved_path': f'outputs/{UNAPPROVED_TARGET}'})
    # A pending observation intentionally does not change the row version.
    # Observe the real row across the supervisor's 30-second recovery interval.
    start = time.monotonic()
    while True:
        pending = file_snapshot(mysql, tenant, execution)
        physical_outputs = sorted(path.name for path in outputs.iterdir())
        try:
            verify_pending(pending, authorization)
            require(not physical_outputs, 'files exist with only one target approved')
        except SuiteError:
            record({'stage': 'one_approval_assertion_failed', 'execution_id': execution,
                    'worker': pending, 'physical_outputs': physical_outputs})
            raise
        if time.monotonic() - start >= 35:
            break
        time.sleep(2)
    record({'stage': 'one_approval_unclaimed', 'execution_id': execution, 'worker': pending,
            'worker_observation_seconds': int(time.monotonic() - start), 'physical_outputs': []})

    second = approve(execution, second_name, 1)
    require(first['approval_id'] != second['approval_id'], 'each target must have a distinct receipt')
    result = wait_execution(execution, 'success')
    saved = {port: f'addp://engine/{engine}/path/outputs/{name}?type=file'
             for port, name in zip(PROFILE['ports'], PROFILE['targets'])}
    topology.validate_saved_facts(result, [('input.normal_input', source)], saved, 2)
    final = file_snapshot(mysql, tenant, execution)
    verify_worker_result(final, 'success', [first['approval_id'], second['approval_id']])
    require(final['authorization_id'] == authorization and str(result['triggered_by']) == grants.recipient,
            'the published execution changed authorization or User identity')
    native = physical(image, outputs.parent, 'verify')
    require(all(native['source'][key] == value for key, value in initial['source'].items())
            and all(file['uid'] == uid for file in native['outputs'].values())
            and not (outputs / UNAPPROVED_TARGET).exists() and not (outputs / WRONG_OWNER_TARGET).exists(),
            'physical source, output owner or unapproved path differs')
    record({'stage': 'both_approved_files_published', 'execution_id': execution, 'worker': final,
            'outputs': result['outputs'], 'physical': native, 'unapproved_path_absent': f'outputs/{UNAPPROVED_TARGET}'})


if __name__ == '__main__':
    nfs_fixture = NFSExport('develop-file', command, publication_staging=True)
    secret_dir = nfs_fixture.guard(os.environ)
    fixture_run_id = os.environ['ADDP_ONLINE_TEST_RUN_ID']
    if sys.argv[1:] == ['stop-nfs']:
        print(json.dumps(nfs_fixture.lifecycle('stop', secret_dir, fixture_run_id)))
    elif len(sys.argv) == 3 and sys.argv[1] == 'run':
        run(nfs_fixture, secret_dir, fixture_run_id, Path(sys.argv[2]))
    else:
        raise SystemExit('expected run ARTIFACTS or stop-nfs')
