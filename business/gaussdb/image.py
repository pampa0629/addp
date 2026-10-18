#!/usr/bin/env python3
"""Business-owned verified GaussDB image context; shared by local deployment and T5."""
from __future__ import annotations
import argparse
import hashlib
import io
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tarfile
import urllib.request
import uuid
import zipfile

ROOT = Path(__file__).resolve().parents[2]
DIRECTORY = Path(__file__).resolve().parent
FACT_NAMES = ("GAUSSDB_OFFICIAL_VERSION", "GAUSSDB_OFFICIAL_MEDIA_URL",
              "GAUSSDB_OFFICIAL_MEDIA_SHA256", "GAUSSDB_OFFICIAL_BASE_IMAGE",
              "GAUSSDB_GO_MODULE", "GAUSSDB_GO_VERSION", "GAUSSDB_GO_SUM", "GAUSSDB_GO_MOD_SUM")


def go_architecture(machine: str) -> str:
    if machine in {"x86_64", "amd64"}:
        return "amd64"
    if machine in {"aarch64", "arm64"}:
        return "arm64"
    raise RuntimeError("unsupported GaussDB architecture: " + machine)


def verify_daemon_architecture(architecture: str, daemon: str) -> None:
    system, _, machine = daemon.partition("/")
    if system != "linux" or go_architecture(machine) != architecture:
        raise RuntimeError("matching native Linux Docker architecture is required")


def media_facts(machine: str | None = None) -> dict[str, str]:
    architecture = go_architecture(machine or platform.machine())
    script = 'source "$1"; gaussdb_select_official_media "$2"; '
    script += 'printf "%s\\n" ' + " ".join('"$' + name + '"' for name in FACT_NAMES)
    values = subprocess.check_output(
        ["bash", "-eu", "-c", script, "facts", str(ROOT / "scripts/lib/gaussdb-official-media.sh"), architecture],
        text=True).splitlines()
    if len(values) != len(FACT_NAMES) or not all(values):
        raise RuntimeError("incomplete official media facts")
    return dict(zip(FACT_NAMES, values, strict=True))


def sha256(path: Path) -> str:
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def download(url: str, expected: str, directory: Path) -> Path:
    directory.mkdir(parents=True, exist_ok=True)
    path = directory / url.rsplit("/", 1)[1]
    if path.exists() and sha256(path) == expected:
        return path
    partial = directory / (path.name + "." + uuid.uuid4().hex + ".partial")
    try:
        with urllib.request.urlopen(url, timeout=60) as response, partial.open("wb") as stream:
            shutil.copyfileobj(response, stream, 1024 * 1024)
        if sha256(partial) != expected:
            raise RuntimeError("official media SHA256 mismatch")
        partial.replace(path)
        return path
    finally:
        partial.unlink(missing_ok=True)


def unique_member(names: list[str], suffix: str) -> str:
    matches = [name for name in names if name.endswith(suffix)]
    if len(matches) != 1:
        raise RuntimeError("official media must contain exactly one " + suffix)
    return matches[0]


def extract_kernel(archive: Path, destination: Path, architecture: str) -> None:
    # Explicit member selection keeps both nested archives and architectures apart.
    with zipfile.ZipFile(archive) as package:
        suffix = "ARM" if go_architecture(architecture) == "arm64" else "X86"
        outer = unique_member(package.namelist(), "_Server_" + suffix + "_Developer.tar.gz")
        with tarfile.open(fileobj=io.BytesIO(package.read(outer))) as first:
            inner = unique_member(first.getnames(), "_64bit_Server.tar.gz")
            with tarfile.open(fileobj=first.extractfile(inner)) as second:
                name = unique_member(second.getnames(), ".bin")
                binary = second.extractfile(name).read()
                digest_name = unique_member(second.getnames(), "_64bit.sha256")
                expected = second.extractfile(digest_name).read().decode().strip()
                if hashlib.sha256(binary).hexdigest() != expected:
                    raise RuntimeError("official inner kernel SHA256 mismatch")
                destination.write_bytes(binary)



def prepare_context(directory: Path, architecture: str, cache: Path) -> dict[str, str]:
    facts = media_facts(architecture)
    media = download(facts["GAUSSDB_OFFICIAL_MEDIA_URL"], facts["GAUSSDB_OFFICIAL_MEDIA_SHA256"], cache)
    extract_kernel(media, directory / "kernel.bin", architecture)
    for name in ("Dockerfile", "entrypoint.sh", "init.sql"):
        destination = directory / name
        shutil.copyfile(DIRECTORY / name, destination)
        # These contain no credentials. Docker COPY preserves their modes, and
        # the runtime user omm must read init.sql regardless of the caller umask.
        destination.chmod(0o644)
    return facts


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--context", type=Path, required=True)
    args = parser.parse_args()
    architecture = go_architecture(platform.machine())
    daemon = subprocess.check_output(["docker", "info", "--format", "{{.OSType}}/{{.Architecture}}"], text=True).strip()
    verify_daemon_architecture(architecture, daemon)
    prepare_context(args.context, architecture, Path(os.environ.get("ADDP_GAUSSDB_MEDIA_CACHE", "/tmp/addp-gaussdb-official-media")))


if __name__ == "__main__":
    main()
