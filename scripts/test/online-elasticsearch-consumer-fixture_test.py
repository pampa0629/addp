import json
import os
from pathlib import Path
import shutil
import stat
import sys
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[2] / 'business/scripts/online-elasticsearch-consumer-fixture.sh'


class ElasticsearchOnlineFixtureTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='addp-elasticsearch-online-fixture-')
        self.root = Path(self.temporary.name)
        (self.root / 'business/scripts').mkdir(parents=True)
        shutil.copy2(SCRIPT, self.root / 'business/scripts' / SCRIPT.name)
        self.bin = self.root / 'bin'; self.bin.mkdir()
        self.secret = self.root / 'addp-online-secret-test'; self.secret.mkdir(mode=0o700)
        self.state = self.root / 'containers'; self.state.mkdir(); self.trace = self.root / 'trace'
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
    print(json.dumps({'services': {'elasticsearch': {'image': os.environ['FAKE_IMAGE']}, 'spark-master': {'image': os.environ['SPARK_IMAGE']}}}))
elif a[0] == 'run':
    name = a[a.index('--name') + 1]
    (root / name).write_text('elasticsearch-consumer-flow')
    if os.environ.get('FAIL_RUN') == name: sys.exit(1)
elif a[0] == 'port': print(os.environ.get('FAKE_PORT', '127.0.0.1:34567'))
elif a[0] == 'rm':
    if os.environ.get('FAIL_REMOVE') == '1': sys.exit(1)
    (root / a[-1]).unlink(missing_ok=True)
elif a[0] == 'ps':
    if os.environ.get('FAIL_VERIFY') == '1': sys.exit(1)
    for state in root.iterdir(): print(state.name)
else: sys.exit(2)
''')
        self.executable('python3', f'''#!{sys.executable}
import io, json, os, subprocess, sys
from unittest.mock import patch, MagicMock
if any(argument.endswith('/business/elasticsearch/init.py') for argument in sys.argv):
    with open(os.environ['FAKE_TRACE'], 'a') as stream: stream.write('init.py\\n')
    raise SystemExit(1 if os.environ.get('FAIL_SEED') == '1' else 0)
if sys.argv[1:] != ['-']:
    raise SystemExit(subprocess.call([{sys.executable!r}] + sys.argv[1:]))
master = {{'aliveworkers': 1, 'activeapps': [{{'name': 'Thrift JDBC/ODBC Server', 'state': 'RUNNING'}}]}}
code = sys.stdin.read()
with patch('socket.socket', MagicMock()), patch('urllib.request.urlopen', return_value=io.BytesIO(json.dumps(master).encode())):
    exec(compile(code, '<fixture-inline>', 'exec'), {{'__name__': '__main__'}})
