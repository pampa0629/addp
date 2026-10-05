#!/usr/bin/env python3
"""Read-only local Infra backup and isolated restore drill."""

from __future__ import annotations

import argparse
import gzip
import hashlib
import json
import os
import re
import secrets
import shutil
import stat
import subprocess
import tarfile
import tempfile
import time
import urllib.parse
from datetime import datetime, timezone
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
BUCKET_RE = re.compile(r"[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]")
COUNTS = {
    "engines": "SELECT count(*) FROM system.engines",
    "meta_items": "SELECT count(*) FROM meta.meta_item",
    "principals": "SELECT count(*) FROM system.principals",
}


class BackupError(RuntimeError):
    pass


def run(args: list[str], *, env: dict[str, str] | None = None,
        stdin=None, stdout=None, input: bytes | None = None,
        timeout: int = 300) -> subprocess.CompletedProcess:
    result = subprocess.run(args, env=env, stdin=stdin, stdout=stdout,
                            input=input, capture_output=stdout is None, timeout=timeout, check=False)
    if result.returncode:
        detail = ""
        if args[:2] == ["docker", "exec"] and result.stderr:
            detail = ": " + result.stderr.decode(errors="replace").splitlines()[0][:240]
        raise BackupError(f"command failed ({result.returncode}): {args[0]} {args[1]}{detail}")
    return result


def inspect(name: str) -> dict:
    data = json.loads(run(["docker", "inspect", name]).stdout)
    if len(data) != 1:
        raise BackupError(f"container not found: {name}")
    return data[0]


def owned_source(name: str, service: str) -> dict:
    info = inspect(name)
    labels = info["Config"].get("Labels") or {}
    if (labels.get("com.docker.compose.project") != "addp-infra" or
            labels.get("com.docker.compose.service") != service or
            labels.get("com.docker.compose.project.working_dir") != str(ROOT) or
            not info["State"]["Running"]):
        raise BackupError(f"{name} is not the running Infra service of this workspace")
    return info


def private_directory(path: Path, *, create: bool) -> Path:
    if not path.is_absolute() or path.is_symlink():
        raise BackupError("backup path must be an absolute non-symlink path")
    destination = path.resolve(strict=False)
    if destination == ROOT or ROOT in destination.parents:
        raise BackupError("backup path must be outside the repository")
    if create:
        path.mkdir(mode=0o700, parents=True, exist_ok=True)
    real = path.resolve(strict=True)
    mode = real.stat().st_mode
    if not stat.S_ISDIR(mode) or mode & 0o077 or real.stat().st_uid != os.getuid():
        raise BackupError("backup directory must be owned by the current user and mode 0700")
    return real


def digest(path: Path) -> str:
    hash_value = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            hash_value.update(chunk)
    return hash_value.hexdigest()


def files_under(path: Path) -> dict[str, dict[str, str | int]]:
    result = {}
    for file in sorted(path.rglob("*")):
        if file.is_symlink():
            raise BackupError(f"symlinks are not allowed in backup: {file}")
        if file.is_file() and file != path / "manifest.json":
            result[file.relative_to(path).as_posix()] = {
                "sha256": digest(file), "size": file.stat().st_size,
            }
    return result


def image_id(info: dict) -> str:
    value = info["Image"]
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", value):
        raise BackupError("source image ID is invalid")
    return value


def mc_env(info: dict, alias: str, host: str) -> dict[str, str]:
    values = dict(item.split("=", 1) for item in info["Config"]["Env"] if "=" in item)
    username = values["MINIO_ROOT_USER"]
    password = values["MINIO_ROOT_PASSWORD"]
    address = (f"http://{urllib.parse.quote(username, safe='')}:"
               f"{urllib.parse.quote(password, safe='')}@{host}:9000")
    return {**os.environ, f"MC_HOST_{alias}": address}


