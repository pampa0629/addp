import importlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch
import zipfile

owner = importlib.import_module('scripts.test.manager-filegdb-export-online')
MYSQL_FIXTURE = Path(__file__).resolve().parents[2] / 'business/scripts/online-manager-mysql-fixture.sh'


class MySQLFixtureLifecycleTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.private = self.root / 'private'; self.private.mkdir(mode=0o700)
        binary = self.root / 'bin'; binary.mkdir()
        uname = binary / 'uname'; uname.write_text('#!/bin/sh\n[ "$1" = -s ] && echo Linux || echo x86_64\n'); uname.chmod(0o755)
        docker = binary / 'docker'
        docker.write_text('''#!/usr/bin/env python3
import json,os,pathlib,sys
p=pathlib.Path(os.environ['P82_MYSQL_STATE']);s=json.loads(p.read_text()) if p.exists() else {'exists':False,'calls':[]}
a=sys.argv[1:];data=sys.stdin.read() if a and a[0]=='exec' else ''
s['calls'].append({'args':a,'sql':data})
code=0
if a[:2]==['container','inspect']:code=0 if s['exists'] else 1
elif a and a[0]=='compose':print(json.dumps({'services':{'mysql':{'image':'mysql:fixture-pin'}}}))
elif a and a[0]=='run':
 s.update(exists=True,root_password=os.environ['MYSQL_ROOT_PASSWORD'],labels={a[i+1].split('=',1)[0]:a[i+1].split('=',1)[1] for i,v in enumerate(a) if v=='--label'})
 print('owned-mysql-id')
elif a and a[0]=='inspect':
 if not s['exists']:code=1
 elif 'NetworkSettings.Ports' in a[2]:print('127.0.0.1:59306')
 else:
  key='com.addp.online-fixture' if 'com.addp.online-fixture' in a[2] else 'com.addp.online-run'
  print(s['labels'][key])
elif a and a[0]=='rm':s['exists']=False
elif a and a[0]=='exec':
 if 'CREATE DATABASE' in data and os.environ.get('P82_MYSQL_SEED_FAIL')=='1':
  print('private seed detail '+os.environ['MYSQL_PWD'],file=sys.stderr);code=1
 elif 'JSON_OBJECT' in data:print(json.dumps({'count':3,'rows':'a'*32,'columns':'b'*32}))
 elif 'SELECT 1' in a:print('1')
else:code=99
p.write_text(json.dumps(s));sys.exit(code)
''')
        docker.chmod(0o755)
        self.state = self.root / 'state.json'
        self.descriptor = self.private / 'mysql.json'
        self.env = dict(os.environ, PATH=str(binary) + os.pathsep + os.environ['PATH'], GITHUB_ACTIONS='true',
            RUNNER_OS='Linux', ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1', POSTGRES_DB='addp_online',
            ADDP_ONLINE_MANAGER_CASE='filegdb-mysql-export', ADDP_ONLINE_TEST='1',
            ADDP_ONLINE_TEST_RUN_ID='manager-7-1', ADDP_ONLINE_SECRET_DIR=str(self.private),
            ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE=str(self.descriptor), P82_MYSQL_STATE=str(self.state))

    def invoke(self, *args, **environment):
        return subprocess.run(['bash', str(MYSQL_FIXTURE), *args], env={**self.env, **environment},
                              capture_output=True, text=True, timeout=20)

    def test_owned_mysql_seed_snapshot_and_cleanup_keep_credentials_private(self):
        started = self.invoke('start')
        self.assertEqual(started.returncode, 0, started.stdout + started.stderr)
        descriptor = json.loads(self.descriptor.read_text())
        self.assertEqual((descriptor['engine_type'], descriptor['connection_info']['database'], descriptor['connection_info']['port']),
                         ('mysql', 'filegdb_fixture', 59306))
        self.assertEqual(self.descriptor.stat().st_mode & 0o777, 0o600)
        state = json.loads(self.state.read_text())
        for secret in [state['root_password'], descriptor['connection_info']['password']]:
            self.assertNotIn(secret, started.stdout + started.stderr)
        seed = next(c['sql'] for c in state['calls'] if 'CREATE DATABASE' in c['sql'])
        self.assertEqual(seed.count('ENGINE=InnoDB'), 3)
        self.assertEqual(seed.count('GRANT SELECT ON'), 3)
        self.assertNotIn('GRANT ALL', seed)
        snapshot = self.invoke('snapshot-filegdb', 'filegdb_export_points')
        self.assertEqual(snapshot.returncode, 0, snapshot.stderr)
        self.assertEqual(json.loads(snapshot.stdout)['count'], 3)
        before = self.state.read_bytes()
        self.assertNotEqual(self.invoke('snapshot-filegdb', 'another_table').returncode, 0)
        self.assertEqual(self.state.read_bytes(), before)
        self.assertEqual(self.invoke('stop').returncode, 0)
        self.assertFalse(json.loads(self.state.read_text())['exists'])

    def test_foreign_run_and_unapproved_case_are_rejected_without_removing_source(self):
        self.assertNotEqual(self.invoke('start', ADDP_ONLINE_MANAGER_CASE='full').returncode, 0)
        self.assertFalse(self.state.exists())
        self.assertEqual(self.invoke('start').returncode, 0)
        state = json.loads(self.state.read_text());state['labels']['com.addp.online-run'] = 'another-run';self.state.write_text(json.dumps(state))
        self.assertNotEqual(self.invoke('stop').returncode, 0)
        self.assertTrue(json.loads(self.state.read_text())['exists'])

    def test_failed_seed_retains_private_diagnostics_and_allows_owned_cleanup(self):
        failed = self.invoke('start', P82_MYSQL_SEED_FAIL='1')
        self.assertNotEqual(failed.returncode, 0)
        state = json.loads(self.state.read_text())
        self.assertNotIn(state['root_password'], failed.stdout + failed.stderr)
        self.assertEqual((self.private / 'mysql-seed.log').stat().st_mode & 0o777, 0o600)
        self.assertFalse(self.descriptor.exists())
        self.assertEqual(self.invoke('stop').returncode, 0)
        self.assertFalse(json.loads(self.state.read_text())['exists'])


