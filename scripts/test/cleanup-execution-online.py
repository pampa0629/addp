"""Real Gateway/Owner protocol acceptance in a precreated disposable Tenant.

Domain physical-effect fixtures are separate assertions: a successful empty
assessment does not prove object deletion, Capture shutdown or index fencing.
The Hosted lifecycle, not this script, destroys its deployment and Tenant.
"""
from importlib import import_module
import json
import os
from pathlib import Path
import re
import sys
import uuid

S = import_module('scripts.test.execution-online-support')
require = S.require
ROOT = '/api/v1/system/admin/cleanup'
PERMISSIONS = set(S.CONTRACT['cleanup_permissions'])
RESULT_KEYS = {'schema', 'execution_id', 'request_id', 'module', 'attempt', 'action', 'cleanup_mode',
               'status', 'summary', 'statistics', 'reason'}


def accepted(client, action, body):
    value = client.request('POST', ROOT + '/' + action, (200,), body).payload
    require(type(value) is dict and set(value) == {'task_id'} and value['task_id'] == body['request_id'],
            'Cleanup acceptance changed request identity')
    return S.identifier(value['task_id'])


def task(client, request_id):
    value = client.request('GET', ROOT + '/tasks/' + S.identifier(request_id), (200,)).payload
    S.safe_public(value)
    require(value.get('task_id') == request_id and value.get('task', {}).get('task_id') == request_id,
            'Cleanup detail changed identity')
    return value


def children(value, owners):
    rows = value.get('module_executions')
    require(type(rows) is list and len(rows) == len(owners)
            and {x.get('module') for x in rows} == set(owners), 'Cleanup omitted or duplicated an expected Owner')
    parent = S.identifier(value['task']['execution_id'])
    require(len({S.identifier(x.get('execution_id')) for x in rows}) == len(rows), 'Cleanup duplicated child identity')
    for row in rows:
        require(row.get('parent_execution_id') == parent and row.get('task_type') == 'cleanup_executor'
                and row.get('execution_boundary') == 'bounded' and row.get('max_attempts') == 1,
                'Cleanup child has the wrong parent, type or replay policy')
    return rows


def assert_result(value, row, request_id, tenant):
    S.safe_public(value)
    receipt = row.get('receipt')
    require(type(receipt) is dict and receipt.get('schema') == 'addp.cleanup-receipt/v1'
            and receipt.get('tenant_id') == tenant and receipt.get('parent_execution_id') == row['parent_execution_id'],
            'Cleanup receipt identity differs')
    require(type(value) is dict and set(value) <= RESULT_KEYS
            and value.get('schema') == 'addp.cleanup-professional-result/v1'
            and value.get('request_id') == request_id and value.get('execution_id') == row['execution_id']
            and value.get('module') == row['module'] and type(value.get('attempt')) is int
            and type(row.get('attempt')) is int and value.get('attempt') == row['attempt'] == 1
            and value.get('status') == row['status'] == 'success', 'Owner professional result identity/status differs')
    for key in ('request_id', 'execution_id', 'module', 'attempt', 'action', 'cleanup_mode', 'status', 'reason', 'summary'):
        require(value.get(key) == receipt.get(key), 'Owner result and committed receipt disagree')
    require(value.get('reason', '') in {'', 'owner_failed', 'owner_unavailable', 'authorization_denied',
                    'active_execution', 'external_effect_uncertain', 'scope_changed', 'unsupported_scope'},
            'Owner result returned an unsafe reason')
    require(type(value.get('statistics')) is dict and len(value['statistics']) <= 128
            and all(re.fullmatch(r'[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){0,3}', key)
                    and type(n) is int and 0 <= n <= 9007199254740991 for key, n in value['statistics'].items()),
            'Owner result contains invalid counters')


def assert_unstarted(row):
    require(type(row.get('attempt')) is int and row.get('attempt') == 0 and row.get('status') in {'pending', 'failed'}
            and all(row.get(k) is None for k in ('started_at', 'completed_at', 'execution_time_ms')),
            'unstarted child fabricated execution times')


