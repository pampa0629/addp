"""Safety contract for the local Infra backup and isolated restore drill."""

import importlib.util
import io
import json
import os
import sys
import tarfile
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


IMAGE = "sha256:" + "a" * 64
TASK_TIME = "2026-10-05T00:00:00.123456789Z"


def write_dump(path, *, status="succeeded", extra=None, missing=None, task_uid=0):
    entries = {
        "metadata.json": {"dumpVersion": "V6", "dbVersion": "1.7.6", "dumpDate": TASK_TIME},
        "indexes/example/metadata.json": {"uid": "example", "primaryKey": "id",
                                           "createdAt": TASK_TIME, "updatedAt": TASK_TIME},
        "indexes/example/settings.json": {"filterableAttributes": ["tenant_id"], "embedders": None},
    }
    raw = {name: json.dumps(value).encode() for name, value in entries.items()}
    raw["indexes/example/documents.jsonl"] = b'{"id":9007199254740993,"tenant_id":1,"title":"example"}\n'
    raw["keys.jsonl"] = b""
    raw["tasks/queue.jsonl"] = (json.dumps({"uid": task_uid, "status": status,
        "type": {"dumpCreation": {"keys": [], "instance_uid": None}},
        "enqueuedAt": TASK_TIME}) + "\n").encode()
    if extra:
        raw.update(extra)
    raw.pop(missing, None)
    with tarfile.open(path, "w:gz") as archive:
        for directory in (".", "./indexes/", "./indexes/example/", "./tasks/"):
            member = tarfile.TarInfo(directory)
            member.type = tarfile.DIRTYPE
            archive.addfile(member)
        for name, content in raw.items():
            if isinstance(content, tarfile.TarInfo):
                archive.addfile(content)
                continue
            member = tarfile.TarInfo("./" + name)
            member.size = len(content)
            archive.addfile(member, io.BytesIO(content))
    os.chmod(path, 0o600)


