"""Safety contract for the local Infra backup and isolated restore drill."""

import importlib.util
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


SCRIPT = Path(__file__).resolve().parents[1] / "infra" / "backup.py"
SPEC = importlib.util.spec_from_file_location("addp_infra_backup", SCRIPT)
BACKUP = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BACKUP)
CLOUD_SCRIPT = SCRIPT.with_name("backup-to-netdisk.py")
CLOUD_SPEC = importlib.util.spec_from_file_location("addp_infra_cloud_backup", CLOUD_SCRIPT)
CLOUD = importlib.util.module_from_spec(CLOUD_SPEC)
with patch.dict(sys.modules, {"backup": BACKUP}):
    CLOUD_SPEC.loader.exec_module(CLOUD)


class InfraBackupSafetyTest(unittest.TestCase):
    def test_backup_root_rejects_repository_and_symlinks(self):
        with self.assertRaises(BACKUP.BackupError):
            BACKUP.private_directory(BACKUP.ROOT, create=False)
        candidate = BACKUP.ROOT / ".backup-safety-test-must-not-exist"
        with self.assertRaises(BACKUP.BackupError):
            BACKUP.private_directory(candidate, create=True)
        self.assertFalse(candidate.exists())
        with tempfile.TemporaryDirectory() as parent:
            safe = Path(parent) / "safe"
            safe.mkdir(mode=0o700)
            alias = Path(parent) / "alias"
            alias.symlink_to(safe)
            with self.assertRaises(BACKUP.BackupError):
                BACKUP.private_directory(alias, create=False)
            os.chmod(safe, 0o755)
            with self.assertRaises(BACKUP.BackupError):
                BACKUP.private_directory(safe, create=False)

    def test_manifest_hashes_every_file_including_nested_manifest_name(self):
        with tempfile.TemporaryDirectory() as parent:
            root = Path(parent) / "backup"
            root.mkdir(mode=0o700)
            (root / "minio").mkdir()
            (root / "minio" / "manifest.json").write_text("object")
            (root / "postgres.dump").write_bytes(b"snapshot")
            files = BACKUP.files_under(root)
            self.assertEqual({"minio/manifest.json", "postgres.dump"}, set(files))
            manifest = {"format": "addp-infra-local-backup/v1", "files": files}
            (root / "manifest.json").write_text(json.dumps(manifest))
            self.assertEqual(manifest, BACKUP.validate(root))
            (root / "postgres.dump").write_bytes(b"changed")
            with self.assertRaises(BACKUP.BackupError):
                BACKUP.validate(root)

    def test_incomplete_backup_is_not_a_restore_source(self):
        with tempfile.TemporaryDirectory() as parent:
            root = Path(parent) / "backup.incomplete"
            root.mkdir(mode=0o700)
            with self.assertRaises(BACKUP.BackupError):
                BACKUP.validate(root)

    def test_foreign_source_is_rejected_before_backup(self):
        info = {"Config": {"Labels": {"com.docker.compose.project": "business",
                                      "com.docker.compose.service": "postgres",
                                      "com.docker.compose.project.working_dir": str(BACKUP.ROOT)}},
                "State": {"Running": True}}
        with patch.object(BACKUP, "inspect", return_value=info):
            with self.assertRaises(BACKUP.BackupError):
                BACKUP.owned_source("addp-postgres", "postgres")

    def test_cleanup_refuses_an_unowned_container(self):
        payload = [{"Config": {"Labels": {"addp.restore-drill": "another-run"}}}]
        completed = type("Completed", (), {"returncode": 0, "stdout": json.dumps(payload).encode()})()
        with patch.object(BACKUP.subprocess, "run", return_value=completed), \
                patch.object(BACKUP, "run") as guarded_run:
            with self.assertRaises(BACKUP.BackupError):
                BACKUP.cleanup_owned("foreign-container", "this-run")
            guarded_run.assert_not_called()

    def test_bucket_listing_rejects_unsafe_names(self):
        good = b'{"status":"success","key":"manager/"}\n'
        self.assertEqual(["manager"], BACKUP.bucket_names(good))
        with self.assertRaises(BACKUP.BackupError):
            BACKUP.bucket_names(b'{"status":"success","key":"../"}\n')

    def test_postgres_wait_ignores_the_temporary_initialization_server(self):
        observed = []

        def fake_run(args, **_kwargs):
            observed.append(args[1])
            if args[1] == "logs":
                content = (b"temporary server is ready" if observed.count("logs") == 1
                           else b"PostgreSQL init process complete; ready for start up.")
                return type("Completed", (), {"returncode": 0, "stdout": content,
                                               "stderr": b""})()
            return type("Completed", (), {"returncode": 0, "stdout": b"",
                                           "stderr": b""})()

        with patch.object(BACKUP.subprocess, "run", side_effect=fake_run), \
                patch.object(BACKUP.time, "sleep"):
            BACKUP.wait_postgres_initialized("isolated-pg")
        self.assertEqual(["logs", "logs", "exec"], observed)

    def test_cloud_recipient_must_be_an_age_public_key(self):
        with tempfile.TemporaryDirectory() as parent:
            key = Path(parent) / "recipient.txt"
            for value in ("", "password", "age1" + "q" * 57, "age1" + "q" * 59):
                key.write_text(value)
                with self.assertRaises(BACKUP.BackupError):
                    CLOUD.recipient(key)
            valid = "age1" + "q" * 58
            key.write_text(valid + "\n")
            self.assertEqual(valid, CLOUD.recipient(key))


if __name__ == "__main__":
    unittest.main()