def assert_rejected(row):
    # An Owner rejection may record a completion instant, but never a start,
    # attempt, execution duration, module result, or runtime process event.
    require(row.get('status') == 'failed' and type(row.get('attempt')) is int and row.get('attempt') == 0 and row.get('started_at') is None
            and row.get('execution_time_ms') is None and not row.get('receipt'), 'Owner rejection fabricated a run')
    view = row.get('cleanup_admission')
    require(type(view) is dict and view.get('schema') == 'addp.cleanup-admission-view/v1'
            and view.get('state') == 'closed_rejected', 'missing committed closed/rejected projection')


def verify_results(client, other_user, unauthorized, tenant, deployment, completed, report, checkpoint):
    request_id = completed['task_id']
    rows = children(completed, S.OWNERS)
    for row in rows:
        result_path = f"/api/v1/{row['module']}/cleanup-executions/{row['execution_id']}/result"
        before = deployment.execution(tenant, row['execution_id'], row['module'], 'cleanup_executor')
        value = client.request('GET', result_path, (200,)).payload
        assert_result(value, row, request_id, tenant)
        other_user.request('GET', result_path, (404,))
        unauthorized.request('GET', result_path, (403,))
        client.request('GET', f"/api/v1/{row['module']}/cleanup-executions/{completed['task']['execution_id']}/result", (404,))
        require(deployment.execution(tenant, row['execution_id'], row['module'], 'cleanup_executor') == before,
                'professional GET changed execution state or events')
        report['owner_results'].append({'module': row['module'], 'execution_id': row['execution_id'],
                                        'action': value['action'], 'receipt_result_equal': True,
                                        'other_user_denied': True, 'get_no_write': True})
        checkpoint()
    for observer in (other_user, unauthorized):
        parent_only = task(observer, request_id)
        require(parent_only.get('module_executions') == [] and 'summary' not in parent_only,
                'parent-only reader received hidden child facts or counters')
    return rows


