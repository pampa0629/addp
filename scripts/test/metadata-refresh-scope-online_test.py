import importlib
import json
from copy import deepcopy
from datetime import datetime, timezone
from pathlib import Path
import tempfile
import unittest
import uuid
from types import SimpleNamespace
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

REFRESH = importlib.import_module('scripts.test.metadata-refresh-scope-online')


class RefreshServer:
    def __init__(self, storage='postgresql', fault=None):
        self.storage, self.fault = storage, fault
        names = (['public.metric_people', 'public.metric_events', 'public.metric_facts'] if storage == 'postgresql'
                 else ['selected/current.txt', 'selected/private.txt', 'selected/child/nested.txt', 'sibling/hidden.txt'])
        self.rows = {n: dict(id=i + 1, node_id=11 if '/child/' not in n else 12, full_name=n, scanned_at='old')
                     for i, n in enumerate(names)}
        self.grants, self.runs, self.calls = {}, {}, []
        self.expired = set()
        self.scans = 0
        self.client = SimpleNamespace(token='original-user-token', request=self.request)

    def snapshot(self):
        return deepcopy(self.rows)

    def request(self, method, url, expected, body=None):
        self.calls.append((method, url, deepcopy(body)))
        parsed = urlsplit(url)
        query = parse_qs(parsed.query)
        code, value = 200, {}
        if url.endswith('/authorization-facts'):
            value = dict(item_fingerprint=url.split('/')[-2], basis_state='complete', engine_id='8', boundary_hash='a' * 64)
        elif url.endswith('/access_grants'):
            code = 201
            value = dict(body, engine_id='8', revocation=None)
            if 'expected_boundary_hash' in body:
                value['boundary_hash'] = body['expected_boundary_hash']
            self.grants[body['request_id']] = value
            if self.fault == 'unknown_create':
                raise REFRESH.API.SuiteError('simulated lost response')
        elif parsed.path.endswith('/history'):
            matches = []
            for g in self.grants.values():
                expires = datetime.fromisoformat(g['expires_at'])
                short = (expires - datetime.now(timezone.utc)).total_seconds() < 20
                if query['state'][0] == 'expired' and short:
                    self.expired.add(g['request_id'])
                state = 'revoked' if g.get('revocation') else 'expired' if g['request_id'] in self.expired else 'active'
                if state == query['state'][0] and g['catalog_path'] == json.loads(query['catalog_path'][0]):
                    matches.append(deepcopy(g))
            value = dict(data=matches, total=len(matches))
        elif url.endswith('/revoke'):
            if self.fault == 'cleanup_revoke':
                raise REFRESH.API.SuiteError('simulated cleanup failure')
            self.grants[url.split('/')[-2]]['revocation'] = {'reason': 'withdrawn'}
        elif url.endswith('/scan/run/manual'):
            active = [g for g in self.grants.values() if not g.get('revocation') and g['request_id'] not in self.expired]
            if not active:
                code, value = 403, {'error': 'permission denied', 'error_code': 'permission_denied'}
            else:
                self.scans += 1
                execution = f'00000000-0000-4000-8000-{self.scans:012d}'
                chosen, nodes = set(), 0
                for g in active:
                    segments = g['catalog_path']['segments'][1:]
                    if g.get('node_recursive') is None:
                        name = ('.' if self.storage == 'postgresql' else '/').join(p['name'] for p in segments)
                        chosen.add(name)
                    else:
                        node = '/'.join(p['name'] for p in segments)
                        for name in self.rows:
                            if self.storage == 'postgresql' and name.startswith(node + '.'):
                                chosen.add(name)
                            elif self.storage == 'nfs' and (name.startswith(node + '/') if g['node_recursive']
                                                         else name.rsplit('/', 1)[0] == node):
                                chosen.add(name)
                        nodes = 1
                changed = sorted(chosen)[:1] if self.fault == 'inflight' else chosen
                for name in changed:
                    if self.fault != 'skip_update':
                        self.rows[name]['scanned_at'] = execution
                if self.fault == 'outside_update':
                    self.rows[next(n for n in self.rows if n not in chosen)]['scanned_at'] = execution
                self.runs[execution] = dict(execution_id=execution, tenant_id=3, triggered_by=44,
                    source='manager', status='success', completed_at='2026-11-04T00:00:00Z',
                    scan_context=dict(engine_id=8, scan_depth=body['scan_depth']),
                    metadata=dict(items_scanned=len(chosen), catalog_nodes_scanned=1 if self.fault == 'parent_enumeration' else nodes,
                                  fields_scanned=0))
                if self.fault == 'inflight':
                    self.runs[execution].update(status='running', completed_at=None)
                if self.storage == 'nfs' and nodes:
                    self.runs[execution]['scan_progress'] = {'directories_discovered': len({self.rows[n]['node_id'] for n in chosen}),
                        'files_discovered': len(chosen), 'items_planned': len(chosen)}
                if self.fault == 'leaked_identity':
                    self.runs[execution]['scan_progress'] = {'current_path': next(n for n in self.rows if n not in chosen)}
                if self.fault == 'wrong_actor':
                    self.runs[execution]['triggered_by'] = 45
                if self.fault == 'private_plan':
                    self.runs[execution]['execution_config'] = {'refresh_scope': {}}
                code, value = 201, dict(execution_id=execution)
        elif '/executions/' in url:
            value = deepcopy(self.runs[url.rsplit('/', 1)[-1]])
        else:
            raise AssertionError((method, url))
        if code not in expected:
            raise REFRESH.API.SuiteError('unexpected simulated HTTP status')
        return SimpleNamespace(status=code, payload=deepcopy(value))