''')
        image = next(line.strip().split('image: ', 1)[1] for line in (SCRIPT.parents[1] / 'docker-compose.yml').read_text().splitlines()
                     if 'image: docker.elastic.co/elasticsearch/elasticsearch:' in line)
        self.env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ['PATH'], GITHUB_ACTIONS='true', RUNNER_OS='Linux',
                        ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1', ADDP_ONLINE_SECRET_DIR=str(self.secret),
                        ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE=str(self.secret / 'elasticsearch-engine.json'),
                        FAKE_STATE=str(self.state), FAKE_TRACE=str(self.trace), FAKE_IMAGE=image,
                        SPARK_IMAGE=next(line.strip().split('image: ', 1)[1] for line in (SCRIPT.parents[1] / 'docker-compose.yml').read_text().splitlines() if 'image: apache/spark:' in line))

    def executable(self, name, content):
        path = self.bin / name; path.write_text(content); path.chmod(0o755)

    def tearDown(self): self.temporary.cleanup()

    def run_fixture(self, action, **flags):
        return subprocess.run(['bash', 'business/scripts/' + SCRIPT.name, action], cwd=self.root,
                              env=dict(self.env, **flags), capture_output=True, text=True, timeout=30)

    def test_start_uses_owned_tmpfs_official_image_business_samples_and_reader_only_descriptor(self):
        result = self.run_fixture('start'); self.assertEqual(result.returncode, 0, result.stderr)
        descriptor = self.secret / 'elasticsearch-engine.json'
        value = json.loads(descriptor.read_text())
        self.assertEqual(stat.S_IMODE(descriptor.stat().st_mode), 0o600)
        connection = value['connection_info']
        self.assertEqual((value['engine_type'], connection['user']), ('elasticsearch', 'addp_business_reader'))
        self.assertEqual(connection['endpoint'], 'http://127.0.0.1:34567')
        commands = self.trace.read_text()
        self.assertNotIn(connection['password'], commands + result.stdout + result.stderr)
        self.assertIn('/usr/share/elasticsearch/data:mode=1777', commands)
        self.assertIn('init.py', commands)
        self.assertNotIn('addp-elasticsearch-t2', commands)
        spark = self.secret / 'spark-engine.json'
        self.assertEqual(stat.S_IMODE(spark.stat().st_mode), 0o600)
        self.assertEqual(json.loads(spark.read_text())['engine_type'], 'spark')
        self.assertEqual(len(list(self.state.iterdir())), 3)
        self.assertNotIn('local[*]', commands)
        self.assertEqual(self.run_fixture('stop').returncode, 0)
        self.assertEqual(list(self.state.iterdir()), [])

    def test_personal_environment_is_rejected_before_docker(self):
        for flags in ({'GITHUB_ACTIONS': 'false'}, {'RUNNER_OS': 'macOS'}, {'FAKE_OS': 'Darwin'},
                      {'ADDP_ONLINE_HOSTED': '0'}, {'ADDP_ONLINE_OWNER_MANAGED': '1'}):
            self.assertNotEqual(self.run_fixture('start', **flags).returncode, 0)
            self.assertFalse(self.trace.exists())

    def test_existing_or_foreign_container_is_preserved(self):
        foreign = self.state / 'addp-elasticsearch-online-master'
        foreign.write_text('foreign')
        for action in ('start', 'stop'):
            self.assertNotEqual(self.run_fixture(action).returncode, 0)
            self.assertEqual(foreign.read_text(), 'foreign')

    def test_failure_after_creation_cleans_container_and_rejects_non_loopback_mapping(self):
        for flags in ({'FAIL_RUN': 'addp-elasticsearch-online-disposable'}, {'FAIL_RUN': 'addp-elasticsearch-online-master'}, {'FAIL_RUN': 'addp-elasticsearch-online-worker'}, {'FAIL_SEED': '1'}, {'FAKE_PORT': '0.0.0.0:34567'}):
            with self.subTest(flags=flags):
                result = self.run_fixture('start', **flags)
                self.assertNotEqual(result.returncode, 0, result.stderr)
                self.assertEqual(list(self.state.iterdir()), [])
                self.assertFalse((self.secret / 'elasticsearch-engine.json').exists())

    def test_cleanup_failures_are_not_reported_as_success(self):
        for flag in ('FAIL_REMOVE', 'FAIL_VERIFY'):
            owned = self.state / 'addp-elasticsearch-online-worker'
            owned.write_text('elasticsearch-consumer-flow')
            self.assertNotEqual(self.run_fixture('stop', **{flag: '1'}).returncode, 0)
            owned.unlink(missing_ok=True)

    def test_descriptor_cannot_overwrite_or_escape_secret_partition(self):
        for target in (self.root / 'artifact.json', self.secret / 'existing.json'):
            if target.parent == self.secret: target.write_text('preserve')
            result = self.run_fixture('start', ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE=str(target))
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(list(self.state.iterdir()), [])
            if target.parent == self.secret: self.assertEqual(target.read_text(), 'preserve')

    def test_spark_descriptor_and_unpinned_image_rejected_before_creation(self):
        self.assertNotEqual(self.run_fixture('start', SPARK_IMAGE='apache/spark:latest').returncode, 0)
        self.assertEqual(list(self.state.iterdir()), [])
        descriptor = self.secret / 'spark-engine.json'
        descriptor.write_text('preserve')
        self.assertNotEqual(self.run_fixture('start').returncode, 0)
        self.assertEqual(descriptor.read_text(), 'preserve')
        self.assertEqual(list(self.state.iterdir()), [])


if __name__ == '__main__': unittest.main()
