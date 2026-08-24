import importlib.util
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("check-release-ci-registration.py")
SPEC = importlib.util.spec_from_file_location("release_ci_registration", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class ReleaseCIRegistrationTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-release-ci-")
        self.repository = Path(self.temporary.name)
        self._write(
            "scripts/test/release-gate.py",
            "SUITES = {\n"
            "    'common-python-cli': Suite(target='test-common-python-cli-release'),\n"
            "    'agent-evaluation': Suite(target='test-agent-eval-release'),\n"
            "}\n",
        )
        self._write(
            "Makefile",
            ".PHONY: test-release\n"
            "test-release:\n"
            '\t@python3 scripts/test/release-gate.py --suite "$(RELEASE_SUITE)"\n\n'
            "test-common-python-cli-release:\n\t@true\n\n"
            "test-agent-eval-release:\n\t@true\n\n"
            "test-release-runner:\n"
            "\t@python3 -m unittest scripts/test/release-gate_test.py scripts/ci/check-release-ci-registration_test.py\n"
            "\t@python3 scripts/ci/check-release-ci-registration.py --repository .\n\n"
            "test-platform:\n"
            "\t@$(MAKE) test-release-runner\n",
        )
        self._write(
            ".github/workflows/release-and-t2-gates.yml",
            "jobs:\n"
            "  selection:\n"
            "    steps:\n"
            "      - run: bash scripts/ci/select-gate-by-paths.sh scripts/test/release-gate.py\n"
            "  cli-product-macos-verification:\n"
            "    steps:\n"
            "      - run: make test-release RELEASE_SUITE=common-python-cli\n"
            "        env:\n"
            "          ADDP_RELEASE_ARTIFACT_DIR: /tmp/release\n"
            "      - uses: ./.github/actions/ci-gate-summary\n"
            "        with:\n"
            "          details-file: /tmp/release/release-summary.md\n",
        )

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def _write(self, relative_path: str, content: str) -> None:
        path = self.repository / relative_path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")

    def test_accepts_complete_registration(self) -> None:
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_rejects_missing_owner_target(self) -> None:
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8").replace(
                "test-agent-eval-release:\n\t@true\n\n", ""
            ),
            encoding="utf-8",
        )
        self.assertIn(
            "release suite agent-evaluation: Makefile owner target test-agent-eval-release is missing",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_owner_target_exposed_as_public_help_entry(self) -> None:
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8").replace(
                "test-agent-eval-release:\n",
                "test-agent-eval-release: ## direct public entry\n",
            ),
            encoding="utf-8",
        )
        self.assertIn(
            "release suite agent-evaluation: Makefile owner target "
            "test-agent-eval-release must remain internal to test-release",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_direct_cli_owner_invocation(self) -> None:
        workflow = self.repository / ".github/workflows/release-and-t2-gates.yml"
        workflow.write_text(
            workflow.read_text(encoding="utf-8").replace(
                "make test-release RELEASE_SUITE=common-python-cli",
                "make test-common-python-cli-release",
            ),
            encoding="utf-8",
        )
        errors = MODULE.validate_registration(self.repository)
        self.assertIn("CLI T5 workflow must call the shared test-release entry", errors)
        self.assertIn("CLI T5 workflow must not bypass the shared test-release entry", errors)

    def test_rejects_missing_shared_summary(self) -> None:
        workflow = self.repository / ".github/workflows/release-and-t2-gates.yml"
        workflow.write_text(
            workflow.read_text(encoding="utf-8").replace(
                "          details-file: /tmp/release/release-summary.md\n", ""
            ),
            encoding="utf-8",
        )
        self.assertIn(
            "CLI T5 workflow must attach the shared release summary",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_unregistered_dispatcher_tests(self) -> None:
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8").replace(
                "scripts/test/release-gate_test.py ", ""
            ),
            encoding="utf-8",
        )
        self.assertIn(
            "Makefile test-release-runner must run scripts/test/release-gate_test.py",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_platform_gate_without_release_runner(self) -> None:
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8").replace(
                "\t@$(MAKE) test-release-runner\n", "\t@true\n"
            ),
            encoding="utf-8",
        )
        self.assertIn(
            "Makefile test-platform must run test-release-runner",
            MODULE.validate_registration(self.repository),
        )


if __name__ == "__main__":
    unittest.main()
