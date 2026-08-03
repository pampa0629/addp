import json
import os

import httpx
import pytest

from addp_common.notebook import engines


def postgresql_descriptor(engine_id=21):
    return {
        "id": engine_id,
        "name": "PostgreSQL",
        "engine_type": "postgresql",
        "capabilities": {
            "engine_type": "postgresql",
            "storage": {
                "catalog": {"supported": True},
                "catalog_model": {
                    "path_version": "catalog.path/v1",
                    "root_term": "server",
                    "levels": [
                        {"term": "schema", "role": "branch"},
                        {"term": "table", "role": "leaf"},
                    ],
                },
            },
        },
    }


def catalog_entry(engine_id, name, term, kind, role, segments):
    return {
        "name": name,
        "term": term,
        "kind": kind,
        "role": role,
        "path": {
            "version": "catalog.path/v1",
            "engine_id": engine_id,
            "segments": segments,
        },
    }


@pytest.fixture
def notebook_session(monkeypatch):
    descriptor_endpoint = (
        "http://develop:8185/api/v1/develop/notebook-kernel-sessions/"
        "00000000-0000-0000-0000-000000000001/engine-descriptors"
    )
    catalog_endpoint = (
        "http://develop:8185/api/v1/develop/notebook-kernel-sessions/"
        "00000000-0000-0000-0000-000000000001/catalog/children"
    )
    table_scan_endpoint = (
        "http://develop:8185/api/v1/develop/notebook-kernel-sessions/"
        "00000000-0000-0000-0000-000000000001/table-scans"
    )
    query_endpoint = (
        "http://develop:8185/api/v1/develop/notebook-kernel-sessions/"
        "00000000-0000-0000-0000-000000000001/queries"
    )
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_API_ENDPOINT", descriptor_endpoint)
    monkeypatch.setenv("ADDP_NOTEBOOK_CATALOG_API_ENDPOINT", catalog_endpoint)
    monkeypatch.setenv("ADDP_NOTEBOOK_TABLE_SCAN_API_ENDPOINT", table_scan_endpoint)
    monkeypatch.setenv("ADDP_NOTEBOOK_QUERY_API_ENDPOINT", query_endpoint)
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN", "addp_nkc_kernel-secret")
    return descriptor_endpoint, catalog_endpoint


def arrow_stream_bytes():
    import pyarrow as pa

    sink = pa.BufferOutputStream()
    schema = pa.schema([("id", pa.int64()), ("name", pa.string())])
    with pa.ipc.new_stream(sink, schema) as writer:
        writer.write_batch(pa.record_batch([[1, 2], ["first", "second"]], schema=schema))
    return sink.getvalue().to_pybytes()


def test_list_uses_only_injected_session_capability(monkeypatch):
    endpoint = "http://develop:8185/api/v1/develop/notebook-kernel-sessions/session-1/engine-descriptors"
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_API_ENDPOINT", endpoint)
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN", "addp_nkc_kernel-secret")
    monkeypatch.setenv("ADDP_TOKEN", "must-not-be-used")

    def handler(request: httpx.Request) -> httpx.Response:
        assert str(request.url) == endpoint
        assert request.headers["Authorization"] == "Bearer addp_nkc_kernel-secret"
        assert request.headers["Cache-Control"] == "no-store"
        assert "must-not-be-used" not in str(request.headers)
        return httpx.Response(200, json=[{"id": 21, "name": "PostgreSQL", "engine_type": "postgresql"}])

    monkeypatch.setattr(engines, "_transport", httpx.MockTransport(handler))
    assert engines.list() == [{"id": 21, "name": "PostgreSQL", "engine_type": "postgresql"}]


@pytest.mark.parametrize(
    ("endpoint", "token"),
    [
        ("", "addp_nkc_kernel-secret"),
        ("http://develop/session", ""),
        ("http://develop/session", "wrong-prefix"),
    ],
)
def test_list_fails_closed_without_valid_session_environment(monkeypatch, endpoint, token):
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_API_ENDPOINT", endpoint)
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN", token)
    with pytest.raises(engines.NotebookSessionUnavailableError):
        engines.list()


