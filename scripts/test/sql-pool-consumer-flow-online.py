#!/usr/bin/env python3
"""Isolated PostgreSQL/MySQL physical sessions for the real Console pool flow."""

import argparse
from importlib import import_module
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import time

SUPPORT = import_module("scripts.test.security-transfer-protection-online")
REGISTRATION = import_module("scripts.test.online-engine-registration")
ROOT = Path(__file__).resolve().parents[2]
NAME = "sql-pool-consumer-flow"
DATABASE = "pool_fixture"
TABLE = "pool_source"
READER = "pool_reader"
LABEL = "addp.online.sql-pool"
PROFILES = {"postgresql": ("postgres", 5432, "public", "schema"), "mysql": ("mysql", 3306, DATABASE, "database")}
PERMISSIONS = {
    "system.engine_catalog.read", "meta.catalog.read", "manager.data_item.read", "manager.content.read",
    "develop.task.read", "develop.task.execute", "develop.data_read.execute", "system.execution_authorization.create",
}


def require(condition, message):
    if not condition:
        raise SUPPORT.SuiteError(message)


def container(engine):
    return "addp-sql-pool-" + engine


def docker(*args, sql=None, check=True):
    result = subprocess.run(("docker", *args), input=sql, text=True, capture_output=True, timeout=240)
    if check and result.returncode:
        # Command output can contain seed SQL, native logs or credentials.
        raise SUPPORT.SuiteError(f"SQL pool fixture docker {args[0]} failed")
    return result


