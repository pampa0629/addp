import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
KEYS = ("LOKI_READ_TOKEN", "LOKI_WRITE_TOKEN", "LOKI_S3_SECRET_KEY", "LOG_OBSERVER_SERVICE_CLIENT_SECRET")


class InfraRuntimeLogLifecycleTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="addp-infra-log-lifecycle-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / "repository"
        shutil.copytree(ROOT / "scripts/infra", self.repo / "scripts/infra")
        shutil.copytree(ROOT / "scripts/utils", self.repo / "scripts/utils")
        self.bin = self.root / "bin"
        self.bin.mkdir()
        docker = self.bin / "docker"
        docker.write_text("#!/bin/sh\nexit 44\n")
        docker.chmod(0o755)
        self.secrets = self.root / "secrets"
        self.secrets.mkdir(mode=0o700)
        self.envfile = self.secrets / "runtime.env"

    def run_up(self, **values):
        env = dict(os.environ)
        for key in (*KEYS, "LOKI_S3_ACCESS_KEY", "ADDP_HOST_NODE_NAME", "ADDP_RUNTIME_LOG_OWNER", "ADDP_ONLINE_ENV_FILE"):
            env.pop(key, None)
        env.update(PATH=f"{self.bin}:{env['PATH']}", ENV="development", INFRA_FALKORDB_PASSWORD="graph-test", REDIS_PASSWORD="redis-test")
        env.update(values)
        return subprocess.run(["bash", "scripts/infra/up.sh"], cwd=self.repo, env=env,
                              capture_output=True, text=True, timeout=10)

    def test_online_initialization_never_creates_root_env_and_preserves_existing_secrets(self):
        values = dict(ADDP_ONLINE_HOST="1", ADDP_ONLINE_ENV_FILE=str(self.envfile))
        result = self.run_up(**values)
        self.assertNotEqual(result.returncode, 0)  # Deliberately stop before Docker startup.
        self.assertFalse((self.repo / ".env").exists(), result.stdout + result.stderr)
        self.assertTrue(self.envfile.exists())
        first = self.envfile.read_bytes()
        modified = self.envfile.stat().st_mtime_ns
        self.assertEqual(self.envfile.stat().st_mode & 0o777, 0o600)
        for line in first.decode().splitlines():
            if not line.strip():
                continue
            key, value = line.split("=", 1)
            if key in KEYS:
                self.assertEqual(len(value), 64)
                self.assertNotIn(value, result.stdout + result.stderr)
        self.run_up(**values)
        self.assertEqual(self.envfile.read_bytes(), first)
        self.assertEqual(self.envfile.stat().st_mtime_ns, modified)

    def test_online_rejects_repository_secret_path_before_writing(self):
        result = self.run_up(ADDP_ONLINE_HOST="1", ADDP_ONLINE_ENV_FILE=str(self.repo / ".env"))
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.repo / ".env").exists())
        self.assertIn("outside", result.stderr)

    def test_online_rejects_secret_file_inside_artifacts(self):
        result = self.run_up(ADDP_ONLINE_HOST="1", ADDP_ONLINE_ENV_FILE=str(self.envfile),
                             ADDP_ONLINE_ARTIFACT_DIR=str(self.secrets))
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.envfile.exists())
        self.assertIn("artifacts", result.stderr)

    def test_online_requires_explicit_external_file(self):
        result = self.run_up(ADDP_ONLINE_HOST="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.repo / ".env").exists())
        self.assertIn("ADDP_ONLINE_ENV_FILE", result.stderr)

    def test_development_initializes_once_and_prepares_host_owned_log_parents(self):
        result = self.run_up()
        self.assertNotEqual(result.returncode, 0)
        envfile = self.repo / ".env"
        self.assertTrue(envfile.exists())
        first = envfile.read_bytes()
        for name in ("logs", "logs/runtime"):
            path = self.repo / name
            self.assertTrue(path.is_dir(), name)
            self.assertEqual(path.stat().st_uid, os.getuid())
            self.assertEqual(path.stat().st_mode & 0o777, 0o700)
        self.run_up()
        self.assertEqual(envfile.read_bytes(), first)

    def test_complete_environment_does_not_create_an_env_file(self):
        values = {key: "a" * 64 for key in KEYS}
        values.update(LOKI_WRITE_TOKEN="b" * 64, LOKI_S3_ACCESS_KEY="log-account", ADDP_HOST_NODE_NAME="owned-host")
        result = self.run_up(**values)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.repo / ".env").exists())

    def test_online_rejects_existing_root_env_without_changing_it(self):
        envfile = self.repo / ".env"
        envfile.write_text("POSTGRES_DB=personal\n")
        result = self.run_up(ADDP_ONLINE_HOST="1", ADDP_ONLINE_ENV_FILE=str(self.envfile))
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(envfile.read_text(), "POSTGRES_DB=personal\n")
        self.assertFalse(self.envfile.exists())

    def test_directory_owner_mismatch_fails_before_container_start(self):
        result = self.run_up(ADDP_RUNTIME_LOG_OWNER="11001:11001")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must match source directory ownership", result.stderr)

    def test_force_does_not_authorize_personal_volume_deletion(self):
        docker = self.bin / "docker"
        docker.write_text("#!/bin/sh\nif [ \"$1 $2\" = \"compose version\" ]; then exit 0; fi\nexit 99\n")
        result = subprocess.run(["bash", "scripts/infra/down.sh", "--volumes", "--force"], cwd=self.repo,
                                env=dict(os.environ, PATH=f"{self.bin}:{os.environ['PATH']}",
                                         GITHUB_ACTIONS="false", ADDP_ONLINE_HOST="0"),
                                capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("禁止删除", result.stderr)


if __name__ == "__main__":
    unittest.main()
