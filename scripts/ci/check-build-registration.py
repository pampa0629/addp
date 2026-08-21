#!/usr/bin/env python3
"""校验正式服务、Worker、前端和 Compose 镜像均登记到唯一构建入口。"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path


class RegistrationError(RuntimeError):
    pass


RETIRED_MAKE_TARGETS = {
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
}


def git_files(repository: Path, *patterns: str) -> list[str]:
    result = subprocess.run(
        ["git", "ls-files", *patterns],
        cwd=repository,
        check=True,
        capture_output=True,
        text=True,
    )
    return sorted(line for line in result.stdout.splitlines() if line)


def shell_array(text: str, declaration: str) -> dict[str, str]:
    match = re.search(
        rf"(?ms)^\s*(?:local\s+)?{re.escape(declaration)}=\(\s*(?P<body>.*?)^\s*\)",
        text,
    )
    if not match:
        raise RegistrationError(f"shell array {declaration} is missing")
    entries = re.findall(r'^\s*"([^":]+):([^"\n]+)"\s*$', match.group("body"), re.MULTILINE)
    return dict(entries)


def expected_compile_entries(repository: Path) -> dict[str, str]:
    expected: dict[str, str] = {}
    for path in git_files(repository, "*/backend/cmd/server/main.go"):
        module = path.split("/", 1)[0]
        expected[f"{module}-backend"] = f"{module}/backend"
    if (repository / "gateway/cmd/gateway/main.go").is_file():
        expected["gateway"] = "gateway"
    for path in git_files(repository, "*/backend/cmd/*worker/main.go"):
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


def validate_registration(repository: Path) -> list[str]:
    compile_script = (repository / "scripts/build/compile.sh").read_text(encoding="utf-8")
    image_script = (repository / "scripts/build/build-images.sh").read_text(encoding="utf-8")
    compose = (repository / "docker-compose.yml").read_text(encoding="utf-8")
    makefile = (repository / "Makefile").read_text(encoding="utf-8")
    compiled = shell_array(compile_script, "SERVICES")
    images = shell_array(image_script, "services")
    expected_compiled = expected_compile_entries(repository)
    errors: list[str] = []

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

    for path in git_files(repository, "*/frontend/package.json"):
        module = path.split("/", 1)[0]
        image = "console" if module == "console" else f"{module}-frontend"
        if image not in images:
            errors.append(f"{path}: image registration {image} is missing")

    for name in sorted(expected_compiled):
        if name not in images:
            errors.append(f"{name}: compiled service/worker image registration is missing")

    build_recipe = make_recipe(makefile, "build")
    if build_recipe is None or "scripts/build/compile.sh" not in build_recipe:
        errors.append("Makefile target build must invoke scripts/build/compile.sh")
    images_recipe = make_recipe(makefile, "build-images")
    if images_recipe is None or "scripts/build/build-images.sh" not in images_recipe:
        errors.append("Makefile target build-images must invoke scripts/build/build-images.sh")
    for target in sorted(RETIRED_MAKE_TARGETS):
        if make_recipe(makefile, target) is not None:
            errors.append(f"Makefile retired build target still exists: {target}")
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
