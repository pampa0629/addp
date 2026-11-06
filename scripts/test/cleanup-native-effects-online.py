"""Native effects through the approved Engine and explicit task-delete entries.

Only disposable Hosted resources from the private fixture manifest are touched.
Unknown effects retain their original operation and are never automatically
retried. Physical evidence comes from direct native probes, not API timeouts.
"""
from importlib import import_module
import hashlib
import json
import os
from pathlib import Path
import re
import time
import uuid
from urllib.error import HTTPError
from urllib.parse import quote

S = import_module('scripts.test.execution-online-support')
PROTOCOL = import_module('scripts.test.cleanup-execution-online')
MANAGER = import_module('scripts.test.manager-internal-artifact-lineage-online')
TRANSFER = import_module('scripts.test.transfer-continuous-events-online')
GRANTS = import_module('scripts.test.ftp-consumer-flow-online')
REGISTRATION = import_module('scripts.test.online-engine-registration')
require = S.require
SUITE = 'cleanup-native-effects'
CASES = set(S.CONTRACT['native_case_checks'])
ENGINE = '/api/v1/system/engines'
TASKS = '/api/v1/transfer/task-definitions'


def failure_diagnostics(error):
    known = (S.SuiteError, MANAGER.SuiteError, TRANSFER.SuiteError, REGISTRATION.RegistrationError)
    if not isinstance(error, known):
        return {'failure_kind': 'unexpected_exception'}
    message = str(error)
    request = r'(?:GET|POST|PUT|PATCH|DELETE) /[^\r\n]+'
    status = re.fullmatch(request + r' returned HTTP ([1-5][0-9]{2}) \([^()\r\n]*\)', message)
    if status:
        return {'failure_kind': 'http_unexpected_status', 'failure_http_status': int(status.group(1))}
    if re.fullmatch(request + r' returned invalid JSON', message):
        return {'failure_kind': 'http_invalid_json'}
    if re.fullmatch(request + r' transport failed: [^\r\n]*', message):
        return {'failure_kind': 'http_transport'}
    return {'failure_kind': 'assertion_failed'}


def model_prepare_diagnostics(error):
    """Project an existing HTTP failure onto fixed model preparation labels."""
    if failure_diagnostics(error).get('failure_kind') != 'http_unexpected_status':
        return None
    match = re.fullmatch(r'(GET|POST|PUT|PATCH|DELETE) (/[^\r\n]+) returned HTTP ([1-5][0-9]{2}) \(([^()\r\n]*)\)', str(error))
    if not match:
        return dict(operation='unrecognized', error_code='unrecognized')
    method, path, _, code = match.groups()
    path = path.split('?', 1)[0]
    operations = {
        '/api/v1/manager/model_3d_glb': 'manager_model_results_read',
        '/api/v1/manager/tasks': 'manager_model_tasks_read',
        '/api/v1/manager/downloads/file': 'manager_source_download',
    }
    operation = 'unrecognized'
    if method == 'GET':
        operation = ('meta_items_read' if re.fullmatch(r'/api/v1/meta/engines/[1-9][0-9]*/items', path)
                     else operations.get(path, 'unrecognized'))
    codes = {'permission_denied', 'security_protection_required', 'source_authorization_required',
             'source_composition_incomplete', 'engine_unavailable', 'authorization_service_unavailable',
             'download_bundle_invalid', 'download_source_changed', 'unknown'}
    return dict(operation=operation, error_code=code if code in codes else 'unrecognized')


def manager_inventory_diagnostics(probe, error):
    reasons = {
        'private Hosted control failed': 'private_read_failed',
        'native GLB target missing': 'storage_reference_missing',
        'native artifact requires the canonical Manager object reference': 'storage_reference_invalid',
        'artifact is outside this Tenant output scope': 'storage_reference_outside_scope',
        'native artifact unexpectedly exists or is empty': 'object_empty',
        'native artifact absence is not proven': 'object_unproven',
        'native index count is incomplete': 'index_response_incomplete',
        'native index returned a foreign projection': 'index_scope_mismatch',
        'real source indexing never completed': 'index_not_observed',
        'native artifact is not a live generated result': 'artifact_not_live',
    }
    if isinstance(error, HTTPError) and type(error.code) is int and 100 <= error.code <= 599:
        return dict(probe=probe, reason='http_error', http_status=error.code)
    reason = reasons.get(str(error), 'unrecognized') if isinstance(error, S.SuiteError) else 'unrecognized'
    return dict(probe=probe, reason=reason)


def assert_proxy(value, run_id, key):
    require(type(value) is dict and set(value) == {'run_id', 'key', 'forwarded', 'upstream_response', 'blocked_replays'}
            and value['run_id'] == run_id and value['key'] == key
            and type(value['forwarded']) is int and value['forwarded'] == 1
            and value['upstream_response'] is True
            and type(value['blocked_replays']) is int and value['blocked_replays'] >= 0,
            'fault did not observe exactly one sent request and its real upstream response')


def engine_owners_match(owners):
    expected = S.CONTRACT['cleanup_owners_by_cause']['engine.deleting']
    return (type(owners) is list and all(type(owner) is str for owner in owners)
            and len(owners) == len(expected) and set(owners) == set(expected))


class EngineWorkflow:
    def __init__(self, user, tenant, report, checkpoint):
        self.user, self.tenant, self.report, self.checkpoint = user, S.positive(tenant), report, checkpoint
        self.principal = S.user_identity(user, tenant, S.CONTRACT['native_permissions']['engine'])
        self.owned = set()
        self.cleanup_observation = {}

    def _stage(self, stage):
        self.report['stage'] = stage
        self.checkpoint()

    def register(self, descriptor):
        require(type(descriptor) is dict and descriptor.get('engine_type') in {'minio', 'postgresql', 'kafka'}
                and descriptor.get('engine_origin') == 'general', 'unsupported native fixture Engine')
        created = self.user.request('POST', ENGINE, (201,), descriptor).payload
        engine_id = S.positive(created['id'])
        require(created['engine_type'] == descriptor['engine_type'] and created['tenant_id'] == self.tenant
                and created['lifecycle_state'] == 'active', 'registered Engine identity or lifecycle differs')
        self.owned.add(engine_id)
        self.report['engines'].append(engine_id); self.checkpoint()
        return engine_id

    def engine(self, engine_id, *, deadline=None):
        require(engine_id in self.owned, 'Engine mutation outside this run')
        reader = self.user
        if deadline is not None:
            remaining = deadline - time.monotonic()
            require(remaining > 0, 'native Engine observation deadline expired')
            reader = S.local_client(self.user.base_url, self.user.token, min(self.user.timeout, remaining))
        value = reader.request('GET', ENGINE + '/' + str(engine_id), (200,)).payload
        require(value['id'] == engine_id and value['tenant_id'] == self.tenant, 'Engine read identity changed')
        if deadline is not None:
            require(time.monotonic() < deadline, 'native Engine observation exceeded preparation deadline')
        return value

    def begin(self, engine_id):
        self._stage('engine_before_assessment_read')
        before = self.engine(engine_id)
        require(before['lifecycle_state'] == 'active' and type(before['version']) is int and before['version'] > 0,
                'Engine is not an active exact-version fixture')
        request_id = str(uuid.uuid4())
        self.report['requests'].append(request_id); self.checkpoint()
        self._stage('engine_assessment_create')
        accepted = self.user.request('POST', f'{ENGINE}/{engine_id}/deletion-assessments', (202,),
                                    {'request_id': request_id, 'external_artifact_policy': 'delete'}).payload
        require(accepted == {'assessment_id': request_id}, 'assessment identity changed')
        self._stage('engine_assessment_read')
        last = dict(request_id=request_id, observation='not_observed')
        def read_assessment():
            nonlocal last
            value = self.user.request('GET', f'{ENGINE}/{engine_id}/deletion-assessments/{request_id}', (200,)).payload
            last = engine_task_observation(value, request_id, engine_id, self.principal, action='scan')
            return value
        try:
            assessment = S.wait_for(read_assessment,
                lambda x: x.get('status') in S.TERMINAL, 'Engine assessment did not settle')
            task = assessment['task']
            require(assessment['status'] == 'success' and task['context']['engine_id'] == engine_id
                    and task['requested_by'] == int(self.principal) and task['action'] == 'scan'
                    and engine_owners_match(task['expected_modules']), 'Engine assessment is incomplete or foreign')
        except Exception:
            self.report['engine_assessment_diagnostic'] = last
            self.checkpoint()
            raise
        # The server validates the complete committed impact digest. Public
        # observations are independently scoped; professional reads remain
        # limited to Manager/Transfer and cannot reconstruct that digest.
        self._stage('engine_before_delete_read')
        current = self.engine(engine_id)
        require(current['version'] == before['version'], 'Engine changed after assessment')
        self._stage('engine_delete_confirm')
        result = self.user.request('DELETE', f'{ENGINE}/{engine_id}', (202,),
            {'version': current['version'], 'assessment_id': request_id,
             'confirmation_token': current['name'], 'external_artifact_policy': 'delete'}).payload
        require(result['engine']['id'] == engine_id and result['engine']['lifecycle_state'] == 'deleting',
                'Engine deletion was not accepted for the exact target')
        return request_id

    def _cleanup_engine(self, engine_id):
        engine = self.engine(engine_id)
        def reference(field):
            value = engine.get(field)
            if value is None: return 'absent'
            try: S.identifier(value)
            except Exception: return 'invalid'
            return 'present'
        lifecycle, reason = engine.get('lifecycle_state'), engine.get('deletion_error', '')
        self.cleanup_observation['engine'] = dict(
            lifecycle_state=lifecycle if type(lifecycle) is str and lifecycle in {'active', 'deleting', 'deleted'} else 'unrecognized',
            deletion_error=reason if type(reason) is str and reason in {'', 'cleanup_scan_incomplete_or_failed',
                'cleanup_execution_rejected', 'cleanup_execution_incomplete_or_failed', 'engine_deletion_finalize_failed'} else 'unrecognized',
            validation_reference=reference('deletion_scan_task_id'), execute_reference=reference('deletion_execute_task_id'))
        return engine

    def execution(self, engine_id, *, deadline):
        self._stage('engine_cleanup_engine_read')
        engine = self._cleanup_engine(engine_id)
        diagnostic = self.cleanup_observation
        request_id = engine.get('deletion_execute_task_id')
        if request_id is None:
            remaining = deadline - time.monotonic()
            if diagnostic['engine']['validation_reference'] == 'present' and remaining > 0:
                self._stage('engine_cleanup_validation_read')
                diagnostic.pop('task_read_failure', None)
                try:
                    reader = S.local_client(self.user.base_url, self.user.token, min(self.user.timeout, remaining))
                    validation_id = engine['deletion_scan_task_id']
                    value = PROTOCOL.task(reader, validation_id)
                    diagnostic.update(task_phase='validation', task=engine_task_observation(
                        value, validation_id, engine_id, self.principal, action='scan'))
                except Exception as error:
                    diagnostic['task_read_failure'] = failure_diagnostics(error)
            return None
        S.identifier(request_id)
        self._stage('engine_cleanup_task_read')
        result = PROTOCOL.task(self.user, request_id)
        diagnostic.update(task_phase='execute', task=engine_task_observation(
            result, request_id, engine_id, self.principal, action='execute'))
        diagnostic.pop('task_read_failure', None)
        require(result['task']['context']['engine_id'] == engine_id
                and result['task']['requested_by'] == int(self.principal)
                and result['task']['action'] == 'execute'
                and engine_owners_match(result['task']['expected_modules']), 'Engine execute ownership changed')
        return result

    def settled(self, engine_id, *, success):
        self.cleanup_observation = dict(engine_id=engine_id, engine={'observation': 'not_observed'},
                                        task_phase='not_observed')
        deadline = time.monotonic() + 360
        try:
            result = S.wait_for(lambda: self.execution(engine_id, deadline=deadline),
                lambda x: x is not None and x['status'] in S.TERMINAL,
                'Engine execute did not settle', timeout=360)
            require((result['status'] == 'success') is success, 'Engine execute has an unexpected outcome')
            self._stage('engine_tombstone_read')
            if success:
                S.wait_for(lambda: self._cleanup_engine(engine_id), lambda x: x['lifecycle_state'] == 'deleted',
                           'strict Engine completion did not create the tombstone')
            else:
                require(self._cleanup_engine(engine_id)['lifecycle_state'] == 'deleting', 'unknown effect incorrectly deleted Engine')
            return result
        except Exception:
            self.report['engine_cleanup_diagnostic'] = self.cleanup_observation
            self.checkpoint()
            raise


