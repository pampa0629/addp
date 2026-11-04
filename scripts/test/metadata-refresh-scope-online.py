"""Shared PG/NFS scoped-refresh acceptance; not a production scanner or ACL.

Only existing User APIs perform scans/Grants. The disposable database observer
independently checks persisted effects, never supplies an authorization verdict.
"""
from __future__ import annotations

from datetime import datetime, timedelta, timezone
import hashlib
import importlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
from urllib.parse import urlencode
import uuid

API = importlib.import_module('scripts.utils.online-api')


def require(value, message):
    if not value:
        raise API.SuiteError(message)


def path(engine, storage, name, *, node=False):
    require(storage in {'postgresql', 'nfs'} and type(engine) is int and engine > 0,
            'scoped refresh only admits PG/NFS fixture targets')
    parts = name.split('/')
    require(parts and all(re.fullmatch(r'[A-Za-z0-9_.-]+', p) and p not in {'.', '..'} for p in parts),
            'invalid owned refresh path')
    if storage == 'postgresql':
        require(len(parts) == (1 if node else 2), 'invalid PG refresh target')
        segments = [dict(term='server', kind='server', name=''),
                    dict(term='schema', kind='namespace', name=parts[0])]
        if not node:
            segments.append(dict(term='table', kind='table', name=parts[1]))
    else:
        segments = [dict(term='root', kind='root', name='')]
        segments += [dict(term='directory', kind='directory', name=p) for p in (parts if node else parts[:-1])]
        if not node:
            segments.append(dict(term='file', kind='file', name=parts[-1]))
    return dict(version='catalog.path/v1', engine_id=engine, segments=segments)


class PersistedScanObservation:
    """Bounded, read-only observation on a verified exclusive Hosted database."""
    def __init__(self, tenant, engine, storage):
        require(type(tenant) is int and tenant > 1 and type(engine) is int and engine > 0
                and storage in {'postgresql', 'nfs'}, 'disposable refresh identity required')
        self.tenant, self.engine, self.storage = tenant, engine, storage

    def snapshot(self):
        require(all(os.environ.get(k) == v for k, v in dict(ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1',
                ADDP_ONLINE_TEST='1', GITHUB_ACTIONS='true', RUNNER_OS='Linux', POSTGRES_DB='addp_online').items())
                and sys.platform == 'linux' and not (Path(__file__).resolve().parents[2] / '.env').exists(),
                'refresh observation refuses a personal deployment')
        inspected = subprocess.run(['docker', 'inspect', '--format', '{{json .}}', 'addp-postgres'],
                                   text=True, capture_output=True, check=True, timeout=5)
        owner = json.loads(inspected.stdout)
        require(owner['State']['Running'] is True and owner['Config']['Labels'].get('com.docker.compose.service') == 'postgres'
                and 'POSTGRES_DB=addp_online' in owner['Config']['Env'], 'foreign refresh observation database')
        query = f"""BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;
          SELECT json_build_object('engine',EXISTS(SELECT 1 FROM system.engines
            WHERE id={self.engine} AND tenant_id={self.tenant} AND engine_type='{self.storage}'),
            'count',(SELECT count(*) FROM meta.meta_item WHERE tenant_id={self.tenant} AND engine_id={self.engine}),
            'items',COALESCE((SELECT json_agg(row_to_json(s) ORDER BY id) FROM
              (SELECT i.id,i.node_id,i.full_name,i.scanned_at,n.scanned_at AS node_scanned_at,
                 n.scanned_depth AS node_scanned_depth,n.scan_status AS node_scan_status
               FROM meta.meta_item i JOIN meta.meta_node n ON n.id=i.node_id
                 AND n.tenant_id=i.tenant_id AND n.engine_id=i.engine_id
               WHERE i.tenant_id={self.tenant} AND i.engine_id={self.engine} ORDER BY i.id LIMIT 257) s),'[]'::json));
          COMMIT;"""
        result = subprocess.run(['docker', 'exec', '-i', owner['Id'], 'psql', '-XqAt', '-v', 'ON_ERROR_STOP=1',
            '-U', os.environ['POSTGRES_USER'], '-d', 'addp_online'], input=query, text=True,
            capture_output=True, check=True, timeout=10)
        require(len(result.stdout) <= 131072, 'refresh observation exceeded its budget')
        payload = json.loads(result.stdout)
        rows = payload.get('items')
        require(payload.get('engine') is True and type(rows) is list and len(rows) == payload.get('count') <= 256
                and all(type(r.get('id')) is int and type(r.get('node_id')) is int and isinstance(r.get('full_name'), str)
                        for r in rows) and len({r['full_name'] for r in rows}) == len(rows),
                'refresh observation lost its complete engine snapshot')
        return {r['full_name']: r for r in rows}


