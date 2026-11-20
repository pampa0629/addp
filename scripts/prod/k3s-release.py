#!/usr/bin/env python3
"""Deliver a frozen ADDP release to a mixed-architecture offline K3s cluster."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile


ARCHITECTURES = ("arm64", "amd64")
PROJECTS = ("infra", "platform", "runtimes")
SUPERMAP = "runtimes/supermap-workflow-engine"
SUPERMAP_OPTION = "ADDP_SUPERMAP_WORKFLOW_ENABLED"
PLACEHOLDER = "addp-release.invalid/"
IMAGE_HOST = "addp-k3s.local"
SCHEMA = "addp.k3s-release/v1"
IMAGE_ARCHIVE = "images.tar.gz"
DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
SERVICE = re.compile(r"[a-z0-9][a-z0-9_-]*")
POD_SPEC_PATH = {
    "Pod": ("spec",),
    "Deployment": ("spec", "template", "spec"),
    "StatefulSet": ("spec", "template", "spec"),
    "DaemonSet": ("spec", "template", "spec"),
    "Job": ("spec", "template", "spec"),
    "CronJob": ("spec", "jobTemplate", "spec", "template", "spec"),
}


def json_text(value):
    return json.dumps(value, indent=2, sort_keys=True) + "\n"


def read_json(path):
    return json.loads(path.read_text())


def sha256_file(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def read_frozen(path, arch):
    release = read_json(path)
    if (release["schema"] != "addp.compose-release/v1"
            or release["platform"] != f"linux/{arch}"
            or not re.fullmatch(r"[0-9a-f]{40}", release["source_commit"])
            or set(release["projects"]) != set(PROJECTS)):
        raise ValueError(f"Expected a frozen Compose release for linux/{arch}")
    return release


def frozen_images(release):
    entries = {project: release["projects"][project] for project in PROJECTS}
    entries["sources"] = {}
    for original, reference in release["sources"].items():
        name, _, _ = original.partition(":")
        if name in entries["sources"]:
            raise ValueError(f"Duplicate metrics source name: {name}")
        entries["sources"][name] = reference

    images = {}
    for project, services in entries.items():
        if project in PROJECTS and not services:
            raise ValueError(f"Frozen project has no images: {project}")
        for service, reference in services.items():
            repository, marker, digest = reference.rpartition("@")
            docker_id = release["image_ids"][reference]
            if not SERVICE.fullmatch(service) or not repository or not marker or not DIGEST.fullmatch(digest):
                raise ValueError(f"Invalid frozen image reference for {project}/{service}")
            if not DIGEST.fullmatch(docker_id):
                raise ValueError(f"Invalid Docker image identity for {project}/{service}")
            images[f"{project}/{service}"] = {"source": reference, "docker_id": docker_id}
    return images


def release_id(release):
    identity = {key: value for key, value in release.items() if key != "id"}
    return hashlib.sha256(json_text(identity).encode()).hexdigest()


def pair(arm_path, amd_path):
    arm = read_frozen(arm_path, "arm64")
    amd = read_frozen(amd_path, "amd64")
    for field in ("source_commit", "version", "inputs"):
        if arm[field] != amd[field]:
            raise ValueError(f"Native releases disagree on {field}")
    arm_selection = dict(arm["selection"])
    amd_selection = dict(amd["selection"])
    arm_supermap = arm_selection.pop(SUPERMAP_OPTION)
    amd_supermap = amd_selection.pop(SUPERMAP_OPTION)
    if arm_selection != amd_selection or arm_supermap not in ("true", "false") or amd_supermap != "false":
        raise ValueError("Native releases must have the same options, with SuperMap allowed on ARM64 only")

    arm_images = frozen_images(arm)
    amd_images = frozen_images(amd)
    if ((SUPERMAP in arm_images) != (arm_supermap == "true")
            or SUPERMAP in amd_images or arm_images.keys() - {SUPERMAP} != amd_images.keys()):
        raise ValueError("Native releases differ in selected services or metrics sources")

    images = {}
    for name, image in arm_images.items():
        images[name] = {"arm64": image}
        if name in amd_images:
            images[name]["amd64"] = amd_images[name]
    release = {"schema": SCHEMA, "commit": arm["source_commit"], "version": arm["version"],
               "selection": arm["selection"], "images": images}
    release["id"] = release_id(release)
    return release


def read_release(path):
    release = read_json(path)
    if release["schema"] != SCHEMA or release["id"] != release_id(release):
        raise ValueError("K3s release identity does not match the manifest")
    return release


def image_ref(release, name):
    return f"{IMAGE_HOST}/{name}:{release['id']}"


def release_label(release):
    return "addp.io/release-" + release["id"][:48]


def native_arch():
    arch = {"aarch64": "arm64", "arm64": "arm64", "x86_64": "amd64"}.get(os.uname().machine)
    if os.uname().sysname != "Linux" or arch not in ARCHITECTURES:
        raise ValueError("Run on a native ARM64 or AMD64 Linux host")
    return arch


def images_for_arch(release, arch):
    return {image_ref(release, name): builds[arch]["docker_id"]
            for name, builds in release["images"].items() if arch in builds}


def inspect_docker_images(identities, arch):
    for reference, expected in identities.items():
        response = subprocess.check_output(["docker", "image", "inspect", reference])
        image, = json.loads(response)
        if image["Id"] != expected or image["Os"] != "linux" or image["Architecture"] != arch:
            raise ValueError(f"Docker image identity or architecture differs: {reference}")


def archive_config_digests(path, arch, expected_images):
    identities = {}
    with tarfile.open(path, "r:") as archive:
        manifest = json.load(archive.extractfile("manifest.json"))
        for entry in manifest:
            config = archive.extractfile(entry["Config"]).read()
            platform = json.loads(config)
            if platform["os"] != "linux" or platform["architecture"] != arch:
                raise ValueError(f"Docker archive contains a non-native image: {entry['RepoTags']}")
            digest = "sha256:" + hashlib.sha256(config).hexdigest()
            for image in entry["RepoTags"]:
                if image in identities:
                    raise ValueError(f"Docker archive contains a duplicate image reference: {image}")
                identities[image] = digest
    if identities.keys() != expected_images.keys():
        raise ValueError("Docker archive does not contain exactly the release image references")
    return identities


def export_bundle(release, output):
    arch = native_arch()
    identities = images_for_arch(release, arch)
    inspect_docker_images({config: config for config in identities.values()}, arch)
    if output.exists():
        raise ValueError(f"Bundle destination already exists: {output}")
    with tempfile.TemporaryDirectory(dir=output.parent, prefix=".k3s-release-") as staging:
        bundle = Path(staging) / arch
        bundle.mkdir(mode=0o700)
        for image, config in identities.items():
            subprocess.run(["docker", "tag", config, image], check=True)
        tar_path = bundle / "images.tar"
        subprocess.run(["docker", "save", "--platform", f"linux/{arch}",
                        "-o", str(tar_path), *sorted(identities)], check=True)
        config_digests = archive_config_digests(tar_path, arch, identities)
        (bundle / "config-digests.json").write_text(json_text(config_digests))
        subprocess.run(["gzip", "-1", str(tar_path)], check=True)
        (bundle / "release.json").write_text(json_text(release))
        (bundle / "architecture").write_text(arch + "\n")
        script = Path(__file__).resolve()
        shutil.copy2(script, bundle / script.name)
        shutil.copy2(script.with_suffix(".md"), bundle / "README.md")
        for name in ("k3s-meta-worker-autoscaling.json", "k3s-meta-worker-deployment-patch.json"):
            shutil.copy2(script.with_name(name), bundle / name)
        with (bundle / "SHA256SUMS").open("x") as sums:
            for path in sorted(p for p in bundle.iterdir() if p.name != "SHA256SUMS"):
                sums.write(f"{sha256_file(path)}  {path.name}\n")
        bundle.rename(output)
    print(f"Exported {len(identities)} image references for linux/{arch}: {output}")


def load_node(bundle):
    arch = native_arch()
    if os.geteuid() != 0 or (bundle / "architecture").read_text().strip() != arch:
        raise ValueError("Run load-node as root on the K3s Linux node matching the bundle")
    subprocess.run(["sha256sum", "--check", "--strict", "--quiet", "SHA256SUMS"], cwd=bundle, check=True)
    release = read_release(bundle / "release.json")
    identities = read_json(bundle / "config-digests.json")
    if (identities.keys() != images_for_arch(release, arch).keys()
            or any(not DIGEST.fullmatch(digest) for digest in identities.values())):
        raise ValueError("Image configuration digests do not cover the native release images")
    subprocess.run(["k3s", "ctr", "-n", "k8s.io", "images", "import",
                    "--platform", f"linux/{arch}", str((bundle / IMAGE_ARCHIVE).resolve())],
                   stdout=subprocess.DEVNULL, check=True)
    for reference, config in identities.items():
        result = json.loads(subprocess.check_output(["k3s", "crictl", "inspecti", reference]))
        if result["status"]["id"] != config:
            raise ValueError(f"Imported image configuration digest differs: {reference}")
    print(f"Verified {len(identities)} image references for linux/{arch}")
    print(f"{release_label(release)}=ready")


def render(release, workloads):
    if workloads["kind"] != "List" or workloads["apiVersion"] != "v1":
        raise ValueError("Expected a v1 Kubernetes JSON List")
    for workload in workloads["items"]:
        kind = workload["kind"]
        if kind not in POD_SPEC_PATH:
            continue
        pod = workload
        for field in POD_SPEC_PATH[kind]:
            pod = pod[field]
        if "nodeName" in pod:
            raise ValueError("Use scheduling constraints rather than assigning nodeName")
        arches = set(ARCHITECTURES)
        for container in [*pod["containers"], *pod.get("initContainers", [])]:
            reference = container["image"]
            name = reference.removeprefix(PLACEHOLDER)
            if name == reference or name not in release["images"]:
                raise ValueError(f"Container image is not in this ADDP release: {reference}")
            arches.intersection_update(release["images"][name])
            container.update(image=image_ref(release, name), imagePullPolicy="Never")
        selector = pod.setdefault("nodeSelector", {})
        desired_arch = selector.get("kubernetes.io/arch")
        if desired_arch:
            arches.intersection_update((desired_arch,))
        if not arches:
            raise ValueError(f"No compatible architecture for {workload['metadata']['name']}")
        if len(arches) == 1:
            selector["kubernetes.io/arch"], = arches
        selector[release_label(release)] = "ready"
    return workloads


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    actions = parser.add_subparsers(dest="action", required=True)
    pair_action = actions.add_parser("pair")
    for arch in ARCHITECTURES:
        pair_action.add_argument("--" + arch, required=True, type=Path)
    pack_action = actions.add_parser("export")
    pack_action.add_argument("--release", required=True, type=Path)
    pack_action.add_argument("--output", required=True, type=Path)
    load_action = actions.add_parser("load-node")
    load_action.add_argument("--bundle", required=True, type=Path)
    render_action = actions.add_parser("render")
    render_action.add_argument("--release", required=True, type=Path)
    render_action.add_argument("--workloads", required=True, type=Path)
    args = parser.parse_args()
    try:
        if args.action == "pair":
            print(json_text(pair(args.arm64, args.amd64)), end="")
        elif args.action == "export":
            export_bundle(read_release(args.release), args.output.resolve())
        elif args.action == "load-node":
            load_node(args.bundle)
        else:
            print(json_text(render(read_release(args.release), read_json(args.workloads))), end="")
    except (OSError, ValueError, KeyError, TypeError, tarfile.TarError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"K3s release {args.action} failed: {error}\n")


if __name__ == "__main__":
    main()
