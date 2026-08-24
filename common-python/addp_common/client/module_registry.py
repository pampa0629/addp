"""OAuth-authenticated System Module Registry client."""

from __future__ import annotations

import asyncio
import logging
import os
import threading
from typing import Literal
from urllib.parse import quote
from uuid import uuid4

import httpx
from pydantic import BaseModel, ConfigDict, Field

from .service_token import OAuthServiceTokenSource


MANAGEMENT_SCHEMA_VERSION = "addp.configuration-management/v1"
logger = logging.getLogger("addp.module_registry")
_PROCESS_INSTANCE_LOCK = threading.Lock()
_PROCESS_INSTANCE_PID = 0
_PROCESS_INSTANCE_ID = ""


def _process_instance_id() -> str:
    """Return one stable registration identity for the current process."""
    global _PROCESS_INSTANCE_ID, _PROCESS_INSTANCE_PID

    process_id = os.getpid()
    with _PROCESS_INSTANCE_LOCK:
        if _PROCESS_INSTANCE_PID != process_id or not _PROCESS_INSTANCE_ID:
            _PROCESS_INSTANCE_PID = process_id
            _PROCESS_INSTANCE_ID = str(uuid4())
        return _PROCESS_INSTANCE_ID


class _ContractModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class ConfigurationManagementEntry(_ContractModel):
    id: str
    owner_module: str
    scope_types: list[str]
    frontend_route: str
    read_permission: str
    update_permission: str


class ConfigurationManagementDeclaration(_ContractModel):
    schema_version: str = MANAGEMENT_SCHEMA_VERSION
    entries: list[ConfigurationManagementEntry] = Field(min_length=1)


class ModuleRegistration(_ContractModel):
    module_name: str
    instance_id: str = Field(default_factory=_process_instance_id, min_length=1, max_length=100)
    role: Literal["backend", "worker", "scheduler"] = "backend"
    module_url: str
    route_prefix: str
    health_check_url: str = ""
    metadata: dict[str, object] = Field(default_factory=dict)
    configuration_management: ConfigurationManagementDeclaration | None = None


class ModuleRegistryAPIError(httpx.HTTPStatusError):
    def __init__(self, response: httpx.Response) -> None:
        body = response.text.strip()
        if len(body) > 8192:
            body = f"{body[:8192]}..."
        error_code = ""
        error_message = ""
        try:
            payload = response.json()
        except ValueError:
            payload = None
        if isinstance(payload, dict):
            if isinstance(payload.get("error_code"), str):
                error_code = payload["error_code"]
            if isinstance(payload.get("error"), str):
                error_message = payload["error"]

        self.method = response.request.method
        self.path = response.request.url.path
        self.status_code = response.status_code
        self.error_code = error_code
        self.error_message = error_message
        self.response_body = body

        message = f"System API {self.method} {self.path} returned HTTP {self.status_code}"
        if self.error_code:
            message = f'{message} error_code="{self.error_code}"'
        if self.error_message:
            message = f'{message} error="{self.error_message}"'
        if self.response_body:
            message = f'{message} response_body="{self.response_body}"'
        super().__init__(message, request=response.request, response=response)


