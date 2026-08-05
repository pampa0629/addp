"""OAuth-authenticated System Module Registry client."""

from __future__ import annotations

import asyncio
import logging

import httpx
from pydantic import BaseModel, ConfigDict, Field

from .service_token import OAuthServiceTokenSource


MANAGEMENT_SCHEMA_VERSION = "addp.configuration-management/v1"
logger = logging.getLogger("addp.module_registry")


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
    module_url: str
    route_prefix: str
    health_check_url: str = ""
    metadata: dict[str, object] = Field(default_factory=dict)
    configuration_management: ConfigurationManagementDeclaration | None = None


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

    async def register(self, registration: ModuleRegistration) -> None:
        await self._post("/api/v1/system/runtime/modules", registration.model_dump(exclude_none=True))

    async def heartbeat(self, module_name: str) -> None:
        await self._post("/api/v1/system/runtime/modules/heartbeat", {"module_name": module_name})

    async def run(self, registration: ModuleRegistration, *, interval: float = 10.0) -> None:
        registered = False
        failures = 0
        while True:
            try:
                if not registered:
                    await self.register(registration)
                    registered = True
                    failures = 0
                    logger.info("模块注册成功: %s", registration.module_name)
                await asyncio.sleep(interval)
                await self.heartbeat(registration.module_name)
                failures = 0
            except asyncio.CancelledError:
                raise
            except Exception as exc:
                failures += 1
                logger.warning("模块注册或心跳失败: module=%s error=%s", registration.module_name, exc)
                if failures >= 3:
                    registered = False
                await asyncio.sleep(min(float(failures * 5), 20.0))

    async def _post(self, path: str, payload: dict[str, object]) -> None:
        token = await self._token_source.platform_token()
        response = await self._client.post(path, json=payload, headers={"Authorization": f"Bearer {token}"})
        if response.status_code == 401:
            self._token_source.invalidate_platform(token)
            token = await self._token_source.platform_token()
            response = await self._client.post(path, json=payload, headers={"Authorization": f"Bearer {token}"})
        response.raise_for_status()

    async def close(self) -> None:
        await self._client.aclose()
