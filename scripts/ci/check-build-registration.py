#!/usr/bin/env python3
"""校验正式服务、Worker、前端和 Compose 镜像均登记到唯一构建入口。"""

from __future__ import annotations

import argparse
import glob
import re
import shlex
import subprocess
import sys
from pathlib import Path


class RegistrationError(RuntimeError):
    pass


RETIRED_MAKE_TARGETS = {
    "backup",
    "build-backend",
    "build-workers",
    "build-backend-multiarch",
    "build-workers-multiarch",
    "build-backend-all-multiarch",
    "build-backend-all-local",
    "build-frontend",
    "build-debug",
    "build-release",
    "build-backends",
    "prod-build-images",
    "docker-build",
    "docker-build-all",
    "clean",
    "clean-all",
    "clean-dist",
    "check-frontend",
    "dev-all",
    "dev-gateway",
    "dev-geopython-workflow",
    "dev-health",
    "dev-manager",
    "dev-meta",
    "dev-orchestrator",
    "dev-system",
    "dev-transfer",
    "db-migrate",
    "db-shell",
    "docs",
    "down",
    "fix-frontend",
    "fmt",
    "health",
    "init-minio-mvt",
    "init",
    "install-deps",
    "lint",
    "logs",
    "logs-gateway",
    "logs-manager",
    "logs-meta",
    "logs-orchestrator",
    "logs-system",
    "logs-transfer",
    "minio-setup",
    "prod-down",
    "prod-down-addp",
    "prod-down-infra",
    "prod-logs",
    "prod-logs-addp",
    "prod-logs-develop",
    "prod-logs-infra",
    "prod-logs-orchestrator",
    "prod-restart-addp",
    "prod-restart-infra",
    "prod-status",
    "prod-up",
    "prod-up-addp",
    "prod-up-infra",
    "ps",
    "restart",
    "restart-full",
    "registry-restart",
    "registry-stop",
    "redis-cli",
    "restore",
    "status",
    "test-system",
    "up",
    "up-full",
    "up-infra",
    "update-deps",
}

AUXILIARY_DOCKERFILES = {
    "engines/model3d-workflow/docker/converter/Dockerfile": "model3d converter build",
    "engines/model3d-workflow/docker/runtime/Dockerfile": "model3d runtime build",
    "engines/supermap-workflow/Dockerfile.base": "SuperMap SDK base image build",
    "scripts/infra/Dockerfile.postgres": "infra PostgreSQL image build",
}


def compiled_binary_name(service: str) -> str:
    if service.endswith("-backend"):
        return service.removesuffix("-backend")
    return service


def image_build_definition(service: str, directory: str) -> tuple[str, str | None, str | None]:
    """返回构建定义、预编译二进制名和 Docker build context。"""
    if service == "model3d-workflow-engine":
        return f"{directory}/scripts/build-linux-arm64-images.sh", None, None
    if service == "supermap-workflow-engine":
        return f"{directory}/Dockerfile", None, directory
    if service in {"transfer-bounded-worker", "meta-worker", "quality-worker", "security-worker"}:
        return f"{directory}/Dockerfile.prebuilt.worker", service, "."
    if service == "develop-query-worker":
        return f"{directory}/Dockerfile.prebuilt.query-worker", service, "."
    if service == "transfer-continuous-worker":
        return f"{directory}/Dockerfile.prebuilt.continuous-worker", service, "."
    if service in {"agent-backend", "copilot-backend"}:
        return f"{directory}/Dockerfile", None, "."
    if service.endswith("-backend") or service == "gateway":
        return f"{directory}/Dockerfile.prebuilt", compiled_binary_name(service), "."
    if service == "nginx":
        return "nginx/Dockerfile", None, "nginx"
    if service == "spark-workflow-engine":
        return f"{directory}/Dockerfile", None, directory
    if service.endswith("-frontend") or service == "console" or service.endswith("-engine") or service == "raster-mosaic-runtime":
        return f"{directory}/Dockerfile", None, "."
    raise RegistrationError(f"{service}: no static image build definition rule")


