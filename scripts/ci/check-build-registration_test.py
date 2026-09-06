#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
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
        self._write("sample/frontend/package.json", "{}\n")
        self._write("sample/frontend/Dockerfile", "FROM scratch\n")
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
        self._write(
            "docker-compose.yml",
            "services:\n  sample:\n"
            "    image: ${REGISTRY:-localhost:5001}/addp-sample-backend:${IMAGE_TAG:-latest}\n",
        )
        self._write(
            "Makefile",
            "build:\n\t@bash scripts/build/compile.sh $(BUILD_ARGS)\n\n"
            "build-images:\n\t@bash scripts/build/build-images.sh $(IMAGE_BUILD_ARGS)\n\n"
            "select-image-services:\n\t@python3 scripts/ci/select-image-services.py\n\n"
            "prepare-cli-release:\n\t@python3 scripts/ci/update-cli-version.py\n\n"
            "check-cli-release:\n\t@python3 scripts/ci/check-release-eligibility.py --pre-tag\n\n"
            "test-platform:\n\t@python3 scripts/ci/update-cli-version_test.py\n\n"
            "test-go:\n\t@echo $${ADDP_CI_SUMMARY_FILE:-}; GOWORK=off go mod tidy -diff\n",
        )
        self._write(
            ".github/workflows/platform-ci.yml",
            "jobs:\n  go-tests:\n    steps:\n"
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

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def _write(self, relative_path: str, content: str) -> None:
        path = self.repository / relative_path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")

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
                "test-platform:\n\t@python3 scripts/ci/update-cli-version_test.py\n\n",
                "test-platform:\n\t@true\n\n",
            ),
            encoding="utf-8",
        )
        self.assertIn(
            "Makefile test-platform must run the CLI version updater tests",
            MODULE.validate_registration(self.repository),
        )

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
            "@bash scripts/root.sh\n@bash -n business/scripts/start.sh\n"
        )

        self.assertEqual({"scripts/root.sh"}, references)

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

    def test_module_local_image_uses_module_build_context(self) -> None:
        self.assertEqual(
            (
                "engines/spark-workflow/Dockerfile",
                None,
                "engines/spark-workflow",
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
            "FROM scratch\nCOPY sample/frontend/runtime-config.json ./runtime-config.json\n",
        )
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
            "ARG BASE_IMAGE=localhost:5001/custom-node:22\n"
            "FROM ${BASE_IMAGE} AS builder\n"
            "FROM builder\n",
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


if __name__ == "__main__":
    unittest.main()
