"""Deterministic Hosted safety and file recovery, with no developer services."""
import copy
from contextlib import contextmanager
import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

SCRIPT = Path(__file__).with_name("workflow-runtime-health-recovery-online.py")

def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value

SUITE = module("workflow_health", SCRIPT)
AGENT_PATH = SCRIPT.with_name("workflow-runtime-health-tool.py")
AGENT = module("workflow_health_tool", AGENT_PATH)
class FakeObserver:
    def __init__(self, tool, name, dep):
        self.runtimes = {name: {"tool": tool, "dep": dep, "id": 42}}
        self.events = []
        self.registration = {"http_status": 202, "engine_id": 42, "success": True}
        self.changed_dependency = False
        self.fail_offline = False

    def health(self, name, missing=None):
        self.events.append("missing" if missing else "healthy")
        facts = {self.runtimes[name]["dep"]: {"required": True, "available": missing is None},
                 "other": {"required": True, "available": not (missing and self.changed_dependency)}}
        return {"status": "unhealthy" if missing else "healthy", "dependencies": facts}

    def state(self, name, expected):
        self.events.append(expected)
        if expected == "offline" and self.fail_offline:
            raise SUITE.SuiteError("peer or lifecycle changed")
        return True

    def log_offset(self, name):
        return 0

    def register_unhealthy(self, name):
        self.events.append("register")
        assert not self.runtimes[name]["tool"].exists()
        return self.registration

    def registration_probe_finished(self, name, offset):
        self.events.append("registration_probe_finished")
        assert not self.runtimes[name]["tool"].exists()
        return True

    def stable(self):
        self.events.append("stable")



class RuntimeHealthRecoveryTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-health-unit-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.journal = self.root / 'journal.json'
        self.tool = self.root / 'tool'
        self.tool.write_bytes(b'owned product executable')
        self.tool.chmod(0o755)
        self.fault = AGENT.ToolFault(self.tool, self.journal)

    def test_three_cycles_preserve_order_and_file_identity(self):
        for name, dep, _ in SUITE.CASES:
            identity = AGENT.file_identity(self.tool)
            observer = FakeObserver(self.tool, name, dep)
            fault = self.fault
            class Adapter:
                @contextmanager
                def missing(_, runtime):
                    fault.missing()
                    try:
                        yield
                    finally:
                        fault.restore()
            with self.subTest(name=name):
                report = SUITE.run_cycle(observer, Adapter(), name)
                self.assertEqual(report['result'], 'passed')
                self.assertEqual(observer.events, ['healthy','online','missing','offline','register',
                    'registration_probe_finished','offline','healthy','online','stable'])
                self.assertEqual(AGENT.file_identity(self.tool), identity)
                self.assertFalse(self.journal.exists())

    def test_failed_assertions_restore_file_and_never_report_online_recovery(self):
        for failure in ('wrong_id','registration_rejected','dependency_changed','peer_changed','probe_timeout'):
            observer = FakeObserver(self.tool, 'pointcloud', 'pdal')
            if failure == 'wrong_id': observer.registration['engine_id'] = 43
            elif failure == 'registration_rejected': observer.registration['http_status'] = 503
            elif failure == 'dependency_changed': observer.changed_dependency = True
            elif failure == 'peer_changed': observer.fail_offline = True
            else: observer.registration_probe_finished = lambda *_: (_ for _ in ()).throw(SUITE.SuiteError('timeout'))
            fault = self.fault
            class Adapter:
                @contextmanager
                def missing(_, runtime):
                    fault.missing()
                    try: yield
                    finally: fault.restore()
            with self.subTest(failure=failure), self.assertRaises(SUITE.SuiteError):
                SUITE.run_cycle(observer, Adapter(), 'pointcloud')
            self.assertTrue(self.tool.exists())
            self.assertFalse(self.journal.exists())
            self.assertEqual(observer.events.count('online'), 1)

    def test_sigkill_leaves_recoverable_journal_and_restore_is_idempotent(self):
        identity = AGENT.file_identity(self.tool)
        code = f'''import importlib.util, pathlib, os, signal
+s=importlib.util.spec_from_file_location("fault",{str(AGENT_PATH)!r})
+m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
+m.ToolFault(pathlib.Path({str(self.tool)!r}),pathlib.Path({str(self.journal)!r})).missing()
+os.kill(os.getpid(),signal.SIGKILL)
+'''.replace('\n+', '\n')
        result = subprocess.run([sys.executable,'-c',code],capture_output=True,text=True)
        self.assertEqual(result.returncode, -signal.SIGKILL, result.stderr)
        self.assertFalse(self.tool.exists())
        self.fault.restore();self.fault.restore()
        self.assertEqual(AGENT.file_identity(self.tool),identity)
        self.assertFalse(self.fault.backup.exists())

    def test_sigterm_finally_restores_hardlinked_file_without_chmod(self):
        cache = self.root/'cache'
        os.link(self.tool,cache)
        identity = AGENT.file_identity(cache)
        try:
            self.fault.missing()
            with self.assertRaises(SUITE.SuiteError): SUITE.interrupted(signal.SIGTERM,None)
        finally: self.fault.restore()
        self.assertEqual(AGENT.file_identity(cache),identity)
        self.assertEqual(AGENT.file_identity(self.tool),identity)

    def test_refuses_concurrent_replacement_or_modified_backup(self):
        self.fault.missing()
        self.tool.write_bytes(b'other owner replacement')
        with self.assertRaisesRegex(RuntimeError,'refusing overwrite'): self.fault.restore()
        self.assertTrue(self.journal.exists())
        self.assertEqual(self.tool.read_bytes(),b'other owner replacement')
        self.tool.unlink()
        self.fault.backup.write_bytes(b'changed backup')
        with self.assertRaisesRegex(RuntimeError,'backup identity'): self.fault.restore()

    def test_agent_materializes_private_inode_then_restores_exactly(self):
        alias = self.root/'alias'
        alias.symlink_to(self.tool)
        before = AGENT.file_identity(self.tool)
        with patch.object(AGENT,'TOOLS',{'TEST_TOOL':str(self.tool)}), patch.object(AGENT,'JOURNAL',self.journal), \
             patch.dict(os.environ,{'TEST_TOOL':str(alias)}), patch.object(sys,'argv',['agent','prepare','TEST_TOOL']):
            AGENT.main()
            private = AGENT.file_identity(self.tool)
            self.assertEqual(private['sha256'],before['sha256'])
            self.assertEqual(private['mode'],before['mode'])
            self.assertNotEqual(private['inode'],before['inode'])
            self.fault.missing();self.fault.restore()
            self.assertEqual(AGENT.file_identity(self.tool),private)
        self.assertFalse(list(self.root.glob('*.addp-health-*')))

    def test_personal_environment_is_rejected_before_container_commands(self):
        (self.root/'.env').touch()
        env={'ADDP_ONLINE_HOST':'1','ADDP_ONLINE_HOSTED':'1','GITHUB_ACTIONS':'true','RUNNER_OS':'Linux'}
        with patch.object(SUITE,'ROOT',self.root),patch.dict(os.environ,env), self.assertRaisesRegex(SUITE.SuiteError,'repository .env'):
            SUITE.isolation()

    def test_product_identity_rejects_wrong_owner_entry_and_container_replacement(self):
        info={'Id':'container-1','Image':'image-1','RestartCount':0,
              'State':{'Running':True,'Pid':123,'StartedAt':'now'},'HostConfig':{'NetworkMode':'host'},
              'Config':{'Env':['PORT=8102'],'Cmd':['python','api_server.py'],'Labels':{'com.addp.online-runtime':SUITE.SUITE,'com.addp.online-run-id':'run-1'}}}
        def command(*args,**kwargs): return 'container-1\n' if args[0]=='ps' else json.dumps([info])
        with patch.dict(os.environ,{'ADDP_ONLINE_TEST_RUN_ID':'run-1'}),patch.object(SUITE,'docker',side_effect=command):
            runtime=SUITE.Runtime('pointcloud');runtime.stable()
            info['Id']='other-container'
            with self.assertRaises(SUITE.SuiteError):runtime.stable()
            info['Id']='container-1';info['Config']['Labels']['com.addp.online-run-id']='other-run'
            with self.assertRaisesRegex(SUITE.SuiteError,'ownership'):SUITE.Runtime('pointcloud')
            info['Config']['Labels']['com.addp.online-run-id']='run-1';info['Config']['Cmd']=['fake-server']
            with self.assertRaisesRegex(SUITE.SuiteError,'entry'):SUITE.Runtime('pointcloud')

    def test_descriptor_identity_lifecycle_and_other_instances_remain_stable(self):
        from types import SimpleNamespace
        observer=SUITE.Observer.__new__(SUITE.Observer)
        observer.runtimes={};observer.products={};observer.stable=lambda:None
        descriptors=[]
        for number,(name,dep,_) in enumerate(SUITE.CASES,42):
            observer.runtimes[name]={'dep':dep}
            observer.products[name]=SimpleNamespace(identity={'port':8100+number},logs=lambda name=name:f'{name}_workflow registered to System')
            descriptors.append({'id':number,'engine_type':name+'_workflow','lifecycle_state':'active','connection_status':'online',
                                'runtime_endpoint':{'protocol':'http','host':'127.0.0.1','port':8100+number}})
        observer.descriptors=lambda:copy.deepcopy(descriptors)
        observer.bind()
        self.assertTrue(observer.state('pointcloud','online'))
        descriptors[0]['connection_status']='offline'
        self.assertTrue(observer.state('pointcloud','offline'))
        descriptors[1]['connection_status']='offline'
        self.assertFalse(observer.state('pointcloud','offline'))
        descriptors[0]['id']=99
        with self.assertRaises(SUITE.SuiteError):observer.state('pointcloud','offline')
        descriptors[0]['id']=42;descriptors[0]['lifecycle_state']='retired'
        with self.assertRaises(SUITE.SuiteError):observer.state('pointcloud','offline')

    def test_registration_probe_counts_colored_werkzeug_health_responses(self):
        from types import SimpleNamespace
        # Hosted Docker emits Werkzeug's ANSI-styled 503 status lines.
        plain = '127.0.0.1 - - [20/Oct/2026 15:46:13] "GET /health HTTP/1.1" 503 -\n'
        colored = '127.0.0.1 - - [20/Oct/2026 15:46:15] "\x1b[35m\x1b[1mGET /health HTTP/1.1\x1b[0m" 503 -\n'
        unrelated = '"GET /health HTTP/1.1" 200 -\n"GET /operators HTTP/1.1" 503 -\n'
        observer = SUITE.Observer.__new__(SUITE.Observer)
        stream = plain + unrelated
        observer.products = {'pointcloud': SimpleNamespace(logs=lambda: stream)}
        offset = observer.log_offset('pointcloud')
        self.assertEqual(offset, 1)
        stream += colored
        self.assertEqual(observer.log_offset('pointcloud'), 2)
        self.assertFalse(observer.registration_probe_finished('pointcloud', offset))
        stream += colored
        self.assertTrue(observer.registration_probe_finished('pointcloud', offset))

    def test_health_contract_rejects_false_positive_and_legacy_shapes(self):
        body = {"status": "healthy", "service": "pointcloud-workflow", "version": "1.0",
                "dependencies": {"pdal": {"required": True, "available": True}}}
        SUITE.validate_health(200, body)
        for modify, status in ((lambda b: b.update(status="degraded"), 200),
                               (lambda b: b.update(conversion_ready=True), 200),
                               (lambda b: b["dependencies"]["pdal"].update(available=False), 200),
                               (lambda b: b["dependencies"]["pdal"].update(required="true"), 200),
                               (lambda b: b["dependencies"]["pdal"].update(path="/private"), 200),
                               (lambda b: b.update(service=""), 200), (lambda b: None, 503)):
            invalid = copy.deepcopy(body)
            modify(invalid)
            with self.subTest(body=invalid), self.assertRaises(SUITE.SuiteError):
                SUITE.validate_health(status, invalid)
        body["status"] = "unhealthy"
        body["dependencies"]["pdal"]["available"] = False
        SUITE.validate_health(503, body, "pdal")


if __name__ == '__main__': unittest.main()