def engine_selected_children(value):
    """Validate the complete authorized public projection; select no extra reads."""
    task, rows = value.get('task'), value.get('module_executions')
    owners = S.CONTRACT['cleanup_owners_by_cause']['engine.deleting']
    def identifier(candidate):
        try: return type(candidate) is str and str(uuid.UUID(candidate)) == candidate and uuid.UUID(candidate).int != 0
        except ValueError: return False
    if type(task) is not dict or not identifier(task.get('execution_id')):
        return None, 'invalid_parent_identity'
    if type(rows) is not list or len(rows) > len(owners):
        return None, 'invalid_collection'
    modules, identities = set(), set()
    for row in rows:
        if type(row) is not dict or type(row.get('module')) is not str:
            return None, 'invalid_child_shape'
        if row['module'] not in owners:
            return None, 'unexpected_owner'
        if row.get('task_type') != 'cleanup_executor':
            return None, 'wrong_task_type'
        if row.get('parent_execution_id') != task['execution_id']:
            return None, 'foreign_parent'
        if not identifier(row.get('execution_id')):
            return None, 'invalid_execution_identity'
        if row['module'] in modules:
            return None, 'duplicate_owner'
        if row['execution_id'] in identities:
            return None, 'duplicate_execution'
        modules.add(row['module']); identities.add(row['execution_id'])
    return [row for row in rows if row['module'] in {'manager', 'transfer'}], None


def engine_task_observation(value, request_id, engine_id, principal, *, action):
    """Project the existing authorized read; never query hidden Owner facts."""
    require(action in {'scan', 'execute'}, 'invalid Engine task observation action')
    result = dict(request_id=request_id)
    if type(value) is not dict or type(value.get('task')) is not dict:
        return {**result, 'observation': 'invalid_response'}
    task = value['task']
    def closed(candidate, allowed):
        return candidate if type(candidate) is str and candidate in allowed else 'unrecognized'
    states = {'pending', 'running', 'success', 'failed', 'timeout', 'cancelled'}
    context, owners = task.get('context'), task.get('expected_modules')
    bindings = dict(request=value.get('task_id') == task.get('task_id') == request_id,
        engine=type(context) is dict and type(context.get('engine_id')) is int and context['engine_id'] == engine_id,
        actor=type(task.get('requested_by')) is int and task['requested_by'] == int(principal),
        action=task.get('action') == action,
        owners=engine_owners_match(owners))
    result.update(observation='observed' if all(bindings.values()) else 'identity_mismatch',
                  status=closed(value.get('status'), states), bindings=bindings)
    if not all(bindings.values()): return result
    result.update(children_observation='not_observed', children=[])
    if 'module_executions' not in value: return result
    rows, failure = engine_selected_children(value)
    if failure is not None:
        result.update(children_observation='invalid_response', children_failure_kind=failure)
        return result
    result['children_observation'] = 'observed'
    for row in sorted(rows, key=lambda row: row['module']):
        receipt = row.get('receipt') if type(row.get('receipt')) is dict else {}
        admission = row.get('cleanup_admission') if type(row.get('cleanup_admission')) is dict else {}
        details = row.get('error_details') if type(row.get('error_details')) is dict else {}
        result['children'].append(dict(module=row['module'], status=closed(row.get('status'), states),
            attempt=row['attempt'] if type(row.get('attempt')) is int and row['attempt'] in {0, 1} else 'unrecognized',
            started=row.get('started_at') is not None,
            receipt_status=closed(receipt.get('status'), states) if receipt else 'not_observed',
            receipt_reason=closed(receipt.get('reason', ''), {'', 'owner_failed', 'owner_unavailable', 'authorization_denied',
                'active_execution', 'external_effect_uncertain', 'scope_changed', 'unsupported_scope'}) if receipt else 'not_observed',
            admission_state=closed(admission.get('state'), {'rejected_unstarted', 'closed_rejected', 'closed_unclaimed'})
                if admission else 'not_observed',
            authorization_denied=details.get('code') == 'cleanup_authorization_denied',
            authorization_unavailable=details.get('code') == 'cleanup_authorization_unavailable'))
        step = row.get('current_step')
        stages = {
            'manager': {'manager_cleanup_plan', 'manager_cleanup_inventory', 'manager_cleanup_engine_facts',
                'manager_cleanup_confirmation', 'manager_cleanup_commit', 'manager_cleanup_artifact',
                'manager_cleanup_index_effect', 'manager_cleanup_finalize'},
            'transfer': {'transfer_cleanup_plan', 'transfer_cleanup_stop', 'transfer_cleanup_capture_plan',
                'transfer_cleanup_capture_connector', 'transfer_cleanup_capture_source', 'transfer_cleanup_capture_group',
                'transfer_cleanup_capture_topic', 'transfer_cleanup_capture_acl', 'transfer_cleanup_capture_history_topic',
                'transfer_cleanup_capture_history_acl', 'transfer_cleanup_dlq', 'transfer_cleanup_commit'},
        }.get(row['module'], set())
        steps = row.get('steps')
        if (row['module'] in {'manager', 'transfer'} and row.get('status') == 'failed' and row.get('started_at') is not None
                and type(row.get('attempt')) is int and row['attempt'] == 1
                and type(step) is str and step in stages and type(steps) is list
                and len(steps) == 1 and type(steps[0]) is dict and steps[0].get('id') == step
                and steps[0].get('status') == 'failed' and steps[0].get('phase') == 'terminal'):
            result['children'][-1]['failure_step'] = step
    return result


