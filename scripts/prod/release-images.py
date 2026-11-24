#!/usr/bin/env python3
"""Prepare and verify the immutable images consumed by the Compose production entry."""

import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tempfile


ROOT = Path(__file__).resolve().parents[2]
COMPOSE_FILES = {
    "infra": "docker-compose.infra.yml",
    "platform": "docker-compose.yml",
    "runtimes": "docker-compose.runtimes.yml",
}
CAPABILITIES = {
    "ADDP_SUPERMAP_WORKFLOW_ENABLED": "true",
    "ADDP_OBSERVABILITY_LOGS_ENABLED": "true",
    "ADDP_OBSERVABILITY_TRACES_ENABLED": "false",
    "ADDP_OBSERVABILITY_METRICS_ENABLED": "false",
    "ADDP_REDIS_METRICS_ENABLED": "false",
    "ADDP_MINIO_METRICS_ENABLED": "false",
    "ADDP_REDPANDA_METRICS_ENABLED": "false",
    "ADDP_KAFKA_CONNECT_METRICS_ENABLED": "false",
    "ADDP_MEILISEARCH_METRICS_ENABLED": "false",
    **{"ADDP_"+kind+"_METRICS_ENABLED": "false" for kind in ("FALKORDB","MYSQL","NEO4J","FTP","SPARK")},
}
# The release owns these choices. New providers enter the production selection
# after the selected consumers and the deployment lifecycle have been verified.
INFRA_PROVIDERS = {
    "ADDP_INFRA_DATABASE_PROVIDER": "postgres",
    "ADDP_INFRA_OBJECTSTORE_PROVIDER": "minio",
    "ADDP_INFRA_SEARCH_PROVIDER": "meilisearch",
    "ADDP_INFRA_MESSAGE_PROVIDER": "redpanda",
    "ADDP_INFRA_CACHE_PROVIDER": "redis",
}
PROFILES = {
    "ADDP_SUPERMAP_WORKFLOW_ENABLED": "supermap-workflow",
    "ADDP_OBSERVABILITY_LOGS_ENABLED": "observability-logs",
    "ADDP_OBSERVABILITY_TRACES_ENABLED": "observability-traces",
    "ADDP_OBSERVABILITY_METRICS_ENABLED": "observability-metrics",
}
METRIC_IMAGES = (
    ("redis", "ADDP_REDIS_METRICS_ENABLED", "scripts/infra/redis-metrics.py"),
    ("minio", "ADDP_MINIO_METRICS_ENABLED", "scripts/infra/minio-observation.py"),
    ("redpanda", "ADDP_REDPANDA_METRICS_ENABLED", "scripts/infra/cdc-metrics.py"),
    ("kafka-connect", "ADDP_KAFKA_CONNECT_METRICS_ENABLED", "scripts/infra/cdc-metrics.py"),
    *((kind, "ADDP_"+kind.upper()+"_METRICS_ENABLED", "scripts/infra/cdc-metrics.py")
      for kind in ("falkordb","mysql","neo4j","ftp","spark")),
    ("meilisearch", "ADDP_MEILISEARCH_METRICS_ENABLED", "scripts/infra/cdc-metrics.py"),
)


def output(arguments, env=None):
    return subprocess.check_output(arguments, cwd=ROOT, env=env, text=True).strip()


def execute(arguments, env):
    print(shlex.join(arguments), file=sys.stderr, flush=True)
    subprocess.run(arguments, cwd=ROOT, env=env, stdout=sys.stderr, check=True)