def run(client, other_user, unauthorized, tenant, deployment, report, checkpoint=lambda: None):
    require(tenant > 1, 'Cleanup acceptance must not use the default Tenant')
    principal = S.user_identity(client, tenant, PERMISSIONS)
    other = S.user_identity(other_user, tenant, PERMISSIONS)
    outsider = S.user_identity(unauthorized, tenant, {'system.cleanup.read'})
    require(len({principal, other, outsider}) == 3, 'Cleanup identities must be distinct Users')
    report.update(schema_version='addp.cleanup-execution-online/v1', result='failed',
                  scope='gateway-scan-logical-execute-authorization-results-expiry', tenant_id=str(tenant),
                  principal_id=principal, checks=[], requests=[], owner_results=[], control_cleanup='pending',
                  hosted_destruction='required', physical_effect_acceptance='not_run')
    try:
        report['stage'] = 'ten_owner_scan'
        request_id = str(uuid.uuid4())
        body = {'request_id': request_id, 'scope': list(S.OWNERS)}
        report['requests'].append(request_id)
        checkpoint()
        accepted(client, 'scan', body)
        accepted(client, 'scan', body)
        client.request('POST', ROOT + '/scan', (409,), {'request_id': request_id, 'scope': ['graph']})
        completed = S.wait_for(lambda: task(client, request_id), lambda x: x.get('status') in S.TERMINAL,
                              'Cleanup assessment did not settle', timeout=90)
        require(completed['status'] == 'success' and completed.get('cutoff_at'), 'not all Cleanup Owners succeeded')
        rows = verify_results(client, other_user, unauthorized, tenant, deployment, completed, report, checkpoint)
        report['checks'].append('ten_owner_scan_idempotence_and_private_results')

        execute_id = str(uuid.uuid4())
        report['stage'] = 'ten_owner_logical_execute'
        report['requests'].append(execute_id)
        execute_body = {'request_id': execute_id, 'based_on_scan': request_id, 'cleanup_mode': 'logical_cleanup',
                        'confirmed': True, 'confirmation_token': 'CONFIRM'}
        checkpoint()
        accepted(client, 'execute', execute_body)
        accepted(client, 'execute', execute_body)
        executed = S.wait_for(lambda: task(client, execute_id), lambda x: x.get('status') in S.TERMINAL,
                             'Cleanup execution did not settle', timeout=360)
        require(executed['status'] == 'success' and executed.get('cutoff_at'), 'not all Cleanup execute Owners succeeded')
        verify_results(client, other_user, unauthorized, tenant, deployment, executed, report, checkpoint)
        report['checks'].append('ten_owner_logical_execute_one_shot')

        meta_id, denied_id = str(uuid.uuid4()), str(uuid.uuid4())
        report['stage'] = 'meta_manager_pairing'
        report['requests'].append(meta_id)
        checkpoint()
        accepted(client, 'scan', {'request_id': meta_id, 'scope': ['meta']})
        meta_scan = S.wait_for(lambda: task(client, meta_id), lambda x: x.get('status') in S.TERMINAL,
                              'Meta-only assessment did not settle', timeout=90)
        require(meta_scan['status'] == 'success', 'Meta-only assessment failed before pairing assertion')
        refusal = client.request('POST', ROOT + '/execute', (409,), dict(execute_body, request_id=denied_id, based_on_scan=meta_id)).payload
        require(refusal.get('error_code') == 'cleanup_manager_scope_required', 'Meta-only rejection had an unrelated cause')
        count = deployment.read(f"SELECT count(*) FROM system.cleanup_requests WHERE tenant_id={tenant} AND request_id='{denied_id}'")
        require(type(count) is int and count == 0, 'Meta-only rejection still created a Cleanup request')
        count = deployment.read(f"SELECT count(*) FROM common.task_executions WHERE tenant_id={tenant} AND source='system' AND task_type IN ('cleanup','cleanup_executor') AND ((execution_config->>'cleanup_request')::jsonb->>'request_id')='{denied_id}'")
        require(type(count) is int and count == 0, 'Meta-only rejection still created public executions')
        report['checks'].append('meta_only_execute_refused_without_request')

        # Freeze a terminal execution so concurrent legitimate work cannot be
        # confused with a GET mutation. Pausing an Owner must make aggregation
        # unavailable, never an apparently successful empty set.
        graph_child = next(x for x in rows if x['module'] == 'graph')
        report['stage'] = 'owner_read_outage'
        before = deployment.execution(tenant, graph_child['execution_id'], 'graph', 'cleanup_executor')
        with deployment.paused_owner('graph-backend'):
            response = client.request('GET', ROOT + '/tasks/' + request_id, (503,)).payload
            S.safe_public(response)
            require('module_executions' not in response and 'summary' not in response, 'Owner outage returned partial detail')
            require(deployment.execution(tenant, graph_child['execution_id'], 'graph', 'cleanup_executor') == before,
                    'failed aggregate GET advanced state')
        report['checks'].append('owner_outage_fails_closed_without_writes')
        checkpoint()

        # Asset has one embedded Owner in this profile. Pause only that runtime,
        # create the exact request, wait the real frozen 30-second deadline, then
        # resume. Never edit deadlines or forge a rejection/receipt in PostgreSQL.
        expired_id = str(uuid.uuid4())
        report['stage'] = 'unclaimed_deadline'
        report['requests'].append(expired_id)
        checkpoint()
        with deployment.paused_owner('asset-backend'):
            accepted(client, 'scan', {'request_id': expired_id, 'scope': ['asset']})
            record = S.wait_for(lambda: deployment.read(f"SELECT row_to_json(r) FROM (SELECT parent_execution_id FROM system.cleanup_requests WHERE tenant_id={tenant} AND request_id='{expired_id}') r"),
                                lambda x: isinstance(x, dict), 'Cleanup request missing')
            parent = S.identifier(record['parent_execution_id'])
            S.wait_for(lambda: deployment.execution(tenant, parent, 'system', 'cleanup'),
                       lambda x: x['status'] in S.TERMINAL, 'frozen Cleanup deadline did not close', timeout=90)
            child = deployment.read(f"SELECT row_to_json(e) FROM (SELECT execution_id FROM common.task_executions WHERE tenant_id={tenant} AND parent_execution_id='{parent}' AND module='asset' AND task_type='cleanup_executor') e")
            child_id = S.identifier(child['execution_id'])
            snapshot = deployment.execution(tenant, child_id, 'asset', 'cleanup_executor')
            assert_unstarted(snapshot)
            require(snapshot['event_count'] == 0, 'unclaimed child emitted process events')
        rejected = S.wait_for(lambda: task(client, expired_id),
                             lambda x: any(r.get('status') == 'failed' for r in x.get('module_executions', [])),
                             'resumed Owner did not reject the closed request', timeout=60)
        row, = children(rejected, ['asset'])
        assert_rejected(row)
        snapshot = deployment.execution(tenant, child_id, 'asset', 'cleanup_executor')
        require(snapshot['event_count'] == 0, 'Owner rejection fabricated started/completed process events')
        client.request('GET', f'/api/v1/asset/cleanup-executions/{child_id}/result', (409,))
        report['checks'].append('real_deadline_unclaimed_owner_rejection')
        report['result'] = 'passed'
    except Exception as error:
        report.update(result='failed', failure_stage=report.get('stage'), error_type=type(error).__name__)
        raise
    finally:
        primary_failure = sys.exc_info()[0] is not None
        try:
            deployment.close()
            report['control_cleanup'] = 'passed'
        except Exception as error:
            report.update(result='failed', control_cleanup='failed', cleanup_error_type=type(error).__name__)
            if not primary_failure:
                raise
        finally:
            checkpoint()
    return report


