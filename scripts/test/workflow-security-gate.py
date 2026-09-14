#!/usr/bin/env python3
"""Run the same pinned release-workflow audit locally and in CI."""

from __future__ import annotations

import argparse
from pathlib import Path
import subprocess
import sys
import tempfile


ZIZMOR_VERSION = "1.28.0"
WORKFLOW = ".github/workflows/release-and-t2-gates.yml"


def audit(repository: Path) -> int:
    if not (repository / WORKFLOW).is_file():
        print(f"Workflow security audit: missing {WORKFLOW}", file=sys.stderr)
        return 1
    # Keep the tool out of project runtimes; always clean up on failure as well.
    with tempfile.TemporaryDirectory(prefix="addp-workflow-security-") as directory:
        environment = Path(directory) / "venv"
        commands = (
            [sys.executable, "-m", "venv", str(environment)],
            [
                str(environment / "bin/python"), "-m", "pip", "install",
                "--disable-pip-version-check", "--only-binary=:all:", "--no-deps",
                f"zizmor=={ZIZMOR_VERSION}",
            ],
            [
                str(environment / "bin/zizmor"),
                "--no-online-audits", "--persona=auditor", "--min-severity=medium",
                "--format=github", "--no-progress", "--strict-collection",
                "--no-config", "--no-ignores", WORKFLOW,
            ],
        )
        for command in commands:
            result = subprocess.run(command, cwd=repository, check=False)
            if result.returncode:
                return result.returncode
    return 0


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, required=True)
    args = parser.parse_args()
    try:
        return audit(args.repository.resolve())
    except OSError as error:
        print(f"Workflow security audit failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
