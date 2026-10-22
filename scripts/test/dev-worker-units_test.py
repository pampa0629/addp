"""Exercise production launch-unit helpers with owned native process fixtures."""
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import sys
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parents[2]


class WorkerUnitsTest(unittest.TestCase):
    family = 'meta-worker'
    module = 'meta'
    setting = 'ADDP_DEV_META_WORKER_REPLICAS'

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='addp-meta-worker-units-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        for name in ('.dev-bins', '.dev-pids', 'scripts/dev'):
            (self.root / name).mkdir(parents=True)
        shutil.copy2(ROOT / 'scripts/dev/process-metrics-launch.py', self.root / 'scripts/dev/process-metrics-launch.py')
        for name in ('stop-exact-process.sh', 'lifecycle-lock.sh'):
            shutil.copy2(ROOT / 'scripts/dev' / name, self.root / 'scripts/dev' / name)
        source = (ROOT / 'scripts/dev/start.sh').read_text()
        self.helpers = source[source.index('require_started_process() {'):source.index('# 编译函数:')]
        self.helpers += source[source.index('start_module_workers() ('):source.index('BACKEND_STARTS=()')]
        self.environment = dict(os.environ, PROJECT_ROOT=str(self.root), ROOT_DIR=str(self.root),
                                ADDP_PROCESS_METRICS_DEPLOYMENTS='',
                                ADDP_DEV_PROCESS_METRICS_PLAN='', ADDP_HOST_NODE_ID='test-host')
        self.environment[self.setting] = '2'
        self.environment.pop('TRANSFER_CONTINUOUS_WORKER_INSTANCE_ID', None)
        self.children = set()
        self.addCleanup(self.cleanup_children)
        worker = self.root / ('.dev-bins/addp-'+self.family)
        worker.write_text('#!'+sys.executable+'''\nimport json, os, signal, time
from pathlib import Path
Path('started-'+str(os.getpid())+'.json').write_text(json.dumps({
 'host':os.environ.get('ADDP_HOST_NODE_ID'),
 'metrics':json.loads(os.environ.get('ADDP_PROCESS_METRICS_DEPLOYMENTS') or '[]')}))
signal.signal(signal.SIGTERM, lambda *_: exit(0))
while True: time.sleep(.05)
''')
        worker.chmod(0o755)
        launcher = self.root / '.dev-bins/addp-runtime-log'
        launcher.write_text('#!/bin/bash\n[ "$1" = launch ] || exit 11\nshift 6\nexec "$@"\n')
        launcher.chmod(0o755)

    def cleanup_children(self):
        for path in (self.root / '.dev-pids').glob('*.pid'):
            try: self.children.add(int(path.read_text()))
            except ValueError: pass
        owned = set()
        for pid in self.children:
            command = subprocess.run(['ps', '-p', str(pid), '-o', 'command='], capture_output=True, text=True).stdout
            if str(self.root / ('.dev-bins/addp-'+self.family)) not in command.split():
                continue
            owned.add(pid)
            try: os.kill(pid, 15)
            except ProcessLookupError: pass
        deadline = time.monotonic()+3
        while time.monotonic() < deadline:
            alive = [pid for pid in owned if (self.root / ('started-'+str(pid)+'.json')).exists() and self.running(pid)]
            if not alive: break
            time.sleep(.02)
        for pid in owned:
            if self.running(pid):
                try: os.kill(pid, 9)
                except ProcessLookupError: pass

    @staticmethod
    def running(pid):
        result = subprocess.run(['ps', '-p', str(pid), '-o', 'stat='], capture_output=True, text=True)
        return result.returncode == 0 and result.stdout.strip() and not result.stdout.strip().startswith('Z')

    def run_shell(self, body, **extra):
        with (self.root / 'controller.log').open('a') as log:
            result = subprocess.run(['bash', '-c', 'set -e\nsource '+shlex.quote(str(ROOT / 'scripts/dev/worker-units.sh'))+'\n'+body],
                                    cwd=self.root, env=dict(self.environment, WORKER_FAMILY=self.family, WORKER_MODULE=self.module, **extra), stdout=log, stderr=log, timeout=15)
        return result.returncode, (self.root / 'controller.log').read_text()

    def start(self):
        result = self.run_shell(self.helpers+'''
addp_check_worker_inventory "$WORKER_FAMILY"
units=$(addp_worker_units "$WORKER_FAMILY")
META_WORKER_UNITS=()
while IFS= read -r unit; do META_WORKER_UNITS+=("$unit"); done <<< "$units"
curl() { return 0; }
start_module_workers "$WORKER_MODULE" "'''+str(os.getpid())+'''" 12345 "${META_WORKER_UNITS[@]}"
''')
        self.assertEqual(result[0], 0, result[1])
        pids = [int(path.read_text()) for path in sorted((self.root / '.dev-pids').glob('*.pid'))]
        self.children.update(pids)
        deadline = time.monotonic()+5
        while not all((self.root / ('started-'+str(pid)+'.json')).exists() for pid in pids):
            if time.monotonic() >= deadline:
                states = subprocess.run(['ps', '-p', ','.join(map(str, pids)), '-o', 'pid=,ppid=,stat=,command='],
                                        capture_output=True, text=True).stdout
                self.fail('owned worker startup timed out; receipts: '+repr([p.name for p in self.root.glob('started-*')])+'; process states: '+states+'; controller: '+result[1])
            time.sleep(.02)
        return pids

    def test_count_is_bounded_and_units_map_to_one_module_and_binary(self):
        code, _ = self.run_shell('addp_worker_units '+self.family+' > units')
        self.assertEqual(code, 0)
        self.assertEqual((self.root/'units').read_text().splitlines(), [self.family+'-1', self.family+'-2'])
        for value in ('', '0', '3', '01', ' 2', '2\n', 'true'):
            code, _ = self.run_shell('addp_worker_units '+self.family, **{self.setting:value})
            self.assertNotEqual(code, 0, value)
        code, _ = self.run_shell('source '+shlex.quote(str(ROOT/'scripts/dev/ports.sh'))+'''
[ "$(addp_dev_process_module ${WORKER_FAMILY}-1)" = "$WORKER_MODULE" ]
[ "$(addp_dev_process_module ${WORKER_FAMILY}-2)" = "$WORKER_MODULE" ]
[ "$(addp_worker_binary ${WORKER_FAMILY}-1)" = "$WORKER_FAMILY" ]
[ "$(addp_worker_binary ${WORKER_FAMILY}-2)" = "$WORKER_FAMILY" ]
''')
        self.assertEqual(code, 0)

    def test_two_real_launch_units_reuse_and_replace_only_missing_worker(self):
        self.environment['ADDP_DEV_PROCESS_METRICS_PLAN'] = json.dumps({
            self.family+'-'+str(i): {'module_name':self.module,'role':'worker','listen':'127.0.0.1:'+str(19110+i),
                                   'endpoint':'https://127.0.0.1:'+str(19110+i)+'/metrics','tls_dir':'/private/worker-'+str(i)}
            for i in (1, 2)})
        first = self.start()
        self.assertEqual(len(set(first)), 2)
        self.assertEqual(self.start(), first)
        for index, pid in enumerate(first, 1):
            record = json.loads((self.root/('started-'+str(pid)+'.json')).read_text())
            self.assertEqual(record['host'], 'test-host')
            self.assertEqual(record['metrics'][0]['listen'], '127.0.0.1:'+str(19110+index))
        with (self.root / 'stop.log').open('w') as log:
            stopped = subprocess.run(['bash', str(self.root/'scripts/dev/stop-exact-process.sh'), '-'+self.family+'-1'],
                                     cwd=self.root, env=dict(self.environment, ADDP_ONLINE_HOST='1'),
                                     stdout=log, stderr=log, timeout=15)
        self.assertEqual(stopped.returncode, 0, (self.root/'stop.log').read_text())
        self.assertFalse((self.root/('.dev-pids/'+self.family+'-1.pid')).exists())
        self.assertFalse(self.running(first[0]))
        second = self.start()
        self.assertNotEqual(second[0], first[0])
        self.assertEqual(second[1], first[1])
        self.assertTrue(self.running(first[1]))

    def test_live_shrink_and_duplicate_pids_are_refused_without_stopping_peers(self):
        pids = self.start()
        code, _ = self.run_shell('addp_check_worker_inventory '+self.family, **{self.setting:'1'})
        self.assertNotEqual(code, 0)
        (self.root/('.dev-pids/'+self.family+'-2.pid')).write_text(str(pids[0]))
        code, _ = self.run_shell('addp_check_worker_inventory '+self.family)
        self.assertNotEqual(code, 0)
        self.assertTrue(all(self.running(pid) for pid in pids))

    def test_foreign_pid_is_not_reused_or_stopped(self):
        child = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(60)'], cwd=self.root)
        self.addCleanup(child.wait)
        self.addCleanup(child.terminate)
        (self.root/('.dev-pids/'+self.family+'-1.pid')).write_text(str(child.pid))
        code, _ = self.run_shell('addp_check_worker_inventory '+self.family)
        self.assertNotEqual(code, 0)
        self.assertIsNone(child.poll())
        (self.root/('.dev-pids/'+self.family+'-1.pid')).unlink()


    def test_default_is_one_and_legacy_live_unit_is_rejected(self):
        code, _ = self.run_shell('unset '+self.setting+'; addp_worker_units '+self.family+' > units')
        self.assertEqual(code, 0)
        self.assertEqual((self.root/'units').read_text().splitlines(), [self.family+'-1'])
        pids = self.start()
        (self.root/('.dev-pids/'+self.family+'.pid')).write_text(str(pids[0]))
        code, _ = self.run_shell('addp_check_worker_inventory '+self.family)
        self.assertNotEqual(code, 0)
        self.assertTrue(all(self.running(pid) for pid in pids))

    def test_restart_rejects_invalid_selection_before_stop(self):
        # Execute production restart preflight with no lifecycle side effects.
        source = (ROOT/'scripts/dev/restart.sh').read_text()
        preflight = source[source.index('source "${SCRIPT_DIR}/worker-units.sh"'):source.index('STOP_ARGS=()')]
        body = 'SCRIPT_DIR='+shlex.quote(str(ROOT/'scripts/dev'))+'\n'+preflight+'\ntouch stopped'
        code, _ = self.run_shell(body, **{self.setting:'3'})
        self.assertNotEqual(code, 0)
        self.assertFalse((self.root/'stopped').exists())



class BoundedWorkerUnitsTest(WorkerUnitsTest):
    family = 'transfer-bounded-worker'
    module = 'transfer'
    setting = 'ADDP_DEV_TRANSFER_BOUNDED_WORKER_REPLICAS'


class ContinuousWorkerUnitsTest(WorkerUnitsTest):
    family = 'transfer-continuous-worker'
    module = 'transfer'
    setting = 'ADDP_DEV_TRANSFER_CONTINUOUS_WORKER_REPLICAS'

    def test_fixed_owner_cannot_be_shared_by_two_workers(self):
        code, _ = self.run_shell('addp_worker_units '+self.family,
                                 TRANSFER_CONTINUOUS_WORKER_INSTANCE_ID='fixed-owner')
        self.assertNotEqual(code, 0)
        code, _ = self.run_shell('addp_worker_units '+self.family,
                                 TRANSFER_CONTINUOUS_WORKER_INSTANCE_ID='fixed-owner', **{self.setting:'1'})
        self.assertEqual(code, 0)


if __name__ == '__main__':
    unittest.main()