def professional(user, completed, module, tenant):
    rows, failure = engine_selected_children(completed)
    require(module in {'manager', 'transfer'} and failure is None
            and {row['module'] for row in rows} == {'manager', 'transfer'},
            'Engine professional result references are incomplete or foreign')
    row, = [row for row in rows if row['module'] == module]
    value = user.request('GET', f"/api/v1/{module}/cleanup-executions/{S.identifier(row['execution_id'])}/result", (200,)).payload
    PROTOCOL.assert_result(value, row, completed['task_id'], tenant)
    return value


def wait_paused(user, task_id, controls):
    user.request('POST', f'{TASKS}/{S.positive(task_id)}/pause', (200,))
    value = S.wait_for(lambda: user.request('GET', f'{TASKS}/{task_id}', (200,)).payload,
        lambda x: x.get('desired_state') == 'paused' and x.get('status') == 'idle', 'native task did not pause')
    controls.require_invocations_stopped(task_id)
    return value


def transfer_runtime_observation(value, task_id, execution_id):
    """Project only this execution's closed public states, never private prose."""
    result = {'execution_id': execution_id, 'task_id': task_id}
    if type(value) is not dict:
        return {**result, 'observation': 'invalid_response'}
    if (value.get('execution_id') != execution_id or type(value.get('task_id')) is not int
            or value['task_id'] != task_id):
        return {**result, 'observation': 'identity_mismatch'}

    def enum(value, allowed):
        return value if type(value) is str and value in allowed else 'unrecognized'

    def fields(value, allowed):
        if type(value) is not dict:
            return {}
        return {key: enum(value[key], choices) for key, choices in allowed.items() if key in value}

    # These are the existing ExecutionResponse / Common observation enums.
    result.update(observation='observed', status=enum(value.get('status'),
                  {'pending', 'running', 'success', 'failed', 'timeout', 'cancelled'}))
    codes = {'schema_change_blocked', 'lease_expired', 'invalid_json_object', 'multiple_json_values',
             'unknown_source_field', 'missing_source_key', 'missing_required_field',
             'incompatible_field_type', 'null_target_key'}
    result['error_details'] = fields(value.get('error_details'), {
        'category': {'owner_unavailable', 'submission_uncertain', 'coordinator_lost', 'child_failed',
                     'permission_denied', 'timeout', 'cancelled', 'connection_failed', 'resource_exhausted',
                     'invalid_input', 'execution_failed'},
        'code': codes, 'error_code': codes})
    metadata = value.get('metadata')
    result['metadata'] = fields(metadata, {
        'stop_reason': {'paused', 'stopped', 'cleanup', 'worker_shutdown', 'lease_expired',
                        'execution_failed', 'schema_change_blocked'},
        'recovery_reason': {'worker_shutdown', 'lease_expired', 'execution_failed'}})
    if type(metadata) is dict and 'metadata_scan' in metadata:
        result['metadata']['metadata_scan'] = fields(metadata['metadata_scan'], {
            'status': {'submitted', 'failed'},
            'error_code': {'scan_origin_unavailable', 'scan_output_unavailable',
                           'scan_service_unavailable', 'scan_submission_failed'}})
    return result


def wait_transfer_runtime(user, task_id, execution_id, report, checkpoint):
    last = {'execution_id': execution_id, 'task_id': task_id, 'observation': 'not_observed'}
    def read():
        nonlocal last
        value = user.request('GET', f'/api/v1/transfer/executions/{execution_id}', (200,)).payload
        last = transfer_runtime_observation(value, task_id, execution_id)
        require(last['observation'] == 'observed', 'native Transfer execution response identity differs')
        return value
    try:
        # Keep the canonical 120s wait, original running predicate and GET cadence.
        S.wait_for(read, lambda x: x.get('status') == 'running', 'native Transfer runtime did not start')
    except Exception:
        report['runtime_start_diagnostic'] = last
        checkpoint()
        raise


def wait_initial_records(user, task_id, execution_id, kind, report, checkpoint):
    """Keep the physical predicate; observe only the same owned execution."""
    require(kind in {'cdc', 'kafka'}, 'invalid native initial record kind')
    deadline = time.monotonic() + 120  # The unchanged canonical wait window.
    diagnostic = dict(task_kind=kind, physical_probe='not_observed', runtime=dict(
        execution_id=execution_id, task_id=task_id, observation='not_observed'))

    def probe():
        diagnostic['physical_probe'] = 'not_observed'
        ready = TRANSFER.physical('verify-' + kind + '-initial',
            observe=lambda outcome: diagnostic.update(physical_probe=outcome))
        remaining = deadline - time.monotonic()
        if not ready and remaining > 0:
            diagnostic.pop('runtime_read_failure', None)
            try:
                reader = S.local_client(user.base_url, user.token, min(user.timeout, remaining))
                value = reader.request('GET', f'/api/v1/transfer/executions/{execution_id}', (200,)).payload
                diagnostic['runtime'] = transfer_runtime_observation(value, task_id, execution_id)
            except Exception as error:
                # A diagnostic read must not change the original row predicate.
                diagnostic['runtime_read_failure'] = failure_diagnostics(error)
        return ready

    try:
        S.wait_for(probe, bool, 'native ' + ('CDC' if kind == 'cdc' else 'Kafka') + ' target rows were not committed')
    except Exception:
        report['initial_records_diagnostic'] = diagnostic
        checkpoint()
        raise


def wait_native_engines(workflow, engines, deadline, report, checkpoint):
    """Observe registration readiness inside the existing source-scan budget."""
    require(type(engines) is list and 1 <= len(engines) <= 2
            and all(type(identity) is int and identity > 0 and kind in {'minio', 'postgresql', 'kafka'}
                    for identity, kind in engines)
            and len({identity for identity, _ in engines}) == len(engines), 'invalid native Engine readiness scope')
    last = [dict(engine_id=identity, engine_type=kind, observation='not_observed') for identity, kind in engines]
    try:
        while time.monotonic() < deadline:
            for i, (identity, kind) in enumerate(engines):
                value = workflow.engine(identity, deadline=deadline)  # Same User, exact owned ID and original window.
                require(type(value) is dict and value.get('engine_type') == kind, 'fixture Engine type changed')
                def state(field, allowed):
                    candidate = value.get(field)
                    return candidate if type(candidate) is str and candidate in allowed else 'unrecognized'
                last[i] = dict(engine_id=identity, engine_type=kind, observation='observed',
                    lifecycle_state=state('lifecycle_state', {'active', 'deleting', 'deleted'}),
                    connection_status=state('connection_status', {'unknown', 'checking', 'offline', 'online'}))
                require(last[i]['lifecycle_state'] == 'active', 'fixture Engine is no longer active')
            if all(row['connection_status'] == 'online' for row in last):
                return
            time.sleep(1)
        raise S.SuiteError('native Engines were not ready before the source preparation deadline')
    except Exception:
        report['engine_readiness_diagnostic'] = {'engines': last}
        checkpoint()
        raise


def task_create_rejection(payload, kind, pg, kafka):
    """Classify known validation branches without retaining server error prose."""
    source, target = (pg if kind == 'cdc' else kafka), pg
    error = payload.get('error') if type(payload) is dict else None
    reason = 'unrecognized'
    if kind == 'cdc' and type(error) is str:
        prefix = 'invalid transfer task config: '
        for endpoint, identity in (('source', source), ('target', target)):
            branch = prefix + f'resolve database CDC {endpoint} engine: '
            if error == branch + f'engine {identity} is unavailable':
                reason = endpoint + '_engine_unavailable'
            elif error.startswith(branch):
                reason = endpoint + '_engine_resolution_failed'
        if error == prefix + 'database CDC target engine "postgresql" does not declare atomic monotonic upsert/delete partitioned_table_change_apply':
            reason = 'target_apply_unsupported'
        elif error.startswith(prefix + 'continuous_kafka=') and '; database_cdc=' in error:
            reason = 'configuration_rejected'
    return dict(task_kind=kind, source_engine_id=source, target_engine_id=target, http_status=400, reason=reason)


def require_continuous_task_definition(value, task_id, tenant):
    # enabled controls task-owned scheduling; manual continuous start uses
    # desired_state and never turns this fixture's scheduling switch on.
    require(type(value) is dict and type(value.get('id')) is int and value['id'] == task_id
            and type(value.get('tenant_id')) is int and value['tenant_id'] == tenant
            and value.get('enabled') is False,
            'native continuous task definition differs from the accepted no-schedule fixture')


