#!/usr/bin/env python3
"""Accept real Manager image embedding and hybrid retrieval through Gateway."""

from __future__ import annotations

import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Iterable, Mapping


TERMINAL_STATUSES = {"success", "failed", "timeout", "cancelled"}
REQUIRED_PERMISSIONS = {
    "manager.derived_artifact.create",
    "manager.derived_artifact.delete",
    "manager.derived_artifact.read",
    "manager.search.execute",
    "meta.catalog.read",
    "meta.scan_task.execute",
    "meta.scan_task.read",
}
FORBIDDEN_ADMIN_ROLES = {
    "platform.audit_administrator",
    "platform.security_administrator",
    "platform.system_administrator",
    "tenant.administrator",
}
SEMANTIC_QUERY = "黑色手枪和游戏屏幕"


class SuiteError(RuntimeError):
    pass


@dataclass
class Response:
    status: int
    payload: Any
    headers: Mapping[str, str]


class GatewayClient:
    def __init__(self, base_url: str, token: str, timeout: float) -> None:
        parsed = urllib.parse.urlsplit(base_url)
        if parsed.scheme not in {"http", "https"} or not parsed.netloc:
            raise SuiteError("GATEWAY_URL must be an absolute HTTP(S) URL")
        self.base_url = base_url.rstrip("/")
        self.token = token
        self.timeout = timeout

    def request(
        self,
        method: str,
        path: str,
        expected: Iterable[int],
        body: dict[str, object] | None = None,
    ) -> Response:
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(
            self.base_url + path,
            data=data,
            method=method,
            headers={
                "Accept": "application/json",
                "Content-Type": "application/json",
                "Authorization": f"Bearer {self.token}",
            },
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                status = response.status
                raw = response.read()
                headers = dict(response.headers.items())
        except urllib.error.HTTPError as error:
            status = error.code
            raw = error.read()
            headers = dict(error.headers.items()) if error.headers else {}
        except (urllib.error.URLError, TimeoutError) as error:
            raise SuiteError(f"{method} {path} transport failed: {error}") from error
        payload: Any = {}
        if raw:
            try:
                payload = json.loads(raw)
            except json.JSONDecodeError as error:
                raise SuiteError(f"{method} {path} returned invalid JSON") from error
        if status not in set(expected):
            code = payload.get("error_code", "unknown") if isinstance(payload, dict) else "unknown"
            raise SuiteError(f"{method} {path} returned HTTP {status} ({code})")
        return Response(status=status, payload=payload, headers=headers)


def _object(value: Any, resource: str) -> dict[str, object]:
    if not isinstance(value, dict):
        raise SuiteError(f"{resource} must be an object")
    return value


def _array(value: Any, resource: str) -> list[object]:
    if not isinstance(value, list):
        raise SuiteError(f"{resource} must be an array")
    return value


def positive_int(value: object, field: str) -> int:
    if isinstance(value, bool):
        raise SuiteError(f"{field} must be a positive integer")
    try:
        parsed = int(value)  # type: ignore[arg-type]
    except (TypeError, ValueError) as error:
        raise SuiteError(f"{field} must be a positive integer") from error
    if parsed <= 0 or str(parsed) != str(value):
        raise SuiteError(f"{field} must be a canonical positive integer")
    return parsed


def non_negative_int(value: object, field: str) -> int:
    if isinstance(value, bool):
        raise SuiteError(f"{field} must be a non-negative integer")
    try:
        parsed = int(value)  # type: ignore[arg-type]
    except (TypeError, ValueError) as error:
        raise SuiteError(f"{field} must be a non-negative integer") from error
    if parsed < 0 or str(parsed) != str(value):
        raise SuiteError(f"{field} must be a canonical non-negative integer")
    return parsed


def validate_user_identity(client: GatewayClient, tenant_id: int) -> dict[str, object]:
    context = _object(
        client.request("GET", "/api/v1/system/auth/context", (200,)).payload,
        "AuthContext",
    )
    principal = _object(context.get("principal"), "AuthContext principal")
    tenant = _object(context.get("context"), "AuthContext context")
    token = _object(context.get("token"), "AuthContext token")
    authorization = _object(context.get("authorization"), "AuthContext authorization")
    if principal.get("type") != "user":
        raise SuiteError("Online Manager hybrid-search token must belong to a User")
    principal_id = positive_int(principal.get("id"), "AuthContext principal.id")
    if tenant.get("type") != "tenant" or tenant.get("tenant_id") != str(tenant_id):
        raise SuiteError("Online Manager hybrid-search token must use the configured Tenant Context")
    if token.get("type") not in {"first_party_access_token", "oauth_access_token"}:
        raise SuiteError("Online Manager hybrid-search token must be a User Access Token")
    roles: set[str] = set()
    permissions: set[str] = set()
    for assignment in _array(authorization.get("role_assignments"), "AuthContext role_assignments"):
        item = _object(assignment, "AuthContext role assignment")
        role = item.get("role_key")
        granted = item.get("permissions")
        if not isinstance(role, str) or not isinstance(granted, list) or not all(isinstance(key, str) for key in granted):
            raise SuiteError("AuthContext role assignment is incomplete")
        roles.add(role)
        permissions.update(granted)
    forbidden = roles & FORBIDDEN_ADMIN_ROLES
    if forbidden:
        raise SuiteError("Online Manager hybrid-search token must not use administrator roles: " + ", ".join(sorted(forbidden)))
    missing = REQUIRED_PERMISSIONS - permissions
    if missing:
        raise SuiteError("Online Manager hybrid-search token is missing required permissions: " + ", ".join(sorted(missing)))
    return {
        "principal_id": str(principal_id),
        "principal_type": "user",
        "tenant_id": str(tenant_id),
        "roles": sorted(roles),
        "permissions_verified": sorted(REQUIRED_PERMISSIONS),
    }


def wait_for_meta_scan(client: GatewayClient, engine_id: int, deadline: float) -> str:
    run = _object(
        client.request(
            "POST",
            "/api/v1/meta/scan/run/manual",
            (201,),
            {"engine_id": engine_id, "scan_depth": "deep", "trigger_type": "manual", "force": True},
        ).payload,
        "Meta scan execution",
    )
    execution_id = run.get("execution_id")
    if not isinstance(execution_id, str) or not execution_id:
        raise SuiteError("Meta scan execution_id is missing")
    while time.monotonic() < deadline:
        execution = _object(
            client.request("GET", f"/api/v1/meta/executions/{urllib.parse.quote(execution_id)}", (200,)).payload,
            "Meta execution",
        )
        status = execution.get("status")
        if status == "success":
            return execution_id
        if status in TERMINAL_STATUSES:
            raise SuiteError(f"Meta scan ended with status {status}")
        time.sleep(1)
    raise SuiteError("Meta scan did not finish before the convergence timeout")


def find_fixture_item(client: GatewayClient, engine_id: int, full_name: str) -> dict[str, object]:
    items = _array(
        client.request("GET", f"/api/v1/meta/engines/{engine_id}/items", (200,)).payload,
        "Meta engine items",
    )
    matches = [item for item in items if isinstance(item, dict) and item.get("full_name") == full_name]
    if len(matches) != 1:
        raise SuiteError(f"Meta must return exactly one hybrid-search fixture {full_name}")
    item = matches[0]
    if item.get("item_type") not in {"object", "file"}:
        raise SuiteError("hybrid-search fixture must be an object or file DataItem")
    fingerprint = item.get("fingerprint")
    if not isinstance(fingerprint, str) or not fingerprint:
        raise SuiteError("hybrid-search fixture fingerprint is missing")
    positive_int(item.get("id"), "hybrid-search fixture id")
    return item


def build_item_locator(engine_id: int, item: Mapping[str, object]) -> str:
    item_id = positive_int(item.get("id"), "fixture item id")
    full_name = item.get("full_name")
    if not isinstance(full_name, str) or not full_name.strip():
        raise SuiteError("fixture full_name is missing")
    path = urllib.parse.quote(full_name.strip("/"), safe="/")
    return f"addp://engine/{engine_id}/path/{path}?type=object&item_id={item_id}"


def list_item_embeddings(client: GatewayClient, item_id: int) -> list[dict[str, object]]:
    query = urllib.parse.urlencode({"item_id": item_id, "page": 1, "page_size": 20})
    response = _object(
        client.request("GET", f"/api/v1/manager/embeddings?{query}", (200,)).payload,
        "Manager embedding list",
    )
    rows = [_object(value, "Manager embedding") for value in _array(response.get("data"), "Manager embedding list data")]
    total = non_negative_int(response.get("total"), "Manager embedding list total")
    if total != len(rows):
        raise SuiteError("Manager embedding list total does not match its rows")
    return rows


def wait_for_manager_execution(client: GatewayClient, execution_id: str, deadline: float) -> dict[str, object]:
    while time.monotonic() < deadline:
        execution = _object(
            client.request("GET", f"/api/v1/manager/executions/{urllib.parse.quote(execution_id)}", (200,)).payload,
            "Manager embedding execution",
        )
        status = execution.get("status")
        if status == "success":
            return execution
        if status in TERMINAL_STATUSES:
            raise SuiteError(f"Manager embedding execution ended with status {status}: {execution.get('error_details')!r}")
        time.sleep(1)
    raise SuiteError("Manager embedding execution did not finish before the convergence timeout")


def search(client: GatewayClient, engine_id: int, query_text: str, page_size: int) -> dict[str, object]:
    query = urllib.parse.urlencode({"q": query_text, "engine_id": engine_id, "page": 1, "page_size": page_size})
    response = _object(client.request("GET", f"/api/v1/manager/search?{query}", (200,)).payload, "Manager search response")
    if "vector_hits" in response or "vector_hits" in json.dumps(response, ensure_ascii=False):
        raise SuiteError("Manager search response must not expose vector_hits")
    return _object(response.get("data"), "Manager search data")


def matching_hits(result: Mapping[str, object], document_id: str) -> list[dict[str, object]]:
    hits = [_object(value, "Manager search hit") for value in _array(result.get("results"), "Manager search results")]
    ids = [value.get("document_id") for value in hits]
    if len(ids) != len(set(ids)):
        raise SuiteError("Manager search results contain duplicate document_id values")
    return [value for value in hits if value.get("document_id") == document_id]


def assert_match(hit: Mapping[str, object], expected_methods: list[str], locator: str) -> None:
    methods = _array(hit.get("match_methods"), "Manager search match_methods")
    if methods != expected_methods:
        raise SuiteError(f"Manager search match_methods must be {expected_methods}, got {methods}")
    if hit.get("locator") != locator:
        raise SuiteError("Manager search hit locator does not point to the source DataItem")
    score = hit.get("score")
    if not isinstance(score, (int, float)) or isinstance(score, bool) or not 0 <= float(score) <= 1:
        raise SuiteError("Manager search score must be normalized to [0, 1]")


def run_scenario(environment: Mapping[str, str]) -> dict[str, object]:
    tenant_id = positive_int(environment["ADDP_ONLINE_TEST_TENANT_ID"], "ADDP_ONLINE_TEST_TENANT_ID")
    engine_id = positive_int(environment["ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID"], "ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID")
    try:
        expected_profile_id = str(uuid.UUID(environment["ADDP_ONLINE_MANAGER_EMBEDDING_MODEL_PROFILE_ID"]))
    except (ValueError, AttributeError) as error:
        raise SuiteError("ADDP_ONLINE_MANAGER_EMBEDDING_MODEL_PROFILE_ID must be a UUID") from error
    timeout = float(environment.get("ADDP_ONLINE_MANAGER_HYBRID_SEARCH_CONVERGENCE_TIMEOUT_SECONDS", "180"))
    if timeout <= 0:
        raise SuiteError("ADDP_ONLINE_MANAGER_HYBRID_SEARCH_CONVERGENCE_TIMEOUT_SECONDS must be positive")
    client = GatewayClient(environment["GATEWAY_URL"], environment["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"], min(timeout, 30))
    identity = validate_user_identity(client, tenant_id)
    bucket = environment["ADDP_ONLINE_MANAGER_MINIO_BUCKET"].strip("/")
    image_object = environment["ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT"].strip("/")
    image_full_name = f"{bucket}/{image_object}"
    deadline = time.monotonic() + timeout
    scan_execution_id = wait_for_meta_scan(client, engine_id, deadline)
    item = find_fixture_item(client, engine_id, image_full_name)
    item_id = positive_int(item.get("id"), "hybrid-search fixture id")
    fingerprint = str(item["fingerprint"])
    locator = build_item_locator(engine_id, item)
    if list_item_embeddings(client, item_id):
        raise SuiteError("a stale embedding exists for the dedicated hybrid-search fixture")

    embedding_id: int | None = None
    execution_id = ""
    cleanup = {"embedding_deleted": False, "residual_resources": -1}
    try:
        created = _object(
            client.request(
                "POST",
                "/api/v1/manager/embedding_executions",
                (200,),
                {
                    "scope": "item",
                    "target": {
                        "engine_id": engine_id,
                        "item_id": item_id,
                        "item_fingerprint": fingerprint,
                        "locator": locator,
                    },
                    "entry": "online_gate",
                },
            ).payload,
            "Manager embedding execution start",
        )
        execution_id_value = created.get("execution_id")
        if not isinstance(execution_id_value, str) or not execution_id_value:
            raise SuiteError("Manager embedding execution_id is missing")
        execution_id = execution_id_value
        wait_for_manager_execution(client, execution_id, deadline)

        embeddings = list_item_embeddings(client, item_id)
        if len(embeddings) != 1:
            raise SuiteError("Manager embedding execution must create exactly one result")
        embedding = embeddings[0]
        embedding_id = positive_int(embedding.get("id"), "Manager embedding id")
        if embedding.get("status") != "ready":
            raise SuiteError("Manager embedding result must be ready")
        if embedding.get("item_fingerprint") != fingerprint:
            raise SuiteError("Manager embedding fingerprint does not match the source item")
        if embedding.get("model_profile_id") != expected_profile_id:
            raise SuiteError("Manager embedding did not use the configured Online Model Profile")

        keyword_query = Path(image_object).stem
        hybrid_result = search(client, engine_id, keyword_query, 10)
        hybrid_hits = matching_hits(hybrid_result, fingerprint)
        if len(hybrid_hits) != 1:
            raise SuiteError("hybrid query must return the fixture exactly once")
        assert_match(hybrid_hits[0], ["keyword", "vector"], locator)

        semantic_result = search(client, engine_id, SEMANTIC_QUERY, 1)
        semantic_hits = matching_hits(semantic_result, fingerprint)
        if len(semantic_hits) != 1:
            raise SuiteError("vector-only fixture must participate before unified pagination")
        assert_match(semantic_hits[0], ["vector"], locator)

        return {
            "schema_version": "addp.manager-hybrid-search/v1",
            "suite": "manager-hybrid-search",
            "scenario": "business-image-to-embedding-to-hybrid-retrieval",
            "run_id": environment["ADDP_ONLINE_TEST_RUN_ID"],
            "result": "passed",
            "tenant_id": str(tenant_id),
            "identity": identity,
            "engine_id": engine_id,
            "model_profile_id": expected_profile_id,
            "meta_scan_execution_id": scan_execution_id,
            "embedding_execution_id": execution_id,
            "source": {"item_id": item_id, "fingerprint": fingerprint, "locator": locator},
            "retrieval": {
                "hybrid_match_methods": hybrid_hits[0]["match_methods"],
                "semantic_match_methods": semantic_hits[0]["match_methods"],
                "semantic_page_size": 1,
                "deduplicated": True,
                "legacy_vector_hits_absent": True,
            },
            "cleanup": cleanup,
        }
    finally:
        cleanup_errors: list[str] = []
        if embedding_id is not None:
            try:
                client.request("DELETE", f"/api/v1/manager/embeddings/{embedding_id}", (200,))
                cleanup["embedding_deleted"] = True
            except SuiteError as error:
                cleanup_errors.append(str(error))
        try:
            residual = len(list_item_embeddings(client, item_id))
            cleanup["residual_resources"] = residual
            if residual != 0:
                cleanup_errors.append(f"Manager hybrid-search residual embedding count is {residual}")
        except SuiteError as error:
            cleanup_errors.append(str(error))
        if cleanup_errors:
            raise SuiteError("Manager hybrid-search cleanup failed: " + "; ".join(cleanup_errors))


def required_environment() -> dict[str, str]:
    names = (
        "ADDP_ONLINE_ARTIFACT_DIR",
        "ADDP_ONLINE_TEST_RUN_ID",
        "ADDP_ONLINE_TEST_TENANT_ID",
        "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN",
        "ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID",
        "ADDP_ONLINE_MANAGER_MINIO_BUCKET",
        "ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT",
        "ADDP_ONLINE_MANAGER_EMBEDDING_MODEL_PROFILE_ID",
        "GATEWAY_URL",
    )
    missing = [name for name in names if not os.environ.get(name)]
    if missing:
        raise SuiteError("missing Online environment: " + ", ".join(missing))
    return dict(os.environ)


def main() -> int:
    try:
        report = run_scenario(required_environment())
    except (OSError, ValueError, SuiteError) as error:
        print(f"Manager hybrid-search Online acceptance failed: {error}", file=sys.stderr)
        return 1
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