def dockerfile_copies_binary(text: str, binary: str) -> bool:
    expected = rf"dist/\$\{{BUILD_TYPE\}}-\$\{{GOOS\}}-\$\{{BUILD_ARCH\}}/{re.escape(binary)}"
    return re.search(rf"(?m)^\s*COPY\s+{expected}(?:\s|$)", text) is not None


def dockerignore_pattern_regex(pattern: str) -> re.Pattern[str]:
    anchored = pattern.startswith("/")
    pattern = pattern.removeprefix("/").removesuffix("/")
    contains_slash = "/" in pattern
    result = ""
    index = 0
    while index < len(pattern):
        character = pattern[index]
        if character == "*":
            if index + 1 < len(pattern) and pattern[index + 1] == "*":
                index += 1
                if index + 1 < len(pattern) and pattern[index + 1] == "/":
                    index += 1
                    result += "(?:.*/)?"
                else:
                    result += ".*"
            else:
                result += "[^/]*"
        elif character == "?":
            result += "[^/]"
        else:
            result += re.escape(character)
        index += 1
    prefix = "^" if anchored or contains_slash else r"(?:^|.*/)"
    return re.compile(prefix + result + r"(?:/.*)?$")


def dockerignore_excludes(context_root: Path, relative_path: str) -> bool:
    ignore_file = context_root / ".dockerignore"
    if not ignore_file.is_file():
        return False
    excluded = False
    normalized = relative_path.strip("/")
    for raw_line in ignore_file.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        negated = line.startswith("!")
        pattern = line[1:] if negated else line
        if not pattern or pattern == ".":
            continue
        if dockerignore_pattern_regex(pattern).match(normalized):
            excluded = not negated
    return excluded


def dockerfile_context_errors(
    repository: Path,
    service: str,
    dockerfile: str,
    context: str,
    available_files: set[str],
) -> list[str]:
    errors: list[str] = []
    repository_root = repository.resolve()
    context_root = (repository / context).resolve()
    text = (repository / dockerfile).read_text(encoding="utf-8")
    for line_number, line in enumerate(text.splitlines(), start=1):
        stripped = line.strip()
        if not stripped.upper().startswith("COPY ") or "--from=" in stripped:
            continue
        try:
            tokens = shlex.split(stripped)
        except ValueError as error:
            errors.append(f"{service}: {dockerfile}:{line_number} invalid COPY: {error}")
            continue
        arguments = [token for token in tokens[1:] if not token.startswith("--")]
        for source in arguments[:-1]:
            if "$" in source:
                continue
            source_path = (context_root / source.removesuffix("/")).resolve()
            try:
                source_path.relative_to(context_root)
            except ValueError:
                errors.append(
                    f"{service}: {dockerfile}:{line_number} COPY source escapes build context {context}: {source}"
                )
                continue
            matches = glob.glob(str(source_path)) if glob.has_magic(source) else [str(source_path)]
            existing_matches = [Path(match) for match in matches if Path(match).exists()]
            if not existing_matches:
                errors.append(
                    f"{service}: {dockerfile}:{line_number} COPY source is missing from build context {context}: {source}"
                )
                continue
            available_matches = []
            for match in existing_matches:
                relative_match = match.resolve().relative_to(repository_root).as_posix()
                if match.is_file() and relative_match in available_files:
                    available_matches.append(match)
                elif match.is_dir() and (
                    relative_match == "."
                    or any(
                        available == relative_match
                        or available.startswith(relative_match + "/")
                        for available in available_files
                    )
                ):
                    available_matches.append(match)
            if not available_matches:
                errors.append(
                    f"{service}: {dockerfile}:{line_number} COPY source is unavailable in the worktree: {source}"
                )
                continue
            visible_matches = [
                match
                for match in available_matches
                if not dockerignore_excludes(
                    context_root, match.relative_to(context_root).as_posix()
                )
            ]
            if not visible_matches:
                errors.append(
                    f"{service}: {dockerfile}:{line_number} COPY source is excluded by "
                    f"{context}/.dockerignore: {source}"
                )
    return errors


