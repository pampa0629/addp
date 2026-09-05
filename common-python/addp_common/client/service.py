"""Typed Python client for the ADDP Service Consumer Contract."""

from __future__ import annotations

import asyncio
import re
from collections.abc import AsyncIterator, Awaitable, Callable
from typing import Any, Literal
from urllib.parse import urlsplit

import httpx
from pydantic import BaseModel, ConfigDict, Field, model_validator

CONSUMER_DESCRIPTOR_SCHEMA_VERSION = "addp.service_consumer/v1"
_FINGERPRINT_PATTERN = r"^sha256:[0-9a-f]{64}$"
_QUERY_OPERATION_PATH = re.compile(r"^/api/query/[a-z0-9_-]+/query$")

ServiceType = Literal["query", "graph", "tile", "registered"]
OutputKind = Literal["tabular", "spatial_tabular"]
FieldType = Literal[
    "unknown",
    "string",
    "bool",
    "bytes",
    "mixed",
    "int",
    "bigint",
    "float",
    "double",
    "decimal",
    "date",
    "time",
    "timestamp",
    "json",
    "array",
    "uuid",
    "geometry",
]
TokenLoader = Callable[[], Awaitable[str]]


class _ContractModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class ServiceReference(_ContractModel):
    service_type: ServiceType
    service_id: int = Field(gt=0)


class ConsumerServiceSummary(_ContractModel):
    ref: ServiceReference
    title: str
    description: str
    access_mode: Literal["public", "private"]
    output_kind: OutputKind
    contract_fingerprint: str = Field(pattern=_FINGERPRINT_PATTERN)


class ConsumerServicePage(_ContractModel):
    data: list[ConsumerServiceSummary]
    total: int = Field(ge=0)
    page: int = Field(ge=1)
    page_size: int = Field(ge=1, le=100)
    total_pages: int = Field(ge=0)


class ConsumerOperation(_ContractModel):
    key: str
    method: str
    path: str
    input_kind: str
    output_kind: str


class ConsumerNamedParameter(_ContractModel):
    name: str
    type: FieldType
    required: bool
    description: str = ""
    default: Any = None


class ConsumerQueryField(_ContractModel):
    name: str
    type: FieldType
    element_type: FieldType | None = None
    nullable: bool
    selectable: bool
    filterable: bool
    operators: list[str]
    sortable: bool


class ConsumerFilterContract(_ContractModel):
    combinators: list[str]
    max_depth: int = Field(gt=0)
    max_nodes: int = Field(gt=0)
    max_in_values: int = Field(gt=0)


class ConsumerOrderContract(_ContractModel):
    directions: list[Literal["asc", "desc"]]
    stable_key: list[str]


class ConsumerPageContract(_ContractModel):
    kind: Literal["cursor"]
    default_limit: int = Field(gt=0)
    max_limit: int = Field(gt=0)

    @model_validator(mode="after")
    def validate_limits(self) -> "ConsumerPageContract":
        if self.default_limit > self.max_limit:
            raise ValueError("consumer page default_limit exceeds max_limit")
        return self


class ConsumerQueryIntent(_ContractModel):
    header: Literal["X-ADDP-Query-Intent"]
    allowed_values: list[Literal["query", "export"]]
    default_value: Literal["query", "export"]


class StructuredQueryInputContract(_ContractModel):
    kind: Literal["structured_query"]
    named_parameters: list[ConsumerNamedParameter]
    fields: list[ConsumerQueryField]
    default_selection: list[str]
    filter: ConsumerFilterContract
    order: ConsumerOrderContract
    page: ConsumerPageContract
    formats: list[Literal["json", "csv", "geojson"]]
    intent: ConsumerQueryIntent


class ConsumerOutputField(_ContractModel):
    name: str
    type: FieldType
    element_type: FieldType | None = None
    nullable: bool
    comment: str = ""


class ConsumerGeometryField(_ContractModel):
    name: str
    geometry_type: str = ""
    srid: int | None = None
    crs_ref: str = ""
    dimension: int | None = None


class ConsumerSpatialContract(_ContractModel):
    primary_geometry_field: str
    srid: int | None = None
    crs_ref: str = ""
    geometry_fields: list[ConsumerGeometryField]


