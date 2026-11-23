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
CONTEXT = Path("scripts/infra")
DOCKERFILE = CONTEXT / "Dockerfile.minio"
IGNORE_FILE = DOCKERFILE.with_name(DOCKERFILE.name + ".dockerignore")
COMPOSE_FILE = "docker-compose.infra.yml"


def digest(path: Path) -> str:
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def compose_environment() -> dict[str, str]:
    # Compose interpolates other services' mandatory settings as well.
    return {
        **os.environ,
        "COMPOSE_DISABLE_ENV_FILE": "1",
        "INFRA_FALKORDB_PASSWORD": secrets.token_urlsafe(32),
        "REDIS_PASSWORD": secrets.token_urlsafe(32),
    }


def cache_inputs(repository: Path) -> tuple[str, str]:
    # Use Compose's resolved build configuration, without retaining runtime settings.
    config = subprocess.check_output(
        ["docker", "compose", "-f", COMPOSE_FILE, "config",
         "--no-env-resolution", "--no-path-resolution", "--format", "json", "minio"],
        cwd=repository, env=compose_environment(), stderr=subprocess.PIPE,
    )
    service = json.loads(config)["services"]["minio"]
    image = service["image"]
    definition = service["build"]
    if not re.fullmatch(r"[^\s]+", image) or definition != {
        "context": CONTEXT.as_posix(), "dockerfile": DOCKERFILE.name,
    }:
        raise ValueError("Infra MinIO Compose build must match the fixed cache builder")

    # The Dockerfile-specific filter is also the source of the key's context paths.
    patterns = [line for line in (repository / IGNORE_FILE).read_text().splitlines()
                if line.strip() and not line.startswith("#")]
    sources = []
    if not patterns or patterns[0] != "**" or len(patterns) < 3 or len(patterns) % 2 != 1:
        raise ValueError("MinIO context must deny all before allowing source directories")
    for directory, contents in zip(patterns[1::2], patterns[2::2]):
        match = re.fullmatch(r"!([a-zA-Z0-9_-]+)/", directory)
        if not match or contents != directory + "**":
            raise ValueError("MinIO context must allow literal source directory pairs")
        sources.append((CONTEXT / match.group(1)).as_posix() + "/")
    required = {DOCKERFILE.as_posix(), IGNORE_FILE.as_posix(), "scripts/ci/minio-build-cache.py"}
    paths = subprocess.check_output(
        ["git", "ls-files", "-z", "--", *sorted(required), *sources],
        cwd=repository,
    ).decode().split("\0")
    if not required.issubset(paths) or any(not any(p.startswith(src) for p in paths) for src in sources):
        raise ValueError("MinIO build inputs must be tracked by Git")
    checksum = hashlib.sha256(b"addp-minio-cache-v2/linux/amd64\0")
    checksum.update(json.dumps({"image": image, "build": definition}, sort_keys=True).encode() + b"\0")
    for name in sorted(filter(None, paths)):
        path = repository / name
        if path.is_symlink() or not path.is_file():
            raise ValueError(f"build input must be a regular file: {name}")
        checksum.update(name.encode() + b"\0" + bytes([path.stat().st_mode & 0o111]))
        checksum.update(bytes.fromhex(digest(path)))
    return "addp-minio-linux-amd64-" + checksum.hexdigest(), image


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
    environment = compose_environment() if command[:2] == ["docker", "compose"] else None
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
    key, final_image = cache_inputs(repository)
    receipt_path, archive = directory / "receipt.json", directory / "images.tar"
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
                   "--build-arg", "BUILDKIT_INLINE_CACHE=1", "--file", DOCKERFILE.as_posix(),
                   "--target", "build", "--tag", BUILDER_IMAGE]
        if previous:
            builder += ["--cache-from", BUILDER_IMAGE]
        first = execute(repository, builder + [CONTEXT.as_posix()], work / "builder.log")
        override = work / "compose-cache.json"
        override.write_text(json.dumps({"services": {"minio": {"build": {
            "cache_from": [BUILDER_IMAGE] + ([final_image] if previous else []),
        }}}}))
        # The final image still comes from the real Infra Compose definition.
        second = execute(repository, ["docker", "compose", "--progress", "plain",
                         "-f", COMPOSE_FILE, "-f", str(override),
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
                            COMPOSE_FILE, "build", "minio"])
        if not all_runs_cached(ordinary):
            raise ValueError("ordinary Infra Compose build did not reuse prepared MinIO cache")
        # The ordinary build need not export inline metadata. Restore that
        # metadata on the transfer image after proving the real Infra route.
        exported = execute(repository, ["docker", "compose", "--progress", "plain",
                           "-f", COMPOSE_FILE, "-f", str(override),
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
            require_runner(ROOT)
            key, _ = cache_inputs(ROOT)
            print("key=" + key)
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
