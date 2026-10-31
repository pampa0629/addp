"""Native runner refusal/orchestration contracts, not Hosted success evidence."""
import copy
import importlib
import json
import sys
import tempfile
import unittest
import uuid
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
from contextlib import contextmanager

M = importlib.import_module('scripts.test.cleanup-native-effects-online')
S = M.S
ID = str(uuid.uuid4())


class NativeCleanupRunnerTest(unittest.TestCase):
    def test_failure_diagnostics_publish_only_closed_categories_and_http_status(self):
        cases = [
            (S.SuiteError('POST /private?token=secret returned HTTP 403 (private_code)'),
             {'failure_kind': 'http_unexpected_status', 'failure_http_status': 403}),
            (S.SuiteError('DELETE /private returned HTTP 503 (secret)'),
             {'failure_kind': 'http_unexpected_status', 'failure_http_status': 503}),
            (S.SuiteError('GET /private returned invalid JSON'), {'failure_kind': 'http_invalid_json'}),
            (S.SuiteError('GET /private transport failed: secret connection'), {'failure_kind': 'http_transport'}),
            (S.SuiteError('GET /private returned HTTP 700 (secret)'), {'failure_kind': 'assertion_failed'}),
            (S.SuiteError('private assertion and response'), {'failure_kind': 'assertion_failed'}),
            (RuntimeError('POST /private returned HTTP 403 (secret)'), {'failure_kind': 'unexpected_exception'}),
        ]
        for error, expected in cases:
            with self.subTest(expected=expected):
                result = M.failure_diagnostics(error)
                self.assertEqual(result, expected)
                self.assertNotIn('private', json.dumps(result))
                self.assertNotIn('secret', json.dumps(result))

    def test_transfer_scan_and_grant_failure_checkpoints_precede_task_creation(self):
        for failed in ('scan', 'grant'):
            engines = iter((9, 10))
            workflow = SimpleNamespace(register=lambda descriptor: next(engines))
            controls = SimpleNamespace(tenant=2, run_id='native-1', descriptor=lambda kind: {})
            report, stages = {'checks': []}, []
            with self.subTest(failed=failed), \
                    patch.object(M.MANAGER, 'wait_for_meta_scan', side_effect=S.SuiteError('private') if failed == 'scan' else None), \
                    patch.object(M.REGISTRATION, 'initialize_exact_record_read_grants', side_effect=S.SuiteError('private')) as grant, \
                    patch.object(M, 'prepare_task') as prepare:
                with self.assertRaises(S.SuiteError):
                    M.run_transfer(object(), workflow, controls, object(), 'transfer-engine', report,
                                   lambda: stages.append(report['stage']))
                self.assertEqual(report['stage'], 'transfer_source_scan' if failed == 'scan' else 'transfer_source_grant')
                self.assertEqual(stages[-1], report['stage'])
                prepare.assert_not_called()
                if failed == 'scan': grant.assert_not_called()
                else: grant.assert_called_once()

    def test_dlq_case_uses_kafka_input_and_never_prepares_unrelated_cdc_grant(self):
        engines = iter((9, 10))
        workflow = SimpleNamespace(register=lambda descriptor: next(engines))
        controls = SimpleNamespace(tenant=2, run_id='native-1', descriptor=lambda kind: {})
        user, source = object(), object()
        report = {'checks': []}
        with patch.object(M.MANAGER, 'wait_for_meta_scan') as scan, \
                patch.object(M.REGISTRATION, 'initialize_exact_record_read_grants',
                             side_effect=AssertionError('DLQ must not require an unrelated CDC Grant')) as grant, \
                patch.object(M, 'prepare_task', return_value=(7, ID)) as prepare, \
                patch.object(M, 'run_dlq_unknown') as unknown:
            checkpoint = lambda: None
            M.run_transfer(user, workflow, controls, source, 'transfer-dlq-unknown', report, checkpoint)
            scan.assert_called_once()
            self.assertEqual(scan.call_args.args[:2], (user, 9))
            grant.assert_not_called()
            prepare.assert_called_once_with(user, controls, 9, 10, 'kafka', 'native-1', report, checkpoint)
            unknown.assert_called_once_with(user, controls, 7, report, checkpoint)
        config = M.TRANSFER.task_config('kafka', 9, 10)
        self.assertEqual(config['source']['locator'], 'addp://engine/10/path/continuous_events?type=topic')
        self.assertEqual(config['target']['parent_locator'], 'addp://engine/9/path/public?type=schema')
        self.assertNotIn('continuous_source', json.dumps(config))
        self.assertNotIn('manager.data_item.read', S.CONTRACT['native_permissions']['transfer'])

    def test_transfer_task_preparation_records_the_failing_call_before_effects(self):
        stages = {'create': 'transfer_task_create', 'start': 'transfer_task_start',
                  'runtime': 'transfer_runtime_start', 'initial': 'transfer_initial_records',
                  'invalid': 'transfer_dlq_records', 'dead_letter_read': 'transfer_dlq_records',
                  'topic': 'transfer_dlq_native_verify', 'pause': 'transfer_task_pause',
                  'quiescence': 'transfer_task_pause', None: 'transfer_task_pause'}
        for failed, expected_stage in stages.items():
            report, checkpoints = {'tasks': [], 'executions': []}, []
            def effect(name):
                if name == failed: raise S.SuiteError('private failure')
            class User:
                def request(self, method, path, expected, body=None):
                    if method == 'POST' and path == M.TASKS: effect('create'); value = {'id': 7}
                    elif method == 'POST' and path.endswith('/start'): effect('start'); value = {'execution_id': ID}
                    elif method == 'GET' and '/executions/' in path: effect('runtime'); value = {'status': 'running'}
                    elif method == 'GET' and '/dead-letters?' in path: effect('dead_letter_read'); value = {'total': 1}
                    elif method == 'POST' and path.endswith('/pause'): effect('pause'); value = {}
                    elif method == 'GET' and path == M.TASKS + '/7': value = {'desired_state': 'paused', 'status': 'idle'}
                    else: raise AssertionError('unexpected preparation request')
                    return SimpleNamespace(payload=value)
            class Controls:
                def produce_initial(self): effect('initial')
                def produce_invalid(self): effect('invalid')
                def topic(self, task, *, present): effect('topic')
                def require_invocations_stopped(self, task): effect('quiescence')
            def wait(read, predicate, message, **kwargs):
                value = read(); M.require(predicate(value), message); return value
            with self.subTest(failed=failed), patch.object(S, 'wait_for', side_effect=wait), \
                    patch.object(M.TRANSFER, 'physical', return_value=True):
                def run():
                    return M.prepare_task(User(), Controls(), 9, 10, 'kafka', 'native-1', report,
                                          lambda: checkpoints.append(report['stage']))
                if failed is None: self.assertEqual(run(), (7, ID))
                else:
                    with self.assertRaises(S.SuiteError): run()
                self.assertEqual(report['stage'], expected_stage)
                self.assertEqual(checkpoints[-1], expected_stage)

    def test_proxy_evidence_requires_actual_upstream_response_and_single_forward(self):
        valid = dict(run_id='native-1', key=ID, forwarded=1, upstream_response=True, blocked_replays=0)
        M.assert_proxy(valid, 'native-1', ID)
        for update in ({'run_id': 'foreign'}, {'key': str(uuid.uuid4())}, {'forwarded': 0}, {'forwarded': 2},
                       {'forwarded': True}, {'upstream_response': False}, {'upstream_response': 1},
                       {'blocked_replays': -1}, {'blocked_replays': False}, {'raw_response': 'sensitive'}):
            value = dict(valid, **update)
            with self.subTest(update=update), self.assertRaises(S.SuiteError): M.assert_proxy(value, 'native-1', ID)

    def test_engine_workflow_keeps_name_version_assessment_and_ten_owner_identity(self):
        for mutation in ('none', 'wrong_assessment', 'missing_owner', 'running', 'changed_version', 'foreign_engine', 'wrong_creator'):
            calls = []; reads = 0; deleted = False
            class Client:
                def request(self, method, path, expected, body=None):
                    nonlocal reads, deleted
                    calls.append((method, path, body))
                    engine = dict(id=9, tenant_id=2, engine_type='minio', lifecycle_state='active', version=3, name='owned')
                    if mutation == 'foreign_engine': engine['tenant_id'] = 3
                    if method == 'POST' and path == M.ENGINE: return SimpleNamespace(payload=engine)
                    if method == 'GET' and path == M.ENGINE + '/9':
                        reads += 1
                        if mutation == 'changed_version' and reads >= 2: engine['version'] = 4
                        return SimpleNamespace(payload=engine)
                    if method == 'POST' and path.endswith('/deletion-assessments'):
                        self.assessment = body['request_id']
                        return SimpleNamespace(payload={'assessment_id': ID if mutation == 'wrong_assessment' else self.assessment})
                    if method == 'GET' and '/deletion-assessments/' in path:
                        owners = list(S.OWNERS)
                        if mutation == 'missing_owner': owners.pop()
                        return SimpleNamespace(payload=dict(status='failed' if mutation == 'running' else 'success',
                            task=dict(context={'engine_id': 9}, requested_by=99 if mutation == 'wrong_creator' else 17,
                                      action='scan', expected_modules=owners)))
                    if method == 'DELETE':
                        deleted = True
                        self_body = dict(version=3, assessment_id=self.assessment, confirmation_token='owned', external_artifact_policy='delete')
                        if body != self_body: raise AssertionError('deletion lost frozen confirmation')
                        engine['lifecycle_state'] = 'deleting'
                        return SimpleNamespace(payload={'engine': engine})
                    raise AssertionError('unexpected native workflow API')
            report = dict(engines=[], requests=[])
            with patch.object(S, 'user_identity', return_value='17'):
                workflow = M.EngineWorkflow(Client(), 2, report, lambda: None)
                def run():
                    engine = workflow.register(dict(engine_type='minio', engine_origin='general'))
                    workflow.begin(engine)
                if mutation == 'none': run(); self.assertTrue(deleted)
                else:
                    with self.subTest(mutation=mutation), self.assertRaises(S.SuiteError): run()
                    self.assertFalse(deleted)
                with self.assertRaises(S.SuiteError): workflow.engine(999)

    def test_direct_delete_unknown_requires_physical_absence_retained_facts_and_no_replay(self):
        for mutation in ('none', 'no_source', 'no_response', 'topic_present', 'index_erased', 'operation_missing', 'late_write', 'resent', 'wrong_refusal_code', 'legacy_refusal'):
            calls = []; snapshots = 0; observations = 0
            class User:
                def request(self, method, path, expected):
                    calls.append((method, path, expected))
                    if method == 'POST':
                        if expected != (409,): raise AssertionError('producer refusal must be a conflict')
                        payload = {'error': 'safe localized message', 'error_code': 'transfer_cleanup_unresolved'}
                        if mutation == 'wrong_refusal_code': payload['error_code'] = 'unrelated_conflict'
                        if mutation == 'legacy_refusal': payload = {'error': 'start continuous execution: transfer external cleanup operation is unresolved'}
                        return SimpleNamespace(payload=payload)
                    return SimpleNamespace(payload={})
            class Controls:
                tenant, run_id = 2, 'native-1'
                def transfer_snapshot(self, task):
                    nonlocal snapshots
                    snapshots += 1
                    value = dict(task_exists=True, dead_letters=2, unknown_operations=0 if snapshots == 1 else 1)
                    if mutation == 'no_source' and snapshots == 1: value['dead_letters'] = 0
                    if mutation == 'index_erased' and snapshots >= 2: value['dead_letters'] = 0
                    if mutation == 'operation_missing': value['unknown_operations'] = 0
                    if mutation == 'late_write' and snapshots >= 3: value['dead_letters'] = 3
                    return value
                def target_rows(self, kind):
                    if kind != 'kafka': raise AssertionError('wrong native target')
                def arm(self, kind, *, topic):
                    if kind != 'dlq' or topic != '__addp_dlq.2.7': raise AssertionError('wrong task target')
                    return ID
                def fault(self, kind, key):
                    nonlocal observations
                    observations += 1
                    return dict(run_id='native-1', key=ID, forwarded=2 if mutation == 'resent' and observations > 1 else 1,
                                upstream_response=mutation != 'no_response', blocked_replays=0)
                def topic(self, task, *, present):
                    if present or mutation == 'topic_present': raise S.SuiteError('native topic still exists')
            report = dict(checks=[])
            if mutation == 'none':
                M.run_dlq_unknown(User(), Controls(), 7, report, lambda: None)
                self.assertEqual(report['entry'], 'transfer_task_delete')
                self.assertEqual(report['producer_refusal_http_status'], 409)
                self.assertEqual(report['producer_refusal_error_code'], 'transfer_cleanup_unresolved')
                self.assertEqual([x[:2] for x in calls], [('DELETE', M.TASKS + '/7'), ('DELETE', M.TASKS + '/7'), ('POST', M.TASKS + '/7/start')])
            else:
                with self.subTest(mutation=mutation), self.assertRaises(S.SuiteError): M.run_dlq_unknown(User(), Controls(), 7, report, lambda: None)
                self.assertNotIn('new_producer_rejected', report['checks'])
                self.assertEqual(report['stage'], {
                    'no_source': 'direct_task_delete_unknown', 'no_response': 'transfer_dlq_effect_probe',
                    'topic_present': 'transfer_dlq_native_absence', 'index_erased': 'transfer_dlq_hold_verify',
                    'operation_missing': 'transfer_dlq_hold_verify', 'late_write': 'transfer_dlq_hold_after_refusal',
                    'resent': 'transfer_dlq_no_replay', 'wrong_refusal_code': 'transfer_dlq_producer_refusal',
                    'legacy_refusal': 'transfer_dlq_producer_refusal',
                }[mutation])

    def test_manifest_rejects_foreign_runs_extra_fields_and_nonprivate_files(self):
        for mutation in ('foreign', 'case', 'extra', 'permissions', 'symlink'):
            with tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'cleanup-native-fixture.json'
                value = dict(schema='addp.cleanup-native-fixture/v1', run_id='native-1', case='manager-success',
                             descriptors={}, source_bucket='source', proxies={}, native={})
                if mutation == 'foreign': value['run_id'] = 'other'
                if mutation == 'case': value['case'] = 'transfer-engine'
                if mutation == 'extra': value['raw_data'] = 'untrusted'
                path.write_text(json.dumps(value)); path.chmod(0o644 if mutation == 'permissions' else 0o600)
                if mutation == 'symlink':
                    target = path.with_suffix('.other'); path.rename(target); path.symlink_to(target)
                with self.subTest(mutation=mutation), self.assertRaises(S.SuiteError):
                    M.NativeControls(SimpleNamespace(secret=Path(directory), run_id='native-1'), 2, 'manager-success')

    def test_manager_prepares_grant_before_source_download_and_rejects_missing_native_evidence(self):
        for case in ('manager-success', 'manager-object-unknown', 'manager-index-unknown'):
            for mutation in ('none', 'missing_effect', 'lost_hold', 'dirty_audit', 'changed_after_refusal'):
                if mutation == 'dirty_audit' and case != 'manager-success': continue
                if mutation in {'missing_effect', 'lost_hold', 'changed_after_refusal'} and case == 'manager-success': continue
                granted, reads, indexed = [], [], []
                model = SimpleNamespace(item={'fingerprint': 'fp'}, result_id=7, execution_id=ID, locator='addp://engine/9/path/source/model.obj?type=object')
                class User:
                    def request(self, method, path, expected, body=None):
                        return SimpleNamespace(payload={})
                class Controls:
                    tenant, run_id = 2, 'native-1'
                    manifest = {'source_bucket': 'source', 'native': {'meili': {'index': 'content'}}}
                    def source_objects(self): pass
                    def descriptor(self, kind): return {}
                    def glb_storage(self, result): return 'ref'
                    def object(self, ref, *, present):
                        reads.append(present); return '/manager/tenant_2/out.glb'
                    def index_count(self, engine):
                        indexed.append(engine); return 2 if len(indexed) == 1 else 0
                    def manager_snapshot(self, engine, result):
                        current = dict(artifact=dict(status='ready', deleted=False, source_engine_id=9,
                            lifecycle={'input_preparations': [{'cleaned': False}]}),
                            artifact_unknown=0, index_unknown=0, target_holds=0, index_targets=0)
                        snapshots.append(1)
                        if len(snapshots) > 1:
                            if case == 'manager-success':
                                current['artifact']['deleted'] = True
                                current['artifact']['lifecycle']['input_preparations'][0]['cleaned'] = mutation != 'dirty_audit'
                            else:
                                current['artifact_unknown' if case == 'manager-object-unknown' else 'index_unknown'] = 1
                                current['target_holds'] = current['index_targets'] = 0 if mutation == 'lost_hold' else 1
                        if mutation == 'changed_after_refusal' and len(snapshots) > 2: current['artifact']['status'] = 'building'
                        return current
                    def arm(self, kind, **target): return ID
                    def fault(self, kind, key):
                        return dict(run_id='native-1', key=ID, forwarded=1, upstream_response=mutation != 'missing_effect', blocked_replays=0)
                workflow = SimpleNamespace(user=User(), register=lambda descriptor: 9, begin=lambda engine: None,
                    settled=lambda engine, **kwargs: {'task': {'based_on_scan': ID}},
                    engine=lambda engine: {'version': 3, 'name': 'owned'})
                def prepare(*args):
                    if not granted: raise AssertionError('source download occurred before exact Grant')
                    return model
                def wait(read, predicate, message, **kwargs):
                    value = read(); M.require(predicate(value), message); return value
                snapshots = []
                with patch.object(M.MANAGER, 'wait_for_meta_scan'), patch.object(M.MANAGER, 'find_fixture_item', return_value={'fingerprint': 'fp'}), \
                     patch.object(M.MANAGER, 'prepare_model_fixture', side_effect=prepare), patch.object(M.MANAGER, 'generate_model_glb'), \
                     patch.object(M.GRANTS, 'ModelSourceAuthorization', return_value=SimpleNamespace(grant=lambda value: granted.append(value))), \
                     patch.object(M, 'professional', return_value={'summary': {'deleted_physical_artifacts': 1}}), patch.object(S, 'wait_for', side_effect=wait):
                    report = {'checks': [], 'principals': {'manager': '18'}}
                    if mutation == 'none':
                        M.run_manager(User(), workflow, Controls(), User(), case, report, lambda: None)
                        self.assertEqual(reads, [True, False])
                        self.assertEqual(report['entry'], 'engine_deletion')
                    else:
                        with self.subTest(case=case, mutation=mutation), self.assertRaises(S.SuiteError):
                            M.run_manager(User(), workflow, Controls(), User(), case, report, lambda: None)

    def test_transfer_engine_preserves_definition_and_dlq_until_explicit_delete(self):
        for case in ('transfer-engine', 'transfer-capture-unknown'):
            for mutation in ('none', 'definition_missing', 'dlq_erased', 'native_capture_remains', 'business_rows_changed'):
                if case == 'transfer-capture-unknown' and mutation == 'dlq_erased': continue
                calls, deleted, snapshots = [], [], []
                class User:
                    def request(self, method, path, expected):
                        calls.append((method, path))
                        if method == 'DELETE': deleted.append(8)
                        if method == 'GET' and path.endswith('/8') and expected == (404,): return SimpleNamespace(payload={})
                        value = dict(id=int(path.rsplit('/', 1)[-1]) if method == 'GET' else 7,
                                     tenant_id=2, enabled=mutation == 'definition_missing')
                        return SimpleNamespace(payload=value)
                class Controls:
                    tenant, run_id = 2, 'native-1'
                    def descriptor(self, kind): return {'kind': kind}
                    def capture(self, task, *, present, connector_only=False):
                        if not present and mutation == 'native_capture_remains': raise S.SuiteError('native connector still exists')
                        return '/connectors/owned-generation'
                    def arm(self, kind, *, path):
                        if kind != 'capture' or path != '/connectors/owned-generation': raise AssertionError('wrong generation')
                        return ID
                    def fault(self, kind, key):
                        return dict(run_id='native-1', key=ID, forwarded=1, upstream_response=True, blocked_replays=0)
                    def require_invocations_stopped(self, task): pass
                    def topic(self, task, *, present):
                        calls.append(('NATIVE_TOPIC', present))
                        if present and mutation == 'dlq_erased': raise S.SuiteError('Engine erased DLQ')
                    def transfer_snapshot(self, task):
                        snapshots.append(task)
                        return dict(task_exists=True, dead_letters=0 if deleted else 2, unknown_operations=1)
                    def target_rows(self, kind):
                        if mutation == 'business_rows_changed': raise S.SuiteError('target rows changed')
                workflow = SimpleNamespace(user=User(), register=lambda descriptor: 9 if descriptor['kind'] == 'pg' else 10,
                    begin=lambda engine: calls.append(('ENGINE_BEGIN', engine)), settled=lambda *args, **kwargs: {})
                def prepare(user, controls, pg, kafka, kind, run_id, report, checkpoint):
                    task = 7 if kind == 'cdc' else 8
                    report['tasks'].append(task)
                    return task, ID
                with patch.object(M.MANAGER, 'wait_for_meta_scan'), patch.object(M.REGISTRATION, 'initialize_exact_record_read_grants'), \
                     patch.object(M, 'prepare_task', side_effect=prepare), patch.object(M, 'professional', return_value={}):
                    report = dict(checks=[], tasks=[])
                    if mutation == 'none':
                        M.run_transfer(User(), workflow, Controls(), User(), case, report, lambda: None)
                        self.assertEqual(set(report['checks']), set(S.CONTRACT['native_case_checks'][case]))
                        if case == 'transfer-engine':
                            self.assertLess(calls.index(('ENGINE_BEGIN', 9)), calls.index(('DELETE', M.TASKS + '/8')))
                            self.assertLess(calls.index(('NATIVE_TOPIC', True)), calls.index(('DELETE', M.TASKS + '/8')))
                        else: self.assertFalse(deleted)
                    else:
                        with self.subTest(case=case, mutation=mutation), self.assertRaises(S.SuiteError):
                            M.run_transfer(User(), workflow, Controls(), User(), case, report, lambda: None)

    def test_native_object_never_treats_auth_failure_or_missing_bucket_as_absence(self):
        class S3Error(Exception):
            def __init__(self, code): self.code = code
        for code in ('present', 'NoSuchKey', 'NoSuchBucket', 'AccessDenied'):
            def stat(*_):
                if code != 'present': raise S3Error(code)
                return SimpleNamespace(size=9)
            sdk = SimpleNamespace(Minio=lambda *args, **kwargs: SimpleNamespace(stat_object=stat))
            controls = M.NativeControls.__new__(M.NativeControls)
            controls.tenant = 2
            controls.manifest = {'native': {'minio': {'endpoint': '127.0.0.1:1', 'access_key': 'private', 'secret_key': 'private'}}}
            with patch.dict(sys.modules, {'minio': sdk, 'minio.error': SimpleNamespace(S3Error=S3Error)}):
                if code == 'NoSuchKey':
                    self.assertEqual(controls.object('addp-infra://minio/manager/tenant_2/output.glb?type=object', present=False), '/manager/tenant_2/output.glb')
                else:
                    with self.subTest(code=code), self.assertRaises(S.SuiteError):
                        controls.object('addp-infra://minio/manager/tenant_2/output.glb?type=object', present=False)
                with self.assertRaises(S.SuiteError): controls.object('addp-infra://minio/manager/tenant_3/output.glb', present=False)

    def test_late_writer_requires_real_old_attempt_failed_assessment_and_unpublished_native_upload(self):
        for mutation in ('none', 'not_running', 'wrong_failure', 'replayed', 'cleanup_passed', 'other_owner_failed',
                         'engine_changed', 'no_native_write', 'published', 'lost_hold', 'changed_terminal'):
            events, attempted = [], []
            state = dict(crashed=False, released=False, observed=False)
            model = SimpleNamespace(locator='owned-model', item={}, format='obj')
            pending = dict(execution_id=ID, task_id='11', storage_ref='owned-ref')
            class Deployment:
                @contextmanager
                def paused_owner(self, name):
                    if name != 'model3d-workflow-engine': raise AssertionError('foreign pause')
                    events.append('paused')
                    try: yield
                    finally: events.append('resumed')
                def crash_and_start_manager(self):
                    if 'resumed' not in events or 'put_entered' not in events: raise AssertionError('crashed before real PUT')
                    state['crashed'] = True; events.append('crashed')
            class Controls:
                tenant, run_id, deployment = 2, 'native-1', Deployment()
                def pending_glb(self, engine, execution):
                    return dict(id=7, status='ready' if state['released'] and mutation == 'published' else 'failed' if state['crashed'] else 'building',
                        pending=copy.deepcopy(pending), storage_ref='published' if state['released'] and mutation == 'published' else '',
                        generation_holds=0 if state['released'] and mutation == 'lost_hold' else 2, deleted=False)
                def object(self, ref, *, present, verify_glb=False):
                    if ref != 'owned-ref': raise AssertionError('wrong native output')
                    if present and (not state['released'] or mutation == 'no_native_write'): raise S.SuiteError('no native write')
                    if present: state['observed'] = True
                    return '/manager/tenant_2/model3d-quick-view/fp/output.glb'
                def arm_writer(self, target):
                    if 'resumed' in events: raise AssertionError('proxy armed after Runtime resumed')
                    return ID
                def fault(self, kind, key):
                    events.append('put_entered')
                    return dict(run_id='native-1', key=ID, entered=1, forwarded=1 if state['released'] else 0,
                        upstream_response=state['released'], released=state['released'], expired=False, blocked_replays=0)
                def manager_attempt(self, execution):
                    status = 'failed' if state['crashed'] or mutation == 'not_running' else 'running'
                    return dict(execution_id=ID, status=status, attempt=2 if state['crashed'] and mutation == 'replayed' else 1,
                        started_at='original-start', completed_at='finished' if state['crashed'] else None,
                        lease_live=not state['crashed'], error_code='wrong' if mutation == 'wrong_failure' else 'manager.execution.lease_expired',
                        state_digest='changed' if state['released'] and mutation == 'changed_terminal' else 'stable')
                def release_writer(self, key):
                    if not state['crashed'] or 'assessment_failed' not in events: raise AssertionError('released before terminal/assessment proof')
                    state['released'] = True; events.append('released')
                def source_objects(self): events.append('source_unchanged')
            class Manager:
                def request(self, method, path, expected, body=None):
                    if method == 'POST' and expected == (202,):
                        return SimpleNamespace(payload=dict(task_id=11, task_type=M.MANAGER.MODEL_TASK_TYPE, execution_id=ID))
                    if method == 'GET':
                        if not state['observed']: raise AssertionError('public visibility checked before native effect')
                    if method == 'POST' and expected == (409,): attempted.append('new_generation')
                    return SimpleNamespace(payload={})
            class EngineUser:
                def request(self, method, path, expected, body=None):
                    if method == 'POST':
                        self.request_id = body['request_id']; return SimpleNamespace(payload={'assessment_id': self.request_id})
                    events.append('assessment_failed')
                    return SimpleNamespace(payload=dict(status='success' if mutation == 'cleanup_passed' else 'failed',
                        task={'context': {'engine_id': 9}, 'expected_modules': list(S.OWNERS), 'requested_by': 17},
                        module_executions=[dict(module='manager', status='success' if mutation == 'other_owner_failed' else 'failed')]))
            workflow = SimpleNamespace(user=EngineUser(), principal='17',
                engine=lambda engine: {'lifecycle_state': 'deleting' if mutation == 'engine_changed' else 'active'})
            def wait(read, predicate, message, **kwargs):
                value = read(); M.require(predicate(value), message); return value
            with patch.object(M, 'prepare_manager', return_value=(9, model)), patch.object(S, 'wait_for', side_effect=wait), \
                 patch.object(M.MANAGER, 'verify_mesh_source'):
                report = dict(checks=[], tasks=[], executions=[], requests=[])
                if mutation == 'none':
                    M.run_late_writer(Manager(), workflow, Controls(), None, report, lambda: None)
                    self.assertEqual(set(report['checks']), set(S.CONTRACT['native_case_checks']['manager-late-writer']))
                    self.assertEqual(report['native_late_writer_acceptance'], 'passed')
                    self.assertEqual(report['entry'], 'engine_deletion_assessment')
                    self.assertEqual(attempted, ['new_generation'])
                else:
                    with self.subTest(mutation=mutation), self.assertRaises(S.SuiteError):
                        M.run_late_writer(Manager(), workflow, Controls(), None, report, lambda: None)
                    self.assertNotIn('native_late_writer_acceptance', report)
                    if mutation == 'not_running': self.assertNotIn('crashed', events)
                    if mutation in {'cleanup_passed', 'other_owner_failed', 'engine_changed'}: self.assertNotIn('released', events)

    def test_manager_crash_and_model_pause_are_restricted_to_the_exact_late_writer_case(self):
        deployment = S.OwnedDeployment.__new__(S.OwnedDeployment)
        deployment.paused, deployment.pins = set(), {'manager-backend': ('owned-id', 'owned-image', 'old-start')}
        before = dict(Id='owned-id', Image='owned-image', State=dict(Running=True, StartedAt='old-start', Health={'Status': 'healthy'}),
            HostConfig={'RestartPolicy': {'Name': 'no'}}, Config={'Env': ['MANAGER_EXECUTION_LEASE_DURATION=30s', 'MANAGER_EXECUTION_HEARTBEAT_INTERVAL=5s']})
        after = copy.deepcopy(before); after['State']['StartedAt'] = 'new-start'
        stopped = dict(Id='owned-id', Image='owned-image', State={'Running': False, 'ExitCode': 137})
        for case in ('', 'manager-success', 'transfer-engine'):
            with patch.dict(M.os.environ, {'ADDP_ONLINE_NATIVE_CASE': case}), patch.object(deployment, 'inspect') as inspect, \
                 patch.object(deployment, 'command') as command:
                with self.assertRaises(S.SuiteError): deployment.crash_and_start_manager()
                with self.assertRaises(S.SuiteError): S.OwnedDeployment.inspect(deployment, 'model3d-workflow-engine')
                inspect.assert_not_called(); command.assert_not_called()
        with patch.dict(M.os.environ, {'ADDP_ONLINE_NATIVE_CASE': 'manager-late-writer'}), \
             patch.object(deployment, 'inspect', side_effect=[before, before, after]), \
             patch.object(deployment, 'command', side_effect=['', json.dumps([stopped]), '']) as command, \
             patch.object(S, 'wait_for', return_value=after):
            deployment.crash_and_start_manager()
        self.assertEqual([call.args[0] for call in command.call_args_list], [
            ['docker', 'kill', '--signal=KILL', 'owned-id'], ['docker', 'inspect', 'owned-id'], ['docker', 'start', 'owned-id']])

    def test_uncertain_writer_arm_retains_release_intent_and_never_releases_foreign_key(self):
        controls = M.NativeControls.__new__(M.NativeControls)
        controls.run_id, controls.proxies, controls.writer_keys = 'native-1', {}, set()
        calls = []
        def control(kind, action, body):
            calls.append(action)
            if action == 'arm':
                self.assertEqual(controls.proxies['object'], body['key'])
                self.assertIn(body['key'], controls.writer_keys)
                raise S.SuiteError('arm response lost')
            self.assertEqual(kind, 'object')
            self.assertEqual(body, {'run_id': 'native-1', 'key': controls.proxies['object']})
            return {'released': True, 'expired': False}
        with patch.object(controls, 'control', side_effect=control):
            with self.assertRaises(S.SuiteError): controls.arm_writer('/manager/tenant_2/model3d-quick-view/fp/out.glb')
            self.assertEqual(len(controls.writer_keys), 1)
            with self.assertRaises(S.SuiteError): controls.release_writer(str(uuid.uuid4()))
            self.assertEqual(calls, ['arm'])
            controls.close()
        self.assertEqual(calls, ['arm', 'release'])
        self.assertEqual(controls.writer_keys, set())

    def test_pending_glb_can_be_absent_before_worker_preparation_but_never_ambiguous(self):
        controls = M.NativeControls.__new__(M.NativeControls)
        controls.tenant = 2
        pending = {'id': 7, 'pending': {'execution_id': ID, 'storage_ref': 'owned'}}
        read = SimpleNamespace(read=None)
        controls.deployment = read
        with patch.object(read, 'read', side_effect=[[], [pending], [pending, pending]]):
            self.assertIsNone(controls.pending_glb(9, ID))
            self.assertEqual(controls.pending_glb(9, ID), pending)
            with self.assertRaises(S.SuiteError): controls.pending_glb(9, ID)

    def test_native_grants_use_exact_existing_initializer_without_deny_authority(self):
        initializer = M.REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS
        consumer = set(S.CONTRACT['native_permissions']['manager'])
        class User:
            def __init__(self, principal, permissions): self.principal, self.permissions = principal, permissions
            def request(self, *_):
                return SimpleNamespace(payload=dict(principal={'id': self.principal, 'type': 'user'},
                    context={'type': 'tenant', 'tenant_id': '2'}, authorization={'role_assignments': [
                    {'role_key': 'online.native', 'permissions': sorted(self.permissions)}]}))
        with patch.object(M.GRANTS.API, 'validate_user_identity'):
            identity = M.GRANTS.ModelSourceAuthorization(User('17', initializer), User('18', consumer),
                dict(tenant_id=2, engine_id=9, principal_id='18'), source_type='minio',
                consumer_permissions=consumer, issuer_permissions=initializer)
            self.assertEqual(identity.operator, '17')
            with self.assertRaises(M.GRANTS.SuiteError):
                M.GRANTS.ModelSourceAuthorization(User('17', initializer | {'system.engine_access_deny.create'}),
                    User('18', consumer), dict(tenant_id=2, engine_id=9, principal_id='18'),
                    source_type='minio', consumer_permissions=consumer, issuer_permissions=initializer)

    def test_paused_does_not_mean_runtime_returned(self):
        calls = []
        class User:
            def request(self, method, path, expected):
                return SimpleNamespace(payload=dict(desired_state='paused', status='idle'))
        controls = SimpleNamespace(require_invocations_stopped=lambda task: calls.append(task))
        M.wait_paused(User(), 9, controls)
        self.assertEqual(calls, [9])
        def unfinished(_): raise S.SuiteError('original invocation still running')
        with self.assertRaises(S.SuiteError): M.wait_paused(User(), 9, SimpleNamespace(require_invocations_stopped=unfinished))


if __name__ == '__main__': unittest.main()
