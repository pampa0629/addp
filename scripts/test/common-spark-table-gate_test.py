"""Deterministic registration and disposable lifecycle contract, no Docker needed."""
from pathlib import Path
import json
import os
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class SparkTableGateTest(unittest.TestCase):
    def test_lifecycle_cleans_success_and_failure_and_rejects_leaked_volumes(self):
        docker = r'''
import json, os, sys
from pathlib import Path
p = Path(os.environ['FAKE_STATE'])
s = json.loads(p.read_text()) if p.exists() else {'containers': [], 'networks': [], 'volumes': []}
a = sys.argv[1:]; status = 0
if a[0] == 'info': print('amd64')
elif a[:2] == ['network', 'create']: s['networks'].append(a[-1])
elif a[:2] == ['network', 'inspect']: status = int(a[-1] not in s['networks'])
elif a[:2] == ['network', 'rm']: s['networks'].remove(a[-1])
elif a[:2] == ['network', 'ls']: print('\n'.join(s['networks']))
elif a[0] == 'run':
    name = a[a.index('--name')+1]; s['containers'].append(name); s['volumes'].append('v-' + name)
elif a[:2] == ['container', 'inspect']: status = int(a[-1] not in s['containers'])
elif a[0] == 'inspect':
    status = int(a[-1] not in s['containers'])
    if not status: print('v-' + a[-1] if '.Mounts' in ' '.join(a) else 'true')
elif a[0] == 'exec':
    if 'SHOW BACKENDS;' in a: print('BackendId\tAlive\n1\ttrue')
    if '/opt/spark/bin/spark-submit' in a:
        if os.environ.get('FAIL_SUBMIT'): status=42
        else: print('SPARK_MYSQL_PUBLICATION_PASS fake\nSPARK_DORIS_PUBLICATION_PASS fake')
elif a[0] == 'rm':
    s['containers'].remove(a[-1])
    if not os.environ.get('LEAK_VOLUME'): s['volumes'].remove('v-' + a[-1])
elif a[:2] == ['volume', 'inspect']: status = int(a[-1] not in s['volumes'])
elif a[0] == 'ps': print('\n'.join(s['containers']))
elif a[0] != 'logs': raise AssertionError(a)
p.write_text(json.dumps(s)); sys.exit(status)
'''
        for flag, expected_success in ((None, True), ('FAIL_SUBMIT', False), ('LEAK_VOLUME', False)):
            with self.subTest(flag=flag), tempfile.TemporaryDirectory() as work:
                directory = Path(work)
                (directory / 'docker').write_text('#!' + sys.executable + '\n' + docker)
                (directory / 'python3').write_text('#!/bin/sh\nexit 0\n')
                (directory / 'docker').chmod(0o755)
                (directory / 'python3').chmod(0o755)
                env = dict(os.environ, PATH=work + os.pathsep + os.environ['PATH'],
                           TMPDIR=work, FAKE_STATE=str(directory / 'state.json'), SPARK_TABLE_TEST_ENGINE='all')
                env.pop('FAIL_SUBMIT', None); env.pop('LEAK_VOLUME', None)
                if flag: env[flag] = '1'
                result = subprocess.run(['bash', str(ROOT / 'scripts/test/common-spark-table-gate.sh')],
                                        env=env, capture_output=True, text=True, timeout=20)
                self.assertEqual(result.returncode == 0, expected_success, result.stderr)
                state = json.loads((directory / 'state.json').read_text())
                self.assertEqual(state['containers'], [])
                self.assertEqual(state['networks'], [])
                if flag != 'LEAK_VOLUME': self.assertEqual(state['volumes'], [])

    def test_gate_owns_services_and_cleanup_without_personal_environment(self):
        path = ROOT / 'scripts/test/common-spark-table-gate.sh'
        subprocess.run(['bash', '-n', str(path)], check=True)
        source = path.read_text()
        for required in ('ADDP_T2_HOSTED_ONLY=doris',
                         'docker rm --force --volumes', 'docker ps -aq',
                         'docker network ls -q', 'SPARK_MYSQL_PUBLICATION_PASS', 'SPARK_DORIS_PUBLICATION_PASS',
                         'ensure_doris_jar', 'trap cleanup EXIT'):
            self.assertIn(required, source)
        self.assertNotIn('source "$ROOT_DIR/.env"', source)
        self.assertNotIn('--publish', source)
        self.assertEqual(source.count('@sha256:'), 3)
        self.assertIn('requires native Linux x86_64 Docker', source)
        self.assertIn('--network-alias host.docker.internal', source)
        self.assertIn('doris_register_backend; doris_health', source)

    def test_physical_oracle_checks_failure_and_commit_evidence(self):
        source = (ROOT / 'scripts/test/spark-table-publication-contract.py').read_text()
        compile(source, 'spark-table-publication-contract.py', 'exec')
        for required in ('SPARK_TABLE_REAL_WORKER', 'injected Worker failure', 'SHOW CREATE TABLE',
                         'injected commit acknowledgement loss', '9007199254740993', 'decimal(38,18)',
                         'frame.limit(0)', 'assert not stages()'):
            self.assertIn(required, source)


if __name__ == '__main__':
    unittest.main()
