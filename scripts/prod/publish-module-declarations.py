#!/usr/bin/env python3
"""Prepare per-module deployment credentials and publish the selected artifacts."""

import argparse
import json
import os
from pathlib import Path
import secrets
import subprocess
import tempfile


def publisher_secret(directory, module):
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    target = directory / module
    if not target.exists():
        with tempfile.NamedTemporaryFile(mode="w", dir=directory, delete=False) as temporary:
            temporary.write(secrets.token_hex(32))
        try:
            os.link(temporary.name, target)
        except FileExistsError:
            pass
        finally:
            os.unlink(temporary.name)
    return target.read_text()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    actions = parser.add_mutually_exclusive_group()
    actions.add_argument("--prepare", action="store_true", help="prepare deployment-side credentials before System starts")
    actions.add_argument("--provision", action="store_true", help="install publisher credentials using the System database administrator context")
    parser.add_argument("--compose-file", action="append", default=[])
    parser.add_argument("--artifact-dir", type=Path, default=Path.cwd())
    parser.add_argument("--credential-dir", type=Path, default=(Path(os.environ["ADDP_ONLINE_SECRET_DIR"]) / "publishers"
                        if "ADDP_ONLINE_SECRET_DIR" in os.environ else Path(".dev-state/module-publishers")))
    parser.add_argument("modules", nargs="+")
    args = parser.parse_args()
    compose = ["docker", "compose"]
    for filename in args.compose_file:
        compose.extend(["-f", filename])
    publish_command = None
    if args.compose_file and not (args.prepare or args.provision):
        system = subprocess.check_output([*compose, "ps", "-q", "system-backend"], text=True).strip()
        image = subprocess.check_output(["docker", "inspect", "-f", "{{.Image}}", system], text=True).strip()
        system_url = subprocess.check_output([*compose, "exec", "-T", "system-backend", "printenv", "SYSTEM_URL"], text=True).strip()
        publish_command = ["docker", "run", "--rm", "-i", "--network", "container:" + system,
                           "-e", "SYSTEM_URL=" + system_url, "--entrypoint", "/app/server", image]
    for module in args.modules:
        if not module.isascii() or not module.isalpha() or not module.islower():
            parser.error("invalid module name")
        if args.prepare:
            publisher_secret(args.credential_dir, module)
            continue
        if args.compose_file:
            service = "gateway" if module == "gateway" else module + "-backend"
            raw = None if args.provision else subprocess.check_output([*compose, "run", "--rm", "--no-deps", "-T",
                                           "--entrypoint", "cat", service, "/app/module-declaration.json"])
            command = publish_command or [*compose, "run", "--rm", "--no-deps", "-T",
                       "--entrypoint", "/app/server", "system-backend"]
        else:
            raw = None if args.provision else (args.artifact_dir / module / "module-declaration.json").read_bytes()
            command = [str(Path(".dev-bins/addp-system").resolve())]
        if args.provision:
            action = "provision-module-publisher"
            payload = {"secret": publisher_secret(args.credential_dir, module)}
        else:
            action = "publish-module-declaration"
            declaration = json.loads(raw)
            if declaration["module_name"] != module:
                parser.error(f"artifact module mismatch for {module}")
            payload = {"secret": (args.credential_dir / module).read_text(), "declaration": declaration}
        subprocess.run([*command, action, module], input=json.dumps(payload).encode(), check=True)


if __name__ == "__main__":
    main()