class ScopedRefreshTest(unittest.TestCase):
    def run_flow(self, storage='postgresql', fault=None):
        server = RefreshServer(storage, fault)
        report = {}
        scenario = REFRESH.ScopedRefreshScenario(server.client, server.client, 3, '44', 8, storage, server, report)
        error = None
        try:
            names = list(server.rows)
            scenario.precise_and_node_flow('public' if storage == 'postgresql' else 'selected', names[0], names[1:])
        except REFRESH.API.SuiteError as caught:
            error = caught
        finally:
            scenario.close()
        return server, report, error

    def test_pg_current_user_precise_node_withdrawal_and_expiry(self):
        server, report, error = self.run_flow()
        self.assertIsNone(error)
        self.assertEqual(report['result'], 'passed')
        self.assertEqual(len(report['checks']), 7)
        self.assertEqual(report['cleanup'], 'zero_current_source_rules_and_active_executions')
        self.assertEqual(server.scans, 4)
        self.assertTrue(all(b['action'] == 'metadata_read' for m, u, b in server.calls if u.endswith('/access_grants')))
        self.assertTrue(all(b['source'] == 'manager' for m, u, b in server.calls if u.endswith('/manual')))
        self.assertEqual({b['scan_depth'] for m, u, b in server.calls if u.endswith('/manual')}, {'basic', 'deep'})

    def test_nfs_nonrecursive_excludes_child_and_recursive_excludes_sibling(self):
        server, report, error = self.run_flow('nfs')
        self.assertIsNone(error)
        self.assertEqual([a['counters']['items_scanned'] for a in report['attempts']], [1, 1, 2, 3])
        self.assertEqual([a['counters']['catalog_nodes_scanned'] for a in report['attempts']], [0, 0, 1, 1])
        self.assertEqual(server.rows['sibling/hidden.txt']['scanned_at'], 'old')

    def test_detects_real_effect_or_privacy_violations_and_still_revokes(self):
        for fault in ('outside_update', 'skip_update', 'parent_enumeration', 'leaked_identity', 'private_plan'):
            with self.subTest(fault=fault):
                server, report, error = self.run_flow(fault=fault)
                self.assertIsInstance(error, REFRESH.API.SuiteError)
                self.assertEqual(report['result'], 'failed')
                self.assertEqual(report['cleanup'], 'zero_current_source_rules_and_active_executions')
                self.assertTrue(all(g.get('revocation') for g in server.grants.values()))

    def test_lost_grant_reply_is_owned_before_post_and_reclaimed(self):
        server, report, error = self.run_flow(fault='unknown_create')
        self.assertIn('lost response', str(error))
        self.assertTrue(all(g.get('revocation') for g in server.grants.values()))
        self.assertEqual(report['cleanup'], 'zero_current_source_rules_and_active_executions')

    def test_cleanup_failure_is_not_counted_as_success(self):
        with self.assertRaisesRegex(REFRESH.API.SuiteError, 'cleanup failed'):
            self.run_flow(fault='cleanup_revoke')

    def test_wrong_actor_cannot_be_claimed_as_a_clean_execution(self):
        with self.assertRaisesRegex(REFRESH.API.SuiteError, 'cleanup failed'):
            self.run_flow(fault='wrong_actor')

    def test_unknown_target_cannot_be_used_to_construct_sql_or_path(self):
        for storage, name in [('nfs', '../selected'), ('nfs', 'data/../x'), ('postgresql', "public/x';DROP")]:
            with self.assertRaises(REFRESH.API.SuiteError):
                REFRESH.path(8, storage, name)

    def test_observer_refuses_local_deployment_before_docker(self):
        with patch.dict(REFRESH.os.environ, {}, clear=True), patch.object(REFRESH.subprocess, 'run') as run:
            with self.assertRaises(REFRESH.API.SuiteError):
                REFRESH.PersistedScanObservation(3, 8, 'nfs').snapshot()
            run.assert_not_called()

    def test_nfs_fixture_refuses_personal_environment_before_disk_or_api(self):
        client = SimpleNamespace(request=lambda *a: self.fail('must not call a personal API'))
        with self.assertRaises(REFRESH.API.SuiteError):
            REFRESH.run_nfs_refresh(client, client, 3, '44', 8, {}, {}, initializer='45')

    def test_inflight_withdrawal_and_expiry_allow_one_read_but_reject_next(self):
        for cutoff in ('withdrawal', 'expiry'):
            for extra_read in (False, True):
                with self.subTest(cutoff=cutoff, extra_read=extra_read):
                    server = RefreshServer(fault='inflight')
                    proof = dict(reset_at='unchanged', calls={})
                    class Barrier:
                        baseline = proof
                        closed = released = False
                        def open(inner, *, tables):
                            self.assertEqual(tables, ('metric_people', 'metric_events'))
                            return {'locked': True}
                        def wait_blocked(inner):
                            return {'pid': 123, 'blocker_pid': 456}
                        def release(inner):
                            inner.released = True
                            for run in server.runs.values():
                                run.update(status='failed', completed_at='2026-11-04T00:00:00Z',
                                           error_details={'category': 'permission_denied'})
                        def counts(inner):
                            return proof
                        def connection_observation(inner, before, after, blocked):
                            return dict(verified=True, by_module={'meta': dict(namespaces=0,
                                tables=2 if extra_read else 1, workspace_detection=0, other=0)})
                        def close(inner):
                            inner.release()
                            inner.closed = True
                    evidence = {}
                    scenario = REFRESH.ScopedRefreshScenario(server.client, server.client, 3, '44', 8,
                        'postgresql', server, evidence)
                    barrier = Barrier()
                    try:
                        if extra_read:
                            with self.assertRaisesRegex(REFRESH.API.SuiteError, 'beyond its single admitted'):
                                scenario.inflight_items('public', ['public.metric_people', 'public.metric_events'], cutoff, barrier)
                        else:
                            scenario.inflight_items('public', ['public.metric_people', 'public.metric_events'], cutoff, barrier)
                            self.assertEqual(evidence['inflight'][cutoff]['result'], 'passed')
                    finally:
                        scenario.close()
                    self.assertTrue(barrier.closed and barrier.released)

    def test_nfs_physical_fixture_cleanup_preserves_first_failure(self):
        for fail_flow, fail_close in ((False, False), (True, False), (True, True)):
            with self.subTest(fail_flow=fail_flow, fail_close=fail_close), tempfile.TemporaryDirectory() as state_dir:
                # Exact /tmp prefix and private owner state mirror the existing
                # Hosted fixture; no NFS server, API or source service is started.
                root = Path('/tmp/addp-online-manager-nfs.' + uuid.uuid4().hex[:6])
                root.mkdir()
                root.joinpath('.addp-online-owner').write_text('owned-test')
                self.addCleanup(lambda p=root: p.rmdir() if p.exists() else None)
                self.addCleanup(lambda p=root/'.addp-online-owner': p.unlink(missing_ok=True))
                secret = Path(state_dir)
                (secret/'nfs-root').write_text(str(root))
                env = dict(ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1', GITHUB_ACTIONS='true', RUNNER_OS='Linux',
                    ADDP_ONLINE_MANAGER_CASE='native-envi', ADDP_ONLINE_SECRET_DIR=state_dir,
                    ADDP_ONLINE_TEST_RUN_ID='owned-test')
                execution = '00000000-0000-4000-8000-000000000001'
                def request(method, url, expected, body=None):
                    return SimpleNamespace(payload=dict(execution_id=execution) if method == 'POST' else
                        dict(execution_id=execution, tenant_id=3, triggered_by=45, source='meta',
                             status='success', metadata={'items_scanned': 4}))
                issuer = SimpleNamespace(request=request)
                failure = RuntimeError('original scoped failure')
                class Scenario:
                    def __init__(inner, *args, **kwargs):
                        pass
                    def precise_and_node_flow(inner, *args):
                        if fail_flow:
                            raise failure
                    def close(inner):
                        if fail_close:
                            raise ValueError('secondary cleanup failure')
                report = {}
                with patch.object(REFRESH, 'ScopedRefreshScenario', Scenario):
                    if fail_flow:
                        with self.assertRaises(RuntimeError) as caught:
                            REFRESH.run_nfs_refresh(issuer, object(), 3, '44', 8, env, report, initializer='45')
                        self.assertIs(caught.exception, failure)
                    else:
                        REFRESH.run_nfs_refresh(issuer, object(), 3, '44', 8, env, report, initializer='45')
                self.assertEqual(list(root.iterdir()), [root/'.addp-online-owner'])
                self.assertTrue(report['source_bytes_unchanged_and_fixture_removed'])
                if fail_close:
                    self.assertEqual(report['cleanup'], 'failed')


if __name__ == '__main__':
    unittest.main()
