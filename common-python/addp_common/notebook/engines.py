"""面向 Notebook 使用者的 ADDP 原生引擎发现门面。"""

from __future__ import annotations

import builtins
import base64
import io
import json
import os
import re
import time
from dataclasses import dataclass, field
from typing import Any
from urllib.parse import urlsplit

import httpx


_DESCRIPTOR_ENDPOINT_ENV = "ADDP_NOTEBOOK_OWNER_API_ENDPOINT"
_CATALOG_ENDPOINT_ENV = "ADDP_NOTEBOOK_CATALOG_API_ENDPOINT"
_TABLE_SCAN_ENDPOINT_ENV = "ADDP_NOTEBOOK_TABLE_SCAN_API_ENDPOINT"
_RECORD_SCAN_ENDPOINT_ENV = "ADDP_NOTEBOOK_RECORD_SCAN_API_ENDPOINT"
_QUERY_ENDPOINT_ENV = "ADDP_NOTEBOOK_QUERY_API_ENDPOINT"
_GRAPH_SAMPLE_ENDPOINT_ENV = "ADDP_NOTEBOOK_GRAPH_SAMPLE_API_ENDPOINT"
_GRAPH_QUERY_ENDPOINT_ENV = "ADDP_NOTEBOOK_GRAPH_QUERY_API_ENDPOINT"
_CONTENT_READ_ENDPOINT_ENV = "ADDP_NOTEBOOK_CONTENT_READ_API_ENDPOINT"
_CHANGE_STREAM_ENDPOINT_ENV = "ADDP_NOTEBOOK_CHANGE_STREAM_API_ENDPOINT"
_CAPABILITY_ENV = "ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN"
_CAPABILITY_PREFIX = "addp_nkc_"
_CATALOG_PATH_VERSION = "catalog.path/v1"
_CATALOG_PAGE_SIZE = 1000
_transport: httpx.BaseTransport | None = None


class NotebookSessionUnavailableError(RuntimeError):
    """当前 Python process 不属于有效的受控 Notebook 会话。"""


class NotebookEngineDiscoveryError(RuntimeError):
    """当前 Notebook 会话未能读取可用 Engine 描述。"""


class NotebookEngineCatalogError(RuntimeError):
    """Notebook 原生引擎门面的 Catalog 基础错误。"""


class NotebookEngineCatalogRequestError(NotebookEngineCatalogError):
    pass


class NotebookEngineCatalogForbiddenError(NotebookEngineCatalogError):
    pass


class NotebookEngineNotFoundError(NotebookEngineCatalogError):
    pass


class NotebookEngineCatalogEntryNotFoundError(NotebookEngineCatalogError):
    pass


class NotebookEngineCatalogUnsupportedError(NotebookEngineCatalogError):
    pass


class NotebookEngineCatalogControlPlaneError(NotebookEngineCatalogError):
    pass


class NotebookEngineCatalogProviderError(NotebookEngineCatalogError):
    pass


class NotebookEngineUnavailableError(NotebookEngineCatalogError):
    pass


class NotebookEngineCatalogTimeoutError(NotebookEngineCatalogError):
    pass


class NotebookDataError(RuntimeError):
    """Notebook 受控数据读取基础错误。"""


class NotebookDataForbiddenError(NotebookDataError):
    pass


class NotebookDataRequestError(NotebookDataError):
    pass


class NotebookDataUnsupportedError(NotebookDataError):
    pass


class NotebookDataControlPlaneError(NotebookDataError):
    pass


class NotebookDataProviderError(NotebookDataError):
    pass


class NotebookDataTimeoutError(NotebookDataError):
    pass


class NotebookMemoryLimitError(NotebookDataError):
    pass


def list(*, timeout: float = 10.0) -> builtins.list[dict[str, Any]]:
    """返回当前会话所属租户中可用的脱敏查询 Engine 描述列表。"""
    endpoint, token = _session_endpoint_and_token(_DESCRIPTOR_ENDPOINT_ENV)
    response = _request("GET", endpoint, token, timeout=timeout)
    if response.status_code != httpx.codes.OK:
        raise NotebookEngineDiscoveryError(f"ADDP Notebook Engine 发现接口返回 HTTP {response.status_code}")
    try:
        payload = response.json()
    except ValueError as exc:
        raise NotebookEngineDiscoveryError("ADDP Notebook Engine 发现接口返回了无效 JSON") from exc
    if not isinstance(payload, builtins.list) or any(not isinstance(item, dict) for item in payload):
        raise NotebookEngineDiscoveryError("ADDP Notebook Engine 发现接口返回了无效描述列表")
    return payload


def client(engine_id: int, *, timeout: float = 10.0) -> Any:
    """按精确 engine_type 返回已注册的原生只读引擎客户端。"""
    if isinstance(engine_id, bool) or not isinstance(engine_id, int) or engine_id <= 0:
        raise NotebookEngineCatalogRequestError("engine_id 必须是正整数")
    descriptor = next((item for item in list(timeout=timeout) if item.get("id") == engine_id), None)
    if descriptor is None:
        raise NotebookEngineNotFoundError(f"Engine {engine_id} 不存在或当前不可用")
    engine_type = descriptor.get("engine_type")
    client_type = _CLIENT_TYPES.get(engine_type)
    if client_type is None:
        raise NotebookEngineCatalogUnsupportedError(f"当前 SDK 尚未注册 engine_type={engine_type!r} 的原生门面")
    client_type.validate_descriptor(descriptor)
    return client_type(engine_id=engine_id, descriptor=descriptor, timeout=timeout)


@dataclass(frozen=True)
class PostgreSQLTable:
    name: str
    schema: str
    kind: str
    _client: PostgreSQLEngine = field(repr=False, compare=False)
    _path: dict[str, Any] = field(repr=False, compare=False)
    _facts: dict[str, Any] = field(default_factory=dict, repr=False, compare=False)

    def scan(self, *, batch_size: int = 65536):
        """按扫描开始时的一致快照流式返回 pyarrow.RecordBatch。"""
        return self._client._scan_table(self._path, batch_size=batch_size, max_rows=0)

    def head(self, max_rows: int = 100):
        """以有界扫描返回前 max_rows 行 pandas DataFrame。"""
        max_rows = _positive_int(max_rows, "max_rows")
        import pyarrow as pa

        batches = builtins.list(
            self._client._scan_table(self._path, batch_size=min(max_rows, 65536), max_rows=max_rows)
        )
        return pa.Table.from_batches(batches).to_pandas()

    def to_pandas(self, *, memory_limit: str | int):
        """在显式内存预算内消费完整 scan()，超限时不返回部分结果。"""
        limit = _memory_limit_bytes(memory_limit)
        estimated = self._facts.get("size_bytes")
        if isinstance(estimated, int) and not isinstance(estimated, bool) and estimated > limit:
            raise NotebookMemoryLimitError(
                f"表的 Engine Catalog 估算大小 {estimated} bytes 超过 memory_limit={limit} bytes"
            )
        import pyarrow as pa

        batches = []
        decoded_bytes = 0
        for batch in self.scan():
            decoded_bytes += batch.nbytes
            if decoded_bytes > limit:
                raise NotebookMemoryLimitError(
                    f"实际解码数据 {decoded_bytes} bytes 超过 memory_limit={limit} bytes"
                )
            batches.append(batch)
        return pa.Table.from_batches(batches).to_pandas()

    def to_geopandas(
        self,
        *,
        memory_limit: str | int,
        geometry_column: str,
        crs: str,
    ):
        """使用已验证的 EWKB 几何列和 CRS 构造 GeoDataFrame。"""
        geometry_column = _required_name(geometry_column, "geometry_column")
        crs = _required_name(crs, "crs")
        frame = self.to_pandas(memory_limit=memory_limit)
        if geometry_column not in frame.columns:
            raise NotebookDataRequestError(
                f"geometry_column {geometry_column!r} 不存在于表扫描结果"
            )
        try:
            import geopandas as gpd
        except ImportError as exc:
            raise NotebookDataUnsupportedError(
                "当前 Notebook Kernel 未安装 GeoPandas"
            ) from exc

        geometry = gpd.GeoSeries.from_wkb(frame[geometry_column], crs=crs)
        frame[geometry_column] = geometry
        return gpd.GeoDataFrame(frame, geometry=geometry_column, crs=crs)


