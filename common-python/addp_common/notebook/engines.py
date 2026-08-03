"""面向 Notebook 使用者的 ADDP 原生引擎发现门面。"""

from __future__ import annotations

import builtins
import io
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
_QUERY_ENDPOINT_ENV = "ADDP_NOTEBOOK_QUERY_API_ENDPOINT"
_CAPABILITY_ENV = "ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN"
_CAPABILITY_PREFIX = "addp_nkc_"
_CATALOG_PATH_VERSION = "catalog.path/v1"
_CATALOG_PAGE_SIZE = 1000
_transport: httpx.BaseTransport | None = None


class NotebookSessionUnavailableError(RuntimeError):
    """当前 Python process 不属于有效的受控 Notebook 会话。"""


class NotebookEngineDiscoveryError(RuntimeError):
    """当前 Notebook 会话未能读取可用 Engine 描述。"""


class NotebookCatalogError(RuntimeError):
    """Notebook 原生引擎门面的 Catalog 基础错误。"""


class NotebookCatalogRequestError(NotebookCatalogError):
    pass


class NotebookCatalogForbiddenError(NotebookCatalogError):
    pass


class NotebookEngineNotFoundError(NotebookCatalogError):
    pass


class NotebookCatalogEntryNotFoundError(NotebookCatalogError):
    pass


class NotebookCatalogUnsupportedError(NotebookCatalogError):
    pass


class NotebookCatalogControlPlaneError(NotebookCatalogError):
    pass


class NotebookCatalogProviderError(NotebookCatalogError):
    pass


class NotebookEngineUnavailableError(NotebookCatalogError):
    pass


class NotebookCatalogTimeoutError(NotebookCatalogError):
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