class TabularOutputContract(_ContractModel):
    kind: OutputKind
    fields: list[ConsumerOutputField]
    spatial: ConsumerSpatialContract | None = None

    @model_validator(mode="after")
    def validate_spatial_contract(self) -> "TabularOutputContract":
        if (self.kind == "spatial_tabular") != (self.spatial is not None):
            raise ValueError("spatial_tabular output requires one spatial contract")
        return self


class ConsumerDescriptor(_ContractModel):
    schema_version: Literal[CONSUMER_DESCRIPTOR_SCHEMA_VERSION]
    ref: ServiceReference
    title: str
    description: str
    status: Literal["active"]
    access_mode: Literal["public", "private"]
    contract_fingerprint: str = Field(pattern=_FINGERPRINT_PATTERN)
    operations: list[ConsumerOperation]
    input_contract: StructuredQueryInputContract
    output_contract: TabularOutputContract

    def query_operation(self) -> ConsumerOperation:
        operations = [operation for operation in self.operations if operation.key == "query"]
        if len(operations) != 1:
            raise ServiceConsumerContractError("descriptor must declare exactly one query operation")
        operation = operations[0]
        if (
            operation.method != "POST"
            or operation.input_kind != "structured_query"
            or operation.output_kind != self.output_contract.kind
            or not _QUERY_OPERATION_PATH.fullmatch(operation.path)
        ):
            raise ServiceConsumerContractError("descriptor declares an invalid query operation")
        return operation


class QueryOrder(_ContractModel):
    field: str
    direction: Literal["asc", "desc"]


class QueryPageRequest(_ContractModel):
    limit: int = Field(gt=0)
    cursor: str = ""


class StructuredQueryRequest(_ContractModel):
    parameters: dict[str, Any] = Field(default_factory=dict)
    select: list[str] = Field(default_factory=list)
    filter: dict[str, Any] | None = None
    order_by: list[QueryOrder] = Field(default_factory=list)
    page: QueryPageRequest
    format: Literal["json"] = "json"

    def payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "page": self.page.model_dump(exclude_defaults=True),
            "format": "json",
        }
        if self.parameters:
            payload["parameters"] = self.parameters
        if self.select:
            payload["select"] = self.select
        if self.filter is not None:
            payload["filter"] = self.filter
        if self.order_by:
            payload["order_by"] = [item.model_dump() for item in self.order_by]
        return payload


class QueryPageResult(_ContractModel):
    limit: int = Field(gt=0)
    has_more: bool
    next_cursor: str = ""

    @model_validator(mode="after")
    def validate_cursor(self) -> "QueryPageResult":
        if self.has_more and not self.next_cursor:
            raise ValueError("query page with has_more must include next_cursor")
        if not self.has_more and self.next_cursor:
            raise ValueError("terminal query page must not include next_cursor")
        return self


class QueryResult(_ContractModel):
    data: list[dict[str, Any]]
    page: QueryPageResult
    service_version: str = Field(min_length=1)


class ServiceConsumerContractError(ValueError):
    """Raised when a server response violates the Service Consumer Contract."""


class ServiceConsumerAPIError(RuntimeError):
    """Stable, non-secret projection of an unsuccessful Service API response."""

    def __init__(self, status_code: int, error_code: str, message: str) -> None:
        super().__init__(f"{error_code}: {message}" if message else error_code)
        self.status_code = status_code
        self.error_code = error_code