def git_files(repository: Path, *patterns: str) -> list[str]:
    result = subprocess.run(
        ["git", "ls-files", *patterns],
        cwd=repository,
        check=True,
        capture_output=True,
        text=True,
    )
    return sorted(line for line in result.stdout.splitlines() if line)


def repository_files(repository: Path, *patterns: str) -> list[str]:
    """Return tracked and untracked worktree files for pre-commit validation."""
    result = subprocess.run(
        ["git", "ls-files", "-z", "-co", "--exclude-standard", "--", *patterns],
        cwd=repository,
        check=True,
        capture_output=True,
        text=True,
    )
    return sorted(
        path
        for path in result.stdout.split("\0")
        if path and (repository / path).is_file()
    )


def shell_array(text: str, declaration: str) -> dict[str, str]:
    match = re.search(
        rf"(?ms)^\s*(?:local\s+)?{re.escape(declaration)}=\(\s*(?P<body>.*?)^\s*\)",
        text,
    )
    if not match:
        raise RegistrationError(f"shell array {declaration} is missing")
    entries = re.findall(r'^\s*"([^":]+):([^"\n]+)"\s*$', match.group("body"), re.MULTILINE)
    return dict(entries)


def quoted_shell_array(text: str, declaration: str) -> list[str]:
    match = re.search(
        rf"(?ms)^\s*(?:local\s+)?{re.escape(declaration)}=\(\s*(?P<body>.*?)^\s*\)",
        text,
    )
    if not match:
        raise RegistrationError(f"shell array {declaration} is missing")
    return re.findall(r'^\s*"([^"\n]+)"\s*$', match.group("body"), re.MULTILINE)


def base_image_seed_entries(text: str) -> list[tuple[str, str]]:
    entries: list[tuple[str, str]] = []
    for entry in quoted_shell_array(text, "base_images"):
        source, separator, target = entry.partition("=")
        entries.append((source, target if separator else source))
    return entries


def seeded_base_images(text: str) -> set[str]:
    return {target for _, target in base_image_seed_entries(text)}


def uses_latest_tag(image: str) -> bool:
    name_and_tag = image.partition("@")[0]
    return name_and_tag.rpartition(":")[2] == "latest"


def local_registry_base_images(text: str) -> set[str]:
    """返回 Dockerfile 从本地 Registry 引用的基础镜像，排除内部构建阶段。"""
    arguments: dict[str, str] = {}
    stages: set[str] = set()
    images: set[str] = set()
    for line in text.splitlines():
        stripped = line.strip()
        argument = re.match(r"(?i)^ARG\s+([A-Za-z_][A-Za-z0-9_]*)(?:=(\S+))?", stripped)
        if argument and argument.group(2) is not None:
            arguments[argument.group(1)] = argument.group(2)
            continue
        instruction = re.match(
            r"(?i)^FROM(?:\s+--\S+)*\s+(\S+)(?:\s+AS\s+(\S+))?", stripped
        )
        if not instruction:
            continue
        image = instruction.group(1)
        variable = re.fullmatch(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}", image)
        if variable:
            image = arguments.get(variable.group(1), image)
        if image not in stages and image.startswith("localhost:5001/"):
            images.add(image.removeprefix("localhost:5001/"))
        if instruction.group(2):
            stages.add(instruction.group(2))
    return images


def expected_compile_entries(repository: Path) -> dict[str, str]:
    expected: dict[str, str] = {}
    for path in repository_files(repository, "*/backend/cmd/server/main.go"):
        module = path.split("/", 1)[0]
        expected[f"{module}-backend"] = f"{module}/backend"
    if (repository / "gateway/cmd/gateway/main.go").is_file():
        expected["gateway"] = "gateway"
    for path in repository_files(repository, "*/backend/cmd/*worker/main.go"):
        parts = Path(path).parts
        module, command = parts[0], parts[3]
        if command == "worker":
            name = f"{module}-bounded-worker" if module == "transfer" else f"{module}-worker"
        else:
            name = f"{module}-{command}"
        expected[name] = f"{module}/backend"
    return expected


def compose_images(text: str) -> set[str]:
    return set(re.findall(r"^\s*image:\s*.*?/addp-([a-z0-9-]+):", text, re.MULTILINE))