@dataclass(frozen=True)
class PostgreSQLSchema:
    name: str
    _client: PostgreSQLEngine = field(repr=False, compare=False)
    _path: dict[str, Any] = field(repr=False, compare=False)

    def tables(self) -> builtins.list[PostgreSQLTable]:
        return self._client.tables(schema=self.name)

    def table(self, name: str) -> PostgreSQLTable:
        return self._client.table(schema=self.name, name=name)


class PostgreSQLEngine:
    """PostgreSQL 原生术语的只读目录客户端。"""

    def __init__(self, *, engine_id: int, descriptor: dict[str, Any], timeout: float) -> None:
        if timeout <= 0:
            raise NotebookEngineCatalogRequestError("timeout 必须大于 0")
        self.engine_id = engine_id
        self.descriptor = dict(descriptor)
        self.timeout = float(timeout)
        self._root_path: dict[str, Any] | None = None
        self._schema_paths: dict[str, dict[str, Any]] = {}

    @staticmethod
    def validate_descriptor(descriptor: dict[str, Any]) -> None:
        _validate_native_descriptor(descriptor, "postgresql", "server", (("schema", "branch"), ("table", "leaf")))

    def schemas(self) -> builtins.list[PostgreSQLSchema]:
        deadline = time.monotonic() + self.timeout
        return self._schemas(deadline)

    def tables(self, *, schema: str = "public") -> builtins.list[PostgreSQLTable]:
        schema = _required_name(schema, "schema")
        deadline = time.monotonic() + self.timeout
        schema_path = self._schema_paths.get(schema)
        if schema_path is None:
            self._schemas(deadline)
            schema_path = self._schema_paths.get(schema)
        if schema_path is None:
            raise NotebookEngineCatalogEntryNotFoundError(f"PostgreSQL schema {schema!r} 不存在")
        entries = self._all_children(schema_path, deadline)
        tables: builtins.list[PostgreSQLTable] = []
        for entry in entries:
            if entry["term"] != "table" or entry["role"] != "leaf":
                raise NotebookEngineCatalogControlPlaneError("PostgreSQL Engine Catalog 返回了不符合模型的 table 条目")
            tables.append(
                PostgreSQLTable(
                    name=entry["name"], schema=schema, kind=entry["kind"], _client=self,
                    _path=entry["path"], _facts=entry.get("table") or {},
                )
            )
        return tables

    def table(self, *, schema: str = "public", name: str) -> PostgreSQLTable:
        name = _required_name(name, "name")
        for table_resource in self.tables(schema=schema):
            if table_resource.name == name:
                return table_resource
        raise NotebookEngineCatalogEntryNotFoundError(
            f"PostgreSQL table {schema!r}.{name!r} 不存在"
        )

    def sql(
        self,
        query: str,
        *,
        params: builtins.list[Any] | None = None,
        max_rows: int,
        timeout: int,
    ):
        """执行有显式行数和超时边界的 PostgreSQL 参数化只读 SQL。"""
        if not isinstance(query, str) or not query.strip():
            raise NotebookDataRequestError("query 必须是非空 SQL")
        max_rows = _positive_int(max_rows, "max_rows")
        if max_rows > 1_000_000:
            raise NotebookDataRequestError("max_rows 不能超过 1000000")
        timeout = _positive_int(timeout, "timeout")
        if timeout > 300:
            raise NotebookDataRequestError("timeout 不能超过 300 秒")
        if params is None:
            params = []
        if not isinstance(params, builtins.list):
            raise NotebookDataRequestError("params 必须是 list")
        endpoint, token = _session_endpoint_and_token(_QUERY_ENDPOINT_ENV)
        import pyarrow as pa

        batches = builtins.list(
            _iter_arrow_batches(
                endpoint,
                token,
                timeout=float(timeout),
                payload={
                    "engine_id": self.engine_id,
                    "language": "sql",
                    "query": query,
                    "params": params,
                    "max_rows": max_rows,
                    "timeout": timeout,
                },
            )
        )
        return pa.Table.from_batches(batches).to_pandas()

    def _scan_table(self, path: dict[str, Any], *, batch_size: int, max_rows: int):
        batch_size = _positive_int(batch_size, "batch_size")
        if batch_size > 1_000_000:
            raise NotebookDataRequestError("batch_size 不能超过 1000000")
        if isinstance(max_rows, bool) or not isinstance(max_rows, int) or max_rows < 0:
            raise NotebookDataRequestError("max_rows 必须是非负整数")
        endpoint, token = _session_endpoint_and_token(_TABLE_SCAN_ENDPOINT_ENV)
        return _iter_arrow_batches(
            endpoint,
            token,
            timeout=self.timeout,
            payload={
                "engine_id": self.engine_id,
                "path": path,
                "batch_size": batch_size,
                "max_rows": max_rows,
            },
        )

    def _schemas(self, deadline: float) -> builtins.list[PostgreSQLSchema]:
        root_path = self._root_path
        if root_path is None:
            roots = self._children_page(
                {"version": _CATALOG_PATH_VERSION, "engine_id": self.engine_id, "segments": []},
                deadline,
            )
            if len(roots) != 1 or roots[0]["role"] != "branch" or roots[0]["term"] != "server":
                raise NotebookEngineCatalogControlPlaneError("PostgreSQL Engine Catalog 未返回唯一 server root")
            root_path = roots[0]["path"]
            self._root_path = root_path
        entries = self._all_children(root_path, deadline)
        schemas: builtins.list[PostgreSQLSchema] = []
        for entry in entries:
            if entry["term"] != "schema" or entry["role"] != "branch":
                raise NotebookEngineCatalogControlPlaneError("PostgreSQL Engine Catalog 返回了不符合模型的 schema 条目")
            self._schema_paths[entry["name"]] = entry["path"]
            schemas.append(PostgreSQLSchema(name=entry["name"], _client=self, _path=entry["path"]))
        return schemas

    def _all_children(
        self, path: dict[str, Any], deadline: float
    ) -> builtins.list[dict[str, Any]]:
        entries: builtins.list[dict[str, Any]] = []
        offset = 0
        while True:
            page = self._children_page(path, deadline, offset=offset)
            entries.extend(page)
            if len(page) < _CATALOG_PAGE_SIZE:
                return entries
            offset += len(page)

    def _children_page(
        self, path: dict[str, Any], deadline: float, *, offset: int = 0
    ) -> builtins.list[dict[str, Any]]:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise NotebookEngineCatalogTimeoutError("Notebook Engine Catalog 请求超时")
        endpoint, token = _session_endpoint_and_token(_CATALOG_ENDPOINT_ENV)
        response = _request(
            "POST",
            endpoint,
            token,
            timeout=remaining,
            json={
                "engine_id": self.engine_id,
                "path": path,
                "options": {
                    "recursive": False,
                    "limit": _CATALOG_PAGE_SIZE,
                    "offset": offset,
                },
            },
        )
        if response.status_code != httpx.codes.OK:
            _raise_catalog_http_error(response)
        try:
            payload = response.json()
        except ValueError as exc:
            raise NotebookEngineCatalogControlPlaneError("Notebook Engine Catalog 返回了无效 JSON") from exc
        nodes = payload.get("nodes") if isinstance(payload, dict) else None
        if not isinstance(nodes, builtins.list):
            raise NotebookEngineCatalogControlPlaneError("Notebook Engine Catalog 返回了无效节点列表")
        return [_validate_catalog_entry(node, self.engine_id) for node in nodes]


