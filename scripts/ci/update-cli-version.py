#!/usr/bin/env python3
"""一致地更新 ADDP CLI 的运行时和长期文档版本。"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path


VERSION_PATTERN = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")


class VersionUpdateError(RuntimeError):
    pass


def replace_exactly(text: str, old: str, new: str, path: Path) -> str:
    count = text.count(old)
    if count != 1:
        raise VersionUpdateError(
            f"{path}: expected exactly one occurrence of {old!r}, found {count}"
        )
    return text.replace(old, new)


def prepare_updates(repository: Path, target_version: str) -> dict[Path, str]:
    if not VERSION_PATTERN.fullmatch(target_version):
        raise VersionUpdateError(
            "target version must use the stable X.Y.Z format"
        )

    version_file = repository / "common-python/addp_common/__init__.py"
    version_text = version_file.read_text(encoding="utf-8")
    match = re.search(r'^__version__ = "([0-9]+\.[0-9]+\.[0-9]+)"$', version_text, re.MULTILINE)
    if match is None:
        raise VersionUpdateError(f"{version_file}: canonical __version__ is missing")
    current_version = match.group(1)
    if tuple(map(int, target_version.split("."))) <= tuple(map(int, current_version.split("."))):
        raise VersionUpdateError(
            f"target version {target_version} must be greater than current version {current_version}"
        )

    replacements = {
        version_file: [(f'__version__ = "{current_version}"', f'__version__ = "{target_version}"')],
        repository / "README.md": [
            (f"version-{current_version}-blue.svg", f"version-{target_version}-blue.svg")
        ],
        repository / "common-python/README.md": [
            (f"当前版本为 `{current_version}`", f"当前版本为 `{target_version}`"),
            (f"RELEASE=v{current_version}", f"RELEASE=v{target_version}"),
        ],
        repository / "common-python/common-python实施报告.md": [
            (f"当前版本为 `{current_version}`", f"当前版本为 `{target_version}`")
        ],
        repository / "scripts/README.md": [
            (f"**Version**: {current_version}", f"**Version**: {target_version}")
        ],
    }

    updates: dict[Path, str] = {}
    for path, pairs in replacements.items():
        text = path.read_text(encoding="utf-8")
        for old, new in pairs:
            text = replace_exactly(text, old, new, path)
        updates[path] = text
    return updates


def update_version(repository: Path, target_version: str) -> list[Path]:
    updates = prepare_updates(repository.resolve(), target_version)
    for path, text in updates.items():
        path.write_text(text, encoding="utf-8")
    return list(updates)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", type=Path, required=True)
    parser.add_argument("--version", required=True)
    args = parser.parse_args()
    try:
        updated = update_version(args.repository, args.version)
    except (OSError, VersionUpdateError) as error:
        print(f"CLI version update failed: {error}", file=sys.stderr)
        return 1
    for path in updated:
        print(path.relative_to(args.repository.resolve()))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