def mc(args: list[str], *, env: dict[str, str], image: str, network: str,
       mounts: list[tuple[Path, str, bool]] | None = None, workdir: str | None = None,
       timeout: int = 300) -> subprocess.CompletedProcess:
    command = ["docker", "run", "--rm", "--network", network]
    for key in env:
        if key.startswith("MC_HOST_"):
            command += ["-e", key]
    for source, target, readonly in mounts or []:
        command += ["--mount", f"type=bind,src={source},dst={target}"
                    + (",readonly" if readonly else "")]
    if workdir:
        command += ["-w", workdir]
    return run(command + [image, *args], env=env, timeout=timeout)


def pg_counts(container: str, database: str) -> dict[str, int]:
    counts = {}
    for name, query in COUNTS.items():
        result = run(["docker", "exec", container, "psql", "-U", "addp", "-d", database,
                      "-Atc", query])
        counts[name] = int(result.stdout.decode().strip())
    return counts


def bucket_names(output: bytes) -> list[str]:
    names = []
    for line in output.splitlines():
        item = json.loads(line)
        name = item.get("key", "").rstrip("/")
        if item.get("status") != "success" or not BUCKET_RE.fullmatch(name):
            raise BackupError("MinIO returned an invalid bucket listing")
        names.append(name)
    return sorted(names)


def json_value(data: bytes):
    def invalid_constant(_value):
        raise BackupError("non-finite JSON value in dump")

    def unique_pairs(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise BackupError("duplicate JSON key in dump")
            result[key] = value
        return result

    return json.loads(data, parse_constant=invalid_constant, object_pairs_hook=unique_pairs)


def document_hash(value: dict) -> str:
    if not isinstance(value, dict):
        raise BackupError("dump document must be an object")
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False,
                                     separators=(",", ":"), allow_nan=False).encode()).hexdigest()


def document_set_hash(hashes: list[str]) -> str:
    return hashlib.sha256("".join(sorted(hashes)).encode()).hexdigest()


TASK_KINDS = {
    "documentImport": "documentAdditionOrUpdate", "documentDeletion": "documentDeletion",
    "documentClear": "documentDeletion", "documentDeletionByFilter": "documentDeletion",
    "settings": "settingsUpdate", "indexDeletion": "indexDeletion",
    "indexCreation": "indexCreation", "indexUpdate": "indexUpdate", "indexSwap": "indexSwap",
    "taskCancelation": "taskCancelation", "tasksDeletion": "taskDeletion",
    "dumpCreation": "dumpCreation", "snapshotCreation": "snapshotCreation",
}


def json_lines(stream):
    while line := stream.readline(16 * 1024**2 + 1):
        if len(line) > 16 * 1024**2:
            raise BackupError("oversized dump JSON record")
        yield json_value(line)