class _NativeEngineCatalog:
    engine_type = ""
    root_term = ""
    model_levels: tuple[tuple[str, str], ...] = ()

    def __init__(self, *, engine_id: int, descriptor: dict[str, Any], timeout: float) -> None:
        if timeout <= 0:
            raise NotebookEngineCatalogRequestError("timeout 必须大于 0")
        self.engine_id = engine_id
        self.descriptor = dict(descriptor)
        self.timeout = float(timeout)
        self._root_path: dict[str, Any] | None = None
        self._branch_paths: dict[tuple[str, ...], dict[str, Any]] = {}

    @classmethod
    def validate_descriptor(cls, descriptor: dict[str, Any]) -> None:
        _validate_native_descriptor(descriptor, cls.engine_type, cls.root_term, cls.model_levels)

    def _root(self, deadline: float) -> dict[str, Any]:
        if self._root_path is None:
            roots = self._children_page(
                {"version": _CATALOG_PATH_VERSION, "engine_id": self.engine_id, "segments": []},
                deadline,
            )
            if len(roots) != 1 or roots[0]["role"] != "branch" or roots[0]["term"] != self.root_term:
                raise NotebookEngineCatalogControlPlaneError(
                    f"{self.engine_type} Engine Catalog 未返回唯一 {self.root_term} root"
                )
            self._root_path = roots[0]["path"]
        return self._root_path

    def _branches(
        self,
        parent: dict[str, Any],
        *,
        term: str,
        deadline: float,
        cache_prefix: tuple[str, ...] = (),
    ) -> builtins.list[dict[str, Any]]:
        entries = self._all_children(parent, deadline)
        for entry in entries:
            if entry["term"] != term or entry["role"] != "branch":
                raise NotebookEngineCatalogControlPlaneError(
                    f"{self.engine_type} Engine Catalog 返回了不符合模型的 {term} 条目"
                )
            self._branch_paths[(*cache_prefix, entry["name"])] = entry["path"]
        return entries

    def _branch(
        self,
        name: str,
        *,
        parent: dict[str, Any],
        term: str,
        deadline: float,
        cache_prefix: tuple[str, ...] = (),
    ) -> dict[str, Any]:
        key = (*cache_prefix, name)
        path = self._branch_paths.get(key)
        if path is None:
            self._branches(parent, term=term, deadline=deadline, cache_prefix=cache_prefix)
            path = self._branch_paths.get(key)
        if path is None:
            raise NotebookEngineCatalogEntryNotFoundError(f"{self.engine_type} {term} {name!r} 不存在")
        return path

    def _leaves(
        self, parent: dict[str, Any], *, term: str, deadline: float
    ) -> builtins.list[dict[str, Any]]:
        entries = self._all_children(parent, deadline)
        for entry in entries:
            if entry["term"] != term or entry["role"] != "leaf":
                raise NotebookEngineCatalogControlPlaneError(
                    f"{self.engine_type} Engine Catalog 返回了不符合模型的 {term} 条目"
                )
        return entries

    def _all_children(
        self, path: dict[str, Any], deadline: float
    ) -> builtins.list[dict[str, Any]]:
        entries: builtins.list[dict[str, Any]] = []
        offset = 0
        while True:
            page = self._children_page(path, deadline, offset=offset)
            entries.extend(page)
            if len(page) < _CATALOG_PAGE_SIZE:
                return entries
            offset += len(page)

    def _children_page(
        self, path: dict[str, Any], deadline: float, *, offset: int = 0
    ) -> builtins.list[dict[str, Any]]:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise NotebookEngineCatalogTimeoutError("Notebook Engine Catalog 请求超时")
        endpoint, token = _session_endpoint_and_token(_CATALOG_ENDPOINT_ENV)
        response = _request(
            "POST",
            endpoint,
            token,
            timeout=remaining,
            json={
                "engine_id": self.engine_id,
                "path": path,
                "options": {
                    "recursive": False,
                    "limit": _CATALOG_PAGE_SIZE,
                    "offset": offset,
                },
            },
        )
        if response.status_code != httpx.codes.OK:
            _raise_catalog_http_error(response)
        try:
            payload = response.json()
        except ValueError as exc:
            raise NotebookEngineCatalogControlPlaneError("Notebook Engine Catalog 返回了无效 JSON") from exc
        nodes = payload.get("nodes") if isinstance(payload, dict) else None
        if not isinstance(nodes, builtins.list):
            raise NotebookEngineCatalogControlPlaneError("Notebook Engine Catalog 返回了无效节点列表")
        return [_validate_catalog_entry(node, self.engine_id) for node in nodes]

    def _open_content(self, path: dict[str, Any], *, byte_range: dict[str, int] | None = None):
        endpoint, token = _session_endpoint_and_token(_CONTENT_READ_ENDPOINT_ENV)
        payload: dict[str, Any] = {"engine_id": self.engine_id, "path": path}
        if byte_range is not None:
            payload["range"] = byte_range
        return _open_data_stream(
            endpoint,
            token,
            payload=payload,
            connect_timeout=self.timeout,
            expected_content_type="application/octet-stream",
        )


@dataclass(frozen=True)
class DatabaseTable:
    name: str
    database: str
    kind: str
    _client: _DatabaseTabularEngine = field(repr=False, compare=False)
    _path: dict[str, Any] = field(repr=False, compare=False)
    _facts: dict[str, Any] = field(default_factory=dict, repr=False, compare=False)

    def scan(self, *, batch_size: int = 65536):
        """按扫描开始时的引擎一致性边界流式返回 pyarrow.RecordBatch。"""
        return self._client._scan_table(self._path, batch_size=batch_size, max_rows=0)

    def head(self, max_rows: int = 100):
        max_rows = _positive_int(max_rows, "max_rows")
        import pyarrow as pa

        batches = builtins.list(
            self._client._scan_table(self._path, batch_size=min(max_rows, 65536), max_rows=max_rows)
        )
        return pa.Table.from_batches(batches).to_pandas()

    def to_pandas(self, *, memory_limit: str | int):
        return _table_to_pandas(self.scan(), self._facts, memory_limit)


@dataclass(frozen=True)
class DatabaseNamespace:
    name: str
    _client: _DatabaseTabularEngine = field(repr=False, compare=False)
    _path: dict[str, Any] = field(repr=False, compare=False)

    def tables(self) -> builtins.list[DatabaseTable]:
        return self._client.tables(database=self.name)

    def table(self, name: str) -> DatabaseTable:
        return self._client.table(database=self.name, name=name)


class _DatabaseTabularEngine(_NativeEngineCatalog):
    root_term = "server"
    model_levels = (("database", "branch"), ("table", "leaf"))

    def databases(self) -> builtins.list[DatabaseNamespace]:
        deadline = time.monotonic() + self.timeout
        entries = self._branches(self._root(deadline), term="database", deadline=deadline)
        return [DatabaseNamespace(name=entry["name"], _client=self, _path=entry["path"]) for entry in entries]

    def tables(self, *, database: str) -> builtins.list[DatabaseTable]:
        database = _required_name(database, "database")
        deadline = time.monotonic() + self.timeout
        path = self._branch(
            database, parent=self._root(deadline), term="database", deadline=deadline
        )
        return [
            DatabaseTable(
                name=entry["name"], database=database, kind=entry["kind"], _client=self,
                _path=entry["path"], _facts=entry.get("table") or {},
            )
            for entry in self._leaves(path, term="table", deadline=deadline)
        ]

    def table(self, *, database: str, name: str) -> DatabaseTable:
        name = _required_name(name, "name")
        for resource in self.tables(database=database):
            if resource.name == name:
                return resource
        raise NotebookEngineCatalogEntryNotFoundError(
            f"{self.engine_type} table {database!r}.{name!r} 不存在"
        )

    def sql(
        self,
        query: str,
        *,
        params: builtins.list[Any] | None = None,
        max_rows: int,
        timeout: int,
    ):
        return _query_dataframe(
            self.engine_id, "sql", query, params=params, max_rows=max_rows, timeout=timeout
        )

    def _scan_table(self, path: dict[str, Any], *, batch_size: int, max_rows: int):
        return _scan_table_batches(
            self.engine_id, path, batch_size=batch_size, max_rows=max_rows, timeout=self.timeout
        )


