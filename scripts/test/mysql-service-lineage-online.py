"""Observe Service publication and deactivation on the release's disposable MySQL."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
from urllib.error import HTTPError
from urllib.parse import urlencode
from urllib.request import ProxyHandler, Request, build_opener
from uuid import uuid4


def run(work, container, artifacts):
    tenant = int(json.loads((work / "tenant.json").read_text())["tenant_id"])
    http = build_opener(ProxyHandler({}))
    origin = os.environ["ADDP_PUBLIC_ORIGIN"]
    evidence = {"tenant_id": tenant, "requests": []}

    def record():
        (artifacts / "service-lineage.json").write_text(json.dumps(evidence, indent=2) + "\n")

    def call(method, path, status, payload=None, token=None):
        request_id = str(uuid4())
        headers = {"Content-Type": "application/json", "Origin": origin, "X-Request-ID": request_id}
        if token:
            headers["Authorization"] = "Bearer " + token
        req = Request(origin + path, method=method, headers=headers,
                      data=json.dumps(payload).encode() if payload is not None else None)
        try:
            response = http.open(req, timeout=15)
        except HTTPError as err:
            response = err
        with response:
            evidence["requests"].append({"method": method, "path": path, "status": response.status,
                                         "request_id": response.headers.get("X-Request-ID", request_id)})
            record()
            if response.status != status:
                raise RuntimeError(f"{method} {path}: HTTP {response.status}, expected {status}, request_id={request_id}")
            return json.load(response)

    def sql(query):
        command = ["docker", "exec", "-i", "-e", "MYSQL_PWD", container, "mysql", "--protocol=tcp",
                   "-h127.0.0.1", "-u" + os.environ["ADDP_DATABASE_RUNTIME_USER"], "-NBr"]
        result = subprocess.run(command, input=query, text=True, stdout=subprocess.PIPE, timeout=15,
                                env=dict(os.environ, MYSQL_PWD=os.environ["ADDP_DATABASE_RUNTIME_PASSWORD"]), check=True)
        return result.stdout.strip()

    # Seed only the pre-existing source catalog. All service and lineage records
    # must originate from the Gateway request and the Service principal's delivery.
    source = sql(f"""
        START TRANSACTION;
        INSERT INTO `system`.engines(tenant_id,name,engine_type,connection_info,identity_key)
        VALUES({tenant},'Lineage source','mysql','{{"host":"lineage-source"}}','{{"host":"lineage-source"}}');
        SET @engine=LAST_INSERT_ID();
        INSERT INTO meta.meta_node(tenant_id,engine_id,node_type,name,depth)
        VALUES({tenant},@engine,'schema','orders',0);
        INSERT INTO meta.meta_item(tenant_id,engine_id,node_id,item_type,name,full_name,fingerprint,attributes,scanned_at)
        VALUES({tenant},@engine,LAST_INSERT_ID(),'table','entries','orders.entries',SHA2(CONCAT(@engine,':orders.entries'),256),
          '{{"storage":{{"schema_name":"orders"}},"type_info":{{"table":{{"primary_key":["id"],"fields":[{{"name":"id","type":"int","nullable":false,"primary_key":true}}]}}}}}}', UTC_TIMESTAMP(6));
        SELECT @engine,LAST_INSERT_ID();
        COMMIT;""")
    engine, item = map(int, source.split())
    login = call("POST", "/api/v1/system/login", 200, {"username": "lineage-publisher",
                 "password": os.environ["ADDP_TEST_LINEAGE_PUBLISHER_PASSWORD"]})
    assert login["next_action"] == "session_issued"
    publisher = login["session"]["access_token"]
    identity = call("GET", "/api/v1/system/auth/context", 200, token=publisher)
    assert int(identity["context"]["tenant_id"]) == tenant
    roles = identity["authorization"]["role_assignments"]
    assert [r["role_key"] for r in roles] == ["tenant.service_publisher"]
    assert {"service.definition.create", "service.definition.update"} <= set(roles[0]["permissions"])
    owner = "/api/v1/service/query"
    created = call("POST", owner, 201, {"service_name": "lineage_entries", "title": "Lineage entries",
                   "config_type": "table", "data_config": {"locator": f"addp://engine/{engine}/path/orders/entries?type=table&item_id={item}"}}, publisher)
    service = int(created["id"])
    snapshot = created["data_config"]["source_snapshot"]
    revision = snapshot["dependency_hash"]
    assert re.fullmatch(r"[0-9a-f]{64}", revision)
    assert created["status"] == "active" and int(created["tenant_id"]) == tenant
    assert int(snapshot["source"]["item_id"]) == item
    key = f"tenant_id={tenant} AND service_id={service}"
    # Read the observation and the projection in one statement. Fail on extra
    # revisions or edges; the inactive phase must close this exact projection.
    statement = f"""
        SELECT d.id, d.status, d.closed_at IS NOT NULL, o.id, d.service_updated_at=t.service_updated_at
        FROM meta.lineage_service_dependencies d
        JOIN meta.lineage_service_publication_times t USING (tenant_id,service_id)
        JOIN meta.lineage_observations o USING (tenant_id,service_id,source_item_id,published_revision)
        WHERE d.tenant_id={tenant} AND d.service_id={service} AND d.source_item_id={item}
          AND d.published_revision='{revision}' AND d.dependency_hash='{revision}' AND d.service_name='Lineage entries'
          AND o.relation_kind='serve' AND o.producer_module='service' AND o.capture_method='declared'
          AND (SELECT COUNT(*) FROM meta.lineage_service_dependencies WHERE {key})=1
          AND (SELECT COUNT(*) FROM meta.lineage_observations WHERE {key})=1"""
    projected = sql(statement).split()
    if len(projected) != 5:
        raise RuntimeError(f"Missing or ambiguous Meta publication for service {service}, item {item}, revision {revision}")
    dep, status, closed, obs, latest = projected
    assert (status, closed, latest) == ("active", "0", "1")
    reader_file = work / "lineage-reader.token"
    reader = reader_file.read_text()
    reader_file.unlink()
    reader_identity = call("GET", "/api/v1/system/auth/context", 200, token=reader)
    assert int(reader_identity["context"]["tenant_id"]) == tenant
    assert any("meta.lineage.read" in role["permissions"] for role in reader_identity["authorization"]["role_assignments"])
    graph = "/api/v1/meta/lineage/graph?" + urlencode({"subject_kind": "published_service",
                                                       "service_id": service, "revision": revision})
    call("GET", graph, 403, token=reader)
    offline = call("PUT", f"{owner}/{service}", 200, {"status": "inactive", "version": created["version"]}, publisher)
    assert offline["status"] == "inactive" and offline["version"] > created["version"]
    current = call("GET", f"{owner}/{service}", 200, token=publisher)
    assert current["status"] == "inactive" and current["version"] == offline["version"]
    # Status changes are delivered by the production minute replay.
    deadline = time.monotonic() + 100
    while True:
        row = sql(statement).split()
        if row == [dep, "closed", "1", obs, "1"]:
            break
        if time.monotonic() >= deadline:
            raise RuntimeError(f"Meta did not close the published dependency: {row}")
        time.sleep(2)
    evidence.update(service_id=service, source_item_id=item, dependency_hash=revision, dependency_id=int(dep),
                    observation_id=int(obs), published_version=created["version"], inactive_version=offline["version"],
                    dependency_states=["active", "closed"], retained_observations=1)
    record()
    print(f"Service {service}, item {item}, revision {revision}: dependency {dep} active -> closed; observation {obs} retained")


if __name__ == "__main__":
    run(Path(sys.argv[1]), sys.argv[2], Path(sys.argv[3]))
