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
        for relative in ('scripts/test/http-quality-local-browser_test.mjs','scripts/test/monitor-http-local-gate.py','scripts/test/monitor-http-local-online.py','console/frontend/e2e/online/http-quality-local.spec.js','console/frontend/e2e/http-quality-page-support.mjs','console/frontend/e2e/online/local-identity-support.mjs','system/backend/cmd/online-test-fixture/http_quality_local.go'):
            path=self.root/relative;path.parent.mkdir(parents=True,exist_ok=True);path.write_text('owned fixture')
        self.make='test-monitor-http-local:\n\t@python3 scripts/test/monitor-http-local-gate.py --case=$(or $(MONITOR_HTTP_LOCAL_CASE),full)\ntest-monitor-http-local-runner:\n\t@node --test scripts/test/http-quality-local-browser_test.mjs\n\t@command -v openssl >/dev/null\n\t@$(MAKE) test-monitor-http-local-runner\n\t@python3 -m unittest scripts/test/monitor-http-local-gate_test.py scripts/test/monitor-http-local-online_test.py\n'
        (self.root/'Makefile').write_text(self.make)
        self.workflow='jobs:\n  platform-consistency:\n    steps:\n      - run: |\n          sudo apt-get install -y --no-install-recommends openssl\n          command -v openssl\n      - run: make test-platform\n'
        (self.root/'.github/workflows/platform-ci.yml').write_text(self.workflow)
    def test_manual_registration_and_existing_deterministic_runner(self):
        m.validate_local_http_quality_profile(self.root)
    def test_case_cannot_be_dropped_or_forced_to_full(self):
        for replacement in ('','--case=full'):
            (self.root/'Makefile').write_text(self.make.replace('--case=$(or $(MONITOR_HTTP_LOCAL_CASE),full)',replacement))
            with self.assertRaisesRegex(m.RegistrationError,'case forwarding'):
                m.validate_local_http_quality_profile(self.root)
    def test_missing_actual_entry_or_formal_identity_is_rejected(self):
        for marker in ('test-monitor-http-local:','@$(MAKE) test-monitor-http-local-runner','scripts/test/monitor-http-local-online_test.py'):
            (self.root/'Makefile').write_text(self.make.replace(marker,'omitted'))
            with self.subTest(marker=marker),self.assertRaises(m.RegistrationError):m.validate_local_http_quality_profile(self.root)
        (self.root/'Makefile').write_text(self.make)
        (self.root/'system/backend/cmd/online-test-fixture/http_quality_local.go').unlink()
        with self.assertRaises(m.RegistrationError):m.validate_local_http_quality_profile(self.root)
    def test_shared_page_oracle_is_a_required_formal_input(self):
        (self.root/'console/frontend/e2e/http-quality-page-support.mjs').unlink()
        with self.assertRaises(m.RegistrationError):m.validate_local_http_quality_profile(self.root)

    def test_native_tls_requires_local_admission_and_prior_ci_dependency_preparation(self):
        for content in (self.make.replace('@command -v openssl >/dev/null','@true'),
                self.make.replace('@command -v openssl >/dev/null\n','')+'\t@command -v openssl >/dev/null\n'):
            (self.root/'Makefile').write_text(content)
            with self.assertRaisesRegex(m.RegistrationError,'OpenSSL admission'):m.validate_local_http_quality_profile(self.root)
        (self.root/'Makefile').write_text(self.make)
        path=self.root/'.github/workflows/platform-ci.yml'
        for content in (self.workflow.replace('sudo apt-get install -y --no-install-recommends openssl','true'),
                self.workflow.replace('command -v openssl','true'),
                self.workflow.replace('      - run: make test-platform\n','').replace('    steps:\n','    steps:\n      - run: make test-platform\n')):
            path.write_text(content)
            with self.assertRaisesRegex(m.RegistrationError,'CI preparation'):m.validate_local_http_quality_profile(self.root)

    def test_browser_logout_regression_cannot_be_removed_from_the_existing_runner(self):
        for replacement in ('@true','@echo node --test scripts/test/http-quality-local-browser_test.mjs','@true # node --test scripts/test/http-quality-local-browser_test.mjs'):
            (self.root/'Makefile').write_text(self.make.replace('@node --test scripts/test/http-quality-local-browser_test.mjs',replacement))
            with self.subTest(replacement=replacement),self.assertRaisesRegex(m.RegistrationError,'deterministic Node'):m.validate_local_http_quality_profile(self.root)
        (self.root/'Makefile').write_text(self.make)
        (self.root/'scripts/test/http-quality-local-browser_test.mjs').unlink()
        with self.assertRaisesRegex(m.RegistrationError,'input missing'):m.validate_local_http_quality_profile(self.root)

    def test_public_workflows_cannot_dispatch_real_local_gate(self):
        path=self.root/'.github/workflows/rejected.yml'
        for invocation in ('make test-monitor-http-local','python3 scripts/test/monitor-http-local-gate.py',
                           'make test-monitor-http-local MONITOR_HTTP_LOCAL_CASE=compile-checkpoints',
                           'python3 scripts/test/monitor-http-local-gate.py --case=compile-checkpoints'):
            path.write_text('run: '+invocation)
            with self.subTest(invocation=invocation),self.assertRaises(m.RegistrationError):m.validate_local_http_quality_profile(self.root)
        path.write_text('run: make test-monitor-http-local-runner')
        m.validate_local_http_quality_profile(self.root)