def make_recipe(makefile: str, target: str) -> str | None:
    match = re.search(
        rf"(?ms)^{re.escape(target)}\s*:[^\n]*\n(?P<recipe>(?:\t[^\n]*\n?)*)",
        makefile,
    )
    return match.group("recipe") if match else None


def makefile_script_references(makefile: str) -> set[str]:
    """返回根 Makefile 引用的仓库内 scripts/ 路径。"""
    return set(re.findall(r"(?<![A-Za-z0-9_./-])(scripts/[A-Za-z0-9_./-]+)", makefile))


def validate_registration(repository: Path) -> list[str]:
    compile_script = (repository / "scripts/build/compile.sh").read_text(encoding="utf-8")
    image_script = (repository / "scripts/build/build-images.sh").read_text(encoding="utf-8")
    compose = (repository / "docker-compose.yml").read_text(encoding="utf-8")
    makefile = (repository / "Makefile").read_text(encoding="utf-8")
    platform_workflow_path = repository / ".github/workflows/platform-ci.yml"
    release_workflow_path = repository / ".github/workflows/release-and-t2-gates.yml"
    compiled = shell_array(compile_script, "SERVICES")
    images = shell_array(image_script, "services")
    seed_entries = base_image_seed_entries(image_script)
    seeded_images = seeded_base_images(image_script)
    expected_compiled = expected_compile_entries(repository)
    available_files = set(repository_files(repository))
    errors: list[str] = []
    registered_dockerfiles: set[str] = set()

    if not platform_workflow_path.is_file():
        errors.append(".github/workflows/platform-ci.yml is missing")
    else:
        platform_workflow = platform_workflow_path.read_text(encoding="utf-8")
        product_match = re.search(
            r"(?ms)^  product-build:\s*\n(?P<job>.*?)(?=^  [A-Za-z0-9_-]+:\s*$|\Z)",
            platform_workflow,
        )
        product_job = product_match.group("job") if product_match else ""
        if not product_job:
            errors.append("Platform CI product-build job is missing")
        elif not re.search(
            r"(?m)^\s*(?:-\s*)?run:\s*make\s+build\s+BUILD_ARGS=--force\s*$",
            product_job,
        ):
            errors.append("Platform CI must run make build BUILD_ARGS=--force")
        if "fetch-depth: 0" not in product_job:
            errors.append("Platform CI product build must check out full Git history")
        if 'make registry-start' not in product_job:
            errors.append("Platform CI product build must start the standard local registry")
        if "make --no-print-directory select-image-services" not in product_job:
            errors.append("Platform CI must select baseline and affected product images")
        if not re.search(
            r'(?m)^\s*(?:-\s*)?run:\s*make\s+build-images\s+'
            r'IMAGE_BUILD_ARGS="--verify\s+--services\s+\$IMAGE_SERVICES"\s*$',
            product_job,
        ):
            errors.append("Platform CI must verify selected images through make build-images")
        if (
            "ADDP_CI_SUMMARY_FILE" not in product_job
            or "details-file:" not in product_job
        ):
            errors.append("Platform CI product build must publish image diagnostics")

        go_match = re.search(
            r"(?ms)^  go-tests:\s*\n(?P<job>.*?)(?=^  [A-Za-z0-9_-]+:\s*$|\Z)",
            platform_workflow,
        )
        go_job = go_match.group("job") if go_match else ""
        if not go_job:
            errors.append("Platform CI go-tests job is missing")
        elif "ADDP_CI_SUMMARY_FILE" not in go_job or "details-file:" not in go_job:
            errors.append("Platform CI Go workspace gate must publish module diagnostics")

    if not release_workflow_path.is_file():
        errors.append(".github/workflows/release-and-t2-gates.yml is missing")
    else:
        release_workflow = release_workflow_path.read_text(encoding="utf-8")
        eligibility_match = re.search(
            r"(?ms)^  cli-release-eligibility:\s*\n(?P<job>.*?)(?=^  [A-Za-z0-9_-]+:\s*$|\Z)",
            release_workflow,
        )
        eligibility_job = eligibility_match.group("job") if eligibility_match else ""
        if not eligibility_job:
            errors.append("CLI release eligibility job is missing")
        else:
            if "fetch-depth: 0" not in eligibility_job:
                errors.append("CLI release eligibility must check out full Git history")
            if "actions: read" not in eligibility_job:
                errors.append("CLI release eligibility must read Platform CI results")
            if "scripts/ci/check-release-eligibility.py" not in eligibility_job:
                errors.append("CLI release eligibility must invoke its standard checker")
        release_match = re.search(
            r"(?ms)^  release-cli:\s*\n(?P<job>.*?)(?=^  [A-Za-z0-9_-]+:\s*$|\Z)",
            release_workflow,
        )
        release_job = release_match.group("job") if release_match else ""
        if not release_job:
            errors.append("CLI GitHub Release job is missing")
        else:
            if "cli-release-eligibility" not in release_job:
                errors.append("CLI GitHub Release must require release eligibility")
            if "actions/attest@" not in release_job:
                errors.append("CLI GitHub Release must attest the verified wheel")
            if "gh release create" not in release_job:
                errors.append("CLI GitHub Release must publish through the single release path")

    version_prepare_recipe = make_recipe(makefile, "prepare-cli-release")
    if version_prepare_recipe is None:
        errors.append("Makefile target prepare-cli-release is missing")
    elif "scripts/ci/update-cli-version.py" not in version_prepare_recipe:
        errors.append("Makefile prepare-cli-release must invoke the standard version updater")
    if "scripts/ci/update-cli-version_test.py" not in makefile:
        errors.append("Makefile test-platform must run the CLI version updater tests")

    release_check_recipe = make_recipe(makefile, "check-cli-release")
    if release_check_recipe is None:
        errors.append("Makefile target check-cli-release is missing")
    elif (
        "scripts/ci/check-release-eligibility.py" not in release_check_recipe
        or "--pre-tag" not in release_check_recipe
    ):
        errors.append("Makefile check-cli-release must invoke the standard pre-tag checker")

    for source, target in seed_entries:
        if uses_latest_tag(source):
            errors.append(
                f"seed_base_images source uses floating latest tag: {source}"
            )
        if uses_latest_tag(target):
            errors.append(
                f"seed_base_images target uses floating latest tag: {target}"
            )

    for name, directory in sorted(expected_compiled.items()):
        if name not in compiled:
            errors.append(f"{name}: scripts/build/compile.sh registration is missing")
        elif compiled[name] != directory:
            errors.append(f"{name}: compile directory must be {directory}, got {compiled[name]}")
    for name in sorted(set(compiled) - set(expected_compiled)):
        errors.append(f"{name}: compile registration has no formal server/worker entry point")

    required_images = compose_images(compose)
    for name in sorted(required_images - set(images)):
        errors.append(f"{name}: scripts/build/build-images.sh registration is missing")
    for name, directory in sorted(images.items()):
        if not (repository / directory).is_dir():
            errors.append(f"{name}: image build directory does not exist: {directory}")
            continue
        try:
            definition, binary, context = image_build_definition(name, directory)
        except RegistrationError as error:
            errors.append(str(error))
            continue
        definition_path = repository / definition
        if not definition_path.is_file():
            errors.append(f"{name}: image build definition does not exist: {definition}")
            continue
        if definition_path.name.startswith("Dockerfile"):
            registered_dockerfiles.add(definition)
        if binary is not None and not dockerfile_copies_binary(
            definition_path.read_text(encoding="utf-8"), binary
        ):
            errors.append(
                f"{name}: {definition} does not COPY compiled binary {binary} "
                "from dist/${BUILD_TYPE}-${GOOS}-${BUILD_ARCH}"
            )
        if context is not None:
            errors.extend(
                dockerfile_context_errors(
                    repository, name, definition, context, available_files
                )
            )
            for base_image in sorted(
                local_registry_base_images(definition_path.read_text(encoding="utf-8"))
            ):
                if uses_latest_tag(base_image):
                    errors.append(
                        f"{name}: base image localhost:5001/{base_image} uses "
                        "floating latest tag"
                    )
                if base_image not in seeded_images:
                    errors.append(
                        f"{name}: base image localhost:5001/{base_image} is not "
                        "registered in seed_base_images"
                    )

    for path in repository_files(repository, "*/frontend/package.json"):
        module = path.split("/", 1)[0]
        image = "console" if module == "console" else f"{module}-frontend"
        if image not in images:
            errors.append(f"{path}: image registration {image} is missing")

    available_dockerfiles = set(repository_files(repository, "*Dockerfile*"))
    for path in sorted(available_dockerfiles):
        dockerfile_path = repository / path
        if not dockerfile_path.is_file():
            continue
        dockerfile_text = dockerfile_path.read_text(encoding="utf-8")
        if re.search(r"@rollup/rollup-linux-(?:arm64|x64)-musl", dockerfile_text):
            errors.append(
                f"{path}: Rollup native package architecture must be selected dynamically"
            )
    for path in sorted(
        available_dockerfiles - registered_dockerfiles - set(AUXILIARY_DOCKERFILES)
    ):
        errors.append(f"{path}: Dockerfile is not registered or classified as auxiliary")
    for path, purpose in sorted(AUXILIARY_DOCKERFILES.items()):
        if not (repository / path).is_file():
            errors.append(f"{path}: auxiliary Dockerfile is missing ({purpose})")
        elif path not in available_files:
            errors.append(f"{path}: auxiliary Dockerfile is unavailable in the worktree ({purpose})")

    for name in sorted(expected_compiled):
        if name not in images:
            errors.append(f"{name}: compiled service/worker image registration is missing")

    build_recipe = make_recipe(makefile, "build")
    if build_recipe is None or "scripts/build/compile.sh" not in build_recipe:
        errors.append("Makefile target build must invoke scripts/build/compile.sh")
    images_recipe = make_recipe(makefile, "build-images")
    if images_recipe is None or "scripts/build/build-images.sh" not in images_recipe:
        errors.append("Makefile target build-images must invoke scripts/build/build-images.sh")
    selection_recipe = make_recipe(makefile, "select-image-services")
    if selection_recipe is None or "scripts/ci/select-image-services.py" not in selection_recipe:
        errors.append(
            "Makefile target select-image-services must invoke scripts/ci/select-image-services.py"
        )
    go_test_recipe = make_recipe(makefile, "test-go")
    if go_test_recipe is None or "GOWORK=off go mod tidy -diff" not in go_test_recipe:
        errors.append("Makefile target test-go must reject untidy Go module files")
    if go_test_recipe is None or "ADDP_CI_SUMMARY_FILE" not in go_test_recipe:
        errors.append("Makefile target test-go must report the failing Go module to CI")
    if "ADDP_CI_SUMMARY_FILE" not in image_script:
        errors.append("build-images.sh must report selected and failed images to CI")
    for target in sorted(RETIRED_MAKE_TARGETS):
        if make_recipe(makefile, target) is not None:
            errors.append(f"Makefile retired target still exists: {target}")
    available_scripts = set(repository_files(repository, "scripts/**"))
    for path in sorted(makefile_script_references(makefile) - available_scripts):
        errors.append(f"Makefile references missing script: {path}")
    for path in repository_files(repository, "*Makefile"):
        if path != "Makefile":
            errors.append(f"{path}: module Makefile duplicates the root build entry point")
    return errors


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, default=Path.cwd())
    return parser.parse_args()


def main() -> int:
    repository = parse_args().repository.resolve()
    try:
        errors = validate_registration(repository)
        compile_count = len(expected_compile_entries(repository))
        image_count = len(shell_array(
            (repository / "scripts/build/build-images.sh").read_text(encoding="utf-8"),
            "services",
        ))
    except (OSError, RegistrationError, subprocess.CalledProcessError) as error:
        print(f"Build registration check failed: {error}", file=sys.stderr)
        return 1
    if errors:
        for error in errors:
            print(f"Build registration check failed: {error}", file=sys.stderr)
        return 1
    print(
        "Build registration check passed: "
        f"{compile_count} Go services/workers and {image_count} images are registered."
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
