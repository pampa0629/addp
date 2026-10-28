"""Adversarial recovery evidence checks; these doubles do not establish T4."""
from contextlib import contextmanager
import copy
from importlib import import_module
import json
from pathlib import Path
import tempfile
import unittest
import uuid
from unittest.mock import patch

M = import_module('scripts.test.quality-bounded-recovery-online')
BASE = import_module('scripts.test.quality-dynamic-binding-online_test')


class Gateway(BASE.Gateway):
    def __init__(self, fault=None):
        super().__init__()
        self.fault, self.records, self.events = fault, {}, {}
        self.current = None

    def dispatch(self, method, path, body):
        if method == 'POST' and path.endswith('/run'):
            if self.fault == 'lost_run':
                raise M.SuiteError('lost run response')
            identity = str(uuid.uuid4())
            self.current = identity
            self.records[identity] = {'execution_id': identity, 'created_at': '2026-10-28T00:00:00Z',
                'started_at': '2026-10-28T00:00:01Z', 'completed_at': None, 'execution_time_ms': None,
                'status': 'running', 'attempt': 1, 'lease_token': str(uuid.uuid4()), 'lease_owner': 'first',
                'lease_expires_at': '2026-10-28T00:00:31Z', 'expired': False}
            self.events[identity] = [{'kind': 'started', 'attempt': 1}]
            return {'execution_id': identity}, 202
        if method == 'GET' and '/executions/' in path:
            identity = next((x for x in self.records if x in path), None)
            if identity is None:
                return {}, 404
            if '/events?' in path:
                events = copy.deepcopy(self.events[identity])
                if self.fault == 'old_event':
                    events[-1]['attempt'] = 1
                return {'items': events, 'has_more': False}, 200
            item = copy.deepcopy(self.records[identity])
            for key in ('lease_token', 'lease_owner', 'lease_expires_at', 'expired'):
                item.pop(key)
            if '/monitor/' in path:
                item.update(queue_duration_ms=40000, run_duration_ms=item['execution_time_ms'] or 0,
                            lease_state='none' if item['status'] == 'pending' else 'active')
                if self.fault == 'old_duration' and item['status'] == 'pending':
                    item['run_duration_ms'] = 30000
                if self.fault == 'old_queue' and item['status'] == 'pending':
                    item['queue_duration_ms'] = 1000
            if self.fault == 'public_token':
                item['lease_token'] = 'private-token'
            return item, 200
        return super().dispatch(method, path, body)


class Faults:
    def __init__(self, client, root):
        self.client, self.root, self.phase, self.calls = client, root, None, []
        self.old_tokens = []

    def pin(self, unit):
        self.calls.append(('pin', unit))

    def pending(self):
        item = self.client.records[self.client.current]
        self.old_tokens.append(item['lease_token'])
        item.update(status='pending', started_at=None, lease_token=None, lease_owner=None, lease_expires_at=None)
        if self.client.fault == 'old_start':
            item['started_at'] = '2026-10-28T00:00:01Z'

    def pause(self, unit, paused):
        self.calls.append(('pause', unit, paused))
        item = self.client.records.get(self.client.current)
        if item and unit == 'quality-worker-1' and paused:
            item['expired'] = True
        if self.phase == 'events' and unit == 'quality-worker-2' and not paused:
            self.pending()
        if item and unit == 'quality-worker-1' and not paused:
            with (self.root / 'logs/quality-worker-1.log').open('a') as log:
                log.write(f"quality plan {self.client.current} lease renewal failed: conflict\n")
            if self.client.fault == 'old_overwrite':
                item['attempt'] = 1

    def crash_and_replace(self, unit):
        self.calls.append(('crash', unit))
        self.pending()
        return {'old_pid': 10, 'replacement_pid': 11, 'peer_preserved': True}

    def track(self, identity):
        self.calls.append(('track', identity))

    def source_waiters(self):
        return 1

    def old_worker_rejected(self, identity):
        return self.client.fault != 'missing_rejection'

    def snapshot(self, identity):
        return copy.deepcopy(self.client.records[identity])

    @contextmanager
    def locked(self, kind):
        self.phase = kind
        try:
            yield
        finally:
            item = self.client.records.get(self.client.current)
            if item and kind == 'events':
                item.update(status='running', attempt=2, started_at='2026-10-28T00:00:32Z',
                            lease_token=str(uuid.uuid4()), lease_owner='replacement')
                if self.client.fault == 'same_token':
                    item['lease_token'] = self.old_tokens[-1]
                if self.client.fault == 'changed_creation':
                    item['created_at'] = '2026-10-28T00:00:02Z'
                self.client.events[self.client.current].append({'kind': 'started', 'attempt': 2})
            if item and kind == 'source':
                item.update(status='success', completed_at='2026-10-28T00:00:33Z', execution_time_ms=1000,
                            lease_token=None)
                self.client.events[self.client.current].extend([{'kind': 'progress', 'attempt': 2}, {'kind': 'completed', 'attempt': 2}])
            self.phase = None

    def close(self):
        self.calls.append(('close',))
        if self.client.fault == 'cleanup':
            raise ValueError('failed recovery')