def dump_inventory(path: Path) -> dict:
    """Read V6 without extracting files or executing restored work."""
    metadata = None
    indexes: dict[str, dict] = {}
    receipts: dict[int, dict] = {}
    seen: set[str] = set()
    total_size = 0
    with tarfile.open(path, mode="r|gz") as archive:
        for member in archive:
            name = member.name.removeprefix("./")
            if member.isdir():
                name = name.rstrip("/")
            parts = name.split("/")
            if (name.startswith("/") or ".." in parts or "" in parts or
                    ("." in parts and not (name == "." and member.isdir())) or
                    not (member.isfile() or member.isdir()) or name in seen):
                raise BackupError("unsafe or duplicate dump archive entry")
            seen.add(name)
            if len(seen) > 100000:
                raise BackupError("dump archive exceeds validation budget")
            if member.isdir():
                continue
            total_size += member.size
            if total_size > 4 * 1024**3:
                raise BackupError("dump archive exceeds validation budget")
            stream = archive.extractfile(member)
            if stream is None:
                raise BackupError("unreadable dump archive entry")
            if name == "metadata.json":
                if member.size > 1024**2:
                    raise BackupError("oversized dump metadata")
                metadata = json_value(stream.read())
            elif name == "tasks/queue.jsonl":
                for task in json_lines(stream):
                    if not isinstance(task, dict):
                        raise BackupError("invalid dump task")
                    uid = task.get("uid")
                    if (type(uid) is not int or uid < 0 or uid in receipts or
                            task.get("status") not in {"succeeded", "failed", "canceled"}):
                        raise BackupError("dump has duplicate, invalid or unfinished task")
                    kind = task.get("type")
                    key = None
                    if isinstance(kind, str):
                        key = kind
                    elif isinstance(kind, dict) and len(kind) == 1:
                        key = next(iter(kind))
                    if key not in TASK_KINDS:
                        raise BackupError("unsupported dump task kind")
                    enqueued = task.get("enqueuedAt")
                    if not isinstance(enqueued, str) or datetime.fromisoformat(enqueued.replace("Z", "+00:00")).tzinfo is None:
                        raise BackupError("invalid dump task time")
                    receipts[uid] = {"uid": uid, "indexUid": task.get("indexUid"),
                                     "status": task["status"], "type": TASK_KINDS[key],
                                     "enqueuedAt": enqueued, "customMetadata": task.get("customMetadata")}
                    if len(receipts) > 100000:
                        raise BackupError("dump task history exceeds validation budget")
            elif len(parts) == 3 and parts[0] == "indexes":
                uid, filename = parts[1:]
                if not re.fullmatch(r"[a-zA-Z0-9_-]+", uid):
                    raise BackupError("invalid dump index identity")
                index = indexes.setdefault(uid, {})
                if filename in {"metadata.json", "settings.json"}:
                    if member.size > 16 * 1024**2:
                        raise BackupError("oversized dump index metadata")
                    index[filename] = json_value(stream.read())
                elif filename == "documents.jsonl":
                    hashes = []
                    for document in json_lines(stream):
                        if len(hashes) >= 1000000:
                            raise BackupError("dump documents exceed validation budget")
                        hashes.append(document_hash(document))
                    index[filename] = {"count": len(hashes), "sha256": document_set_hash(hashes)}
                else:
                    raise BackupError("unexpected dump index file")
            # Consume even auxiliary files, so gzip truncation cannot be hidden.
            while stream.read(1024 * 1024):
                pass
    # tar EOF alone does not prove a complete gzip stream/trailer.
    with gzip.open(path, "rb") as stream:
        size = 0
        while chunk := stream.read(1024 * 1024):
            size += len(chunk)
            if size > 4 * 1024**3:
                raise BackupError("dump archive exceeds validation budget")
    if (not isinstance(metadata, dict) or metadata.get("dumpVersion") != "V6" or
            not re.fullmatch(r"\d+\.\d+\.\d+", str(metadata.get("dbVersion", ""))) or
            "tasks/queue.jsonl" not in seen or "keys.jsonl" not in seen):
        raise BackupError("incomplete or unsupported Meilisearch dump")
    for uid, index in indexes.items():
        if (set(index) != {"metadata.json", "settings.json", "documents.jsonl"} or
                not isinstance(index["metadata.json"], dict) or
                index["metadata.json"].get("uid") != uid or
                not isinstance(index["settings.json"], dict)):
            raise BackupError("incomplete or conflicting dump index")
    return {"metadata": metadata, "indexes": indexes,
            "tasks": [receipts[uid] for uid in sorted(receipts)]}


def meili_get(container: str, route: str, key: str) -> dict:
    if not route.startswith("/") or any(ord(char) < 32 for char in route + key):
        raise BackupError("invalid Meilisearch read request")
    config = f"header = {json.dumps('Authorization: Bearer ' + key)}\n".encode()
    response = run(["docker", "exec", "-i", container, "curl", "--fail", "--silent",
                    "--show-error", "--max-time", "20", "--config", "-",
                    "http://127.0.0.1:7700" + route], input=config, timeout=30)
    value = json_value(response.stdout)
    if not isinstance(value, dict):
        raise BackupError("invalid Meilisearch read response")
    return value


