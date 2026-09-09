import json
import os
import shutil
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path


SCRIPT = (
    Path(__file__).parents[2]
    / "business/scripts/online-opengauss-consumer-fixture.sh"
)
MEDIA_HELPER = Path(__file__).parents[1] / "lib/opengauss-official-media.sh"


class OnlineOpenGaussConsumerFixtureTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(
            prefix="addp-online-opengauss-consumer-"
        )
        self.root = Path(self.temporary.name)
        self.repository = self.root / "repository"
        self.bin = self.root / "bin"
        self.state = self.root / "state"
        self.log = self.root / "docker.log"
        self.descriptor_file = (
            self.root / "addp-online-secret-test/opengauss-engine.json"
        )
        (self.repository / "business/scripts").mkdir(parents=True)
        (self.repository / "scripts/lib").mkdir(parents=True)
        self.bin.mkdir()
        self.state.mkdir()
        shutil.copy2(
            SCRIPT,
            self.repository
            / "business/scripts/online-opengauss-consumer-fixture.sh",
        )
        shutil.copy2(
            MEDIA_HELPER,
            self.repository / "scripts/lib/opengauss-official-media.sh",
        )
        self._write_fake_commands()
        self.environment = dict(os.environ)
        self.environment.update(
            {
                "PATH": f"{self.bin}:{self.environment['PATH']}",
                "GITHUB_ACTIONS": "true",
                "RUNNER_OS": "Linux",
                "RUNNER_TEMP": str(self.root),
                "ADDP_ONLINE_HOSTED": "1",
                "ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE": str(
                    self.descriptor_file
                ),
                "ADDP_OPENGAUSS_MEDIA_CACHE": str(self.root / "media"),
                "ADDP_TEST_DOCKER_LOG": str(self.log),
                "ADDP_TEST_STATE": str(self.state),
            }
        )

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def _executable(self, name: str, content: str) -> None:
        path = self.bin / name
        path.write_text(textwrap.dedent(content).lstrip(), encoding="utf-8")
        path.chmod(0o755)

    def _write_fake_commands(self) -> None:
        self._executable(
            "uname",
            """
            #!/usr/bin/env bash
            if [ "${1:-}" = "-s" ]; then
              printf '%s\n' "${ADDP_TEST_UNAME_S:-Linux}"
            else
              printf '%s\n' "${ADDP_TEST_UNAME_M:-x86_64}"
            fi
            """,
        )
        self._executable(
            "curl",
            """
            #!/usr/bin/env bash
            previous=
            for argument in "$@"; do
              if [ "$previous" = "--output" ]; then
                : > "$argument"
                exit 0
              fi
              previous=$argument
            done
            exit 2
            """,
        )
        self._executable(
            "sha256sum",
            """
            #!/usr/bin/env bash
            cat >/dev/null
            exit 0
            """,
        )
        self._executable(
            "docker",
            """
            #!/usr/bin/env bash
            printf '%s\n' "$*" >> "$ADDP_TEST_DOCKER_LOG"
            image=$ADDP_TEST_STATE/image
            container=$ADDP_TEST_STATE/container
            case "$1 $2" in
              "image inspect")
                if [ "${3:-}" = "--format" ]; then
                  printf '%s\n' amd64
                else
                  [ "${ADDP_TEST_PREEXIST_IMAGE:-0}" = "1" ] || [ -f "$image" ]
                fi
                ;;
              "container inspect")
                [ -f "$container" ]
                ;;
              "rm --force")
                rm -f "$container"
                ;;
              *)
                case "$1" in
                  load) touch "$image" ;;
                  run) touch "$container"; printf '%s\n' fake-container ;;
                  inspect)
                    [ -f "$container" ] || exit 1
                    case "$*" in
                      *com.addp.online-fixture*) printf '%s\n' "${ADDP_TEST_OWNER:-opengauss-consumer-flow}" ;;
                      *) printf '%s\n' "${ADDP_TEST_RUNNING:-true}" ;;
                    esac
                    ;;
                  port) printf '%s\n' 127.0.0.1:35432 ;;
                  exec)
                    input=$(cat)
                    [ -z "$input" ] || printf 'stdin:%s\n' "$input" >> "$ADDP_TEST_DOCKER_LOG"
                    case "$* $input" in
                      *"updated_at >"*) printf '%s\n' 2 ;;
                      *"COUNT"*addp_online_consumer_source*) printf '%s\n' 5 ;;
                      *"COUNT"*addp_online_consumer_target*) printf '%s\n' 0 ;;
                      *"SELECT 1"*) printf '%s\n' 1 ;;
                    esac
                    ;;
                  *) exit 9 ;;
                esac
                ;;
            esac
            """,
        )

    def run_fixture(
        self, action: str, **overrides: str
    ) -> subprocess.CompletedProcess[str]:
        environment = dict(self.environment)
        environment.update(overrides)
        return subprocess.run(
            [
                "bash",
                "business/scripts/online-opengauss-consumer-fixture.sh",
                action,
            ],
            cwd=self.repository,
            env=environment,
            capture_output=True,
            text=True,
        )

    def test_uses_official_media_resets_advances_and_removes_owned_container(self) -> None:
        started = self.run_fixture("start")
        advanced = self.run_fixture("advance")
        stopped = self.run_fixture("stop")

        self.assertEqual(started.returncode, 0, started.stderr)
        self.assertEqual(advanced.returncode, 0, advanced.stderr)
        self.assertEqual(stopped.returncode, 0, stopped.stderr)
        commands = self.log.read_text(encoding="utf-8")
        self.assertIn("load --input", commands)
        self.assertIn("--label com.addp.online-fixture=opengauss-consumer-flow", commands)
        self.assertIn("exec --interactive --user omm", commands)
        self.assertIn("CREATE DATABASE addp_opengauss_online DBCOMPATIBILITY 'PG'", commands)
        self.assertGreaterEqual(
            commands.count('DROP TABLE IF EXISTS "addp_online_consumer_target"'), 2
        )
        self.assertIn("MERGE INTO \"addp_online_consumer_source\"", commands)
        self.assertIn("rm --force addp-opengauss-online-disposable", commands)
        self.assertFalse((self.state / "container").exists())
        self.assertEqual(self.descriptor_file.stat().st_mode & 0o777, 0o600)
        descriptor = json.loads(self.descriptor_file.read_text(encoding="utf-8"))
        self.assertEqual(descriptor["engine_type"], "opengauss")
        self.assertEqual(descriptor["engine_origin"], "general")
        self.assertEqual(descriptor["connection_info"]["port"], 35432)
        self.assertEqual(
            set(descriptor),
            {"name", "engine_type", "engine_origin", "connection_info", "description"},
        )
        self.assertNotIn(
            "ADDP_TEST_OPENGAUSS",
            self.descriptor_file.read_text(encoding="utf-8"),
        )

    def test_rejects_non_github_environment_before_docker(self) -> None:
        result = self.run_fixture("start", GITHUB_ACTIONS="false")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("restricted to GitHub Actions", result.stderr)
        self.assertFalse(self.log.exists())

    def test_rejects_engine_descriptor_inside_repository(self) -> None:
        result = self.run_fixture(
            "start",
            ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE=str(
                self.repository / "opengauss-engine.json"
            ),
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must be outside the repository", result.stderr)
        self.assertNotIn("load --input", self.log.read_text(encoding="utf-8"))

    def test_refuses_preexisting_image_before_loading_media(self) -> None:
        result = self.run_fixture("start", ADDP_TEST_PREEXIST_IMAGE="1")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("refusing to reuse existing image", result.stderr)
        self.assertNotIn("load --input", self.log.read_text(encoding="utf-8"))

    def test_refuses_to_remove_container_owned_by_another_fixture(self) -> None:
        (self.state / "container").touch()

        result = self.run_fixture("stop", ADDP_TEST_OWNER="another-owner")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not owned", result.stderr)
        self.assertTrue((self.state / "container").exists())

    def test_removes_owned_container_even_after_it_has_stopped(self) -> None:
        (self.state / "container").touch()

        result = self.run_fixture("stop", ADDP_TEST_RUNNING="false")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse((self.state / "container").exists())


if __name__ == "__main__":
    unittest.main()