def prepare_task(user, controls, pg, kafka, kind, run_id, report, checkpoint):
    report['stage'] = 'transfer_task_create'; checkpoint()
    config = TRANSFER.task_config(kind, pg, kafka)
    if kind == 'kafka': config['runtime']['record_failure']['mode'] = 'dead_letter'
    name = 'native-' + run_id + '-' + kind
    response = user.request('POST', TASKS, (201, 400), {'name': name, 'task_type': 'sync',
        'enabled': False, 'auto_scan_metadata': True, 'batch_size': 100, 'config': config})
    if response.status == 400:
        report['task_create_diagnostic'] = task_create_rejection(response.payload, kind, pg, kafka)
        checkpoint()
        raise S.SuiteError(f'POST {TASKS} returned HTTP 400 (unknown)')
    require(response.status == 201, 'native Transfer task creation did not succeed')
    created = response.payload
    task_id = S.positive(created['id'])
    require_continuous_task_definition(created, task_id, controls.tenant)
    report['tasks'].append(task_id); checkpoint()
    report['stage'] = 'transfer_task_start'; checkpoint()
    started = user.request('POST', f'{TASKS}/{task_id}/start', (200,)).payload
    execution_id = S.identifier(started['execution_id'])
    report['executions'].append(execution_id); checkpoint()
    report['stage'] = 'transfer_runtime_start'; checkpoint()
    wait_transfer_runtime(user, task_id, execution_id, report, checkpoint)
    if kind == 'kafka':
        report['stage'] = 'transfer_initial_records'; checkpoint()
        controls.produce_initial()
        wait_initial_records(user, task_id, execution_id, kind, report, checkpoint)
        report['stage'] = 'transfer_dlq_records'; checkpoint()
        controls.produce_invalid()
        S.wait_for(lambda: user.request('GET', f'{TASKS}/{task_id}/dead-letters?page=1&page_size=10', (200,)).payload,
                   lambda x: x.get('total', 0) > 0, 'native DLQ record was not created')
        report['stage'] = 'transfer_dlq_native_verify'; checkpoint()
        controls.topic(task_id, present=True)
    else:
        report['stage'] = 'transfer_initial_records'; checkpoint()
        wait_initial_records(user, task_id, execution_id, kind, report, checkpoint)
        report['stage'] = 'transfer_capture_native_verify'; checkpoint()
        controls.capture(task_id, present=True)
    report['stage'] = 'transfer_task_pause'; checkpoint()
    wait_paused(user, task_id, controls)
    return task_id, execution_id


def run_dlq_unknown(user, controls, task_id, report, checkpoint):
    report['stage'] = 'direct_task_delete_unknown'; checkpoint()
    before = controls.transfer_snapshot(task_id)
    require(before['dead_letters'] > 0 and before['task_exists'] is True, 'DLQ has no retained control facts')
    report['stage'] = 'transfer_dlq_fault_arm'; checkpoint()
    key = controls.arm('dlq', topic=f'__addp_dlq.{controls.tenant}.{task_id}')
    report['stage'] = 'transfer_dlq_delete_unknown'; checkpoint()
    user.request('DELETE', f'{TASKS}/{task_id}', (503,))
    report['stage'] = 'transfer_dlq_effect_probe'; checkpoint()
    evidence = controls.fault('dlq', key)
    assert_proxy(evidence, controls.run_id, key)
    report['stage'] = 'transfer_dlq_native_absence'; checkpoint()
    controls.topic(task_id, present=False)
    report['stage'] = 'transfer_dlq_hold_verify'; checkpoint()
    after = controls.transfer_snapshot(task_id)
    require(after['task_exists'] and after['dead_letters'] == before['dead_letters']
            and after['unknown_operations'] == 1, 'unknown topic deletion erased its task, index or operation')
    # Explicit second user command must not resend the unknown native effect.
    report['stage'] = 'transfer_dlq_repeat_delete'; checkpoint()
    user.request('DELETE', f'{TASKS}/{task_id}', (503,))
    report['stage'] = 'transfer_dlq_producer_refusal'; checkpoint()
    refused = user.request('POST', f'{TASKS}/{task_id}/start', (409,))
    require(refused.payload.get('error_code') == 'transfer_cleanup_unresolved',
            'producer refusal has an unrelated cause')
    report['producer_refusal_http_status'] = 409
    report['producer_refusal_error_code'] = refused.payload['error_code']
    report['stage'] = 'transfer_dlq_hold_after_refusal'; checkpoint()
    require(controls.transfer_snapshot(task_id) == after, 'repeated command or producer changed isolated task facts')
    report['stage'] = 'transfer_dlq_no_replay'; checkpoint()
    assert_proxy(controls.fault('dlq', key), controls.run_id, key)
    report['checks'].extend(['real_dlq_created', 'direct_task_delete_sent_response_lost',
        'native_topic_absent', 'unknown_preserves_task_and_index', 'unknown_delete_not_resent', 'new_producer_rejected'])
    report['stage'] = 'transfer_target_rows_verify'; checkpoint()
    controls.target_rows('kafka')
    report['checks'].append('target_business_rows_preserved')
    report['entry'] = 'transfer_task_delete'
    checkpoint()


