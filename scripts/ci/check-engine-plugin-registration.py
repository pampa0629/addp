#!/usr/bin/env python3
"""Enforce one complete, origin-correct compile-time registry for Engine Plugins."""

from __future__ import annotations

import argparse
import os
import re
import sys
from collections import Counter
from pathlib import Path


PLUGIN_IMPORT_PREFIX = "github.com/addp/common/engine/plugins/"
AGGREGATE_PATHS = {
    "general": Path("common/engine/plugins/builtin/general/init.go"),
    "extension": Path("common/engine/plugins/builtin/extension/init.go"),
}
ALL_AGGREGATE_PATH = Path("common/engine/plugins/builtin/all/init.go")
ENGINE_PLUGIN_IMPORT = re.compile(
    r'(?m)^\s*(?:import\s+)?(?:(?P<alias>[A-Za-z_][A-Za-z0-9_]*)\s+)?'
    r'"github\.com/addp/common/engine/plugin"'
)
ORIGIN_METHOD = re.compile(
    r"func\s+\([^)]*\)\s+EngineOrigin\(\)\s+string\s*\{\s*"
    r'return\s+"(?P<origin>[^"]+)"',
    re.DOTALL,
)
BLANK_IMPORT = re.compile(
    rf'(?m)^\s*(?:import\s+)?_\s+"{re.escape(PLUGIN_IMPORT_PREFIX)}(?P<package>[^"]+)"'
)


def production_go_files(repository: Path) -> list[Path]:
    result: list[Path] = []
    for directory, subdirectories, filenames in os.walk(repository):
        subdirectories[:] = [
            name
            for name in subdirectories
            if not name.startswith(".") and name not in {"node_modules", "vendor", "dist"}
        ]
        root = Path(directory)
        result.extend(
            root / name
            for name in filenames
            if name.endswith(".go") and not name.endswith("_test.go")
        )
    return sorted(result)


def registers_engine_plugin(source: str) -> bool:
    aliases = {
        match.group("alias") or "plugin" for match in ENGINE_PLUGIN_IMPORT.finditer(source)
    }
    return any(re.search(rf"\b{re.escape(alias)}\.Register\s*\(", source) for alias in aliases)


def registered_plugin_packages(repository: Path) -> tuple[dict[str, str], list[str]]:
    plugin_root = repository / "common/engine/plugins"
    sources_by_package: dict[str, list[str]] = {}
    for path in production_go_files(repository):
        try:
            relative = path.relative_to(plugin_root)
        except ValueError:
            continue
        if "builtin" in relative.parts:
            continue
        package = relative.parent.as_posix()
        sources_by_package.setdefault(package, []).append(path.read_text(encoding="utf-8"))

    sources_by_package = {
        package: sources
        for package, sources in sources_by_package.items()
        if any(registers_engine_plugin(source) for source in sources)
    }

    origins: dict[str, str] = {}
    errors: list[str] = []
    for package, sources in sorted(sources_by_package.items()):
        matches = {match.group("origin") for source in sources for match in ORIGIN_METHOD.finditer(source)}
        if len(matches) != 1:
            errors.append(
                f"common/engine/plugins/{package}: registered plugin must declare exactly one literal EngineOrigin(), got {sorted(matches)}"
            )
            continue
        origin = next(iter(matches))
        if origin not in AGGREGATE_PATHS:
            errors.append(
                f"common/engine/plugins/{package}: unsupported EngineOrigin {origin!r}"
            )
            continue
        origins[package] = origin
    if not sources_by_package:
        errors.append("no production Engine Plugin registration packages were found")
    return origins, errors


def aggregate_imports(repository: Path, relative_path: Path) -> tuple[list[str], list[str]]:
    path = repository / relative_path
    if not path.is_file():
        return [], [f"{relative_path}: aggregate entrypoint is missing"]
    return [match.group("package") for match in BLANK_IMPORT.finditer(path.read_text(encoding="utf-8"))], []


def validate_aggregates(repository: Path, plugins: dict[str, str]) -> list[str]:
    errors: list[str] = []
    imports_by_origin: dict[str, list[str]] = {}
    for origin, relative_path in AGGREGATE_PATHS.items():
        imports, import_errors = aggregate_imports(repository, relative_path)
        imports_by_origin[origin] = imports
        errors.extend(import_errors)
        for package, count in sorted(Counter(imports).items()):
            if count > 1:
                errors.append(f"{relative_path}: duplicate blank import for {package}")
            if package.startswith("builtin/"):
                errors.append(f"{relative_path}: must import concrete plugin packages, got {package}")
            elif package not in plugins:
                errors.append(
                    f"{relative_path}: {package} does not contain a production plugin.Register call"
                )
            elif plugins[package] != origin:
                errors.append(
                    f"{relative_path}: {package} declares EngineOrigin {plugins[package]!r}, not {origin!r}"
                )

    for package, origin in sorted(plugins.items()):
        owners = [name for name, imports in imports_by_origin.items() if package in imports]
        if owners != [origin]:
            errors.append(
                f"common/engine/plugins/{package}: expected exactly one {origin} aggregate import, got {owners}"
            )

    all_imports, all_errors = aggregate_imports(repository, ALL_AGGREGATE_PATH)
    errors.extend(all_errors)
    expected_all = ["builtin/extension", "builtin/general"]
    if sorted(all_imports) != expected_all:
        errors.append(
            f"{ALL_AGGREGATE_PATH}: must import only {expected_all}, got {sorted(all_imports)}"
        )
    return errors


def validate_upper_layer_blank_imports(repository: Path) -> list[str]:
    errors: list[str] = []
    plugin_root = repository / "common/engine/plugins"
    for path in production_go_files(repository):
        try:
            path.relative_to(plugin_root)
            continue
        except ValueError:
            pass
        relative = path.relative_to(repository)
        for match in BLANK_IMPORT.finditer(path.read_text(encoding="utf-8")):
            package = match.group("package")
            if not package.startswith("builtin/"):
                errors.append(
                    f"{relative}: upper-layer production code blank-imports concrete Engine Plugin {package}; import a builtin aggregate at the process entrypoint"
                )
    return errors


def validate_make_registration(repository: Path) -> list[str]:
    makefile_path = repository / "Makefile"
    if not makefile_path.is_file():
        return ["Makefile is missing"]
    makefile = makefile_path.read_text(encoding="utf-8")
    required = (
        "test-engine-plugin-registration:",
        "python3 scripts/ci/check-engine-plugin-registration_test.py",
        'python3 scripts/ci/check-engine-plugin-registration.py --repository "$(CURDIR)"',
        "$(MAKE) test-engine-plugin-registration",
    )
    return [f"Makefile registration is missing {fragment}" for fragment in required if fragment not in makefile]


def validate(repository: Path) -> list[str]:
    plugins, errors = registered_plugin_packages(repository)
    errors.extend(validate_aggregates(repository, plugins))
    errors.extend(validate_upper_layer_blank_imports(repository))
    errors.extend(validate_make_registration(repository))
    return errors


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, default=Path.cwd())
    return parser.parse_args()


def main() -> int:
    errors = validate(parse_args().repository.resolve())
    if errors:
        for error in errors:
            print(f"Engine Plugin registration check failed: {error}", file=sys.stderr)
        return 1
    print("Engine Plugin registration consistency check passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