def collect_meili_dump(path: Path, task_uid: int, destination: Path) -> dict:
    private_directory(path.parent, create=False)
    if (not path.is_absolute() or path.is_symlink() or not path.is_file() or
            path.stat().st_uid != os.getuid() or stat.S_IMODE(path.stat().st_mode) != 0o600 or
            type(task_uid) is not int or task_uid < 0):
        raise BackupError("dump must be an absolute owner-only regular file with a valid task UID")
    source = owned_source("addp-meilisearch", "meilisearch")
    values = dict(item.split("=", 1) for item in source["Config"]["Env"] if "=" in item)
    key = values.get("MEILI_MASTER_KEY", "")
    if len(key) < 16:
        raise BackupError("owned Meilisearch source has no valid master key")
    task = meili_get("addp-meilisearch", f"/tasks/{task_uid}", key)
    details = task.get("details")
    dump_uid = details.get("dumpUid") if isinstance(details, dict) else None
    if (type(task.get("uid")) is not int or task.get("uid") != task_uid or task.get("type") != "dumpCreation" or
            task.get("status") != "succeeded" or not isinstance(dump_uid, str) or
            not re.fullmatch(r"[a-zA-Z0-9_-]+", dump_uid)):
        raise BackupError("source dump task is not a proven successful export")
    # Standard Infra dump directory only; never guess another path or invoke export.
    source_path = f"/meili_data/dumps/{dump_uid}.dump"
    source_hash_parts = run(["docker", "exec", "addp-meilisearch", "sha256sum", source_path]).stdout.decode().split()
    source_hash = source_hash_parts[0] if source_hash_parts else ""
    if not re.fullmatch(r"[0-9a-f]{64}", source_hash) or digest(path) != source_hash:
        raise BackupError("dump does not match the owned source export")
    inventory = dump_inventory(path)
    receipt = next((item for item in inventory["tasks"] if item["uid"] == task_uid), None)
    if (receipt is None or receipt["type"] != "dumpCreation" or receipt["status"] != "succeeded" or
            receipt["enqueuedAt"] != task.get("enqueuedAt")):
        raise BackupError("dump does not preserve the successful source export receipt")
    version = meili_get("addp-meilisearch", "/version", key).get("pkgVersion")
    if inventory["metadata"]["dbVersion"] != version:
        raise BackupError("dump version differs from the owned source")
    shutil.copyfile(path, destination)
    os.chmod(destination, 0o600)
    if digest(destination) != source_hash:
        raise BackupError("dump changed while being collected")
    return {"file": "meilisearch.dump", "sha256": source_hash, "source_image": image_id(source),
            "source_version": version, "dump_uid": dump_uid, "dump_task_uid": task_uid,
            "dump_task_enqueued_at": task.get("enqueuedAt"),
            "indexes": len(inventory["indexes"]), "tasks": len(inventory["tasks"])}


