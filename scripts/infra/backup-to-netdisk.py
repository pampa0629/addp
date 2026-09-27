#!/usr/bin/env python3
"""Create, restore-check, and encrypt a local Infra backup for cloud upload."""

from __future__ import annotations

import argparse
import fcntl
import hashlib
import json
import os
import re
import secrets
import subprocess
import tarfile
from datetime import datetime, timezone
from pathlib import Path

import backup


RECIPIENT_RE = re.compile(r"age1[023456789acdefghjklmnpqrstuvwxyz]{58}")


def recipient(path: Path) -> str:
    value = path.read_text().strip()
    if not RECIPIENT_RE.fullmatch(value):
        raise backup.BackupError("invalid age recipient")
    return value


def encrypted_file_hash(path: Path) -> str:
    return backup.digest(path)


def encrypted_contents_match(path: Path, identity: Path, source: Path) -> None:
    manifest = backup.validate(source)
    expected = {name: entry["sha256"] for name, entry in manifest["files"].items()}
    expected["manifest.json"] = backup.digest(source / "manifest.json")
    seen: set[str] = set()
    process = subprocess.Popen(
        ["age", "-d", "-i", str(identity), str(path)], stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    try:
        assert process.stdout is not None
        with tarfile.open(fileobj=process.stdout, mode="r|") as archive:
            for member in archive:
                name = member.name.removeprefix(source.name + "/")
                if member.name == source.name and member.isdir():
                    continue
                if member.isdir() and name not in expected:
                    continue
                if not member.isfile() or name not in expected or name in seen:
                    raise backup.BackupError("encrypted archive has unexpected content")
                stream = archive.extractfile(member)
                if stream is None:
                    raise backup.BackupError("encrypted archive entry is unreadable")
                digest = hashlib.sha256()
                for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                    digest.update(chunk)
                if digest.hexdigest() != expected[name]:
                    raise backup.BackupError(f"encrypted archive checksum differs: {name}")
                seen.add(name)
        if process.wait(timeout=120) != 0 or seen != set(expected):
            raise backup.BackupError("encrypted archive is incomplete")
    finally:
        if process.poll() is None:
            process.kill()
        process.communicate()


def seal(source: Path, export_root: Path, private_root: Path) -> Path:
    backup.validate(source)
    export_root = backup.private_directory(export_root, create=True)
    private_root = backup.private_directory(private_root, create=False)
    identity = private_root / "identity.txt"
    if not identity.is_file() or identity.is_symlink() or identity.stat().st_mode & 0o077:
        raise backup.BackupError("private age identity is missing or accessible to others")
    public = recipient(private_root / "recipient.txt")
    if subprocess.run(["age-keygen", "-y", str(identity)], capture_output=True,
                      check=True).stdout.decode().strip() != public:
        raise backup.BackupError("age recipient does not match private identity")
    output = export_root / f"{source.name}.tar.age"
    if output.exists() or output.is_symlink():
        raise backup.BackupError("encrypted archive already exists")
    staged = private_root / f"staging-{secrets.token_hex(12)}.age"
    process = subprocess.Popen(["age", "-r", public, "-o", str(staged)],
                               stdin=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        assert process.stdin is not None
        with tarfile.open(fileobj=process.stdin, mode="w|") as archive:
            archive.add(source, arcname=source.name,
                        filter=lambda item: item if not item.issym() and not item.islnk() else None)
        process.stdin.close()
        process.stdin = None
        if process.wait(timeout=120) != 0:
            raise backup.BackupError("age encryption failed")
        os.chmod(staged, 0o600)
        encrypted_contents_match(staged, identity, source)
        os.replace(staged, output)
        checksum = encrypted_file_hash(output)
        sidecar = export_root / f"{output.name}.sha256"
        staged_sidecar = private_root / f"checksum-{secrets.token_hex(12)}.sha256"
        staged_sidecar.write_text(f"{checksum}  {output.name}\n")
        os.chmod(staged_sidecar, 0o600)
        os.replace(staged_sidecar, sidecar)
        status = {"completed_at": datetime.now(timezone.utc).isoformat(),
                  "backup": source.name, "archive": output.name, "sha256": checksum,
                  "scope": "addp-infra"}
        temp_status = private_root / f"status-{secrets.token_hex(6)}.json"
        temp_status.write_text(json.dumps(status, indent=2) + "\n")
        os.chmod(temp_status, 0o600)
        os.replace(temp_status, private_root / "last-run.json")
        return output
    finally:
        if process.poll() is None:
            process.kill()
        process.communicate()
        staged.unlink(missing_ok=True)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("daily", "seal-existing"))
    parser.add_argument("--backup-root", type=Path, required=True)
    parser.add_argument("--export-root", type=Path, required=True)
    parser.add_argument("--private-root", type=Path, required=True)
    parser.add_argument("--backup-dir", type=Path)
    args = parser.parse_args()
    try:
        private_root = backup.private_directory(args.private_root, create=False)
        lock = os.open(private_root / "backup.lock", os.O_CREAT | os.O_RDWR, 0o600)
        try:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                raise backup.BackupError("another backup is already running") from None
            if args.action == "daily":
                source = backup.create(args.backup_root)
                backup.drill(source)
            else:
                if args.backup_dir is None:
                    raise backup.BackupError("--backup-dir is required for seal-existing")
                source = args.backup_dir
            print(f"Encrypted backup ready: {seal(source, args.export_root, private_root)}")
        finally:
            os.close(lock)
    except (backup.BackupError, OSError, ValueError, subprocess.SubprocessError,
            tarfile.TarError) as error:
        raise SystemExit(f"Encrypted backup failed: {error}") from None


if __name__ == "__main__":
    main()