def client(engine_id: int, *, timeout: float = 10.0) -> PostgreSQLEngine:
    """按精确 engine_type 返回已注册的原生只读引擎客户端。"""
    if isinstance(engine_id, bool) or not isinstance(engine_id, int) or engine_id <= 0:
        raise NotebookCatalogRequestError("engine_id 必须是正整数")
    descriptor = next((item for item in list(timeout=timeout) if item.get("id") == engine_id), None)
    if descriptor is None:
        raise NotebookEngineNotFoundError(f"Engine {engine_id} 不存在或当前不可用")
    engine_type = descriptor.get("engine_type")
    if engine_type != "postgresql":
        raise NotebookCatalogUnsupportedError(f"当前 SDK 尚未注册 engine_type={engine_type!r} 的原生门面")
    _validate_postgresql_descriptor(descriptor)
    return PostgreSQLEngine(engine_id=engine_id, descriptor=descriptor, timeout=timeout)


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
                f"表的 Catalog 估算大小 {estimated} bytes 超过 memory_limit={limit} bytes"
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
            raise NotebookCatalogRequestError("timeout 必须大于 0")
        self.engine_id = engine_id
        self.descriptor = dict(descriptor)
        self.timeout = float(timeout)
        self._root_path: dict[str, Any] | None = None
        self._schema_paths: dict[str, dict[str, Any]] = {}

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
            raise NotebookCatalogEntryNotFoundError(f"PostgreSQL schema {schema!r} 不存在")
        entries = self._all_children(schema_path, deadline)
        tables: builtins.list[PostgreSQLTable] = []
        for entry in entries:
            if entry["term"] != "table" or entry["role"] != "leaf":
                raise NotebookCatalogControlPlaneError("PostgreSQL Catalog 返回了不符合模型的 table 条目")
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
        raise NotebookCatalogEntryNotFoundError(
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
                raise NotebookCatalogControlPlaneError("PostgreSQL Catalog 未返回唯一 server root")
            root_path = roots[0]["path"]
            self._root_path = root_path
        entries = self._all_children(root_path, deadline)
        schemas: builtins.list[PostgreSQLSchema] = []
        for entry in entries:
            if entry["term"] != "schema" or entry["role"] != "branch":
                raise NotebookCatalogControlPlaneError("PostgreSQL Catalog 返回了不符合模型的 schema 条目")
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
            raise NotebookCatalogTimeoutError("Notebook Catalog 请求超时")
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
            raise NotebookCatalogControlPlaneError("Notebook Catalog 返回了无效 JSON") from exc
        nodes = payload.get("nodes") if isinstance(payload, dict) else None
        if not isinstance(nodes, builtins.list):
            raise NotebookCatalogControlPlaneError("Notebook Catalog 返回了无效节点列表")
        return [_validate_catalog_entry(node, self.engine_id) for node in nodes]


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
        raise NotebookCatalogTimeoutError("Notebook Catalog 请求超时") from exc
    except httpx.HTTPError as exc:
        if method == "GET":
            raise NotebookEngineDiscoveryError("无法连接 ADDP Notebook Engine 发现接口") from exc
        raise NotebookCatalogControlPlaneError("无法连接 ADDP Notebook Catalog 接口") from exc


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
        "notebook_data_forbidden": NotebookDataForbiddenError,
        "table_scan_unsupported": NotebookDataUnsupportedError,
        "table_scan_failed": NotebookDataProviderError,
        "table_scan_timeout": NotebookDataTimeoutError,
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


def _validate_postgresql_descriptor(descriptor: dict[str, Any]) -> None:
    capabilities = descriptor.get("capabilities")
    if not isinstance(capabilities, dict) or capabilities.get("engine_type") != "postgresql":
        raise NotebookCatalogUnsupportedError("PostgreSQL Engine capabilities 与 engine_type 不一致")
    storage = capabilities.get("storage") if isinstance(capabilities, dict) else None
    catalog = storage.get("catalog") if isinstance(storage, dict) else None
    model = storage.get("catalog_model") if isinstance(storage, dict) else None
    levels = model.get("levels") if isinstance(model, dict) else None
    if (
        not isinstance(catalog, dict)
        or catalog.get("supported") is not True
        or not isinstance(model, dict)
        or model.get("path_version") != _CATALOG_PATH_VERSION
        or model.get("root_term") != "server"
        or not isinstance(levels, builtins.list)
        or len(levels) < 2
        or not isinstance(levels[0], dict)
        or not isinstance(levels[1], dict)
        or levels[0].get("term") != "schema"
        or levels[0].get("role") != "branch"
        or levels[1].get("term") != "table"
        or levels[1].get("role") != "leaf"
    ):
        raise NotebookCatalogUnsupportedError("PostgreSQL Engine 未声明兼容的 CatalogModel")


def _validate_catalog_entry(value: Any, engine_id: int) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise NotebookCatalogControlPlaneError("Notebook Catalog 返回了无效条目")
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
        raise NotebookCatalogControlPlaneError("Notebook Catalog 返回了无效规范路径")
    return value


def _raise_catalog_http_error(response: httpx.Response) -> None:
    try:
        payload = response.json()
    except ValueError:
        payload = {}
    code = payload.get("error_code") if isinstance(payload, dict) else None
    mapping: dict[str, type[NotebookCatalogError]] = {
        "catalog_request_invalid": NotebookCatalogRequestError,
        "notebook_catalog_forbidden": NotebookCatalogForbiddenError,
        "engine_not_found": NotebookEngineNotFoundError,
        "catalog_entry_not_found": NotebookCatalogEntryNotFoundError,
        "catalog_operation_unsupported": NotebookCatalogUnsupportedError,
        "catalog_control_plane_failed": NotebookCatalogControlPlaneError,
        "catalog_provider_failed": NotebookCatalogProviderError,
        "engine_unavailable": NotebookEngineUnavailableError,
        "catalog_timeout": NotebookCatalogTimeoutError,
    }
    if response.status_code == httpx.codes.UNAUTHORIZED and code == "notebook_session_unavailable":
        raise NotebookSessionUnavailableError("Notebook 会话已失效，请重新打开")
    error_type = mapping.get(code, NotebookCatalogControlPlaneError)
    raise error_type(f"Notebook Catalog 接口返回 HTTP {response.status_code} ({code or 'unknown'})")


def _required_name(value: str, field_name: str) -> str:
    if not isinstance(value, str) or not value or value.strip() != value:
        raise NotebookCatalogRequestError(f"{field_name} 必须是非空规范名称")
    return value


__all__ = [
    "NotebookCatalogControlPlaneError",
    "NotebookCatalogEntryNotFoundError",
    "NotebookCatalogError",
    "NotebookCatalogForbiddenError",
    "NotebookCatalogProviderError",
    "NotebookCatalogRequestError",
    "NotebookCatalogTimeoutError",
    "NotebookCatalogUnsupportedError",
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
    "PostgreSQLEngine",
    "PostgreSQLSchema",
    "PostgreSQLTable",
    "client",
    "list",
]
