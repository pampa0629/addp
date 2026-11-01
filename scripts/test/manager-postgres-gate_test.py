#!/usr/bin/env python3
"""Exercise the canonical Manager gate with fake processes, never Docker/DB."""
from __future__ import annotations

import functools
import http.server
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import threading
import unittest


ROOT = Path(__file__).resolve().parents[2]
REVISION = "2627c5ff775ae6d7aef0c430121ccf857842d2f2"
IMAGE_ID = "sha256:" + "a" * 64
SOURCE = "b" * 40
DSN = "postgres://addp_ci:addp_ci_password@127.0.0.1:5432/addp_manager_disposable?sslmode=disable"

# Stateful fake CLI: record commands and make successful teardown observable.
DOCKER = r'''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
a=sys.argv[1:]; p=Path(os.environ['FAKE_STATE'])
s=json.loads(p.read_text()) if p.exists() else {'image':False,'container':False,'network':False,'calls':[]}
s['calls'].append(a); mode=os.environ.get('FAKE_MODE','')
def finish(out='', code=0):
    p.write_text(json.dumps(s)); print(out) if out else None; sys.exit(code)
if a[0]=='compose':
    assert a[1:3]==['--env-file','/dev/null'] and a[3]=='-p' and a[5]=='-f'
    op=a[7]; s['owner']=os.environ['MANAGER_PG_TEST_OWNER']
    s['tag']=os.environ['MANAGER_PG_TEST_IMAGE']; s['project']=a[4]
    if op=='build':
        s['image']=True; finish(code=1 if mode=='build-fail' else 0)
    if op=='up':
        s.update(container=True,network=True); finish(code=1 if mode=='up-fail' else 0)
    if op=='ps': finish('c'*64)
    if op=='exec':
        s['probe_sql']=sys.stdin.read()
        if mode=='probe-fail': finish(code=1)
        row=dict(postgres_version='PostgreSQL 15 fixture',server_version_num='150010',postgis_version='3.5.2',vector_version='0.8.0',vector_probe='[1,2]')
        if mode=='wrong-vector': row['vector_version']='0.7.0'
        if mode=='wrong-postgres': row['server_version_num']='160000'
        finish(json.dumps(row)+'\n'+('1' if mode=='probe-residue' else '0'))
    if op=='down':
        if mode=='cleanup-fail': finish(code=1)
        s.update(container=False,network=False); finish()
if a[:2]==['image','inspect']:
    if not s['image']: finish(code=1)
    if '-f' not in a: finish('{}')
    f=a[a.index('-f')+1]
    if 'source_revision' in f: finish('2627c5ff775ae6d7aef0c430121ccf857842d2f2')
    finish('sha256:'+('d' if mode=='image-replaced' and s.get('go') else 'a')*64)
if a[:2]==['image','rm']:
    s['image']=False; finish()
if a[0]=='ps': finish('c'*64 if s['container'] else '')
if a[:2]==['network','ls']: finish('owned-network' if s['network'] else '')
if a[:2]==['volume','ls']: finish()
if 'inspect' in a:
    f=a[a.index('-f')+1]
    if '.Image' in f: finish('sha256:'+'a'*64)
    if 'addp.test-owner' in f:
        foreign=mode=='foreign-container' and a[0]=='container' or mode=='foreign-network' and a[0]=='network'
        finish('foreign' if foreign else 'manager-postgres')
    if 'addp.test-run' in f: finish(s['owner'])
finish('unexpected Docker call '+repr(a),2)
'''