class NativeControls:
    """Read-only native oracles and immutable, run-owned transport bindings."""
    def __init__(self, deployment, tenant, case):
        self.deployment, self.tenant, self.run_id = deployment, S.positive(tenant), deployment.run_id
        path = deployment.secret / 'cleanup-native-fixture.json'
        require(path.is_file() and not path.is_symlink() and path.stat().st_mode & 0o777 == 0o600
                and path.stat().st_size < 32768, 'private native fixture manifest missing')
        self.manifest = json.loads(path.read_text())
        require(set(self.manifest) == {'schema', 'run_id', 'case', 'descriptors', 'source_bucket', 'proxies', 'native'}
                and self.manifest['schema'] == 'addp.cleanup-native-fixture/v1'
                and self.manifest['run_id'] == self.run_id and self.manifest['case'] == case,
                'native fixture manifest identity differs')
        self.proxies, self.captures, self.writer_keys = {}, {}, set()
        for kind, pin in self.manifest['proxies'].items():
            require(kind in {'object', 'index', 'dlq', 'capture'}, 'unknown native control')
            self.container(pin, proxy=True)

    def container(self, pin, *, proxy=False):
        require(type(pin) is dict and set(pin) == {'id', 'name', 'image', 'started_at'}
                and re.fullmatch(r'[0-9a-f]{64}', pin['id']) and re.fullmatch(r'[A-Za-z0-9_-]+', pin['name']),
                'invalid native container pin')
        row, = json.loads(self.deployment.command(['docker', 'inspect', pin['id']]))
        require(row['Id'] == pin['id'] and row['Name'] == '/' + pin['name'] and row['Image'] == pin['image']
                and row['State']['Running'] and row['State']['StartedAt'] == pin['started_at'],
                'native fixture container was replaced or restarted')
        if proxy:
            env = dict(v.split('=', 1) for v in row['Config'].get('Env', []) if '=' in v)
            labels = row['Config'].get('Labels') or {}
            require(env.get('ADDP_ONLINE_HOSTED') == '1' and env.get('ADDP_ONLINE_TEST') == '1'
                    and env.get('ADDP_ONLINE_TEST_RUN_ID') == self.run_id
                    and labels.get('com.addp.online-fixture') == SUITE
                    and labels.get('com.addp.online-run') == self.run_id,
                    'fault proxy is not owned by this disposable run')
        return row['Id']

    def descriptor(self, kind):
        filename = self.manifest['descriptors'][kind]
        require(re.fullmatch(r'[a-z0-9-]+\.json', filename) is not None, 'invalid native descriptor path')
        path = self.deployment.secret / filename
        require(path.stat().st_mode & 0o777 == 0o600 and not path.is_symlink(), 'native descriptor is not private')
        return REGISTRATION.load_descriptor(path)

    def control(self, kind, action, value):
        pin = self.manifest['proxies'][kind]
        container = self.container(pin, proxy=True)
        program = """import json,sys,urllib.request
request=urllib.request.Request('http://127.0.0.1:18087/'+sys.argv[1],data=sys.stdin.buffer.read(),headers={'Content-Type':'application/json'},method='POST')
with urllib.request.urlopen(request,timeout=5) as response: print(response.read().decode())
"""
        return json.loads(self.deployment.command(['docker', 'exec', '-i', container, 'python', '-c', program, action],
                                                 input=json.dumps(value)))

    def arm(self, kind, *, topic=None, path=None, body=b''):
        key = str(uuid.uuid4())
        binding = dict(run_id=self.run_id, key=key, protocol='kafka' if kind == 'dlq' else 'http')
        if kind == 'dlq': binding['topic'] = topic
        else:
            binding.update(method='POST' if kind == 'index' else 'DELETE', path=path,
                           correlated=kind == 'index', body_sha256=hashlib.sha256(body).hexdigest())
        value = self.control(kind, 'arm', binding)
        require(value['key'] == key and value['forwarded'] == 0 and value['upstream_response'] is False,
                'native fault binding was not fresh')
        self.proxies[kind] = key
        return key

    def arm_writer(self, path):
        key = str(uuid.uuid4())
        require('object' not in self.proxies, 'object proxy already belongs to another operation')
        self.proxies['object'] = key
        self.writer_keys.add(key)  # Retain intent even if the arm response is lost.
        value = self.control('object', 'arm', dict(run_id=self.run_id, key=key, protocol='http-write', path=path))
        require(value['key'] == key and value['entered'] == 0 and value['forwarded'] == 0
                and value['released'] is False and value['expired'] is False, 'late writer binding was not fresh')
        return key

    def release_writer(self, key):
        require(self.proxies.get('object') == key, 'foreign late writer release')
        value = self.control('object', 'release', {'run_id': self.run_id, 'key': key})
        require(value['released'] is True and value['expired'] is False, 'late writer release was not confirmed')
        self.writer_keys.discard(key)

    def close(self):
        failures = []
        for key in list(self.writer_keys):
            try: self.release_writer(key)
            except Exception: failures.append(key)
        require(not failures, 'owned writer release failed; final deployment destruction remains mandatory')

    def manager_attempt(self, execution_id):
        return self.deployment.read(f"""SELECT row_to_json(e) FROM (
 SELECT execution_id,status,attempt,started_at,completed_at,error_details->>'code' AS error_code,
 COALESCE(lease_expires_at > clock_timestamp(),false) AS lease_live,
 md5((to_jsonb(t)-'lease_token'-'execution_config')::text) AS state_digest
 FROM common.task_executions t WHERE tenant_id={self.tenant} AND execution_id='{S.identifier(execution_id)}'
 AND module='manager' AND task_type='model_3d_glb_generation') e""")

    def pending_glb(self, engine_id, execution_id):
        rows = self.deployment.read(f"""SELECT COALESCE(json_agg(a),'[]'::json) FROM (
 SELECT id,status,storage_ref,deleted_at IS NOT NULL AS deleted,
 metadata#>'{{model3d_output_lifecycle,pending}}' AS pending,
 (SELECT count(*) FROM manager.cleanup_targets WHERE tenant_id={self.tenant}
  AND operation_id='{S.identifier(execution_id)}') AS generation_holds
 FROM manager.model_3d_glb WHERE tenant_id={self.tenant} AND source_engine_id={S.positive(engine_id)}
 AND metadata#>>'{{model3d_output_lifecycle,pending,execution_id}}'='{execution_id}') a""")
        require(isinstance(rows, list) and len(rows) <= 1, 'duplicate or invalid pending GLB identity')
        return rows[0] if rows else None

    def fault(self, kind, key):
        require(self.proxies.get(kind) == key, 'foreign native operation binding')
        return self.control(kind, 'state', {'run_id': self.run_id})

    def produce_initial(self):
        require(TRANSFER.physical('produce-initial'), 'native source records were not produced')

    def produce_invalid(self):
        # Existing fixture validates exact labels and source ownership itself.
        require(TRANSFER.physical('produce-invalid'), 'native invalid source record was not produced')

    def require_invocations_stopped(self, task_id):
        task_id = S.positive(task_id)
        value = S.wait_for(lambda: self.deployment.read(f"""SELECT json_build_object(
 'total',count(*),'open',count(*) FILTER (WHERE stopped_at IS NULL))
 FROM transfer.runtime_sessions WHERE tenant_id={self.tenant} AND task_id={task_id}"""),
            lambda x: x['total'] > 0 and x['open'] == 0, 'original native runtime has no actual return proof')
        require(value['total'] > 0, 'native runtime never ran')

    def transfer_snapshot(self, task_id):
        task_id = S.positive(task_id)
        return self.deployment.read(f"""SELECT json_build_object(
 'task_exists',EXISTS(SELECT 1 FROM transfer.transfer_tasks WHERE tenant_id={self.tenant} AND id={task_id} AND deleted_at IS NULL),
 'dead_letters',(SELECT count(*) FROM transfer.dead_letters WHERE tenant_id={self.tenant} AND task_id={task_id}),
 'active_executions',(SELECT count(*) FROM common.task_executions WHERE tenant_id={self.tenant} AND module='transfer' AND task_type='sync' AND source_task_id='{task_id}' AND status IN ('pending','running')),
 'unknown_operations',(SELECT count(*) FROM transfer.cleanup_operations WHERE tenant_id={self.tenant} AND task_id={task_id} AND status='unknown'),
 'operations_digest',(SELECT md5(COALESCE(json_agg(o ORDER BY target_key)::text,'')) FROM
  (SELECT target_key,operation_id,kind,status,capture_resource_id,resource_version FROM transfer.cleanup_operations WHERE tenant_id={self.tenant} AND task_id={task_id}) o))""")

    def native_json(self, path, *, service):
        import base64
        import urllib.request
        config = self.manifest['native'][service]
        headers = {}
        if service == 'meili': headers['Authorization'] = 'Bearer ' + config['key']
        elif config.get('username'):
            headers['Authorization'] = 'Basic ' + base64.b64encode((config['username'] + ':' + config['password']).encode()).decode()
        request = urllib.request.Request(config['url'] + path, headers=headers)
        try:
            with urllib.request.urlopen(request, timeout=10) as response:
                data = response.read(1024 * 1024 + 1)
                require(len(data) <= 1024 * 1024, 'native observation exceeded its budget')
                return response.status, json.loads(data)
        except urllib.error.HTTPError as error:
            if error.code == 404 and service == 'connect': return 404, None
            raise

    def source_objects(self):
        from minio import Minio
        config = self.manifest['native']['source_minio']
        client = Minio(config['endpoint'], access_key=config['access_key'], secret_key=config['secret_key'], secure=False)
        bucket = self.manifest['source_bucket']
        for name, expected in MANAGER.MESH_SOURCE_FACTS['obj']['download_members'].items():
            response = client.get_object(bucket, 'model3d/obj/' + name)
            try:
                data = response.read(expected['size_bytes'] + 1)
                require(len(data) == expected['size_bytes'] and hashlib.sha256(data).hexdigest() == expected['sha256'],
                        'owned source object changed during native cleanup')
            finally:
                response.close(); response.release_conn()

    def target_rows(self, kind):
        require(kind in {'kafka', 'cdc'}, 'invalid native rowset')
        require(TRANSFER.physical('verify-' + kind + '-initial'), 'Cleanup changed target business rows')

    def index_count(self, engine_id):
        from urllib.parse import urlencode
        index = self.manifest['native']['meili']['index']
        require(re.fullmatch(r'[A-Za-z0-9_-]+', index) is not None, 'invalid physical index name')
        query = urlencode(dict(filter=f'tenant_id = {self.tenant} AND engine_id = {S.positive(engine_id)}',
                               limit=1, offset=0, fields='id,tenant_id,engine_id'))
        _, result = self.native_json(f'/indexes/{index}/documents?{query}', service='meili')
        require(type(result.get('total')) is int and result['total'] >= 0 and result.get('offset') == 0
                and result.get('limit') == 1 and len(result['results']) == min(1, result['total']),
                'native index count is incomplete')
        require(all(row['tenant_id'] == self.tenant and row['engine_id'] == engine_id for row in result['results']),
                'native index returned a foreign projection')
        return result['total']

    def object(self, storage_ref, *, present, verify_glb=False):
        from minio import Minio
        from minio.error import S3Error
        try:
            location = GRANTS.object_ref(storage_ref)
        except (TypeError, ValueError, GRANTS.SuiteError):
            raise S.SuiteError('native artifact requires the canonical Manager object reference') from None
        bucket, key = location['bucket'], location['object']
        require(bucket == 'manager' and key.startswith(f'tenant_{self.tenant}/'),
                'artifact is outside this Tenant output scope')
        config = self.manifest['native']['minio']
        client = Minio(config['endpoint'], access_key=config['access_key'], secret_key=config['secret_key'], secure=False)
        try:
            value = client.stat_object(bucket, key)
            require(present and value.size > 0, 'native artifact unexpectedly exists or is empty')
            if verify_glb:
                require(value.size <= 8 * 1024 * 1024, 'native GLB exceeds bounded observation size')
                response = client.get_object(bucket, key)
                try:
                    data = response.read(value.size + 1)
                    require(len(data) == value.size, 'native GLB changed during observation')
                    MANAGER.validate_model_glb(data, 'obj')
                finally:
                    response.close(); response.release_conn()
        except S3Error as error:
            require(not present and error.code == 'NoSuchKey', 'native artifact absence is not proven')
        return '/' + quote(bucket, safe='') + '/' + quote(key, safe='/')

    def topic(self, task_id, *, present):
        config = self.manifest['native']['kafka']
        container = self.container(config['container'])
        def read():
            result = self.deployment.command(['docker', 'exec', container, 'rpk', 'topic', 'list', '--format', 'json',
                '-X', 'brokers=' + config['brokers'], '-X', 'user=' + config['username'],
                '-X', 'pass=' + config['password'], '-X', 'sasl.mechanism=SCRAM-SHA-256'])
            topics = json.loads(result)
            require(type(topics) is list and len(topics) < 1000
                    and all(type(x) is dict and isinstance(x.get('name'), str) for x in topics),
                    'native topic listing is not a complete successful observation')
            return f'__addp_dlq.{self.tenant}.{S.positive(task_id)}' in {x['name'] for x in topics}
        S.wait_for(read, lambda exists: exists is present, 'native task-owned DLQ presence differs', timeout=60)

    def capture_facts(self, task_id):
        task_id = S.positive(task_id)
        rows = self.deployment.read(f"""SELECT COALESCE(json_agg(c),'[]'::json) FROM (
 SELECT r.id,r.generation,r.resource_version,r.connector_name,p.slot_name,p.publication_name,p.slot_owned,p.publication_owned
 FROM transfer.capture_resources r JOIN transfer.postgresql_capture_resources p ON p.capture_resource_id=r.id
 WHERE r.tenant_id={self.tenant} AND r.task_id={task_id} ORDER BY r.generation DESC LIMIT 1) c""")
        require(len(rows) == 1 and rows[0]['slot_owned'] is True and rows[0]['publication_owned'] is True,
                'native Capture has no exact owned generation')
        current = rows[0]
        for key in ('connector_name', 'slot_name', 'publication_name'):
            require(re.fullmatch(r'[A-Za-z0-9_.-]+', current[key]) is not None, 'invalid native Capture identity')
        previous = self.captures.setdefault(task_id, current)
        require(all(previous[k] == current[k] for k in previous if k != 'resource_version')
                and current['resource_version'] >= previous['resource_version'], 'native Capture generation or target changed')
        return current

    def capture(self, task_id, *, present, connector_only=False):
        facts = self.capture_facts(task_id)
        status, _ = self.native_json('/connectors/' + quote(facts['connector_name'], safe=''), service='connect')
        require(status == (200 if present else 404), 'native connector presence differs')
        if not connector_only:
            container = self.container(self.manifest['native']['pg_container'])
            sql = ("BEGIN READ ONLY; SET LOCAL statement_timeout='5s'; SELECT json_build_object('slot',"
                f"EXISTS(SELECT 1 FROM pg_replication_slots WHERE slot_name='{facts['slot_name']}'),'publication',"
                f"EXISTS(SELECT 1 FROM pg_publication WHERE pubname='{facts['publication_name']}')); COMMIT;")
            raw = self.deployment.command(['docker', 'exec', '-i', container, 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1',
                '-U', 'postgres', '-d', 'continuous_fixture'], input=sql)
            observed = json.loads(raw)
            require(observed == {'slot': present, 'publication': present}, 'native slot/publication presence differs')
        return '/connectors/' + quote(facts['connector_name'], safe='')

    def manager_snapshot(self, engine_id, result_id):
        engine_id, result_id = S.positive(engine_id), S.positive(result_id)
        return self.deployment.read(f"""SELECT json_build_object(
 'artifact',(SELECT json_build_object('status',status,'deleted',deleted_at IS NOT NULL,
   'source_engine_id',source_engine_id,'lifecycle',metadata->'model3d_output_lifecycle')
   FROM manager.model_3d_glb WHERE tenant_id={self.tenant} AND id={result_id}),
 'active_generations',(SELECT count(*) FROM common.task_executions WHERE tenant_id={self.tenant} AND module='manager' AND task_type='model_3d_glb_generation' AND status IN ('pending','running')),
 'artifact_unknown',(SELECT count(*) FROM manager.cleanup_artifact_operations WHERE tenant_id={self.tenant} AND origin='cleanup' AND state='unknown'),
 'index_unknown',(SELECT count(*) FROM manager.content_index_deliveries WHERE tenant_id={self.tenant} AND kind='delete' AND status='unknown'),
 'target_holds',(SELECT count(*) FROM manager.cleanup_targets WHERE tenant_id={self.tenant} AND operation_id<>''),
 'index_targets',(SELECT count(*) FROM manager.cleanup_index_targets WHERE tenant_id={self.tenant} AND engine_id={engine_id}))""")

    def glb_storage(self, result_id):
        value = self.deployment.read(f"SELECT row_to_json(x) FROM (SELECT storage_ref FROM manager.model_3d_glb WHERE tenant_id={self.tenant} AND id={S.positive(result_id)}) x")
        require(type(value) is dict and isinstance(value.get('storage_ref'), str), 'native GLB target missing')
        return value['storage_ref']


