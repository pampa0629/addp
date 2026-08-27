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


class LocalMacOSCiTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary_directory.name)
        self.origin = self.root / "origin.git"
        self.repository = self.root / "checkout"
        self.publisher = self.root / "publisher"
        self.fake_bin = self.root / "bin"
        self.make_log = self.root / "make.log"

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
        files = {
            ".gitignore": "**/.venv/\n**/venv/\n",
            ".node-version": "24\n",
            "Makefile": "help:\n\t@true\n",
            "common-python/pyproject.toml": "[project]\nname='fixture'\n",
            "agent/backend/requirements.txt": "# fixture\n",
            "copilot/backend/requirements.txt": "# fixture\n",
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
            if [ -n "${FAIL_MAKE_TARGET:-}" ] && [[ "$*" == *"$FAIL_MAKE_TARGET"* ]]; then
              exit 17
            fi
            """,
        )
        self._executable(
            "docker",
            """
            #!/bin/bash
            case "${1:-}" in
              info) exit 0 ;;
              ps)
                if [ "${ACTIVE_INFRA:-0}" = "1" ]; then
                  printf 'addp-postgres\n'
                fi
                exit 0
                ;;
              *) exit 0 ;;
            esac
            """,
        )
        self._executable(
            "node",
            """
            #!/bin/bash
            printf '%s\n' "${NODE_VERSION:-v24.20.0}"
            """,
        )
        self._executable("npm", "#!/bin/bash\nexit 0\n")
        self._executable(
            "go",
            """
            #!/bin/bash
            if [ "${1:-}" = "env" ] && [ "${2:-}" = "GOVERSION" ]; then
              printf 'go1.24.2\n'
              exit 0
            fi
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
        node_version: str = "v24.20.0",
    ) -> subprocess.CompletedProcess[str]:
        environment = os.environ.copy()
        environment["PATH"] = f"{self.fake_bin}:{environment['PATH']}"
        environment["MAKE_LOG"] = str(self.make_log)
        environment["FAIL_MAKE_TARGET"] = fail_target
        environment["ACTIVE_INFRA"] = "1" if active_infra else "0"
        environment["NODE_VERSION"] = node_version
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
        self.assertEqual(
            ["test", "build BUILD_ARGS=--force", "infra-up", "test-integration", "infra-down"],
            self._make_commands(),
        )
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

    def test_check_only_rejects_nonstandard_node_major(self) -> None:
        result = self._run("--check-only", node_version="v22.22.0")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Node.js 24 is required, found v22.22.0", result.stderr)
        self.assertEqual([], self._make_commands())


if __name__ == "__main__":
    unittest.main()
