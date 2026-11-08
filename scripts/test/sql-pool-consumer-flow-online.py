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
READERS = (READER, "pool_reader_next")
PHASES = ("address", "account", "password", "pool")
LABEL = "addp.online.sql-pool"
PROVISIONER_ROLE = "tenant.infrastructure_administrator"
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
        admin, password, next_password, rotated_password, invalidated_password = (secrets.token_hex(24) for _ in range(5))
        private_write(private / f"{engine}-credentials.json", json.dumps({
            "initial": password, "account": next_password, "rotated": rotated_password,
            "invalidated": invalidated_password,
        }))
        settings = (
            f"POSTGRES_DB={DATABASE}\nPOSTGRES_USER=fixture_admin\nPOSTGRES_PASSWORD={admin}\n"
            "POSTGRES_INITDB_ARGS=--auth-host=scram-sha-256 --auth-local=trust\n"
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
                f"CREATE ROLE {READERS[1]} LOGIN PASSWORD '{next_password}';"
                f"GRANT SELECT ON {namespace}.{TABLE} TO {READERS[1]};"
            )
        else:
            statement += (
                f"CREATE USER '{READER}'@'%' IDENTIFIED BY '{password}';"
                f"GRANT SELECT ON {DATABASE}.{TABLE} TO '{READER}'@'%';"
                f"CREATE USER '{READERS[1]}'@'%' IDENTIFIED BY '{next_password}';"
                f"GRANT SELECT ON {DATABASE}.{TABLE} TO '{READERS[1]}'@'%';"
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
        verify_native_passwords(engine, run_id, rotated_password, next_password, "preflight")


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
    provisioner = client("ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN")
    tenant = SUPPORT.positive_int(SUPPORT.required_environment("ADDP_ONLINE_TEST_TENANT_ID"), "tenant")
    require(tenant > 1, "isolated tenant required")
    context = consumer.request("GET", "/api/v1/system/auth/context", (200,)).payload
    require({p for role in context["authorization"]["role_assignments"] for p in role["permissions"]} == PERMISSIONS,
            "unexpected pool consumer permissions")
    registration_context = provisioner.request("GET", "/api/v1/system/auth/context", (200,)).payload
    principal = registration_context["principal"]
    scope = registration_context["context"]
    operator_id = principal["id"]
    roles = registration_context["authorization"]["role_assignments"]
    require(principal["type"] == "user" and isinstance(operator_id, str)
            and re.fullmatch(r"[1-9][0-9]*", operator_id) and operator_id != context["principal"]["id"]
            and scope["type"] == "tenant" and scope["tenant_id"] == str(tenant),
            "pool configuration requires the separate Tenant registration user")
    require([role["role_key"] for role in roles] == [PROVISIONER_ROLE]
            and {"system.engine.read", "system.engine.update"} <= set(roles[0]["permissions"]),
            "pool registration user requires the published infrastructure role")
    grants = []

    def pending(engine_id, request_id, target):
        grants.append({"engine_id": engine_id, "request_id": request_id, "target": target})
        private_write(private / "pool-grants.json", json.dumps(grants))

    cases = []
    for engine, (_, _, namespace, term) in PROFILES.items():
        descriptor = REGISTRATION.load_descriptor(private / f"{engine}-engine.json")
        engine_id = REGISTRATION.register_engine(provisioner.base_url, provisioner.token, descriptor)
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
            "source_identity": fixture_identity(engine, run_id),
            "read_target": next(entry["target"] for entry in grants if entry["engine_id"] == engine_id),
        })
    private_write(private / "pool-expectations.json", json.dumps({
        "run_id": run_id, "tenant_id": str(tenant), "principal_id": str(context["principal"]["id"]),
        "permissions": sorted(PERMISSIONS), "cases": cases, "operator_principal_id": operator_id,
        "operator_role": PROVISIONER_ROLE, "operator_permissions": sorted(set(roles[0]["permissions"])),
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


def pg_statement_ids(log, reader=READER):
    require(reader in READERS, "unknown SQL pool fixture reader")
    entries = re.split(r"(?m)(?=^pid=)", log)
    table = r'\bFROM\s+(?:ONLY\s+)?(?:"?public"?\.)?"?pool_source"?(?![a-z_0-9])'
    return sorted({
        int(match.group(1)) for entry in entries
        if (match := re.match(r"^pid=(\d+) user=" + re.escape(reader) + r" LOG:\s+(?:statement:|execute [^:]+:)\s*(?:SELECT|WITH)\b", entry, re.I))
        and re.search(table, entry, re.I)
    })


def fixture_identity(engine, run_id):
    info = docker("inspect", "--format", '{{.Id}} {{index .Config.Labels "' + LABEL + '"}}', container(engine)).stdout.split()
    require(len(info) == 2 and info[1] == run_id, "SQL pool operation requires the owned fixture")
    native = sql(engine, f"SELECT oid::text FROM pg_database WHERE datname='{DATABASE}';"
                 if engine == "postgresql" else "SELECT @@server_uuid;").stdout.strip()
    require(bool(native), "native SQL pool source identity missing")
    return {"container_id": info[0], "database": DATABASE, "native_id": native}


def verify_native_passwords(engine, run_id, rejected_password, accepted_password, stage):
    reader = READERS[1]
    if engine == "postgresql":
        command = f'IFS= read -r PGPASSWORD; export PGPASSWORD; exec psql -XAt -v ON_ERROR_STOP=1 -h127.0.0.1 -U {reader} -d {DATABASE} -c "SELECT 1"'
    else:
        command = f'IFS= read -r MYSQL_PWD; export MYSQL_PWD; exec mysql --batch --skip-column-names -h127.0.0.1 -u{reader} {DATABASE} -e "SELECT 1"'
    old = docker("exec", "-i", container(engine), "sh", "-c", command, sql=rejected_password + "\n", check=False)
    new = docker("exec", "-i", container(engine), "sh", "-c", command, sql=accepted_password + "\n", check=False)
    rejected = old.returncode != 0 and ("password authentication failed" in old.stderr if engine == "postgresql"
                                      else "ERROR 1045 (28000)" in old.stderr)
    diagnostic = {"engine": engine, "run_id": run_id, "stage": stage,
                  "rejected_attempt": {"exit_code": old.returncode, "returned_one": old.stdout.strip() == "1", "authentication_denied": rejected},
                  "accepted_attempt": {"exit_code": new.returncode, "returned_one": new.stdout.strip() == "1"}}
    if engine == "postgresql":
        diagnostic["host_authentication_rules"] = json.loads(sql(engine, """SELECT json_agg(json_build_object(
            'type',type,'address',address,'method',auth_method,'error',error) ORDER BY line_number)
            FROM pg_hba_file_rules;""").stdout)
    artifact = Path(SUPPORT.required_environment("ADDP_ONLINE_ARTIFACT_DIR")) / "sql-pool-auth-observations.jsonl"
    with artifact.open("a") as stream:
        stream.write(json.dumps(diagnostic) + "\n")
    require(rejected and new.returncode == 0 and new.stdout.strip() == "1", "native password authentication proof failed")


def rotate_password(engine, private, artifacts, run_id):
    before = fixture_identity(engine, run_id)
    marker = private / f"{engine}-password-rotated"
    require(not marker.exists(), "fixture password rotation cannot be repeated")
    credentials = json.loads((private / f"{engine}-credentials.json").read_text())
    require(all(re.fullmatch(r"[0-9a-f]{48}", value) for value in credentials.values()), "invalid fixture credentials")
    reader = READERS[1]
    password = credentials["rotated"]
    set_reader_password(engine, password)
    marker.write_text("rotated\n")
    verify_native_passwords(engine, run_id, credentials["account"], password, "rotated")
    require(fixture_identity(engine, run_id) == before, "password rotation changed the source")
    proof = {"engine": engine, "run_id": run_id, "reader": reader, "source_identity": before,
             "old_password_rejected": True, "new_password_accepted": True}
    with (artifacts / "sql-pool-password-probes.jsonl").open("a") as stream:
        stream.write(json.dumps(proof) + "\n")
    print(json.dumps(proof))


def set_reader_password(engine, password):
    require(bool(re.fullmatch(r"[0-9a-f]{48}", password)), "invalid fixture password")
    reader = READERS[1]
    statement = (f"SET log_statement='none'; ALTER ROLE {reader} PASSWORD '{password}';"
                 if engine == "postgresql" else f"SET sql_log_off=1; ALTER USER '{reader}'@'%' IDENTIFIED BY '{password}';")
    sql(engine, statement)


def credential_fault(engine, private, artifacts, run_id, restore):
    before = fixture_identity(engine, run_id)
    require((private / f"{engine}-password-rotated").is_file(), "credential fault requires the completed rotation")
    invalidated = private / f"{engine}-password-invalidated"
    restored = private / f"{engine}-password-restored"
    require(invalidated.exists() is restore and not restored.exists(), "invalid credential fault transition")
    credentials = json.loads((private / f"{engine}-credentials.json").read_text())
    require(all(re.fullmatch(r"[0-9a-f]{48}", value) for value in credentials.values()), "invalid fixture credentials")
    accepted = credentials["rotated" if restore else "invalidated"]
    rejected = credentials["invalidated" if restore else "rotated"]
    set_reader_password(engine, accepted)
    (restored if restore else invalidated).write_text("applied\n")
    stage = "restored" if restore else "invalidated"
    verify_native_passwords(engine, run_id, rejected, accepted, stage)
    require(fixture_identity(engine, run_id) == before, "credential fault changed the source")
    proof = {"engine": engine, "run_id": run_id, "stage": stage, "reader": READERS[1], "source_identity": before,
             "configured_password_accepted": restore, "alternative_password_accepted": not restore}
    with (artifacts / "sql-pool-recovery-probes.jsonl").open("a") as stream:
        stream.write(json.dumps(proof) + "\n")
    print(json.dumps(proof))


def mysql_log_contract():
    # general_log.argument is a BLOB; LOWER alone preserves its uppercase bytes.
    result = sql("mysql", """SELECT JSON_OBJECT(
        'argument_type', (SELECT DATA_TYPE FROM information_schema.COLUMNS
            WHERE TABLE_SCHEMA='mysql' AND TABLE_NAME='general_log' AND COLUMN_NAME='argument'),
        'binary_lower', HEX(LOWER(_binary 'SELECT')),
        'decoded_lower', HEX(LOWER(CONVERT(_binary 'SELECT' USING utf8mb4))));""")
    facts = json.loads(result.stdout)
    require(facts == {"argument_type": "mediumblob", "binary_lower": "53454C454354",
                      "decoded_lower": "73656C656374"}, "MySQL native log text contract changed")
    return facts


def observe(engine, run_id, reader=READER):
    require(reader in READERS, "unknown SQL pool fixture reader")
    source = fixture_identity(engine, run_id)
    if engine == "postgresql":
        result = docker("logs", container(engine))
        native_log = result.stdout + "\n" + result.stderr
        statements = pg_statement_ids(native_log, reader)
        reader_ids = sorted({int(match[1]) for match in re.finditer(r"(?m)^pid=(\d+) user=" + re.escape(reader) + r"(?=\s|$)", native_log)})
        active = sql(engine, f"SELECT pid FROM pg_stat_activity WHERE usename='{reader}';").stdout
    else:
        log_contract = mysql_log_contract()
        logged = sql(engine, f"""SELECT DISTINCT thread_id FROM mysql.general_log
            WHERE SUBSTRING_INDEX(user_host, '[', 1) = '{reader}' AND command_type IN ('Query','Execute')
            AND LOWER(CONVERT(argument USING utf8mb4)) REGEXP '^[[:space:]]*(select|with)[[:space:]]'
            AND LOWER(CONVERT(argument USING utf8mb4)) REGEXP '[[:space:]]from[[:space:]]+(`?{DATABASE}`?[.])?`?{TABLE}`?([^a-z_0-9]|$)';""").stdout
        statements = sorted(int(value) for value in logged.split())
        active = sql(engine, f"SELECT ID FROM information_schema.PROCESSLIST WHERE USER='{reader}';").stdout
        reader_log = sql(engine, f"""SELECT JSON_OBJECT('thread_id', thread_id, 'command_type', command_type,
            'reader', SUBSTRING_INDEX(user_host, '[', 1) = '{reader}',
            'select', LOWER(CONVERT(argument USING utf8mb4)) REGEXP '^[[:space:]]*(select|with)[[:space:]]',
            'sample', LOWER(CONVERT(argument USING utf8mb4)) REGEXP '[[:space:]]from[[:space:]]+(`?{DATABASE}`?[.])?`?{TABLE}`?([^a-z_0-9]|$)')
            FROM mysql.general_log WHERE command_type IN ('Query','Execute');""").stdout
        log_entries = [json.loads(line) for line in reader_log.splitlines()]
        reader_ids = sorted({entry["thread_id"] for entry in log_entries if entry["reader"]})
    observation = {"reader": reader, "source_identity": source, "statement_ids": statements, "active_ids": sorted(int(value) for value in active.split()),
                   "reader_ids": reader_ids}
    if engine == "mysql":
        observation["native_log_filters"] = log_entries
        observation["native_log_contract"] = log_contract
    artifact = Path(SUPPORT.required_environment("ADDP_ONLINE_ARTIFACT_DIR")) / "sql-pool-observations.jsonl"
    with artifact.open("a") as stream:
        stream.write(json.dumps({"run_id": run_id, "engine": engine, **observation}) + "\n")
    return observation


def validate_recovery(case, run_id, executions):
    proof = case["recovery"]
    require(set(proof) == {"source_identity", "invalidated", "offline", "manager_unavailable", "develop_unavailable",
                           "failed_read_ids", "restored", "online", "restored_read_status", "consumers"},
            "unexpected credential recovery evidence fields")
    source, last = case["source_identity"], case["changes"][-1]
    require(proof["source_identity"] == source, "credential recovery source changed")
    for stage, restored in (("invalidated", False), ("restored", True)):
        require(proof[stage] == {"engine": case["engine_type"], "run_id": run_id, "stage": stage,
                "reader": READERS[1], "source_identity": source, "configured_password_accepted": restored,
                "alternative_password_accepted": not restored}, "missing native credential recovery proof")
    for stage, online in (("offline", False), ("online", True)):
        require(proof[stage] == {"engine_version": last["after_version"],
                "connection_status": "online" if online else "offline", "probe_success": online},
                "credential observation changed configuration or missed availability")
    require(proof["manager_unavailable"] == {"notice_visible": True, "stale_rows_visible": False, "preview_requests": 0},
            "Manager must clear stale rows and suppress unavailable preview")
    require(proof["develop_unavailable"] == {"engine_id": case["engine_id"], "connection_status": "offline",
            "disabled": True, "submitted_executions": 0, "source_read_status": 200},
            "Develop unavailable selection or unchanged authorization unproven")
    require(proof["failed_read_ids"] == [] and proof["restored_read_status"] == 200,
            "credential failure read or unchanged authorization unproven")
    require(set(proof["consumers"]) == {"manager", "develop"}, "missing recovery consumer")
    used = {value for change in case["changes"] for consumer in change["consumers"].values()
            for field in ("old_ids", "new_ids") for value in consumer[field]}
    for consumer, reading in proof["consumers"].items():
        require(set(reading) == {"new_ids", "active_after", "rows"} |
                ({"before_ids"} if consumer == "manager" else {"previous_execution_id", "execution_id"}),
                "unexpected recovery consumer fields")
        require(reading["rows"] == 1 and reading["active_after"] == [], "restored read or idle lifecycle unproven")
        fields = ("before_ids", "new_ids") if consumer == "manager" else ("new_ids",)
        for field in fields:
            values = reading[field]
            require(isinstance(values, list) and bool(values) and all(type(value) is int and value > 0 for value in values)
                    and len(set(values)) == len(values) and not used.intersection(values), "invalid or reused recovery sessions")
            used.update(values)
        if consumer == "develop":
            require(reading["previous_execution_id"] == last["consumers"]["develop"]["execution_ids"][1]
                    and isinstance(reading["execution_id"], str) and bool(reading["execution_id"])
                    and reading["execution_id"] not in executions, "reused or discontinuous recovery execution")
            executions.add(reading["execution_id"])


def validate_browser(evidence, expected):
    require(set(evidence) == {"schema_version", "run_id", "consumer_principal_id", "operator_principal_id", "cases"},
            "unexpected Console report fields")
    require(evidence.get("schema_version") == "addp.sql-pool-console/v3"
            and evidence.get("run_id") == expected["run_id"], "wrong Console evidence identity")
    require(evidence.get("consumer_principal_id") == expected["principal_id"], "wrong Console consumer")
    require(evidence.get("operator_principal_id") == expected["operator_principal_id"]
            and expected["operator_principal_id"] != expected["principal_id"],
            "configuration operator must be the registration user")
    cases = evidence.get("cases", [])
    require([case["engine_id"] for case in cases] == [case["engine_id"] for case in expected["cases"]], "missing or reordered Console cases")
    executions = set()
    for case, sample in zip(cases, expected["cases"], strict=True):
        require(set(case) == {"engine_id", "engine_type", "source_identity", "changes", "recovery"}, "unexpected Console case fields")
        require(case["engine_type"] == sample["engine_type"] and case["source_identity"] == sample["source_identity"],
                "Console source identity changed")
        require(set(case["source_identity"]) == {"container_id", "database", "native_id"}, "unexpected source identity fields")
        changes = case["changes"]
        require([change["phase"] for change in changes] == list(PHASES), "missing or reordered configuration phases")
        version, previous, previous_execution = None, None, None
        for change in changes:
            phase = change["phase"]
            fields = {"address": ["host"], "account": ["password", "user"], "password": ["password"], "pool": ["pool_max_idle_conns"]}[phase]
            allowed = {"before_version", "after_version", "phase", "changed_fields", "same_engine_confirmed",
                       "host", "database", "reader", "max_open", "max_idle", "connection_online", "consumers"}
            require(set(change) == allowed | ({"password_proof"} if phase == "password" else set()), "unexpected configuration evidence fields")
            require(change["changed_fields"] == fields and change["same_engine_confirmed"] is (phase == "address"),
                    "configuration change differs from its phase")
            require(change["after_version"] > change["before_version"]
                    and (version is None or change["before_version"] == version), "configuration version did not advance continuously")
            version = change["after_version"]
            reader = READERS[0] if phase == "address" else READERS[1]
            old_reader = READERS[0] if phase in ("address", "account") else READERS[1]
            idle = 0 if phase == "pool" else 1
            require(change["host"] == "localhost" and change["database"] == DATABASE and change["reader"] == reader
                    and change["max_open"] == 2 and change["max_idle"] == idle and change["connection_online"] is True,
                    "source or pool settings differ from the confirmed contract")
            if phase == "password":
                require(change["password_proof"] == {
                    "engine": case["engine_type"], "run_id": expected["run_id"], "reader": READERS[1],
                    "source_identity": sample["source_identity"], "old_password_rejected": True, "new_password_accepted": True,
                }, "missing native password rotation proof")
            require(set(change["consumers"]) == {"manager", "develop"}, "missing one of the two consumers")
            old_ids, new_ids = set(), set()
            for consumer, proof in change["consumers"].items():
                require(set(proof) == {"old_ids", "new_ids", "old_active_after", "active_after", "old_reader", "reader",
                                       "before_rows", "after_rows"} | ({"execution_ids"} if consumer == "develop" else set()),
                        "unexpected physical session evidence fields")
                require(proof["before_rows"] == proof["after_rows"] == 1, "expected one row before and after the change")
                require(proof["old_reader"] == old_reader and proof["reader"] == reader, "physical connection belongs to wrong reader")
                for field in ("old_ids", "new_ids", "old_active_after", "active_after"):
                    values = proof[field]
                    require(isinstance(values, list) and all(type(value) is int and value > 0 for value in values)
                            and len(set(values)) == len(values), "invalid native session IDs")
                old, fresh = set(proof["old_ids"]), set(proof["new_ids"])
                require(old and fresh and not (old & fresh), "missing physical pool change")
                require(not (old & set(proof["old_active_after"])), "old physical connection remains open")
                require(old_reader != reader or proof["old_active_after"] == proof["active_after"],
                        "same reader has inconsistent active-session evidence")
                require(not (fresh & set(proof["active_after"])) if idle == 0 else fresh <= set(proof["active_after"]),
                        "new physical connection has wrong idle lifecycle")
                require(not (old_ids & old or new_ids & fresh), "one physical connection was attributed to both consumers")
                if previous is not None:
                    require(old == set(previous["consumers"][consumer]["new_ids"]), "physical session chain is discontinuous")
                old_ids.update(old)
                new_ids.update(fresh)
                if consumer == "develop":
                    pair = proof["execution_ids"]
                    require(len(pair) == 2 and pair[0] != pair[1] and pair[1] not in executions,
                            "Develop execution IDs must be distinct")
                    if previous_execution is None:
                        require(pair[0] not in executions, "Develop initial execution was reused")
                        executions.add(pair[0])
                    else:
                        require(pair[0] == previous_execution, "Develop execution chain is discontinuous")
                    executions.add(pair[1])
                    previous_execution = pair[1]
            require(not (old_ids & new_ids), "one consumer reused the other's old pool")
            previous = change
        validate_recovery(case, expected["run_id"], executions)


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
        env={**os.environ, "ADDP_ONLINE_POOL_EXPECTATIONS": str(source), "ADDP_ONLINE_POOL_REPORT": str(report),
             "ADDP_ONLINE_POOL_PRIVATE": str(private)},
    )
    require(result.returncode == 0 and report.is_file(), "SQL pool Console flow failed or omitted evidence")
    evidence = json.loads(report.read_text())
    validate_browser(evidence, json.loads(source.read_text()))
    require(process_identity() == before, "System, Manager or Develop restarted during the flow")
    (artifacts / "sql-pool-processes.json").write_text(json.dumps({"unchanged": True, "processes": before}))
    print(json.dumps(evidence, sort_keys=True))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("action", nargs="?", default="run", choices=("start", "stop", "prepare", "cleanup", "observe", "rotate-password", "invalidate-password", "restore-password", "run"))
    parser.add_argument("--engine", choices=PROFILES)
    parser.add_argument("--reader", choices=READERS, default=READER)
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
        print(json.dumps(observe(args.engine, run_id, args.reader)))
    elif args.action == "rotate-password":
        require(args.engine is not None, "password rotation engine is required")
        rotate_password(args.engine, private, artifacts, run_id)
    elif args.action in ("invalidate-password", "restore-password"):
        require(args.engine is not None, "credential fault engine is required")
        credential_fault(args.engine, private, artifacts, run_id, args.action == "restore-password")
    else:
        run(private, artifacts)


if __name__ == "__main__":
    try:
        main()
    except (SUPPORT.SuiteError, REGISTRATION.RegistrationError) as error:
        raise SystemExit(str(error)) from None
