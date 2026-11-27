"""Verify current Gateway search authorization on an isolated MySQL platform."""
import json
import os
from pathlib import Path
import runpy
import sys
import time
from urllib.parse import urlencode
from uuid import uuid4


TABLES = ("hostedcursor_orders", "hostedcursor_shipments")
OPERATOR_ROLES = {
    "tenant.data_steward",
    "tenant.engine_lifecycle_operator",
    "tenant.engine_access_delegation_administrator",
}


def run(work, artifacts):
    directory = Path(__file__).resolve().parent
    client = runpy.run_path(str(directory / "manager-hybrid-search-online.py"))["GatewayClient"]
    registration = runpy.run_path(str(directory / "online-engine-registration.py"))
    exact_target = registration["ExactRecordGrants"].target
    authorization_facts = registration["read_exact_record_authorization_facts"]
    facts = json.loads((work / "tenant.json").read_text())
    reader_file = work / "manager-reader.token"
    origin = os.environ["ADDP_PUBLIC_ORIGIN"]
    reader = client(origin, reader_file.read_text(), 15)
    reader_file.unlink()
    login = client(origin, "", 15).request("POST", "/api/v1/system/login", (200,), {
        "username": "search-operator", "password": os.environ["ADDP_TEST_MYSQL_SEARCH_OPERATOR_PASSWORD"],
    }).payload
    assert login["next_action"] == "session_issued"
    operator = client(origin, login["session"]["access_token"], 15)
    operator_id = None

    for actor, roles in ((reader, {"tenant.data_steward"}), (operator, OPERATOR_ROLES)):
        identity = actor.request("GET", "/api/v1/system/auth/context", (200,)).payload
        assert identity["principal"]["type"] == "user"
        assert identity["context"]["type"] == "tenant" and int(identity["context"]["tenant_id"]) == facts["tenant_id"]
        assert {role["role_key"] for role in identity["authorization"]["role_assignments"]} == roles
        principal = int(identity["principal"]["id"])
        assert (principal == facts["principal_id"]) == (actor is reader)
        if actor is operator:
            operator_id = principal

    evidence = {"platform_provider": "mysql", "source_provider": "postgresql",
                "tenant_id": facts["tenant_id"], "reader_principal_id": facts["principal_id"],
                "operator_principal_id": operator_id}

    def record(**observations):
        evidence.update(observations)
        (artifacts / "manager-search-cursor.json").write_text(json.dumps(evidence, indent=2) + "\n")

    engine = operator.request("POST", "/api/v1/system/engines", (201,), {
        "name": "Hosted cursor source", "engine_type": "postgresql", "engine_origin": "general",
        "connection_info": {"host": "search-source", "port": 5432, "database": "search_fixture",
                            "user": "search_reader", "password": os.environ["SEARCH_SOURCE_READER_PASSWORD"],
                            "sslmode": "disable"},
    }).payload
    engine_id = int(engine["id"])
    assert engine_id > 0 and engine["engine_type"] == "postgresql"
    record(engine_id=engine_id)
    execution = operator.request("POST", "/api/v1/meta/scan/run/manual", (201,), {
        "engine_id": engine_id, "catalog_paths": ["public"], "scan_depth": "basic",
        "trigger_type": "manual", "force": True,
    }).payload["execution_id"]
    record(meta_execution_id=execution)
    deadline = time.monotonic() + 90
    while True:
        status = operator.request("GET", f"/api/v1/meta/executions/{execution}", (200,)).payload["status"]
        if status == "success":
            break
        if status not in {"pending", "running"} or time.monotonic() >= deadline:
            raise RuntimeError(f"Meta source scan {execution} ended at {status}")
        time.sleep(1)
    record(meta_scan_status=status)

    grants = {}
    endpoint = f"/api/v1/system/engines/{engine_id}/access_grants"
    documents = {}

    def revoke(doc):
        grant_id, confirmed = grants[doc]
        response = operator.request("POST", f"{endpoint}/{grant_id}/revoke", (200,) if confirmed else (200, 404),
                                    {"reason": "Hosted Manager exact search revocation"})
        if response.status == 200:
            receipt = response.payload
            assert receipt["request_id"] == grant_id and receipt["revoked_request_ids"] == [grant_id]
            assert int(receipt["revoked_by_principal_id"]) == operator_id and receipt["revoked_at"]
        del grants[doc]

    def search(page_size, cursor=None, expected=200):
        query = {"q": "hostedcursor", "engine_id": engine_id, "page_size": page_size}
        if cursor is not None:
            query["cursor"] = cursor
        try:
            response = reader.request("GET", "/api/v1/manager/search?" + urlencode(query), (expected,))
        except RuntimeError as error:
            raise RuntimeError(str(error).replace(cursor, "<cursor>") if cursor else str(error)) from None
        return response.payload if expected != 200 else response.payload["data"]

    def hits(page):
        ids = []
        for hit in page["results"]:
            document = hit["document_id"]
            assert document in documents and document not in ids, "unexpected or duplicate search document"
            assert hit["engine_id"] == engine_id and hit["data_item_type"] == "table"
            assert hit["full_name"] == f"public.{documents[document]}" and hit["match_methods"] == ["keyword"]
            ids.append(document)
        return ids

    try:
        for table in TABLES:
            target = exact_target(engine_id, "public", table, "table", namespace_term="schema")
            source = authorization_facts(operator, target)
            document = source["item_fingerprint"]
            documents[document] = table
            grant_id = str(uuid4())
            grants[document] = (grant_id, False)
            payload = {
                "request_id": grant_id, "catalog_path": target, "item_fingerprint": document,
                "expected_boundary_hash": source["boundary_hash"], "recipient_type": "user",
                "recipient_id": str(facts["principal_id"]), "action": "read", "expiry_mode": "until_revoked",
                "reason": "Hosted Manager exact search read",
            }
            receipt = operator.request("POST", endpoint, (201,), payload).payload
            grants[document] = (grant_id, True)
            assert all(receipt[key] == payload[key] for key in (
                "request_id", "catalog_path", "item_fingerprint", "recipient_type", "recipient_id", "action",
            )) and receipt["revocation"] is None
        record(grant_ids={documents[doc]: grant[0] for doc, grant in grants.items()})

        deadline = time.monotonic() + 30
        while True:
            page = search(4)
            found = hits(page)
            assert page["exhausted"] and not page.get("next_cursor")
            if set(found) == documents.keys():
                break
            if time.monotonic() >= deadline:
                raise RuntimeError("Manager index did not return the two scanned source documents")
            time.sleep(1)
        first_page = search(1)
        first = hits(first_page)
        cursor = first_page.get("next_cursor")
        assert len(first) == 1 and cursor and not first_page["exhausted"]
        second = hits(search(1, cursor))
        assert len(second) == 1 and set(first + second) == documents.keys()
        record(first_page=first, second_page=second, cursor_verified=True)

        withdrawn, retained = second[0], first[0]
        revoke(withdrawn)
        record(revoked_document=withdrawn, revoke_http_status=200)
        old = search(1, cursor, expected=409)
        assert old["error_code"] == "search_context_changed" and "data" not in old
        record(old_cursor_http_status=409, old_cursor_error_code=old["error_code"])
        fresh = search(4)
        assert hits(fresh) == [retained] and fresh["exhausted"] and not fresh.get("next_cursor")
        record(fresh_search_http_status=200, fresh_documents=[retained])
    finally:
        primary = sys.exc_info()[1]
        failed_cleanup = []
        for document in list(grants):
            try:
                revoke(document)
            except Exception as error:
                failed_cleanup.append(type(error).__name__)
        record(grant_cleanup="failed" if failed_cleanup else "passed")
        if failed_cleanup and primary is None:
            raise RuntimeError(f"failed to clean {len(failed_cleanup)} exact search grants")
    print("Manager keyword search: two documents, real next page, stale cursor 409, one visible document after revocation")


if __name__ == "__main__":
    run(Path(sys.argv[1]), Path(sys.argv[2]))
