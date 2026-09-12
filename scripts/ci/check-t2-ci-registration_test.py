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
            "#!/usr/bin/env bash\n# ADDP_T2_SERVICES=postgres\n",
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