def wait_manager_projection_ready(manager, fingerprint, deadline):
    # Meta's initial technical metadata write crosses the same Manager protection boundary.
    path = f'/api/v1/manager/model_3d_glb?item_fingerprint={fingerprint}&page=1&page_size=1'
    while (remaining := deadline - time.monotonic()) > 0:
        reader = MANAGER.GatewayClient(manager.base_url, manager.token, min(manager.timeout, remaining))
        response = reader.request('GET', path, (200, 403))
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            break
        payload = response.payload
        if response.status == 200:
            require(type(payload) is dict and type(payload.get('total')) is int and payload['total'] >= 0,
                    'Manager protection readiness response is invalid')
            return
        require(type(payload) is dict and payload.get('error_code') == 'security_protection_required',
                'Manager protection readiness read was denied')
        time.sleep(min(1, remaining))
    raise S.SuiteError('Manager protection projection was not ready before the source preparation deadline')


def prepare_manager(manager, workflow, controls, source, report, checkpoint):
    report['stage'] = 'manager_native_prepare'; checkpoint()
    controls.source_objects()
    engine_id = workflow.register(controls.descriptor('minio'))
    preparation_deadline = time.monotonic() + 180
    bucket = controls.manifest['source_bucket']
    require(isinstance(bucket, str) and re.fullmatch(r'[a-z0-9.-]+', bucket), 'invalid owned source bucket')
    full_name = bucket + '/model3d/obj/model.obj'
    # The canonical fingerprint locates trusted System facts; it is never
    # authorization or proof that the source exists. The consumer cannot
    # enumerate the ungranted source through Meta.
    fingerprint = hashlib.sha256(f'{engine_id}:{full_name}'.encode()).hexdigest()
    report['stage'] = 'manager_protection_readiness'; checkpoint()
    wait_manager_projection_ready(manager, fingerprint, preparation_deadline)
    report['stage'] = 'manager_source_scan'; checkpoint()
    MANAGER.wait_for_meta_scan(manager, engine_id, preparation_deadline)
    report['stage'] = 'manager_engine_readiness'; checkpoint()
    wait_native_engines(workflow, [(engine_id, 'minio')], preparation_deadline, report, checkpoint)
    report['stage'] = 'manager_source_grant'; checkpoint()
    grants = GRANTS.ModelSourceAuthorization(source, manager,
        dict(tenant_id=controls.tenant, engine_id=engine_id, principal_id=report['principals']['manager']),
        source_type='minio', consumer_permissions=set(S.CONTRACT['native_permissions']['manager']),
        issuer_permissions=REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS)
    grants.grant(dict(format='obj', full_name=full_name, fingerprint=fingerprint, composition=[
        dict(path=full_name, role='model', required=True, primary=True),
        dict(path=bucket + '/model3d/obj/texture.png', role='texture', required=True, primary=False),
        dict(path=bucket + '/model3d/obj/model.mtl', role='material_library', required=True, primary=False)]))
    # Explicit Grants and original source bytes remain owned by this disposable
    # Tenant and are destroyed with it. A failed case never abandons its evidence.
    report['stage'] = 'manager_source_identity'; checkpoint()
    item = MANAGER.find_fixture_item(manager, engine_id, full_name, 'obj')
    require(item['fingerprint'] == fingerprint, 'authorized Manager source identity differs')
    report['stage'] = 'manager_model_prepare'; checkpoint()
    model = MANAGER.prepare_model_fixture(manager, engine_id, bucket, 'obj')
    return engine_id, model


