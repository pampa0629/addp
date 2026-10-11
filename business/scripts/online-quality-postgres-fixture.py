"""Own the disposable Hosted Quality data source; never use local Infra data."""

from __future__ import annotations

import json
import os
import secrets
import subprocess
import sys
import time
from pathlib import Path

CONTAINER = "addp-quality-online-disposable"
LABEL = "com.addp.online-fixture=quality"


class FixtureError(RuntimeError):
    pass


def command(arguments, *, data=None, environment=None):
    try:
        result = subprocess.run(arguments, input=data, env=environment, text=True,
                                capture_output=True, timeout=90)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise FixtureError("source command could not finish") from error
    if result.returncode:
        # Docker/SQL stderr may contain connection facts or credentials.
        raise FixtureError("source command failed")
    return result.stdout.strip()


def existing():
    return command(["docker", "container", "ls", "-aq", "--filter", f"name=^/{CONTAINER}$"])


def require_owner():
    if not existing():
        raise FixtureError("owned source is missing")
    label = command(["docker", "inspect", "--format",
                     '{{ index .Config.Labels "com.addp.online-fixture" }}', CONTAINER])
    if label != "quality":
        raise FixtureError("container ownership mismatch")


def sql(statement):
    require_owner()
    return command(["docker", "exec", "-i", CONTAINER, "psql", "-U", "postgres",
                    "-d", "quality_fixture", "-v", "ON_ERROR_STOP=1", "-Atq"], data=statement)


def run(action, environment):
    if any(environment.get(key) != value for key, value in {
        "GITHUB_ACTIONS": "true", "RUNNER_OS": "Linux", "ADDP_ONLINE_HOSTED": "1",
        "ADDP_ONLINE_HOST": "1", "POSTGRES_DB": "addp_online",
    }.items()):
        raise FixtureError("disposable Hosted Linux Online is required")
    if action == "stop":
        if existing():
            require_owner()
            command(["docker", "rm", "-fv", CONTAINER])
        if existing():
            raise FixtureError("container remains after cleanup")
        return
    if action == "start":
        if existing():
            raise FixtureError("refusing an existing source container")
        directory = Path(environment["ADDP_ONLINE_SECRET_DIR"])
        descriptor = Path(environment["ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE"])
        if not directory.is_absolute() or not directory.is_dir() or descriptor.parent != directory:
            raise FixtureError("source descriptor must use the owned secret directory")
        password = secrets.token_hex(32)
        child_environment = dict(environment, POSTGRES_PASSWORD=password)
        command(["docker", "run", "-d", "--name", CONTAINER, "--label", LABEL,
                 "--mount", "type=tmpfs,destination=/var/lib/postgresql/data",
                 "-p", "127.0.0.1:55435:5432", "-e", "POSTGRES_PASSWORD", "-e",
                 "POSTGRES_DB=quality_fixture", "addp-postgres-pgvector:latest"], environment=child_environment)
        for attempt in range(60):
            try:
                command(["docker", "exec", CONTAINER, "pg_isready", "-U", "postgres", "-d", "quality_fixture"])
                break
            except FixtureError:
                if attempt == 59:
                    raise FixtureError("source PostgreSQL did not become ready")
                time.sleep(1)
        # No target table is created here: Model owns its definition and marker.
        sql(f"CREATE USER quality_owner WITH PASSWORD '{password}';\n"
            "GRANT CONNECT ON DATABASE quality_fixture TO quality_owner;\n"
            "GRANT USAGE, CREATE ON SCHEMA public TO quality_owner;\n")
        payload = {"name": "Hosted Quality PostgreSQL", "engine_type": "postgresql", "engine_origin": "general",
                   "description": "Disposable Model and Quality acceptance source", "connection_info": {
                       "host": "127.0.0.1", "port": 55435, "database": "quality_fixture",
                       "user": "quality_owner", "password": password}}
        with open(descriptor, "x", opener=lambda p, flags: os.open(p, flags, 0o600)) as output:
            json.dump(payload, output)
        return
    if action == "seed":
        sql("BEGIN;\nINSERT INTO public.quality_target_a (id) VALUES (1), (2);\n"
            "INSERT INTO public.quality_target_b (id) VALUES (3), (4);\nCOMMIT;\n")
        run("verify", environment)
        return
    if action == "verify":
        observed = sql("SELECT count(*)=2 AND array_agg(id ORDER BY id)=ARRAY[1,2]::bigint[] FROM public.quality_target_a;\n"
                       "SELECT count(*)=2 AND array_agg(id ORDER BY id)=ARRAY[3,4]::bigint[] FROM public.quality_target_b;\n")
        if observed.splitlines() != ["t", "t"]:
            raise FixtureError("source data changed or was not initialized exactly")
        return
    raise FixtureError("expected start, seed, verify or stop")


if __name__ == "__main__":
    try:
        if len(sys.argv) != 2:
            raise FixtureError("expected one action")
        run(sys.argv[1], os.environ)
        print("Quality source operation completed")
    except (FixtureError, KeyError, OSError):
        print("Quality source operation failed; inspect lifecycle evidence", file=sys.stderr)
        raise SystemExit(1)
