#!/usr/bin/env python3
"""Reuse the fixed Infra MinIO build in disposable GitHub Linux runners."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import secrets
import subprocess
import sys
import tempfile
import time


ROOT = Path(__file__).resolve().parents[2]
BUILDER_IMAGE = "addp-minio-build-cache:local"


def digest(path: Path) -> str:
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def input_key(repository: Path) -> str:
    # Hash every tracked build-context input, including Docker ignore files.
    paths = subprocess.check_output(
        ["git", "ls-files", "-z", "--", "scripts/infra", "scripts/ci/minio-build-cache.py",
         "docker-compose.infra.yml"],
        cwd=repository,
    ).decode().split("\0")
    checksum = hashlib.sha256(b"addp-minio-cache-v1/linux/amd64\0")
    for name in sorted(filter(None, paths)):
        path = repository / name
        if path.is_symlink() or not path.is_file():
            raise ValueError(f"build input must be a regular file: {name}")
        checksum.update(name.encode() + b"\0" + path.read_bytes() + b"\0")
    return "addp-minio-linux-amd64-" + checksum.hexdigest()


def cache_path(path: Path) -> Path:
    runner = Path(os.environ["RUNNER_TEMP"]).resolve()
    expected = runner / "addp-minio-build-cache"
    if path.is_symlink() or path.absolute() != expected or path.resolve() != expected:
        raise ValueError("cache must be the Runner temporary addp-minio-build-cache directory")
    return expected


def require_runner(repository: Path) -> None:
    if (os.environ.get("GITHUB_ACTIONS"), os.environ.get("RUNNER_OS"),
            os.environ.get("RUNNER_ENVIRONMENT"), platform.system(), platform.machine()) != (
            "true", "Linux", "github-hosted", "Linux", "x86_64"):
        raise ValueError("MinIO build cache requires a disposable GitHub Linux amd64 runner")
    if (repository / ".env").exists():
        raise ValueError("MinIO build cache forbids a repository .env")
    if subprocess.check_output(["git", "status", "--porcelain"], cwd=repository).strip():
        raise ValueError("MinIO build cache requires a clean checkout")


def all_runs_cached(log: str) -> bool:
    runs = set(re.findall(r"(?m)^#(\d+) \[[^\n]+\] RUN ", log))
    cached = set(re.findall(r"(?m)^#(\d+) CACHED\s*$", log))
    return bool(runs) and runs <= cached


def execute(repository: Path, command: list[str], log: Path | None = None) -> str:
    print("MinIO cache: " + " ".join(command), flush=True)
    environment = os.environ.copy()
    if command[:2] == ["docker", "compose"]:
        # Compose interpolates unselected services too. No FalkorDB service is
        # started, and its throwaway build-only value never enters an image.
        environment["INFRA_FALKORDB_PASSWORD"] = secrets.token_urlsafe(32)
    result = subprocess.run(command, cwd=repository, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, text=True, check=False, env=environment)
    if log:
        log.write_text(result.stdout)
    print(result.stdout, end="", flush=True)
    result.check_returncode()
    return result.stdout


def image_identity(repository: Path, image: str) -> str:
    data = json.loads(execute(repository, ["docker", "image", "inspect", image]))[0]
    if data["Os"] != "linux" or data["Architecture"] != "amd64":
        raise ValueError("cached image platform differs from linux/amd64")
    return data["Id"]


def build(repository: Path, directory: Path, require_hit: bool) -> dict:
    require_runner(repository)
    directory = cache_path(directory)
    directory.mkdir(parents=True, exist_ok=True)
    key = input_key(repository)
    receipt_path, archive = directory / "receipt.json", directory / "images.tar"
    compose = (repository / "docker-compose.infra.yml").read_text()
    image = re.search(r"(?m)^  minio:\n    image: ([^\s]+)$", compose)
    if image is None:
        raise ValueError("Infra MinIO must declare its single image")
    final_image = image.group(1)
    expected_images = {BUILDER_IMAGE, final_image}
    previous = None
    if receipt_path.exists() or archive.exists():
        if any(path.is_symlink() or not path.is_file() for path in (receipt_path, archive)):
            raise ValueError("MinIO cache requires regular receipt and archive files")
        previous = json.loads(receipt_path.read_text())
        if previous["key"] != key or previous["archive_sha256"] != digest(archive):
            raise ValueError("MinIO cache input key or archive digest differs")
        if set(previous["images"]) != expected_images:
            raise ValueError("cache contains unexpected image names")
        execute(repository, ["docker", "image", "load", "--input", str(archive)])
        for image, identity in previous["images"].items():
            if image_identity(repository, image) != identity:
                raise ValueError("restored MinIO image identity differs from receipt")
    elif require_hit:
        raise ValueError("fresh-runner verification requires an exact cache hit")

    started = time.monotonic()
    with tempfile.TemporaryDirectory(prefix="addp-minio-build-", dir=directory.parent) as temp:
        work = Path(temp)
        builder = ["docker", "build", "--platform", "linux/amd64", "--progress=plain",
                   "--build-arg", "BUILDKIT_INLINE_CACHE=1", "--file", "scripts/infra/Dockerfile.minio",
                   "--target", "build", "--tag", BUILDER_IMAGE]
        if previous:
            builder += ["--cache-from", BUILDER_IMAGE]
        first = execute(repository, builder + ["scripts/infra"], work / "builder.log")
        override = work / "compose-cache.json"
        override.write_text(json.dumps({"services": {"minio": {"build": {
            "cache_from": [BUILDER_IMAGE] + ([final_image] if previous else []),
        }}}}))
        # The final image still comes from the real Infra Compose definition.
        second = execute(repository, ["docker", "compose", "--progress", "plain",
                         "-f", "docker-compose.infra.yml", "-f", str(override),
                         "build", "--build-arg", "BUILDKIT_INLINE_CACHE=1", "minio"], work / "compose.log")
        if previous and not (all_runs_cached(first) and all_runs_cached(second)):
            raise ValueError("warm MinIO build executed a Dockerfile RUN instead of reusing cache")
        binary_hashes = execute(repository, ["docker", "run", "--rm", "--network", "none",
                                "--entrypoint", "sha256sum", final_image,
                                "/usr/local/bin/minio", "/usr/local/bin/mc"]).strip()
        if re.fullmatch(r"[0-9a-f]{64}  /usr/local/bin/minio\n[0-9a-f]{64}  /usr/local/bin/mc", binary_hashes) is None:
            raise ValueError("MinIO binary checksum evidence is incomplete")
        if previous and binary_hashes != previous["binary_sha256"]:
            raise ValueError("warm MinIO binaries differ from cached build")
        for binary in ("minio", "mc"):
            execute(repository, ["docker", "run", "--rm", "--network", "none",
                                "--entrypoint", binary, final_image, "--version"])
        # Confirm the ordinary, unmodified Compose build also reuses the warmed daemon.
        ordinary = execute(repository, ["docker", "compose", "--progress", "plain", "-f",
                            "docker-compose.infra.yml", "build", "minio"])
        if not all_runs_cached(ordinary):
            raise ValueError("ordinary Infra Compose build did not reuse prepared MinIO cache")
        # The ordinary build need not export inline metadata. Restore that
        # metadata on the transfer image after proving the real Infra route.
        exported = execute(repository, ["docker", "compose", "--progress", "plain",
                           "-f", "docker-compose.infra.yml", "-f", str(override),
                           "build", "--build-arg", "BUILDKIT_INLINE_CACHE=1", "minio"])
        if not all_runs_cached(exported):
            raise ValueError("inline cache export unexpectedly executed a Dockerfile RUN")

    images = {name: image_identity(repository, name) for name in sorted(expected_images)}
    execute(repository, ["docker", "image", "save", "--output", str(archive), *sorted(expected_images)])
    receipt = {"key": key, "cache_hit": previous is not None, "images": images,
               "binary_sha256": binary_hashes, "archive_sha256": digest(archive),
               "archive_bytes": archive.stat().st_size,
               "duration_seconds": round(time.monotonic() - started, 3),
               "ordinary_compose_cached": True, "warm_runs_cached": previous is not None}
    receipt_path.write_text(json.dumps(receipt, indent=2) + "\n")
    print("MINIO_BUILD_CACHE_RESULT=" + json.dumps(receipt, sort_keys=True), flush=True)
    return receipt


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("key", "build", "clean"))
    parser.add_argument("--cache-dir", type=Path, required=True)
    parser.add_argument("--require-hit", action="store_true")
    args = parser.parse_args()
    try:
        if args.mode == "key":
            print("key=" + input_key(ROOT))
        elif args.mode == "build":
            receipt = build(ROOT, args.cache_dir, args.require_hit)
            (cache_path(args.cache_dir).parent / "addp-minio-cache-attempt.json").write_text(
                json.dumps({"result": "passed", **receipt}, indent=2) + "\n")
        else:
            # Only the transferred archive is removed; retain the small build receipt.
            require_runner(ROOT)
            (cache_path(args.cache_dir) / "images.tar").unlink(missing_ok=True)
    except (OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError) as error:
        print(f"MinIO cache failed: {error}", file=sys.stderr)
        if args.mode == "build" and os.environ.get("GITHUB_ACTIONS") == "true":
            try:
                attempt = cache_path(args.cache_dir).parent / "addp-minio-cache-attempt.json"
                attempt.write_text(json.dumps({"result": "failed", "error": str(error)}) + "\n")
            except (OSError, ValueError, KeyError):
                pass  # Invalid paths must not create evidence outside the Runner directory.
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
