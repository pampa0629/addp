#!/usr/bin/env python3

from __future__ import annotations

import ast
import re
import shlex
import importlib.util
import json
import os
import signal
import time
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("check-build-registration.py")
SPEC = importlib.util.spec_from_file_location("build_registration", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class BuildRegistrationTest(unittest.TestCase):
    def test_agent_image_cache_observes_skill_changes_and_removals(self):
        script = (SCRIPT.parents[2] / "scripts/build/build-images.sh").read_text(encoding="utf-8")
        function = "check_service_changed() {" + script.split("check_service_changed() {", 1)[1].split("\n}\n", 1)[0] + "\n}\n"
        self._write("agent/backend/main.py", "# fixture\n")
        self._write("dist/release-linux-arm64/runtime-log", "fixture\n")
        self._write("skills/fixture/SKILL.md", "fixture\n")
        self._write(".build-cache/agent-backend-latest.timestamp", "200\n")
        self._write("fake-bin/stat", "#!/usr/bin/env python3\nimport os, sys\nfor path in sys.argv[3:]:\n print(int(os.stat(path).st_mtime))\n")
        (self.repository / "fake-bin/stat").chmod(0o755)
        for path in [self.repository / "skills", *list((self.repository / "skills").rglob("*")), self.repository / "agent/backend/main.py", self.repository / "dist/release-linux-arm64/runtime-log"]:
            os.utime(path, (100, 100))
        command = (
            'REGISTRY=fixture; IMAGE_TAG=latest; BUILD_PLATFORMS=linux/arm64; '
            'curl() { echo \'{"tags":["latest"]}\'; }; '
            'common_python_latest_time() { echo 100; }; '
            + function + '\ncheck_service_changed agent-backend agent/backend'
        )
        environment = dict(os.environ, PATH=f"{self.repository / 'fake-bin'}:{os.environ['PATH']}")

        def check():
            return subprocess.run(["bash", "-c", command], cwd=self.repository, env=environment, capture_output=True).returncode

        self.assertEqual(check(), 0)
        skill = self.repository / "skills/fixture/SKILL.md"
        os.utime(skill, (300, 300))
        self.assertEqual(check(), 1)
        skill.unlink()
        os.utime(skill.parent, (300, 300))
        self.assertEqual(check(), 1)

    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.repository = Path(self.temporary_directory.name)
        subprocess.run(["git", "init", "-q"], cwd=self.repository, check=True)
        self._write("sample/backend/cmd/server/main.go", "package main\n")
        self._write(
            "sample/backend/Dockerfile.prebuilt",
            "FROM scratch\n"
            "ARG BUILD_TYPE=release\nARG GOOS=linux\nARG BUILD_ARCH=amd64\n"
            "COPY dist/${BUILD_TYPE}-${GOOS}-${BUILD_ARCH}/sample ./server\n",
        )
        self._write(".node-version", "24\n")
        self._write("sample/frontend/package.json", "{}\n")
        self._write("sample/frontend/package-lock.json", json.dumps({"packages": {
            "node_modules/@rollup/rollup-linux-arm64-musl": {},
            "node_modules/@rollup/rollup-linux-x64-musl": {},
        }}))
        self._write("sample/frontend/Dockerfile", self.frontend_definition())
        for dockerfile in MODULE.AUXILIARY_DOCKERFILES:
            self._write(dockerfile, "FROM scratch\n")
        self._write(
            "scripts/build/compile.sh",
            'SERVICES=(\n    "sample-backend:sample/backend"\n)\n',
        )
        self._write(
            "scripts/build/build-images.sh",
            'ADDP_CI_SUMMARY_FILE="${ADDP_CI_SUMMARY_FILE:-}"\n'
            'seed_base_images() {\n    local base_images=(\n'
            '        "python:3.12-slim"\n'
            '        "node:${NODE_VERSION}-alpine"\n'
            "    )\n}\n\n"
            'main() {\n    local services=(\n'
            '        "sample-backend:sample/backend"\n'
            '        "sample-frontend:sample/frontend"\n'
            "    )\n}\n",
        )
        self._write("scripts/ci/select-image-services.py", "print('sample-backend')\n")
        self._write("scripts/ci/check-release-eligibility.py", "print('eligible')\n")
        self._write("scripts/ci/update-cli-version.py", "print('updated')\n")
        self._write("scripts/ci/update-cli-version_test.py", "print('tested')\n")
        self._write("scripts/test/dev-lifecycle-and-build.sh", "#!/bin/bash\n")
        self._write("scripts/test/schema-ownership-gates_test.py", "# ownership regression fixture\n")
        self._write("scripts/test/check-execution-test-fixtures.sh", "#!/bin/bash\n")
        self._write("scripts/test/check-protection-projection-store-ownership.sh", "#!/bin/bash\n")
        self._write(
            "docker-compose.yml",
            "services:\n  sample:\n"
            "    image: ${REGISTRY:-localhost:5001}/addp-sample-backend:${IMAGE_TAG:-latest}\n",
        )
        self._write("docker-compose.runtimes.yml", "services: {}\n")
        self._write(
            "Makefile",
            "build:\n\t@bash scripts/build/compile.sh $(BUILD_ARGS)\n\n"
            "build-images:\n\t@bash scripts/build/build-images.sh $(IMAGE_BUILD_ARGS)\n\n"
            "select-image-services:\n\t@python3 scripts/ci/select-image-services.py\n\n"
            "prepare-cli-release:\n\t@python3 scripts/ci/update-cli-version.py\n\n"
            "check-cli-release:\n\t@python3 scripts/ci/check-release-eligibility.py --pre-tag\n\n"
            "test-platform:\n\t@python3 scripts/ci/update-cli-version_test.py\n"
            "\t@$(MAKE) test-dev-lifecycle\n"
            "\t@$(MAKE) test-execution-fixtures\n\t@$(MAKE) test-projection-store-ownership\n\n"
            "test-execution-fixtures:\n"
            "\t@python3 scripts/test/schema-ownership-gates_test.py ExecutionFixtureGateTest\n"
            "\t@bash scripts/test/check-execution-test-fixtures.sh\n\n"
            "test-projection-store-ownership:\n"
            "\t@python3 scripts/test/schema-ownership-gates_test.py ProjectionStoreGateTest\n"
            "\t@bash scripts/test/check-protection-projection-store-ownership.sh\n\n"
            "test-dev-lifecycle:\n\t@bash scripts/test/dev-lifecycle-and-build.sh\n\n"
            "test-go:\n\t@echo $${ADDP_CI_SUMMARY_FILE:-}; GOWORK=off go mod tidy -diff\n",
        )
        self._write(
            ".github/workflows/platform-ci.yml",
            "jobs:\n  platform-consistency:\n    steps:\n"
            "      - run: |\n          sudo apt-get install -y --no-install-recommends ripgrep\n"
            "      - run: make test-platform\n"
            "  go-tests:\n    steps:\n"
            "      - run: make test-go\n        env:\n          ADDP_CI_SUMMARY_FILE: go.md\n"
            "      - uses: ./.github/actions/ci-gate-summary\n        with:\n          details-file: go.md\n"
            "  product-build:\n    steps:\n"
            "      - uses: actions/checkout@sha\n        with:\n          fetch-depth: 0\n"
            "      - run: make build BUILD_ARGS=--force\n"
            "      - run: echo \"services=$(make --no-print-directory select-image-services)\"\n"
            "      - run: make registry-start\n"
            "      - run: make build-images IMAGE_BUILD_ARGS=\"--verify --services $IMAGE_SERVICES\"\n"
            "        env:\n          ADDP_CI_SUMMARY_FILE: images.md\n"
            "      - uses: ./.github/actions/ci-gate-summary\n        with:\n          details-file: images.md\n",
        )
        self._write(
            ".github/workflows/release-and-t2-gates.yml",
            "jobs:\n"
            "  cli-release-eligibility:\n"
            "    permissions:\n      actions: read\n"
            "    steps:\n"
            "      - uses: actions/checkout@sha\n        with:\n          fetch-depth: 0\n"
            "      - run: python3 scripts/ci/check-release-eligibility.py\n"
            "  release-cli:\n"
            "    needs:\n      - cli-release-eligibility\n"
            "    steps:\n"
            "      - uses: actions/attest@sha\n"
            "      - run: gh release create v1 artifact\n",
        )
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)

    @staticmethod
    def frontend_definition() -> str:
        return ("ARG NODE_VERSION\n"
                "FROM localhost:5001/node:${NODE_VERSION}-alpine AS builder\n"
                "COPY sample/frontend/package.json sample/frontend/package-lock.json ./\n"
                "RUN npm ci\n")

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def _write(self, relative_path: str, content: str) -> None:
        path = self.repository / relative_path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")

    def test_cmake_tests_require_root_and_ci_registration(self):
        self._write("engines/native/CMakeLists.txt", "project(native LANGUAGES CXX)\n")
        errors = MODULE.validate_registration(self.repository)
        self.assertIn("engines/native/CMakeLists.txt: Makefile target test-native is missing", errors)
        self.assertIn("engines/native/CMakeLists.txt: root test dependency test-native is missing", errors)
        self.assertIn("engines/native/CMakeLists.txt: Platform CI target test-native is missing", errors)
        makefile = self.repository / "Makefile"
        makefile.write_text(makefile.read_text() + "\ntest: test-native\ntest-native:\n\t@true\n")
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        workflow.write_text(workflow.read_text() + "  native-tests:\n    steps:\n      - name: Native unit tests\n        run: make test-native\n")
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_rejects_frontend_hardcoded_node_version(self):
        self._write("sample/frontend/Dockerfile", self.frontend_definition().replace("${NODE_VERSION}","18"))
        self.assertTrue(any("root NODE_VERSION" in error for error in MODULE.validate_registration(self.repository)))

    def test_rejects_frontend_unlocked_install_after_ci(self):
        self._write("sample/frontend/Dockerfile", self.frontend_definition()+"RUN npm install --no-save extra\n")
        self.assertIn("sample-frontend: frontend image must install exclusively through npm ci", MODULE.validate_registration(self.repository))

    def test_rejects_missing_lockfile_copy_before_install(self):
        self._write("sample/frontend/Dockerfile",self.frontend_definition().replace(" sample/frontend/package-lock.json", ""))
        self.assertIn("sample-frontend: frontend image must COPY package-lock.json before npm ci", MODULE.validate_registration(self.repository))

    def test_rejects_copying_host_frontend_dependencies(self):
        self._write("sample/frontend/Dockerfile",self.frontend_definition()+"COPY sample/frontend .\n")
        self.assertIn("sample-frontend: frontend image must COPY source inputs without host node_modules",MODULE.validate_registration(self.repository))

    def test_rejects_missing_locked_musl_platform_package(self):
        self._write("sample/frontend/package-lock.json",json.dumps({"packages": {}}))
        self.assertIn("sample-frontend: lockfile must include the Rollup linux-x64-musl package",MODULE.validate_registration(self.repository))

    def test_accepts_complete_registration(self) -> None:
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_rejects_missing_compile_and_image_registration(self) -> None:
        self._write("extra/backend/cmd/server/main.go", "package main\n")
        self._write("extra/frontend/package.json", "{}\n")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        errors = MODULE.validate_registration(self.repository)
        self.assertIn(
            "extra-backend: scripts/build/compile.sh registration is missing",
            errors,
        )
        self.assertIn(
            "extra/frontend/package.json: image registration extra-frontend is missing",
            errors,
        )

    def test_rejects_missing_product_build_ci_gate(self) -> None:
        self._write(".github/workflows/platform-ci.yml", "jobs: {}\n")
        self.assertIn(
            "Platform CI product-build job is missing",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_release_without_eligibility_gate(self) -> None:
        workflow = self.repository / ".github/workflows/release-and-t2-gates.yml"
        workflow.write_text(
            "jobs:\n  release-cli:\n    steps:\n"
            "      - uses: actions/attest@sha\n"
            "      - run: gh release create v1 artifact\n",
            encoding="utf-8",
        )
        errors = MODULE.validate_registration(self.repository)
        self.assertIn("CLI release eligibility job is missing", errors)
        self.assertIn("CLI GitHub Release must require release eligibility", errors)

    def test_rejects_missing_local_pre_tag_gate(self) -> None:
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8").replace(
                "check-cli-release:\n\t@python3 scripts/ci/check-release-eligibility.py --pre-tag\n\n",
                "",
            ),
            encoding="utf-8",
        )
        self.assertIn(
            "Makefile target check-cli-release is missing",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_missing_version_prepare_gate(self) -> None:
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8").replace(
                "prepare-cli-release:\n\t@python3 scripts/ci/update-cli-version.py\n\n",
                "",
            ),
            encoding="utf-8",
        )
        self.assertIn(
            "Makefile target prepare-cli-release is missing",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_missing_version_updater_test_registration(self) -> None:
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8").replace(
                "\t@python3 scripts/ci/update-cli-version_test.py\n",
                "",
            ),
            encoding="utf-8",
        )
        self.assertIn(
            "Makefile test-platform must run the CLI version updater tests",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_missing_lifecycle_gate_registration(self) -> None:
        makefile = self.repository / "Makefile"
        original = makefile.read_text(encoding="utf-8")
        for removed, expected in (
            ("\t@$(MAKE) test-dev-lifecycle\n",
             "Makefile test-platform must run test-dev-lifecycle"),
            ("\t@bash scripts/test/dev-lifecycle-and-build.sh\n",
             "Makefile test-dev-lifecycle must run the dev lifecycle tests"),
        ):
            with self.subTest(removed=removed):
                makefile.write_text(original.replace(removed, ""), encoding="utf-8")
                self.assertIn(expected, MODULE.validate_registration(self.repository))

    def test_ownership_regressions_cannot_be_removed_from_standard_gates(self) -> None:
        makefile = self.repository / "Makefile"
        original = makefile.read_text()
        for target, test_class in (
            ("test-execution-fixtures", "ExecutionFixtureGateTest"),
            ("test-projection-store-ownership", "ProjectionStoreGateTest"),
        ):
            for removed, expected in (
                (f"\t@$(MAKE) {target}\n", f"Makefile test-platform must run {target}"),
                (f"\t@python3 scripts/test/schema-ownership-gates_test.py {test_class}\n",
                 f"Makefile {target} must run its regression tests and repository check"),
            ):
                with self.subTest(removed=removed):
                    makefile.write_text(original.replace(removed, ""))
                    self.assertIn(expected, MODULE.validate_registration(self.repository))
        makefile.write_text(original)
        (self.repository / "scripts/test/schema-ownership-gates_test.py").unlink()
        self.assertIn("scripts/test/schema-ownership-gates_test.py is missing", MODULE.validate_registration(self.repository))

    def test_platform_ripgrep_preparation_is_required_before_the_gate(self) -> None:
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        original = workflow.read_text()
        install = "          sudo apt-get install -y --no-install-recommends ripgrep\n"
        for changed in (
            original.replace(install, ""),
            original.replace(install, "").replace("      - run: make test-platform\n", "      - run: make test-platform\n" + install),
        ):
            with self.subTest(workflow=changed):
                workflow.write_text(changed)
                self.assertIn("Platform CI must install ripgrep before make test-platform", MODULE.validate_registration(self.repository))

    def test_rejects_shallow_product_checkout(self) -> None:
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        workflow.write_text(
            workflow.read_text(encoding="utf-8").replace(
                "          fetch-depth: 0\n", ""
            ),
            encoding="utf-8",
        )
        self.assertIn(
            "Platform CI product build must check out full Git history",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_missing_product_image_verification(self) -> None:
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        workflow.write_text(
            workflow.read_text(encoding="utf-8").replace(
                "      - run: make build-images IMAGE_BUILD_ARGS=\"--verify --services $IMAGE_SERVICES\"\n",
                "",
            ),
            encoding="utf-8",
        )
        self.assertIn(
            "Platform CI must verify selected images through make build-images",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_missing_product_image_selection(self) -> None:
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        workflow.write_text(
            workflow.read_text(encoding="utf-8").replace(
                "      - run: echo \"services=$(make --no-print-directory select-image-services)\"\n",
                "",
            ),
            encoding="utf-8",
        )
        self.assertIn(
            "Platform CI must select baseline and affected product images",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_missing_product_image_diagnostics(self) -> None:
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        workflow.write_text(
            workflow.read_text(encoding="utf-8")
            .replace("          ADDP_CI_SUMMARY_FILE: images.md\n", "")
            .replace("          details-file: images.md\n", ""),
            encoding="utf-8",
        )
        self.assertIn(
            "Platform CI product build must publish image diagnostics",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_missing_go_workspace_diagnostics(self) -> None:
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        workflow.write_text(
            workflow.read_text(encoding="utf-8")
            .replace("          ADDP_CI_SUMMARY_FILE: go.md\n", "")
            .replace("          details-file: go.md\n", ""),
            encoding="utf-8",
        )
        self.assertIn(
            "Platform CI Go workspace gate must publish module diagnostics",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_missing_standard_registry_start(self) -> None:
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        workflow.write_text(
            workflow.read_text(encoding="utf-8").replace(
                "      - run: make registry-start\n", ""
            ),
            encoding="utf-8",
        )
        self.assertIn(
            "Platform CI product build must start the standard local registry",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_go_test_without_module_tidy_gate(self) -> None:
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8").replace(
                "\ntest-go:\n\t@echo $${ADDP_CI_SUMMARY_FILE:-}; GOWORK=off go mod tidy -diff\n", ""
            ),
            encoding="utf-8",
        )
        self.assertIn(
            "Makefile target test-go must reject untidy Go module files",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_retired_make_target(self) -> None:
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8")
            + "\nbuild-release:\n\t@echo old route\n",
            encoding="utf-8",
        )
        self.assertIn(
            "Makefile retired target still exists: build-release",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_retired_lifecycle_target(self) -> None:
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8")
            + "\nup-full:\n\t@docker compose up -d\n",
            encoding="utf-8",
        )
        self.assertIn(
            "Makefile retired target still exists: up-full",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_missing_makefile_script(self) -> None:
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8")
            + "\nmissing-script:\n\t@bash scripts/missing.sh\n",
            encoding="utf-8",
        )
        self.assertIn(
            "Makefile references missing script: scripts/missing.sh",
            MODULE.validate_registration(self.repository),
        )

    def test_makefile_script_references_ignore_nested_script_directories(self) -> None:
        references = MODULE.makefile_script_references(
            "\t@bash scripts/root.sh\n\t@bash -n business/scripts/start.sh\n"
        )

        self.assertEqual({"scripts/root.sh"}, references)

    def test_makefile_script_references_ignore_paths_inside_search_needles(self) -> None:
        references = MODULE.makefile_script_references(
            "\t@grep -Fq 'bash scripts/start.sh -kingbase' business/README.md\n"
            "\t@bash scripts/real.sh\n"
        )

        self.assertEqual({"scripts/real.sh"}, references)

    def test_rejects_module_makefile(self) -> None:
        self._write("sample/Makefile", "build:\n\t@echo duplicate\n")
        subprocess.run(
            ["git", "add", "sample/Makefile"], cwd=self.repository, check=True
        )
        self.assertIn(
            "sample/Makefile: module Makefile duplicates the root build entry point",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_missing_image_build_definition(self) -> None:
        (self.repository / "sample/frontend/Dockerfile").unlink()
        self.assertIn(
            "sample-frontend: image build definition does not exist: "
            "sample/frontend/Dockerfile",
            MODULE.validate_registration(self.repository),
        )

    def test_accepts_untracked_image_build_definition_before_first_commit(self) -> None:
        subprocess.run(
            ["git", "rm", "--cached", "sample/frontend/Dockerfile"],
            cwd=self.repository,
            check=True,
            capture_output=True,
        )
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_rejects_mismatched_compiled_binary(self) -> None:
        self._write(
            "sample/backend/Dockerfile.prebuilt",
            "FROM scratch\n"
            "COPY dist/${BUILD_TYPE}-${GOOS}-${BUILD_ARCH}/wrong ./server\n",
        )
        self.assertIn(
            "sample-backend: sample/backend/Dockerfile.prebuilt does not COPY "
            "compiled binary sample from "
            "dist/${BUILD_TYPE}-${GOOS}-${BUILD_ARCH}",
            MODULE.validate_registration(self.repository),
        )

    def test_python_backends_use_source_dockerfile(self) -> None:
        self.assertEqual(
            ("agent/backend/Dockerfile", None, "."),
            MODULE.image_build_definition("agent-backend", "agent/backend"),
        )
        self.assertEqual(
            ("copilot/Dockerfile", None, "."),
            MODULE.image_build_definition("copilot-backend", "copilot"),
        )

    def test_spark_runtime_uses_shared_python_build_context(self) -> None:
        self.assertEqual(
            (
                "engines/spark-workflow/Dockerfile",
                None,
                ".",
            ),
            MODULE.image_build_definition(
                "spark-workflow-engine", "engines/spark-workflow"
            ),
        )

    def test_rejects_copy_source_missing_from_build_context(self) -> None:
        self._write(
            "sample/frontend/Dockerfile",
            "FROM scratch\nCOPY missing-package.json ./package.json\n",
        )
        self.assertIn(
            "sample-frontend: sample/frontend/Dockerfile:2 COPY source is "
            "missing from build context .: missing-package.json",
            MODULE.validate_registration(self.repository),
        )

    def test_accepts_untracked_copy_source_before_first_commit(self) -> None:
        self._write("sample/frontend/runtime-config.json", "{}\n")
        self._write(
            "sample/frontend/Dockerfile",
            self.frontend_definition() + "COPY sample/frontend/runtime-config.json ./runtime-config.json\n",
        )
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_shared_launch_utility_must_compile_and_ship_with_application(self):
        self._write("common/cmd/runtime-log/main.go", "package main\n")
        self.assertIn("runtime-log: scripts/build/compile.sh registration is missing", MODULE.validate_registration(self.repository))
        self._write("scripts/build/compile.sh", 'SERVICES=(\n    "sample-backend:sample/backend"\n    "runtime-log:common"\n)\n')
        self.assertIn("sample-backend: sample/backend/Dockerfile.prebuilt must COPY shared runtime-log utility", MODULE.validate_registration(self.repository))
        path = self.repository / "sample/backend/Dockerfile.prebuilt"
        path.write_text(path.read_text() + "COPY dist/${BUILD_TYPE}-${GOOS}-${BUILD_ARCH}/runtime-log ./runtime-log\n")
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_accepts_untracked_auxiliary_dockerfile_before_first_commit(self) -> None:
        path = next(iter(MODULE.AUXILIARY_DOCKERFILES))
        subprocess.run(
            ["git", "rm", "--cached", path],
            cwd=self.repository,
            check=True,
            capture_output=True,
        )
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_rejects_copy_source_excluded_by_dockerignore(self) -> None:
        self._write(".dockerignore", "**/package.json\n")
        self._write(
            "sample/frontend/Dockerfile",
            "FROM scratch\nCOPY sample/frontend/package.json ./package.json\n",
        )
        self.assertIn(
            "sample-frontend: sample/frontend/Dockerfile:2 COPY source is "
            "excluded by ./.dockerignore: sample/frontend/package.json",
            MODULE.validate_registration(self.repository),
        )

    def test_dockerignore_negation_restores_copy_source(self) -> None:
        self._write(
            ".dockerignore",
            "**/package.json\n!sample/frontend/package.json\n",
        )
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_seeded_base_images_use_target_alias(self) -> None:
        self.assertEqual(
            {"node:20-alpine", "debian-slim:latest"},
            MODULE.seeded_base_images(
                'local base_images=(\n'
                '    "node:20-alpine"\n'
                '    "debian:bookworm-slim=debian-slim:latest"\n'
                ")\n"
            ),
        )

    def test_rejects_unseeded_local_registry_base_image(self) -> None:
        self._write(
            "sample/frontend/Dockerfile",
            "FROM localhost:5001/node:22-alpine\n",
        )
        self.assertIn(
            "sample-frontend: base image localhost:5001/node:22-alpine is not "
            "registered in seed_base_images",
            MODULE.validate_registration(self.repository),
        )

    def test_accepts_seeded_alias_from_build_argument(self) -> None:
        build_script = self.repository / "scripts/build/build-images.sh"
        build_script.write_text(
            build_script.read_text(encoding="utf-8").replace(
                '        "python:3.12-slim"',
                '        "python:3.12-slim"\n'
                '        "node:22-alpine=custom-node:22"',
            ),
            encoding="utf-8",
        )
        self._write(
            "sample/frontend/Dockerfile",
            self.frontend_definition() +
            "ARG BASE_IMAGE=localhost:5001/custom-node:22\n"
            "FROM ${BASE_IMAGE} AS alias\n"
            "FROM alias\n",
        )
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_rejects_latest_seed_source_and_target(self) -> None:
        build_script = self.repository / "scripts/build/build-images.sh"
        build_script.write_text(
            build_script.read_text(encoding="utf-8").replace(
                '        "python:3.12-slim"',
                '        "python:latest=python-runtime:latest"',
            ),
            encoding="utf-8",
        )
        errors = MODULE.validate_registration(self.repository)
        self.assertIn(
            "seed_base_images source uses floating latest tag: python:latest",
            errors,
        )
        self.assertIn(
            "seed_base_images target uses floating latest tag: python-runtime:latest",
            errors,
        )

    def test_rejects_latest_local_registry_base_image(self) -> None:
        self._write(
            "sample/frontend/Dockerfile",
            "FROM localhost:5001/python:latest\n",
        )
        self.assertIn(
            "sample-frontend: base image localhost:5001/python:latest uses "
            "floating latest tag",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_unclassified_dockerfile(self) -> None:
        self._write("legacy/Dockerfile", "FROM scratch\n")
        subprocess.run(
            ["git", "add", "legacy/Dockerfile"], cwd=self.repository, check=True
        )
        self.assertIn(
            "legacy/Dockerfile: Dockerfile is not registered or classified as auxiliary",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_hardcoded_rollup_architecture(self) -> None:
        self._write(
            "sample/frontend/Dockerfile",
            "FROM scratch\nRUN npm install @rollup/rollup-linux-arm64-musl\n",
        )
        self.assertIn(
            "sample/frontend/Dockerfile: Rollup native package architecture must be selected dynamically",
            MODULE.validate_registration(self.repository),
        )


class ImageBuildRuntimeTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='addp-image-builder-test-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.builder = self.root / 'scripts/build/build-images.sh'
        self.builder.parent.mkdir(parents=True)
        shutil.copyfile(SCRIPT.parents[1] / 'build/build-images.sh', self.builder)
        (self.root / '.node-version').write_text('24\n')
        self.services = ['console', 'system-frontend', 'manager-frontend', 'standard-frontend']
        for module in ('console', 'system', 'manager', 'standard'):
            directory = self.root / module / 'frontend'
            directory.mkdir(parents=True)
            (directory / 'Dockerfile').write_text('ARG NODE_VERSION\nFROM localhost:5001/node:${NODE_VERSION}-alpine\n')
        self.bin = self.root / 'fake-bin'
        self.bin.mkdir()
        executable = self.bin / 'docker'
        executable.write_text('''#!/usr/bin/env python3
import fcntl, json, os, signal, subprocess, sys, time
from pathlib import Path
root=Path(os.environ['FAKE_STATE'])
args=sys.argv[1:]
if Path(sys.argv[0]).name=='curl':
    print('{"tags": []}')
    sys.exit(0)
if args[:2]==['image','inspect']:
    if '--format' in args:
        print(os.environ['FAKE_ARCH'])
        sys.exit(0)
    sys.exit(1)
if not args or args[0]!='build':
    sys.exit(0)
name=args[args.index('--tag')+1].split('/addp-')[1].split(':')[0]
def event(kind):
    with (root/'lock').open('a') as lock:
        fcntl.flock(lock,fcntl.LOCK_EX)
        with (root/'events').open('a') as output:
            output.write(json.dumps({'kind':kind,'name':name,'pid':os.getpid(),'group':os.getpgrp(),'args':args})+'\\n')
def interrupted(signum,frame):
    raise SystemExit(128+signum)
signal.signal(signal.SIGTERM,interrupted)
signal.signal(signal.SIGINT,interrupted)
event('start')
child=None
try:
    print(name+' log start',flush=True)
    if os.environ.get('FAKE_BLOCK')=='1':
        child=subprocess.Popen([sys.executable,'-c','import time; time.sleep(60)'])
        event('child')
        time.sleep(60)
    else:
        time.sleep(0.2)
    print(name+' log end',flush=True)
finally:
    if child:
        child.wait(timeout=10)
    event('end')
sys.exit(9 if name==os.environ.get('FAKE_FAILURE') else 0)
''')
        executable.chmod(0o755)
        shutil.copyfile(executable, self.bin / 'curl')
        (self.bin / 'curl').chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.bin)+os.pathsep+os.environ['PATH'],
                        FAKE_STATE=str(self.root), FAKE_ARCH='arm64' if os.uname().machine in ('arm64','aarch64') else 'amd64',
                        TMPDIR=str(self.root), ADDP_CI_SUMMARY_FILE=str(self.root/'summary'))

    def run_builder(self, *args, **extra):
        return subprocess.run(['bash',str(self.builder),'--verify','--services',','.join(self.services),*args],
                              env=dict(self.env,**extra),text=True,capture_output=True,timeout=20)

    def events(self):
        path = self.root/'events'
        return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []

    def peak(self):
        active = maximum = 0
        for event in self.events():
            if event['kind']=='start': active+=1
            elif event['kind']=='end': active-=1
            maximum=max(maximum,active)
        self.assertEqual(0,active)
        return maximum

    def test_default_two_workers_use_root_node_version_and_isolated_logs(self):
        (self.root/'.node-version').write_text('26\n')
        result=self.run_builder()
        self.assertEqual(0,result.returncode,result.stdout+result.stderr)
        self.assertEqual(2,self.peak())
        starts=[event for event in self.events() if event['kind']=='start']
        self.assertEqual(set(self.services),{event['name'] for event in starts})
        for event in starts: self.assertIn('NODE_VERSION=26',event['args'])
        markers=[line for line in result.stdout.splitlines() if ' log ' in line]
        for index in range(0,len(markers),2):
            self.assertEqual(markers[index].replace('start','end'),markers[index+1])
        self.assertIn('duration=',(self.root/'summary').read_text())
        self.assertEqual([],list(self.root.glob('addp-image-build.*')))

    def test_single_worker_uses_same_scheduler(self):
        result=self.run_builder('--jobs','1')
        self.assertEqual(0,result.returncode,result.stdout+result.stderr)
        self.assertEqual(1,self.peak())

    def test_failure_is_reported_and_other_images_finish(self):
        result=self.run_builder(FAKE_FAILURE='manager-frontend')
        self.assertNotEqual(0,result.returncode)
        self.assertEqual(set(self.services),{event['name'] for event in self.events() if event['kind']=='end'})
        self.assertIn('- Failed: 1 service(s)\n  - manager-frontend',(self.root/'summary').read_text())
        self.assertEqual(2,self.peak())
        self.assertEqual([],list(self.root.glob('addp-image-build.*')))

    def test_rejects_more_than_two_workers_before_docker(self):
        result=self.run_builder('--jobs','3')
        self.assertNotEqual(0,result.returncode)
        self.assertEqual([],self.events())

    def test_interrupt_stops_worker_process_groups_and_cleans_logs(self):
        process=subprocess.Popen(['bash',str(self.builder),'--verify','--services',','.join(self.services)],
                                 env=dict(self.env,FAKE_BLOCK='1'),stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True)
        try:
            deadline=time.monotonic()+10
            while time.monotonic()<deadline:
                if sum(event['kind']=='child' for event in self.events())==2: break
                time.sleep(0.02)
            else: self.fail('Both workers did not reach the blocking build')
            process.send_signal(signal.SIGTERM)
            output,_=process.communicate(timeout=15)
            self.assertEqual(143,process.returncode,output)
            self.assertEqual(2,self.peak())
            for event in self.events():
                if event['kind']=='start':
                    with self.assertRaises(ProcessLookupError): os.killpg(event['group'],0)
            self.assertEqual([],list(self.root.glob('addp-image-build.*')))
        finally:
            if process.poll() is None:
                process.kill();process.communicate()
            for event in self.events():
                if event['kind']=='start':
                    try: os.killpg(event['group'],signal.SIGKILL)
                    except ProcessLookupError: pass



class GeoPythonPackagedRuntimeTest(unittest.TestCase):
    def test_image_copies_every_imported_local_module(self):
        root = SCRIPT.parents[2]
        engine = root / "engines/geopython-workflow"
        sources = []
        for line in (engine / "Dockerfile").read_text().splitlines():
            if line.startswith("COPY "):
                sources.extend(shlex.split(line)[1:-1])

        def copied(path):
            relative = path.relative_to(root).as_posix()
            return any(relative == source or (source.endswith("/") and relative.startswith(source)) for source in sources)

        api = engine / "api_server.py"
        self.assertTrue(copied(api))
        for path in engine.rglob("*.py"):
            if not copied(path):
                continue
            for node in ast.walk(ast.parse(path.read_text(), filename=str(path))):
                imports = []
                if isinstance(node, ast.Import):
                    imports = [alias.name for alias in node.names]
                elif isinstance(node, ast.ImportFrom) and node.level == 0 and node.module:
                    imports = [node.module]
                for name in imports:
                    module = engine.joinpath(*name.split("."))
                    local = module.with_suffix(".py")
                    if not local.is_file():
                        local = module / "__init__.py"
                    if local.is_file():
                        self.assertTrue(copied(local), f"{path.name} imports {name}, missing from the product image")

    def test_development_has_one_native_entry_and_no_image_builder(self):
        root = SCRIPT.parents[2]
        for relative in ('scripts/dev/start.sh', 'scripts/dev/restart.sh'):
            source = (root / relative).read_text()
            self.assertIn('source "${SCRIPT_DIR}/geopython-workflow.sh"', source)
            self.assertNotIn('geopython_workflow_source_fingerprint', source)
            self.assertNotIn('ensure_geopython_workflow_image', source)
            self.assertNotIn('--name geopython-workflow-engine', source)
        entry = (root / 'engines/geopython-workflow/container_entrypoint.sh').read_text()
        self.assertIn('exec python api_server.py', entry)


if __name__ == "__main__":
    unittest.main()