class MySQLEngine(_DatabaseTabularEngine):
    engine_type = "mysql"


class DorisEngine(_DatabaseTabularEngine):
    engine_type = "doris"


class ClickHouseEngine(_DatabaseTabularEngine):
    engine_type = "clickhouse"


class SparkSQLEngine(_DatabaseTabularEngine):
    engine_type = "spark"


@dataclass(frozen=True)
class MongoDBCollection:
    name: str
    database: str
    kind: str
    _client: MongoDBEngine = field(repr=False, compare=False)
    _path: dict[str, Any] = field(repr=False, compare=False)
    _facts: dict[str, Any] = field(default_factory=dict, repr=False, compare=False)

    def scan(self, *, batch_size: int = 65536):
        """以 MongoDB Cursor 语义逐条返回规范化 dict 文档。"""
        return self._client._scan_collection(
            self._path, batch_size=batch_size, max_rows=0
        )

    def head(self, max_rows: int = 100):
        max_rows = _positive_int(max_rows, "max_rows")
        import pandas as pd

        records = builtins.list(
            self._client._scan_collection(
                self._path, batch_size=min(max_rows, 65536), max_rows=max_rows
            )
        )
        return pd.DataFrame.from_records(records)

    def to_pandas(self, *, memory_limit: str | int):
        limit = _memory_limit_bytes(memory_limit)
        records = []
        decoded_bytes = 0
        for record in self.scan():
            decoded_bytes += len(json.dumps(record, ensure_ascii=False).encode("utf-8"))
            if decoded_bytes > limit:
                raise NotebookMemoryLimitError(
                    f"实际解码数据 {decoded_bytes} bytes 超过 memory_limit={limit} bytes"
                )
            records.append(record)
        import pandas as pd

        return pd.DataFrame.from_records(records)


@dataclass(frozen=True)
class MongoDBDatabase:
    name: str
    _client: MongoDBEngine = field(repr=False, compare=False)
    _path: dict[str, Any] = field(repr=False, compare=False)

    def collections(self) -> builtins.list[MongoDBCollection]:
        return self._client.collections(database=self.name)

    def collection(self, name: str) -> MongoDBCollection:
        return self._client.collection(database=self.name, name=name)


class MongoDBEngine(_NativeEngineCatalog):
    engine_type = "mongodb"
    root_term = "server"
    model_levels = (("database", "branch"), ("collection", "leaf"))

    def databases(self) -> builtins.list[MongoDBDatabase]:
        deadline = time.monotonic() + self.timeout
        entries = self._branches(self._root(deadline), term="database", deadline=deadline)
        return [MongoDBDatabase(name=entry["name"], _client=self, _path=entry["path"]) for entry in entries]

    def collections(self, *, database: str) -> builtins.list[MongoDBCollection]:
        database = _required_name(database, "database")
        deadline = time.monotonic() + self.timeout
        path = self._branch(database, parent=self._root(deadline), term="database", deadline=deadline)
        return [
            MongoDBCollection(
                name=entry["name"], database=database, kind=entry["kind"], _client=self,
                _path=entry["path"], _facts=entry.get("collection") or {},
            )
            for entry in self._leaves(path, term="collection", deadline=deadline)
        ]

    def collection(self, *, database: str, name: str) -> MongoDBCollection:
        name = _required_name(name, "name")
        for resource in self.collections(database=database):
            if resource.name == name:
                return resource
        raise NotebookEngineCatalogEntryNotFoundError(
            f"MongoDB collection {database!r}.{name!r} 不存在"
        )

    def mql(self, query: str, *, max_rows: int, timeout: int):
        return _query_dataframe(
            self.engine_id, "mql", query, params=None, max_rows=max_rows, timeout=timeout
        )

    def _scan_collection(self, path: dict[str, Any], *, batch_size: int, max_rows: int):
        batch_size = _positive_int(batch_size, "batch_size")
        if batch_size > 1_000_000:
            raise NotebookDataRequestError("batch_size 不能超过 1000000")
        if isinstance(max_rows, bool) or not isinstance(max_rows, int) or max_rows < 0:
            raise NotebookDataRequestError("max_rows 必须是非负整数")
        endpoint, token = _session_endpoint_and_token(_RECORD_SCAN_ENDPOINT_ENV)
        for batch in _iter_arrow_batches(
            endpoint,
            token,
            timeout=self.timeout,
            payload={
                "engine_id": self.engine_id,
                "path": path,
                "batch_size": batch_size,
                "max_rows": max_rows,
            },
        ):
            documents = batch.column("document")
            for index in range(len(documents)):
                value = documents[index].as_py()
                if isinstance(value, str):
                    try:
                        value = json.loads(value)
                    except json.JSONDecodeError as exc:
                        raise NotebookDataProviderError(
                            "MongoDB record stream 返回了无效 document JSON"
                        ) from exc
                if not isinstance(value, dict):
                    raise NotebookDataProviderError("MongoDB record stream 返回了无效 document")
                yield value


@dataclass(frozen=True)
class Neo4jGraph:
    name: str
    database: str
    kind: str
    _client: Neo4jEngine = field(repr=False, compare=False)
    _path: dict[str, Any] = field(repr=False, compare=False)

    def sample(self, *, limit: int = 50, timeout: int = 30) -> dict[str, Any]:
        return self._client._sample_graph(self._path, limit=limit, timeout=timeout)


@dataclass(frozen=True)
class Neo4jDatabase:
    name: str
    _client: Neo4jEngine = field(repr=False, compare=False)
    _path: dict[str, Any] = field(repr=False, compare=False)

    def graphs(self) -> builtins.list[Neo4jGraph]:
        return self._client.graphs(database=self.name)

    def graph(self, name: str) -> Neo4jGraph:
        return self._client.graph(database=self.name, name=name)


class Neo4jEngine(_NativeEngineCatalog):
    engine_type = "neo4j"
    root_term = "server"
    model_levels = (("database", "branch"), ("graph", "leaf"))

    def databases(self) -> builtins.list[Neo4jDatabase]:
        deadline = time.monotonic() + self.timeout
        entries = self._branches(self._root(deadline), term="database", deadline=deadline)
        return [Neo4jDatabase(name=entry["name"], _client=self, _path=entry["path"]) for entry in entries]

    def graphs(self, *, database: str) -> builtins.list[Neo4jGraph]:
        database = _required_name(database, "database")
        deadline = time.monotonic() + self.timeout
        path = self._branch(database, parent=self._root(deadline), term="database", deadline=deadline)
        return [
            Neo4jGraph(
                name=entry["name"], database=database, kind=entry["kind"], _client=self,
                _path=entry["path"],
            )
            for entry in self._leaves(path, term="graph", deadline=deadline)
        ]

    def graph(self, *, database: str, name: str) -> Neo4jGraph:
        name = _required_name(name, "name")
        for resource in self.graphs(database=database):
            if resource.name == name:
                return resource
        raise NotebookEngineCatalogEntryNotFoundError(f"Neo4j graph {database!r}.{name!r} 不存在")

    def cypher(self, query: str, *, max_rows: int, timeout: int) -> dict[str, Any]:
        query, max_rows, timeout = _bounded_query_arguments(
            query, max_rows=max_rows, timeout=timeout
        )
        endpoint, token = _session_endpoint_and_token(_GRAPH_QUERY_ENDPOINT_ENV)
        return _request_data_json(
            endpoint,
            token,
            timeout=float(timeout),
            payload={
                "engine_id": self.engine_id,
                "query": query,
                "max_rows": max_rows,
                "timeout": timeout,
            },
        )

    def _sample_graph(self, path: dict[str, Any], *, limit: int, timeout: int) -> dict[str, Any]:
        limit = _positive_int(limit, "limit")
        if limit > 10_000:
            raise NotebookDataRequestError("limit 不能超过 10000")
        timeout = _query_timeout(timeout)
        endpoint, token = _session_endpoint_and_token(_GRAPH_SAMPLE_ENDPOINT_ENV)
        return _request_data_json(
            endpoint,
            token,
            timeout=float(timeout),
            payload={
                "engine_id": self.engine_id,
                "path": path,
                "limit": limit,
                "timeout": timeout,
            },
        )


