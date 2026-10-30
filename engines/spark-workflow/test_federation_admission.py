"""Disposable admission lifecycle regression; product query capability is not enabled."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class FederationAdmissionTest(unittest.TestCase):
    def test_cleanup_runs_after_success_startup_failure_and_driver_failure(self):
        fake = r'''
import json, os, sys
from pathlib import Path
path = Path(os.environ['STATE'])
state = json.loads(path.read_text()) if path.exists() else {'active': False, 'down': False, 'phases': []}
args = sys.argv[1:]
status = 0
if args[0] == 'compose':
    args = args[5:]
    if args[0] == 'up':
        state['active'] = True
        status = 17 if os.environ.get('FAIL_AT') == 'up' else 0
    elif args[0] == 'down':
        assert '--volumes' in args and '--remove-orphans' in args
        state['active'] = False
        state['down'] = True
    elif args[0] == 'exec':
        if '/opt/spark/bin/spark-submit' in args:
            state['phases'].append(args[-1])
            status = 19 if os.environ.get('FAIL_AT') == 'driver' else 0
        else:
            sys.stdin.read()
            if 'SELECT HEX(label) FROM customers WHERE id=1' in args:
                print('00' if os.environ.get('FAIL_AT') == 'seed' else 'E4B8ADE696872020')
    else: raise AssertionError(args)
elif args[0] in ('ps', 'volume', 'network'):
    if state['active'] or os.environ.get('FAIL_AT') == 'leak': print('owned-residual')
else: raise AssertionError(args)
path.write_text(json.dumps(state))
sys.exit(status)
'''
        gate = ROOT / 'scripts/test/common-spark-federation-gate.sh'
        subprocess.run(['bash', '-n', str(gate)], check=True)
        for failure in ('', 'up', 'driver', 'leak', 'seed'):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as directory:
                path = Path(directory)
                docker = path / 'docker'
                docker.write_text('#!' + sys.executable + '\n' + fake)
                docker.chmod(0o700)
                env = dict(os.environ, PATH=directory + os.pathsep + os.environ['PATH'],
                           STATE=str(path / 'state.json'), FAIL_AT=failure)
                result = subprocess.run(['bash', str(gate)], env=env, capture_output=True,
                                        text=True, timeout=20)
                state = json.loads((path / 'state.json').read_text())
                self.assertTrue(state['down'], result.stderr)
                self.assertFalse(state['active'])
                self.assertEqual(result.returncode == 0, not failure, result.stderr)
                if not failure:
                    self.assertEqual(state['phases'], ['query', 'fresh', 'supervised'])
                    self.assertIn('zero_residuals', result.stdout)


if __name__ == '__main__':
    unittest.main()
