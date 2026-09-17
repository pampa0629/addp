#!/usr/bin/env python3
"""Verify disposable FalkorDB gate policy without running Docker or Go."""
import os
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import unittest

SCRIPT = Path(__file__).with_name("ontology-falkor-gate.sh")
REPOSITORY = SCRIPT.resolve().parents[2]


class FalkorGateTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-falkor-gate-policy-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        target = self.root / "scripts/test/ontology-falkor-gate.sh"
        target.parent.mkdir(parents=True)
        shutil.copyfile(SCRIPT, target)
        (self.root / "ontology/backend").mkdir(parents=True)
        fake_bin = self.root / "fake-bin"
        fake_bin.mkdir()
        programs = {
            "docker": '''#!/bin/bash
echo "docker $*" >> "$TEST_INVOCATIONS"
case "$*" in
    *"--force-recreate"*) exit "${TEST_RECREATE_EXIT:-0}" ;;
    *"up -d"*) exit "${TEST_START_EXIT:-0}" ;;
    *"--raw SAVE"*) echo "${TEST_SAVE_RESULT:-OK}" ;;
    *"--raw GRAPH.RO_QUERY"*) echo ontology-infra-recovered ;;
    *"env -u REDISCLI_AUTH"*) echo 'NOAUTH Authentication required.' ;;
    *"port falkordb"*) echo "${TEST_PORT:-127.0.0.1:23456}" ;;
    *"down --volumes --remove-orphans"*) exit "${TEST_CLEANUP_EXIT:-0}" ;;
    "ps -aq"*) [ "${TEST_RESIDUAL:-0}" = 0 ] || echo residual ;;
esac
exit 0
''',
            "go": '''#!/bin/bash
echo "go $*" >> "$TEST_INVOCATIONS"
[ "$ONTOLOGY_FALKOR_TEST_ADDRESS" = '127.0.0.1:23456' ] || exit 2
[ "${#ONTOLOGY_FALKOR_TEST_PASSWORD}" = 32 ] || exit 2
[ "$INFRA_FALKORDB_PASSWORD" = "$ONTOLOGY_FALKOR_TEST_PASSWORD" ] || exit 2
[ "$ADDP_ONTOLOGY_FALKOR_INTEGRATION" = 1 ] || exit 2
if [ "${TEST_MODE:-}" = skip ]; then echo '--- SKIP: fixture'; fi
if [ "${TEST_MODE:-}" = pause ]; then sleep 0.5; fi
exit "${TEST_MAIN_EXIT:-0}"
''',
        }
        for name, content in programs.items():
            path = fake_bin / name
            path.write_text(content)
            path.chmod(0o700)
        self.log = self.root / "invocations.log"
        self.command = ["bash", str(target)]
        self.env = dict(os.environ, PATH=str(fake_bin) + os.pathsep + os.environ["PATH"],
                        TEST_INVOCATIONS=str(self.log),
                        ONTOLOGY_FALKOR_TEST_ADDRESS="production:6379",
                        INFRA_FALKORDB_PASSWORD="inherited-infra-not-allowed",
                        ONTOLOGY_FALKOR_TEST_PASSWORD="inherited-not-allowed")

    def test_exit_paths_cleanup_and_reject_residuals(self):
        for extra, success in (({}, True), ({"TEST_MAIN_EXIT": "1"}, False),
                               ({"TEST_MODE": "skip"}, False), ({"TEST_START_EXIT": "1"}, False),
                               ({"TEST_RECREATE_EXIT": "1"}, False), ({"TEST_SAVE_RESULT": "ERR"}, False),
                               ({"TEST_CLEANUP_EXIT": "1"}, False), ({"TEST_RESIDUAL": "1"}, False),
                               ({"TEST_PORT": "0.0.0.0:6379"}, False)):
            with self.subTest(extra=extra):
                result = subprocess.run(self.command, env=dict(self.env, **extra), capture_output=True, text=True, timeout=10)
                self.assertEqual(success, result.returncode == 0, result.stdout + result.stderr)
                log = self.log.read_text()
                self.assertIn("down --volumes --remove-orphans", log)
                self.assertIn("--env-file /dev/null", log)
                self.assertNotIn("production", log)
                self.log.unlink()

    def test_term_cleans_owned_project_and_is_not_success(self):
        process = subprocess.Popen(self.command, env=dict(self.env, TEST_MODE="pause"),
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            if self.log.exists() and "go " in self.log.read_text():
                break
            time.sleep(0.01)
        else:
            process.kill()
            process.communicate()
            self.fail("fake test did not start")
        process.terminate()
        process.communicate(timeout=10)
        self.assertEqual(143, process.returncode)
        self.assertIn("down --volumes --remove-orphans", self.log.read_text())


class InfraContractTest(unittest.TestCase):
    def render(self, path, password="contract-test-only"):
        result = subprocess.run(
            ["docker", "compose", "--env-file", str(REPOSITORY / ".env.example"),
             "-f", str(REPOSITORY / path), "config", "--format", "json"],
            env=dict(os.environ, INFRA_FALKORDB_PASSWORD=password),
            capture_output=True, text=True, timeout=30,
        )
        if password:
            self.assertEqual(0, result.returncode, result.stderr)
            return json.loads(result.stdout)
        self.assertNotEqual(0, result.returncode)

    def test_shared_service_boundary_and_volume(self):
        infra = self.render("docker-compose.infra.yml")["services"]["falkordb"]
        test = self.render("scripts/test/docker-compose.ontology-falkor-t2.yml")["services"]["falkordb"]
        for key in ("image", "entrypoint", "command", "environment", "healthcheck", "mem_limit", "cpus"):
            self.assertEqual(infra[key], test[key], key)
        self.assertIn("@sha256:", infra["image"])
        self.assertEqual(["redis-server"], infra["entrypoint"])
        self.assertEqual("contract-test-only", infra["environment"]["REDISCLI_AUTH"])
        self.assertEqual("/data", infra["volumes"][0]["target"])
        self.assertEqual("volume", infra["volumes"][0]["type"])
        self.assertEqual("127.0.0.1", infra["ports"][0]["host_ip"])
        self.assertEqual("16479", infra["ports"][0]["published"])
        self.assertEqual(1, len(infra["ports"]))
        declaration = next(line for line in SCRIPT.read_text().splitlines()
                           if line.startswith("# ADDP_T2_INPUT_FILES="))
        inputs = declaration.split("=", 1)[1].split()
        self.assertIn("scripts/infra/falkordb.yml", inputs)
        self.assertIn("docker-compose.infra.yml", inputs)
        for path in inputs:
            self.assertTrue((REPOSITORY / path).is_file(), path)
        self.assertNotIn("addp-network", test["networks"])
        self.assertNotIn("container_name", test)
        for flag, value in (("TIMEOUT_MAX", "2000"), ("TIMEOUT_DEFAULT", "2000"),
                            ("--save", "60 1"), ("--dir", "/data"),
                            ("--maxmemory-policy", "noeviction")):
            self.assertEqual(value, infra["command"][infra["command"].index(flag) + 1])
        self.render("docker-compose.infra.yml", password="")

    def test_production_secret_generation_and_fail_closed_validation(self):
        with tempfile.TemporaryDirectory(prefix="addp-infra-secret-test-") as directory:
            root = Path(directory)
            shutil.copyfile(REPOSITORY / ".env.example", root / ".env.example")
            binary = root / "bin"
            binary.mkdir()
            docker = binary / "docker"
            docker.write_text("#!/bin/sh\nexit 1\n")
            docker.chmod(0o700)
            env = dict(os.environ, PATH=str(binary) + os.pathsep + os.environ["PATH"])
            command = ["bash", str(REPOSITORY / "scripts/prod/setup-env.sh")]
            result = subprocess.run(command, cwd=root, env=env, capture_output=True, text=True, timeout=30)
            self.assertEqual(0, result.returncode, result.stdout + result.stderr)
            baseline = (root / ".env").read_text()
            values = dict(line.split("=", 1) for line in baseline.splitlines() if "=" in line and not line.startswith("#"))
            secret = values["INFRA_FALKORDB_PASSWORD"]
            self.assertEqual(24, len(secret))
            self.assertNotEqual(secret, values["REDIS_PASSWORD"])
            self.assertNotIn(secret, result.stdout + result.stderr)
            for invalid in ("", "addp_falkordb", values["REDIS_PASSWORD"]):
                current = baseline.replace("INFRA_FALKORDB_PASSWORD=" + secret, "INFRA_FALKORDB_PASSWORD=" + invalid)
                (root / ".env").write_text(current)
                result = subprocess.run(command, cwd=root, env=env, capture_output=True, text=True, timeout=30)
                self.assertNotEqual(0, result.returncode, "invalid production secret was accepted")
                self.assertEqual(current, (root / ".env").read_text())


if __name__ == "__main__":
    unittest.main()