def run_revocation(user, controller, tenant, assignment_id, deployment, report, checkpoint):
    """Revoke one exact fixture assignment after queueing, before Owner claim."""
    principal = S.user_identity(user, tenant, PERMISSIONS)
    context = user.request('GET', '/api/v1/system/auth/context', (200,)).payload
    membership = context['context']['tenant_membership_id']
    admin = controller.request('GET', '/api/v1/system/auth/context', (200,)).payload
    assignments = admin['authorization']['role_assignments']
    roles = {x['role_key'] for x in assignments}
    permissions = {p for x in assignments for p in x['permissions']}
    require(admin['principal']['type'] == 'user' and admin['context']['type'] == 'tenant'
            and admin['context']['tenant_id'] == str(tenant) and str(admin['principal']['id']) != principal
            and admin['token']['type'] == 'first_party_access_token' and 'tenant.administrator' in roles
            and not roles.intersection(S.HTTP.FORBIDDEN_ADMIN_ROLES - {'tenant.administrator'})
            and {'iam.tenant_role_assignment.read', 'iam.tenant_role_assignment.revoke'} <= permissions,
            'revocation controller is not the disposable Tenant administrator')
    require(type(assignment_id) is str and re.fullmatch(r'[1-9][0-9]*', assignment_id), 'invalid fixture assignment identity')
    path = '/api/v1/system/tenant/role_assignments/' + assignment_id
    def check(value, status):
        require(value.get('id') == assignment_id and value.get('principal_id') == principal
                and value.get('membership_id') == membership and value.get('principal_type') == 'user'
                and value.get('username') == 'external-online-cleanup-revokee'
                and value.get('role_key') == 'online.external_online_cleanup_revokee'
                and value.get('scope_type') == 'tenant' and value.get('status') == status,
                'revocation target differs from the exact fixture assignment')
    check(controller.request('GET', path, (200,)).payload, 'active')
    report['stage'] = 'queued_user_revocation'
    report['result'] = 'failed'
    request_id = str(uuid.uuid4())
    report['requests'].append(request_id)
    checkpoint()
    with deployment.paused_owner('asset-backend'):
        accepted(user, 'scan', {'request_id': request_id, 'scope': ['asset']})
        record = deployment.read(f"SELECT row_to_json(r) FROM (SELECT parent_execution_id FROM system.cleanup_requests WHERE tenant_id={tenant} AND request_id='{request_id}') r")
        parent = S.identifier(record['parent_execution_id'])
        child = deployment.read(f"SELECT row_to_json(e) FROM (SELECT execution_id FROM common.task_executions WHERE tenant_id={tenant} AND parent_execution_id='{parent}' AND module='asset' AND task_type='cleanup_executor') e")
        child_id = S.identifier(child['execution_id'])
        assert_unstarted(deployment.execution(tenant, child_id, 'asset', 'cleanup_executor'))
        report['uncertain_revocation'] = True
        checkpoint()
        check(controller.request('POST', path + '/revoke', (200,), {'reason': 'Disposable Cleanup original User revocation acceptance'}).payload, 'revoked')
        report['uncertain_revocation'] = False
        checkpoint()
        user.request('GET', ROOT + '/tasks/' + request_id, (401,))
        require(deployment.execution(tenant, parent, 'system', 'cleanup')['status'] in {'pending', 'running'},
                'request expired before revocation could be tested')
    rejected = S.wait_for(lambda: deployment.execution(tenant, child_id, 'asset', 'cleanup_executor'),
                         lambda x: x['status'] == 'failed', 'Owner did not reject revoked requester', timeout=20)
    require(rejected['attempt'] == 0 and rejected['started_at'] is None and rejected['event_count'] == 0
            and rejected['error_code'] == 'cleanup_authorization_denied', 'revoked requester was claimed or rejected for another reason')
    report['checks'].append('queued_original_user_revoked_before_claim')
    report['result'] = 'passed'
    checkpoint()


