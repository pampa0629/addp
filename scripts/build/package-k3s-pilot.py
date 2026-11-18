#!/usr/bin/env python3
"""Build and export one native K3s pilot node bundle from a fixed clean checkout."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile

TOOLS = Path(__file__).resolve().parents[1]
PRODUCTS = {"system": "system-backend", "gateway": "gateway", "catalog": "catalog-backend"}
INFRA = ("postgres", "redis", "minio", "meilisearch")


def output(*command: str) -> str:
    return subprocess.check_output(command, text=True).strip()


def digest(path: Path) -> str:
    with path.open("rb") as file:
        return hashlib.file_digest(file, "sha256").hexdigest()


def archive_identities(path: Path) -> dict[str, str]:
    identities = {}
    with tarfile.open(path, "r:") as archive:
        manifest = json.load(archive.extractfile("manifest.json"))
        for entry in manifest:
            with archive.extractfile(entry["Config"]) as config:
                identity = "sha256:" + hashlib.file_digest(config, "sha256").hexdigest()
            for tag in entry["RepoTags"]:
                identities[tag] = identity
    return identities


def system_secret_keys(compose: str) -> list[str]:
    system = compose.split("\n  system-backend:\n", 1)[1].split("\n  system-frontend:", 1)[0]
    keys = set(re.findall(r"^      - ([A-Z0-9_]+_SERVICE_CLIENT_SECRET)=", system, re.M))
    return sorted(keys - {"LOG_OBSERVER_SERVICE_CLIENT_SECRET", "PROMETHEUS_SERVICE_CLIENT_SECRET"})


def package(commit: str, destination: Path, infra_release: Path | None, builder_registry: str) -> None:
    if not re.fullmatch(r"[a-f0-9]{40}", commit) or output("git", "rev-parse", "HEAD") != commit:
        raise ValueError("Checkout HEAD must be the requested full source commit")
    if output("git", "status", "--porcelain", "--untracked-files=normal"):
        raise ValueError("A clean build checkout is required")
    platform = output("docker", "version", "--format", "{{.Server.Os}}/{{.Server.Arch}}")
    if platform not in ("linux/amd64", "linux/arm64"):
        raise ValueError("A native Linux Docker engine is required")
    arch = platform.split("/")[1]
    local_arch = {"aarch64": "arm64", "x86_64": "amd64"}.get(os.uname().machine)
    if os.uname().sysname != "Linux" or local_arch != arch:
        raise ValueError("Run the build inside the native Linux node")
    if (arch == "arm64") != (infra_release is not None):
        raise ValueError("Provide the frozen Compose Infra manifest on the ARM64 builder only")
    root = Path.cwd().resolve()
    destination = destination.resolve()
    if destination.is_relative_to(root):
        raise ValueError("The bundle destination must be outside the source checkout")
    destination.mkdir(mode=0o700)
    images = {}
    infra = None
    tag = f"{commit}-{arch}"

    def inspect(name: str, reference: str) -> None:
        image = json.loads(output("docker", "image", "inspect", reference))[0]
        if f"{image['Os']}/{image['Architecture']}" != platform:
            raise ValueError(f"Image architecture mismatch: {name}")
        images[name] = {"image": reference, "id": image["Id"]}

    if infra_release is not None:
        infra = json.loads(infra_release.read_text())
        if infra["schema"] != "addp.compose-release/v1" or infra["platform"] != platform:
            raise ValueError("The Infra manifest must be a frozen ARM64 Compose release")
        for service in INFRA:
            source = infra["projects"]["infra"][service]
            expected = infra["image_ids"][source]
            reference = f"addp-pilot.local/addp-infra-{service}:{tag}"
            subprocess.run(["docker", "tag", expected, reference], check=True)
            inspect(service, reference)
            if images[service]["id"] != expected:
                raise ValueError(f"Infra image differs from the frozen release: {service}")
            images[service]["source"] = source

    services = ",".join(PRODUCTS.values())
    subprocess.run(["make", "build", f"BUILD_ARGS=--arch {arch} --services {services}"], check=True)
    image_args = f"--verify --jobs 1 --services {services} --tag {tag} --registry {builder_registry}"
    subprocess.run(["make", "build-images", f"IMAGE_BUILD_ARGS={image_args}"], check=True)
    bootstrap_env = {**os.environ,
                     "GOMODCACHE": os.environ.get("ADDP_BUILD_GOMODCACHE", str(root / ".gomodcache")),
                     "GOCACHE": os.environ.get("ADDP_BUILD_GOCACHE", str(root / ".cache/go-build"))}
    subprocess.run(["make", "build-iam-bootstrap", "GOOS=linux", f"GOARCH={arch}"], env=bootstrap_env, check=True)
    shutil.copy2(Path(f"dist/release-linux-{arch}/addp-iam-bootstrap"), destination)
    for module, service in PRODUCTS.items():
        reference = f"addp-pilot.local/addp-{service}:{tag}"
        subprocess.run(["docker", "tag", f"{builder_registry}/addp-{service}:{tag}", reference], check=True)
        inspect(module, reference)
        declaration = output("docker", "run", "--rm", "--network", "none", "--entrypoint", "cat",
                             reference, "/app/module-declaration.json")
        if json.loads(declaration)["module_name"] != module:
            raise ValueError(f"Image declaration belongs to a different module: {module}")
        (destination / f"{module}.json").write_text(declaration + "\n")
    archive = destination / "images.tar"
    subprocess.run(["docker", "save", "-o", str(archive),
                    *(image["image"] for image in images.values())], check=True)
    identities = archive_identities(archive)
    for image in images.values():
        image["config_digest"] = identities[image["image"]]
    manifest = {"schema": "addp.k3s-pilot/v1", "commit": commit, "arch": arch, "images": images}
    if infra is not None:
        manifest["infra_source"] = {"commit": infra["source_commit"], "manifest_sha256": digest(infra_release)}
        # The official System Compose service already specifies this complete key set.
        manifest["service_secret_keys"] = system_secret_keys(Path("docker-compose.yml").read_text())
        shutil.copy2("scripts/infra/redis-authentication.sh", destination)
    (destination / "release.json").write_text(json.dumps(manifest, indent=2) + "\n")
    for name in ("k3s-pilot.py", "k3s-pilot.md"):
        shutil.copy2(TOOLS / "prod" / name, destination)
    shutil.copy2("docs/guide/addp IAM三员初始化操作指南.md", destination / "iam-bootstrap.md")
    files = sorted(destination.iterdir())
    with (destination / "SHA256SUMS").open("x") as sums:
        for file in files:
            sums.write(f"{digest(file)}  {file.name}\n")
    print(f"Exported {arch} bundle for {commit}: {destination}")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--infra-release", type=Path)
    parser.add_argument("--builder-registry", default="localhost:5001")
    args = parser.parse_args()
    try:
        if not re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9.-]*(?::[0-9]+)?", args.builder_registry):
            raise ValueError("Expected a builder registry hostname and optional port")
        package(args.commit, args.output, args.infra_release, args.builder_registry)
    except (OSError, ValueError, KeyError, tarfile.TarError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"K3s pilot packaging failed: {error}\n")


if __name__ == "__main__":
    main()
