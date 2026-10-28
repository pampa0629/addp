import importlib.util
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("quality_online_ci_registration", ROOT / "scripts/ci/check-online-ci-registration.py")
CHECK = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = CHECK
SPEC.loader.exec_module(CHECK)
RECOVERY_SPEC = importlib.util.spec_from_file_location("quality_recovery_registration", ROOT / "scripts/ci/check-quality-bounded-recovery.py")
RECOVERY = importlib.util.module_from_spec(RECOVERY_SPEC)
RECOVERY_SPEC.loader.exec_module(RECOVERY)



class QualityOnlineRegistrationTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-quality-registration-")
        self.addCleanup(self.temporary.cleanup)
        self.repository = Path(self.temporary.name)
        for relative in (".github/workflows/online-t4-gates.yml", "Makefile", "scripts/test/online-host-gate.sh",
                         "scripts/test/online-hosted-quality-gate.sh", "scripts/test/quality-online-fixture.py",
                         "business/scripts/online-quality-postgres-fixture.py", "system/backend/cmd/online-test-fixture/main.go", "scripts/utils/hosted-online.sh",
                         "scripts/test/quality-bounded-recovery-online.py", "scripts/test/quality-bounded-recovery-online_test.py",
                         "scripts/test/quality-bounded-recovery-faults.py", "scripts/test/quality-bounded-recovery-faults_test.py"):
            path = self.repository / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT / relative, path)

    def check(self):
        CHECK.validate_quality_dynamic_binding_profile(self.repository, {"quality-dynamic-binding"})
        RECOVERY.validate(self.repository)

    def test_uses_one_manual_hosted_profile_and_registered_owner_test_entry(self):
        self.check()
        for relative, fragment in ((".github/workflows/online-t4-gates.yml", "&& inputs.suite != 'quality-dynamic-binding'"),
                                   (".github/workflows/online-t4-gates.yml", "github.event_name == 'workflow_dispatch' && inputs.suite == 'quality-dynamic-binding'"),
                                   ("Makefile", "$(MAKE) test-quality-online-runner"),
                                   ("scripts/test/online-hosted-quality-gate.sh", "-model -meta -quality -orchestrator -monitor"),
                                   ("scripts/test/online-hosted-quality-gate.sh", "unset ADDP_ONLINE_FIXTURE_QUALITY_ACCESS_TOKEN"),
                                   ("business/scripts/online-quality-postgres-fixture.py", "container remains after cleanup")):
            path = self.repository / relative
            original = path.read_text()
            with self.subTest(fragment=fragment):
                path.write_text(original.replace(fragment, "removed"))
                with self.assertRaises(CHECK.RegistrationError):
                    self.check()
                path.write_text(original)

    def test_self_hosted_profile_cannot_be_reintroduced(self):
        path = self.repository / "scripts/test/online-host-gate.sh"
        path.write_text(path.read_text() + "\n  quality-dynamic-binding)\n    START_TARGET=-all\n")
        with self.assertRaises(CHECK.RegistrationError):
            self.check()

    def test_recovery_case_admission_cleanup_and_both_regressions_are_registered(self):
        self.check()
        cases = ((".github/workflows/online-t4-gates.yml", "- bounded-recovery"),
                 (".github/workflows/online-t4-gates.yml", "ADDP_ONLINE_QUALITY_CASE: ${{ inputs.quality_case || 'full' }}"),
                 ("Makefile", "quality-bounded-recovery-online_test.py"),
                 ("Makefile", "quality-bounded-recovery-faults_test.py"),
                 ("Makefile", "scripts/ci/check-quality-bounded-recovery.py"),
                 ("scripts/test/online-hosted-quality-gate.sh", "full|bounded-recovery"),
                 ("scripts/test/online-hosted-quality-gate.sh", "scripts.test.quality-bounded-recovery-faults resume"),
                 ("scripts/utils/hosted-online.sh", "before_online_application_stop ||"))
        for relative, fragment in cases:
            path = self.repository / relative
            original = path.read_text()
            with self.subTest(fragment=fragment):
                path.write_text(original.replace(fragment, "removed"))
                with self.assertRaises(ValueError):
                    RECOVERY.validate(self.repository)
                path.write_text(original)


if __name__ == "__main__":
    unittest.main()