def main():
    report = {'result': 'failed', 'hosted_destruction': 'required'}
    deployment, exit_code = None, 1
    output = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']) / 'cleanup-execution.json'
    def checkpoint():
        output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
    try:
        deployment = S.OwnedDeployment()
        gateway = os.environ['GATEWAY_URL']
        clients = [S.local_client(gateway, os.environ[key], 45) for key in (
            'ADDP_ONLINE_CLEANUP_ACCESS_TOKEN', 'ADDP_ONLINE_CLEANUP_OTHER_ACCESS_TOKEN', 'ADDP_ONLINE_CLEANUP_READ_ACCESS_TOKEN')]
        run(*clients, int(os.environ['ADDP_ONLINE_TEST_TENANT_ID']), deployment, report, checkpoint)
        run_revocation(S.local_client(gateway, os.environ['ADDP_ONLINE_CLEANUP_REVOKEE_ACCESS_TOKEN']),
                       S.local_client(gateway, os.environ['ADDP_ONLINE_CLEANUP_IAM_ACCESS_TOKEN']),
                       int(os.environ['ADDP_ONLINE_TEST_TENANT_ID']), os.environ['ADDP_ONLINE_CLEANUP_ASSIGNMENT_ID'],
                       deployment, report, checkpoint)
        exit_code = 0
    except Exception as error:
        report.update(result='failed', error_type=type(error).__name__)
        report.setdefault('failure_stage', report.get('stage', 'identity_preflight'))
    finally:
        if deployment is not None:
            try:
                deployment.close()
            except Exception as error:
                report.update(result='failed', control_cleanup='failed', cleanup_error_type=type(error).__name__)
                exit_code = 1
        checkpoint()
        print(json.dumps(report, sort_keys=True))
    return exit_code


if __name__ == '__main__':
    raise SystemExit(main())