class FileGDBArchiveProofTest(unittest.TestCase):
    def test_native_profiles_are_explicit_and_mysql_uses_database_identity(self):
        mysql = owner.source_profile({'ADDP_ONLINE_MANAGER_CASE': 'filegdb-mysql-export'})
        self.assertEqual((mysql['engine_type'], mysql['namespace'], mysql['term']), ('mysql', 'filegdb_fixture', 'database'))
        self.assertTrue(mysql['fixture'].endswith('online-manager-mysql-fixture.sh'))
        pg = owner.source_profile({'ADDP_ONLINE_MANAGER_CASE': 'filegdb-export'})
        self.assertEqual((pg['engine_type'], pg['namespace'], pg['term']), ('postgresql', 'public', 'schema'))
        for value in (None, '', 'MYSQL', 'full'):
            with self.subTest(value=value), self.assertRaises(ValueError):
                owner.source_profile({'ADDP_ONLINE_MANAGER_CASE': value})

    def test_mysql_snapshot_uses_only_its_owned_source_fixture(self):
        import json
        environment = {'ADDP_ONLINE_MANAGER_CASE': 'filegdb-mysql-export'}
        output = {'count': 3, 'rows': 'a' * 32, 'columns': 'b' * 32}
        with patch.object(owner.subprocess, 'run', return_value=SimpleNamespace(stdout=json.dumps(output))) as run:
            self.assertEqual(owner.source_snapshot(Path('/repo'), environment, 'filegdb_export_points'), output)
        self.assertEqual(run.call_args.args[0], ['bash', 'business/scripts/online-manager-mysql-fixture.sh',
                                               'snapshot-filegdb', 'filegdb_export_points'])
        helper, initializer = Mock(), Mock()
        helper.wait_for_meta_scan.return_value = 'scan-7'
        initializer.request.return_value = SimpleNamespace(payload={'metadata': {'items_scanned': 1, 'catalog_nodes_scanned': 0}})
        locator, _ = owner.scan_source(helper, initializer, 2, 'filegdb_export_points', 'filegdb_fixture')
        self.assertEqual(locator, 'addp://engine/2/path/filegdb_fixture/filegdb_export_points?type=table')

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
        locator, execution = owner.scan_source(helper, initializer, 2, 'filegdb_export_points', 'public')
        self.assertEqual(locator, 'addp://engine/2/path/public/filegdb_export_points?type=table')
        self.assertEqual(execution, 'execution-7')
        self.assertEqual(helper.wait_for_meta_scan.call_args.kwargs, {'scope': {'targets': [locator]}})
        initializer.request.return_value.payload['metadata']['items_scanned'] = 0
        with self.assertRaisesRegex(ValueError, 'requires one scanned source table'):
            owner.scan_source(helper, initializer, 2, 'filegdb_export_points', 'public')
