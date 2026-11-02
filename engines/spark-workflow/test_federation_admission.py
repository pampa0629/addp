"""Disposable admission build, process and cleanup lifecycle regression."""
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
print('fixture docker command:', args, file=sys.stderr, flush=True)
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
        elif '/addp/contract.py' in args:
            state['phases'].append(args[-1])
            status = 24 if os.environ.get('FAIL_AT') == 'multicluster' else 0
        elif 'sys.prefix' in ' '.join(args):
            print('/tmp/owned-test-artifacts')
        elif 'uname' in args:
            print('aarch64')
        elif '/tmp/addp-spark-probe.test' in args:
            status = 21 if os.environ.get('FAIL_AT') == 'probe' else 0
        else:
            sys.stdin.read()
            if 'SELECT HEX(label) FROM customers WHERE id=1' in args:
                print('00' if os.environ.get('FAIL_AT') == 'seed' else 'E4B8ADE696872020')
    elif args[0] == 'cp':
        if args[1].startswith('spark-master:'):
            Path(args[2]).mkdir(parents=True)
        else: assert Path(args[1]).is_file()
    else: raise AssertionError(args)
elif args[0] in ('ps', 'volume', 'network'):
    if state['active'] or os.environ.get('FAIL_AT') == 'leak': print('owned-residual')
else: raise AssertionError(args)
path.write_text(json.dumps(state))
sys.exit(status)
'''
        gate = ROOT / 'scripts/test/common-spark-federation-gate.sh'
        subprocess.run(['bash', '-n', str(gate)], check=True)
        for failure in ('', 'up', 'compile', 'dialect-build', 'probe', 'driver', 'leak', 'seed', 'multicluster'):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as directory:
                path = Path(directory)
                docker = path / 'docker'
                docker.write_text('#!' + sys.executable + '\n' + fake)
                docker.chmod(0o700)
                compiler = path / 'go'
                compiler.write_text('#!' + sys.executable + '\nimport os,sys\nfrom pathlib import Path\nif os.environ.get("FAIL_AT")=="compile":sys.exit(18)\nPath(sys.argv[sys.argv.index("-o")+1]).write_bytes(b"fixture-test-only")\n')
                compiler.chmod(0o700)
                builder = path / 'python3'
                builder.write_text('#!' + sys.executable + '\nimport os,sys\nfrom pathlib import Path\nif os.environ.get("FAIL_AT")=="dialect-build":sys.exit(23)\np=Path(sys.argv[sys.argv.index("--output")+1]);p.write_bytes(b"owned-test-jar");p.with_suffix(".json").write_text("{}")\n')
                builder.chmod(0o700)
                sleeper = path / 'sleep'
                sleeper.write_text('#!/bin/sh\nexit 0\n')
                sleeper.chmod(0o700)
                env = dict(os.environ, PATH=directory + os.pathsep + os.environ['PATH'],
                           STATE=str(path / 'state.json'), FAIL_AT=failure)
                try:
                    result = subprocess.run(['bash', str(gate)], env=env, stdin=subprocess.DEVNULL, capture_output=True,
                                            text=True, timeout=20)
                except subprocess.TimeoutExpired as error:
                    self.fail('disposable lifecycle fixture stalled: ' + str(error.stderr)[-3000:])
                state = json.loads((path / 'state.json').read_text())
                self.assertTrue(state['down'], result.stderr)
                self.assertFalse(state['active'])
                self.assertEqual(result.returncode == 0, not failure, result.stderr)
                if not failure:
                    self.assertEqual(state['phases'], ['query', 'guard', 'fresh', 'supervised', 'driver', 'multicluster'])
                    self.assertIn('zero_residuals', result.stdout)


if __name__ == '__main__':
    unittest.main()
