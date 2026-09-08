#!/usr/bin/env python3

from __future__ import annotations

import json
import os
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("opengauss-official-media-release-gate.sh")


class OpenGaussOfficialMediaReleaseGateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-opengauss-gate-")
        self.root = Path(self.temporary.name)
        self.repository = self.root / "repository"
        self.script = self.repository / "scripts/test/opengauss-official-media-release-gate.sh"
        self.script.parent.mkdir(parents=True)
        self.script.write_text(SCRIPT.read_text(encoding="utf-8"), encoding="utf-8")
        (self.repository / "common/engine/certification/opengauss").mkdir(parents=True)
        self.fake_bin = self.root / "bin"
        self.fake_bin.mkdir()
        self.state = self.root / "state"
        self.state.mkdir()
        self.tmpdir = self.root / "tmp"
        self.tmpdir.mkdir()
        self.artifacts = self.root / "artifacts"
        self.artifacts.mkdir()
        self.report = self.artifacts / "opengauss-official-media.json"
        self.command_log = self.state / "commands.log"
        self.sha_log = self.state / "sha.log"
        self.go_environment_log = self.state / "go-environment.log"
        self._write_fake_commands()

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def _write_executable(self, name: str, content: str) -> None:
        path = self.fake_bin / name
        path.write_text(textwrap.dedent(content).lstrip(), encoding="utf-8")
        path.chmod(0o755)

    def _write_fake_commands(self) -> None:
        self._write_executable(
            "uname",
            """
            #!/usr/bin/env bash
            if [ "$1" = "-s" ]; then
                printf '%s\n' "${FAKE_UNAME_S:-Linux}"
            else
                printf '%s\n' "${FAKE_UNAME_M:-x86_64}"
            fi
            """,
        )
        self._write_executable(
            "curl",
            """
            #!/usr/bin/env bash
            printf 'curl %s\n' "$*" >> "$FAKE_COMMAND_LOG"
            output=
            previous=
            for argument in "$@"; do
                if [ "$previous" = "--output" ]; then
                    output=$argument
                    break
                fi
                previous=$argument
            done
            : > "$output"
            """,
        )
        self._write_executable(
            "sha256sum",
            """
            #!/usr/bin/env bash
            printf 'sha256sum %s\n' "$*" >> "$FAKE_COMMAND_LOG"
            tee "$FAKE_SHA_LOG" >/dev/null
            """,
        )
        self._write_executable(
            "go",
            """
            #!/usr/bin/env bash
            printf 'go %s\n' "$*" >> "$FAKE_COMMAND_LOG"
            printf '%s|%s|%s\n' "$PWD" "${ADDP_OPENGAUSS_CERTIFICATION:-UNSET}" "${ADDP_TEST_OPENGAUSS_DSN:-UNSET}" > "$FAKE_GO_ENVIRONMENT_LOG"
            printf '%s\n' '=== RUN   TestOpenGaussOfficialMediaCertification'
            if [ "${FAKE_GO_FAILURE:-0}" = "1" ]; then
                printf '%s\n' '--- FAIL: TestOpenGaussOfficialMediaCertification (0.01s)'
                exit 7
            fi
            printf '%s\n' '--- PASS: TestOpenGaussOfficialMediaCertification (0.01s)'
            printf '%s\n' 'PASS'
            """,
        )
        self._write_executable(
            "docker",
            """
            #!/usr/bin/env bash
            printf 'docker %s\n' "$*" >> "$FAKE_COMMAND_LOG"
            image_state=$FAKE_STATE/image-loaded
            container_state=$FAKE_STATE/container-running

            if [ "$1 $2" = "container inspect" ]; then
                [ "${FAKE_PREEXIST_CONTAINER:-0}" = "1" ] || [ -f "$container_state" ]
                exit
            fi
            if [ "$1 $2" = "image inspect" ] && [ "$3" != "--format" ]; then
                [ "${FAKE_PREEXIST_IMAGE:-0}" = "1" ] || [ -f "$image_state" ]
                exit
            fi
            if [ "$1" = "load" ]; then
                : > "$image_state"
                printf '%s\n' 'Loaded image: opengauss:6.0.6'
                exit
            fi
            if [ "$1 $2 $3" = "image inspect --format" ]; then
                if [ "$4" = "{{.Architecture}}" ]; then
                    printf '%s\n' amd64
                else
                    printf '%s\n' sha256:certified-image-id
                fi
                exit
            fi
            if [ "$1" = "run" ]; then
                : > "$container_state"
                printf '%s\n' certified-container-id
                exit
            fi
            if [ "$1" = "exec" ]; then
                case "$*" in
                    *"SELECT 1"*) printf '%s\n' 1 ;;
                esac
                exit
            fi
            if [ "$1" = "inspect" ]; then
                printf '%s\n' true
                exit
            fi
            if [ "$1" = "port" ]; then
                printf '%s\n' 127.0.0.1:35432
                exit
            fi
            if [ "$1" = "logs" ]; then
                printf '%s\n' 'openGauss fake container log'
                exit
            fi
            if [ "$1 $2" = "rm --force" ]; then
                rm -f "$container_state"
                exit
            fi
            if [ "$1 $2" = "image rm" ]; then
                rm -f "$image_state"
                exit
            fi
            printf 'unexpected docker invocation: %s\n' "$*" >&2
            exit 9
            """,
        )

    def _run(self, **overrides: str) -> subprocess.CompletedProcess[str]:
        environment = dict(os.environ)
        environment.update(
            {
                "PATH": f"{self.fake_bin}:{environment['PATH']}",
                "TMPDIR": str(self.tmpdir),
                "ADDP_OPENGAUSS_CERTIFICATION_REPORT": str(self.report),
                "FAKE_COMMAND_LOG": str(self.command_log),
                "FAKE_SHA_LOG": str(self.sha_log),
                "FAKE_GO_ENVIRONMENT_LOG": str(self.go_environment_log),
                "FAKE_STATE": str(self.state),
            }
        )
        environment.update(overrides)
        return subprocess.run(
            ["bash", str(self.script)],
            cwd=self.repository,
            env=environment,
            capture_output=True,
            text=True,
        )

    def test_certifies_pinned_media_and_cleans_disposable_runtime(self) -> None:
        result = self._run()

        self.assertEqual(0, result.returncode, result.stderr)
        commands = self.command_log.read_text(encoding="utf-8")
        self.assertIn(
            "https://opengauss.obs.cn-south-1.myhuaweicloud.com/6.0.6/"
            "openGauss-Docker-6.0.6-x86_64.tar",
            commands,
        )
        self.assertIn("docker run --detach --name addp-opengauss-official-media-certification --privileged=true", commands)
        self.assertIn("CREATE DATABASE addp_opengauss_disposable DBCOMPATIBILITY 'PG'", commands)
        self.assertIn("docker rm --force addp-opengauss-official-media-certification", commands)
        self.assertIn("docker image rm opengauss:6.0.6", commands)
        self.assertIn(
            "4c50bd0f8884f3c0716872ee7d277bad99cdee3d75193a948c7ae9d4b7dfd14d",
            self.sha_log.read_text(encoding="utf-8"),
        )
        go_environment = self.go_environment_log.read_text(encoding="utf-8")
        self.assertIn("/common|1|host=127.0.0.1 port=35432 user=gaussdb", go_environment)
        self.assertNotIn("AddpGauss606@", commands)
        report = json.loads(self.report.read_text(encoding="utf-8"))
        self.assertEqual("passed", report["result"])
        self.assertEqual("addp.opengauss-official-media-certification/v1", report["schema_version"])
        self.assertEqual("PG", report["database_compatibility"])
        self.assertFalse((self.state / "container-running").exists())
        self.assertFalse((self.state / "image-loaded").exists())

    def test_rejects_non_linux_x86_64_before_downloading(self) -> None:
        result = self._run(FAKE_UNAME_M="arm64")

        self.assertNotEqual(0, result.returncode)
        self.assertIn("requires Linux x86_64", result.stderr)
        self.assertFalse(self.command_log.exists())

    def test_refuses_to_replace_existing_image(self) -> None:
        result = self._run(FAKE_PREEXIST_IMAGE="1")

        self.assertNotEqual(0, result.returncode)
        self.assertIn("refusing to replace existing image: opengauss:6.0.6", result.stderr)
        self.assertNotIn("curl ", self.command_log.read_text(encoding="utf-8"))

    def test_failed_driver_gate_collects_logs_and_cleans_runtime(self) -> None:
        result = self._run(FAKE_GO_FAILURE="1")

        self.assertNotEqual(0, result.returncode)
        self.assertFalse(self.report.exists())
        self.assertEqual(
            "openGauss fake container log\n",
            (self.artifacts / "opengauss-official-media-container.log").read_text(
                encoding="utf-8"
            ),
        )
        self.assertFalse((self.state / "container-running").exists())
        self.assertFalse((self.state / "image-loaded").exists())


if __name__ == "__main__":
    unittest.main()