@dataclass(frozen=True)
class ObjectStorageObject:
    name: str
    bucket: str
    prefix: str
    kind: str
    _client: _ObjectStorageEngine = field(repr=False, compare=False)
    _path: dict[str, Any] = field(repr=False, compare=False)
    _facts: dict[str, Any] = field(default_factory=dict, repr=False, compare=False)

    def open(self):
        """返回拥有 HTTP 与 Provider 生命周期的可关闭二进制流。"""
        return io.BufferedReader(self._client._open_content(self._path))

    def read_range(self, *, offset: int, length: int) -> bytes:
        offset, length = _byte_range(offset, length)
        with io.BufferedReader(
            self._client._open_content(
                self._path, byte_range={"offset": offset, "length": length}
            )
        ) as stream:
            return stream.read()


@dataclass(frozen=True)
class ObjectStorageBucket:
    name: str
    _client: _ObjectStorageEngine = field(repr=False, compare=False)
    _path: dict[str, Any] = field(repr=False, compare=False)

    def objects(self, *, prefix: str = "") -> builtins.list[ObjectStorageObject]:
        return self._client.objects(bucket=self.name, prefix=prefix)

    def object(self, name: str, *, prefix: str = "") -> ObjectStorageObject:
        return self._client.object(bucket=self.name, prefix=prefix, name=name)


class _ObjectStorageEngine(_NativeEngineCatalog):
    root_term = "service"
    model_levels = (("bucket", "branch"), ("prefix", "branch"), ("object", "leaf"))

    def buckets(self) -> builtins.list[ObjectStorageBucket]:
        deadline = time.monotonic() + self.timeout
        entries = self._branches(self._root(deadline), term="bucket", deadline=deadline)
        return [ObjectStorageBucket(name=entry["name"], _client=self, _path=entry["path"]) for entry in entries]

    def objects(self, *, bucket: str, prefix: str = "") -> builtins.list[ObjectStorageObject]:
        bucket = _required_name(bucket, "bucket")
        deadline = time.monotonic() + self.timeout
        parent = self._branch(bucket, parent=self._root(deadline), term="bucket", deadline=deadline)
        prefix_parts = _path_parts(prefix, "prefix")
        resolved: builtins.list[str] = []
        for part in prefix_parts:
            parent = self._branch(
                part, parent=parent, term="prefix", deadline=deadline,
                cache_prefix=(bucket, *resolved),
            )
            resolved.append(part)
        entries = self._all_children(parent, deadline)
        objects = []
        for entry in entries:
            if entry["term"] == "prefix" and entry["role"] == "branch":
                self._branch_paths[(bucket, *resolved, entry["name"])] = entry["path"]
                continue
            if entry["term"] != "object" or entry["role"] != "leaf":
                raise NotebookEngineCatalogControlPlaneError(
                    f"{self.engine_type} Engine Catalog 返回了不符合模型的 object 条目"
                )
            objects.append(ObjectStorageObject(
                name=entry["name"], bucket=bucket, prefix="/".join(resolved), kind=entry["kind"],
                _client=self, _path=entry["path"], _facts=entry.get("storage") or {},
            ))
        return objects

    def object(self, *, bucket: str, name: str, prefix: str = "") -> ObjectStorageObject:
        name = _required_name(name, "name")
        for resource in self.objects(bucket=bucket, prefix=prefix):
            if resource.name == name:
                return resource
        raise NotebookEngineCatalogEntryNotFoundError(
            f"{self.engine_type} object {bucket!r}/{prefix!r}/{name!r} 不存在"
        )


class MinIOEngine(_ObjectStorageEngine):
    engine_type = "minio"


class S3Engine(_ObjectStorageEngine):
    engine_type = "s3"


@dataclass(frozen=True)
class NFSFile:
    name: str
    directory: str
    kind: str
    _client: NFSEngine = field(repr=False, compare=False)
    _path: dict[str, Any] = field(repr=False, compare=False)
    _facts: dict[str, Any] = field(default_factory=dict, repr=False, compare=False)

    def open(self):
        """返回拥有 HTTP 与 Provider 生命周期的可关闭二进制流。"""
        return io.BufferedReader(self._client._open_content(self._path))

    def read_range(self, *, offset: int, length: int) -> bytes:
        offset, length = _byte_range(offset, length)
        with io.BufferedReader(
            self._client._open_content(
                self._path, byte_range={"offset": offset, "length": length}
            )
        ) as stream:
            return stream.read()


@dataclass(frozen=True)
class NFSDirectory:
    name: str
    path: str
    _client: NFSEngine = field(repr=False, compare=False)
    _path: dict[str, Any] = field(repr=False, compare=False)

    def directories(self) -> builtins.list[NFSDirectory]:
        return self._client.directories(path=self.path)

    def files(self) -> builtins.list[NFSFile]:
        return self._client.files(path=self.path)


class NFSEngine(_NativeEngineCatalog):
    engine_type = "nfs"
    root_term = "root"
    model_levels = (("directory", "branch"), ("file", "leaf"))

    def _directory_path(self, path: str, deadline: float) -> dict[str, Any]:
        parent = self._root(deadline)
        resolved: builtins.list[str] = []
        for part in _path_parts(path, "path"):
            parent = self._branch(
                part, parent=parent, term="directory", deadline=deadline,
                cache_prefix=tuple(resolved),
            )
            resolved.append(part)
        return parent

    def directories(self, *, path: str = "") -> builtins.list[NFSDirectory]:
        deadline = time.monotonic() + self.timeout
        parts = _path_parts(path, "path")
        parent = self._directory_path(path, deadline)
        entries = self._all_children(parent, deadline)
        result = []
        for entry in entries:
            if entry["term"] == "file" and entry["role"] == "leaf":
                continue
            if entry["term"] != "directory" or entry["role"] != "branch":
                raise NotebookEngineCatalogControlPlaneError("NFS Engine Catalog 返回了不符合模型的 directory 条目")
            child_parts = [*parts, entry["name"]]
            self._branch_paths[tuple(child_parts)] = entry["path"]
            result.append(NFSDirectory(
                name=entry["name"], path="/".join(child_parts), _client=self, _path=entry["path"]
            ))
        return result

    def files(self, *, path: str = "") -> builtins.list[NFSFile]:
        deadline = time.monotonic() + self.timeout
        parent = self._directory_path(path, deadline)
        result = []
        for entry in self._all_children(parent, deadline):
            if entry["term"] == "directory" and entry["role"] == "branch":
                continue
            if entry["term"] != "file" or entry["role"] != "leaf":
                raise NotebookEngineCatalogControlPlaneError("NFS Engine Catalog 返回了不符合模型的 file 条目")
            result.append(NFSFile(
                name=entry["name"], directory="/".join(_path_parts(path, "path")), kind=entry["kind"],
                _client=self, _path=entry["path"], _facts=entry.get("storage") or {},
            ))
        return result

    def file(self, *, path: str = "", name: str) -> NFSFile:
        name = _required_name(name, "name")
        for resource in self.files(path=path):
            if resource.name == name:
                return resource
        raise NotebookEngineCatalogEntryNotFoundError(f"NFS file {path!r}/{name!r} 不存在")