class ScopedRefreshScenario:
    def __init__(self, issuer, scanner, tenant, principal, engine, storage, observer, evidence, *, register=None):
        self.issuer, self.scanner = issuer, scanner
        self.tenant, self.principal, self.engine, self.storage = tenant, str(principal), engine, storage
        self.observer, self.evidence, self.register = observer, evidence, register
        self.base = f'/api/v1/system/engines/{engine}'
        self.commands, self.executions = {}, []
        self.token = scanner.token
        evidence.update(result='failed', checks=[], attempts=[], cleanup='pending')

    def grant(self, target, *, recursive=None, fingerprint=None, seconds=300):
        body = dict(request_id=str(uuid.uuid4()), catalog_path=target, recipient_type='user',
            recipient_id=self.principal, action='metadata_read', expiry_mode='at_time',
            expires_at=(datetime.now(timezone.utc) + timedelta(seconds=seconds)).isoformat(),
            reason='Disposable PG/NFS scoped metadata refresh acceptance')
        if recursive is not None:
            require(type(recursive) is bool and fingerprint is None, 'ambiguous refresh Grant')
            body['node_recursive'] = recursive
        else:
            require(isinstance(fingerprint, str) and re.fullmatch(r'[a-f0-9]{64}', fingerprint), 'exact identity required')
            facts = self.issuer.request('GET', self.base + f'/data-items/{fingerprint}/authorization-facts', (200,)).payload
            require(facts.get('item_fingerprint') == fingerprint and facts.get('basis_state') == 'complete'
                    and facts.get('engine_id') == str(self.engine), 'refresh facts mismatch')
            body.update(item_fingerprint=fingerprint, expected_boundary_hash=facts['boundary_hash'])
        self.commands[body['request_id']] = body  # Register before an unknown POST outcome.
        if self.register is not None:
            self.register(body)
        receipt = self.issuer.request('POST', self.base + '/access_grants', (201,), body).payload
        require(all(receipt.get(k) == body[k] for k in ('request_id', 'catalog_path', 'recipient_type', 'recipient_id',
                'action', 'expiry_mode')) and receipt.get('node_recursive') == recursive
                and receipt.get('revocation') is None and receipt.get('engine_id') == str(self.engine)
                and datetime.fromisoformat(receipt['expires_at'].replace('Z', '+00:00')) == datetime.fromisoformat(body['expires_at']),
                'refresh Grant receipt changed its target, subject, action or expiry')
        if recursive is None:
            require(receipt.get('item_fingerprint') == fingerprint and receipt.get('boundary_hash') == body['expected_boundary_hash'],
                    'precise refresh Grant lost its approved boundary')
        else:
            require(type(receipt.get('node_recursive')) is bool and not receipt.get('item_fingerprint')
                    and not receipt.get('boundary_hash'), 'node refresh Grant became a leaf snapshot')
        return receipt

    def history(self, body, state):
        query = dict(catalog_path=json.dumps(body['catalog_path'], separators=(',', ':')),
                     recipient_type='user', recipient_id=self.principal, state=state, page_size=100)
        rows = self.issuer.request('GET', self.base + '/access_grants/history?' + urlencode(query), (200,)).payload
        require(type(rows.get('data')) is list and rows.get('total') == len(rows['data']) <= 100,
                'refresh cleanup history is incomplete')
        return rows['data']

    def revoke(self, receipt):
        self.issuer.request('POST', self.base + f'/access_grants/{receipt["request_id"]}/revoke', (200,),
                            {'reason': 'Disposable scoped refresh withdrawal'})

    def start(self, node_name, node_id, *, denied=False, depth='basic'):
        require(depth in {'basic', 'deep'}, 'unknown refresh depth')
        before = self.observer.snapshot()
        target = f'addp://engine/{self.engine}/path/{node_name}?' + urlencode(
            dict(type='schema' if self.storage == 'postgresql' else 'dir', node_id=node_id))
        response = self.scanner.request('POST', '/api/v1/meta/scan/run/manual', (403,) if denied else (201,),
            dict(engine_id=self.engine, targets=[target], scan_depth=depth, force=True,
                 trigger_type='manual', source='manager'))
        if denied:
            require(set(response.payload) <= {'error', 'error_code'} and response.payload.get('error')
                    and self.observer.snapshot() == before, 'denied refresh leaked details or changed persisted facts')
            return None
        execution = response.payload.get('execution_id')
        require(isinstance(execution, str) and str(uuid.UUID(execution)) == execution and execution not in self.executions,
                'refresh did not create one exact execution')
        self.executions.append(execution)
        return execution

    def observe(self, execution, *, expected_items, expected_nodes, forbidden=(), failed=False, directories=None, depth='basic'):
        attempt = dict(execution_id=execution, terminal='pending', privacy_verified=False)
        self.evidence['attempts'].append(attempt)
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            row = self.scanner.request('GET', f'/api/v1/meta/executions/{execution}', (200,)).payload
            text = json.dumps(row, ensure_ascii=False)
            def safe(value):
                if isinstance(value, dict):
                    return not set(value).intersection({'execution_config', 'actor_origin', 'lease_token', 'refresh_scope',
                        'sample_data', 'samples', 'statistics', 'row_count', 'connection_info', 'access_token'}) and all(safe(v) for v in value.values())
                return not isinstance(value, list) or all(safe(v) for v in value)
            require(not any(name and name in text for name in forbidden)
                    and safe(row),
                    'refresh execution disclosed an unauthorized identity or private plan')
            require(row.get('execution_id') == execution and row.get('tenant_id') == self.tenant
                    and row.get('triggered_by') == int(self.principal) and row.get('source') == 'manager'
                    and row.get('source_task_id') is None
                    and row.get('scan_context') == dict(engine_id=self.engine, scan_depth=depth),
                    'refresh changed its User, engine, target class or depth')
            attempt['terminal'] = row.get('status')
            metadata = row.get('metadata', {})
            attempt['counters'] = {key: value if type(value) is int and 0 <= value <= 10**9 else 'absent_or_invalid'
                for key in ('items_scanned', 'catalog_nodes_scanned', 'fields_scanned') for value in [metadata.get(key)]}
            if row.get('status') in {'pending', 'running'}:
                time.sleep(.2)
                continue
            require(row.get('status') == ('failed' if failed else 'success') and row.get('completed_at'),
                    'refresh did not reach its expected terminal state')
            if not failed:
                require(metadata.get('items_scanned') == expected_items
                        and metadata.get('catalog_nodes_scanned') == expected_nodes,
                        'refresh enumerated or processed beyond its authorized range')
                if directories is not None:
                    progress = row.get('scan_progress', {})
                    require(progress.get('directories_discovered') == directories
                            and progress.get('files_discovered') == progress.get('items_planned') == expected_items,
                            'NFS refresh enumerated an unexpected number of physical directories')
            else:
                require(row.get('error_details', {}).get('category') == 'permission_denied',
                        'refresh in-flight rejection was not an authorization failure')
            attempt.update(privacy_verified=True, counters={key: metadata.get(key) for key in
                           ('items_scanned', 'catalog_nodes_scanned', 'fields_scanned')})
            return row
        raise API.SuiteError('scoped refresh exceeded its execution deadline')

    def run_once(self, node_name, node_id, expected_names, *, nodes, forbidden=(), directories=None, depth='basic'):
        before = self.observer.snapshot()
        execution = self.start(node_name, node_id, depth=depth)
        self.observe(execution, expected_items=len(expected_names), expected_nodes=nodes, forbidden=forbidden,
                     directories=directories, depth=depth)
        after = self.observer.snapshot()
        proof = self.evidence['attempts'][-1]
        proof['identity_set_unchanged'] = before.keys() == after.keys()
        require(proof['identity_set_unchanged'], 'refresh added or deleted unrelated identities')
        changed = {name for name in before if before[name] != after[name]}
        proof['only_expected_items_and_parent_states_changed'] = changed == set(expected_names)
        require(proof['only_expected_items_and_parent_states_changed'], 'persisted refresh effects exceeded or missed the exact allowed set')
        return execution

    def close(self):
        errors = []
        for body in self.commands.values():
            try:
                rows = self.history(body, 'active')
                for row in rows:
                    if row.get('request_id') == body['request_id']:
                        require(all(row.get(k) == body[k] for k in ('catalog_path', 'recipient_type', 'recipient_id', 'action'))
                                and row.get('engine_id') == str(self.engine), 'refusing a foreign refresh Grant')
                        self.revoke(row)
                require(not any(row.get('request_id') == body['request_id'] for row in self.history(body, 'active')),
                        'owned refresh Grant remains active')
            except Exception:
                errors.append('owned refresh Grant cleanup failed')
        for execution in self.executions:
            try:
                deadline = time.monotonic() + 120
                while True:
                    row = self.scanner.request('GET', f'/api/v1/meta/executions/{execution}', (200,)).payload
                    require(row.get('execution_id') == execution and row.get('tenant_id') == self.tenant
                            and row.get('triggered_by') == int(self.principal), 'foreign refresh execution during cleanup')
                    if row.get('status') not in {'pending', 'running'}:
                        break
                    require(time.monotonic() < deadline, 'refresh cleanup did not terminate')
                    time.sleep(.2)
                require(row.get('status') in {'success', 'failed', 'cancelled', 'timeout'}, 'refresh execution remains active')
            except Exception:
                errors.append('refresh execution cleanup verification failed')
        self.evidence['cleanup'] = 'failed' if errors else 'zero_current_source_rules_and_active_executions'
        require(not errors, 'scoped refresh cleanup failed')

    def precise_and_node_flow(self, node_name, current_name, hidden_names, *, node_count=1):
        """One current User, no content Grant, no reread via an admin identity."""
        target = path(self.engine, self.storage, node_name, node=True)
        # Identity comes from the test's independent persisted-effects observer.
        # It is not used to grant read authority; System independently proves it.
        initial = self.observer.snapshot()
        require(current_name in initial and all(n in initial for n in hidden_names), 'refresh fixture identity missing')
        current = initial[current_name]
        fingerprint = hashlib.sha256(f'{self.engine}:{current_name}'.encode()).hexdigest()
        self.evidence['phase'] = 'ungranted_denial'
        self.start(node_name, current['node_id'], denied=True)
        self.evidence['phase'] = 'precise_basic'
        exact = self.grant(path(self.engine, self.storage, current_name.replace('.', '/', 1) if self.storage == 'postgresql' else current_name),
                           fingerprint=fingerprint)
        self.run_once(node_name, current['node_id'], [current_name], nodes=0,
                      forbidden=[name for name in initial if name != current_name])
        self.evidence['checks'].append('precise_item_refresh_without_parent_enumeration')
        self.evidence['phase'] = 'precise_deep'
        self.run_once(node_name, current['node_id'], [current_name], nodes=0, depth='deep',
                      forbidden=[name for name in initial if name != current_name])
        self.evidence['checks'].append('same_metadata_grant_deep_refresh_stays_exact')
        self.evidence['phase'] = 'withdrawal'
        self.revoke(exact)
        self.start(node_name, current['node_id'], denied=True)
        self.evidence['checks'].append('withdrawal_denied_before_enqueue')
        self.evidence['phase'] = 'nonrecursive_node'
        node = self.grant(target, recursive=False)
        names = [name for name in initial if (name.startswith(node_name + '.') if self.storage == 'postgresql'
                 else name.rsplit('/', 1)[0] == node_name)]
        excluded = [name for name in initial if name not in names]
        self.run_once(node_name, current['node_id'], names, nodes=node_count, forbidden=excluded,
                      directories=1 if self.storage == 'nfs' else None)
        self.evidence['checks'].append('nonrecursive_node_does_not_scan_descendants_or_siblings')
        self.revoke(node)
        self.evidence['phase'] = 'recursive_node'
        recursive = self.grant(target, recursive=True)
        names = [name for name in initial if name.startswith(node_name + ('.' if self.storage == 'postgresql' else '/'))]
        excluded = [name for name in initial if name not in names]
        # catalog_nodes_scanned counts submitted roots, not recursive physical
        # directories. Keep both independent counters instead of conflating them.
        self.run_once(node_name, current['node_id'], names, nodes=node_count, forbidden=excluded,
                      directories=len({initial[name]['node_id'] for name in names}) if self.storage == 'nfs' else None)
        self.evidence['checks'].append('recursive_node_keeps_siblings_untouched')
        self.revoke(recursive)
        self.evidence['phase'] = 'natural_expiry'
        expiring = self.grant(target, recursive=False, seconds=10)
        deadline = time.monotonic() + 40
        while not any(row.get('request_id') == expiring['request_id'] and row.get('revocation') is None
                      for row in self.history(expiring, 'expired')):
            require(time.monotonic() < deadline, 'refresh Grant did not naturally expire')
            time.sleep(.2)
        self.start(node_name, current['node_id'], denied=True)
        require(self.scanner.token == self.token, 'refresh acceptance replaced the User credential')
        self.evidence['checks'].extend(['natural_expiry_denied_without_withdrawal', 'same_user_credential_and_safe_progress'])
        self.evidence['result'] = 'passed'
        self.evidence['phase'] = 'completed'

    def inflight_items(self, node_name, names, cutoff, barrier):
        """Two exact ranges: release one proven in-flight source read after cutoff.

        Lock both fixture tables so the proof does not assume the canonical
        fingerprint ordering. Never interrupt an already admitted source call.
        """
        require(self.storage == 'postgresql' and cutoff in {'withdrawal', 'expiry'}
                and set(names) == {'public.metric_people', 'public.metric_events'}, 'invalid owned in-flight case')
        self.evidence['result'] = 'failed'
        evidence = self.evidence.setdefault('inflight', {})[cutoff] = {'result': 'failed'}
        before = self.observer.snapshot()
        node_id = before[names[0]]['node_id']
        receipts = []
        failure = None
        try:
            evidence['lock'] = barrier.open(tables=('metric_people', 'metric_events'))
            for name in names:
                receipts.append(self.grant(path(self.engine, self.storage, name.replace('.', '/', 1)),
                    fingerprint=hashlib.sha256(f'{self.engine}:{name}'.encode()).hexdigest(),
                    seconds=15 if cutoff == 'expiry' else 300))
            execution = self.start(node_name, node_id)
            evidence['blocked'] = barrier.wait_blocked()
            if cutoff == 'withdrawal':
                for receipt in receipts:
                    self.revoke(receipt)
            else:
                deadline = time.monotonic() + 25
                for receipt in receipts:
                    while not any(row.get('request_id') == receipt['request_id'] and row.get('revocation') is None
                                  for row in self.history(receipt, 'expired')):
                        require(time.monotonic() < deadline, 'in-flight Grant did not naturally expire')
                        time.sleep(.2)
            barrier.release()
            self.observe(execution, expected_items=0, expected_nodes=0, failed=True,
                         forbidden=[name for name in before if name not in names])
            after = barrier.counts()
            connection = barrier.connection_observation(barrier.baseline, after, evidence['blocked'])
            evidence.update(source_counter_baseline=barrier.baseline, source_counter_observed=after,
                            connection_observation=connection)
            require(after['reset_at'] == barrier.baseline['reset_at'] and connection.get('verified') is True
                    and connection.get('by_module', {}).get('meta') ==
                        dict(namespaces=0, tables=1, workspace_detection=0, other=0),
                    'scoped refresh read beyond its single admitted exact lookup')
            persisted = self.observer.snapshot()
            changed = {name for name in before if before[name] != persisted.get(name)}
            require(before.keys() == persisted.keys() and changed <= set(names) and len(changed) <= 1,
                    'in-flight refresh updated a later range or an unrelated identity')
            self.start(node_name, node_id, denied=True)
            evidence['result'] = 'passed'
            self.evidence['checks'].append('inflight_' + cutoff + '_stops_next_exact_source_read')
        except BaseException as error:
            failure = error
            raise
        finally:
            try:
                barrier.close()
                require(barrier.closed and barrier.released, 'source barrier remained active')
                evidence['source_cleanup'] = 'zero_source_locks_and_schema'
            except Exception:
                evidence.update(result='failed', source_cleanup='failed')
                if failure is None:
                    raise
        self.evidence['result'] = 'passed'


