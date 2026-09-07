#!/usr/bin/env python3

from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("local-macos-ci.sh")
MYSQL_GATE = Path(__file__).with_name("common-mysql-data-protection-gate.sh")
OCEANBASE_GATE = Path(__file__).with_name("common-oceanbase-gate.sh")
LOCAL_CI_COMPOSE = Path(__file__).with_name("docker-compose.local-macos-ci.yml")
T2_WORKFLOW = Path(__file__).parents[2] / ".github/workflows/release-and-t2-gates.yml"


class LocalMacOSCiTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary_directory.name)
        self.origin = self.root / "origin.git"
        self.repository = self.root / "checkout"
        self.publisher = self.root / "publisher"
        self.fake_bin = self.root / "bin"
        self.make_log = self.root / "make.log"
        self.make_environment_log = self.root / "make-environment.log"
        self.mysql_make_environment_log = self.root / "mysql-make-environment.log"
        self.oceanbase_make_environment_log = self.root / "oceanbase-make-environment.log"
        self.npm_log = self.root / "npm.log"
        self.go_log = self.root / "go.log"
        self.oceanbase_go_log = self.root / "oceanbase-go.log"
        self.docker_log = self.root / "docker.log"
        self.event_log = self.root / "events.log"
        self.mysql_state = self.root / "mysql-running"
        self.oceanbase_state = self.root / "oceanbase-running"

        subprocess.run(["git", "init", "--bare", "-q", str(self.origin)], check=True)
        subprocess.run(["git", "init", "-q", str(self.repository)], check=True)
        self._write_repository_files()
        self._git(self.repository, "add", ".")
        self._git(
            self.repository,
            "-c",
            "user.name=Test",
            "-c",
            "user.email=test@example.com",
            "commit",
            "-qm",
            "initial",
        )
        self._git(self.repository, "branch", "-M", "main")
        self._git(self.repository, "remote", "add", "origin", str(self.origin))
        self._git(self.repository, "push", "-qu", "origin", "main")
        subprocess.run(
            ["git", "--git-dir", str(self.origin), "symbolic-ref", "HEAD", "refs/heads/main"],
            check=True,
        )
        subprocess.run(["git", "clone", "-q", str(self.origin), str(self.publisher)], check=True)
        self._write_fake_commands()

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    @staticmethod
    def _git(repository: Path, *arguments: str) -> str:
        result = subprocess.run(
            ["git", *arguments],
            cwd=repository,
            check=True,
            capture_output=True,
            text=True,
        )
        return result.stdout.strip()

    def _write_repository_files(self) -> None:
        target = self.repository / "scripts/test/local-macos-ci.sh"
        target.parent.mkdir(parents=True)
        shutil.copy2(SCRIPT, target)
        shutil.copy2(MYSQL_GATE, target.with_name("common-mysql-data-protection-gate.sh"))
        shutil.copy2(OCEANBASE_GATE, target.with_name("common-oceanbase-gate.sh"))
        if LOCAL_CI_COMPOSE.exists():
            shutil.copy2(LOCAL_CI_COMPOSE, target.with_name("docker-compose.local-macos-ci.yml"))
        files = {
            ".gitignore": "**/.venv/\n**/venv/\n",
            ".node-version": "24\n",
            "Makefile": "help:\n\t@true\n",
            "common-python/pyproject.toml": "[project]\nname='fixture'\n",
            "agent/backend/requirements.txt": "# fixture\n",
            "copilot/backend/requirements.txt": "# fixture\n",
            "common/.keep": "",
            "manager/backend/.keep": "",
            "develop/backend/.keep": "",
            "service/backend/.keep": "",
            "transfer/backend/.keep": "",
        }
        for relative_path, content in files.items():
            path = self.repository / relative_path
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content, encoding="utf-8")

    def _executable(self, name: str, content: str) -> None:
        path = self.fake_bin / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(textwrap.dedent(content).lstrip(), encoding="utf-8")
        path.chmod(0o755)

    def _write_fake_commands(self) -> None:
        self._executable(
            "make",
            """
            #!/bin/bash
            printf '%s\n' "$*" >> "$MAKE_LOG"
            printf '%s|%s\n' "$*" "${DEVELOP_POSTGRES_TEST_DSN-UNSET}" >> "$MAKE_ENVIRONMENT_LOG"
            printf '%s|%s|%s|%s|%s|%s\n' "$*" \
              "${ADDP_LOCAL_CI_MYSQL-UNSET}" \
              "${ADDP_TEST_MYSQL_HOST-UNSET}" \
              "${ADDP_TEST_MYSQL_PORT-UNSET}" \
              "${ADDP_TEST_MYSQL_USER-UNSET}" \
              "${ADDP_TEST_MYSQL_PASSWORD-UNSET}" >> "$MYSQL_MAKE_ENVIRONMENT_LOG"
            printf '%s|%s|%s|%s|%s|%s|%s|%s\n' "$*" \
              "${ADDP_LOCAL_CI_OCEANBASE-UNSET}" \
              "${ADDP_TEST_OCEANBASE_HOST-UNSET}" \
              "${ADDP_TEST_OCEANBASE_PORT-UNSET}" \
              "${ADDP_TEST_OCEANBASE_TENANT-UNSET}" \
              "${ADDP_TEST_OCEANBASE_USER-UNSET}" \
              "${ADDP_TEST_OCEANBASE_PASSWORD-UNSET}" \
              "${ADDP_TEST_OCEANBASE_DATABASE-UNSET}" >> "$OCEANBASE_MAKE_ENVIRONMENT_LOG"
            printf 'make:%s\n' "$*" >> "$EVENT_LOG"
            if [ -n "${FAIL_MAKE_TARGET:-}" ] && [[ "$*" == *"$FAIL_MAKE_TARGET"* ]]; then
              exit 17
            fi
            """,
        )
        self._executable(
            "docker",
            """
            #!/bin/bash
            printf '%s\n' "$*" >> "$DOCKER_LOG"
            printf 'docker:%s\n' "$*" >> "$EVENT_LOG"
            case "${1:-}" in
              info) exit 0 ;;
              ps)
                if [ "${ACTIVE_INFRA:-0}" = "1" ]; then
                  printf 'addp-postgres\n'
                fi
                if [ "${ACTIVE_MYSQL:-0}" = "1" ]; then
                  printf 'addp-local-ci-mysql\n'
                fi
                if [ "${ACTIVE_OCEANBASE:-0}" = "1" ]; then
                  printf 'addp-local-ci-oceanbase\n'
                fi
                exit 0
                ;;
              compose)
                case " $* " in
                  *" config --quiet "*)
                    if [ "${FAIL_MYSQL_PREFLIGHT:-0}" = "1" ]; then
                      exit 18
                    fi
                    ;;
                  *" up -d --force-recreate mysql "*)
                    if [ "${FAIL_MYSQL_START:-0}" = "1" ]; then
                      exit 19
                    fi
                    touch "$MYSQL_STATE"
                    ;;
                  *" up -d --force-recreate oceanbase "*)
                    if [ "${FAIL_OCEANBASE_START:-0}" = "1" ]; then
                      exit 20
                    fi
                    touch "$OCEANBASE_STATE"
                    ;;
                  *" down --volumes --remove-orphans "*)
                    rm -f "$MYSQL_STATE"
                    rm -f "$OCEANBASE_STATE"
                    ;;
                esac
                exit 0
                ;;
              inspect)
                case "${*: -1}" in
                  addp-local-ci-mysql) [ -f "$MYSQL_STATE" ] || exit 1 ;;
                  addp-local-ci-oceanbase) [ -f "$OCEANBASE_STATE" ] || exit 1 ;;
                  *) exit 1 ;;
                esac
                printf 'healthy\n'
                exit 0
                ;;
              *) exit 0 ;;
            esac
            """,
        )
        self._executable("uname", "#!/bin/bash\nprintf 'Darwin\\n'\n")
        self._executable(
            "node",
            """
            #!/bin/bash
            printf '%s\n' "${NODE_VERSION:-v24.20.0}"
            """,
        )
        self._executable(
            "npm",
            """
            #!/bin/bash
            printf '%s\n' "$*" >> "$NPM_LOG"
            exit 0
            """,
        )
        self._executable(
            "go",
            """
            #!/bin/bash
            if [ "${1:-}" = "env" ] && [ "${2:-}" = "GOVERSION" ]; then
              printf 'go1.24.2\n'
              exit 0
            fi
            printf '%s|%s|%s|%s|%s\n' "$*" \
              "${ADDP_TEST_MYSQL_HOST-UNSET}" \
              "${ADDP_TEST_MYSQL_PORT-UNSET}" \
              "${ADDP_TEST_MYSQL_USER-UNSET}" \
              "${ADDP_TEST_MYSQL_PASSWORD-UNSET}" >> "$GO_LOG"
            printf '%s|%s|%s|%s|%s|%s|%s\n' "$*" \
              "${ADDP_TEST_OCEANBASE_HOST-UNSET}" \
              "${ADDP_TEST_OCEANBASE_PORT-UNSET}" \
              "${ADDP_TEST_OCEANBASE_TENANT-UNSET}" \
              "${ADDP_TEST_OCEANBASE_USER-UNSET}" \
              "${ADDP_TEST_OCEANBASE_PASSWORD-UNSET}" \
              "${ADDP_TEST_OCEANBASE_DATABASE-UNSET}" >> "$OCEANBASE_GO_LOG"
            exit 0
            """,
        )
        self._executable("curl", "#!/bin/bash\nexit 0\n")
        self._executable(
            "python3",
            """
            #!/bin/bash
            if [ "${1:-}" = "--version" ]; then
              printf 'Python 3.11.11\n'
              exit 0
            fi
            if [ "${1:-}" = "-" ]; then
              cat >/dev/null
              exit 0
            fi
            if [ "${1:-}" = "-m" ] && [ "${2:-}" = "venv" ]; then
              venv=$3
              mkdir -p "$venv/bin"
              printf '#!/bin/bash\nexit 0\n' > "$venv/bin/python"
              chmod +x "$venv/bin/python"
              exit 0
            fi
            exit 0
            """,
        )

    def _run(
        self,
        *arguments: str,
        fail_target: str = "",
        active_infra: bool = False,
        active_mysql: bool = False,
        active_oceanbase: bool = False,
        mysql_preflight_failure: bool = False,
        mysql_start_failure: bool = False,
        oceanbase_start_failure: bool = False,
        node_version: str = "v24.20.0",
        develop_postgres_test_dsn: str | None = None,
        inherited_oceanbase_environment: bool = False,
    ) -> subprocess.CompletedProcess[str]:
        environment = os.environ.copy()
        environment["PATH"] = f"{self.fake_bin}:{environment['PATH']}"
        environment["MAKE_LOG"] = str(self.make_log)
        environment["MAKE_ENVIRONMENT_LOG"] = str(self.make_environment_log)
        environment["MYSQL_MAKE_ENVIRONMENT_LOG"] = str(self.mysql_make_environment_log)
        environment["OCEANBASE_MAKE_ENVIRONMENT_LOG"] = str(
            self.oceanbase_make_environment_log
        )
        environment["NPM_LOG"] = str(self.npm_log)
        environment["GO_LOG"] = str(self.go_log)
        environment["OCEANBASE_GO_LOG"] = str(self.oceanbase_go_log)
        environment["DOCKER_LOG"] = str(self.docker_log)
        environment["EVENT_LOG"] = str(self.event_log)
        environment["MYSQL_STATE"] = str(self.mysql_state)
        environment["OCEANBASE_STATE"] = str(self.oceanbase_state)
        environment["FAIL_MAKE_TARGET"] = fail_target
        environment["ACTIVE_INFRA"] = "1" if active_infra else "0"
        environment["ACTIVE_MYSQL"] = "1" if active_mysql else "0"
        environment["ACTIVE_OCEANBASE"] = "1" if active_oceanbase else "0"
        environment["FAIL_MYSQL_PREFLIGHT"] = "1" if mysql_preflight_failure else "0"
        environment["FAIL_MYSQL_START"] = "1" if mysql_start_failure else "0"
        environment["FAIL_OCEANBASE_START"] = "1" if oceanbase_start_failure else "0"
        environment["NODE_VERSION"] = node_version
        if inherited_oceanbase_environment:
            environment.update(
                {
                    "ADDP_LOCAL_CI_OCEANBASE": "unsafe-marker",
                    "ADDP_TEST_OCEANBASE_HOST": "unsafe.example.com",
                    "ADDP_TEST_OCEANBASE_PORT": "2881",
                    "ADDP_TEST_OCEANBASE_TENANT": "unsafe-tenant",
                    "ADDP_TEST_OCEANBASE_USER": "unsafe-user",
                    "ADDP_TEST_OCEANBASE_PASSWORD": "unsafe-password",
                    "ADDP_TEST_OCEANBASE_DATABASE": "unsafe_database",
                }
            )
        if develop_postgres_test_dsn is not None:
            environment["DEVELOP_POSTGRES_TEST_DSN"] = develop_postgres_test_dsn
        return subprocess.run(
            ["bash", "scripts/test/local-macos-ci.sh", *arguments],
            cwd=self.repository,
            env=environment,
            capture_output=True,
            text=True,
        )

    def _make_commands(self) -> list[str]:
        if not self.make_log.exists():
            return []
        return self.make_log.read_text(encoding="utf-8").splitlines()

    def _npm_commands(self) -> list[str]:
        if not self.npm_log.exists():
            return []
        return self.npm_log.read_text(encoding="utf-8").splitlines()

    def _make_environments(self) -> list[str]:
        if not self.make_environment_log.exists():
            return []
        return self.make_environment_log.read_text(encoding="utf-8").splitlines()

    def _mysql_make_environments(self) -> list[str]:
        if not self.mysql_make_environment_log.exists():
            return []
        return self.mysql_make_environment_log.read_text(encoding="utf-8").splitlines()

    def _oceanbase_make_environments(self) -> list[str]:
        if not self.oceanbase_make_environment_log.exists():
            return []
        return self.oceanbase_make_environment_log.read_text(
            encoding="utf-8"
        ).splitlines()

    def _docker_commands(self) -> list[str]:
        if not self.docker_log.exists():
            return []
        return self.docker_log.read_text(encoding="utf-8").splitlines()

    def _events(self) -> list[str]:
        if not self.event_log.exists():
            return []
        return self.event_log.read_text(encoding="utf-8").splitlines()

    def _run_mysql_gate(self) -> subprocess.CompletedProcess[str]:
        environment = os.environ.copy()
        environment["PATH"] = f"{self.fake_bin}:{environment['PATH']}"
        environment["GO_LOG"] = str(self.go_log)
        environment["OCEANBASE_GO_LOG"] = str(self.oceanbase_go_log)
        environment["ADDP_LOCAL_CI_MYSQL"] = "1"
        environment["ADDP_TEST_MYSQL_HOST"] = "unsafe.example.com"
        environment["ADDP_TEST_MYSQL_PORT"] = "3306"
        environment["ADDP_TEST_MYSQL_USER"] = "unsafe"
        environment["ADDP_TEST_MYSQL_PASSWORD"] = "unsafe-password"
        return subprocess.run(
            ["bash", "scripts/test/common-mysql-data-protection-gate.sh"],
            cwd=self.repository,
            env=environment,
            capture_output=True,
            text=True,
        )

    def _run_oceanbase_gate(self) -> subprocess.CompletedProcess[str]:
        environment = os.environ.copy()
        environment["PATH"] = f"{self.fake_bin}:{environment['PATH']}"
        environment["GO_LOG"] = str(self.go_log)
        environment["OCEANBASE_GO_LOG"] = str(self.oceanbase_go_log)
        environment["ADDP_LOCAL_CI_OCEANBASE"] = "1"
        environment["ADDP_TEST_OCEANBASE_HOST"] = "unsafe.example.com"
        environment["ADDP_TEST_OCEANBASE_PORT"] = "2881"
        environment["ADDP_TEST_OCEANBASE_TENANT"] = "unsafe-tenant"
        environment["ADDP_TEST_OCEANBASE_USER"] = "unsafe-user"
        environment["ADDP_TEST_OCEANBASE_PASSWORD"] = "unsafe-password"
        environment["ADDP_TEST_OCEANBASE_DATABASE"] = "unsafe_database"
        return subprocess.run(
            ["bash", "scripts/test/common-oceanbase-gate.sh"],
            cwd=self.repository,
            env=environment,
            capture_output=True,
            text=True,
        )

    def _publish_change(self) -> str:
        change = self.publisher / "change.txt"
        change.write_text("changed\n", encoding="utf-8")
        self._git(self.publisher, "add", "change.txt")
        self._git(
            self.publisher,
            "-c",
            "user.name=Test",
            "-c",
            "user.email=test@example.com",
            "commit",
            "-qm",
            "change",
        )
        self._git(self.publisher, "push", "-q", "origin", "main")
        return self._git(self.publisher, "rev-parse", "HEAD")

    def _commit_local_change(self) -> str:
        change = self.repository / "local-change.txt"
        change.write_text("changed locally\n", encoding="utf-8")
        self._git(self.repository, "add", "local-change.txt")
        self._git(
            self.repository,
            "-c",
            "user.name=Test",
            "-c",
            "user.email=test@example.com",
            "commit",
            "-qm",
            "local change",
        )
        return self._git(self.repository, "rev-parse", "HEAD")

    def test_first_run_is_full_and_second_run_skips_same_sha(self) -> None:
        first = self._run()

        self.assertEqual(first.returncode, 0, first.stderr + first.stdout)
        self.assertIn(
            "--prefix model/frontend exec -- playwright install chromium",
            self._npm_commands(),
        )
        self.assertEqual(
            ["test", "build BUILD_ARGS=--force", "infra-up", "test-integration", "infra-down"],
            self._make_commands(),
        )
        events = self._events()
        mysql_start = next(
            index
            for index, event in enumerate(events)
            if " up -d --force-recreate mysql" in event
        )
        oceanbase_start = next(
            index
            for index, event in enumerate(events)
            if " up -d --force-recreate oceanbase" in event
        )
        deterministic_test = events.index("make:test")
        mysql_stop = next(index for index, event in enumerate(events) if " down --volumes --remove-orphans" in event)
        self.assertLess(mysql_start, deterministic_test)
        self.assertLess(oceanbase_start, deterministic_test)
        self.assertGreater(mysql_stop, events.index("make:infra-down"))
        first_sha = self._git(self.repository, "rev-parse", "HEAD")
        state = self.repository / ".git/addp-local-ci/last-success-sha"
        self.assertEqual(first_sha, state.read_text(encoding="utf-8").strip())

        second = self._run()

        self.assertEqual(second.returncode, 0, second.stderr + second.stdout)
        self.assertIn("already successful", second.stdout)
        self.assertEqual(5, len(self._make_commands()))

    def test_new_main_commit_runs_incremental_gate_from_last_success(self) -> None:
        first = self._run()
        self.assertEqual(first.returncode, 0, first.stderr + first.stdout)
        baseline = self._git(self.repository, "rev-parse", "HEAD")
        target = self._publish_change()

        second = self._run()

        self.assertEqual(second.returncode, 0, second.stderr + second.stdout)
        self.assertEqual(target, self._git(self.repository, "rev-parse", "HEAD"))
        self.assertEqual(
            [
                "test",
                "build BUILD_ARGS=--force",
                "infra-up",
                "test-integration",
                "infra-down",
                "build BUILD_ARGS=--force",
                "infra-up",
                f"test-changed BASE_REF={baseline}",
                "infra-down",
            ],
            self._make_commands(),
        )

    def test_no_fetch_runs_current_main_without_remote_sync(self) -> None:
        first = self._run()
        self.assertEqual(first.returncode, 0, first.stderr + first.stdout)
        baseline = self._git(self.repository, "rev-parse", "HEAD")
        target = self._commit_local_change()
        self._git(self.repository, "remote", "set-url", "origin", str(self.root / "missing-origin.git"))

        second = self._run("--no-fetch")

        self.assertEqual(second.returncode, 0, second.stderr + second.stdout)
        self.assertIn("current main checkout (no fetch)", second.stdout)
        self.assertEqual(target, self._git(self.repository, "rev-parse", "HEAD"))
        self.assertEqual(
            [
                "test",
                "build BUILD_ARGS=--force",
                "infra-up",
                "test-integration",
                "infra-down",
                "build BUILD_ARGS=--force",
                "infra-up",
                f"test-changed BASE_REF={baseline}",
                "infra-down",
            ],
            self._make_commands(),
        )

    def test_no_fetch_full_runs_all_gates_without_remote_sync(self) -> None:
        self._git(
            self.repository,
            "remote",
            "set-url",
            "origin",
            str(self.root / "missing-origin.git"),
        )

        result = self._run("--no-fetch", "--full")

        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        self.assertIn("current main checkout (no fetch)", result.stdout)
        self.assertIn("scope=full remote_sync=skipped", result.stdout)
        self.assertEqual(
            [
                "test",
                "build BUILD_ARGS=--force",
                "infra-up",
                "test-integration",
                "infra-down",
            ],
            self._make_commands(),
        )
        summary = (self.repository / ".git/addp-local-ci/latest-summary.txt").read_text(
            encoding="utf-8"
        )
        self.assertIn("schema_version=addp.local-ci-summary/v2\n", summary)
        self.assertIn("scope=full\n", summary)
        self.assertIn("remote_sync=skipped\n", summary)

    def test_full_and_no_fetch_options_are_order_independent(self) -> None:
        self._git(
            self.repository,
            "remote",
            "set-url",
            "origin",
            str(self.root / "missing-origin.git"),
        )

        result = self._run("--full", "--no-fetch")

        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        self.assertIn("scope=full remote_sync=skipped", result.stdout)

    def test_check_only_cannot_be_combined_with_run_options(self) -> None:
        result = self._run("--check-only", "--no-fetch")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("--check-only cannot be combined with other options", result.stderr)
        self.assertEqual([], self._make_commands())

    def test_failed_gate_does_not_advance_successful_sha(self) -> None:
        first = self._run()
        self.assertEqual(first.returncode, 0, first.stderr + first.stdout)
        baseline = self._git(self.repository, "rev-parse", "HEAD")
        self._publish_change()

        failed = self._run(fail_target="test-changed")

        self.assertEqual(failed.returncode, 17, failed.stderr + failed.stdout)
        state = self.repository / ".git/addp-local-ci/last-success-sha"
        self.assertEqual(baseline, state.read_text(encoding="utf-8").strip())
        self.assertEqual("infra-down", self._make_commands()[-1])
        self.assertTrue(any(" down --volumes --remove-orphans" in command for command in self._docker_commands()))

    def test_postgres_gates_replace_external_develop_dsn_with_shared_test_database(self) -> None:
        result = self._run(
            "--no-fetch",
            develop_postgres_test_dsn="postgres://external/unsafe_database",
        )

        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        self.assertIn("test|UNSET", self._make_environments())
        self.assertIn(
            "test-integration|postgres://addp:addp_password@127.0.0.1:15432/addp_test?sslmode=disable",
            self._make_environments(),
        )

    def test_mysql_connection_is_materialized_only_inside_mysql_gate(self) -> None:
        result = self._run("--no-fetch")

        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        self.assertIn(
            "test-integration|1|UNSET|UNSET|UNSET|UNSET",
            self._mysql_make_environments(),
        )
        self.assertTrue(
            all(
                "--env-file /dev/null" in command
                for command in self._docker_commands()
                if command.startswith("compose ")
            )
        )

        gate = self._run_mysql_gate()

        self.assertEqual(gate.returncode, 0, gate.stderr + gate.stdout)
        go_environments = self.go_log.read_text(encoding="utf-8").splitlines()
        self.assertEqual(5, len(go_environments))
        for environment in go_environments:
            self.assertTrue(
                environment.endswith(
                    "|127.0.0.1|13306|root|addp_local_ci_mysql_password"
                ),
                environment,
            )

    def test_oceanbase_connection_is_materialized_only_inside_oceanbase_gate(self) -> None:
        result = self._run(
            "--no-fetch",
            inherited_oceanbase_environment=True,
        )

        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        self.assertIn(
            "test|UNSET|UNSET|UNSET|UNSET|UNSET|UNSET|UNSET",
            self._oceanbase_make_environments(),
        )
        self.assertIn(
            "test-integration|1|UNSET|UNSET|UNSET|UNSET|UNSET|UNSET",
            self._oceanbase_make_environments(),
        )

        gate = self._run_oceanbase_gate()

        self.assertEqual(gate.returncode, 0, gate.stderr + gate.stdout)
        go_environments = self.oceanbase_go_log.read_text(
            encoding="utf-8"
        ).splitlines()
        self.assertEqual(1, len(go_environments))
        self.assertTrue(
            go_environments[0].endswith(
                "|127.0.0.1|12881|test|root@test|"
                "addp_local_ci_oceanbase_password|addp_oceanbase_disposable"
            ),
            go_environments[0],
        )

    def test_mysql_start_failure_stops_before_deterministic_gates(self) -> None:
        result = self._run("--no-fetch", mysql_start_failure=True)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("disposable MySQL failed to start", result.stderr + result.stdout)
        self.assertEqual([], self._make_commands())
        self.assertTrue(
            any(
                " up -d --force-recreate mysql" in command
                for command in self._docker_commands()
            )
        )
        self.assertTrue(any(" down --volumes --remove-orphans" in command for command in self._docker_commands()))

    def test_oceanbase_start_failure_stops_before_deterministic_gates_and_cleans_both_services(self) -> None:
        result = self._run("--no-fetch", oceanbase_start_failure=True)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("disposable OceanBase failed to start", result.stderr + result.stdout)
        self.assertEqual([], self._make_commands())
        self.assertTrue(
            any(
                " up -d --force-recreate oceanbase" in command
                for command in self._docker_commands()
            )
        )
        self.assertTrue(
            any(
                " down --volumes --remove-orphans" in command
                for command in self._docker_commands()
            )
        )
        self.assertFalse(self.mysql_state.exists())
        self.assertFalse(self.oceanbase_state.exists())

    def test_check_only_rejects_invalid_local_database_compose_definition(self) -> None:
        result = self._run("--check-only", mysql_preflight_failure=True)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("disposable database Compose definition is invalid", result.stderr)
        self.assertEqual([], self._make_commands())

    def test_local_database_compose_is_pinned_healthy_and_disposable(self) -> None:
        compose = LOCAL_CI_COMPOSE.read_text(encoding="utf-8")
        workflow = T2_WORKFLOW.read_text(encoding="utf-8")
        oceanbase_image = (
            "oceanbase/oceanbase-ce:4.4.2-lts@sha256:"
            "eaa2c3b3513fa21e4a658f197c1deb93c9d6b6eb742cfa5ef0f085f82b9cec54"
        )

        self.assertIn(
            "mysql:8.0@sha256:7dcddc01f13bab2f15cde676d44d01f61fc9f99fe7785e86196dfc07d358ae2b",
            compose,
        )
        self.assertIn(oceanbase_image, compose)
        self.assertIn(oceanbase_image, workflow)
        self.assertIn("healthcheck:", compose)
        self.assertIn('127.0.0.1:13306:3306', compose)
        self.assertIn('127.0.0.1:12881:2881', compose)
        self.assertIn("addp_oceanbase_disposable", OCEANBASE_GATE.read_text(encoding="utf-8"))
        self.assertNotIn("volumes:", compose)

    def test_local_ci_waits_for_both_database_health_checks(self) -> None:
        result = self._run("--no-fetch")

        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        docker_commands = self._docker_commands()
        self.assertTrue(
            any(command.endswith("addp-local-ci-mysql") for command in docker_commands)
        )
        self.assertTrue(
            any(command.endswith("addp-local-ci-oceanbase") for command in docker_commands)
        )

    def test_check_only_rejects_dirty_checkout(self) -> None:
        (self.repository / "untracked.txt").write_text("dirty\n", encoding="utf-8")

        result = self._run("--check-only")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("checkout is not clean", result.stderr)
        self.assertEqual([], self._make_commands())

    def test_check_only_rejects_running_addp_infrastructure(self) -> None:
        result = self._run("--check-only", active_infra=True)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("running ADDP Infra belongs to another session", result.stderr)
        self.assertEqual([], self._make_commands())

    def test_check_only_rejects_running_local_ci_mysql(self) -> None:
        result = self._run("--check-only", active_mysql=True)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("running ADDP Infra belongs to another session: addp-local-ci-mysql", result.stderr)
        self.assertEqual([], self._make_commands())

    def test_check_only_rejects_running_local_ci_oceanbase(self) -> None:
        result = self._run("--check-only", active_oceanbase=True)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn(
            "running ADDP Infra belongs to another session: addp-local-ci-oceanbase",
            result.stderr,
        )
        self.assertEqual([], self._make_commands())

    def test_check_only_rejects_nonstandard_node_major(self) -> None:
        result = self._run("--check-only", node_version="v22.22.0")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Node.js 24 is required, found v22.22.0", result.stderr)
        self.assertEqual([], self._make_commands())


if __name__ == "__main__":
    unittest.main()