@dataclass(frozen=True)
class KafkaTopic:
    name: str
    kind: str
    _client: KafkaEngine = field(repr=False, compare=False)
    _path: dict[str, Any] = field(repr=False, compare=False)

    def stream(
        self,
        *,
        initial_position: str,
        positions: dict[str | int, int] | None = None,
        batch_size: int = 1000,
        poll_timeout: int = 5,
    ):
        initial_position = str(initial_position).strip().lower()
        if initial_position not in {"earliest", "latest"}:
            raise NotebookDataRequestError("initial_position 必须是 earliest 或 latest")
        batch_size = _positive_int(batch_size, "batch_size")
        if batch_size > 10_000:
            raise NotebookDataRequestError("batch_size 不能超过 10000")
        poll_timeout = _positive_int(poll_timeout, "poll_timeout")
        if poll_timeout > 60:
            raise NotebookDataRequestError("poll_timeout 不能超过 60 秒")
        normalized_positions: dict[str, int] = {}
        for partition, next_offset in (positions or {}).items():
            partition_text = str(partition)
            if not partition_text.isdigit() or isinstance(next_offset, bool) or not isinstance(next_offset, int) or next_offset < 0:
                raise NotebookDataRequestError("positions 必须是 partition 到非负 next_offset 的映射")
            normalized_positions[partition_text] = next_offset
        endpoint, token = _session_endpoint_and_token(_CHANGE_STREAM_ENDPOINT_ENV)
        return _iter_change_records(
            endpoint,
            token,
            payload={
                "engine_id": self._client.engine_id,
                "path": self._path,
                "initial_position": initial_position,
                "positions": normalized_positions,
                "batch_size": batch_size,
                "poll_timeout": poll_timeout,
            },
            connect_timeout=self._client.timeout,
        )


class KafkaEngine(_NativeEngineCatalog):
    engine_type = "kafka"
    root_term = "service"
    model_levels = (("topic", "leaf"),)

    def topics(self) -> builtins.list[KafkaTopic]:
        deadline = time.monotonic() + self.timeout
        return [
            KafkaTopic(name=entry["name"], kind=entry["kind"], _client=self, _path=entry["path"])
            for entry in self._leaves(self._root(deadline), term="topic", deadline=deadline)
        ]

    def topic(self, name: str) -> KafkaTopic:
        name = _required_name(name, "name")
        for resource in self.topics():
            if resource.name == name:
                return resource
        raise NotebookEngineCatalogEntryNotFoundError(f"Kafka topic {name!r} 不存在")


def _session_endpoint_and_token(endpoint_env: str) -> tuple[str, str]:
    endpoint = os.environ.get(endpoint_env, "").strip()
    token = os.environ.get(_CAPABILITY_ENV, "").strip()
    parsed = urlsplit(endpoint)
    if (
        parsed.scheme not in {"http", "https"}
        or not parsed.netloc
        or parsed.username is not None
        or parsed.password is not None
        or parsed.query
        or parsed.fragment
        or not token.startswith(_CAPABILITY_PREFIX)
    ):
        raise NotebookSessionUnavailableError("当前 Python process 不属于有效的 ADDP Notebook 会话")
    return endpoint, token


def _request(method: str, endpoint: str, token: str, *, timeout: float, json: Any = None) -> httpx.Response:
    try:
        options: dict[str, Any] = {"timeout": timeout, "trust_env": False}
        if _transport is not None:
            options["transport"] = _transport
        with httpx.Client(**options) as http_client:
            return http_client.request(
                method,
                endpoint,
                headers={"Authorization": f"Bearer {token}", "Cache-Control": "no-store"},
                json=json,
            )
    except httpx.TimeoutException as exc:
        raise NotebookEngineCatalogTimeoutError("Notebook Engine Catalog 请求超时") from exc
    except httpx.HTTPError as exc:
        if method == "GET":
            raise NotebookEngineDiscoveryError("无法连接 ADDP Notebook Engine 发现接口") from exc
        raise NotebookEngineCatalogControlPlaneError("无法连接 ADDP Notebook Engine Catalog 接口") from exc


class _IteratorReader(io.RawIOBase):
    def __init__(self, chunks) -> None:
        self._chunks = iter(chunks)
        self._pending = memoryview(b"")

    def readable(self) -> bool:
        return True

    def readinto(self, target) -> int:
        view = memoryview(target)
        written = 0
        while written < len(view):
            if len(self._pending) == 0:
                try:
                    self._pending = memoryview(next(self._chunks))
                except StopIteration:
                    break
            count = min(len(view) - written, len(self._pending))
            view[written : written + count] = self._pending[:count]
            self._pending = self._pending[count:]
            written += count
        return written


class _HTTPStreamReader(_IteratorReader):
    def __init__(self, client: httpx.Client, response: httpx.Response) -> None:
        self._client = client
        self._response = response
        super().__init__(response.iter_bytes())

    def close(self) -> None:
        if not self.closed:
            self._response.close()
            self._client.close()
        super().close()

    def readinto(self, buffer) -> int:
        try:
            return super().readinto(buffer)
        except httpx.TimeoutException as exc:
            raise NotebookDataTimeoutError("Notebook 内容流读取超时") from exc
        except httpx.HTTPError as exc:
            raise NotebookDataControlPlaneError("Notebook 内容流中断") from exc


def _stream_client_options(connect_timeout: float) -> dict[str, Any]:
    options: dict[str, Any] = {
        "timeout": httpx.Timeout(connect=connect_timeout, read=None, write=connect_timeout, pool=connect_timeout),
        "trust_env": False,
    }
    if _transport is not None:
        options["transport"] = _transport
    return options


def _open_data_stream(
    endpoint: str,
    token: str,
    *,
    payload: dict[str, Any],
    connect_timeout: float,
    expected_content_type: str,
) -> io.RawIOBase:
    client = httpx.Client(**_stream_client_options(connect_timeout))
    try:
        request = client.build_request(
            "POST",
            endpoint,
            headers={"Authorization": f"Bearer {token}", "Cache-Control": "no-store"},
            json=payload,
        )
        response = client.send(request, stream=True)
        if response.status_code != httpx.codes.OK:
            response.read()
            _raise_data_http_error(response)
        content_type = response.headers.get("Content-Type", "").split(";", 1)[0].strip()
        if content_type != expected_content_type:
            response.close()
            raise NotebookDataControlPlaneError(
                f"Notebook 数据接口未返回 {expected_content_type}"
            )
        return _HTTPStreamReader(client, response)
    except httpx.TimeoutException as exc:
        client.close()
        raise NotebookDataTimeoutError("Notebook 内容流连接超时") from exc
    except httpx.HTTPError as exc:
        client.close()
        raise NotebookDataControlPlaneError("无法连接 ADDP Notebook 内容接口") from exc
    except Exception:
        client.close()
        raise


def _iter_change_records(
    endpoint: str,
    token: str,
    *,
    payload: dict[str, Any],
    connect_timeout: float,
):
    try:
        with httpx.Client(**_stream_client_options(connect_timeout)) as http_client:
            with http_client.stream(
                "POST",
                endpoint,
                headers={"Authorization": f"Bearer {token}", "Cache-Control": "no-store"},
                json=payload,
            ) as response:
                if response.status_code != httpx.codes.OK:
                    response.read()
                    _raise_data_http_error(response)
                content_type = response.headers.get("Content-Type", "").split(";", 1)[0].strip()
                if content_type != "application/x-ndjson":
                    raise NotebookDataControlPlaneError("Notebook change stream 未返回 NDJSON")
                for line in response.iter_lines():
                    if not line:
                        continue
                    try:
                        record = json.loads(line)
                    except json.JSONDecodeError as exc:
                        raise NotebookDataProviderError("Notebook change stream 返回了无效 JSON") from exc
                    if not isinstance(record, dict):
                        raise NotebookDataProviderError("Notebook change stream 返回了无效记录")
                    for field_name in ("key", "value"):
                        value = record.get(field_name)
                        if isinstance(value, str):
                            record[field_name] = base64.b64decode(value)
                    headers = record.get("headers")
                    if isinstance(headers, builtins.list):
                        for header in headers:
                            if isinstance(header, dict) and isinstance(header.get("value"), str):
                                header["value"] = base64.b64decode(header["value"])
                    yield record
    except httpx.TimeoutException as exc:
        raise NotebookDataTimeoutError("Notebook change stream 连接超时") from exc
    except httpx.HTTPError as exc:
        raise NotebookDataControlPlaneError("Notebook change stream 中断") from exc