class ModuleRegistryClient:
    def __init__(
        self,
        system_url: str,
        token_source: OAuthServiceTokenSource,
        *,
        timeout: float = 10.0,
        transport: httpx.AsyncBaseTransport | None = None,
    ) -> None:
        self._token_source = token_source
        self._client = httpx.AsyncClient(
            base_url=system_url.strip().rstrip("/"),
            timeout=timeout,
            transport=transport,
            trust_env=False,
            headers={"Content-Type": "application/json"},
        )
        self._active_instances: dict[tuple[str, str], str] = {}

    async def register(self, registration: ModuleRegistration) -> None:
        await self._request(
            "POST",
            "/api/v1/system/runtime/modules",
            registration.model_dump(exclude_none=True),
        )
        self._active_instances[(registration.module_name, registration.instance_id)] = registration.role

    async def heartbeat(self, module_name: str, instance_id: str) -> None:
        await self._request(
            "POST",
            "/api/v1/system/runtime/modules/heartbeat",
            {"module_name": module_name, "instance_id": instance_id},
        )

    async def deregister(self, module_name: str, instance_id: str) -> None:
        encoded_module_name = quote(module_name.strip(), safe="")
        encoded_instance_id = quote(instance_id.strip(), safe="")
        if not encoded_module_name or not encoded_instance_id:
            raise ValueError("module_name and instance_id are required for deregistration")
        await self._request(
            "DELETE",
            f"/api/v1/system/runtime/modules/{encoded_module_name}/instances/{encoded_instance_id}",
        )
        self._active_instances.pop((module_name, instance_id), None)

    async def run(self, registration: ModuleRegistration, *, interval: float = 10.0) -> None:
        registered = False
        failures = 0
        try:
            while True:
                operation = "register" if not registered else "heartbeat"
                try:
                    if not registered:
                        await self.register(registration)
                        registered = True
                        failures = 0
                        logger.info(
                            "模块注册成功: module=%s instance_id=%s role=%s",
                            registration.module_name,
                            registration.instance_id,
                            registration.role,
                        )
                    operation = "heartbeat"
                    await asyncio.sleep(interval)
                    await self.heartbeat(registration.module_name, registration.instance_id)
                    failures = 0
                except asyncio.CancelledError:
                    raise
                except Exception as exc:
                    failures += 1
                    self._log_failure(operation, registration, exc)
                    if operation == "heartbeat":
                        registered = False
                        failures = 0
                        continue
                    await asyncio.sleep(min(float(failures * 5), 20.0))
        finally:
            await self._deregister_safely(registration.module_name, registration.instance_id)

    async def _request(
        self,
        method: str,
        path: str,
        payload: dict[str, object] | None = None,
    ) -> None:
        token = await self._token_source.platform_token()
        request_kwargs: dict[str, object] = {
            "headers": {"Authorization": f"Bearer {token}"},
        }
        if payload is not None:
            request_kwargs["json"] = payload
        response = await self._client.request(method, path, **request_kwargs)
        if response.status_code == 401:
            self._token_source.invalidate_platform(token)
            token = await self._token_source.platform_token()
            request_kwargs["headers"] = {"Authorization": f"Bearer {token}"}
            response = await self._client.request(method, path, **request_kwargs)
        self._raise_for_status(response)

    @staticmethod
    def _raise_for_status(response: httpx.Response) -> None:
        if response.is_success:
            return
        raise ModuleRegistryAPIError(response)

    @staticmethod
    def _log_failure(operation: str, registration: ModuleRegistration, exc: Exception) -> None:
        if isinstance(exc, ModuleRegistryAPIError):
            logger.warning(
                "module registry request failed: operation=%s module=%s instance_id=%s role=%s "
                "status_code=%s error_code=%r error_message=%r response_body=%r",
                operation,
                registration.module_name,
                registration.instance_id,
                registration.role,
                exc.status_code,
                exc.error_code,
                exc.error_message,
                exc.response_body,
            )
            return
        logger.warning(
            "module registry request failed: operation=%s module=%s instance_id=%s role=%s error=%r",
            operation,
            registration.module_name,
            registration.instance_id,
            registration.role,
            exc,
        )

    async def _deregister_safely(self, module_name: str, instance_id: str) -> None:
        if (module_name, instance_id) not in self._active_instances:
            return
        role = self._active_instances[(module_name, instance_id)]
        try:
            await asyncio.wait_for(self.deregister(module_name, instance_id), timeout=2.0)
            logger.info("模块注销成功: module=%s instance_id=%s", module_name, instance_id)
        except Exception as exc:
            self._log_failure(
                "deregister",
                ModuleRegistration(
                    module_name=module_name,
                    instance_id=instance_id,
                    role=role,
                    module_url="",
                    route_prefix="",
                ),
                exc,
            )

    async def close(self) -> None:
        if self._client.is_closed:
            return
        for module_name, instance_id in tuple(self._active_instances):
            await self._deregister_safely(module_name, instance_id)
        await self._client.aclose()