def create(root: Path, *, meilisearch_dump: Path | None = None,
           meilisearch_dump_task: int | None = None) -> Path:
    if (meilisearch_dump is None) != (meilisearch_dump_task is None):
        raise BackupError("Meilisearch dump path and task UID must be provided together")
    root = private_directory(root, create=True)
    pg = owned_source("addp-postgres", "postgres")
    minio = owned_source("addp-minio", "minio")
    env_file = ROOT / ".env"
    if not env_file.is_file() or env_file.is_symlink():
        raise BackupError("root .env is unavailable")
    mc_image = json.loads(run(["docker", "image", "inspect", "minio/mc:latest"]).stdout)[0]["Id"]
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    target = root / f"addp-infra-{stamp}-{secrets.token_hex(3)}.incomplete"
    target.mkdir(mode=0o700)
    try:
        meili = (collect_meili_dump(meilisearch_dump, meilisearch_dump_task, target / "meilisearch.dump")
                 if meilisearch_dump is not None else None)
        with (target / "postgres.dump").open("wb") as stream:
            run(["docker", "exec", "addp-postgres", "pg_dump", "-U", "addp", "-d", "addp",
                 "-Fc", "--no-owner"], stdout=stream, timeout=600)
        os.chmod(target / "postgres.dump", 0o600)
        run(["pg_restore", "--file=/dev/null", str(target / "postgres.dump")], timeout=600)
        shutil.copyfile(env_file, target / "source.env")
        os.chmod(target / "source.env", 0o600)

        minio_dir = target / "minio"
        minio_dir.mkdir(mode=0o700)
        source_env = mc_env(minio, "src", "minio")
        mounts = [(minio_dir, "/backup", False)]
        for kind in ("bucket", "iam"):
            mc(["admin", "cluster", kind, "export", "src"], env=source_env,
               image=mc_image, network="addp-network", mounts=mounts, workdir="/backup")
        buckets = bucket_names(mc(["--json", "ls", "src"], env=source_env,
                                  image=mc_image, network="addp-network").stdout)
        objects = minio_dir / "objects"
        objects.mkdir(mode=0o700)
        for bucket in buckets:
            (objects / bucket).mkdir(mode=0o700)
            mc(["mirror", "--summary", f"src/{bucket}", f"/backup/objects/{bucket}"],
               env=source_env, image=mc_image, network="addp-network", mounts=mounts,
               timeout=3600)
        (minio_dir / "buckets.json").write_text(json.dumps(buckets, indent=2) + "\n")
        if not (minio_dir / "src-bucket-metadata.zip").is_file() or not (minio_dir / "src-iam-info.zip").is_file():
            raise BackupError("MinIO metadata export is incomplete")
        manifest = {
            "format": "addp-infra-local-backup/v1", "created_at": stamp,
            "source": {"postgres_image": image_id(pg), "minio_image": image_id(minio),
                       "mc_image": mc_image},
            "postgres_counts": pg_counts("addp-postgres", "addp"),
            "buckets": buckets, "files": files_under(target),
        }
        if meili is not None:
            manifest["meilisearch"] = meili
        (target / "manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
        os.chmod(target / "manifest.json", 0o600)
        final = target.with_name(target.name.removesuffix(".incomplete"))
        target.rename(final)
        return final
    except Exception:
        print(f"Incomplete backup retained for inspection: {target}")
        raise


def validate(path: Path) -> dict:
    path = private_directory(path, create=False)
    if path.name.endswith(".incomplete"):
        raise BackupError("incomplete backups cannot be restored")
    manifest = json.loads((path / "manifest.json").read_text())
    if manifest.get("format") != "addp-infra-local-backup/v1":
        raise BackupError("unsupported backup format")
    if manifest.get("files") != files_under(path):
        raise BackupError("backup contents or SHA-256 checksums differ from manifest")
    meili = manifest.get("meilisearch")
    if (meili is None) != (not (path / "meilisearch.dump").exists()):
        raise BackupError("Meilisearch backup scope differs from manifest")
    if meili is not None:
        if (not isinstance(meili, dict) or meili.get("file") != "meilisearch.dump" or
                type(meili.get("dump_task_uid")) is not int or meili["dump_task_uid"] < 0 or
                not re.fullmatch(r"[a-zA-Z0-9_-]+", str(meili.get("dump_uid", ""))) or
                not re.fullmatch(r"sha256:[0-9a-f]{64}", str(meili.get("source_image", ""))) or
                digest(path / "meilisearch.dump") != meili.get("sha256")):
            raise BackupError("invalid Meilisearch backup evidence")
        inventory = dump_inventory(path / "meilisearch.dump")
        if (inventory["metadata"]["dbVersion"] != meili.get("source_version") or
                len(inventory["indexes"]) != meili.get("indexes") or
                len(inventory["tasks"]) != meili.get("tasks")):
            raise BackupError("Meilisearch dump inventory differs from manifest")
        receipt = next((item for item in inventory["tasks"]
                        if item["uid"] == meili.get("dump_task_uid")), None)
        if (receipt is None or receipt["type"] != "dumpCreation" or
                receipt["status"] != "succeeded" or
                receipt["enqueuedAt"] != meili.get("dump_task_enqueued_at")):
            raise BackupError("Meilisearch export receipt differs from manifest")
    return manifest


def wait_postgres_initialized(name: str, timeout: int = 90) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        logs = subprocess.run(["docker", "logs", name], capture_output=True)
        if (logs.returncode == 0 and
                b"PostgreSQL init process complete" in logs.stdout + logs.stderr):
            ready = subprocess.run(["docker", "exec", name, "pg_isready", "-U", "addp",
                                    "-d", "postgres"], capture_output=True)
            if ready.returncode == 0:
                return
        time.sleep(1)
    raise BackupError(f"isolated PostgreSQL initialization did not finish: {name}")


def cleanup_owned(name: str, token: str) -> None:
    result = subprocess.run(["docker", "inspect", name], capture_output=True)
    if result.returncode:
        if run(["docker", "ps", "-aq", "--filter", f"name=^/{name}$"]).stdout.strip():
            raise BackupError(f"could not verify restore container ownership: {name}")
        return
    inspected = json_value(result.stdout)
    if (not isinstance(inspected, list) or len(inspected) != 1 or
            not isinstance(inspected[0], dict) or not isinstance(inspected[0].get("Config"), dict)):
        raise BackupError(f"invalid restore container ownership response: {name}")
    info = inspected[0]
    labels = info["Config"].get("Labels") or {}
    if labels.get("addp.restore-drill") != token:
        raise BackupError(f"refusing to remove an unowned container: {name}")
    run(["docker", "rm", "-f", name])
    if run(["docker", "ps", "-aq", "--filter", f"name=^/{name}$"]).stdout.strip():
        raise BackupError(f"restore container remains after cleanup: {name}")


def settings_preserved(expected, actual) -> bool:
    if isinstance(expected, dict):
        return isinstance(actual, dict) and all(
            value is None or (key in actual and settings_preserved(value, actual[key]))
            for key, value in expected.items()
        )
    return expected == actual


def verify_meili_restore(name: str, key: str, inventory: dict) -> None:
    def get(route):
        return meili_get(name, route, key)

    indexes = {}
    offset = 0
    while True:
        page = get(f"/indexes?limit=1000&offset={offset}")
        rows = page.get("results")
        if not isinstance(rows, list) or page.get("total") != len(inventory["indexes"]):
            raise BackupError("restored Meilisearch index count differs")
        for row in rows:
            if not isinstance(row, dict) or not isinstance(row.get("uid"), str):
                raise BackupError("invalid restored Meilisearch index identity")
            uid = row.get("uid")
            if uid in indexes or uid not in inventory["indexes"]:
                raise BackupError("restored Meilisearch index identity differs")
            indexes[uid] = row
        offset += len(rows)
        if offset == page["total"]:
            break
        if not rows or offset > page["total"]:
            raise BackupError("incomplete Meilisearch index pagination")
    for uid, expected in inventory["indexes"].items():
        if indexes[uid].get("primaryKey") != expected["metadata.json"].get("primaryKey"):
            raise BackupError("restored Meilisearch primary key differs")
        if not settings_preserved(expected["settings.json"], get(f"/indexes/{uid}/settings")):
            raise BackupError("restored Meilisearch explicit settings differ")
        count = expected["documents.jsonl"]["count"]
        hashes = []
        while True:
            page = get(f"/indexes/{uid}/documents?limit=100&offset={len(hashes)}")
            rows = page.get("results")
            if not isinstance(rows, list) or page.get("total") != count:
                raise BackupError("restored Meilisearch document count differs")
            hashes.extend(document_hash(row) for row in rows)
            if len(hashes) == count:
                break
            if not rows or len(hashes) > count:
                raise BackupError("incomplete Meilisearch document pagination")
        if document_set_hash(hashes) != expected["documents.jsonl"]["sha256"]:
            raise BackupError("restored Meilisearch document contents differ")
    if get("/tasks?limit=1").get("total") != len(inventory["tasks"]):
        raise BackupError("restored Meilisearch task history count differs")
    for expected in inventory["tasks"]:
        actual = get(f"/tasks/{expected['uid']}")
        if any(actual.get(field) != value for field, value in expected.items()):
            raise BackupError("restored Meilisearch task identity or terminal evidence differs")


def drill_meili(path: Path, evidence: dict, name: str, token: str,
                restore_image: str | None) -> None:
    inventory = dump_inventory(path / "meilisearch.dump")
    image = restore_image or evidence["source_image"]
    key = secrets.token_urlsafe(32)
    env = {**os.environ, "MEILI_MASTER_KEY": key}
    run(["docker", "run", "--rm", "-d", "--name", name,
         "--label", f"addp.restore-drill={token}", "--network", "none",
         "--no-healthcheck", "-e", "MEILI_MASTER_KEY", "--workdir", "/restore",
         "--mount", f"type=bind,src={path / 'meilisearch.dump'},dst=/backup/meilisearch.dump,readonly",
         image, "/bin/meilisearch", "--db-path", "/restore/data.ms",
         "--import-dump", "/backup/meilisearch.dump", "--http-addr", "127.0.0.1:7700",
         "--env", "production", "--no-analytics"], env=env)
    deadline = time.monotonic() + 1200
    while True:
        if not inspect(name)["State"]["Running"]:
            raise BackupError("isolated Meilisearch import process stopped")
        try:
            if meili_get(name, "/health", key).get("status") == "available":
                break
        except BackupError:
            pass
        if time.monotonic() >= deadline:
            raise BackupError("isolated Meilisearch import timed out")
        time.sleep(1)
    expected_version = (restore_image.split(":v", 1)[1].split("@", 1)[0]
                        if restore_image else evidence["source_version"])
    if meili_get(name, "/version", key).get("pkgVersion") != expected_version:
        raise BackupError("isolated Meilisearch version differs from selected image")
    verify_meili_restore(name, key, inventory)
    print(f"PASS: isolated Meilisearch restore {evidence['indexes']} indexes, {evidence['tasks']} tasks")


def drill(path: Path, *, meilisearch_restore_image: str | None = None) -> None:
    path = private_directory(path, create=False)
    manifest = validate(path)
    if meilisearch_restore_image is not None:
        if ("meilisearch" not in manifest or not re.fullmatch(
                r"getmeili/meilisearch:v\d+\.\d+\.\d+@sha256:[0-9a-f]{64}",
                meilisearch_restore_image)):
            raise BackupError("Meilisearch restore requires a collected dump and a fixed tag with digest")
    token = secrets.token_hex(8)
    pg_name = f"addp-restore-drill-pg-{token}"
    minio_name = f"addp-restore-drill-minio-{token}"
    meili_name = f"addp-restore-drill-meili-{token}"
    label = f"addp.restore-drill={token}"
    source = manifest["source"]
    password = secrets.token_urlsafe(24)
    try:
        env = {**os.environ, "POSTGRES_PASSWORD": password}
        run(["docker", "run", "--rm", "-d", "--name", pg_name, "--label", label,
             "--network", "none", "-e", "POSTGRES_PASSWORD", "-e", "POSTGRES_USER=addp",
             "-e", "POSTGRES_DB=postgres", source["postgres_image"]], env=env)
        wait_postgres_initialized(pg_name)
        # The ADDP image's default template includes PostGIS sample schemas.
        # A fresh template0 database lets pg_restore recreate exactly the dump.
        run(["docker", "exec", pg_name, "createdb", "-U", "addp", "-T", "template0",
             "addp_restore"])
        with (path / "postgres.dump").open("rb") as stream:
            run(["docker", "exec", "-i", pg_name, "pg_restore", "-U", "addp", "-d",
                 "addp_restore", "--no-owner", "--no-acl", "--exit-on-error"],
                stdin=stream, timeout=1200)
        if pg_counts(pg_name, "addp_restore") != manifest["postgres_counts"]:
            raise BackupError("restored PostgreSQL row counts differ from backup")

        env = {**os.environ, "MINIO_ROOT_USER": "drilladmin", "MINIO_ROOT_PASSWORD": password}
        run(["docker", "run", "--rm", "-d", "--name", minio_name, "--label", label,
             "--network", "none", "-e", "MINIO_ROOT_USER", "-e", "MINIO_ROOT_PASSWORD",
             source["minio_image"], "server", "/data", "--address", ":9000"], env=env)
        minio_info = inspect(minio_name)
        destination_env = mc_env(minio_info, "dst", "127.0.0.1")
        network = f"container:{minio_name}"
        deadline = time.monotonic() + 90
        while True:
            try:
                mc(["ls", "dst"], env=destination_env, image=source["mc_image"],
                   network=network, timeout=15)
                break
            except BackupError:
                if time.monotonic() >= deadline:
                    raise
                time.sleep(1)
        mount = [(path / "minio", "/backup", True)]
        mc(["admin", "cluster", "bucket", "import", "dst", "/backup/src-bucket-metadata.zip"],
           env=destination_env, image=source["mc_image"], network=network, mounts=mount)
        mc(["admin", "cluster", "iam", "import", "dst", "/backup/src-iam-info.zip"],
           env=destination_env, image=source["mc_image"], network=network, mounts=mount)
        restored = bucket_names(mc(["--json", "ls", "dst"], env=destination_env,
                                   image=source["mc_image"], network=network).stdout)
        if restored != manifest["buckets"]:
            raise BackupError("restored MinIO buckets differ from backup")
        with tempfile.TemporaryDirectory(prefix="addp-restore-verify-") as verify_name:
            verify = Path(verify_name)
            for bucket in restored:
                local = path / "minio" / "objects" / bucket
                mc(["mirror", "--summary", str(Path("/backup") / "objects" / bucket),
                    f"dst/{bucket}"], env=destination_env, image=source["mc_image"],
                   network=network, mounts=mount, timeout=3600)
                (verify / bucket).mkdir()
                mc(["mirror", "--summary", f"dst/{bucket}", f"/verify/{bucket}"],
                   env=destination_env, image=source["mc_image"], network=network,
                   mounts=[(verify, "/verify", False)], timeout=3600)
                if files_under(local) != files_under(verify / bucket):
                    raise BackupError(f"restored MinIO objects differ: {bucket}")
        print(f"PASS: isolated PostgreSQL restore {manifest['postgres_counts']}")
        print(f"PASS: isolated MinIO restore {len(restored)} buckets")
        if "meilisearch" in manifest:
            drill_meili(path, manifest["meilisearch"], meili_name, token, meilisearch_restore_image)
        validate(path)
    finally:
        failures = []
        for name in (meili_name, minio_name, pg_name):
            try:
                cleanup_owned(name, token)
            except (BackupError, OSError, ValueError, subprocess.TimeoutExpired) as error:
                failures.append(str(error))
        if failures:
            raise BackupError("restore drill cleanup not proven: " + "; ".join(failures))
    print("PASS: restore drill containers have zero remaining owned resources")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    action = parser.add_subparsers(dest="action", required=True)
    create_parser = action.add_parser("create")
    create_parser.add_argument("--output-root", required=True, type=Path)
    create_parser.add_argument("--meilisearch-dump", type=Path)
    create_parser.add_argument("--meilisearch-dump-task", type=int)
    drill_parser = action.add_parser("drill")
    drill_parser.add_argument("--backup-dir", required=True, type=Path)
    drill_parser.add_argument("--meilisearch-restore-image")
    args = parser.parse_args()
    try:
        if args.action == "create":
            print(f"Backup created: {create(args.output_root, meilisearch_dump=args.meilisearch_dump, meilisearch_dump_task=args.meilisearch_dump_task)}")
        else:
            drill(args.backup_dir, meilisearch_restore_image=args.meilisearch_restore_image)
    except (BackupError, OSError, KeyError, ValueError, tarfile.TarError, EOFError,
            subprocess.TimeoutExpired) as error:
        raise SystemExit(f"Backup operation failed: {error}") from None


if __name__ == "__main__":
    main()
