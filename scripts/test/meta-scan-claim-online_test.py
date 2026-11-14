from copy import deepcopy
from importlib import import_module
from pathlib import Path
import unittest

claim = import_module('scripts.test.meta-scan-claim-online')


class ScanClaimTest(unittest.TestCase):
    def test_claim_event_and_worker_share_one_attempt(self):
        at = '2026-11-14T05:40:00Z'
        row = dict(execution_id='x', tenant_id=7, source_task_id='3', status='running',
                   attempt=1, lease_valid=True, lease_owner='worker-id-2', started_at=at,
                   events=[dict(execution_id='x', id=8, attempt=1, occurred_at=at)])
        workers = {'worker-id': {'capacity': 2}}
        claim.assert_claim(row, 'x', 3, 7, workers)
        for changes in ({'lease_valid': False}, {'status': 'success'}, {'lease_owner': 'worker-id-0'}, {'lease_owner': 'other-0'},
                        {'events': []}, {'attempt': 2}, {'events': row['events'] * 2},
                        {'events': [dict(row['events'][0], execution_id='other')]}):
            with self.subTest(changes=changes), self.assertRaises(claim.ClaimError):
                claim.assert_claim(deepcopy(row) | changes, 'x', 3, 7, workers)

    def test_hosted_gate_binds_product_before_creating_resources(self):
        source = Path(__file__).with_name('online-hosted-meta-scan-gate.sh').read_text()
        stages = ['rev-parse HEAD', 'source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
                  'infra_owned=1', '--exact-process -system', '--exact-process -gateway',
                  'bash scripts/dev/start.sh -meta', 'test-online "ONLINE_SUITE=$ONLINE_SUITE"']
        self.assertEqual(sorted(source.index(s) for s in stages), [source.index(s) for s in stages])
