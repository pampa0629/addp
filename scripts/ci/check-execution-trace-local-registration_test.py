"""A local-only identity gate must never turn into an Actions workload."""
from pathlib import Path
import importlib.util
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('registration',Path(__file__).with_name('check-online-ci-registration.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)

class LocalTraceRegistrationTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup);self.root=Path(self.tmp.name)
        (self.root/'.github/workflows').mkdir(parents=True)
        for relative in ('scripts/test/monitor-traces-local-gate.py','scripts/test/monitor-traces-local-online.py','console/frontend/e2e/online/execution-trace-local.spec.js','system/backend/cmd/online-test-fixture/execution_trace_local.go','console/frontend/e2e/online/execution-navigation-local-support.mjs','scripts/test/execution-navigation-local_test.mjs','develop/backend/internal/service/navigation_fixture_preflight_test.go'):
            path=self.root/relative;path.parent.mkdir(parents=True,exist_ok=True);path.write_text('owned fixture')
        self.make='test-monitor-traces-local:\n\t@python3 scripts/test/monitor-traces-local-gate.py\ntest-execution-online-ci-runner: test-monitor-traces-local-runner\ntest-monitor-traces-local-runner:\n\t@node --test scripts/test/execution-navigation-local_test.mjs\n\t@cd develop/backend && GOWORK=off go test ./internal/service -run \'^TestLocalNavigationFixturePreparedReadOnly$$\' -count=1 -v\n\t@python3 -m unittest scripts/test/monitor-traces-local-gate_test.py scripts/test/monitor-traces-local-online_test.py\n'
        (self.root/'Makefile').write_text(self.make)
    def test_manual_registration_and_existing_deterministic_runner(self):
        m.validate_local_execution_trace_profile(self.root)
    def test_missing_actual_entry_or_formal_identity_is_rejected(self):
        for marker in ('test-monitor-traces-local:','scripts/test/monitor-traces-local-online_test.py'):
            (self.root/'Makefile').write_text(self.make.replace(marker,'omitted'))
            with self.subTest(marker=marker),self.assertRaises(m.RegistrationError):m.validate_local_execution_trace_profile(self.root)
        (self.root/'Makefile').write_text(self.make)
        (self.root/'system/backend/cmd/online-test-fixture/execution_trace_local.go').unlink()
        with self.assertRaises(m.RegistrationError):m.validate_local_execution_trace_profile(self.root)
    def test_public_workflows_cannot_dispatch_real_local_gate(self):
        path=self.root/'.github/workflows/rejected.yml'
        for invocation in ('make test-monitor-traces-local','python3 scripts/test/monitor-traces-local-gate.py'):
            path.write_text('run: '+invocation)
            with self.subTest(invocation=invocation),self.assertRaises(m.RegistrationError):m.validate_local_execution_trace_profile(self.root)
        path.write_text('run: make test-monitor-traces-local-runner')
        m.validate_local_execution_trace_profile(self.root)

    def test_navigation_requires_actual_suite_and_linux_dependency(self):
        for old, new in (("@node --test scripts/test/execution-navigation-local_test.mjs", "@true"),
                         ("test-execution-online-ci-runner: test-monitor-traces-local-runner", "test-execution-online-ci-runner:")):
            (self.root/'Makefile').write_text(self.make.replace(old,new))
            with self.subTest(old=old),self.assertRaises(m.RegistrationError):m.validate_local_execution_trace_profile(self.root)
        (self.root/'Makefile').write_text(self.make)
        (self.root/'console/frontend/e2e/online/execution-navigation-local-support.mjs').unlink()
        with self.assertRaises(m.RegistrationError):m.validate_local_execution_trace_profile(self.root)
