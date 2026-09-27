#!/usr/bin/env python3
"""Read-only local Infra backup and isolated restore drill."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import secrets
import shutil
import stat
import subprocess
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
        stdin=None, stdout=None, timeout: int = 300) -> subprocess.CompletedProcess:
    result = subprocess.run(args, env=env, stdin=stdin, stdout=stdout,
                            capture_output=stdout is None, timeout=timeout, check=False)
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


def create(root: Path) -> Path:
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
        return
    info = json.loads(result.stdout)[0]
    labels = info["Config"].get("Labels") or {}
    if labels.get("addp.restore-drill") != token:
        raise BackupError(f"refusing to remove an unowned container: {name}")
    run(["docker", "rm", "-f", name])


def drill(path: Path) -> None:
    path = private_directory(path, create=False)
    manifest = validate(path)
    token = secrets.token_hex(8)
    pg_name = f"addp-restore-drill-pg-{token}"
    minio_name = f"addp-restore-drill-minio-{token}"
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
    finally:
        cleanup_owned(minio_name, token)
        cleanup_owned(pg_name, token)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    action = parser.add_subparsers(dest="action", required=True)
    action.add_parser("create").add_argument("--output-root", required=True, type=Path)
    action.add_parser("drill").add_argument("--backup-dir", required=True, type=Path)
    args = parser.parse_args()
    try:
        if args.action == "create":
            print(f"Backup created: {create(args.output_root)}")
        else:
            drill(args.backup_dir)
    except (BackupError, OSError, KeyError, ValueError, subprocess.TimeoutExpired) as error:
        raise SystemExit(f"Backup operation failed: {error}") from None


if __name__ == "__main__":
    main()