GO = r'''#!/usr/bin/env python3
import json, os, signal, sys
from pathlib import Path
p=Path(os.environ['FAKE_STATE']); s=json.loads(p.read_text()); s.setdefault('go',[]).append(sys.argv[1:]);p.write_text(json.dumps(s))
mode=os.environ.get('FAKE_MODE','')
if mode in ('term','int'): os.kill(os.getppid(),signal.SIGTERM if mode=='term' else signal.SIGINT);sys.exit(0)
if mode=='go-fail': sys.exit(1)
if mode=='skip': print('--- SKIP: TestIntegrationPostgresManagerCleanupProductionRepositories (0.01s)');sys.exit(0)
if mode=='no-tests': print('testing: warning: no tests to run');sys.exit(0)
tests=['ExecutionReadIsolation'] if './internal/api' in sys.argv else ['CleanupProductionRepositories','CleanupRuntimeAtomicity','ContentSnapshots']
for t in tests: print('--- PASS: TestIntegrationPostgresManager'+t+' (0.01s)')
'''


class QuietHandler(http.server.SimpleHTTPRequestHandler):
    def log_message(self, *_args):
        pass


class ManagerPostgresGateTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="addp-manager-gate-policy-")
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name).resolve()
        self.repo = self.base / "repo"
        for name in ("scripts/test/manager-postgres-gate.sh", "scripts/infra/ports.sh",
                     "scripts/utils/observability-env.sh"):
            target = self.repo / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / name, target)
        (self.repo / "manager/backend").mkdir(parents=True)
        binary = self.base / "fake-bin"
        binary.mkdir()
        for name, source in {"docker": DOCKER, "go": GO,
                             "git": f"#!/bin/sh\necho {SOURCE}\n",
                             "uname": '#!/bin/sh\nif [ "$1" = -s ]; then echo Linux; else echo x86_64; fi\n'}.items():
            path = binary / name
            path.write_text(source)
            path.chmod(0o755)
        (self.base / "health").write_text("ok")
        handler = functools.partial(QuietHandler, directory=str(self.base))
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        self.env = dict(os.environ, PATH=f"{binary}:{os.environ['PATH']}",
                        CI="true", GITHUB_ACTIONS="true", GITHUB_JOB="manager-postgres",
                        RUNNER_OS="Linux", RUNNER_ARCH="X64", GITHUB_WORKSPACE=str(self.repo),
                        GITHUB_RUN_ID="123", GITHUB_RUN_ATTEMPT="1", RUNNER_TEMP=str(self.base / "runner"),
                        MANAGER_POSTGRES_CI_OWNED="1", MANAGER_POSTGRES_TEST_DSN=DSN,
                        MANAGER_MEILISEARCH_TEST_URL=f"http://127.0.0.1:{server.server_port}",
                        MANAGER_MEILISEARCH_TEST_KEY="fixture", FAKE_STATE=str(self.base / "state.json"))

    def run_gate(self, mode="", **environment):
        result = subprocess.run(["bash", str(self.repo / "scripts/test/manager-postgres-gate.sh")],
                                env=dict(self.env, FAKE_MODE=mode, **environment),
                                capture_output=True, text=True, timeout=20)
        state = self.base / "state.json"
        self.state = json.loads(state.read_text()) if state.exists() else {"calls": []}
        evidence = self.base / "runner/addp-manager-pg-123-1/postgres-evidence.json"
        self.proof = json.loads(evidence.read_text()) if evidence.exists() else None
        return result

    def assert_cleaned(self, result, *, success=False):
        self.assertEqual(0, result.returncode, result.stderr) if success else self.assertNotEqual(0, result.returncode)
        for kind in ("image", "container", "network"):
            self.assertFalse(self.state.get(kind), (kind, result.stderr, self.state))
        self.assertEqual("zero_residuals", self.proof["cleanup"])
        self.assertEqual(result.returncode, self.proof["test_exit_code"])

    def test_success_checks_actual_extensions_full_suite_and_cleanup(self):
        result = self.run_gate()
        self.assert_cleaned(result, success=True)
        self.assertEqual(IMAGE_ID, self.proof["image_id"])
        self.assertEqual(SOURCE, self.proof["source"])
        self.assertEqual(REVISION, self.proof["vector_source_revision"])
        self.assertEqual("0.8.0", self.proof["vector_version"])
        self.assertNotIn("password", json.dumps(self.proof))
        self.assertIn("ROLLBACK;", self.state["probe_sql"])
        self.assertEqual(2, len(self.state["go"]))
        self.assertEqual(["test", "-p", "1", "./internal/repository", "./internal/service", "./internal/preview", "-run", "^TestIntegrationPostgresManager", "-count=1", "-v"], self.state["go"][0])
        self.assertIn("^TestIntegrationPostgresManagerExecutionReadIsolation$", self.state["go"][1])

    def test_preparation_failure_always_cleans_owned_resources(self):
        for mode in ("build-fail", "up-fail", "probe-fail", "wrong-vector", "wrong-postgres", "probe-residue"):
            with self.subTest(mode=mode):
                result = self.run_gate(mode)
                self.assert_cleaned(result)
                self.assertNotIn("go", self.state)

    def test_test_failure_skip_and_missing_required_test_are_not_passed(self):
        for mode in ("go-fail", "skip", "no-tests"):
            with self.subTest(mode=mode):
                self.assert_cleaned(self.run_gate(mode))

    def test_termination_cleans_resources_and_preserves_failure(self):
        for mode, code in (("term", 143), ("int", 130)):
            with self.subTest(mode=mode):
                result = self.run_gate(mode)
                self.assert_cleaned(result)
                self.assertEqual(code, result.returncode)

    def test_cleanup_failure_is_a_failure_even_after_tests_pass(self):
        result = self.run_gate("cleanup-fail")
        self.assertNotEqual(0, result.returncode)
        self.assertTrue(self.state["container"])
        self.assertEqual("failed", self.proof["cleanup"])
        self.assertEqual(0, self.proof["test_exit_code"])

    def test_foreign_resource_prevents_compose_deletion(self):
        for mode in ("foreign-container", "foreign-network"):
            with self.subTest(mode=mode):
                # Each scenario gets an isolated fake daemon state.
                (self.base / "state.json").unlink(missing_ok=True)
                result = self.run_gate(mode)
                self.assertNotEqual(0, result.returncode)
                self.assertFalse(any(a[0] == "compose" and a[7] == "down" for a in self.state["calls"]))
                self.assertEqual("failed", self.proof["cleanup"])

    def test_replaced_image_is_never_removed(self):
        result = self.run_gate("image-replaced")
        self.assertNotEqual(0, result.returncode)
        self.assertFalse(any(a[:2] == ["image", "rm"] for a in self.state["calls"]))
        self.assertEqual("failed", self.proof["cleanup"])

    def test_wrong_ci_identity_or_target_is_refused_before_docker(self):
        for environment in ({"CI": "false"}, {"GITHUB_JOB": "other"}, {"RUNNER_ARCH": "ARM64"},
                            {"GITHUB_WORKSPACE": "/foreign"}, {"MANAGER_POSTGRES_TEST_DSN": DSN.replace("addp_manager_disposable", "addp")},
                            {"MANAGER_POSTGRES_TEST_DSN": DSN.replace("127.0.0.1", "other-host")},
                            {"MANAGER_POSTGRES_CI_OWNED": "0"}):
            with self.subTest(environment=environment):
                result = self.run_gate(**environment)
                self.assertNotEqual(0, result.returncode)
                self.assertEqual([], self.state["calls"])
                self.assertIsNone(self.proof)

    def test_real_manager_build_inputs_select_the_canonical_gate(self):
        spec = importlib.util.spec_from_file_location("manager_changed_gate", ROOT / "scripts/test/changed-gate.py")
        changed = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(changed)
        for path in ("scripts/test/manager-postgres-gate.sh", "scripts/infra/Dockerfile.postgres",
                     "scripts/test/docker-compose.manager-postgres-t2.yml", "scripts/test/manager-postgres-gate_test.py"):
            with self.subTest(path=path):
                self.assertIn("manager", changed.affected_modules(ROOT, [path]))
                commands = [s.command for s in changed.plan_changed(ROOT, [path])]
                self.assertEqual(1, commands.count(("make", "test-manager-postgres")))


if __name__ == "__main__":
    unittest.main()
