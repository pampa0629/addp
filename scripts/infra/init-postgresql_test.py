#!/usr/bin/env python3

from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("init-postgresql.sh")


class InitPostgreSQLTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary_directory.name)
        self.repository = self.root / "repository"
        self.infra = self.repository / "scripts/infra"
        self.fake_bin = self.root / "bin"
        self.docker_log = self.root / "docker.log"

        self.infra.mkdir(parents=True)
        self.fake_bin.mkdir()
        shutil.copy2(SCRIPT, self.infra / "init-postgresql.sh")
        (self.infra / "init-postgresql.sql").write_text("SELECT 1;\n", encoding="utf-8")
        (self.repository / "docker-compose.infra.yml").write_text("services: {}\n", encoding="utf-8")
        self._write_fake_docker()

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def _write_fake_docker(self) -> None:
        docker = self.fake_bin / "docker"
        docker.write_text(
            textwrap.dedent(
                """
                #!/bin/bash
                printf '%s\n' "$*" >> "$ADDP_TEST_DOCKER_LOG"

                if [ "${1:-}" = "compose" ] && [ "${2:-}" = "version" ]; then
                  exit 0
                fi
                if [ "${1:-}" = "ps" ]; then
                  printf 'addp-postgres\n'
                  exit 0
                fi
                if [[ "$*" == *"SELECT 1 FROM pg_database"* ]]; then
                  if [ "${ADDP_TEST_DATABASES_EXIST:-0}" = "1" ]; then
                    printf '1\n'
                  fi
                  exit 0
                fi
                if [[ "$*" == *"SELECT COUNT(*) FROM pg_extension"* ]]; then
                  if [ "${ADDP_TEST_POSTGIS_EXISTS:-0}" = "1" ]; then
                    printf '1\n'
                  fi
                  exit 0
                fi
                if [[ "$*" == *"SELECT PostGIS_Version()"* ]]; then
                  printf '3.4.0\n'
                  exit 0
                fi
                if [[ "$*" == *"dpkg -l"* ]]; then
                  printf 'ii postgis\n'
                  exit 0
                fi
                exit 0
                """
            ).lstrip(),
            encoding="utf-8",
        )
        docker.chmod(0o755)

    def _run(
        self,
        *,
        databases_exist: bool = False,
        postgis_exists: bool = False,
    ) -> subprocess.CompletedProcess[str]:
        environment = os.environ.copy()
        environment["PATH"] = f"{self.fake_bin}:{environment['PATH']}"
        environment["ADDP_TEST_DOCKER_LOG"] = str(self.docker_log)
        environment["ADDP_TEST_DATABASES_EXIST"] = "1" if databases_exist else "0"
        environment["ADDP_TEST_POSTGIS_EXISTS"] = "1" if postgis_exists else "0"
        environment["POSTGRES_USER"] = "addp"
        environment["POSTGRES_PASSWORD"] = "addp_password"
        environment["POSTGRES_DB"] = "addp"
        environment["SKIP_PGVECTOR"] = "true"
        return subprocess.run(
            ["bash", "scripts/infra/init-postgresql.sh", "--skip-hba"],
            cwd=self.repository,
            env=environment,
            capture_output=True,
            text=True,
        )

    def _docker_commands(self) -> list[str]:
        return self.docker_log.read_text(encoding="utf-8").splitlines()

    def test_prepares_reserved_test_databases_and_postgis_for_shared_database(self) -> None:
        result = self._run()

        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        commands = self._docker_commands()
        self.assertTrue(any(command.endswith("createdb -U addp --owner=addp --encoding=UTF8 addp_test") for command in commands))
        self.assertTrue(any(command.endswith("createdb -U addp --owner=addp --encoding=UTF8 addp_iam_test") for command in commands))
        self.assertTrue(
            any(
                "-d addp_test -v ON_ERROR_STOP=1 -c CREATE EXTENSION IF NOT EXISTS postgis;" in command
                for command in commands
            )
        )
        self.assertFalse(
            any(
                "-d addp_iam_test" in command and "CREATE EXTENSION IF NOT EXISTS postgis" in command
                for command in commands
            )
        )
        schema_commands = [command for command in commands if "compose -f docker-compose.infra.yml exec" in command]
        self.assertEqual(1, len(schema_commands))
        self.assertIn("-d addp", schema_commands[0])
        self.assertNotIn("-d addp_test", schema_commands[0])
        self.assertNotIn("-d addp_iam_test", schema_commands[0])

    def test_existing_databases_and_extensions_are_not_recreated(self) -> None:
        result = self._run(databases_exist=True, postgis_exists=True)

        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        commands = self._docker_commands()
        self.assertFalse(any(" createdb " in f" {command} " for command in commands))
        self.assertFalse(any("CREATE EXTENSION IF NOT EXISTS postgis" in command for command in commands))


if __name__ == "__main__":
    unittest.main()
