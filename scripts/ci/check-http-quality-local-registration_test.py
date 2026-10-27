"""A local-only identity gate must never turn into an Actions workload."""
from pathlib import Path
import importlib.util
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('registration',Path(__file__).with_name('check-online-ci-registration.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)

class LocalHTTPRegistrationTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup);self.root=Path(self.tmp.name)
        (self.root/'.github/workflows').mkdir(parents=True)
        for relative in ('scripts/test/monitor-http-local-gate.py','scripts/test/monitor-http-local-online.py','console/frontend/e2e/online/http-quality-local.spec.js','console/frontend/e2e/online/local-identity-support.mjs','system/backend/cmd/online-test-fixture/http_quality_local.go'):
            path=self.root/relative;path.parent.mkdir(parents=True,exist_ok=True);path.write_text('owned fixture')
        self.make='test-monitor-http-local:\n\t@python3 scripts/test/monitor-http-local-gate.py\ntest-monitor-http-local-runner:\n\t@$(MAKE) test-monitor-http-local-runner\n\t@python3 -m unittest scripts/test/monitor-http-local-gate_test.py scripts/test/monitor-http-local-online_test.py\n'
        (self.root/'Makefile').write_text(self.make)
    def test_manual_registration_and_existing_deterministic_runner(self):
        m.validate_local_http_quality_profile(self.root)
    def test_missing_actual_entry_or_formal_identity_is_rejected(self):
        for marker in ('test-monitor-http-local:','@$(MAKE) test-monitor-http-local-runner','scripts/test/monitor-http-local-online_test.py'):
            (self.root/'Makefile').write_text(self.make.replace(marker,'omitted'))
            with self.subTest(marker=marker),self.assertRaises(m.RegistrationError):m.validate_local_http_quality_profile(self.root)
        (self.root/'Makefile').write_text(self.make)
        (self.root/'system/backend/cmd/online-test-fixture/http_quality_local.go').unlink()
        with self.assertRaises(m.RegistrationError):m.validate_local_http_quality_profile(self.root)
    def test_public_workflows_cannot_dispatch_real_local_gate(self):
        path=self.root/'.github/workflows/rejected.yml'
        for invocation in ('make test-monitor-http-local','python3 scripts/test/monitor-http-local-gate.py'):
            path.write_text('run: '+invocation)
            with self.subTest(invocation=invocation),self.assertRaises(m.RegistrationError):m.validate_local_http_quality_profile(self.root)
        path.write_text('run: make test-monitor-http-local-runner')
        m.validate_local_http_quality_profile(self.root)