def _request_data_json(
    endpoint: str, token: str, *, timeout: float, payload: dict[str, Any]
) -> dict[str, Any]:
    try:
        options: dict[str, Any] = {"timeout": timeout, "trust_env": False}
        if _transport is not None:
            options["transport"] = _transport
        with httpx.Client(**options) as http_client:
            response = http_client.post(
                endpoint,
                headers={"Authorization": f"Bearer {token}", "Cache-Control": "no-store"},
                json=payload,
            )
        if response.status_code != httpx.codes.OK:
            _raise_data_http_error(response)
        value = response.json()
        if not isinstance(value, dict):
            raise NotebookDataControlPlaneError("Notebook 数据接口返回了无效 JSON")
        return value
    except httpx.TimeoutException as exc:
        raise NotebookDataTimeoutError("Notebook 数据请求超时") from exc
    except httpx.HTTPError as exc:
        raise NotebookDataControlPlaneError("无法连接 ADDP Notebook 数据接口") from exc
    except ValueError as exc:
        raise NotebookDataControlPlaneError("Notebook 数据接口返回了无效 JSON") from exc


def _query_dataframe(
    engine_id: int,
    language: str,
    query: str,
    *,
    params: builtins.list[Any] | None,
    max_rows: int,
    timeout: int,
):
    query, max_rows, timeout = _bounded_query_arguments(
        query, max_rows=max_rows, timeout=timeout
    )
    if params is None:
        params = []
    if not isinstance(params, builtins.list):
        raise NotebookDataRequestError("params 必须是 list")
    endpoint, token = _session_endpoint_and_token(_QUERY_ENDPOINT_ENV)
    import pyarrow as pa

    batches = builtins.list(
        _iter_arrow_batches(
            endpoint,
            token,
            timeout=float(timeout),
            payload={
                "engine_id": engine_id,
                "language": language,
                "query": query,
                "params": params,
                "max_rows": max_rows,
                "timeout": timeout,
            },
        )
    )
    return pa.Table.from_batches(batches).to_pandas()


def _bounded_query_arguments(query: str, *, max_rows: int, timeout: int) -> tuple[str, int, int]:
    if not isinstance(query, str) or not query.strip():
        raise NotebookDataRequestError("query 必须是非空字符串")
    max_rows = _positive_int(max_rows, "max_rows")
    if max_rows > 1_000_000:
        raise NotebookDataRequestError("max_rows 不能超过 1000000")
    return query, max_rows, _query_timeout(timeout)


def _query_timeout(timeout: int) -> int:
    timeout = _positive_int(timeout, "timeout")
    if timeout > 300:
        raise NotebookDataRequestError("timeout 不能超过 300 秒")
    return timeout


def _scan_table_batches(
    engine_id: int,
    path: dict[str, Any],
    *,
    batch_size: int,
    max_rows: int,
    timeout: float,
):
    batch_size = _positive_int(batch_size, "batch_size")
    if batch_size > 1_000_000:
        raise NotebookDataRequestError("batch_size 不能超过 1000000")
    if isinstance(max_rows, bool) or not isinstance(max_rows, int) or max_rows < 0:
        raise NotebookDataRequestError("max_rows 必须是非负整数")
    endpoint, token = _session_endpoint_and_token(_TABLE_SCAN_ENDPOINT_ENV)
    return _iter_arrow_batches(
        endpoint,
        token,
        timeout=timeout,
        payload={
            "engine_id": engine_id,
            "path": path,
            "batch_size": batch_size,
            "max_rows": max_rows,
        },
    )


def _table_to_pandas(batches, facts: dict[str, Any], memory_limit: str | int):
    limit = _memory_limit_bytes(memory_limit)
    estimated = facts.get("size_bytes")
    if isinstance(estimated, int) and not isinstance(estimated, bool) and estimated > limit:
        raise NotebookMemoryLimitError(
            f"表的 Engine Catalog 估算大小 {estimated} bytes 超过 memory_limit={limit} bytes"
        )
    import pyarrow as pa

    buffered = []
    decoded_bytes = 0
    for batch in batches:
        decoded_bytes += batch.nbytes
        if decoded_bytes > limit:
            raise NotebookMemoryLimitError(
                f"实际解码数据 {decoded_bytes} bytes 超过 memory_limit={limit} bytes"
            )
        buffered.append(batch)
    return pa.Table.from_batches(buffered).to_pandas()


def _byte_range(offset: int, length: int) -> tuple[int, int]:
    if isinstance(offset, bool) or not isinstance(offset, int) or offset < 0:
        raise NotebookDataRequestError("offset 必须是非负整数")
    length = _positive_int(length, "length")
    return offset, length


def _iter_arrow_batches(endpoint: str, token: str, *, timeout: float, payload: dict[str, Any]):
    try:
        import pyarrow as pa
    except ImportError as exc:
        raise NotebookDataUnsupportedError("当前 Notebook Runtime 未安装 pyarrow") from exc
    options: dict[str, Any] = {"timeout": timeout, "trust_env": False}
    if _transport is not None:
        options["transport"] = _transport
    try:
        with httpx.Client(**options) as http_client:
            with http_client.stream(
                "POST",
                endpoint,
                headers={"Authorization": f"Bearer {token}", "Cache-Control": "no-store"},
                json=payload,
            ) as response:
                if response.status_code != httpx.codes.OK:
                    response.read()
                    _raise_data_http_error(response)
                content_type = response.headers.get("Content-Type", "").split(";", 1)[0].strip()
                if content_type != "application/vnd.apache.arrow.stream":
                    raise NotebookDataControlPlaneError("Notebook 数据接口未返回 Arrow IPC 流")
                reader = pa.ipc.open_stream(io.BufferedReader(_IteratorReader(response.iter_bytes())))
                for batch in reader:
                    yield batch
    except httpx.TimeoutException as exc:
        raise NotebookDataTimeoutError("Notebook 数据读取超时") from exc
    except httpx.HTTPError as exc:
        raise NotebookDataControlPlaneError("无法连接 ADDP Notebook 数据接口") from exc
    except (pa.ArrowInvalid, pa.ArrowIOError) as exc:
        raise NotebookDataProviderError("Notebook Arrow 数据流中断或无效") from exc


def _raise_data_http_error(response: httpx.Response) -> None:
    try:
        payload = response.json()
    except ValueError:
        payload = {}
    code = payload.get("error_code") if isinstance(payload, dict) else None
    mapping: dict[str, type[NotebookDataError]] = {
        "table_scan_request_invalid": NotebookDataRequestError,
        "record_scan_request_invalid": NotebookDataRequestError,
        "graph_request_invalid": NotebookDataRequestError,
        "content_read_request_invalid": NotebookDataRequestError,
        "change_stream_request_invalid": NotebookDataRequestError,
        "notebook_data_forbidden": NotebookDataForbiddenError,
        "table_scan_unsupported": NotebookDataUnsupportedError,
        "record_scan_unsupported": NotebookDataUnsupportedError,
        "graph_operation_unsupported": NotebookDataUnsupportedError,
        "content_read_unsupported": NotebookDataUnsupportedError,
        "change_stream_unsupported": NotebookDataUnsupportedError,
        "table_scan_failed": NotebookDataProviderError,
        "notebook_data_provider_failed": NotebookDataProviderError,
        "table_scan_timeout": NotebookDataTimeoutError,
        "record_scan_timeout": NotebookDataTimeoutError,
        "graph_timeout": NotebookDataTimeoutError,
        "content_read_timeout": NotebookDataTimeoutError,
        "change_stream_timeout": NotebookDataTimeoutError,
        "query_request_invalid": NotebookDataRequestError,
        "query_unsupported": NotebookDataUnsupportedError,
        "query_failed": NotebookDataProviderError,
        "query_timeout": NotebookDataTimeoutError,
        "engine_unavailable": NotebookEngineUnavailableError,
        "execution_authorization_conflict": NotebookDataControlPlaneError,
    }
    if response.status_code == httpx.codes.UNAUTHORIZED and code == "notebook_session_unavailable":
        raise NotebookSessionUnavailableError("Notebook 会话已失效，请重新打开")
    error_type = mapping.get(code, NotebookDataControlPlaneError)
    raise error_type(f"Notebook 数据接口返回 HTTP {response.status_code} ({code or 'unknown'})")