def test_list_reports_http_failure_without_exposing_token(monkeypatch):
    secret = "addp_nkc_do-not-leak"
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_API_ENDPOINT", "http://develop/session")
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN", secret)
    monkeypatch.setattr(
        engines,
        "_transport",
        httpx.MockTransport(lambda _request: httpx.Response(401, json={"error": "unauthorized"})),
    )
    with pytest.raises(engines.NotebookEngineDiscoveryError) as error:
        engines.list()
    assert secret not in str(error.value)


def test_list_rejects_non_array_response(monkeypatch):
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_API_ENDPOINT", "http://develop/session")
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN", "addp_nkc_kernel-secret")
    monkeypatch.setattr(
        engines,
        "_transport",
        httpx.MockTransport(lambda _request: httpx.Response(200, json={"data": []})),
    )
    with pytest.raises(engines.NotebookEngineDiscoveryError):
        engines.list()


def test_postgresql_facade_navigates_with_native_terms_and_only_caches_paths(
    monkeypatch, notebook_session
):
    descriptor_endpoint, catalog_endpoint = notebook_session
    engine_id = 21
    server_segments = [{"term": "server", "kind": "server", "name": ""}]
    schema_segments = [
        *server_segments,
        {"term": "schema", "kind": "schema", "name": "public"},
    ]
    table_segments = [
        *schema_segments,
        {"term": "table", "kind": "table", "name": "roads"},
    ]
    catalog_requests = []

    def handler(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            assert str(request.url) == descriptor_endpoint
            return httpx.Response(200, json=[postgresql_descriptor(engine_id)])
        assert str(request.url) == catalog_endpoint
        body = json.loads(request.content)
        catalog_requests.append(body)
        segments = body["path"]["segments"]
        if not segments:
            return httpx.Response(
                200,
                json={
                    "nodes": [
                        catalog_entry(
                            engine_id,
                            "PostgreSQL",
                            "server",
                            "postgresql",
                            "branch",
                            server_segments,
                        )
                    ]
                },
            )
        if segments == server_segments:
            return httpx.Response(
                200,
                json={
                    "nodes": [
                        catalog_entry(
                            engine_id, "public", "schema", "schema", "branch", schema_segments
                        )
                    ]
                },
            )
        if segments == schema_segments:
            return httpx.Response(
                200,
                json={
                    "nodes": [
                        catalog_entry(
                            engine_id, "roads", "table", "table", "leaf", table_segments
                        )
                    ]
                },
            )
        raise AssertionError(f"unexpected catalog path: {segments}")

    monkeypatch.setattr(engines, "_transport", httpx.MockTransport(handler))
    postgresql = engines.client(engine_id)

    schemas = postgresql.schemas()
    assert [schema.name for schema in schemas] == ["public"]
    assert [table.name for table in schemas[0].tables()] == ["roads"]
    assert postgresql.table(schema="public", name="roads").kind == "table"
    assert [table.name for table in postgresql.tables(schema="public")] == ["roads"]

    requested_segments = [request["path"]["segments"] for request in catalog_requests]
    assert requested_segments.count([]) == 1
    assert requested_segments.count(server_segments) == 1
    assert requested_segments.count(schema_segments) == 3
    assert all(request["options"] == {"recursive": False, "limit": 1000, "offset": 0} for request in catalog_requests)


def test_client_uses_exact_engine_type_registration(monkeypatch, notebook_session):
    monkeypatch.setattr(engines, "list", lambda **_kwargs: [{"id": 7, "engine_type": "postgres"}])
    with pytest.raises(engines.NotebookCatalogUnsupportedError):
        engines.client(7)


def test_client_rejects_incompatible_postgresql_catalog_model(monkeypatch, notebook_session):
    descriptor = postgresql_descriptor()
    descriptor["capabilities"]["storage"]["catalog_model"]["levels"][1]["term"] = "relation"
    monkeypatch.setattr(engines, "list", lambda **_kwargs: [descriptor])
    with pytest.raises(engines.NotebookCatalogUnsupportedError):
        engines.client(21)


@pytest.mark.parametrize(
    ("status", "code", "error_type"),
    [
        (400, "catalog_request_invalid", engines.NotebookCatalogRequestError),
        (403, "notebook_catalog_forbidden", engines.NotebookCatalogForbiddenError),
        (404, "engine_not_found", engines.NotebookEngineNotFoundError),
        (404, "catalog_entry_not_found", engines.NotebookCatalogEntryNotFoundError),
        (422, "catalog_operation_unsupported", engines.NotebookCatalogUnsupportedError),
        (502, "catalog_control_plane_failed", engines.NotebookCatalogControlPlaneError),
        (502, "catalog_provider_failed", engines.NotebookCatalogProviderError),
        (503, "engine_unavailable", engines.NotebookEngineUnavailableError),
        (504, "catalog_timeout", engines.NotebookCatalogTimeoutError),
    ],
)
def test_catalog_error_codes_map_to_stable_exception_types(
    monkeypatch, notebook_session, status, code, error_type
):
    monkeypatch.setattr(engines, "list", lambda **_kwargs: [postgresql_descriptor()])
    monkeypatch.setattr(
        engines,
        "_transport",
        httpx.MockTransport(lambda _request: httpx.Response(status, json={"error_code": code})),
    )
    with pytest.raises(error_type):
        engines.client(21).schemas()


def test_catalog_navigation_uses_one_method_deadline(monkeypatch, notebook_session):
    monkeypatch.setattr(engines, "list", lambda **_kwargs: [postgresql_descriptor()])
    observed_timeouts = []
    clock = iter([100.0, 100.1, 100.4])
    monkeypatch.setattr(engines.time, "monotonic", lambda: next(clock))

    server_path = {
        "version": "catalog.path/v1",
        "engine_id": 21,
        "segments": [{"term": "server", "kind": "postgresql", "name": "PostgreSQL"}],
    }

    def request(_method, _endpoint, _token, *, timeout, json=None):
        observed_timeouts.append(timeout)
        if not json["path"]["segments"]:
            return httpx.Response(
                200,
                json={
                    "nodes": [
                        catalog_entry(
                            21,
                            "PostgreSQL",
                            "server",
                            "postgresql",
                            "branch",
                            server_path["segments"],
                        )
                    ]
                },
            )
        return httpx.Response(200, json={"nodes": []})

    monkeypatch.setattr(engines, "_request", request)
    assert engines.client(21, timeout=1.0).schemas() == []
    assert observed_timeouts == pytest.approx([0.9, 0.6])


def test_tables_paginates_within_one_method_call(monkeypatch, notebook_session):
    monkeypatch.setattr(engines, "_CATALOG_PAGE_SIZE", 2)
    monkeypatch.setattr(engines, "list", lambda **_kwargs: [postgresql_descriptor()])
    offsets = []
    server_path = [{"term": "server", "kind": "server", "name": ""}]
    schema_path = [*server_path, {"term": "schema", "kind": "namespace", "name": "public"}]

    def handler(request: httpx.Request) -> httpx.Response:
        body = json.loads(request.content)
        segments = body["path"]["segments"]
        if not segments:
            return httpx.Response(
                200,
                json={
                    "nodes": [
                        catalog_entry(21, "PostgreSQL", "server", "server", "branch", server_path)
                    ]
                },
            )
        if segments == server_path:
            return httpx.Response(
                200,
                json={
                    "nodes": [
                        catalog_entry(21, "public", "schema", "namespace", "branch", schema_path)
                    ]
                },
            )
        offsets.append(body["options"]["offset"])
        names = {0: ["roads", "buildings"], 2: ["stations"]}[body["options"]["offset"]]
        return httpx.Response(
            200,
            json={
                "nodes": [
                    catalog_entry(
                        21,
                        name,
                        "table",
                        "table",
                        "leaf",
                        [*schema_path, {"term": "table", "kind": "table", "name": name}],
                    )
                    for name in names
                ]
            },
        )

    monkeypatch.setattr(engines, "_transport", httpx.MockTransport(handler))
    assert [table.name for table in engines.client(21).tables()] == [
        "roads",
        "buildings",
        "stations",
    ]
    assert offsets == [0, 2]


def test_table_scan_streams_arrow_batches_with_session_capability(monkeypatch, notebook_session):
    scan_endpoint = os.environ["ADDP_NOTEBOOK_TABLE_SCAN_API_ENDPOINT"]
    observed = {}

    def handler(request: httpx.Request) -> httpx.Response:
        assert str(request.url) == scan_endpoint
        assert request.headers["Authorization"] == "Bearer addp_nkc_kernel-secret"
        observed.update(json.loads(request.content))
        return httpx.Response(
            200,
            headers={"Content-Type": "application/vnd.apache.arrow.stream"},
            content=arrow_stream_bytes(),
        )

    monkeypatch.setattr(engines, "_transport", httpx.MockTransport(handler))
    pg = engines.PostgreSQLEngine(engine_id=21, descriptor=postgresql_descriptor(), timeout=10)
    table = engines.PostgreSQLTable(
        name="roads", schema="public", kind="table", _client=pg,
        _path={
            "version": "catalog.path/v1", "engine_id": 21,
            "segments": [{"term": "table", "kind": "table", "name": "roads"}],
        },
    )
    batches = list(table.scan(batch_size=1024))
    assert len(batches) == 1
    assert batches[0].column(0).to_pylist() == [1, 2]
    assert observed["engine_id"] == 21
    assert observed["batch_size"] == 1024
    assert observed["max_rows"] == 0


def test_table_to_pandas_rejects_catalog_estimate_before_scan(monkeypatch, notebook_session):
    pg = engines.PostgreSQLEngine(engine_id=21, descriptor=postgresql_descriptor(), timeout=10)
    table = engines.PostgreSQLTable(
        name="roads", schema="public", kind="table", _client=pg,
        _path={"version": "catalog.path/v1", "engine_id": 21, "segments": [{}]},
        _facts={"size_bytes": 2048},
    )
    with pytest.raises(engines.NotebookMemoryLimitError):
        table.to_pandas(memory_limit="1KiB")


def test_postgresql_sql_uses_bounded_query_endpoint(monkeypatch, notebook_session):
    query_endpoint = os.environ["ADDP_NOTEBOOK_QUERY_API_ENDPOINT"]
    observed = {}

    def handler(request: httpx.Request) -> httpx.Response:
        assert str(request.url) == query_endpoint
        observed.update(json.loads(request.content))
        return httpx.Response(
            200,
            headers={"Content-Type": "application/vnd.apache.arrow.stream"},
            content=arrow_stream_bytes(),
        )

    monkeypatch.setattr(engines, "_transport", httpx.MockTransport(handler))
    pg = engines.PostgreSQLEngine(engine_id=21, descriptor=postgresql_descriptor(), timeout=10)
    result = pg.sql("SELECT * FROM public.roads WHERE id > $1", params=[100], max_rows=1000, timeout=30)
    assert result["id"].tolist() == [1, 2]
    assert observed == {
        "engine_id": 21,
        "query": "SELECT * FROM public.roads WHERE id > $1",
        "params": [100],
        "max_rows": 1000,
        "timeout": 30,
    }


@pytest.mark.parametrize("timeout", [0, 0.5, True, 301])
def test_postgresql_sql_requires_integer_timeout(timeout, notebook_session):
    pg = engines.PostgreSQLEngine(engine_id=21, descriptor=postgresql_descriptor(), timeout=10)
    with pytest.raises(engines.NotebookDataRequestError):
        pg.sql("SELECT 1", max_rows=1, timeout=timeout)
