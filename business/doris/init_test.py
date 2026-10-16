"""Single-Backend migration and readiness without personal Docker services."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class DorisInitializationTest(unittest.TestCase):
    def test_compose_keeps_self_resolution_and_external_ports_consistent(self):
        result = subprocess.run(['docker', 'compose', '-f', str(ROOT / 'business/docker-compose.yml'),
                                 'config', '--format', 'json'], capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stderr)
        services = json.loads(result.stdout)['services']
        doris = services['doris-fe']
        self.assertEqual(doris['extra_hosts'], ['host.docker.internal=127.0.0.1'])
        self.assertEqual(doris['entrypoint'], ['/bin/bash', '/addp/doris/init.sh'])
        self.assertEqual(doris['healthcheck']['test'], ['CMD', 'bash', '/addp/doris/init.sh', '--health'])
        self.assertEqual({port['target'] for port in doris['ports']}, {9030, 8030, 8040, 9050, 9060, 8060})
        self.assertTrue(all(port['host_ip'] == '127.0.0.1' for port in doris['ports']))
        for port in doris['ports']:
            if port['target'] in {8040, 9050, 9060, 8060}:
                self.assertEqual(str(port['published']), str(port['target']))
        self.assertIn('/opt/apache-doris/fe/doris-meta', {v['target'] for v in doris['volumes']})
        self.assertIn('/opt/apache-doris/be/storage', {v['target'] for v in doris['volumes']})

    def run_fixture(self, rows, command, *, change_identity=False, reject_mutation=False,
                    reject_query=False):
        with tempfile.TemporaryDirectory() as work:
            directory = Path(work)
            state = directory / 'state.json'
            state.write_text(json.dumps({'rows': rows, 'mutations': []}))
            mysql = directory / 'mysql'
            mysql.write_text('#!' + sys.executable + '\n' + r'''
import json, os, sys
from pathlib import Path
p = Path(os.environ['DORIS_FIXTURE_STATE']); s = json.loads(p.read_text())
sql = sys.argv[-1]
if sql == "SHOW PROC '/backends';":
    if os.environ.get('DORIS_REJECT_QUERY'): sys.exit(1)
    if s['rows']: print('Host\tAlive\tBackendId\tHeartbeatPort')
    for identifier, host, alive in s['rows']:
        print(f'{host}\t{alive}\t{identifier}\t9050')
elif sql == 'SHOW FRONTENDS;': print('FE')
elif sql.startswith('ALTER SYSTEM'):
    s['mutations'].append(sql); p.write_text(json.dumps(s))
    if os.environ.get('DORIS_REJECT_MUTATION'): sys.exit(1)
    if sql.startswith('ALTER SYSTEM ADD BACKEND'):
        assert not s['rows']; s['rows'] = [[10004, 'host.docker.internal', 'true']]
    else:
        assert sql == 'ALTER SYSTEM MODIFY BACKEND "127.0.0.1:9050" HOSTNAME "host.docker.internal";'
        assert len(s['rows']) == 1; s['rows'][0][1] = 'host.docker.internal'
        if os.environ.get('DORIS_CHANGE_ID'): s['rows'][0][0] += 1
else: raise AssertionError(sql)
p.write_text(json.dumps(s))
''')
            mysql.chmod(0o755)
            env = dict(os.environ, PATH=work + os.pathsep + os.environ['PATH'], DORIS_FIXTURE_STATE=str(state))
            env.pop('DORIS_CHANGE_ID', None); env.pop('DORIS_REJECT_MUTATION', None)
            env.pop('DORIS_REJECT_QUERY', None)
            if change_identity: env['DORIS_CHANGE_ID'] = '1'
            if reject_mutation: env['DORIS_REJECT_MUTATION'] = '1'
            if reject_query: env['DORIS_REJECT_QUERY'] = '1'
            result = subprocess.run(['bash', '-c', 'source "$1"; ' + command,
                                     'fixture', str(ROOT / 'business/doris/init.sh')],
                                    env=env, text=True, capture_output=True, timeout=30)
            return result, json.loads(state.read_text())

    def test_existing_backend_migrates_once_and_keeps_identity(self):
        result, state = self.run_fixture([[10004, '127.0.0.1', 'true']],
                                         'doris_register_backend; doris_register_backend; doris_health')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(state['rows'], [[10004, 'host.docker.internal', 'true']])
        self.assertEqual(len(state['mutations']), 1)
        self.assertIn('MODIFY BACKEND', state['mutations'][0])

    def test_new_cluster_registers_only_once(self):
        result, state = self.run_fixture([], 'doris_register_backend; doris_register_backend; doris_health')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(state['mutations']), 1)
        self.assertIn('ADD BACKEND', state['mutations'][0])

    def test_empty_batch_output_is_a_successful_empty_backend_directory(self):
        result, state = self.run_fixture([], 'doris_backend_facts')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, '')
        self.assertEqual(state['mutations'], [])
        failed, state = self.run_fixture([], 'doris_backend_facts', reject_query=True)
        self.assertNotEqual(failed.returncode, 0)
        self.assertEqual(state['mutations'], [])

    def test_unexpected_topology_and_identity_changes_are_rejected(self):
        for rows in ([[10004, 'foreign.example', 'true']],
                     [[10004, '127.0.0.1', 'true'], [10005, '127.0.0.2', 'true']]):
            result, state = self.run_fixture(rows, 'doris_register_backend')
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(state['mutations'], [])
        result, _ = self.run_fixture([[10004, '127.0.0.1', 'true']],
                                     'doris_register_backend', change_identity=True)
        self.assertNotEqual(result.returncode, 0)
        result, state = self.run_fixture([[10004, '127.0.0.1', 'true']],
                                         'doris_register_backend', reject_mutation=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(len(state['mutations']), 1)

    def test_frontend_queryable_does_not_hide_backend_not_ready(self):
        for rows in ([], [[10004, '127.0.0.1', 'true']], [[10004, 'host.docker.internal', 'false']]):
            result, state = self.run_fixture(rows, 'doris_health')
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(state['mutations'], [])


if __name__ == '__main__':
    unittest.main()