def run_manager(manager, workflow, controls, source, case, report, checkpoint):
    engine_id, model = prepare_manager(manager, workflow, controls, source, report, checkpoint)
    report['stage'] = 'manager_generation'; checkpoint()
    MANAGER.generate_model_glb(manager, model, controls.tenant, 180)
    report.update(artifact_id=S.positive(model.result_id), generation_execution_id=S.identifier(model.execution_id))
    checkpoint()
    report['stage'] = 'manager_native_inventory'; checkpoint()
    probe = 'storage_reference'
    try:
        ref = controls.glb_storage(model.result_id)
        probe = 'native_object'
        object_path = controls.object(ref, present=True)
        probe = 'index_projection'
        S.wait_for(lambda: controls.index_count(engine_id), lambda n: n > 0, 'real source indexing never completed')
        probe = 'artifact_state'
        before = controls.manager_snapshot(engine_id, model.result_id)
        require(before['artifact']['status'] == 'ready' and before['artifact']['deleted'] is False,
                'native artifact is not a live generated result')
    except Exception as error:
        report['manager_inventory_diagnostic'] = manager_inventory_diagnostics(probe, error)
        raise
    key = None
    if case == 'manager-object-unknown': key = controls.arm('object', path=object_path)
    elif case == 'manager-index-unknown':
        index = controls.manifest['native']['meili']['index']
        body = json.dumps({'filter': f'tenant_id = {controls.tenant} AND engine_id = {engine_id}'}, separators=(',', ':')).encode()
        key = controls.arm('index', path=f'/indexes/{index}/documents/delete', body=body)
    report['stage'] = 'manager_engine_deletion'; checkpoint()
    workflow.begin(engine_id)
    completed = workflow.settled(engine_id, success=case == 'manager-success')
    controls.object(ref, present=False)
    after = controls.manager_snapshot(engine_id, model.result_id)
    report['checks'].extend(['real_glb_generated', 'native_object_observed', 'real_index_projection_observed',
                             'engine_deletion_confirmed', 'native_object_absent'])
    if case == 'manager-success':
        result = professional(workflow.user, completed, 'manager', controls.tenant)
        require(result['summary']['deleted_physical_artifacts'] > 0, 'Manager receipt contains no real deletion')
        S.wait_for(lambda: controls.index_count(engine_id), lambda n: n == 0, 'native index projection survived cleanup')
        artifact = after['artifact']
        inputs = artifact['lifecycle'].get('input_preparations', [])
        require(artifact['source_engine_id'] == engine_id and artifact['deleted'] is True
                and inputs and all(x.get('cleaned') is True for x in inputs), 'Cleaned input audit was not retained')
        report['checks'].extend(['native_index_absent', 'professional_receipt_equal', 'cleaned_input_audit_retained'])
    else:
        kind = 'object' if case == 'manager-object-unknown' else 'index'
        assert_proxy(controls.fault(kind, key), controls.run_id, key)
        if kind == 'object':
            require(after['artifact_unknown'] == 1 and after['target_holds'] > 0,
                    'unknown object response lost its durable operation or target hold')
        else:
            require(after['index_unknown'] == 1 and after['index_targets'] == 1,
                    'unknown index response lost its delivery or target hold')
            S.wait_for(lambda: controls.index_count(engine_id), lambda n: n == 0, 'native index deletion did not happen')
        # Repeated Engine DELETE is an observed refusal, never a cleanup retry.
        report['stage'] = 'manager_unknown_engine_read'; checkpoint()
        current = workflow.engine(engine_id)
        report['stage'] = 'manager_repeated_delete_refusal'; checkpoint()
        workflow.user.request('DELETE', f'{ENGINE}/{engine_id}', (409,),
            {'version': current['version'], 'assessment_id': completed['task'].get('based_on_scan', ''),
             'confirmation_token': current['name'], 'external_artifact_policy': 'delete'})
        report['stage'] = 'manager_new_generation_refusal'; checkpoint()
        manager.request('POST', '/api/v1/manager/quick-view/actions', (400, 403, 404, 409),
                        {'locator': model.locator, 'action': 'generate_model_3d_glb'})
        require(controls.manager_snapshot(engine_id, model.result_id) == after, 'blocked request changed isolated native facts')
        assert_proxy(controls.fault(kind, key), controls.run_id, key)
        report['checks'].extend(['native_mutation_sent_response_lost', 'unknown_effect_hold_retained',
                                'engine_remains_deleting', 'repeated_delete_not_resent', 'new_generation_rejected'])
    controls.source_objects()
    report['checks'].append('source_objects_unchanged')
    report['entry'] = 'engine_deletion'
    checkpoint()


def run_transfer(user, workflow, controls, case, report, checkpoint):
    report['stage'] = 'transfer_native_prepare'; checkpoint()
    pg = workflow.register(controls.descriptor('pg'))
    kafka = workflow.register(controls.descriptor('kafka'))
    report['stage'] = 'transfer_source_scan'; checkpoint()
    preparation_deadline = time.monotonic() + 180
    MANAGER.wait_for_meta_scan(user, pg, preparation_deadline)
    if case != 'transfer-dlq-unknown':
        report['stage'] = 'transfer_engine_readiness'; checkpoint()
        wait_native_engines(workflow, [(pg, 'postgresql'), (kafka, 'kafka')], preparation_deadline, report, checkpoint)
    task_ids = []
    if case != 'transfer-dlq-unknown':
        # Current Transfer uses Tenant/task permissions. Manager-purpose source
        # Grants do not prove CDC authorization and have no consumer here.
        cdc, _ = prepare_task(user, controls, pg, kafka, 'cdc', controls.run_id, report, checkpoint)
        report['stage'] = 'transfer_capture_native_verify'; checkpoint()
        controls.capture(cdc, present=True)
        task_ids.append(cdc)
    if case != 'transfer-capture-unknown':
        kafka_task, _ = prepare_task(user, controls, pg, kafka, 'kafka', controls.run_id, report, checkpoint)
        task_ids.append(kafka_task)
    checkpoint()
    if case == 'transfer-dlq-unknown':
        run_dlq_unknown(user, controls, kafka_task, report, checkpoint)
        return
    key = None
    if case == 'transfer-capture-unknown':
        connector = controls.capture(cdc, present=True)
        key = controls.arm('capture', path=connector)
    report['stage'] = 'transfer_engine_deletion'; checkpoint()
    workflow.begin(pg)
    completed = workflow.settled(pg, success=case == 'transfer-engine')
    report['stage'] = 'transfer_capture_cleanup_verify'; checkpoint()
    controls.capture(cdc, present=False, connector_only=case == 'transfer-capture-unknown')
    for task_id in task_ids:
        report['stage'] = 'transfer_actual_stop_verify'; checkpoint()
        controls.require_invocations_stopped(task_id)
        report['stage'] = 'transfer_retained_task_read'; checkpoint()
        retained = user.request('GET', f'{TASKS}/{task_id}', (200,)).payload
        # Both outcomes retain this originally unscheduled definition. Unknown
        # isolation is proved below by stopped invocations, the durable hold and
        # producer refusal, rather than by interpreting enabled as runtime state.
        require_continuous_task_definition(retained, task_id, controls.tenant)
    if case == 'transfer-engine':
        report['stage'] = 'transfer_retained_dlq_verify'; checkpoint()
        controls.topic(kafka_task, present=True)
        require(controls.transfer_snapshot(kafka_task)['dead_letters'] > 0, 'Engine cleanup erased retained DLQ index')
        report['stage'] = 'transfer_professional_result_read'; checkpoint()
        professional(workflow.user, completed, 'transfer', controls.tenant)
        # The ordinary User now explicitly deletes its retained task via the
        # second authorized entry; this is not a System Cleanup child result.
        report['stage'] = 'transfer_task_delete'; checkpoint()
        user.request('DELETE', f'{TASKS}/{kafka_task}', (200,))
        report['stage'] = 'transfer_deleted_dlq_verify'; checkpoint()
        controls.topic(kafka_task, present=False)
        report['stage'] = 'transfer_deleted_task_read'; checkpoint()
        user.request('GET', f'{TASKS}/{kafka_task}', (404,))
        report['stage'] = 'transfer_deleted_dlq_index_verify'; checkpoint()
        require(controls.transfer_snapshot(kafka_task)['dead_letters'] == 0, 'direct deletion retained DLQ private state')
        report['checks'].extend(['real_capture_created', 'actual_invocations_returned', 'native_capture_absent',
            'engine_retains_task_and_dlq', 'professional_receipt_equal', 'explicit_task_delete_removes_dlq'])
        report['entry'] = 'engine_deletion_then_transfer_task_delete'
    else:
        assert_proxy(controls.fault('capture', key), controls.run_id, key)
        snapshot = controls.transfer_snapshot(cdc)
        require(snapshot['unknown_operations'] == 1 and snapshot['task_exists'], 'unknown Capture deletion erased operation evidence')
        user.request('POST', f'{TASKS}/{cdc}/start', (400, 403, 409, 503))
        require(controls.transfer_snapshot(cdc) == snapshot, 'new producer changed isolated Capture facts')
        assert_proxy(controls.fault('capture', key), controls.run_id, key)
        report['checks'].extend(['real_capture_created', 'actual_invocations_returned', 'native_connector_absent',
            'native_mutation_sent_response_lost', 'unknown_effect_hold_retained', 'engine_remains_deleting', 'new_producer_rejected'])
        report['entry'] = 'engine_deletion'
    report['stage'] = 'transfer_business_rows_verify'; checkpoint()
    controls.target_rows('cdc')
    if case == 'transfer-engine': controls.target_rows('kafka')
    report['checks'].append('target_business_rows_preserved')
    checkpoint()


def assert_writer_evidence(value, run_id, key, *, completed):
    require(type(value) is dict and set(value) == {'run_id', 'key', 'forwarded', 'upstream_response',
                'blocked_replays', 'entered', 'released', 'expired'}
            and value['run_id'] == run_id and value['key'] == key
            and type(value['entered']) is int and value['entered'] == 1
            and type(value['forwarded']) is int and value['forwarded'] == (1 if completed else 0)
            and value['upstream_response'] is completed and value['released'] is completed
            and value['expired'] is False and type(value['blocked_replays']) is int and value['blocked_replays'] >= 0,
            'late writer transport lacks a bound real request or response')


