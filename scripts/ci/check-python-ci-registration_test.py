#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("check-python-ci-registration.py")
SPEC = importlib.util.spec_from_file_location("python_ci_registration", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class PythonCIRegistrationTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.repository = Path(self.temporary_directory.name)
        subprocess.run(["git", "init", "-q"], cwd=self.repository, check=True)
        manifest = self.repository / "sample/backend/requirements.txt"
        manifest.parent.mkdir(parents=True)
        manifest.write_text("-e ../../common-python[dev]\n", encoding="utf-8")
        (self.repository / "Makefile").write_text(
            "test: test-sample\n\ntest-sample:\n\t@true\n", encoding="utf-8"
        )
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        workflow.parent.mkdir(parents=True)
        workflow.write_text(
            "jobs:\n"
            "  sample-tests:\n"
            "    steps:\n"
            "      - run: python3 scripts/ci/select-module-gate.py --module sample\n"
            "      - uses: ./.github/actions/prepare-python-gate\n"
            "      - run: make test-sample\n",
            encoding="utf-8",
        )
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def fastapi_registration(self) -> None:
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        makefile = self.repository / "Makefile"
        for owner in ("agent", "copilot"):
            backend = self.repository / owner / "backend"
            backend.mkdir(parents=True)
            (backend / "requirements.txt").write_text("fastapi\n")
            (backend / "openapi.json").write_text("{}\n")
            target = f"test-{owner}"
            makefile.write_text(makefile.read_text().replace("test: ", f"test: {target} ", 1) + f"\n{target}:\n\t@true\n")
            workflow.write_text(workflow.read_text() +
                f"  {owner}-tests:\n    steps:\n"
                f"      - run: python3 scripts/ci/select-module-gate.py --module {owner}\n"
                "      - uses: ./.github/actions/prepare-python-gate\n"
                f"        with:\n          venv-path: {owner}/backend/venv\n"
                "          python-version: \"3.12\"\n"
                f"          requirements-file: {owner}/backend/requirements.txt\n"
                f"      - run: make {target}\n")
        setup = ""
        for owner in ("agent", "copilot"):
            setup += (f"      - name: Prepare {owner} Swagger environment\n"
                "        uses: ./.github/actions/prepare-python-gate\n"
                f"        with:\n          venv-path: {owner}/backend/venv\n"
                "          python-version: \"3.12\"\n"
                f"          requirements-file: {owner}/backend/requirements.txt\n")
        workflow.write_text(workflow.read_text() + "  platform-consistency:\n    steps:\n" + setup + "      - run: make test-platform\n")
        regression = self.repository / "scripts/test/swagger-route-coverage_test.py"
        regression.parent.mkdir(parents=True)
        regression.write_text("# regression fixture\n")
        makefile.write_text(makefile.read_text() +
            "\ntest-swagger:\n\t@python3 scripts/test/swagger-route-coverage_test.py\n"
            "\t@SWAGGER_COVERAGE_WARN_ONLY=1 bash scripts/swagger/check-route-coverage.sh all\n"
            "\ntest-authorization:\n\t@$(MAKE) test-swagger\n")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)

    def test_accepts_fastapi_platform_environments(self) -> None:
        self.fastapi_registration()
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_platform_requires_unconditional_owner_environment_before_check(self) -> None:
        self.fastapi_registration()
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        original = workflow.read_text()
        prefix, platform = original.split("  platform-consistency:\n")
        for changed in (
            platform.replace("uses: ./.github/actions/prepare-python-gate", "run: pip install fastapi", 1),
            platform.replace("agent/backend/venv", "agent/backend/.venv"),
            platform.replace("agent/backend/requirements.txt", "common-python/pyproject.toml"),
            platform.replace('python-version: "3.12"', 'python-version: "3.11"', 1),
            platform.replace("        uses:", "        if: false\n        uses:", 1),
            platform.replace("    steps:\n", "    steps:\n      - run: make test-platform\n", 1),
        ):
            with self.subTest(platform=changed):
                workflow.write_text(prefix + "  platform-consistency:\n" + changed)
                self.assertIn(
                    "agent/backend/openapi.json: platform Swagger checks require the unconditional owner Python environment before make test-platform",
                    MODULE.validate_registration(self.repository),
                )

    def test_fastapi_swagger_standard_entry_cannot_omit_regressions(self) -> None:
        self.fastapi_registration()
        makefile = self.repository / "Makefile"
        original = makefile.read_text()
        for removed in (
            "\t@python3 scripts/test/swagger-route-coverage_test.py\n",
            "\t@SWAGGER_COVERAGE_WARN_ONLY=1 bash scripts/swagger/check-route-coverage.sh all\n",
            "\t@$(MAKE) test-swagger\n",
        ):
            with self.subTest(removed=removed):
                makefile.write_text(original.replace(removed, ""))
                self.assertIn("FastAPI Swagger checks must retain their regression and coverage entry in test-authorization", MODULE.validate_registration(self.repository))
        makefile.write_text(original)
        (self.repository / "scripts/test/swagger-route-coverage_test.py").unlink()
        self.assertIn("FastAPI Swagger checks must retain their regression and coverage entry in test-authorization", MODULE.validate_registration(self.repository))

    def test_accepts_complete_registration(self) -> None:
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_registered_engine_target_requires_root_and_own_ci_job(self) -> None:
        manifest = self.repository / "engines/model3d-workflow/requirements.txt"
        manifest.parent.mkdir(parents=True)
        manifest.write_text("Pillow==12.3.0\n", encoding="utf-8")
        subprocess.run(["git", "add", str(manifest.relative_to(self.repository))], cwd=self.repository, check=True)
        makefile = self.repository / "Makefile"
        makefile.write_text(makefile.read_text() + "\ntest-model3d-workflow:\n\t@true\n")
        errors = MODULE.validate_registration(self.repository)
        self.assertIn("engines/model3d-workflow/requirements.txt: root test dependency test-model3d-workflow is missing", errors)
        self.assertIn("engines/model3d-workflow/requirements.txt: GitHub Actions target test-model3d-workflow is missing", errors)
        makefile.write_text(makefile.read_text().replace("test: test-sample", "test: test-sample test-model3d-workflow"))
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        workflow.write_text(workflow.read_text() + "  model3d-tests:\n    steps:\n      - run: python3 scripts/ci/select-module-gate.py --module engines\n      - uses: ./.github/actions/prepare-python-gate\n      - run: make test-model3d-workflow\n")
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_rejects_missing_root_and_workflow_registration(self) -> None:
        (self.repository / "Makefile").write_text(
            "test:\n\ntest-sample:\n\t@true\n", encoding="utf-8"
        )
        (self.repository / ".github/workflows/platform-ci.yml").write_text(
            "jobs: {}\n", encoding="utf-8"
        )
        errors = MODULE.validate_registration(self.repository)
        self.assertIn(
            "sample/backend/requirements.txt: root test dependency test-sample is missing",
            errors,
        )
        self.assertIn(
            "sample/backend/requirements.txt: GitHub Actions target test-sample is missing",
            errors,
        )

    def test_rejects_gate_without_standard_environment_setup(self) -> None:
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        workflow.write_text(
            "jobs:\n"
            "  sample-tests:\n"
            "    steps:\n"
            "      - run: python3 scripts/ci/select-module-gate.py --module sample\n"
            "      - run: make test-sample\n",
            encoding="utf-8",
        )
        self.assertIn(
            "sample/backend/requirements.txt: Python gate setup action is missing from test-sample job",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_shared_selector_registered_in_another_job(self) -> None:
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        workflow.write_text(
            "jobs:\n"
            "  sample-tests:\n"
            "    steps:\n"
            "      - run: true\n"
            "      - uses: ./.github/actions/prepare-python-gate\n"
            "      - run: make test-sample\n"
            "  unrelated:\n"
            "    steps:\n"
            "      - run: python3 scripts/ci/select-module-gate.py --module sample\n",
            encoding="utf-8",
        )
        self.assertIn(
            "sample/backend/requirements.txt: shared module change selector is missing",
            MODULE.validate_registration(self.repository),
        )


if __name__ == "__main__":
    unittest.main()
