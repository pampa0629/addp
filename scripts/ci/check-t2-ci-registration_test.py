#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import fcntl
import json
import os
import re
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("check-t2-ci-registration.py")
SPEC = importlib.util.spec_from_file_location("t2_ci_registration", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class T2CIRegistrationTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.repository = Path(self.temporary_directory.name)
        subprocess.run(["git", "init", "-q"], cwd=self.repository, check=True)
        script = self.repository / "scripts/test/sample-postgres-gate.sh"
        script.parent.mkdir(parents=True)
        script.write_text(
            "#!/usr/bin/env bash\n"
            "# ADDP_T2_SERVICES=postgres\n"
            "# ADDP_T2_REQUIRED_ENV=SAMPLE_POSTGRES_TEST_DSN\n",
            encoding="utf-8",
        )
        (self.repository / "Makefile").write_text(
            "test-integration:\n"
            "\t@$(MAKE) test-sample-postgres\n\n"
            "test-sample-postgres:\n"
            "\t@bash scripts/test/sample-postgres-gate.sh\n",
            encoding="utf-8",
        )
        workflow_directory = self.repository / ".github/workflows"
        workflow_directory.mkdir(parents=True)
        self.workflow = workflow_directory / "release-and-t2-gates.yml"
        self.workflow.write_text(self._workflow_text(), encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def _workflow_text(self) -> str:
        return (
            "jobs:\n"
            "  sample:\n"
            "    services:\n"
            "      postgres:\n"
            "        image: postgres:15@sha256:" + "a" * 64 + "\n"
            "    steps:\n"
            "      - name: Select sample gate\n"
            "        id: sample\n"
            "        run: python3 scripts/ci/select-module-gate.py --module sample\n"
            "      - name: Run sample gate\n"
            "        env:\n"
            "          SAMPLE_POSTGRES_TEST_DSN: postgres://sample-disposable\n"
            "        run: make test-sample-postgres\n"
        )

    def _add_mysql_gate(self, image: str) -> None:
        script = self.repository / "scripts/test/common-mysql-data-protection-gate.sh"
        script.write_text(
            "#!/usr/bin/env bash\n# ADDP_T2_SERVICES=mysql\n",
            encoding="utf-8",
        )
        (self.repository / "Makefile").write_text(
            "test-integration:\n"
            "\t@$(MAKE) test-sample-postgres\n"
            "\t@$(MAKE) test-common-mysql-data-protection\n\n"
            "test-sample-postgres:\n"
            "\t@bash scripts/test/sample-postgres-gate.sh\n\n"
            "test-common-mysql-data-protection:\n"
            "\t@bash scripts/test/common-mysql-data-protection-gate.sh\n",
            encoding="utf-8",
        )
        self.workflow.write_text(
            self._workflow_text()
            + "  common-mysql-data-protection:\n"
            + "    services:\n"
            + "      mysql:\n"
            + f"        image: {image}\n"
            + "    steps:\n"
            + "      - name: Select common gate\n"
            + "        id: common\n"
            + "        run: python3 scripts/ci/select-module-gate.py --module common\n"
            + "      - name: Run MySQL gate\n"
            + "        run: make test-common-mysql-data-protection\n",
            encoding="utf-8",
        )

    def _add_common_postgres_gate(self, image: str) -> None:
        script = self.repository / "scripts/test/common-postgres-gate.sh"
        script.write_text(
            "#!/usr/bin/env bash\n# ADDP_T2_SERVICES=postgres\n",
            encoding="utf-8",
        )
        (self.repository / "Makefile").write_text(
            "test-integration:\n"
            "\t@$(MAKE) test-sample-postgres\n"
            "\t@$(MAKE) test-common-postgres\n\n"
            "test-sample-postgres:\n"
            "\t@bash scripts/test/sample-postgres-gate.sh\n\n"
            "test-common-postgres:\n"
            "\t@bash scripts/test/common-postgres-gate.sh\n",
            encoding="utf-8",
        )
        self.workflow.write_text(
            self._workflow_text()
            + "  common-postgres:\n"
            + "    services:\n"
            + "      postgres:\n"
            + f"        image: {image}\n"
            + "    steps:\n"
            + "      - name: Select common gate\n"
            + "        id: common\n"
            + "        run: python3 scripts/ci/select-module-gate.py --module common\n"
            + "      - name: Run Common PostgreSQL gate\n"
            + "        run: make test-common-postgres\n",
            encoding="utf-8",
        )

    def _add_hosted_only_gate(self) -> None:
        script = self.repository / "scripts/test/common-opengauss-gate.sh"
        script.write_text(
            "#!/usr/bin/env bash\n"
            "# ADDP_T2_HOSTED_ONLY=opengauss\n"
            "CONTAINER_NAME=addp-opengauss-disposable\n"
            "docker run --name \"$CONTAINER_NAME\" opengauss:6.0.6\n"
            "docker rm --force \"$CONTAINER_NAME\"\n",
            encoding="utf-8",
        )
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8")
            + "\ntest-integration-hosted: test-integration\n"
            + "\t@$(MAKE) test-common-opengauss\n\n"
            + "test-common-opengauss:\n"
            + "\t@bash scripts/test/common-opengauss-gate.sh\n",
            encoding="utf-8",
        )
        self.workflow.write_text(
            self._workflow_text()
            + "  common-opengauss:\n"
            + "    steps:\n"
            + "      - name: Select common gate\n"
            + "        id: common\n"
            + "        run: python3 scripts/ci/select-module-gate.py --module common\n"
            + "      - name: Run openGauss gate\n"
            + "        run: make test-common-opengauss\n",
            encoding="utf-8",
        )

    def _add_owned_service_gate(self, tidb_image: str) -> None:
        script = self.repository / "scripts/test/common-tidb-gate.sh"
        script.write_text(
            "#!/usr/bin/env bash\n"
            "# ADDP_T2_OWNED_SERVICES=tidb-pd,tidb-tikv,tidb\n"
            "# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.tidb-t2.yml\n"
            "database=addp_tidb_disposable\n"
            "docker compose up -d tidb-pd tidb-tikv tidb\n"
            "docker compose down --volumes --remove-orphans\n",
            encoding="utf-8",
        )
        compose = self.repository / "scripts/test/docker-compose.tidb-t2.yml"
        compose.write_text(
            "services:\n"
            "  tidb-pd:\n"
            "    image: pingcap/pd:v8.5.8@sha256:" + "b" * 64 + "\n"
            "  tidb-tikv:\n"
            "    image: pingcap/tikv:v8.5.8@sha256:" + "c" * 64 + "\n"
            "    ulimits:\n"
            "      nofile:\n"
            "        soft: 1000000\n"
            "        hard: 1000000\n"
            "  tidb:\n"
            f"    image: {tidb_image}\n",
            encoding="utf-8",
        )
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8").replace(
                "\t@$(MAKE) test-sample-postgres\n",
                "\t@$(MAKE) test-sample-postgres\n"
                "\t@$(MAKE) test-common-tidb\n",
                1,
            )
            + "\ntest-common-tidb:\n"
            + "\t@bash scripts/test/common-tidb-gate.sh\n",
            encoding="utf-8",
        )
        self.workflow.write_text(
            self._workflow_text()
            + "  common-tidb:\n"
            + "    steps:\n"
            + "      - name: Select common gate\n"
            + "        id: common\n"
            + "        run: python3 scripts/ci/select-module-gate.py --module common\n"
            + "      - name: Run TiDB gate\n"
            + "        run: make test-common-tidb\n",
            encoding="utf-8",
        )

    def _add_owner_managed_gate(self) -> None:
        script = self.repository / "scripts/test/common-kingbase-gate.sh"
        script.write_text(
            "#!/usr/bin/env bash\n"
            "# ADDP_T2_OWNER_MANAGED=kingbase\n"
            "DATABASE_NAME=addp_kingbase_disposable\n"
            "echo \"$ADDP_KINGBASE_LICENSE_SHA256 $KINGBASE_OFFICIAL_MEDIA_SHA256\"\n"
            "docker create --name addp-kingbase-provider-disposable kingbase:v1\n"
            "docker rm --force addp-kingbase-provider-disposable\n",
            encoding="utf-8",
        )
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8")
            + "\ntest-integration-owner-managed:\n"
            + "\t@$(MAKE) test-common-kingbase\n\n"
            + "test-common-kingbase:\n"
            + "\t@bash scripts/test/common-kingbase-gate.sh\n",
            encoding="utf-8",
        )

    def _set_sample_disposable_database_contract(self, database: str) -> None:
        script = self.repository / "scripts/test/sample-postgres-gate.sh"
        script.write_text(
            "#!/usr/bin/env bash\n"
            "# ADDP_T2_SERVICES=postgres\n"
            "case \"$database\" in\n"
            "  addp_test|*disposable*) ;;\n"
            "  *) exit 1 ;;\n"
            "esac\n",
            encoding="utf-8",
        )
        self.workflow.write_text(
            (
                "jobs:\n"
                "  sample:\n"
                "    services:\n"
                "      postgres:\n"
                "        image: postgres:15@sha256:" + "a" * 64 + "\n"
                "        env:\n"
                f"          POSTGRES_DB: {database}\n"
                "        options: >-\n"
                f"          --health-cmd \"pg_isready -U addp_ci -d {database}\"\n"
                "    steps:\n"
                "      - name: Select sample gate\n"
                "        id: sample\n"
                "        run: python3 scripts/ci/select-module-gate.py --module sample\n"
                "      - name: Run sample gate\n"
                "        env:\n"
                f"          SAMPLE_POSTGRES_TEST_DSN: postgres://addp_ci:password@127.0.0.1:5432/{database}?sslmode=disable\n"
                "        run: make test-sample-postgres\n"
            ),
            encoding="utf-8",
        )

    def test_accepts_complete_registration(self) -> None:
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_rejects_running_cancellation_and_pending_replacement(self) -> None:
        for cancellation in ("true", "false"):
            for indentation in ("", "    "):
                with self.subTest(cancel=cancellation, indentation=indentation):
                    concurrency = (
                        f"{indentation}concurrency:\n"
                        f"{indentation}  group: gates-${{{{ github.ref }}}}\n"
                        f"{indentation}  cancel-in-progress: {cancellation}\n"
                    )
                    workflow = self._workflow_text()
                    if indentation:
                        workflow = workflow.replace("  sample:\n", "  sample:\n" + concurrency)
                    else:
                        workflow = concurrency + workflow
                    self.workflow.write_text(workflow, encoding="utf-8")
                    self.assertTrue(any(
                        "preserve running and queued gate evidence" in error
                        for error in MODULE.validate_registration(self.repository)
                    ))

    def test_scheduling_check_discovers_other_path_selected_workflows(self) -> None:
        for selector in ("select-module-gate.py", "select-gate-by-paths.sh", "select-image-services"):
            with self.subTest(selector=selector):
                other = self.workflow.with_name("other.yaml")
                other.write_text(
                    "concurrency: group\njobs:\n  gate:\n    steps:\n"
                    f"      - run: {selector}\n",
                    encoding="utf-8",
                )
                errors = MODULE.validate_registration(self.repository)
                self.assertEqual(1, len(errors))
                self.assertIn("other.yaml", errors[0])

    def test_scheduling_check_preserves_online_resource_locks(self) -> None:
        self.workflow.with_name("online.yaml").write_text(
            "jobs:\n  online:\n    concurrency:\n"
            "      group: dedicated-host\n      cancel-in-progress: false\n"
            "    steps:\n      - run: make test-online ONLINE_SUITE=sample\n",
            encoding="utf-8",
        )
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_repository_path_selected_workflows_preserve_every_run(self) -> None:
        self.assertEqual([], MODULE.validate_path_selected_scheduling(Path(__file__).parents[2]))

    def test_rejects_missing_required_gate_environment_in_workflow(self) -> None:
        self.workflow.write_text(
            self._workflow_text().replace(
                "        env:\n"
                "          SAMPLE_POSTGRES_TEST_DSN: postgres://sample-disposable\n",
                "",
            ),
            encoding="utf-8",
        )

        self.assertIn(
            "scripts/test/sample-postgres-gate.sh: GitHub Actions target "
            "test-sample-postgres does not provide SAMPLE_POSTGRES_TEST_DSN",
            MODULE.validate_registration(self.repository),
        )

    def test_accepts_required_gate_environment_at_job_scope(self) -> None:
        workflow = self._workflow_text().replace(
            "  sample:\n",
            "  sample:\n"
            "    env:\n"
            "      SAMPLE_POSTGRES_TEST_DSN: postgres://sample-disposable\n",
            1,
        ).replace(
            "        env:\n"
            "          SAMPLE_POSTGRES_TEST_DSN: postgres://sample-disposable\n",
            "",
            1,
        )
        self.workflow.write_text(workflow, encoding="utf-8")

        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_rejects_required_gate_environment_only_in_service_scope(self) -> None:
        workflow = self._workflow_text().replace(
            "        image: postgres:15@sha256:" + "a" * 64 + "\n",
            "        image: postgres:15@sha256:"
            + "a" * 64
            + "\n"
            + "        env:\n"
            + "          SAMPLE_POSTGRES_TEST_DSN: postgres://wrong-scope\n",
            1,
        ).replace(
            "        env:\n"
            "          SAMPLE_POSTGRES_TEST_DSN: postgres://sample-disposable\n",
            "",
            1,
        )
        self.workflow.write_text(workflow, encoding="utf-8")

        self.assertIn(
            "scripts/test/sample-postgres-gate.sh: GitHub Actions target "
            "test-sample-postgres does not provide SAMPLE_POSTGRES_TEST_DSN",
            MODULE.validate_registration(self.repository),
        )

    def test_accepts_pinned_mysql_gate(self) -> None:
        self._add_mysql_gate("mysql:8.0@sha256:" + "b" * 64)
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_accepts_common_postgis_gate(self) -> None:
        self._add_common_postgres_gate(
            "postgis/postgis:15-3.4@sha256:" + "b" * 64
        )
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_accepts_new_database_service_without_checker_code_change(self) -> None:
        script = self.repository / "scripts/test/common-oceanbase-gate.sh"
        script.write_text(
            "#!/usr/bin/env bash\n# ADDP_T2_SERVICES=oceanbase\n",
            encoding="utf-8",
        )
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8").replace(
                "\t@$(MAKE) test-sample-postgres\n",
                "\t@$(MAKE) test-sample-postgres\n"
                "\t@$(MAKE) test-common-oceanbase\n",
                1,
            )
            + "\ntest-common-oceanbase:\n"
            + "\t@bash scripts/test/common-oceanbase-gate.sh\n",
            encoding="utf-8",
        )
        self.workflow.write_text(
            self._workflow_text()
            + "  common-oceanbase:\n"
            + "    services:\n"
            + "      oceanbase:\n"
            + "        image: oceanbase/oceanbase-ce:4.4.2-lts@sha256:"
            + "c" * 64
            + "\n"
            + "    steps:\n"
            + "      - name: Select common gate\n"
            + "        id: common\n"
            + "        run: python3 scripts/ci/select-module-gate.py --module common\n"
            + "      - name: Run OceanBase gate\n"
            + "        run: make test-common-oceanbase\n",
            encoding="utf-8",
        )

        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_accepts_hosted_only_gate_outside_local_aggregate(self) -> None:
        self._add_hosted_only_gate()

        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_accepts_owned_service_gate(self) -> None:
        self._add_owned_service_gate(
            "pingcap/tidb:v8.5.8@sha256:" + "d" * 64
        )

        self.assertEqual([], MODULE.validate_registration(self.repository))

    def _add_owned_source_build(self) -> Path:
        image = "pingcap/tidb:v8.5.8@sha256:" + "d" * 64
        self._add_owned_service_gate(image)
        compose = self.repository / "scripts/test/docker-compose.tidb-t2.yml"
        compose.write_text(compose.read_text().replace(
            f"    image: {image}",
            "    image: owned-test:build\n    build:\n      context: ./build\n      dockerfile: Dockerfile",
        ))
        script = self.repository / "scripts/test/common-tidb-gate.sh"
        script.write_text(script.read_text().replace(
            "docker compose up", "docker compose build tidb\ndocker compose up",
        ) + "# ADDP_T2_INPUT_FILES=scripts/test/build/Dockerfile\n")
        dockerfile = self.repository / "scripts/test/build/Dockerfile"
        dockerfile.parent.mkdir()
        dockerfile.write_text(
            "FROM golang:1.24@sha256:" + "a" * 64 + " AS build\n"
            "RUN git clone --depth 1 --branch release https://example.test/source.git source && "
            'test "$(git -C source rev-parse HEAD)" = ' + "b" * 40 + " && echo built\n"
            "FROM alpine:3.20@sha256:" + "c" * 64 + "\n"
            "COPY --from=build /output /output\n"
        )
        return dockerfile

    def test_accepts_source_pinned_owned_build(self) -> None:
        self._add_owned_source_build()
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_rejects_unpinned_owned_build_inputs(self) -> None:
        dockerfile = self._add_owned_source_build()
        original = dockerfile.read_text()
        variants = {
            "floating base": original.replace("golang:1.24@sha256:" + "a" * 64, "golang:latest"),
            "missing git check": original.replace('test "$(git -C source rev-parse HEAD)"', 'echo "source"'),
            "invalid git SHA": original.replace("b" * 40, "release"),
            "masked git check": original.replace(" && echo built", " || true"),
            "unverified later checkout": original + "RUN git -C source checkout main\n",
            "unverified git fetch": original + "RUN git fetch https://example.test/other.git main\n",
            "external stage copy": original.replace("--from=build", "--from=unverified:latest"),
            "malformed local copy": original + 'COPY "unterminated /input\n',
            "dynamic base": original.replace("FROM golang:1.24@sha256:" + "a" * 64, "ARG BASE\nFROM ${BASE}"),
        }
        for name, text in variants.items():
            with self.subTest(name=name):
                dockerfile.write_text(text)
                self.assertTrue(MODULE.validate_registration(self.repository))

    def test_rejects_unregistered_build_inputs_and_missing_build_execution(self) -> None:
        dockerfile = self._add_owned_source_build()
        script = self.repository / "scripts/test/common-tidb-gate.sh"
        original = script.read_text()
        script.write_text(original.replace("# ADDP_T2_INPUT_FILES=scripts/test/build/Dockerfile\n", ""))
        self.assertTrue(any("ADDP_T2_INPUT_FILES" in error for error in MODULE.validate_registration(self.repository)))
        script.write_text(original.replace("docker compose build tidb\n", ""))
        self.assertTrue(any("compose build" in error for error in MODULE.validate_registration(self.repository)))
        script.write_text(original)
        (dockerfile.parent / "input.txt").write_text("source")
        dockerfile.write_text(dockerfile.read_text() + "COPY input.txt /input.txt\n")
        self.assertTrue(any("COPY/ADD" in error for error in MODULE.validate_registration(self.repository)))
        script.write_text(original.replace("scripts/test/build/Dockerfile", "scripts/test/build/Dockerfile scripts/test/build/input.txt"))
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_owned_build_directory_input_covers_only_declared_subtree(self) -> None:
        dockerfile = self._add_owned_source_build()
        script = self.repository / "scripts/test/common-tidb-gate.sh"
        (dockerfile.parent / "input.txt").write_text("source")
        dockerfile.write_text(dockerfile.read_text() + "COPY input.txt /input.txt\n")
        script.write_text(script.read_text().replace("scripts/test/build/Dockerfile", "scripts/test/build/"))
        self.assertEqual([], MODULE.validate_registration(self.repository))
        self.assertFalse(MODULE.MODULE_GATE.gate_input_covers(["scripts/test/build/"], "scripts/test/build-other/input.txt"))
        self.assertTrue(MODULE.MODULE_GATE.gate_input_covers(["scripts/test/build/"], "scripts/test/build/deleted.txt"))

    def test_rejects_build_parameter_override_and_external_context(self) -> None:
        self._add_owned_source_build()
        compose = self.repository / "scripts/test/docker-compose.tidb-t2.yml"
        original = compose.read_text()
        compose.write_text(original.replace("      dockerfile: Dockerfile", "      dockerfile: Dockerfile\n      args: override"))
        self.assertTrue(MODULE.validate_registration(self.repository))
        compose.write_text(original.replace("context: ./build", "context: ../../../../outside"))
        self.assertTrue(MODULE.validate_registration(self.repository))

    def test_image_override_cannot_hide_inherited_unpinned_build(self) -> None:
        dockerfile = self._add_owned_source_build()
        compose = self.repository / "scripts/test/docker-compose.tidb-t2.yml"
        base = self.repository / "scripts/test/base-build.yml"
        base.write_text("services:\n  database:\n    build:\n      context: ./build\n      dockerfile: Dockerfile\n")
        compose.write_text(compose.read_text().replace(
            "    image: owned-test:build\n    build:\n      context: ./build\n      dockerfile: Dockerfile",
            "    image: registry/service:v1@sha256:" + "e" * 64 + "\n    extends:\n      file: base-build.yml\n      service: database",
        ))
        self.assertEqual([], MODULE.validate_registration(self.repository))
        dockerfile.write_text(dockerfile.read_text().replace('test "$(git -C source rev-parse HEAD)"', 'echo "unverified"'))
        self.assertTrue(MODULE.validate_registration(self.repository))

    def test_shared_owned_lifecycle_requires_registered_invocation_and_compose(self) -> None:
        self._add_owned_service_gate("pingcap/tidb:v8.5.8@sha256:" + "d" * 64)
        gate = self.repository / "scripts/test/common-tidb-gate.sh"
        helper = self.repository / "scripts/test/owned-fixture.sh"
        helper.write_text(
            "# disposable lifecycle\n"
            "docker compose -f scripts/test/docker-compose.tidb-t2.yml up -d\n"
            "docker compose down --volumes --remove-orphans\n"
        )
        original = gate.read_text()
        declaration = (
            "# ADDP_T2_LIFECYCLE_SCRIPT=scripts/test/owned-fixture.sh\n"
            "# ADDP_T2_INPUT_FILES=scripts/test/owned-fixture.sh\n"
        )
        owner = original.split("database=", 1)[0] + declaration
        invocation = 'bash "$ROOT_DIR/scripts/test/owned-fixture.sh"\n'
        gate.write_text(owner + invocation)
        self.assertEqual([], MODULE.validate_registration(self.repository))
        gate.write_text(owner)
        self.assertTrue(any("shared lifecycle" in error for error in MODULE.validate_registration(self.repository)))
        gate.write_text((owner + invocation).replace("# ADDP_T2_INPUT_FILES=scripts/test/owned-fixture.sh\n", ""))
        self.assertTrue(any("shared lifecycle" in error for error in MODULE.validate_registration(self.repository)))
        gate.write_text(owner + invocation)
        helper.write_text(helper.read_text().replace("scripts/test/docker-compose.tidb-t2.yml", "other.yml"))
        self.assertTrue(any("declared Compose" in error for error in MODULE.validate_registration(self.repository)))
        helper.unlink()
        self.assertTrue(any("shared lifecycle" in error for error in MODULE.validate_registration(self.repository)))

    def test_resolves_owned_compose_image_extends_without_weakening_pin(self) -> None:
        self._add_owned_service_gate("pingcap/tidb:v8.5.8@sha256:" + "d" * 64)
        compose = self.repository / "scripts/test/docker-compose.tidb-t2.yml"
        base = self.repository / "scripts/test/base.yml"
        base.write_text("services:\n  database:\n    image: example/db:v1@sha256:" + "a" * 64 + "\n")
        original = compose.read_text()
        child = original.replace("    image: pingcap/tidb:v8.5.8@sha256:" + "d" * 64,
                                 "    extends:\n      file: base.yml\n      service: database")
        compose.write_text(child)
        self.assertEqual([], MODULE.validate_registration(self.repository))
        compose.write_text(child + "    image: example/db:latest\n")
        self.assertTrue(any("pin an explicit tag" in error for error in MODULE.validate_registration(self.repository)))
        compose.write_text(child)
        for content in (
            "services:\n  database:\n    extends:\n      file: base.yml\n      service: database\n",
            "services:\n  database:\n    extends:\n      file: ../../../../outside.yml\n      service: database\n",
            "services:\n  other:\n    image: example/db:latest\n",
        ):
            base.write_text(content)
            self.assertTrue(any("Compose extends" in error for error in MODULE.validate_registration(self.repository)))

    def test_accepts_owner_managed_gate(self) -> None:
        self._add_owner_managed_gate()

        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_rejects_owner_managed_gate_in_local_aggregate(self) -> None:
        self._add_owner_managed_gate()
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8").replace(
                "test-integration:\n",
                "test-integration:\n\t@$(MAKE) test-common-kingbase\n",
                1,
            ),
            encoding="utf-8",
        )

        self.assertIn(
            "scripts/test/common-kingbase-gate.sh: owner-managed target "
            "test-common-kingbase must not run in test-integration",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_manual_gate_in_any_workflow(self) -> None:
        self._add_owner_managed_gate()
        for command in (
            "make test-common-kingbase",
            "make test-integration-owner-managed",
            "bash scripts/test/common-kingbase-gate.sh",
        ):
            with self.subTest(command=command):
                workflow = self.workflow.parent / "other.yaml"
                workflow.write_text(
                    f"jobs:\n  manual:\n    steps:\n      - run: |\n          {command}\n",
                    encoding="utf-8",
                )
                self.assertTrue(any(
                    "must not run in GitHub Actions" in error
                    for error in MODULE.validate_registration(self.repository)
                ))

    def test_rejects_missing_manual_aggregate_invocation(self) -> None:
        self._add_owner_managed_gate()
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8").replace(
                "\t@$(MAKE) test-common-kingbase\n", "\t@true\n"
            ), encoding="utf-8",
        )
        self.assertTrue(any(
            "test-integration-owner-managed does not invoke" in error
            for error in MODULE.validate_registration(self.repository)
        ))

    def test_rejects_hosted_only_gate_in_local_aggregate(self) -> None:
        self._add_hosted_only_gate()
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8").replace(
                "test-integration:\n",
                "test-integration:\n\t@$(MAKE) test-common-opengauss\n",
                1,
            ),
            encoding="utf-8",
        )

        self.assertIn(
            "scripts/test/common-opengauss-gate.sh: hosted-only target "
            "test-common-opengauss must not run in local test-integration",
            MODULE.validate_registration(self.repository),
        )

    def test_accepts_explicit_disposable_database_contract(self) -> None:
        self._set_sample_disposable_database_contract("addp_sample_disposable")
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_rejects_unpinned_mysql_gate(self) -> None:
        self._add_mysql_gate("mysql:8.0")
        self.assertIn(
            "scripts/test/common-mysql-data-protection-gate.sh: mysql service image "
            "must pin an explicit tag and digest in test-common-mysql-data-protection job",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_unpinned_owned_service_image(self) -> None:
        self._add_owned_service_gate("pingcap/tidb:v8.5.8")

        self.assertIn(
            "scripts/test/common-tidb-gate.sh: tidb image must pin an explicit tag "
            "and digest in scripts/test/docker-compose.tidb-t2.yml",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_insufficient_tidb_tikv_nofile_limit(self) -> None:
        self._add_owned_service_gate(
            "pingcap/tidb:v8.5.8@sha256:" + "d" * 64
        )
        compose = self.repository / "scripts/test/docker-compose.tidb-t2.yml"
        compose.write_text(
            compose.read_text(encoding="utf-8").replace(
                "soft: 1000000", "soft: 65536"
            ),
            encoding="utf-8",
        )

        self.assertIn(
            "scripts/test/common-tidb-gate.sh: tidb-tikv must set nofile soft/hard limits "
            "to at least 1000000 in scripts/test/docker-compose.tidb-t2.yml",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_service_image_without_explicit_tag(self) -> None:
        self._add_common_postgres_gate("postgis/postgis@sha256:" + "b" * 64)
        self.assertIn(
            "scripts/test/common-postgres-gate.sh: postgres service image must pin an "
            "explicit tag and digest in test-common-postgres job",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_missing_declared_service(self) -> None:
        self._add_mysql_gate("mysql:8.0@sha256:" + "b" * 64)
        self.workflow.write_text(
            self.workflow.read_text(encoding="utf-8").replace(
                "      mysql:\n", "      mariadb:\n"
            ),
            encoding="utf-8",
        )
        self.assertIn(
            "scripts/test/common-mysql-data-protection-gate.sh: declared mysql service "
            "is missing in test-common-mysql-data-protection job",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_non_disposable_database_for_strict_gate(self) -> None:
        self._set_sample_disposable_database_contract("addp_sample_test")
        self.assertIn(
            "scripts/test/sample-postgres-gate.sh: hosted PostgreSQL database "
            "addp_sample_test is not explicitly disposable",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_missing_owner_and_target_registration(self) -> None:
        self.workflow.write_text(
            "jobs:\n  sample:\n    services:\n      postgres:\n"
            "        image: postgres:15@sha256:" + "a" * 64 + "\n",
            encoding="utf-8",
        )
        errors = MODULE.validate_registration(self.repository)
        self.assertIn(
            "scripts/test/sample-postgres-gate.sh: GitHub Actions target "
            "test-sample-postgres is missing",
            errors,
        )
        self.assertIn(
            "scripts/test/sample-postgres-gate.sh: shared module change selector is missing",
            errors,
        )

    def test_rejects_selector_registered_in_another_selection_step(self) -> None:
        self.workflow.write_text(
            self._workflow_text().replace(
                "        run: python3 scripts/ci/select-module-gate.py --module sample\n",
                "        run: true\n"
                "      - name: Select unrelated gate\n"
                "        id: unrelated\n"
                "        run: python3 scripts/ci/select-module-gate.py --module sample\n",
            ),
            encoding="utf-8",
        )
        errors = MODULE.validate_registration(self.repository)
        self.assertIn(
            "scripts/test/sample-postgres-gate.sh: shared module change selector is missing",
            errors,
        )

    def test_rejects_unpinned_postgres_in_target_job(self) -> None:
        self.workflow.write_text(
            self._workflow_text().replace("postgres:15@sha256:" + "a" * 64, "postgres:15"),
            encoding="utf-8",
        )
        self.assertIn(
            "scripts/test/sample-postgres-gate.sh: postgres service image must pin an "
            "explicit tag and digest in test-sample-postgres job",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_gate_missing_from_integration_aggregate(self) -> None:
        (self.repository / "Makefile").write_text(
            "test-integration:\n\n"
            "test-sample-postgres:\n"
            "\t@bash scripts/test/sample-postgres-gate.sh\n",
            encoding="utf-8",
        )
        self.assertIn(
            "scripts/test/sample-postgres-gate.sh: root test-integration does not invoke "
            "test-sample-postgres sequentially",
            MODULE.validate_registration(self.repository),
        )

    def test_detects_untracked_gate_before_commit(self) -> None:
        script = self.repository / "scripts/test/new-postgres-gate.sh"
        script.write_text(
            "#!/usr/bin/env bash\n# ADDP_T2_SERVICES=postgres\n",
            encoding="utf-8",
        )
        errors = MODULE.validate_registration(self.repository)
        self.assertIn(
            "scripts/test/new-postgres-gate.sh: Makefile target test-new-postgres is missing",
            errors,
        )


class CommonPostgresExecutionSelectionTest(unittest.TestCase):
    def setUp(self) -> None:
        runner = SCRIPT.parents[1] / "test/common-postgres-gate.sh"
        self.runner_source = runner.read_text(encoding="utf-8")
        commands = re.findall(r"(?m)^\s*local execution_pattern='([^']+)'$", self.runner_source)
        self.assertEqual(len(commands), 1, "expected one full execution PG test selection")
        self.selection = re.compile(commands[0])

    def test_lifecycle_matrix_is_selected_by_the_real_pg_runner(self) -> None:
        self.assertIsNotNone(
            self.selection.fullmatch("TestExecutionLifecycleAgainstPostgres"),
            "the Common PG gate must select the Owner's real lifecycle matrix",
        )

    def test_existing_execution_pg_contracts_remain_selected(self) -> None:
        for contract in (
            "AuthorizationFactsMigration", "Events", "EventRetention", "RetentionAssessment"
        ):
            with self.subTest(contract=contract):
                self.assertIsNotNone(
                    self.selection.fullmatch(f"TestExecution{contract}AgainstPostgres")
                )

    def test_sqlite_units_and_other_test_names_do_not_substitute_for_pg(self) -> None:
        for name in (
            "TestClaimNextUsesAttemptScopedLeaseToken",
            "TestExpiredAttemptResetsStartTimeAndFencesOldLease",
            "TestExecutionLifecycle",
            "TestExecutionLifecycleAgainstPostgresExtra",
            "TestExecutionUnregisteredAgainstPostgres",
        ):
            with self.subTest(name=name):
                self.assertIsNone(self.selection.fullmatch(name))

    def run_gate_fixture(
        self,
        outcome: str,
        group: str = "execution-lifecycle",
        affected_test: str = "TestExecutionLifecycleAgainstPostgres",
    ) -> tuple[subprocess.CompletedProcess, list[list[str]]]:
        # Run the real shell selector with disposable command stubs. No Go
        # process, PostgreSQL connection or shared infrastructure is used.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for directory in (
                "scripts/test", "scripts/infra", "common", "graph/backend", "bin"
            ):
                (root / directory).mkdir(parents=True, exist_ok=True)
            runner = root / "scripts/test/common-postgres-gate.sh"
            runner.write_text(
                self.runner_source.replace(
                    "/tmp/addp-common-postgres-gate.lock", str(root / "fixture.lock")
                ),
                encoding="utf-8",
            )
            (root / "scripts/infra/ports.sh").write_text(
                'addp_infra_verify_test_postgres_target() { return 0; }\n'
                'addp_infra_verify_test_postgres_dsn() { return 0; }\n',
                encoding="utf-8",
            )
            fake_go = root / "bin/go"
            fake_go.write_text(
                f"#!{sys.executable}\n"
                "import json, os, re, sys\n"
                "with open(os.environ['ARGS_TRACE'], 'a') as trace:\n"
                "    trace.write(json.dumps(sys.argv[1:]) + '\\n')\n"
                "args = sys.argv[1:]\n"
                "pattern = args[args.index('-run') + 1]\n"
                "names = [\n"
                "    'TestIntegrationResolvePostgresQueryOutputRewrite',\n"
                "    'TestIntegrationHighGoTableWrite', 'TestIntegrationPostgresKMLWriter',\n"
                "    'TestExecutionAuthorizationFactsMigrationAgainstPostgres',\n"
                "    'TestExecutionEventsAgainstPostgres', 'TestExecutionEventRetentionAgainstPostgres',\n"
                "    'TestExecutionRetentionAssessmentAgainstPostgres', 'TestExecutionLifecycleAgainstPostgres',\n"
                "    'TestPostgresCleanupProfessionalResultAtomicity',\n"
                "    'TestExportExecutionSourceAgainstPostgres',\n"
                "    'TestProjectionStoreSchemaContractAgainstPostgres',\n"
                "    'TestTableResultAgainstPostgres', 'TestIntegrationPostgresGraphTargetContract',\n"
                "    'TestIntegrationPostgresGraphCleanupRuntime',\n"
                "]\n"
                "mode = os.environ['FAKE_PG_OUTCOME']\n"
                "failed = False\n"
                "for name in names:\n"
                "    if not re.search(pattern, name):\n"
                "        continue\n"
                "    if name == 'TestIntegrationPostgresGraphTargetContract' and args[1] != './internal/repository':\n"
                "        continue\n"
                "    if name == 'TestIntegrationPostgresGraphCleanupRuntime' and args[1] != './internal/service':\n"
                "        continue\n"
                "    if name.startswith('TestIntegrationPostgresGraph'):\n"
                "        assert os.environ['ADDP_POSTGRES_INTEGRATION'] == '1'\n"
                "        assert os.environ['GOWORK'] == 'off'\n"
                "    selected = mode if name == os.environ['FAKE_PG_TEST_NAME'] else 'pass'\n"
                "    if name.startswith('TestExecution') or name == 'TestPostgresCleanupProfessionalResultAtomicity':\n"
                "        assert os.environ['ADDP_TEST_EXECUTION_POSTGRES_DSN'] == 'fixture-dsn'\n"
                "    if selected == 'missing':\n"
                "        print('testing: warning: no matching test to run\\nPASS')\n"
                "        continue\n"
                "    if selected in ['suffix', 'child']:\n"
                "        extra = 'Extra' if selected == 'suffix' else '/child'\n"
                "        print(f'--- PASS: {name}{extra} (0.00s)')\n"
                "        continue\n"
                "    if selected == 'child-skip':\n"
                "        print(f'    --- SKIP: {name}/child (0.00s)')\n"
                "        selected = 'pass'\n"
                "    status = {'pass': 'PASS', 'skip': 'SKIP', 'fail': 'FAIL'}[selected]\n"
                "    print(f'--- {status}: {name} (0.00s)')\n"
                "    failed |= selected == 'fail'\n"
                "sys.exit(int(failed))\n",
                encoding="utf-8",
            )
            fake_go.chmod(0o700)
            trace = root / "go-arguments.json"
            environment = dict(os.environ)
            environment.update(
                PATH=f"{root / 'bin'}{os.pathsep}{environment['PATH']}",
                TMPDIR=str(root),
                ADDP_TEST_POSTGRES_PORT="5432",
                ADDP_TEST_POSTGRES_PASSWORD="fixture-password",
                ADDP_TEST_EXECUTION_POSTGRES_DSN="fixture-dsn",
                ARGS_TRACE=str(trace),
                FAKE_PG_OUTCOME=outcome,
                FAKE_PG_TEST_NAME=affected_test,
            )
            result = subprocess.run(
                ["bash", str(runner), "--test", group],
                env=environment,
                capture_output=True,
                text=True,
                timeout=15,
            )
            self.assertTrue(trace.is_file(), result.stdout + result.stderr)
            return result, [json.loads(line) for line in trace.read_text().splitlines()]

    def test_focused_group_executes_only_the_exact_pg_lifecycle_case(self) -> None:
        result, calls = self.run_gate_fixture("pass")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        expected = [
            "test", "./execution", "-run", "^TestExecutionLifecycleAgainstPostgres$",
            "-count=1", "-v",
        ]
        self.assertEqual(calls, [expected, [
            "test", "./cleanup", "-run", "^TestPostgresCleanupProfessionalResultAtomicity$",
            "-count=1", "-v",
        ]])
        self.assertIn("not the full gate", result.stdout)

    def test_cleanup_atomicity_runs_once_with_the_same_dsn_in_full_and_lifecycle_groups(self) -> None:
        for group in ("all", "execution-lifecycle"):
            with self.subTest(group=group):
                result, calls = self.run_gate_fixture("pass", group)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual([c for c in calls if c[:2] == ["test", "./cleanup"]], [[
                    "test", "./cleanup", "-run", "^TestPostgresCleanupProfessionalResultAtomicity$",
                    "-count=1", "-v",
                ]])

    def test_cleanup_atomicity_requires_exact_main_pass_and_refuses_any_skip(self) -> None:
        name = "TestPostgresCleanupProfessionalResultAtomicity"
        for outcome in ("missing", "suffix", "child", "skip", "child-skip"):
            with self.subTest(outcome=outcome):
                result, _ = self.run_gate_fixture(outcome, affected_test=name)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("Common Cleanup PostgreSQL gate", result.stderr)
                self.assertNotIn("group passed", result.stdout)

    def test_full_gate_requires_cleanup_atomicity(self) -> None:
        result, _ = self.run_gate_fixture(
            "missing", "all", "TestPostgresCleanupProfessionalResultAtomicity"
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Common Cleanup PostgreSQL gate", result.stderr)

    def test_cleanup_atomicity_propagates_owner_test_failure(self) -> None:
        result, _ = self.run_gate_fixture(
            "fail", affected_test="TestPostgresCleanupProfessionalResultAtomicity"
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("group passed", result.stdout)

    def test_graph_and_full_groups_preserve_repository_then_run_exact_cleanup_service(self) -> None:
        for group in ("graph", "all"):
            with self.subTest(group=group):
                result, calls = self.run_gate_fixture("pass", group)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                graph_calls = [c for c in calls if c[1] in ("./internal/repository", "./internal/service")]
                self.assertEqual(graph_calls, [
                    ["test", "./internal/repository", "-run", "^TestIntegrationPostgresGraph", "-count=1", "-v"],
                    ["test", "./internal/service", "-run", "^TestIntegrationPostgresGraphCleanupRuntime$", "-count=1", "-v"],
                ])

    def test_graph_cleanup_service_requires_exact_main_pass_and_refuses_all_skips(self) -> None:
        for outcome in ("missing", "suffix", "child", "skip", "child-skip"):
            with self.subTest(outcome=outcome):
                result, _ = self.run_gate_fixture(outcome, "graph", "TestIntegrationPostgresGraphCleanupRuntime")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("Graph Cleanup PostgreSQL gate", result.stderr)
                self.assertNotIn("contracts passed", result.stdout)

    def test_full_gate_requires_graph_cleanup_service(self) -> None:
        result, _ = self.run_gate_fixture("missing", "all", "TestIntegrationPostgresGraphCleanupRuntime")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Graph Cleanup PostgreSQL gate", result.stderr)

    def test_graph_gate_retains_repository_failure_checks(self) -> None:
        for outcome in ("missing", "skip", "fail"):
            with self.subTest(outcome=outcome):
                result, calls = self.run_gate_fixture(outcome, "graph", "TestIntegrationPostgresGraphTargetContract")
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(len(calls), 1)
                self.assertNotIn("contracts passed", result.stdout)

    def test_graph_service_failure_propagates_and_does_not_expand_execution_groups(self) -> None:
        result, _ = self.run_gate_fixture("fail", "graph", "TestIntegrationPostgresGraphCleanupRuntime")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("contracts passed", result.stdout)
        for group in ("execution-lifecycle", "execution-events"):
            with self.subTest(group=group):
                result, calls = self.run_gate_fixture("fail", group, "TestIntegrationPostgresGraphCleanupRuntime")
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertFalse(any(c[1] in ("./internal/repository", "./internal/service") for c in calls))

    def test_focused_group_refuses_skipped_lifecycle_matrix(self) -> None:
        result, _ = self.run_gate_fixture("skip")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("refuses skipped tests", result.stderr)

    def test_focused_group_refuses_success_when_the_owner_test_is_missing(self) -> None:
        result, _ = self.run_gate_fixture("missing")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires the real lifecycle matrix test", result.stderr)

    def test_focused_group_propagates_test_failure(self) -> None:
        result, _ = self.run_gate_fixture("fail")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("group passed", result.stdout)

    def test_full_gate_includes_the_lifecycle_matrix(self) -> None:
        result, calls = self.run_gate_fixture("pass", "all")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        execution = [call for call in calls if call[:2] == ["test", "./execution"]]
        self.assertEqual(len(execution), 1)
        self.assertIsNotNone(
            re.fullmatch(execution[0][3], "TestExecutionLifecycleAgainstPostgres")
        )

    def test_full_gate_cannot_pass_using_only_the_previous_execution_cases(self) -> None:
        result, _ = self.run_gate_fixture("missing", "all")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires the real lifecycle matrix test", result.stderr)

    def test_event_group_executes_only_the_two_exact_pg_event_cases(self) -> None:
        result, calls = self.run_gate_fixture("pass", "execution-events")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(calls, [[
            "test", "./execution", "-run",
            "^TestExecution(Events|EventRetention)AgainstPostgres$",
            "-count=1", "-v",
        ]])
        self.assertIn("not the full gate", result.stdout)
        pattern = re.compile(calls[0][3])
        for name in (
            "TestExecutionLifecycleAgainstPostgres",
            "TestExecutionRetentionAssessmentAgainstPostgres",
            "TestExecutionEventsAgainstPostgresExtra",
            "TestExecutionEventRetentionAgainstPostgresExtra",
            "TestExecutionEvents",
        ):
            with self.subTest(name=name):
                self.assertIsNone(pattern.search(name))

    def test_event_group_requires_both_main_test_passes(self) -> None:
        for name in (
            "TestExecutionEventsAgainstPostgres",
            "TestExecutionEventRetentionAgainstPostgres",
        ):
            with self.subTest(name=name):
                result, _ = self.run_gate_fixture("missing", "execution-events", name)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(f"requires {name}", result.stderr)
                self.assertNotIn("group passed", result.stdout)

    def test_event_group_refuses_either_skipped_main_test(self) -> None:
        for name in (
            "TestExecutionEventsAgainstPostgres",
            "TestExecutionEventRetentionAgainstPostgres",
        ):
            with self.subTest(name=name):
                result, _ = self.run_gate_fixture("skip", "execution-events", name)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("refuses skipped tests", result.stderr)

    def test_event_group_propagates_either_test_failure(self) -> None:
        for name in (
            "TestExecutionEventsAgainstPostgres",
            "TestExecutionEventRetentionAgainstPostgres",
        ):
            with self.subTest(name=name):
                result, _ = self.run_gate_fixture("fail", "execution-events", name)
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn("group passed", result.stdout)

    def test_event_group_does_not_execute_the_lifecycle_matrix(self) -> None:
        result, _ = self.run_gate_fixture("fail", "execution-events")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertNotIn("TestExecutionLifecycleAgainstPostgres", result.stdout)


class SystemCleanupPostgresRunnerTest(unittest.TestCase):
    CASES = (
        "TestIntegrationPostgresCleanupAcceptanceAtomicity",
        "TestIntegrationPostgresCleanupIdempotence",
        "TestIntegrationPostgresCleanupCoordinatorRecovery",
        "TestIntegrationPostgresCleanupAdmissionRace",
        "TestIntegrationPostgresCleanupReadOnlySnapshot",
    )

    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.repository = SCRIPT.parents[2]
        for directory in ("scripts/test", "scripts/infra", "scripts/utils", "system/backend", "bin"):
            (self.root / directory).mkdir(parents=True, exist_ok=True)
        self.lock = self.root / "common-pg.lock"
        source = (self.repository / "scripts/test/system-cleanup-gate.sh").read_text()
        self.runner = self.root / "scripts/test/system-cleanup-gate.sh"
        self.runner.write_text(source.replace("/tmp/addp-common-postgres-gate.lock", str(self.lock)))
        for relative in ("scripts/infra/ports.sh", "scripts/utils/observability-env.sh"):
            (self.root / relative).write_text((self.repository / relative).read_text())
        go = self.root / "bin/go"
        go.write_text(
            f"#!{sys.executable}\n"
            "import json, os, sys, time\nfrom pathlib import Path\n"
            "args = sys.argv[1:]\n"
            "Path(os.environ['TRACE']).write_text(json.dumps({'args':args,'cwd':str(Path.cwd()),'GOWORK':os.environ.get('GOWORK')}))\n"
            "mode = os.environ.get('OUTCOME', 'pass')\n"
            "if mode == 'hold':\n"
            "    Path(os.environ['STARTED']).touch()\n"
            "    deadline = time.monotonic() + 10\n"
            "    while not Path(os.environ['RELEASE']).exists():\n"
            "        if time.monotonic() > deadline: sys.exit(38)\n"
            "        time.sleep(0.01)\n"
            f"names = {self.CASES!r}\n"
            "if mode == 'fail': sys.exit(37)\n"
            "for name in names:\n"
            "    affected = name == os.environ.get('AFFECTED', names[0])\n"
            "    if mode == 'no-tests' or (mode == 'missing' and affected): continue\n"
            "    if mode == 'skip' and affected: print('--- SKIP: ' + name + ' (0.00s)'); continue\n"
            "    suffix = 'Extra' if mode == 'wrong' and affected else '/child' if mode == 'child' and affected else ''\n"
            "    print('--- PASS: ' + name + suffix + ' (0.01s)')\n"
            "if mode == 'no-tests': print('testing: warning: no tests to run\\nPASS')\n"
            "if mode == 'skip-child': print('    --- SKIP: ' + names[0] + '/child (0.00s)')\n"
        )
        go.chmod(0o700)
        docker = self.root / "bin/docker"
        docker.write_text(
            f"#!{sys.executable}\n"
            "import os, sys\n"
            "args = sys.argv[1:]\n"
            "if args[:2] == ['port', 'addp-postgres']: print('127.0.0.1:25432')\n"
            "elif args[:1] == ['inspect'] and args[2] == '{{.State.Running}}': print('true')\n"
            "elif args[:1] == ['inspect']:\n"
            "    owner = 'foreign' if os.environ.get('FOREIGN') == '1' else os.environ['FIXTURE_ROOT']\n"
            "    print('addp-infra|postgres|' + owner)\n"
            "else: sys.exit(99)\n"
        )
        docker.chmod(0o700)
        self.environment = dict(
            os.environ, PATH=str(self.root / "bin") + os.pathsep + os.environ["PATH"],
            TMPDIR=str(self.root), FIXTURE_ROOT=str(self.root), TRACE=str(self.root / "trace"),
            STARTED=str(self.root / "started"), RELEASE=str(self.root / "release"),
            GITHUB_ACTIONS="true", ADDP_LOCAL_CI_POSTGRES="",
            ADDP_SYSTEM_CLEANUP_POSTGRES_TEST_DSN="postgres://fixture:private@127.0.0.1:25432/addp_cleanup_disposable?sslmode=disable",
        )

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def run_gate(self, arguments=(), **environment):
        trace = self.root / "trace"
        trace.unlink(missing_ok=True)
        result = subprocess.run(
            ["bash", str(self.runner), *arguments], env=dict(self.environment, **environment),
            capture_output=True, text=True, timeout=15,
        )
        self.assertEqual(list(self.root.glob("addp-system-cleanup.*")), [], result.stderr)
        return result, json.loads(trace.read_text()) if trace.exists() else None

    def test_runs_only_service_prefix_with_bounded_uncached_go_and_all_five_passes(self):
        result, trace = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(trace, {
            "args": ["test", "./internal/service", "-run", "^TestIntegrationPostgresCleanup", "-count=1", "-timeout=3m", "-v"],
            "cwd": str((self.root / "system/backend").resolve()), "GOWORK": "off",
        })
        for case in self.CASES:
            self.assertIn("--- PASS: " + case + " ", result.stdout)

    def test_every_required_case_rejects_missing_skip_wrong_name_or_only_child_pass(self):
        for case in self.CASES:
            for mode in ("missing", "skip", "wrong", "child"):
                with self.subTest(case=case, mode=mode):
                    result, _ = self.run_gate(OUTCOME=mode, AFFECTED=case)
                    self.assertNotEqual(result.returncode, 0, result.stdout)
                    self.assertNotIn("contracts passed", result.stdout)

    def test_rejects_no_tests_and_any_skipped_child(self):
        for mode in ("no-tests", "skip-child"):
            with self.subTest(mode=mode):
                result, _ = self.run_gate(OUTCOME=mode)
                self.assertNotEqual(result.returncode, 0, result.stdout)

    def test_owner_failure_including_residue_assertion_propagates(self):
        result, _ = self.run_gate(OUTCOME="fail")
        self.assertEqual(result.returncode, 37, result.stdout + result.stderr)

    def test_rejects_unsafe_or_missing_dsn_before_go_without_exposing_credentials(self):
        for dsn in (
            "", "postgres://fixture:private@127.0.0.1:25432/addp",
            "postgres://fixture:private@127.0.0.1:25432/addp_iam_test",
            "postgres://fixture:private@127.0.0.1:25432/other_test",
            "postgres://fixture:private@127.0.0.1/addp_test",
            "host=localhost port=25432 dbname=addp_test password=private",
            "postgres://fixture:private@127.0.0.1:25432/addp_test?host=foreign",
        ):
            with self.subTest(dsn=dsn.split("@")[0]):
                result, trace = self.run_gate(ADDP_SYSTEM_CLEANUP_POSTGRES_TEST_DSN=dsn)
                self.assertNotEqual(result.returncode, 0)
                self.assertIsNone(trace)
                self.assertNotIn("private", result.stdout + result.stderr)

    def test_local_requires_verified_addp_test_actual_mapping_and_container_owner(self):
        local = "postgres://fixture:private@127.0.0.1:25432/addp_test?sslmode=disable"
        result, _ = self.run_gate(GITHUB_ACTIONS="", ADDP_SYSTEM_CLEANUP_POSTGRES_TEST_DSN=local)
        self.assertEqual(result.returncode, 0, result.stderr)
        for dsn, foreign in (
            (local.replace("25432", "15432"), ""), (local, "1"),
            (local.replace("127.0.0.1", "external.example.test"), ""),
            (local.replace("addp_test", "addp_cleanup_disposable"), ""),
        ):
            with self.subTest(foreign=foreign, target=dsn.split("@")[-1]):
                result, trace = self.run_gate(GITHUB_ACTIONS="", ADDP_SYSTEM_CLEANUP_POSTGRES_TEST_DSN=dsn, FOREIGN=foreign)
                self.assertNotEqual(result.returncode, 0)
                self.assertIsNone(trace)

    def test_does_not_offer_case_filters_that_can_omit_required_owner_contracts(self):
        result, trace = self.run_gate(arguments=("--test", "read-only"))
        self.assertEqual(result.returncode, 2)
        self.assertIsNone(trace)

    def test_lock_is_shared_with_common_and_not_unlinked(self):
        common = (self.repository / "scripts/test/common-postgres-gate.sh").read_text()
        self.assertIn("exec 9>>/tmp/addp-common-postgres-gate.lock", common)
        with self.lock.open("a") as held:
            inode = self.lock.stat().st_ino
            fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB)
            result, trace = self.run_gate()
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("already running", result.stderr)
            self.assertIsNone(trace)
            self.assertEqual(self.lock.stat().st_ino, inode)
        result, _ = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.lock.stat().st_ino, inode)

    def test_gate_holds_lock_while_go_child_runs(self):
        started = self.root / "started"
        release = self.root / "release"
        process = subprocess.Popen(["bash", str(self.runner)], env=dict(self.environment, OUTCOME="hold"),
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            deadline = time.monotonic() + 10
            while not started.exists() and process.poll() is None and time.monotonic() < deadline:
                time.sleep(0.01)
            self.assertTrue(started.exists(), "Go child did not start")
            with self.lock.open("a") as lock:
                with self.assertRaises(BlockingIOError):
                    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            release.touch()
            stdout, stderr = process.communicate(timeout=10)
            self.assertEqual(process.returncode, 0, stdout + stderr)
        finally:
            release.touch()
            if process.poll() is None:
                process.kill()
            process.communicate(timeout=10)

    def test_real_registration_and_automatic_owner_selection_cover_cleanup(self):
        self.assertEqual(MODULE.validate_registration(self.repository), [])
        spec = importlib.util.spec_from_file_location("cleanup_changed_gate", self.repository / "scripts/test/changed-gate.py")
        changed = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(changed)
        for path in (
            "scripts/test/system-cleanup-gate.sh",
            "system/backend/internal/service/cleanup_durable_postgres_test.go",
            "system/backend/internal/migration/sql/000250_cleanup_requests.up.sql",
            "common/execution/cleanup.go", "common/events/cleanup_protocol.go",
        ):
            with self.subTest(path=path):
                self.assertIn("system", changed.affected_modules(self.repository, [path]))
                commands = [s.command for s in changed.plan_changed(self.repository, [path])]
                self.assertEqual(commands.count(("make", "test-system-cleanup")), 1)


if __name__ == "__main__":
    unittest.main()