def run_late_writer(manager, workflow, controls, source, report, checkpoint):
    engine_id, model = prepare_manager(manager, workflow, controls, source, report, checkpoint)
    deployment = controls.deployment
    report['stage'] = 'late_writer_admission'; checkpoint()
    with deployment.paused_owner('model3d-workflow-engine'):
        started = manager.request('POST', '/api/v1/manager/quick-view/actions', (202,),
            {'locator': model.locator, 'action': 'generate_model_3d_glb'}).payload
        require(started['task_type'] == MANAGER.MODEL_TASK_TYPE, 'late writer has the wrong task type')
        task_id, execution_id = S.positive(started['task_id']), S.identifier(started['execution_id'])
        report['tasks'].append(task_id); report['executions'].append(execution_id)
        report['generation_execution_id'] = execution_id; checkpoint()
        pending = S.wait_for(lambda: controls.pending_glb(engine_id, execution_id),
                            lambda x: isinstance(x, dict) and bool(x.get('pending')), 'late writer pending output missing')
        require(pending['pending']['execution_id'] == execution_id and pending['pending']['task_id'] == str(task_id)
                and pending['generation_holds'] > 0 and pending['deleted'] is False and not pending['storage_ref'],
                'late writer pending target was published, unbound or unheld')
        result_id, ref = S.positive(pending['id']), pending['pending']['storage_ref']
        report['artifact_id'] = result_id; checkpoint()
        target = controls.object(ref, present=False)
        key = controls.arm_writer(target)
    report['stage'] = 'late_writer_request_entered'; checkpoint()
    entered = S.wait_for(lambda: controls.fault('object', key), lambda x: x.get('entered') == 1,
                         'real old Runtime PUT did not enter the bound proxy')
    assert_writer_evidence(entered, controls.run_id, key, completed=False)
    before = controls.manager_attempt(execution_id)
    require(before['execution_id'] == execution_id and before['status'] == 'running' and before['attempt'] == 1
            and before['started_at'] and before['completed_at'] is None and before['lease_live'] is True,
            'late writer Owner is not the first live running attempt')
    report['stage'] = 'late_writer_owner_crash'; checkpoint()
    deployment.crash_and_start_manager()
    terminal = S.wait_for(lambda: controls.manager_attempt(execution_id), lambda x: x['status'] == 'failed',
                          'old Manager attempt did not fail after real lease expiry', timeout=90)
    require(terminal['execution_id'] == execution_id and terminal['attempt'] == 1
            and terminal['started_at'] == before['started_at'] and terminal['completed_at']
            and terminal['lease_live'] is False and terminal['error_code'] == 'manager.execution.lease_expired',
            'Manager recovery replayed work or failed for an unrelated reason')
    held = controls.pending_glb(engine_id, execution_id)
    require(held['pending'] == pending['pending'] and held['generation_holds'] == pending['generation_holds']
            and held['status'] == 'failed' and held['storage_ref'] == '', 'old writer lost pending identity or isolation')
    report['stage'] = 'late_writer_cleanup_assessment'; checkpoint()
    request_id = str(uuid.uuid4()); report['requests'].append(request_id); checkpoint()
    accepted = workflow.user.request('POST', f'{ENGINE}/{engine_id}/deletion-assessments', (202,),
        {'request_id': request_id, 'external_artifact_policy': 'delete'}).payload
    require(accepted == {'assessment_id': request_id}, 'blocked assessment identity changed')
    assessment = S.wait_for(lambda: workflow.user.request('GET', f'{ENGINE}/{engine_id}/deletion-assessments/{request_id}', (200,)).payload,
        lambda x: x.get('status') in S.TERMINAL, 'unresolved writer assessment did not settle')
    require(assessment['status'] == 'failed' and assessment['task']['context']['engine_id'] == engine_id
            and engine_owners_match(assessment['task']['expected_modules'])
            and assessment['task']['requested_by'] == int(workflow.principal), 'unresolved writer assessment was accepted or foreign')
    rows = assessment.get('module_executions', [])
    require(len([x for x in rows if x['module'] == 'manager' and x['status'] == 'failed']) == 1,
            'assessment was not blocked by Manager')
    require(workflow.engine(engine_id)['lifecycle_state'] == 'active', 'read-only failed assessment changed Engine lifecycle')
    controls.object(ref, present=False)
    assert_writer_evidence(controls.fault('object', key), controls.run_id, key, completed=False)
    report['stage'] = 'late_native_writer_release'; checkpoint()
    controls.release_writer(key)
    completed = S.wait_for(lambda: controls.fault('object', key), lambda x: x['upstream_response'] is True,
                          'old Runtime upload did not receive a real native response')
    assert_writer_evidence(completed, controls.run_id, key, completed=True)
    controls.object(ref, present=True, verify_glb=True)
    require(controls.manager_attempt(execution_id) == terminal and controls.pending_glb(engine_id, execution_id) == held,
            'late native write changed the terminal attempt or published its result')
    manager.request('GET', f'/api/v1/manager/model_3d_glb/{result_id}/content', (404,))
    # The source still exists and the original User remains authorized. A domain
    # refusal here cannot be explained by a deleted Engine or revoked source.
    MANAGER.verify_mesh_source(manager, model)
    manager.request('POST', '/api/v1/manager/quick-view/actions', (409,),
                    {'locator': model.locator, 'action': 'generate_model_3d_glb'})
    require(controls.manager_attempt(execution_id) == terminal and controls.pending_glb(engine_id, execution_id) == held,
            'new generation changed a retained old-writer target')
    assert_writer_evidence(controls.fault('object', key), controls.run_id, key, completed=True)
    controls.source_objects()
    report.update(native_late_writer_acceptance='passed', native_late_writer_scope='manager_glb_upload_after_owner_crash',
                  entry='engine_deletion_assessment')
    report['checks'].extend(['real_old_runtime_put_held', 'old_running_attempt_observed',
        'real_lease_expiry_after_owner_crash', 'pending_target_identity_retained', 'cleanup_assessment_refused',
        'engine_remains_active', 'late_native_object_observed', 'late_result_not_published', 'new_generation_rejected',
        'old_attempt_not_replayed', 'source_objects_unchanged'])
    checkpoint()


def main():
    output = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']) / 'cleanup-native-effects.json'
    case = os.environ.get('ADDP_ONLINE_NATIVE_CASE', '')
    report = dict(schema_version='addp.cleanup-native-effects/v1', suite=SUITE, case=case,
                  result='failed', checks=[], engines=[], tasks=[], requests=[], executions=[],
                  control_cleanup='pending', hosted_destruction='required', real_model=False,
                  native_late_writer_acceptance='not_run', graph_native_write_acceptance='not_run', positive_reopen_acceptance='not_run')
    deployment, controls = None, None
    def checkpoint(): output.write_text(json.dumps(report, sort_keys=True) + '\n')
    try:
        require(case in CASES, 'an explicit bounded native case is required')
        deployment = S.OwnedDeployment()
        tenant = int(os.environ['ADDP_ONLINE_TEST_TENANT_ID'])
        report.update(run_id=deployment.run_id, tenant_id=str(tenant), stage='identity_preflight')
        clients = {kind: S.local_client(os.environ['GATEWAY_URL'], os.environ[f'ADDP_ONLINE_CLEANUP_NATIVE_{kind.upper()}_ACCESS_TOKEN'], 60)
                   for kind in ('manager', 'transfer', 'engine')}
        principals = {kind: S.user_identity(client, tenant, S.CONTRACT['native_permissions'][kind]) for kind, client in clients.items()}
        require(len(set(principals.values())) == 3, 'native business and Engine Users must be distinct')
        report['principals'] = principals
        workflow = EngineWorkflow(clients['engine'], tenant, report, checkpoint)
        controls = NativeControls(deployment, tenant, case)
        checkpoint()
        if case.startswith('manager-'):
            source = S.local_client(os.environ['GATEWAY_URL'], os.environ['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'], 30)
            # Domain helpers consume raw ZIP/GLB and headers as well as JSON.
            # Keep the same loopback-validated User and request budget.
            identity = clients['manager']
            manager = MANAGER.GatewayClient(identity.base_url, identity.token, identity.timeout)
            if case == 'manager-late-writer': run_late_writer(manager, workflow, controls, source, report, checkpoint)
            else: run_manager(manager, workflow, controls, source, case, report, checkpoint)
        else: run_transfer(clients['transfer'], workflow, controls, case, report, checkpoint)
        require(set(report['checks']) == set(S.CONTRACT['native_case_checks'][case])
                and len(report['checks']) == len(set(report['checks'])), 'native case checks missing, duplicated or outside scope')
        report['result'] = 'passed'
    except Exception as error:
        report.update(result='failed', error_type=type(error).__name__, failure_stage=report.get('stage', 'preflight'),
                      **failure_diagnostics(error))
        if case.startswith('manager-') and report.get('stage') == 'manager_model_prepare':
            diagnostic = model_prepare_diagnostics(error)
            if diagnostic is not None:
                report['model_prepare_diagnostic'] = diagnostic
    finally:
        if deployment is not None:
            try:
                try:
                    deployment.close()
                finally:
                    if controls is not None: controls.close()
                report['control_cleanup'] = 'passed'
            except Exception as error:
                report.update(result='failed', control_cleanup='failed', cleanup_error_type=type(error).__name__)
        checkpoint(); print(json.dumps(report, sort_keys=True))
    return 0 if report['result'] == 'passed' and report['control_cleanup'] == 'passed' else 1


if __name__ == '__main__': raise SystemExit(main())
