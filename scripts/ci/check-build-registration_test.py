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
        self._write(
            "scripts/build/compile.sh",
            'SERVICES=(\n    "sample-backend:sample/backend"\n)\n',
        )
        self._write(
            "scripts/build/build-images.sh",
            'seed_base_images() {\n    local base_images=(\n'
            '        "python:3.12-slim"\n'
            "    )\n}\n\n"
            'main() {\n    local services=(\n'
            '        "sample-backend:sample/backend"\n'
            '        "sample-frontend:sample/frontend"\n'
            "    )\n}\n",
        )
        self._write(
            "docker-compose.yml",
            "services:\n  sample:\n"
            "    image: ${REGISTRY:-localhost:5001}/addp-sample-backend:${IMAGE_TAG:-latest}\n",
        )
        self._write(
            "Makefile",
            "build:\n\t@bash scripts/build/compile.sh $(BUILD_ARGS)\n\n"
            "build-images:\n\t@bash scripts/build/build-images.sh $(IMAGE_BUILD_ARGS)\n",
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

    def test_rejects_retired_make_target(self) -> None:
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8")
            + "\nbuild-release:\n\t@echo old route\n",
            encoding="utf-8",
        )
        self.assertIn(
            "Makefile retired build target still exists: build-release",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_missing_image_build_definition(self) -> None:
        (self.repository / "sample/frontend/Dockerfile").unlink()
        self.assertIn(
            "sample-frontend: image build definition does not exist: "
            "sample/frontend/Dockerfile",
            MODULE.validate_registration(self.repository),
        )

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


if __name__ == "__main__":
    unittest.main()