class ServiceConsumerClient:
    """Consume published ADDP data services through their public descriptor."""

    def __init__(
        self,
        base_url: str,
        *,
        user_token: str | None = None,
        timeout: float = 30.0,
        transport: httpx.AsyncBaseTransport | None = None,
        _token_loader: TokenLoader | None = None,
    ) -> None:
        normalized_url = base_url.strip().rstrip("/")
        parsed = urlsplit(normalized_url)
        if (
            parsed.scheme not in {"http", "https"}
            or not parsed.netloc
            or parsed.path not in {"", "/"}
            or parsed.query
            or parsed.fragment
        ):
            raise ValueError("service consumer client requires an absolute ADDP Gateway HTTP(S) root URL")
        if bool(user_token) == bool(_token_loader):
            raise ValueError("service consumer client requires exactly one user authentication source")

        self._base_url = normalized_url
        self._static_user_token = user_token
        self._token_loader = _token_loader
        self._loaded_user_token: str | None = None
        self._token_lock = asyncio.Lock()
        self._client = httpx.AsyncClient(
            base_url=normalized_url,
            headers={"Content-Type": "application/json"},
            timeout=timeout,
            transport=transport,
            trust_env=False,
        )

    @classmethod
    def from_cli_session(
        cls,
        base_url: str,
        *,
        timeout: float = 30.0,
        transport: httpx.AsyncBaseTransport | None = None,
    ) -> "ServiceConsumerClient":
        """Use the current ``addp auth login`` session from the OS Keychain."""

        normalized_url = base_url.strip().rstrip("/")

        async def load_token() -> str:
            # Desktop OAuth is loaded only for this explicit constructor so
            # server runtimes can import addp_common.client without keyring.
            from addp_common.oauth import refresh_access_token

            return await refresh_access_token(normalized_url)

        return cls(
            normalized_url,
            timeout=timeout,
            transport=transport,
            _token_loader=load_token,
        )

    async def list_services(
        self,
        *,
        search: str = "",
        service_type: ServiceType | None = None,
        output_kind: OutputKind | None = None,
        page: int = 1,
        page_size: int = 20,
    ) -> ConsumerServicePage:
        if page <= 0:
            raise ValueError("page must be greater than zero")
        if page_size <= 0 or page_size > 100:
            raise ValueError("page_size must be between 1 and 100")
        if len(search) > 200:
            raise ValueError("search must not exceed 200 characters")

        params: dict[str, str | int] = {"page": page, "page_size": page_size}
        if search:
            params["search"] = search
        if service_type is not None:
            params["service_type"] = service_type
        if output_kind is not None:
            params["output_kind"] = output_kind
        payload = await self._request_json(
            "GET",
            "/api/v1/service/consumer/services",
            params=params,
        )
        return self._validate(ConsumerServicePage, payload, "consumer catalog")

    async def iter_services(
        self,
        *,
        search: str = "",
        service_type: ServiceType | None = None,
        output_kind: OutputKind | None = None,
        page_size: int = 100,
    ) -> AsyncIterator[ConsumerServiceSummary]:
        page_number = 1
        while True:
            result = await self.list_services(
                search=search,
                service_type=service_type,
                output_kind=output_kind,
                page=page_number,
                page_size=page_size,
            )
            for item in result.data:
                yield item
            if page_number >= result.total_pages:
                return
            page_number += 1

    async def get_descriptor(
        self,
        ref: ServiceReference,
        *,
        expected_contract_fingerprint: str,
    ) -> ConsumerDescriptor:
        if not re.fullmatch(_FINGERPRINT_PATTERN, expected_contract_fingerprint):
            raise ValueError("expected_contract_fingerprint must be a sha256 fingerprint")
        payload = await self._request_json(
            "GET",
            f"/api/v1/service/consumer/services/{ref.service_type}/{ref.service_id}",
        )
        descriptor = self._validate(ConsumerDescriptor, payload, "consumer descriptor")
        if descriptor.ref != ref:
            raise ServiceConsumerContractError("consumer descriptor reference does not match the requested service")
        if descriptor.contract_fingerprint != expected_contract_fingerprint:
            raise ServiceConsumerContractError("consumer descriptor contract fingerprint changed")
        descriptor.query_operation()
        return descriptor

    async def execute_query(
        self,
        descriptor: ConsumerDescriptor,
        request: StructuredQueryRequest,
        *,
        intent: Literal["query", "export"] = "query",
    ) -> QueryResult:
        operation = descriptor.query_operation()
        contract = descriptor.input_contract
        if "json" not in contract.formats:
            raise ServiceConsumerContractError("consumer descriptor does not support JSON queries")
        if intent not in contract.intent.allowed_values:
            raise ValueError("query intent is not allowed by the consumer descriptor")
        if request.page.limit > contract.page.max_limit:
            raise ValueError("query page limit exceeds the consumer descriptor maximum")

        payload = await self._request_json(
            operation.method,
            operation.path,
            json=request.payload(),
            headers={contract.intent.header: intent},
        )
        result = self._validate(QueryResult, payload, "query result")
        if result.page.limit != request.page.limit:
            raise ServiceConsumerContractError("query result page limit does not match the request")
        return result

    async def iter_query_pages(
        self,
        descriptor: ConsumerDescriptor,
        request: StructuredQueryRequest,
        *,
        intent: Literal["query", "export"] = "query",
    ) -> AsyncIterator[QueryResult]:
        cursor = request.page.cursor
        seen_cursors = {cursor} if cursor else set()
        service_version = ""
        while True:
            page_request = request.model_copy(
                update={"page": QueryPageRequest(limit=request.page.limit, cursor=cursor)},
                deep=True,
            )
            result = await self.execute_query(descriptor, page_request, intent=intent)
            if service_version and result.service_version != service_version:
                raise ServiceConsumerContractError("service version changed during cursor pagination")
            service_version = result.service_version
            yield result
            if not result.page.has_more:
                return
            cursor = result.page.next_cursor
            if cursor in seen_cursors:
                raise ServiceConsumerContractError("query cursor pagination cycle detected")
            seen_cursors.add(cursor)

    async def close(self) -> None:
        await self._client.aclose()

    async def __aenter__(self) -> "ServiceConsumerClient":
        return self

    async def __aexit__(self, *_args: object) -> None:
        await self.close()

    async def _request_json(self, method: str, path: str, **kwargs: Any) -> Any:
        response = await self._request(method, path, **kwargs)
        if response.status_code >= 400:
            raise self._api_error(response)
        try:
            return response.json()
        except ValueError as exc:
            raise ServiceConsumerContractError("Service API response must be valid JSON") from exc

    async def _request(self, method: str, path: str, **kwargs: Any) -> httpx.Response:
        token = await self._user_token()
        for attempt in range(2):
            request_kwargs = dict(kwargs)
            headers = dict(request_kwargs.pop("headers", {}) or {})
            headers["Authorization"] = f"Bearer {token}"
            response = await self._client.request(method, path, headers=headers, **request_kwargs)
            if response.status_code != 401 or self._token_loader is None or attempt == 1:
                return response
            await self._invalidate_loaded_token(token)
            token = await self._user_token()
        raise RuntimeError("service consumer request did not produce a response")

    async def _user_token(self) -> str:
        if self._static_user_token is not None:
            return self._static_user_token
        async with self._token_lock:
            if self._loaded_user_token is None:
                assert self._token_loader is not None
                loaded = await self._token_loader()
                if not isinstance(loaded, str) or not loaded:
                    raise ValueError("user token source returned an empty token")
                self._loaded_user_token = loaded
            return self._loaded_user_token

    async def _invalidate_loaded_token(self, rejected_token: str) -> None:
        async with self._token_lock:
            if self._loaded_user_token == rejected_token:
                self._loaded_user_token = None

    @staticmethod
    def _validate(model: type[_ContractModel], payload: Any, label: str) -> Any:
        try:
            return model.model_validate(payload)
        except ValueError as exc:
            raise ServiceConsumerContractError(f"invalid {label}") from exc

    @staticmethod
    def _api_error(response: httpx.Response) -> ServiceConsumerAPIError:
        error_code = f"http_{response.status_code}"
        message = response.reason_phrase
        try:
            payload = response.json()
        except ValueError:
            payload = None
        if isinstance(payload, dict):
            if isinstance(payload.get("error_code"), str) and payload["error_code"]:
                error_code = payload["error_code"]
            if isinstance(payload.get("error"), str) and payload["error"]:
                message = payload["error"]
        return ServiceConsumerAPIError(response.status_code, error_code, message)


__all__ = [
    "CONSUMER_DESCRIPTOR_SCHEMA_VERSION",
    "ConsumerDescriptor",
    "ConsumerServicePage",
    "ConsumerServiceSummary",
    "QueryOrder",
    "QueryPageRequest",
    "QueryResult",
    "ServiceConsumerAPIError",
    "ServiceConsumerClient",
    "ServiceConsumerContractError",
    "ServiceReference",
    "StructuredQueryRequest",
]
