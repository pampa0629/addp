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


def native_descriptor(engine_type, root_term, levels, engine_id=21):
    return {
        "id": engine_id,
        "name": engine_type,
        "engine_type": engine_type,
        "capabilities": {
            "engine_type": engine_type,
            "storage": {
                "catalog": {"supported": True, "real_time": True},
                "catalog_model": {
                    "path_version": "catalog.path/v1",
                    "root_term": root_term,
                    "levels": [
                        {"term": term, "role": role}
                        for term, role in levels
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
    endpoint_base = (
        "http://develop:8185/api/v1/develop/notebook-kernel-sessions/"
        "00000000-0000-0000-0000-000000000001"
    )
    for env_name, suffix in {
        "ADDP_NOTEBOOK_RECORD_SCAN_API_ENDPOINT": "record-scans",
        "ADDP_NOTEBOOK_GRAPH_SAMPLE_API_ENDPOINT": "graph-samples",
        "ADDP_NOTEBOOK_GRAPH_QUERY_API_ENDPOINT": "graph-queries",
        "ADDP_NOTEBOOK_CONTENT_READ_API_ENDPOINT": "content-reads",
        "ADDP_NOTEBOOK_CHANGE_STREAM_API_ENDPOINT": "change-streams",
    }.items():
        monkeypatch.setenv(env_name, f"{endpoint_base}/{suffix}")
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN", "addp_nkc_kernel-secret")
    return descriptor_endpoint, catalog_endpoint


def arrow_stream_bytes():
    import pyarrow as pa

    sink = pa.BufferOutputStream()
    schema = pa.schema([("id", pa.int64()), ("name", pa.string())])
    with pa.ipc.new_stream(sink, schema) as writer:
        writer.write_batch(pa.record_batch([[1, 2], ["first", "second"]], schema=schema))
    return sink.getvalue().to_pybytes()


def spatial_arrow_stream_bytes():
    import pyarrow as pa

    sink = pa.BufferOutputStream()
    schema = pa.schema([("id", pa.int64()), ("shape", pa.binary())])
    point_wkb = bytes.fromhex("0101000000000000000000F03F0000000000000040")
    with pa.ipc.new_stream(sink, schema) as writer:
        writer.write_batch(pa.record_batch([[1], [point_wkb]], schema=schema))
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
    with pytest.raises(engines.NotebookEngineCatalogUnsupportedError):
        engines.client(7)


@pytest.mark.parametrize(
    ("engine_type", "root_term", "levels", "client_type"),
    [
        ("mysql", "server", (("database", "branch"), ("table", "leaf")), engines.MySQLEngine),
        ("doris", "server", (("database", "branch"), ("table", "leaf")), engines.DorisEngine),
        ("clickhouse", "server", (("database", "branch"), ("table", "leaf")), engines.ClickHouseEngine),
        ("spark", "server", (("database", "branch"), ("table", "leaf")), engines.SparkSQLEngine),
        ("mongodb", "server", (("database", "branch"), ("collection", "leaf")), engines.MongoDBEngine),
        ("neo4j", "server", (("database", "branch"), ("graph", "leaf")), engines.Neo4jEngine),
        ("minio", "service", (("bucket", "branch"), ("prefix", "branch"), ("object", "leaf")), engines.MinIOEngine),
        ("s3", "service", (("bucket", "branch"), ("prefix", "branch"), ("object", "leaf")), engines.S3Engine),
        ("nfs", "root", (("directory", "branch"), ("file", "leaf")), engines.NFSEngine),
        ("kafka", "service", (("topic", "leaf"),), engines.KafkaEngine),
    ],
)
def test_client_registers_every_native_catalog_engine(
    monkeypatch, notebook_session, engine_type, root_term, levels, client_type
):
    descriptor = native_descriptor(engine_type, root_term, levels)
    monkeypatch.setattr(engines, "list", lambda **_kwargs: [descriptor])
    assert isinstance(engines.client(21), client_type)


def test_mysql_facade_navigates_database_and_table(monkeypatch, notebook_session):
    descriptor_endpoint, catalog_endpoint = notebook_session
    descriptor = native_descriptor(
        "mysql", "server", (("database", "branch"), ("table", "leaf")), engine_id=3
    )
    root_segments = [{"term": "server", "kind": "server", "name": ""}]
    database_segments = [
        *root_segments,
        {"term": "database", "kind": "namespace", "name": "business"},
    ]

    def handler(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            assert str(request.url) == descriptor_endpoint
            return httpx.Response(200, json=[descriptor])
        assert str(request.url) == catalog_endpoint
        segments = json.loads(request.content)["path"]["segments"]
        if not segments:
            return httpx.Response(200, json={"nodes": [
                catalog_entry(3, "", "server", "server", "branch", root_segments)
            ]})
        if segments == root_segments:
            return httpx.Response(200, json={"nodes": [
                catalog_entry(3, "business", "database", "namespace", "branch", database_segments)
            ]})
        if segments == database_segments:
            return httpx.Response(200, json={"nodes": [
                catalog_entry(
                    3, "orders", "table", "table", "leaf",
                    [*database_segments, {"term": "table", "kind": "table", "name": "orders"}],
                )
            ]})
        raise AssertionError(segments)

    monkeypatch.setattr(engines, "_transport", httpx.MockTransport(handler))
    mysql = engines.client(3)
    assert [database.name for database in mysql.databases()] == ["business"]
    assert [table.name for table in mysql.tables(database="business")] == ["orders"]
    assert mysql.table(database="business", name="orders").database == "business"


def test_duckdb_runtime_is_not_a_native_data_engine(monkeypatch, notebook_session):
    monkeypatch.setattr(engines, "list", lambda **_kwargs: [{"id": 18, "engine_type": "duckdb"}])
    with pytest.raises(engines.NotebookEngineCatalogUnsupportedError):
        engines.client(18)


def test_client_rejects_incompatible_postgresql_catalog_model(monkeypatch, notebook_session):
    descriptor = postgresql_descriptor()
    descriptor["capabilities"]["storage"]["catalog_model"]["levels"][1]["term"] = "relation"
    monkeypatch.setattr(engines, "list", lambda **_kwargs: [descriptor])
    with pytest.raises(engines.NotebookEngineCatalogUnsupportedError):
        engines.client(21)


@pytest.mark.parametrize(
    ("status", "code", "error_type"),
    [
        (400, "engine_catalog_request_invalid", engines.NotebookEngineCatalogRequestError),
        (403, "notebook_engine_catalog_forbidden", engines.NotebookEngineCatalogForbiddenError),
        (404, "engine_not_found", engines.NotebookEngineNotFoundError),
        (404, "engine_catalog_entry_not_found", engines.NotebookEngineCatalogEntryNotFoundError),
        (422, "engine_catalog_operation_unsupported", engines.NotebookEngineCatalogUnsupportedError),
        (502, "engine_catalog_control_plane_failed", engines.NotebookEngineCatalogControlPlaneError),
        (502, "engine_catalog_provider_failed", engines.NotebookEngineCatalogProviderError),
        (503, "engine_unavailable", engines.NotebookEngineUnavailableError),
        (504, "engine_catalog_timeout", engines.NotebookEngineCatalogTimeoutError),
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


def test_postgresql_table_to_geopandas_uses_verified_geometry_column_and_crs(
    monkeypatch, notebook_session
):
    pytest.importorskip("geopandas")
    scan_endpoint = os.environ["ADDP_NOTEBOOK_TABLE_SCAN_API_ENDPOINT"]

    def handler(request: httpx.Request) -> httpx.Response:
        assert str(request.url) == scan_endpoint
        return httpx.Response(
            200,
            headers={"Content-Type": "application/vnd.apache.arrow.stream"},
            content=spatial_arrow_stream_bytes(),
        )

    monkeypatch.setattr(engines, "_transport", httpx.MockTransport(handler))
    pg = engines.PostgreSQLEngine(engine_id=21, descriptor=postgresql_descriptor(), timeout=10)
    table = engines.PostgreSQLTable(
        name="roads", schema="public", kind="table", _client=pg,
        _path={"version": "catalog.path/v1", "engine_id": 21, "segments": [{}]},
    )

    result = table.to_geopandas(
        memory_limit="1MiB", geometry_column="shape", crs="EPSG:4326"
    )

    assert result.geometry.name == "shape"
    assert result.crs.to_string() == "EPSG:4326"
    assert result.geometry.iloc[0].x == 1.0
    assert result.geometry.iloc[0].y == 2.0


def test_postgresql_table_to_geopandas_rejects_unknown_geometry_column(
    monkeypatch, notebook_session
):
    monkeypatch.setattr(
        engines,
        "_transport",
        httpx.MockTransport(lambda _request: httpx.Response(
            200,
            headers={"Content-Type": "application/vnd.apache.arrow.stream"},
            content=spatial_arrow_stream_bytes(),
        )),
    )
    pg = engines.PostgreSQLEngine(engine_id=21, descriptor=postgresql_descriptor(), timeout=10)
    table = engines.PostgreSQLTable(
        name="roads", schema="public", kind="table", _client=pg,
        _path={"version": "catalog.path/v1", "engine_id": 21, "segments": [{}]},
    )

    with pytest.raises(engines.NotebookDataRequestError, match="missing_shape"):
        table.to_geopandas(
            memory_limit="1MiB", geometry_column="missing_shape", crs="EPSG:4326"
        )


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
        "language": "sql",
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


def test_database_tabular_engine_uses_shared_scan_and_native_sql(monkeypatch, notebook_session):
    observed = []

    def handler(request: httpx.Request) -> httpx.Response:
        observed.append((str(request.url), json.loads(request.content)))
        return httpx.Response(
            200,
            headers={"Content-Type": "application/vnd.apache.arrow.stream"},
            content=arrow_stream_bytes(),
        )

    monkeypatch.setattr(engines, "_transport", httpx.MockTransport(handler))
    mysql = engines.MySQLEngine(
        engine_id=3,
        descriptor=native_descriptor(
            "mysql", "server", (("database", "branch"), ("table", "leaf")), engine_id=3
        ),
        timeout=10,
    )
    table = engines.DatabaseTable(
        name="orders",
        database="business",
        kind="table",
        _client=mysql,
        _path={"version": "catalog.path/v1", "engine_id": 3, "segments": [{}]},
    )
    assert list(table.scan(batch_size=256))[0].num_rows == 2
    assert mysql.sql("SELECT id FROM business.orders", max_rows=10, timeout=20)["id"].tolist() == [1, 2]
    assert observed[0][1]["batch_size"] == 256
    assert observed[1][1]["language"] == "sql"


def test_mongodb_collection_scan_restores_dynamic_documents(monkeypatch, notebook_session):
    import pyarrow as pa

    sink = pa.BufferOutputStream()
    schema = pa.schema([("document", pa.string())])
    with pa.ipc.new_stream(sink, schema) as writer:
        writer.write_batch(
            pa.record_batch(
                [[json.dumps({"_id": "a", "value": 1}), json.dumps({"_id": "b", "extra": True})]],
                schema=schema,
            )
        )

    def handler(request: httpx.Request) -> httpx.Response:
        assert str(request.url) == os.environ["ADDP_NOTEBOOK_RECORD_SCAN_API_ENDPOINT"]
        return httpx.Response(
            200,
            headers={"Content-Type": "application/vnd.apache.arrow.stream"},
            content=sink.getvalue().to_pybytes(),
        )

    monkeypatch.setattr(engines, "_transport", httpx.MockTransport(handler))
    mongodb = engines.MongoDBEngine(
        engine_id=5,
        descriptor=native_descriptor(
            "mongodb", "server", (("database", "branch"), ("collection", "leaf")), engine_id=5
        ),
        timeout=10,
    )
    collection = engines.MongoDBCollection(
        name="events", database="business", kind="collection", _client=mongodb,
        _path={"version": "catalog.path/v1", "engine_id": 5, "segments": [{}]},
    )
    assert list(collection.scan(batch_size=2)) == [
        {"_id": "a", "value": 1},
        {"_id": "b", "extra": True},
    ]


def test_neo4j_native_graph_methods_preserve_graph_json(monkeypatch, notebook_session):
    observed = []

    def handler(request: httpx.Request) -> httpx.Response:
        observed.append((str(request.url), json.loads(request.content)))
        if str(request.url).endswith("/graph-samples"):
            return httpx.Response(200, json={"nodes": [], "relationships": []})
        return httpx.Response(
            200,
            json={
                "columns": ["n"],
                "rows": [{"n": {"element_id": "1"}}],
                "graph_data": {
                    "nodes": [
                        {"element_id": "1", "labels": ["Person"], "properties": {}}
                    ],
                    "relationships": [],
                },
            },
        )

    monkeypatch.setattr(engines, "_transport", httpx.MockTransport(handler))
    neo4j = engines.Neo4jEngine(
        engine_id=6,
        descriptor=native_descriptor(
            "neo4j", "server", (("database", "branch"), ("graph", "leaf")), engine_id=6
        ),
        timeout=10,
    )
    graph = engines.Neo4jGraph(
        name="graph", database="neo4j", kind="graph", _client=neo4j,
        _path={"version": "catalog.path/v1", "engine_id": 6, "segments": [{}]},
    )
    assert graph.sample(limit=10) == {"nodes": [], "relationships": []}
    result = neo4j.cypher("MATCH (n) RETURN n", max_rows=20, timeout=30)
    assert result["graph_data"]["nodes"][0]["element_id"] == "1"
    assert observed[0][1]["limit"] == 10
    assert observed[1][1]["max_rows"] == 20


def test_object_content_open_and_range_own_stream_lifecycle(monkeypatch, notebook_session):
    observed = []

    def handler(request: httpx.Request) -> httpx.Response:
        observed.append(json.loads(request.content))
        return httpx.Response(
            200, headers={"Content-Type": "application/octet-stream"}, content=b"abcdef"
        )

    monkeypatch.setattr(engines, "_transport", httpx.MockTransport(handler))
    minio = engines.MinIOEngine(
        engine_id=8,
        descriptor=native_descriptor(
            "minio", "service",
            (("bucket", "branch"), ("prefix", "branch"), ("object", "leaf")),
            engine_id=8,
        ),
        timeout=10,
    )
    resource = engines.ObjectStorageObject(
        name="data.bin", bucket="raw", prefix="", kind="object", _client=minio,
        _path={"version": "catalog.path/v1", "engine_id": 8, "segments": [{}]},
    )
    with resource.open() as stream:
        assert stream.read() == b"abcdef"
    assert resource.read_range(offset=2, length=3) == b"abcdef"
    assert "range" not in observed[0]
    assert observed[1]["range"] == {"offset": 2, "length": 3}


def test_object_content_open_maps_connect_timeout(monkeypatch, notebook_session):
    def handler(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectTimeout("connect timed out", request=request)

    monkeypatch.setattr(engines, "_transport", httpx.MockTransport(handler))
    minio = engines.MinIOEngine(
        engine_id=8,
        descriptor=native_descriptor(
            "minio", "service",
            (("bucket", "branch"), ("prefix", "branch"), ("object", "leaf")),
            engine_id=8,
        ),
        timeout=10,
    )
    resource = engines.ObjectStorageObject(
        name="data.bin", bucket="datasets", prefix="", kind="object", _client=minio,
        _path={"version": "catalog.path/v1", "engine_id": 8, "segments": [{}]},
    )

    with pytest.raises(engines.NotebookDataTimeoutError):
        resource.open()


def test_kafka_stream_restores_binary_record_fields(monkeypatch, notebook_session):
    payload = {
        "topic": "events", "partition": "0", "offset": 7,
        "key": "aw==", "value": "dg==",
        "headers": [{"key": "trace", "value": "aA=="}],
        "position": {"values": {"next_offset": "8"}},
    }

    def handler(request: httpx.Request) -> httpx.Response:
        body = json.loads(request.content)
        assert body["initial_position"] == "earliest"
        assert body["positions"] == {"0": 4}
        return httpx.Response(
            200,
            headers={"Content-Type": "application/x-ndjson"},
            content=(json.dumps(payload) + "\n").encode(),
        )

    monkeypatch.setattr(engines, "_transport", httpx.MockTransport(handler))
    kafka = engines.KafkaEngine(
        engine_id=9,
        descriptor=native_descriptor("kafka", "service", (("topic", "leaf"),), engine_id=9),
        timeout=10,
    )
    topic = engines.KafkaTopic(
        name="events", kind="topic", _client=kafka,
        _path={"version": "catalog.path/v1", "engine_id": 9, "segments": [{}]},
    )
    stream = topic.stream(initial_position="earliest", positions={0: 4})
    record = next(stream)
    stream.close()
    assert record["key"] == b"k"
    assert record["value"] == b"v"
    assert record["headers"][0]["value"] == b"h"
