#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import subprocess
import tempfile
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


if __name__ == "__main__":
    unittest.main()