def _positive_int(value: int, field_name: str) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or value <= 0:
        raise NotebookDataRequestError(f"{field_name} 必须是正整数")
    return value


def _memory_limit_bytes(value: str | int) -> int:
    if isinstance(value, int) and not isinstance(value, bool):
        return _positive_int(value, "memory_limit")
    if not isinstance(value, str):
        raise NotebookDataRequestError("memory_limit 必须是正整数 bytes 或 KiB/MiB/GiB 字符串")
    match = re.fullmatch(r"([1-9][0-9]*)\s*(B|KiB|MiB|GiB)", value.strip(), re.IGNORECASE)
    if match is None:
        raise NotebookDataRequestError("memory_limit 必须是正整数 bytes 或 KiB/MiB/GiB 字符串")
    multiplier = {"b": 1, "kib": 1 << 10, "mib": 1 << 20, "gib": 1 << 30}[match.group(2).lower()]
    return int(match.group(1)) * multiplier


def _validate_native_descriptor(
    descriptor: dict[str, Any],
    engine_type: str,
    root_term: str,
    expected_levels: tuple[tuple[str, str], ...],
) -> None:
    capabilities = descriptor.get("capabilities")
    if not isinstance(capabilities, dict) or capabilities.get("engine_type") != engine_type:
        raise NotebookEngineCatalogUnsupportedError(
            f"{engine_type} Engine capabilities 与 engine_type 不一致"
        )
    storage = capabilities.get("storage") if isinstance(capabilities, dict) else None
    catalog = storage.get("catalog") if isinstance(storage, dict) else None
    model = storage.get("catalog_model") if isinstance(storage, dict) else None
    levels = model.get("levels") if isinstance(model, dict) else None
    if (
        not isinstance(catalog, dict)
        or catalog.get("supported") is not True
        or not isinstance(model, dict)
        or model.get("path_version") != _CATALOG_PATH_VERSION
        or model.get("root_term") != root_term
        or not isinstance(levels, builtins.list)
        or len(levels) != len(expected_levels)
        or any(
            not isinstance(level, dict)
            or level.get("term") != expected[0]
            or level.get("role") != expected[1]
            for level, expected in zip(levels, expected_levels)
        )
    ):
        raise NotebookEngineCatalogUnsupportedError(f"{engine_type} Engine 未声明兼容的 Engine Catalog Model")


def _validate_catalog_entry(value: Any, engine_id: int) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise NotebookEngineCatalogControlPlaneError("Notebook Engine Catalog 返回了无效条目")
    path = value.get("path")
    segments = path.get("segments") if isinstance(path, dict) else None
    if (
        not isinstance(value.get("name"), str)
        or not isinstance(value.get("term"), str)
        or not isinstance(value.get("kind"), str)
        or value.get("role") not in {"branch", "leaf"}
        or not isinstance(path, dict)
        or path.get("version") != _CATALOG_PATH_VERSION
        or path.get("engine_id") != engine_id
        or not isinstance(segments, builtins.list)
        or any(
            not isinstance(segment, dict)
            or not all(isinstance(segment.get(key), str) for key in ("term", "kind", "name"))
            for segment in segments
        )
    ):
        raise NotebookEngineCatalogControlPlaneError("Notebook Engine Catalog 返回了无效规范路径")
    return value


def _raise_catalog_http_error(response: httpx.Response) -> None:
    try:
        payload = response.json()
    except ValueError:
        payload = {}
    code = payload.get("error_code") if isinstance(payload, dict) else None
    mapping: dict[str, type[NotebookEngineCatalogError]] = {
        "engine_catalog_request_invalid": NotebookEngineCatalogRequestError,
        "notebook_engine_catalog_forbidden": NotebookEngineCatalogForbiddenError,
        "engine_not_found": NotebookEngineNotFoundError,
        "engine_catalog_entry_not_found": NotebookEngineCatalogEntryNotFoundError,
        "engine_catalog_operation_unsupported": NotebookEngineCatalogUnsupportedError,
        "engine_catalog_control_plane_failed": NotebookEngineCatalogControlPlaneError,
        "engine_catalog_provider_failed": NotebookEngineCatalogProviderError,
        "engine_unavailable": NotebookEngineUnavailableError,
        "engine_catalog_timeout": NotebookEngineCatalogTimeoutError,
    }
    if response.status_code == httpx.codes.UNAUTHORIZED and code == "notebook_session_unavailable":
        raise NotebookSessionUnavailableError("Notebook 会话已失效，请重新打开")
    error_type = mapping.get(code, NotebookEngineCatalogControlPlaneError)
    raise error_type(f"Notebook Engine Catalog 接口返回 HTTP {response.status_code} ({code or 'unknown'})")


def _required_name(value: str, field_name: str) -> str:
    if not isinstance(value, str) or not value or value.strip() != value:
        raise NotebookEngineCatalogRequestError(f"{field_name} 必须是非空规范名称")
    return value


def _path_parts(value: str, field_name: str) -> builtins.list[str]:
    if not isinstance(value, str) or value.strip() != value or "\\" in value:
        raise NotebookEngineCatalogRequestError(f"{field_name} 必须是规范相对路径")
    if value in {"", "/"}:
        return []
    normalized = value.strip("/")
    parts = normalized.split("/")
    if any(not part or part in {".", ".."} for part in parts):
        raise NotebookEngineCatalogRequestError(f"{field_name} 必须是规范相对路径")
    return parts


_CLIENT_TYPES: dict[str, type[Any]] = {
    "postgresql": PostgreSQLEngine,
    "mysql": MySQLEngine,
    "doris": DorisEngine,
    "clickhouse": ClickHouseEngine,
    "spark": SparkSQLEngine,
    "mongodb": MongoDBEngine,
    "neo4j": Neo4jEngine,
    "minio": MinIOEngine,
    "s3": S3Engine,
    "nfs": NFSEngine,
    "kafka": KafkaEngine,
}


__all__ = [
    "NotebookEngineCatalogControlPlaneError",
    "NotebookEngineCatalogEntryNotFoundError",
    "NotebookEngineCatalogError",
    "NotebookEngineCatalogForbiddenError",
    "NotebookEngineCatalogProviderError",
    "NotebookEngineCatalogRequestError",
    "NotebookEngineCatalogTimeoutError",
    "NotebookEngineCatalogUnsupportedError",
    "NotebookEngineDiscoveryError",
    "NotebookEngineNotFoundError",
    "NotebookEngineUnavailableError",
    "NotebookDataControlPlaneError",
    "NotebookDataError",
    "NotebookDataForbiddenError",
    "NotebookDataProviderError",
    "NotebookDataRequestError",
    "NotebookDataTimeoutError",
    "NotebookDataUnsupportedError",
    "NotebookMemoryLimitError",
    "NotebookSessionUnavailableError",
    "ClickHouseEngine",
    "DatabaseNamespace",
    "DatabaseTable",
    "DorisEngine",
    "KafkaEngine",
    "KafkaTopic",
    "MinIOEngine",
    "MongoDBCollection",
    "MongoDBDatabase",
    "MongoDBEngine",
    "MySQLEngine",
    "NFSDirectory",
    "NFSEngine",
    "NFSFile",
    "Neo4jDatabase",
    "Neo4jEngine",
    "Neo4jGraph",
    "ObjectStorageBucket",
    "ObjectStorageObject",
    "PostgreSQLEngine",
    "PostgreSQLSchema",
    "PostgreSQLTable",
    "S3Engine",
    "SparkSQLEngine",
    "client",
    "list",
]
