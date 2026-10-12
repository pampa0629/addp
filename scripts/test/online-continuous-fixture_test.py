import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[2] / 'business/scripts/online-continuous-fixture.sh'


class ContinuousPhysicalFixtureTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='addp-continuous-fixture-')
        self.root = Path(self.temporary.name).resolve()
        self.secret = self.root / 'addp-online-secret-test'
        self.secret.mkdir(mode=0o700)
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        self.trace = self.root / 'trace.jsonl'
        self.executable('uname', '#!/bin/sh\n[ "$1" = -s ] && echo Linux || echo x86_64\n')
        self.executable('stat', '#!/usr/bin/env python3\nimport os,sys\nprint(oct(os.stat(sys.argv[-1]).st_mode & 0o777)[2:])\n')
        self.executable('docker', '''#!/usr/bin/env python3
import json,os,sys
from pathlib import Path
args=sys.argv[1:]
root=Path(os.environ['FAKE_ROOT'])
with Path(os.environ['FAKE_TRACE']).open('a') as output: output.write(json.dumps(args)+'\\n')
if args[:2]==['container','inspect']:
    raise SystemExit(0 if (root / args[2]).exists() else 1)
if args[0]=='inspect':
    if 'IPAddress' in args[2]: print('172.22.0.8')
    elif 'com.addp.online-run' in args[2]: print('other-run' if os.environ.get('BAD_OWNER') else 'addp-online-secret-test')
    else: print('continuous')
elif args[0]=='run':
    (root / args[args.index('--name')+1]).touch()
elif args[0]=='rm':
    (root / args[-1]).unlink()
elif args[0]=='exec' and 'psql' in args:
    if '-c' in args:
        sql=args[args.index('-c')+1]
        if 'pg_replication_slots' in sql: print(os.environ.get('RESIDUAL_CAPTURE','0'))
        elif 'SELECT id,label' in sql: print(os.environ.get('PHYSICAL_ROWS',''))
    else: sys.stdin.read()
elif args[0]=='exec' and 'produce' in args:
    sys.stdin.read()
''')
        self.environment = {**os.environ, 'PATH': f'{self.bin}:{os.environ["PATH"]}', 'GITHUB_ACTIONS': 'true',
                            'RUNNER_OS': 'Linux', 'RUNNER_TEMP': str(self.root), 'ADDP_ONLINE_HOSTED': '1',
                            'ADDP_ONLINE_HOST': '1', 'POSTGRES_DB': 'addp_online',
                            'ADDP_ONLINE_SECRET_DIR': str(self.secret), 'REDPANDA_IMAGE': 'redpanda-pinned',
                            'FAKE_ROOT': str(self.root), 'FAKE_TRACE': str(self.trace)}

    def executable(self, name, text):
        path = self.bin / name
        path.write_text(text.replace('#!/usr/bin/env python3', '#!' + sys.executable, 1))
        path.chmod(0o755)

    def tearDown(self):
        self.temporary.cleanup()

    def run_fixture(self, action, **env):
        return subprocess.run(['bash', str(SCRIPT), action], env={**self.environment, **env},
                              capture_output=True, text=True, timeout=45)

    def test_separate_source_descriptors_have_private_credentials_and_real_host_address(self):
        result = self.run_fixture('start')
        self.assertEqual(result.returncode, 0, result.stderr)
        pg = json.loads((self.secret / 'continuous-pg.json').read_text())
        kafka = json.loads((self.secret / 'continuous-kafka.json').read_text())
        self.assertEqual(pg['connection_info']['host'], '172.22.0.8')
        self.assertEqual(kafka['connection_info'], {'bootstrap_servers': '127.0.0.1:29094'})
        self.assertNotIn(pg['connection_info']['password'], result.stdout + result.stderr)
        self.assertEqual((self.secret / 'continuous-pg.json').stat().st_mode & 0o777, 0o600)
        trace = self.trace.read_text()
        self.assertIn('wal_level=logical', trace)
        self.assertIn('com.addp.online-run=addp-online-secret-test', trace)
        self.assertNotIn('createdb', trace)

    def test_physical_rows_must_match_exactly_and_are_not_printed(self):
        rows = '1|business-value-sentinel-updated\n3|business-value-sentinel-inserted'
        result = self.run_fixture('verify-cdc-mutated', PHYSICAL_ROWS=rows)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, '')
        self.assertNotIn('business-value-sentinel', result.stderr)
        self.assertNotEqual(self.run_fixture('verify-cdc-mutated', PHYSICAL_ROWS=rows + '\n4|unexpected').returncode, 0)
        self.assertNotEqual(self.run_fixture('verify-cdc-mutated', PHYSICAL_ROWS='').returncode, 0)

    def test_nonzero_capture_residual_fails(self):
        self.assertEqual(self.run_fixture('verify-clean').returncode, 0)
        self.assertNotEqual(self.run_fixture('verify-clean', RESIDUAL_CAPTURE='1').returncode, 0)

    def test_stop_refuses_foreign_owned_containers(self):
        (self.root / 'addp-continuous-pg-online-disposable').touch()
        result = self.run_fixture('stop', BAD_OWNER='1')
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue((self.root / 'addp-continuous-pg-online-disposable').exists())
        self.assertNotIn('"rm"', self.trace.read_text())

    def test_owned_stop_removes_both_containers(self):
        for kind in ('pg', 'kafka'):
            (self.root / f'addp-continuous-{kind}-online-disposable').touch()
        result = self.run_fixture('stop')
        self.assertEqual(result.returncode, 0, result.stderr)
        for kind in ('pg', 'kafka'):
            self.assertFalse((self.root / f'addp-continuous-{kind}-online-disposable').exists())

    def test_local_database_profile_and_nonprivate_secret_refused_before_docker(self):
        for env in [{'GITHUB_ACTIONS': 'false'}, {'POSTGRES_DB': 'addp'}, {'RUNNER_TEMP': str(self.bin)}]:
            with self.subTest(env=env):
                self.assertNotEqual(self.run_fixture('start', **env).returncode, 0)
                self.assertFalse(self.trace.exists())
        self.secret.chmod(0o755)
        self.assertNotEqual(self.run_fixture('start').returncode, 0)
        self.assertFalse(self.trace.exists())


if __name__ == '__main__':
    unittest.main()