def dump_evidence(path):
    return {"file": "meilisearch.dump", "sha256": BACKUP.digest(path),
            "source_image": IMAGE, "source_version": "1.7.6", "dump_uid": "export-1",
            "dump_task_uid": 0, "dump_task_enqueued_at": TASK_TIME, "indexes": 1, "tasks": 1}


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

    def test_v6_inventory_preserves_uid_zero_large_ids_and_nano_time(self):
        with tempfile.TemporaryDirectory() as parent:
            path = Path(parent) / "export.dump"
            write_dump(path)
            inventory = BACKUP.dump_inventory(path)
        self.assertEqual(0, inventory["tasks"][0]["uid"])
        self.assertEqual(TASK_TIME, inventory["tasks"][0]["enqueuedAt"])
        self.assertIsNone(inventory["tasks"][0]["customMetadata"])
        self.assertEqual(BACKUP.document_set_hash([BACKUP.document_hash({
            "id": 9007199254740993, "tenant_id": 1, "title": "example"})]),
            inventory["indexes"]["example"]["documents.jsonl"]["sha256"])

    def test_dump_rejects_unfinished_duplicate_invalid_tasks_and_incomplete_indexes(self):
        with tempfile.TemporaryDirectory() as parent:
            path = Path(parent) / "export.dump"
            for status in ("enqueued", "processing", "unknown"):
                write_dump(path, status=status)
                with self.assertRaises(BACKUP.BackupError):
                    BACKUP.dump_inventory(path)
            for uid in (True, -1, "0"):
                write_dump(path, task_uid=uid)
                with self.assertRaises(BACKUP.BackupError):
                    BACKUP.dump_inventory(path)
            for missing in ("metadata.json", "keys.jsonl", "tasks/queue.jsonl", "indexes/example/settings.json"):
                write_dump(path, missing=missing)
                with self.assertRaises(BACKUP.BackupError):
                    BACKUP.dump_inventory(path)
            duplicate = (json.dumps({"uid": 0, "status": "succeeded", "type": "dumpCreation",
                                    "enqueuedAt": TASK_TIME}) + "\n").encode() * 2
            write_dump(path, extra={"tasks/queue.jsonl": duplicate})
            with self.assertRaises(BACKUP.BackupError):
                BACKUP.dump_inventory(path)

    def test_dump_rejects_traversal_links_duplicates_and_truncated_gzip(self):
        with tempfile.TemporaryDirectory() as parent:
            path = Path(parent) / "export.dump"
            link = tarfile.TarInfo("./linked")
            link.type = tarfile.SYMTYPE
            link.linkname = "/etc/passwd"
            for extra in ({"../escape": b"bad"}, {"linked": link}, {"./metadata.json": b"{}"}):
                write_dump(path, extra=extra)
                with self.assertRaises(BACKUP.BackupError):
                    BACKUP.dump_inventory(path)
            write_dump(path)
            path.write_bytes(path.read_bytes()[:-8])
            with self.assertRaises((BACKUP.BackupError, EOFError, tarfile.TarError)):
                BACKUP.dump_inventory(path)

    def test_dump_rejects_unsupported_format_and_duplicate_json_keys(self):
        with tempfile.TemporaryDirectory() as parent:
            path = Path(parent) / "export.dump"
            for content in (b'{"dumpVersion":"V5","dbVersion":"1.7.6"}',
                            b'{"dumpVersion":"V6","dumpVersion":"V6","dbVersion":"1.7.6"}'):
                write_dump(path, extra={"metadata.json": content})
                with self.assertRaises(BACKUP.BackupError):
                    BACKUP.dump_inventory(path)

    def test_meili_reads_keep_key_out_of_arguments(self):
        key = "private-test-key-do-not-log"
        completed = type("Completed", (), {"stdout": b'{"status":"available"}'})()
        with patch.object(BACKUP, "run", return_value=completed) as execute:
            self.assertEqual({"status": "available"}, BACKUP.meili_get("owned", "/health", key))
        args, kwargs = execute.call_args
        self.assertNotIn(key, " ".join(args[0]))
        self.assertIn(key.encode(), kwargs["input"])
        self.assertNotIn("POST", args[0])
        with self.assertRaises(BACKUP.BackupError):
            BACKUP.meili_get("owned", "/health", "bad\nkey")

    def test_collect_requires_matching_owned_successful_export_and_never_exports(self):
        with tempfile.TemporaryDirectory() as parent:
            root = Path(parent)
            source = root / "export.dump"
            destination = root / "collected.dump"
            write_dump(source)
            key = "private-source-key-do-not-copy"
            info = {"Image": IMAGE, "Config": {"Env": ["MEILI_MASTER_KEY=" + key]}}
            task = {"uid": 0, "type": "dumpCreation", "status": "succeeded",
                    "details": {"dumpUid": "export-1"}, "enqueuedAt": TASK_TIME}
            completed = type("Completed", (), {"stdout": (BACKUP.digest(source) + "  export\n").encode()})()
            with patch.object(BACKUP, "owned_source", return_value=info) as owned, \
                    patch.object(BACKUP, "meili_get", side_effect=[task, {"pkgVersion": "1.7.6"}]) as get, \
                    patch.object(BACKUP, "run", return_value=completed) as execute:
                evidence = BACKUP.collect_meili_dump(source, 0, destination)
                owned.assert_called_once_with("addp-meilisearch", "meilisearch")
                self.assertEqual(["/tasks/0", "/version"], [call.args[1] for call in get.call_args_list])
                self.assertEqual("sha256sum", execute.call_args.args[0][3])
                self.assertEqual(source.read_bytes(), destination.read_bytes())
                self.assertEqual(0o600, destination.stat().st_mode & 0o777)
                self.assertNotIn(key, json.dumps(evidence))
            for invalid in ({**task, "status": "enqueued"}, {**task, "uid": 1},
                            {**task, "uid": False}, {**task, "details": []},
                            {**task, "details": {"dumpUid": "../escape"}}):
                with patch.object(BACKUP, "owned_source", return_value=info), \
                        patch.object(BACKUP, "meili_get", return_value=invalid), \
                        patch.object(BACKUP, "run") as execute:
                    with self.assertRaises(BACKUP.BackupError):
                        BACKUP.collect_meili_dump(source, 0, destination)
                    execute.assert_not_called()
            for output in (b"0" * 64, b"", b"invalid hash"):
                with patch.object(BACKUP, "owned_source", return_value=info), \
                        patch.object(BACKUP, "meili_get", return_value=task), \
                        patch.object(BACKUP, "run", return_value=type("Completed", (), {"stdout": output})()):
                    with self.assertRaises(BACKUP.BackupError):
                        BACKUP.collect_meili_dump(source, 0, destination)

    def test_meili_collection_rejects_public_or_symlink_input_before_docker(self):
        with tempfile.TemporaryDirectory() as parent:
            root = Path(parent)
            source = root / "export.dump"
            write_dump(source)
            alias = root / "alias.dump"
            alias.symlink_to(source)
            with patch.object(BACKUP, "owned_source") as owned:
                with self.assertRaises(BACKUP.BackupError):
                    BACKUP.collect_meili_dump(alias, 0, root / "out")
                os.chmod(source, 0o644)
                with self.assertRaises(BACKUP.BackupError):
                    BACKUP.collect_meili_dump(source, 0, root / "out")
                owned.assert_not_called()

    def test_meili_manifest_rejects_scope_and_export_receipt_conflicts(self):
        with tempfile.TemporaryDirectory() as parent:
            root = Path(parent)
            dump = root / "meilisearch.dump"
            write_dump(dump)
            evidence = dump_evidence(dump)
            manifest = {"format": "addp-infra-local-backup/v1", "meilisearch": evidence,
                        "files": BACKUP.files_under(root)}
            (root / "manifest.json").write_text(json.dumps(manifest))
            self.assertEqual(manifest, BACKUP.validate(root))
            for changed in ({**manifest, "meilisearch": {**evidence, "dump_task_uid": 1}},
                            {**manifest, "meilisearch": {**evidence, "dump_task_uid": False}},
                            {**manifest, "meilisearch": []},
                            {**manifest, "meilisearch": {**evidence, "source_version": "1.54.3"}},
                            {key: value for key, value in manifest.items() if key != "meilisearch"}):
                (root / "manifest.json").write_text(json.dumps(changed))
                with self.assertRaises(BACKUP.BackupError):
                    BACKUP.validate(root)

    def test_create_requires_both_dump_arguments_before_source_operations(self):
        with patch.object(BACKUP, "owned_source") as owned:
            for args in ({"meilisearch_dump": Path("/tmp/dump")}, {"meilisearch_dump_task": 0}):
                with self.assertRaises(BACKUP.BackupError):
                    BACKUP.create(Path("/tmp/unused"), **args)
            owned.assert_not_called()

    def test_meili_restore_checks_contents_settings_primary_key_and_all_receipts(self):
        with tempfile.TemporaryDirectory() as parent:
            path = Path(parent) / "export.dump"
            write_dump(path)
            inventory = BACKUP.dump_inventory(path)
        responses = {
            "/indexes?limit=1000&offset=0": {"results": [{"uid": "example", "primaryKey": "id"}], "total": 1},
            "/indexes/example/settings": {"filterableAttributes": ["tenant_id"], "newDefault": True},
            "/indexes/example/documents?limit=100&offset=0": {
                "results": [{"id": 9007199254740993, "tenant_id": 1, "title": "example"}], "total": 1},
            "/tasks?limit=1000": {"total": 1, "results": inventory["tasks"], "next": None},
        }
        with patch.object(BACKUP, "meili_get", side_effect=lambda _name, route, _key: responses[route]):
            BACKUP.verify_meili_restore("owned", "key", inventory)
        for route, changed in (
            ("/indexes?limit=1000&offset=0", {"results": [], "total": 1}),
            ("/indexes?limit=1000&offset=0", {"results": [None], "total": 1}),
            ("/indexes?limit=1000&offset=0", {"results": [{"uid": "other"}], "total": 1}),
            ("/indexes?limit=1000&offset=0", {"results": [{"uid": "example", "primaryKey": "wrong"}], "total": 1}),
            ("/indexes/example/settings", {"filterableAttributes": []}),
            ("/indexes/example/documents?limit=100&offset=0", {"results": [{"id": 9007199254740992}], "total": 1}),
            ("/indexes/example/documents?limit=100&offset=0", {"results": [], "total": 1}),
            ("/tasks?limit=1000", {"total": 1, "results": [
                {**inventory["tasks"][0], "enqueuedAt": "2026-10-05T00:00:00Z"}], "next": None}),
            ("/tasks?limit=1000", {"total": 1, "results": [
                {**inventory["tasks"][0], "customMetadata": "invented"}], "next": None}),
            ("/tasks?limit=1000", {"total": 0, "results": [], "next": None}),
        ):
            current = {**responses, route: changed}
            with patch.object(BACKUP, "meili_get", side_effect=lambda _name, request, _key: current[request]):
                with self.assertRaises(BACKUP.BackupError):
                    BACKUP.verify_meili_restore("owned", "key", inventory)

    def test_meili_drill_is_private_readonly_and_uses_new_key(self):
        with tempfile.TemporaryDirectory() as parent:
            root = Path(parent)
            path = root / "meilisearch.dump"
            write_dump(path)
            evidence = dump_evidence(path)
            responses = [{"status": "available"}, {"pkgVersion": "1.7.6"}]
            with patch.object(BACKUP, "run") as execute, \
                    patch.object(BACKUP, "inspect", return_value={"State": {"Running": True}}), \
                    patch.object(BACKUP, "meili_get", side_effect=responses), \
                    patch.object(BACKUP, "verify_meili_restore") as verify, \
                    patch("builtins.print"):
                BACKUP.drill_meili(root, evidence, "owned-run", "token", None)
            args = execute.call_args.args[0]
            self.assertIn("addp.restore-drill=token", args)
            self.assertEqual("none", args[args.index("--network") + 1])
            self.assertTrue(any("dst=/backup/meilisearch.dump,readonly" in value for value in args))
            self.assertNotIn("-p", args)
            self.assertNotIn("--volume", args)
            self.assertEqual(IMAGE, args[args.index("/bin/meilisearch") - 1])
            key = execute.call_args.kwargs["env"]["MEILI_MASTER_KEY"]
            self.assertNotIn(key, args)
            self.assertEqual(key, verify.call_args.args[1])

    def test_meili_restore_checks_every_document_page_without_order_assumptions(self):
        documents = [{"id": index, "tenant_id": 1} for index in range(103)]
        inventory = {"indexes": {"example": {
            "metadata.json": {"primaryKey": "id"}, "settings.json": {},
            "documents.jsonl": {"count": 103, "sha256": BACKUP.document_set_hash([
                BACKUP.document_hash(value) for value in documents])}}}, "tasks": []}
        reversed_documents = list(reversed(documents))
        responses = {
            "/indexes?limit=1000&offset=0": {"results": [{"uid": "example", "primaryKey": "id"}], "total": 1},
            "/indexes/example/settings": {},
            "/indexes/example/documents?limit=100&offset=0": {"results": reversed_documents[:100], "total": 103},
            "/indexes/example/documents?limit=100&offset=100": {"results": reversed_documents[100:], "total": 103},
            "/tasks?limit=1000": {"total": 0, "results": [], "next": None},
        }
        with patch.object(BACKUP, "meili_get", side_effect=lambda _name, route, _key: responses[route]) as get:
            BACKUP.verify_meili_restore("owned", "key", inventory)
        self.assertIn("/indexes/example/documents?limit=100&offset=100", [call.args[1] for call in get.call_args_list])
        responses["/indexes/example/documents?limit=100&offset=100"]["results"][0] = documents[0]
        with patch.object(BACKUP, "meili_get", side_effect=lambda _name, route, _key: responses[route]):
            with self.assertRaisesRegex(BACKUP.BackupError, "contents differ"):
                BACKUP.verify_meili_restore("owned", "key", inventory)

    def test_meili_restore_rejects_two_ulp_float_drift_with_safe_index_evidence(self):
        import math

        original = {"id": "private-document", "bounds": {"max_x": 0.1234567890123456}}
        changed = {**original, "bounds": {"max_x": math.nextafter(
            math.nextafter(original["bounds"]["max_x"], math.inf), math.inf)}}
        expected_hash = BACKUP.document_set_hash([BACKUP.document_hash(original)])
        actual_hash = BACKUP.document_set_hash([BACKUP.document_hash(changed)])
        inventory = {"indexes": {"example": {
            "metadata.json": {"primaryKey": "id"}, "settings.json": {},
            "documents.jsonl": {"count": 1, "sha256": expected_hash}}}, "tasks": []}
        responses = {
            "/indexes?limit=1000&offset=0": {"results": [{"uid": "example", "primaryKey": "id"}], "total": 1},
            "/indexes/example/settings": {},
            "/indexes/example/documents?limit=100&offset=0": {"results": [changed], "total": 1},
        }
        with patch.object(BACKUP, "meili_get", side_effect=lambda _name, route, _key: responses[route]):
            with self.assertRaises(BACKUP.BackupError) as failure:
                BACKUP.verify_meili_restore("owned", "private-key", inventory)
        message = str(failure.exception)
        self.assertIn("index=example", message)
        self.assertIn("count=1", message)
        self.assertIn(f"expected_sha256={expected_hash}", message)
        self.assertIn(f"actual_sha256={actual_hash}", message)
        self.assertNotIn("private-document", message)
        self.assertNotIn("private-key", message)
        self.assertNotIn(str(original["bounds"]["max_x"]), message)
        self.assertNotIn(str(changed["bounds"]["max_x"]), message)

    def test_meili_task_history_uses_complete_cursor_pages_including_uid_zero(self):
        tasks = [{"uid": uid, "indexUid": None, "type": "dumpCreation", "status": "succeeded",
                  "enqueuedAt": TASK_TIME, "customMetadata": None} for uid in (7, 4, 0)]
        inventory = {"indexes": {}, "tasks": tasks}
        responses = {
            "/indexes?limit=1000&offset=0": {"results": [], "total": 0},
            "/tasks?limit=1000": {"results": tasks[:2], "total": 3, "next": 0},
            "/tasks?limit=1000&from=0": {"results": tasks[2:], "total": 3, "next": None},
        }
        with patch.object(BACKUP, "meili_get", side_effect=lambda _name, route, _key: responses[route]) as get:
            BACKUP.verify_meili_restore("owned", "key", inventory)
        self.assertEqual(list(responses), [call.args[1] for call in get.call_args_list])
        for route, changed in (
            ("/tasks?limit=1000", {"results": tasks[:2], "total": 3, "next": None}),
            ("/tasks?limit=1000", {"results": [], "total": 3, "next": 0}),
            ("/tasks?limit=1000", {"results": tasks[:2], "total": 3, "next": True}),
            ("/tasks?limit=1000", {"results": tasks[:2], "total": 3, "next": -1}),
            ("/tasks?limit=1000", {"results": tasks[:2], "total": 3, "next": 4}),
            ("/tasks?limit=1000", {"results": tasks[:2], "total": 3, "next": "0"}),
            ("/tasks?limit=1000&from=0", {"results": [tasks[0]], "total": 3, "next": None}),
            ("/tasks?limit=1000&from=0", {"results": [None], "total": 3, "next": None}),
            ("/tasks?limit=1000&from=0", {"results": [{**tasks[2], "uid": True}], "total": 3, "next": None}),
            ("/tasks?limit=1000&from=0", {"results": [{**tasks[2], "uid": 1}], "total": 3, "next": None}),
            ("/tasks?limit=1000&from=0", {"results": [{**tasks[2], "status": "processing"}], "total": 3, "next": None}),
            ("/tasks?limit=1000&from=0", {"results": tasks[2:], "total": 2, "next": None}),
            ("/tasks?limit=1000&from=0", {"results": tasks[2:], "total": 3, "next": 0}),
        ):
            current = {**responses, route: changed}
            with self.subTest(route=route, changed=changed), patch.object(
                    BACKUP, "meili_get", side_effect=lambda _name, request, _key: current[request]):
                with self.assertRaises(BACKUP.BackupError):
                    BACKUP.verify_meili_restore("owned", "key", inventory)

    def test_explicit_content_drift_acceptance_keeps_other_checks_required(self):
        with tempfile.TemporaryDirectory() as parent:
            path = Path(parent) / "export.dump"
            write_dump(path)
            inventory = BACKUP.dump_inventory(path)
        responses = {
            "/indexes?limit=1000&offset=0": {"results": [{"uid": "example", "primaryKey": "id"}], "total": 1},
            "/indexes/example/settings": {"filterableAttributes": ["tenant_id"]},
            "/indexes/example/documents?limit=100&offset=0": {
                "results": [{"id": 9007199254740993, "tenant_id": 1, "title": "changed"}], "total": 1},
            "/tasks?limit=1000": {"results": inventory["tasks"], "total": 1, "next": None},
        }
        with patch.object(BACKUP, "meili_get", side_effect=lambda _name, route, _key: responses[route]) as get, \
                patch("builtins.print") as output:
            changed = BACKUP.verify_meili_restore("owned", "key", inventory, accept_content_drift=True)
        self.assertEqual(["example"], changed)
        self.assertIn("/tasks?limit=1000", [call.args[1] for call in get.call_args_list])
        self.assertIn("WARN: accepted", output.call_args.args[0])
        for route, change in (
            ("/indexes?limit=1000&offset=0", {"results": [{"uid": "example", "primaryKey": "wrong"}], "total": 1}),
            ("/indexes/example/settings", {"filterableAttributes": []}),
            ("/indexes/example/documents?limit=100&offset=0", {"results": [], "total": 0}),
            ("/tasks?limit=1000", {"results": [{**inventory["tasks"][0], "status": "processing"}], "total": 1, "next": None}),
        ):
            current = {**responses, route: change}
            with self.subTest(route=route), patch.object(
                    BACKUP, "meili_get", side_effect=lambda _name, request, _key: current[request]), \
                    patch("builtins.print"):
                with self.assertRaises(BACKUP.BackupError):
                    BACKUP.verify_meili_restore("owned", "key", inventory, accept_content_drift=True)

    def test_manual_drift_acceptance_is_explicit_and_not_enabled_by_other_values(self):
        for value, accepted in ((None, False), ("0", False), ("true", False), ("1", True)):
            args = ["make", "-n", "infra-restore-drill", "BACKUP_DIR=/private/backup"]
            if value is not None:
                args.append(f"MEILISEARCH_ACCEPT_CONTENT_DRIFT={value}")
            result = BACKUP.subprocess.run(args, cwd=BACKUP.ROOT, capture_output=True, check=True)
            self.assertEqual(accepted, b"--accept-meilisearch-content-drift" in result.stdout)
        self.assertNotIn("accept_content_drift", CLOUD_SCRIPT.read_text())

    def test_meili_import_stopped_or_wrong_version_cannot_pass_verification(self):
        with tempfile.TemporaryDirectory() as parent:
            root = Path(parent)
            path = root / "meilisearch.dump"
            write_dump(path)
            for running, responses in (
                (False, []), (True, [{"status": "available"}, {"pkgVersion": "1.54.3"}]),
            ):
                with patch.object(BACKUP, "run"), \
                        patch.object(BACKUP, "inspect", return_value={"State": {"Running": running}}), \
                        patch.object(BACKUP, "meili_get", side_effect=responses), \
                        patch.object(BACKUP, "verify_meili_restore") as verify, \
                        patch("builtins.print") as output:
                    with self.assertRaises(BACKUP.BackupError):
                        BACKUP.drill_meili(root, dump_evidence(path), "owned", "token", None)
                    verify.assert_not_called()
                    output.assert_not_called()

    def test_restore_rejects_floating_target_image_before_starting_containers(self):
        with patch.object(BACKUP, "private_directory", return_value=Path("/unused")), \
                patch.object(BACKUP, "validate", return_value={"meilisearch": {}}), \
                patch.object(BACKUP, "run") as execute:
            for image in ("getmeili/meilisearch:latest", "getmeili/meilisearch:v1.54.3", IMAGE):
                with self.assertRaises(BACKUP.BackupError):
                    BACKUP.drill(Path("/unused"), meilisearch_restore_image=image)
            execute.assert_not_called()

    def test_cleanup_checks_absence_and_does_not_treat_inspect_failure_as_absence(self):
        missing = type("Completed", (), {"returncode": 1, "stdout": b""})()
        with patch.object(BACKUP.subprocess, "run", return_value=missing), \
                patch.object(BACKUP, "run", return_value=type("Completed", (), {"stdout": b"still-there\n"})()):
            with self.assertRaises(BACKUP.BackupError):
                BACKUP.cleanup_owned("owned", "token")

    def test_failed_restore_attempts_all_owned_cleanup_even_if_one_cleanup_fails(self):
        manifest = {"source": {"postgres_image": IMAGE}}
        with patch.object(BACKUP, "private_directory", return_value=Path("/unused")), \
                patch.object(BACKUP, "validate", return_value=manifest), \
                patch.object(BACKUP, "run", side_effect=BACKUP.BackupError("restore failed")), \
                patch.object(BACKUP, "cleanup_owned", side_effect=[BACKUP.BackupError("cleanup failed"), None, None]) as cleanup:
            with self.assertRaisesRegex(BACKUP.BackupError, "cleanup not proven"):
                BACKUP.drill(Path("/unused"))
        self.assertEqual(3, cleanup.call_count)
        names = [call.args[0] for call in cleanup.call_args_list]
        self.assertTrue(names[0].startswith("addp-restore-drill-meili-"))
        self.assertTrue(names[1].startswith("addp-restore-drill-minio-"))
        self.assertTrue(names[2].startswith("addp-restore-drill-pg-"))

    def test_cleanup_verifies_no_container_remains_after_removal(self):
        payload = [{"Config": {"Labels": {"addp.restore-drill": "token"}}}]
        completed = type("Completed", (), {"returncode": 0, "stdout": json.dumps(payload).encode()})()
        for remaining in (b"", b"still-there"):
            with patch.object(BACKUP.subprocess, "run", return_value=completed), \
                    patch.object(BACKUP, "run", side_effect=[None, type("Completed", (), {"stdout": remaining})()]) as execute:
                if remaining:
                    with self.assertRaisesRegex(BACKUP.BackupError, "remains after cleanup"):
                        BACKUP.cleanup_owned("owned", "token")
                else:
                    BACKUP.cleanup_owned("owned", "token")
                self.assertEqual(["docker", "rm", "-f", "owned"], execute.call_args_list[0].args[0])

    def test_cleanup_rejects_malformed_ownership_without_removing_anything(self):
        for payload in ([], [{}], None, [{"Config": []}]):
            completed = type("Completed", (), {"returncode": 0, "stdout": json.dumps(payload).encode()})()
            with patch.object(BACKUP.subprocess, "run", return_value=completed), \
                    patch.object(BACKUP, "run") as execute:
                with self.assertRaises(BACKUP.BackupError):
                    BACKUP.cleanup_owned("owned", "token")
                execute.assert_not_called()

    def test_make_parameters_preserve_dump_uid_zero_and_do_not_enable_daily_export(self):
        result = BACKUP.subprocess.run(["make", "-n", "infra-backup", "BACKUP_ROOT=/private/backups",
            "MEILISEARCH_DUMP=/private/export.dump", "MEILISEARCH_DUMP_TASK=0"],
            cwd=BACKUP.ROOT, capture_output=True, check=True)
        self.assertIn(b'--meilisearch-dump-task "0"', result.stdout)
        self.assertIn(b'--meilisearch-dump "/private/export.dump"', result.stdout)
        self.assertNotIn("--meilisearch", CLOUD_SCRIPT.read_text())


if __name__ == "__main__":
    unittest.main()
