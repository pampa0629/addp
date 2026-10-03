import json
import os
from pathlib import Path
import shutil
import stat
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
        self.state = self.root / 'container'; self.trace = self.root / 'trace'
        self.executable('uname', '#!/bin/bash\n[ "$1" != -s ] || { echo "${FAKE_OS:-Linux}"; exit; }\necho x86_64\n')
        self.executable('docker', '''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
a = sys.argv[1:]; state = Path(os.environ['FAKE_STATE'])
with open(os.environ['FAKE_TRACE'], 'a') as f: f.write(json.dumps(a) + '\\n')
if a[0] == 'container':
    if not state.exists(): sys.exit(1)
    if '--format' in a: print(state.read_text())
elif a[0] == 'compose':
    print(json.dumps({'services': {'elasticsearch': {'image': os.environ['FAKE_IMAGE']}}}))
elif a[0] == 'run':
    state.write_text('elasticsearch-consumer-flow')
    if os.environ.get('FAIL_RUN') == '1': sys.exit(1)
elif a[0] == 'port': print(os.environ.get('FAKE_PORT', '127.0.0.1:34567'))
elif a[0] == 'rm':
    if os.environ.get('FAIL_REMOVE') == '1': sys.exit(1)
    state.unlink(missing_ok=True)
elif a[0] == 'ps':
    if os.environ.get('FAIL_VERIFY') == '1': sys.exit(1)
    if state.exists(): print('container')
else: sys.exit(2)
''')
        self.executable('python3', f'''#!/bin/bash
if [[ "$1" == */business/elasticsearch/init.py ]]; then
  echo init.py >> "$FAKE_TRACE"
  [ "${{FAIL_SEED:-0}}" != 1 ] || exit 1
  exit 0
fi
exec {__import__('sys').executable} "$@"
''')
        image = next(line.strip().split('image: ', 1)[1] for line in (SCRIPT.parents[1] / 'docker-compose.yml').read_text().splitlines()
                     if 'image: docker.elastic.co/elasticsearch/elasticsearch:' in line)
        self.env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ['PATH'], GITHUB_ACTIONS='true', RUNNER_OS='Linux',
                        ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1', ADDP_ONLINE_SECRET_DIR=str(self.secret),
                        ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE=str(self.secret / 'elasticsearch-engine.json'),
                        FAKE_STATE=str(self.state), FAKE_TRACE=str(self.trace), FAKE_IMAGE=image)

    def executable(self, name, content):
        path = self.bin / name; path.write_text(content); path.chmod(0o755)

    def tearDown(self): self.temporary.cleanup()

    def run_fixture(self, action, **flags):
        return subprocess.run(['bash', 'business/scripts/' + SCRIPT.name, action], cwd=self.root,
                              env=dict(self.env, **flags), capture_output=True, text=True, timeout=10)

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
        self.assertEqual(self.run_fixture('stop').returncode, 0)
        self.assertFalse(self.state.exists())

    def test_personal_environment_is_rejected_before_docker(self):
        for flags in ({'GITHUB_ACTIONS': 'false'}, {'RUNNER_OS': 'macOS'}, {'FAKE_OS': 'Darwin'},
                      {'ADDP_ONLINE_HOSTED': '0'}, {'ADDP_ONLINE_OWNER_MANAGED': '1'}):
            self.assertNotEqual(self.run_fixture('start', **flags).returncode, 0)
            self.assertFalse(self.trace.exists())

    def test_existing_or_foreign_container_is_preserved(self):
        self.state.write_text('foreign')
        for action in ('start', 'stop'):
            self.assertNotEqual(self.run_fixture(action).returncode, 0)
            self.assertEqual(self.state.read_text(), 'foreign')

    def test_failure_after_creation_cleans_container_and_rejects_non_loopback_mapping(self):
        for flags in ({'FAIL_RUN': '1'}, {'FAIL_SEED': '1'}, {'FAKE_PORT': '0.0.0.0:34567'}):
            with self.subTest(flags=flags):
                result = self.run_fixture('start', **flags)
                self.assertNotEqual(result.returncode, 0, result.stderr)
                self.assertFalse(self.state.exists())
                self.assertFalse((self.secret / 'elasticsearch-engine.json').exists())

    def test_cleanup_failures_are_not_reported_as_success(self):
        for flag in ('FAIL_REMOVE', 'FAIL_VERIFY'):
            self.state.write_text('elasticsearch-consumer-flow')
            self.assertNotEqual(self.run_fixture('stop', **{flag: '1'}).returncode, 0)
            self.state.unlink(missing_ok=True)

    def test_descriptor_cannot_overwrite_or_escape_secret_partition(self):
        for target in (self.root / 'artifact.json', self.secret / 'existing.json'):
            if target.parent == self.secret: target.write_text('preserve')
            result = self.run_fixture('start', ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE=str(target))
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(self.state.exists())
            if target.parent == self.secret: self.assertEqual(target.read_text(), 'preserve')


if __name__ == '__main__': unittest.main()
