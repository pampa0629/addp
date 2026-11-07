import importlib
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch
import zipfile

owner = importlib.import_module('scripts.test.manager-filegdb-export-online')


class FileGDBArchiveProofTest(unittest.TestCase):
    def revocation_fixture(self, change=None):
        trace = []
        pending = {'execution_id': 'execution-7', 'module': 'transfer', 'source': 'manager', 'status': 'pending', 'attempt': 0}
        failed = {**pending, 'status': 'failed', 'attempt': 1, 'error_details': {
            'code': 'data_protection_required', 'diagnostic': {'stage': 'preparation'}}}
        observations = [dict(pending), dict(pending), failed]
        session = {'status': 'failed'}
        if change:
            change(observations, session)
        def request(method, path, statuses, body=None):
            trace.append((method, path))
            if path.startswith('/api/v1/monitor/'):
                return SimpleNamespace(payload=observations.pop(0))
            if method == 'POST' and statuses == (202,):
                return SimpleNamespace(payload={'status': 'pending', 'id': 9, 'transfer_execution_id': 'execution-7'})
            if path == '/api/v1/manager/exports/9':
                return SimpleNamespace(payload=session)
            if method == 'POST' and statuses == (403,):
                return SimpleNamespace(status=403, payload={'error': 'forbidden'})
            if path == '/api/v1/manager/exports/9/file':
                self.assertEqual(statuses, (409,))
                return SimpleNamespace(status=409)
            self.fail('unexpected request: ' + path)
        client = Mock();client.request.side_effect = request
        initializer = Mock();initializer.request.side_effect = lambda *args: trace.append(('REVOKE', args[1]))
        faults = Mock()
        def paused(operation):
            operation()
            return {'worker_pid': 42, 'stopped_verified': True, 'resumed_verified': True}
        faults.stop_bounded_worker_during.side_effect = paused
        return client, initializer, faults, trace

    def test_revocation_is_between_two_unclaimed_observations_then_real_failed_attempt(self):
        client, initializer, faults, trace = self.revocation_fixture()
        proof = {'result': 'failed'}
        with patch.object(owner.importlib, 'import_module', return_value=SimpleNamespace(HostedFaults=lambda: faults)):
            owner.verify_execution_revocation(client, initializer, 2, {'locator': 'source'}, {'request_id': 'grant-7'}, 'run-7', proof)
        self.assertEqual([method for method, _ in trace[:4]], ['POST', 'GET', 'REVOKE', 'GET'])
        initializer.request.assert_called_once_with('POST', '/api/v1/system/engines/2/access_grants/grant-7/revoke',
            (200,), {'reason': 'Disposable FileGDB queued execution revocation'})
        self.assertEqual(proof['result'], 'passed')
        self.assertEqual(proof['terminal']['attempt'], 1)
        self.assertTrue(proof['worker']['resumed_verified'])
        self.assertEqual((proof['file_status'], proof['resubmit_status']), (409, 403))

    def test_revocation_rejects_races_wrong_failure_stage_content_io_or_published_file(self):
        changes = {
            'already claimed': lambda rows, session: rows[0].update(attempt=1),
            'started during revoke': lambda rows, session: rows[1].update(status='running'),
            'succeeded': lambda rows, session: rows[2].update(status='success'),
            'retried': lambda rows, session: rows[2].update(attempt=2),
            'wrong failure': lambda rows, session: rows[2].update(error_details={'code': 'other'}),
            'content IO': lambda rows, session: rows[2].update(records_read=1),
            'wrong execution': lambda rows, session: rows[2].update(execution_id='other-execution'),
            'published file': lambda rows, session: session.update(download_url='/file'),
        }
        for label, change in changes.items():
            with self.subTest(label=label):
                client, initializer, faults, _ = self.revocation_fixture(change)
                proof = {'result': 'failed'}
                with patch.object(owner.importlib, 'import_module', return_value=SimpleNamespace(HostedFaults=lambda: faults)), \
                        self.assertRaises(ValueError):
                    owner.verify_execution_revocation(client, initializer, 2, {'locator': 'source'}, {'request_id': 'grant-7'}, 'run-7', proof)
                self.assertEqual(proof['result'], 'failed')

    def test_revocation_requires_verified_worker_recovery(self):
        client, initializer, faults, _ = self.revocation_fixture()
        faults.stop_bounded_worker_during.side_effect = None
        faults.stop_bounded_worker_during.return_value = {'worker_pid': 42, 'stopped_verified': True, 'resumed_verified': False}
        with patch.object(owner.importlib, 'import_module', return_value=SimpleNamespace(HostedFaults=lambda: faults)), \
                self.assertRaisesRegex(ValueError, 'Worker recovery'):
            owner.verify_execution_revocation(client, initializer, 2, {'locator': 'source'}, {'request_id': 'grant-7'}, 'run-7', {})

    def test_source_denial_requires_the_real_export_request_and_no_session(self):
        client = Mock()
        client.request.return_value = SimpleNamespace(status=403, payload={'error': 'forbidden'})
        locator = 'addp://engine/2/path/public/filegdb_export_points?type=table'
        self.assertEqual(owner.require_export_denied_without_source_grant(client, locator), 403)
        client.request.assert_called_once_with('POST', '/api/v1/manager/exports', (403,), {
            'source_item_locator': locator, 'format': 'filegdb',
        })
        client.request.return_value.payload['transfer_execution_id'] = 'unexpected'
        with self.assertRaisesRegex(ValueError, 'source denial exposed an export session'):
            owner.require_export_denied_without_source_grant(client, locator)

    def test_unpack_requires_one_safe_geodatabase(self):
        for invalid in (None, 'another.gdb/extra', 'points.gdb/../escape', 'points.gdb/nested/extra'):
            with self.subTest(invalid=invalid), tempfile.TemporaryDirectory() as workspace:
                root = Path(workspace)
                archive = root / 'points.gdb.zip'
                with zipfile.ZipFile(archive, 'w') as output:
                    output.writestr('points.gdb/', '')
                    output.writestr('points.gdb/a00000001.gdbtable', 'fixture')
                    if invalid:
                        output.writestr(invalid, 'extra')
                if invalid:
                    with self.assertRaisesRegex(ValueError, 'unexpected FileGDB archive member'):
                        owner.unpack_archive(archive, root / 'output', 'points.gdb')
                    self.assertFalse((root / 'output').exists())
                else:
                    unpacked = owner.unpack_archive(archive, root / 'output', 'points.gdb')
                    self.assertEqual((unpacked / 'a00000001.gdbtable').read_text(), 'fixture')

    def test_source_scan_addresses_the_table_and_proves_one_item(self):
        helper = Mock()
        helper.wait_for_meta_scan.return_value = 'execution-7'
        initializer = Mock()
        initializer.request.return_value = SimpleNamespace(payload={
            'metadata': {'items_scanned': 1, 'catalog_nodes_scanned': 0},
        })
        locator, execution = owner.scan_source(helper, initializer, 2, 'filegdb_export_points')
        self.assertEqual(locator, 'addp://engine/2/path/public/filegdb_export_points?type=table')
        self.assertEqual(execution, 'execution-7')
        self.assertEqual(helper.wait_for_meta_scan.call_args.kwargs, {'scope': {'targets': [locator]}})
        initializer.request.return_value.payload['metadata']['items_scanned'] = 0
        with self.assertRaisesRegex(ValueError, 'requires one scanned source table'):
            owner.scan_source(helper, initializer, 2, 'filegdb_export_points')