def run_nfs_refresh(issuer, scanner, tenant, principal, engine, environment, evidence, *, initializer):
    """Reuse the owned native-envi NFS server and its existing scan operator.

    The ENVI consumer already has scan execute/read. Reuse it without adding
    roles; only the existing initializer performs initial fixture identification.
    """
    require(all(environment.get(k) == v for k, v in dict(ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1',
                GITHUB_ACTIONS='true', RUNNER_OS='Linux', ADDP_ONLINE_MANAGER_CASE='native-envi').items()),
            'NFS refresh requires the existing isolated ENVI profile')
    secret = Path(environment['ADDP_ONLINE_SECRET_DIR'])
    state = secret / 'nfs-root'
    require(secret.is_absolute() and not secret.is_symlink() and not state.is_symlink()
            and secret.stat().st_mode & 0o077 == 0, 'invalid private NFS fixture state')
    root = Path(state.read_text().strip())
    require(re.fullmatch(r'/tmp/addp-online-manager-nfs\.[A-Za-z0-9]{6}', str(root))
            and root.is_dir() and not root.is_symlink()
            and (root / '.addp-online-owner').read_text().strip() == environment['ADDP_ONLINE_TEST_RUN_ID'],
            'refusing a foreign NFS source')
    name = 'refresh_scope_' + uuid.uuid4().hex
    directory = root / name
    expected = {p: b'metadata scope acceptance\n' for p in
        ('selected/current.txt', 'selected/private.txt', 'selected/child/nested.txt', 'neighbor/other.txt')}
    observer = PersistedScanObservation(tenant, engine, 'nfs')
    scenario = ScopedRefreshScenario(issuer, scanner, tenant, principal, engine, 'nfs', observer, evidence)
    owned = False
    error = None
    seed_execution = None
    try:
        require(not directory.exists(), 'NFS refresh fixture already exists')
        directory.mkdir(mode=0o755)
        owned = True
        for relative, raw in expected.items():
            target = directory / relative
            target.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
            target.write_bytes(raw)
            target.chmod(0o644)
        created = issuer.request('POST', '/api/v1/meta/scan/run/manual', (201,),
            dict(engine_id=engine, catalog_paths=[name], scan_depth='basic', force=True,
                 trigger_type='manual', source='meta')).payload
        execution = created.get('execution_id')
        require(isinstance(execution, str) and str(uuid.UUID(execution)) == execution, 'missing NFS seed identification')
        seed_execution = execution
        deadline = time.monotonic() + 120
        while True:
            row = issuer.request('GET', f'/api/v1/meta/executions/{execution}', (200,)).payload
            require(row.get('execution_id') == execution and row.get('tenant_id') == tenant
                    and row.get('triggered_by') == int(initializer) and row.get('source') == 'meta',
                    'NFS seed scan changed its operator or origin')
            if row.get('status') not in {'pending', 'running'}:
                require(row.get('status') == 'success' and row.get('metadata', {}).get('items_scanned') == len(expected),
                        'NFS seed identification failed or widened its exact fixture range')
                break
            require(time.monotonic() < deadline, 'NFS seed scan did not terminate')
            time.sleep(.2)
        evidence['seed_execution_id'] = execution
        scenario.precise_and_node_flow(name + '/selected', name + '/selected/current.txt',
                                      [name + '/' + p for p in expected if p != 'selected/current.txt'])
    except BaseException as caught:
        error = caught
        raise
    finally:
        cleanup_errors = []
        if seed_execution is not None:
            try:
                deadline = time.monotonic() + 120
                while True:
                    row = issuer.request('GET', f'/api/v1/meta/executions/{seed_execution}', (200,)).payload
                    require(row.get('execution_id') == seed_execution and row.get('tenant_id') == tenant
                            and row.get('triggered_by') == int(initializer), 'foreign NFS seed execution')
                    if row.get('status') not in {'pending', 'running'}:
                        require(row.get('status') in {'success', 'failed', 'cancelled', 'timeout'}, 'unknown NFS seed terminal')
                        break
                    require(time.monotonic() < deadline, 'NFS seed cleanup did not terminate')
                    time.sleep(.2)
            except Exception:
                cleanup_errors.append('seed_execution')
        try:
            scenario.close()
        except Exception:
            cleanup_errors.append('authorization')
        if owned:
            try:
                # Exact ledger only: do not recursively delete arbitrary contents
                # or follow a path a failed scan/upload might have replaced.
                require(not directory.is_symlink(), 'NFS fixture changed identity')
                for relative, raw in expected.items():
                    target = directory / relative
                    if target.exists():
                        require(not target.is_symlink() and target.is_file() and target.read_bytes() == raw,
                                'NFS refresh altered source bytes or introduced a link')
                        target.unlink()
                for relative in ('selected/child', 'selected', 'neighbor', ''):
                    target = directory / relative
                    if target.exists():
                        target.rmdir()
                require(not directory.exists(), 'NFS refresh fixture remains')
                evidence['source_bytes_unchanged_and_fixture_removed'] = True
            except Exception:
                cleanup_errors.append('physical_fixture')
        if cleanup_errors:
            evidence.update(result='failed', cleanup='failed', cleanup_errors=cleanup_errors)
            if error is None:
                raise API.SuiteError('NFS refresh cleanup failed')