class RecoveryTest(unittest.TestCase):
    def run_case(self, fault=None):
        temporary = tempfile.TemporaryDirectory(prefix='quality-recovery-double-')
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        (root / 'logs').mkdir()
        (root / 'logs/quality-worker-1.log').write_text('')
        client, report = Gateway(fault), {}
        faults = Faults(client, root)
        def immediate(read, predicate, message, timeout=120):
            item = read()
            if not predicate(item):
                raise M.SuiteError(message)
            return item
        with patch.object(M.FAULTS, 'HostedFaults', return_value=faults), patch.object(M.FAULTS, 'ROOT', root), patch.object(M, 'wait_for', side_effect=immediate):
            try:
                M.run_suite(client, 2, 'recovery-unit', BASE.FIXTURES, report)
            except (M.SuiteError, ValueError) as error:
                return client, faults, report, error
        return client, faults, report, None

    def test_crash_and_stale_attempts_preserve_identity_and_cleanup(self):
        client, faults, report, error = self.run_case()
        self.assertIsNone(error)
        self.assertEqual(report['checks'], ['crash_replacement', 'stale_worker'])
        self.assertEqual(report['result'], 'passed')
        self.assertEqual(report['cleanup'], 'passed')
        self.assertEqual(len(report['executions']), 2)
        self.assertTrue(report['observations'][1]['old_worker_heartbeat_rejected'])
        self.assertTrue(all(x['event_count'] == 4 and x['lease_token_changed'] for x in report['observations']))
        self.assertEqual(faults.calls[-1], ('close',))
        self.assertEqual(len(client.objects), 2)
        for token in faults.old_tokens:
            self.assertNotIn(token, json.dumps(report))

    def test_invalid_recovery_facts_and_public_token_fail_closed(self):
        for fault in ('old_start', 'same_token', 'changed_creation', 'old_duration', 'old_queue', 'public_token', 'old_event', 'old_overwrite', 'missing_rejection'):
            with self.subTest(fault=fault):
                _, faults, report, error = self.run_case(fault)
                self.assertIsNotNone(error)
                self.assertEqual(report['result'], 'failed')
                self.assertEqual(faults.calls[-1], ('close',))

    def test_unknown_enqueue_and_failed_fault_cleanup_cannot_claim_zero_residue(self):
        for fault in ('lost_run', 'cleanup'):
            with self.subTest(fault=fault):
                client, _, report, error = self.run_case(fault)
                self.assertIsNotNone(error)
                self.assertEqual(report['cleanup'], 'failed')
                self.assertNotIn('temporary_residual_resources', report)
                self.assertFalse(any(method == 'DELETE' for method, _, _ in client.calls))


if __name__ == '__main__':
    unittest.main()