def sql(engine, statement, *, check=True):
    name = container(engine)
    if engine == "postgresql":
        args = ("sh", "-c", 'export PGPASSWORD="$POSTGRES_PASSWORD"; exec psql -XAt -v ON_ERROR_STOP=1 -h 127.0.0.1 -U fixture_admin -d pool_fixture')
    else:
        args = ("sh", "-c", 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql --batch --skip-column-names -h127.0.0.1 -uroot pool_fixture')
    return docker("exec", "-i", name, *args, sql=statement, check=check)


def private_write(path, payload):
    temporary = path.with_suffix(".tmp")
    with os.fdopen(os.open(temporary, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600), "w") as stream:
        stream.write(payload)
    temporary.replace(path)


def start(private, run_id):
    pins = (ROOT / "scripts/test/docker-compose.spark-federation-t2.yml").read_text()
    for engine, (service, port, namespace, _) in PROFILES.items():
        match = re.search(rf"(?m)^  {service}:\n    image: ([\w:.-]+@sha256:[0-9a-f]{{64}})$", pins)
        require(match, f"fixed {service} fixture image is missing")
        admin, password = secrets.token_hex(24), secrets.token_hex(24)
        settings = (
            f"POSTGRES_DB={DATABASE}\nPOSTGRES_USER=fixture_admin\nPOSTGRES_PASSWORD={admin}\n"
            if engine == "postgresql" else f"MYSQL_DATABASE={DATABASE}\nMYSQL_ROOT_PASSWORD={admin}\n"
        )
        directory = "/var/lib/postgresql/data" if engine == "postgresql" else "/var/lib/mysql"
        env_file = private / f"{engine}.env"
        private_write(env_file, settings)
        options = (
            "postgres", "-c", "log_statement=all", "-c", "log_line_prefix=pid=%p user=%u "
        ) if engine == "postgresql" else ()
        docker("run", "-d", "--name", container(engine), "--label", f"{LABEL}={run_id}",
               "--env-file", str(env_file), "--tmpfs", directory, "-p", f"127.0.0.1::{port}", match[1], *options)
        deadline = time.monotonic() + 150
        while sql(engine, "SELECT 1;", check=False).returncode != 0:
            require(time.monotonic() < deadline, f"{engine} fixture did not become ready")
            time.sleep(1)
        sample = f"pool-{engine}-sample"
        statement = (
            f"CREATE TABLE {TABLE} (id INT PRIMARY KEY, label VARCHAR(80) NOT NULL);"
            f"INSERT INTO {TABLE} VALUES (7, '{sample}');"
        )
        if engine == "postgresql":
            statement += (
                f"CREATE ROLE {READER} LOGIN PASSWORD '{password}';"
                f"GRANT SELECT ON {namespace}.{TABLE} TO {READER};"
            )
        else:
            statement += (
                f"CREATE USER '{READER}'@'%' IDENTIFIED BY '{password}';"
                f"GRANT SELECT ON {DATABASE}.{TABLE} TO '{READER}'@'%';"
                "SET GLOBAL log_output='TABLE'; SET GLOBAL general_log=ON;"
            )
        sql(engine, statement)
        mapping = docker("port", container(engine), f"{port}/tcp").stdout.strip()
        require(re.fullmatch(r"127\.0\.0\.1:[1-9][0-9]{0,4}", mapping), "fixture must expose only its loopback port")
        connection = {
            "host": "127.0.0.1", "port": int(mapping.split(":")[-1]), "database": DATABASE,
            "user": READER, "password": password, "pool_max_open_conns": 2, "pool_max_idle_conns": 1,
        }
        if engine == "postgresql":
            connection["sslmode"] = "disable"
        private_write(private / f"{engine}-engine.json", json.dumps({
            "name": f"Hosted SQL pool {engine}", "engine_type": engine, "engine_origin": "general",
            "description": "Disposable connection pool acceptance", "connection_info": connection,
        }))


def stop(artifacts, run_id):
    for engine in PROFILES:
        name = container(engine)
        owned = docker("ps", "-aq", "--filter", f"label={LABEL}={run_id}", "--filter", f"name=^/{name}$").stdout.strip()
        if owned:
            docker("rm", "-fv", owned)
    remaining = docker("ps", "-aq", "--filter", f"label={LABEL}={run_id}").stdout.strip()
    (artifacts / "sql-pool-fixture-cleanup.json").write_text(json.dumps({
        "run_id": run_id, "remaining_containers": len(remaining.splitlines()),
    }))
    require(not remaining, "owned SQL pool containers remain")


def client(key):
    return SUPPORT.GatewayClient(SUPPORT.required_environment("GATEWAY_URL"), SUPPORT.required_environment(key), 30)


def prepare(private, run_id):
    consumer = client("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN")
    preparer = client("ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN")
    tenant = SUPPORT.positive_int(SUPPORT.required_environment("ADDP_ONLINE_TEST_TENANT_ID"), "tenant")
    require(tenant > 1, "isolated tenant required")
    context = consumer.request("GET", "/api/v1/system/auth/context", (200,)).payload
    require({p for role in context["authorization"]["role_assignments"] for p in role["permissions"]} == PERMISSIONS,
            "unexpected pool consumer permissions")
    grants = []

    def pending(engine_id, request_id, target):
        grants.append({"engine_id": engine_id, "request_id": request_id, "target": target})
        private_write(private / "pool-grants.json", json.dumps(grants))

    cases = []
    for engine, (_, _, namespace, term) in PROFILES.items():
        descriptor = REGISTRATION.load_descriptor(private / f"{engine}-engine.json")
        engine_id = REGISTRATION.register_engine(SUPPORT.required_environment("GATEWAY_URL"),
                                                SUPPORT.required_environment("ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN"),
                                                descriptor)
        scan_id = SUPPORT.wait_for_scan(preparer, engine_id, time.monotonic() + 120)
        REGISTRATION.initialize_exact_record_read_grants(
            preparer, consumer, tenant, [(engine_id, term, namespace, TABLE)],
            scan_preparation=True, on_pending=pending,
        )
        item = SUPPORT.find_item(consumer, engine_id, f"{namespace}.{TABLE}", "table")
        cases.append({
            "engine_id": engine_id, "engine_type": engine, "name": descriptor["name"], "namespace": namespace,
            "sample": f"pool-{engine}-sample", "table": TABLE, "scan_execution_id": scan_id,
            "locator": SUPPORT.build_item_locator(engine_id, item),
        })
    private_write(private / "pool-expectations.json", json.dumps({
        "run_id": run_id, "tenant_id": str(tenant), "principal_id": str(context["principal"]["id"]),
        "permissions": sorted(PERMISSIONS), "cases": cases,
    }))


def cleanup(private, artifacts, run_id):
    preparer = client("ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN")
    consumer = client("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN")
    journal = json.loads((private / "pool-grants.json").read_text())
    revoked = []
    for entry in journal:
        endpoint = f"/api/v1/system/engines/{entry['engine_id']}/access_grants/{entry['request_id']}/revoke"
        preparer.request("POST", endpoint, (200, 404), {"reason": "Disposable SQL pool acceptance finished"})
        consumer.request("POST", "/api/v1/system/engine-access/read-checks/manager-preview",
                         (403,), {"targets": [entry["target"]]})
        revoked.append({"engine_id": entry["engine_id"], "request_id": entry["request_id"], "read_denied": True})
    (artifacts / "sql-pool-grant-cleanup.json").write_text(json.dumps({"run_id": run_id, "grants": revoked}))


def pg_statement_ids(log):
    entries = re.split(r"(?m)(?=^pid=)", log)
    table = r'\bFROM\s+(?:ONLY\s+)?(?:"?public"?\.)?"?pool_source"?(?![a-z_0-9])'
    return sorted({
        int(match.group(1)) for entry in entries
        if (match := re.match(r"^pid=(\d+) user=pool_reader LOG:\s+(?:statement:|execute [^:]+:)\s*(?:SELECT|WITH)\b", entry, re.I))
        and re.search(table, entry, re.I)
    })


def observe(engine, run_id):
    owned = docker("inspect", "--format", '{{index .Config.Labels "' + LABEL + '"}}', container(engine)).stdout.strip()
    require(owned == run_id, "SQL pool observer requires the owned fixture")
    if engine == "postgresql":
        result = docker("logs", container(engine))
        statements = pg_statement_ids(result.stdout + "\n" + result.stderr)
        active = sql(engine, f"SELECT pid FROM pg_stat_activity WHERE usename='{READER}';").stdout
    else:
        logged = sql(engine, f"""SELECT DISTINCT thread_id FROM mysql.general_log
            WHERE user_host LIKE '{READER}[%' AND command_type IN ('Query','Execute')
            AND LOWER(argument) REGEXP '^[[:space:]]*(select|with)[[:space:]]'
            AND LOWER(argument) REGEXP '[[:space:]]from[[:space:]]+(`?{DATABASE}`?[.])?`?{TABLE}`?([^a-z_0-9]|$)';""").stdout
        statements = sorted(int(value) for value in logged.split())
        active = sql(engine, f"SELECT ID FROM information_schema.PROCESSLIST WHERE USER='{READER}';").stdout
    return {"statement_ids": statements, "active_ids": sorted(int(value) for value in active.split())}


def validate_browser(evidence, expected):
    require(evidence.get("schema_version") == "addp.sql-pool-console/v1"
            and evidence.get("run_id") == expected["run_id"], "wrong Console evidence identity")
    require(evidence.get("consumer_principal_id") == expected["principal_id"], "wrong Console consumer")
    require(evidence.get("operator_principal_id") not in (None, "", expected["principal_id"]), "expected separate configuration operator")
    cases = evidence.get("cases", [])
    require([case["engine_id"] for case in cases] == [case["engine_id"] for case in expected["cases"]], "missing or reordered Console cases")
    executions = set()
    for case, sample in zip(cases, expected["cases"], strict=True):
        require(case["engine_type"] == sample["engine_type"],
                "wrong Console engine type")
        require(case["after_version"] > case["before_version"], "configuration version did not advance")
        require((case["max_open"], case["idle_before"], case["idle_after"]) == (2, 1, 0),
                "pool settings differ from the test contract")
        require(set(case["consumers"]) == {"manager", "develop"}, "missing one of the two consumers")
        old, fresh = set(), set()
        for consumer, proof in case["consumers"].items():
            require(proof["before_rows"] == proof["after_rows"] == 1, "expected one row before and after the pool change")
            require(proof["old_ids"] and proof["new_ids"] and not (set(proof["old_ids"]) & set(proof["new_ids"])),
                    "missing physical pool change")
            require(not ((set(proof["old_ids"]) | set(proof["new_ids"])) & set(proof["active_after"])),
                    "an old or new session remained open")
            require(not (old & set(proof["old_ids"]) or fresh & set(proof["new_ids"])),
                    "one physical connection was attributed to both consumers")
            old.update(proof["old_ids"])
            fresh.update(proof["new_ids"])
            if consumer == "develop":
                require(len(set(proof["execution_ids"])) == 2 and not (executions & set(proof["execution_ids"])),
                        "Develop execution IDs must be distinct")
                executions.update(proof["execution_ids"])
        require(not (old & fresh), "one consumer reused the other's old pool")


def process_identity():
    # PID plus Linux process start time prevents accepting a restarted process.
    identity = {}
    for name in ("system", "manager", "develop"):
        pid = int((ROOT / f".dev-pids/{name}.pid").read_text())
        fields = Path(f"/proc/{pid}/stat").read_text().rsplit(")", 1)[1].split()
        identity[name] = [pid, fields[19]]
    return identity


def run(private, artifacts):
    require(not any(os.environ.get(key) for key in ("ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN",
                                                    "ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN")),
            "preparation credentials must not reach the browser")
    report = artifacts / "sql-pool-console.json"
    report.unlink(missing_ok=True)
    source = private / "pool-expectations.json"
    before = process_identity()
    result = subprocess.run(
        ["npm", "exec", "--", "playwright", "test", "e2e/online/sql-pool-consumer-flow.spec.js",
         "--config=playwright.online.config.js"],
        cwd=ROOT / "console/frontend",
        env={**os.environ, "ADDP_ONLINE_POOL_EXPECTATIONS": str(source), "ADDP_ONLINE_POOL_REPORT": str(report)},
    )
    require(result.returncode == 0 and report.is_file(), "SQL pool Console flow failed or omitted evidence")
    validate_browser(json.loads(report.read_text()), json.loads(source.read_text()))
    require(process_identity() == before, "System, Manager or Develop restarted during the flow")
    (artifacts / "sql-pool-processes.json").write_text(json.dumps({"unchanged": True, "processes": before}))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("action", nargs="?", default="run", choices=("start", "stop", "prepare", "cleanup", "observe", "run"))
    parser.add_argument("--engine", choices=PROFILES)
    args = parser.parse_args()
    require(os.environ.get("GITHUB_ACTIONS") == "true" and os.environ.get("ADDP_ONLINE_HOSTED") == "1"
            and os.environ.get("ADDP_ONLINE_TEST") == "1" and os.environ.get("ONLINE_SUITE") == NAME,
            "requires the isolated SQL pool Hosted lifecycle")
    private = Path(SUPPORT.required_environment("ADDP_ONLINE_SECRET_DIR"))
    artifacts = Path(SUPPORT.required_environment("ADDP_ONLINE_ARTIFACT_DIR"))
    run_id = SUPPORT.required_environment("ADDP_ONLINE_TEST_RUN_ID")
    if args.action == "start":
        start(private, run_id)
    elif args.action == "stop":
        stop(artifacts, run_id)
    elif args.action == "prepare":
        prepare(private, run_id)
    elif args.action == "cleanup":
        cleanup(private, artifacts, run_id)
    elif args.action == "observe":
        require(args.engine is not None, "observer engine is required")
        print(json.dumps(observe(args.engine, run_id)))
    else:
        run(private, artifacts)


if __name__ == "__main__":
    try:
        main()
    except (SUPPORT.SuiteError, REGISTRATION.RegistrationError) as error:
        raise SystemExit(str(error)) from None
