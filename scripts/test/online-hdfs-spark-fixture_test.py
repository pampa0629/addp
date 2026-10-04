import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[2] / 'business/scripts/online-hdfs-spark-fixture.sh'


class HDFSOnlineFixtureTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='addp-hdfs-fixture-')
        self.root = Path(self.temporary.name)
        (self.root / 'business/scripts').mkdir(parents=True)
        shutil.copy2(SCRIPT, self.root / 'business/scripts' / SCRIPT.name)
        self.bin = self.root / 'bin'; self.bin.mkdir()
        self.secret = self.root / 'secret'; self.secret.mkdir(mode=0o700)
        self.state = self.root / 'state'; self.state.mkdir()
        self.trace = self.root / 'trace'
        self.executable('uname', '#!/bin/bash\n[ "$1" != -s ] || { echo "${FAKE_OS:-Linux}"; exit; }\necho x86_64\n')
        self.executable('docker', '''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
a = sys.argv[1:]; root = Path(os.environ['FAKE_STATE'])
with open(os.environ['FAKE_TRACE'], 'a') as f: f.write(json.dumps(a) + '\\n')
if a[0] == 'container':
    state = root / a[-1]
    if not state.exists(): sys.exit(1)
    if '--format' in a: print(state.read_text())
elif a[0] == 'compose':
    print(json.dumps({'services': {'hdfs-namenode': {'image': os.environ['HADOOP_IMAGE']}, 'spark-master': {'image': os.environ['SPARK_IMAGE']}}}))
elif a[0] == 'run':
    container = a[a.index('--name') + 1]; (root / container).write_text('hdfs-spark-consumer-flow')
    if os.environ.get('FAIL_SIGNAL') == container:
        import signal
        os.kill(os.getppid(), signal.SIGTERM)
    if os.environ.get('FAIL_RUN') == container: sys.exit(1)
elif a[0] == 'exec':
    if os.environ.get('FAIL_SEED') == '1': sys.exit(1)
    print('HDFS_SAMPLE_PASS rows=20 amount_sum=2100 formats=csv,json,parquet')
elif a[0] == 'rm':
    if os.environ.get('FAIL_REMOVE') == '1': sys.exit(1)
    (root / a[-1]).unlink(missing_ok=True)
elif a[0] == 'ps':
    if os.environ.get('FAIL_VERIFY') == '1': sys.exit(1)
    for state in root.iterdir(): print(state.name)
else: sys.exit(2)
''')
        # Inline readiness and bind probes are isolated; physical Docker invocations remain observable.
        self.executable('python3', f'''#!{sys.executable}
import io, json, os, subprocess, sys
from unittest.mock import patch, MagicMock
if sys.argv[1:] != ['-']:
    raise SystemExit(subprocess.call([{sys.executable!r}] + sys.argv[1:]))
code = sys.stdin.read()
master = {{'aliveworkers': 1, 'activeapps': [{{'name': 'Thrift JDBC/ODBC Server', 'state': 'RUNNING'}}]}}
with patch('socket.socket', MagicMock()), patch('urllib.request.urlopen', return_value=io.BytesIO(json.dumps(master).encode())):
    exec(compile(code, '<fixture-inline>', 'exec'), {{'__name__': '__main__'}})
''')
        compose = (SCRIPT.parents[1] / 'docker-compose.yml').read_text()
        images = [line.strip().split('image: ', 1)[1] for line in compose.splitlines() if 'image:' in line]
        self.env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ['PATH'], GITHUB_ACTIONS='true', RUNNER_OS='Linux',
                        ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1', ADDP_ONLINE_SECRET_DIR=str(self.secret),
                        FAKE_STATE=str(self.state), FAKE_TRACE=str(self.trace),
                        HADOOP_IMAGE=next(value for value in images if value.startswith('ghcr.io/apache/hadoop:')),
                        SPARK_IMAGE=next(value for value in images if value.startswith('apache/spark:')))

    def executable(self, name, content):
        path = self.bin / name; path.write_text(content); path.chmod(0o755)

    def tearDown(self): self.temporary.cleanup()

    def run_fixture(self, action, **flags):
        return subprocess.run(['bash', 'business/scripts/' + SCRIPT.name, action], cwd=self.root,
                              env=dict(self.env, **flags), capture_output=True, text=True, timeout=15)

    def test_shared_images_samples_host_network_and_distinct_owner_only_descriptors(self):
        result = self.run_fixture('start')
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        for engine in ('hdfs', 'spark'):
            descriptor = self.secret / (engine + '-engine.json')
            self.assertEqual(stat.S_IMODE(descriptor.stat().st_mode), 0o600)
            self.assertEqual(json.loads(descriptor.read_text())['engine_type'], engine)
        hdfs = json.loads((self.secret / 'hdfs-engine.json').read_text())['connection_info']
        self.assertEqual((hdfs['authentication'], hdfs['user'], hdfs['root_path']), ('simple', 'addp_business_reader', '/addp'))
        self.assertEqual(len(list(self.state.iterdir())), 4)
        commands = [json.loads(line) for line in self.trace.read_text().splitlines()]
        for command in commands:
            if command[0] == 'run': self.assertIn('host', command)
        self.assertTrue(any('/addp/hdfs/init.py' in command for command in commands))
        self.assertNotIn('local[*]', self.trace.read_text())
        self.assertEqual(self.run_fixture('stop').returncode, 0)
        self.assertEqual(list(self.state.iterdir()), [])

    def test_personal_profile_rejected_before_docker(self):
        for flags in ({'GITHUB_ACTIONS': 'false'}, {'ADDP_ONLINE_HOSTED': '0'}, {'ADDP_ONLINE_OWNER_MANAGED': '1'}, {'FAKE_OS': 'Darwin'}):
            self.assertNotEqual(self.run_fixture('start', **flags).returncode, 0)
            self.assertFalse(self.trace.exists())

    def test_existing_or_foreign_container_is_preserved(self):
        foreign = self.state / 'addp-hdfs-online-master'; foreign.write_text('foreign')
        for action in ('start', 'stop'):
            self.assertNotEqual(self.run_fixture(action).returncode, 0)
            self.assertEqual(foreign.read_text(), 'foreign')

    def test_partial_creation_or_seed_failure_cleans_all_owned_containers(self):
        for flags in ({'FAIL_RUN': 'addp-hdfs-online-namenode'}, {'FAIL_RUN': 'addp-hdfs-online-worker'},
                      {'FAIL_SEED': '1'}, {'FAIL_SIGNAL': 'addp-hdfs-online-worker'}):
            with self.subTest(flags=flags):
                result = self.run_fixture('start', **flags)
                self.assertNotEqual(result.returncode, 0, result.stderr)
                self.assertEqual(list(self.state.iterdir()), [])
                self.assertEqual(list(self.secret.iterdir()), [])

    def test_unpinned_image_and_existing_descriptor_rejected_without_creation(self):
        result = self.run_fixture('start', SPARK_IMAGE='apache/spark:latest')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(list(self.state.iterdir()), [])
        (self.secret / 'hdfs-engine.json').write_text('preserve')
        self.assertNotEqual(self.run_fixture('start').returncode, 0)
        self.assertEqual((self.secret / 'hdfs-engine.json').read_text(), 'preserve')

    def test_cleanup_failure_never_passes(self):
        for flag in ('FAIL_REMOVE', 'FAIL_VERIFY'):
            (self.state / 'addp-hdfs-online-worker').write_text('hdfs-spark-consumer-flow')
            self.assertNotEqual(self.run_fixture('stop', **{flag: '1'}).returncode, 0)
            for state in self.state.iterdir(): state.unlink()


if __name__ == '__main__': unittest.main()
