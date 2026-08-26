import importlib.util
import sys
import tempfile
import textwrap
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("check-online-ci-registration.py")
SPEC = importlib.util.spec_from_file_location("check_online_ci_registration", SCRIPT)
CHECK = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = CHECK
SPEC.loader.exec_module(CHECK)


class OnlineCIRegistrationTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-online-registration-")
        self.repository = Path(self.temporary.name)
        (self.repository / "scripts/test").mkdir(parents=True)
        (self.repository / ".github/workflows").mkdir(parents=True)
        (self.repository / "scripts/test/online-gate.py").write_text(
            textwrap.dedent(
                """\
                from dataclasses import dataclass
                @dataclass(frozen=True)
                class Suite:
                    command: tuple[str, ...]
                    services: tuple[tuple[str, str], ...]
                SUITES = {
                    "first-suite": Suite(("first",), (("system", "SYSTEM_URL"),)),
                    "second-suite": Suite(("second",), (("gateway", "GATEWAY_URL"),)),
                }
                """
            ),
            encoding="utf-8",
        )
        (self.repository / "scripts/test/online-host-gate.sh").write_text(
            textwrap.dedent(
                """\
                case "$ONLINE_SUITE" in
                  first-suite)
                    START_TARGET=-system
                    ;;
                  second-suite)
                    START_TARGET=-model
                    ;;
                esac
                python3 scripts/test/online-preflight.py --environment-only
                printf 'database=%s\\n' "$POSTGRES_DB"
                run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
                """
            ),
            encoding="utf-8",
        )
        self.workflow = self.repository / ".github/workflows/online-t4-gates.yml"
        self.workflow.write_text(self._workflow(), encoding="utf-8")

    def tearDown(self) -> None:
        self.temporary.cleanup()

    @staticmethod
    def _workflow() -> str:
        return textwrap.dedent(
            """\
            on:
              workflow_dispatch:
                inputs:
                  suite:
                    options:
                      - first-suite
                      - second-suite
            jobs:
              online:
                runs-on:
                  - self-hosted
                  - macOS
                  - addp-online
                environment: addp-online
                steps:
                  - env:
                      ADDP_ONLINE_ARTIFACT_DIR: ${{ runner.temp }}/addp-online-${{ github.run_id }}
                    run: bash scripts/test/online-host-gate.sh --check-only
                  - env:
                      ADDP_ONLINE_ARTIFACT_DIR: ${{ runner.temp }}/addp-online-${{ github.run_id }}
                    run: bash scripts/test/online-host-gate.sh
                  - uses: actions/upload-artifact@pinned
            """
        )

    def test_accepts_one_profile_and_workflow_choice_per_registered_suite(self) -> None:
        CHECK.check_registration(self.repository)

    def test_rejects_missing_deployment_profile(self) -> None:
        script = self.repository / "scripts/test/online-host-gate.sh"
        script.write_text(
            script.read_text(encoding="utf-8").replace(
                "  second-suite)\n    START_TARGET=-model\n    ;;\n", ""
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "do not match"):
            CHECK.check_registration(self.repository)

    def test_rejects_nightly_schedule_before_first_real_run(self) -> None:
        self.workflow.write_text(
            self._workflow().replace(
                "  workflow_dispatch:\n", "  schedule:\n    - cron: '0 1 * * *'\n  workflow_dispatch:\n"
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "must remain manual"):
            CHECK.check_registration(self.repository)

    def test_rejects_workflow_without_readiness_check(self) -> None:
        self.workflow.write_text(
            self._workflow().replace(
                "      - env:\n"
                "          ADDP_ONLINE_ARTIFACT_DIR: "
                "${{ runner.temp }}/addp-online-${{ github.run_id }}\n"
                "        run: bash scripts/test/online-host-gate.sh --check-only\n",
                "",
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "--check-only"):
            CHECK.check_registration(self.repository)

    def test_rejects_runner_context_in_job_level_environment(self) -> None:
        self.workflow.write_text(
            self._workflow().replace(
                "    steps:\n",
                "    env:\n"
                "      ADDP_ONLINE_ARTIFACT_DIR: ${{ runner.temp }}/invalid\n"
                "    steps:\n",
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "job-level env"):
            CHECK.check_registration(self.repository)

    def test_requires_artifact_directory_on_both_lifecycle_steps(self) -> None:
        self.workflow.write_text(
            self._workflow().replace(
                "      - env:\n"
                "          ADDP_ONLINE_ARTIFACT_DIR: "
                "${{ runner.temp }}/addp-online-${{ github.run_id }}\n"
                "        run: bash scripts/test/online-host-gate.sh\n",
                "      - run: bash scripts/test/online-host-gate.sh\n",
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "both lifecycle steps"):
            CHECK.check_registration(self.repository)

    def test_rejects_host_gate_without_shared_environment_preflight(self) -> None:
        script = self.repository / "scripts/test/online-host-gate.sh"
        script.write_text(
            script.read_text(encoding="utf-8").replace(
                "python3 scripts/test/online-preflight.py --environment-only\n", ""
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "environment-only"):
            CHECK.check_registration(self.repository)

    def test_rejects_module_registry_suite_without_formal_process_profile(self) -> None:
        gate = self.repository / "scripts/test/online-gate.py"
        gate.write_text(
            gate.read_text(encoding="utf-8").replace(
                '"first-suite"', '"module-registry-recovery"'
            ),
            encoding="utf-8",
        )
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(
            host.read_text(encoding="utf-8").replace(
                "first-suite", "module-registry-recovery"
            ),
            encoding="utf-8",
        )
        self.workflow.write_text(
            self.workflow.read_text(encoding="utf-8").replace(
                "first-suite", "module-registry-recovery"
            ),
            encoding="utf-8",
        )

        with self.assertRaisesRegex(CHECK.RegistrationError, "process profile is missing"):
            CHECK.check_registration(self.repository)


if __name__ == "__main__":
    unittest.main()
