#!/usr/bin/env python3
"""Guard production startup against bypassing the platform database binding."""

import re
import subprocess
from pathlib import Path


def main() -> int:
    root = Path(__file__).resolve().parents[2]
    compose = (root / "docker-compose.yml").read_text()
    consumers = 0
    errors = []
    for name, service in re.findall(
        r"(?ms)^  ([a-z0-9-]+):[^\n]*\n(.*?)(?=^  [a-z0-9-]+:|^\w|\Z)", compose
    ):
        if "POSTGRES_HOST=postgres" not in service:
            continue
        consumers += 1
        for setting in (
            "ADDP_INFRA_DATABASE_PROVIDER=${ADDP_INFRA_DATABASE_PROVIDER:-postgres}",
            "TZ=${TZ:-Asia/Shanghai}",
        ):
            if service.count(f"      - {setting}\n") != 1:
                errors.append(f"{name}: expected one platform binding for {setting}")
    if not consumers:
        errors.append("production Compose has no discovered platform database consumers")

    tracked = subprocess.check_output(["git", "ls-files", "-z", "*.go"], cwd=root)
    for filename in tracked.decode().split("\0"):
        path = Path(filename)
        if not filename or path.name.endswith("_test.go"):
            continue
        parts = path.parts
        startup = "cmd" in parts or path.name in {"database.go", "router.go"}
        if not startup or parts[0] in {"common", "scripts"} or "online-test-fixture" in parts:
            continue
        if '"gorm.io/driver/postgres"' in (root / path).read_text():
            errors.append(f"{filename}: use Common Infra to open the platform database")

    if errors:
        print("\n".join(errors))
        return 1
    print(f"Platform database startup check passed ({consumers} direct Compose bindings; workers inherit their shared definition).")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