def load_module(name, path):
    spec = importlib.util.spec_from_file_location(name, ROOT / path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


def selection(env):
    values = {name: env.get(name, default) for name, default in CAPABILITIES.items()}
    if any(value not in ("true", "false") for value in values.values()):
        raise ValueError("Release capability selections must be true or false")
    scope = env.get("ADDP_DEPLOYMENT_SCOPE", "full")
    if scope not in ("full", "system-meta-gateway"):
        raise ValueError(f"unsupported deployment scope {scope!r}")
    values["ADDP_DEPLOYMENT_SCOPE"] = scope
    extra = env.get("ADDP_DEPLOYMENT_MODULES", "").strip()
    if extra:
        modules = {name.strip() for name in extra.split(",")}
        if scope == "full" or not modules.issubset({"inference", "manager", "orchestrator", "security", "system", "workbench"}):
            raise ValueError(f"unsupported ADDP_DEPLOYMENT_MODULES for {scope}: {extra!r}")
        values["ADDP_DEPLOYMENT_MODULES"] = ",".join(sorted(modules))
    for key, supported in INFRA_PROVIDERS.items():
        configured = env.get(key, supported)
        supported_mysql = key == "ADDP_INFRA_DATABASE_PROVIDER" and scope == "system-meta-gateway" and configured == "mysql"
        if configured != supported and not supported_mysql:
            raise ValueError(f"{key}={configured!r} is not supported by the {scope} Compose release")
        values[key] = configured
    return values


def release_environment(env, version, registry, platform):
    result = dict(env, IMAGE_TAG=version, REGISTRY=registry, DOCKER_DEFAULT_PLATFORM=platform)
    result.pop("ADDP_RELEASE_MANIFEST", None)
    result.pop("COMPOSE_PROFILES", None)
    selected = selection(result)
    profiles = [profile for flag, profile in PROFILES.items() if selected[flag] == "true"]
    if "ADDP_DEPLOYMENT_MODULES" in selected:
        profiles.extend("module-" + module for module in selected["ADDP_DEPLOYMENT_MODULES"].split(","))
    if profiles:
        result["COMPOSE_PROFILES"] = ",".join(profiles)
    # Inactive observability ports are still interpolated by Compose.
    for name in ("PROMETHEUS_PORT", "TRACE_QUERY_PORT", "TRACE_OTLP_PORT", "LOKI_PORT", "ALLOY_PORT"):
        result[name] = "0"
    if not result.get("POSTGRES_IMAGE"):
        result["POSTGRES_IMAGE"] = (
            f"{registry}/addp-infra-postgres:{version}"
            if platform == "linux/amd64" else "pampa0629/addp-postgres:15-arm64"
        )
    return result


def compose_model(project, env, override=None):
    source = COMPOSE_FILES[project]
    if selection(env)["ADDP_DEPLOYMENT_SCOPE"] == "system-meta-gateway":
        if project != "platform":
            return {}
        source = "scripts/prod/system-meta-gateway.yml"
    command = ["docker", "compose", "-f", source]
    if project == "infra" and selection(env)["ADDP_OBSERVABILITY_TRACES_ENABLED"] == "true":
        command += ["-f", "scripts/infra/traces-platform.yml"]
    if override is not None:
        command += ["-f", str(override)]
    active = output(command + ["config", "--services"], env).splitlines()
    model = json.loads(output(command + ["config", "--format", "json"], env))
    return {name: model["services"][name] for name in active}


def compose_inputs():
    paths = set(COMPOSE_FILES.values())
    paths.update(("scripts/infra/traces-platform.yml", "scripts/prod/geopython-metrics.yml"))
    pending = list(paths)
    while pending:
        name = pending.pop()
        source = (ROOT / name).read_text()
        # Repository Compose definitions use static extends.file declarations,
        # including the inline {file: ..., service: ...} spelling.
        for match in re.finditer(r"(?:^\s*file:\s*|extends:\s*\{\s*file:\s*)([^,\n}#]+)", source, re.M):
            relative, = shlex.split(match[1].strip())
            path = (ROOT / name).parent / relative
            normalized = str(path.resolve().relative_to(ROOT.resolve()))
            if normalized not in paths:
                paths.add(normalized)
                pending.append(normalized)
    return {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in sorted(paths)}


def native_platform(env):
    info = json.loads(output(["docker", "info", "--format", "{{json .}}"], env))
    arch = {"aarch64": "arm64", "arm64": "arm64", "x86_64": "amd64", "amd64": "amd64"}.get(info["Architecture"])
    if info["OSType"] != "linux" or arch is None:
        raise ValueError("The release requires a Linux arm64 or amd64 Docker engine")
    return "linux/" + arch


def input_fingerprints(env):
    fingerprints = compose_inputs()
    capabilities = selection(env)
    if capabilities["ADDP_DEPLOYMENT_SCOPE"] == "system-meta-gateway":
        name = "scripts/prod/system-meta-gateway.yml"
        fingerprints[name] = hashlib.sha256((ROOT / name).read_bytes()).hexdigest()
    for kind, flag, path in METRIC_IMAGES:
        if capabilities[flag] == "true":
            owner = load_module("release_inputs_" + kind.replace("-", "_"), path)
            for name in owner.source_inputs(kind) if path.endswith("cdc-metrics.py") else owner.source_inputs():
                fingerprints[name] = hashlib.sha256((ROOT / name).read_bytes()).hexdigest()
    return fingerprints


def inspect_image(reference, platform, env):
    image = json.loads(output(["docker", "image", "inspect", reference], env))[0]
    if image["Os"] + "/" + image["Architecture"] != platform:
        raise ValueError(f"Release image platform mismatch: {reference}")
    return image


def owned_image(project, service, definition, env, registrations):
    if project == "infra":
        if service in ("minio", "runtime-log-store-init", "trace-store-init"):
            return "infra-minio"
        if service in ("runtime-log-pruner", "runtime-log-observer"):
            return "runtime-log"
        if service == "postgres" and definition["image"] == f"{env['REGISTRY']}/addp-infra-postgres:{env['IMAGE_TAG']}":
            return "infra-postgres"
        return None
    reference = definition["image"]
    prefix = env["REGISTRY"] + "/addp-"
    suffix = ":" + env["IMAGE_TAG"]
    if not reference.startswith(prefix) or not reference.endswith(suffix):
        raise ValueError(f"Product image does not match the selected registry/version: {service}")
    name = reference[len(prefix):-len(suffix)]
    if name not in registrations:
        raise ValueError(f"Product image is not registered: {name}")
    return name


def publish_image(source, target, platform, env):
    inspect_image(source, platform, env)
    if source != target:
        execute(["docker", "tag", source, target], env)
        execute(["docker", "push", target], env)
    image = inspect_image(target, platform, env)
    repository = target.rsplit(":", 1)[0]
    references = [ref for ref in image.get("RepoDigests", []) if ref.startswith(repository + "@sha256:")]
    if len(references) != 1:
        raise ValueError(f"Published repository digest is unavailable: {target}")
    return references[0]


def prepare_metric_sources(env, platform, registry, version):
    selected = [item for item in METRIC_IMAGES if selection(env)[item[1]] == "true"]
    if not selected:
        return {}
    sources = {}
    module = load_module("release_metrics_deployment", "scripts/utils/deployment-docker.py")
    implementation = load_module("release_metrics_source", "scripts/utils/metrics-source.py")
    docker = module.Docker(env)
    for kind, flag, path in selected:
        owner = load_module("release_metrics_" + kind.replace("-", "_"), path)
        with tempfile.TemporaryDirectory(prefix="addp-release-source-") as state:
            scope = implementation.group.PRODUCTS.get(kind, ("infra", kind))[0]
            source = implementation.Source(docker, scope, state, ROOT, kind, module.inventory.metrics_project.PROJECT)
            image_id = source.build(owner.source_inputs(kind) if path.endswith("cdc-metrics.py") else owner.source_inputs())
        image = inspect_image(image_id, platform, env)
        checkout_suffix = "-" + hashlib.sha256(str(ROOT).encode()).hexdigest()[:16]
        original, = [ref.removesuffix(checkout_suffix) for ref in image["RepoTags"]
                     if re.fullmatch("addp-" + kind + r"-metrics:[0-9a-f]{64}" + checkout_suffix, ref)]
        target = f"{registry}/addp-metrics-{kind}:{version}"
        sources[original] = publish_image(image_id, target, platform, env)
    return sources


def write_override(path, services):
    lines = ["services:"]
    for name, reference in sorted(services.items()):
        lines += [f"  {json.dumps(name)}:", f"    image: {json.dumps(reference)}",
                  "    build: !reset null", "    profiles: !reset []", "    pull_policy: never"]
    path.write_text("\n".join(lines) + "\n")


def activate(directory):
    link = ROOT / "dist/compose-release"
    with tempfile.TemporaryDirectory(prefix=".release-link-", dir=link.parent) as temporary:
        pending = Path(temporary) / "current"
        pending.symlink_to(os.path.relpath(directory, link.parent), target_is_directory=True)
        os.replace(pending, link)


def prepare(version, registry):
    if not re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}", version):
        raise ValueError("Invalid release version")
    if not registry or any(char.isspace() for char in registry) or "://" in registry:
        raise ValueError("Use a registry hostname and optional port/path")
    if subprocess.run(["git", "diff", "--quiet", "HEAD"], cwd=ROOT).returncode:
        raise ValueError("Prepare releases from a fixed clean source checkout")
    platform = native_platform(os.environ)
    env = release_environment(os.environ, version, registry, platform)
    commit = output(["git", "rev-parse", "HEAD"], env)
    configuration = json.dumps({"registry": registry, "selection": selection(env)}, sort_keys=True)
    configuration_id = hashlib.sha256(configuration.encode()).hexdigest()
    destination = ROOT / "dist/releases" / version / platform.split("/")[1] / commit / configuration_id
    if destination.exists():
        verify(destination / "manifest.json", env)
        activate(destination)
        return
    registration = load_module("release_image_registration", "scripts/ci/select-image-services.py")
    registered = dict(registration.registered_services(ROOT))
    models = {project: compose_model(project, env) for project in COMPOSE_FILES}
    owners = {}
    for project, services in models.items():
        for service, definition in services.items():
            if definition.get("platform", platform) != platform:
                raise ValueError(f"Selected service platform differs from the release: {service}")
            owners[project, service] = owned_image(project, service, definition, env, registered)
    build_names = sorted({name for name in owners.values() if name})
    compile_args = ["make", "build"]
    if selection(env)["ADDP_DEPLOYMENT_SCOPE"] == "system-meta-gateway":
        compiled = [name for name in build_names if name == "gateway" or name.endswith(("-backend", "-worker"))]
        compile_args.append("BUILD_ARGS=--services " + ",".join(compiled))
    execute(compile_args, env)
    execute(["make", "build-images", "IMAGE_BUILD_ARGS=" + shlex.join([
        "--force", "--services", ",".join(build_names), "--tag", version, "--registry", registry])], env)
    projects = {}
    published = {}
    for project, services in models.items():
        projects[project] = {}
        for service, definition in sorted(services.items()):
            owner = owners[project, service]
            source = f"{registry}/addp-{owner}:{version}" if owner else definition["image"]
            if source not in published:
                target = source if owner else f"{registry}/addp-vendor-{service}:{version}"
                if not owner:
                    try:
                        inspect_image(source, platform, env)
                    except (subprocess.CalledProcessError, ValueError):
                        execute(["docker", "pull", "--platform", platform, source], env)
                published[source] = publish_image(source, target, platform, env)
            projects[project][service] = published[source]
    sources = prepare_metric_sources(env, platform, registry, version)
    image_ids = {reference: inspect_image(reference, platform, env)["Id"]
                 for reference in {*published.values(), *sources.values()}}
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".prepare-", dir=destination.parent) as temporary:
        directory = Path(temporary)
        for project, services in projects.items():
            write_override(directory / (project + "-images.yml"), {name: image_ids[ref] for name, ref in services.items()})
        manifest = {"schema": "addp.compose-release/v1", "source_commit": commit, "build_root": str(ROOT),
                    "platform": platform, "version": version, "registry": registry,
                    "selection": selection(env), "inputs": input_fingerprints(env),
                    "projects": projects, "sources": sources, "image_ids": image_ids}
        (directory / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
        verify(directory / "manifest.json", env)
        directory.rename(destination)
    activate(destination)
    print(f"Prepared immutable Compose release: {destination}")


def verify(filename, inherited):
    filename = filename.resolve(strict=True)
    manifest = json.loads(filename.read_text())
    if manifest.get("schema") != "addp.compose-release/v1" or set(manifest.get("projects", {})) != set(COMPOSE_FILES):
        raise ValueError("Invalid Compose release manifest")
    if manifest["platform"] != native_platform(inherited):
        raise ValueError("Release platform differs from the Docker engine")
    env = release_environment(inherited, manifest["version"], manifest["registry"], manifest["platform"])
    if manifest["selection"] != selection(env):
        raise ValueError("Capability selection differs from the prepared release")
    if manifest["inputs"] != input_fingerprints(env):
        raise ValueError("Compose source inputs differ from the prepared release")
    references = set(manifest.get("sources", {}).values())
    for project, services in manifest["projects"].items():
        external_only = manifest["selection"]["ADDP_DEPLOYMENT_SCOPE"] == "system-meta-gateway" and project in ("infra", "runtimes")
        if not isinstance(services, dict) or (not services and not external_only):
            raise ValueError("Release services must be nonempty")
        references.update(services.values())
    for reference in sorted(references):
        repository, separator, digest = reference.rpartition("@sha256:")
        if not separator or not repository or not re.fullmatch(r"[0-9a-f]{64}", digest):
            raise ValueError("Every release image must use a repository digest")
        image_id = manifest["image_ids"].get(reference, "")
        if not re.fullmatch(r"sha256:[0-9a-f]{64}", image_id):
            raise ValueError("Every release digest must bind an immutable image ID")
        try:
            image = inspect_image(image_id, manifest["platform"], env)
        except subprocess.CalledProcessError:
            execute(["docker", "pull", reference], env)
            image = inspect_image(reference, manifest["platform"], env)
        if image["Id"] != image_id:
            raise ValueError(f"Release image ID differs from its published digest: {reference}")
    for project, expected in manifest["projects"].items():
        actual = compose_model(project, env, filename.parent / (project + "-images.yml"))
        if set(actual) != set(expected):
            raise ValueError(f"Selected Compose services differ from the release: {project}")
        for service, definition in actual.items():
            if definition["image"] != manifest["image_ids"][expected[service]] or definition.get("build") or definition.get("pull_policy") != "never":
                raise ValueError(f"Compose image is not frozen: {project}/{service}")
    print(f"Verified release {manifest['version']} ({manifest['platform']}), "
          f"{sum(map(len, manifest['projects'].values()))} selected services", file=sys.stderr)
    return filename


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    actions = parser.add_subparsers(dest="action", required=True)
    prepare_parser = actions.add_parser("prepare")
    prepare_parser.add_argument("--tag", required=True)
    prepare_parser.add_argument("--registry", default="localhost:5001")
    verify_parser = actions.add_parser("verify")
    verify_parser.add_argument("--manifest", type=Path, default=ROOT / "dist/compose-release/manifest.json")
    args = parser.parse_args()
    try:
        if args.action == "prepare":
            prepare(args.tag, args.registry)
        else:
            filename = verify(args.manifest, os.environ)
            print(filename)
    except (OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"Compose release {args.action} failed: {error}\n")


if __name__ == "__main__":
    main()
