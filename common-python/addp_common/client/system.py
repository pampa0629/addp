"""System 模块客户端"""
from typing import List, Dict, Any, Optional
from .base import BaseClient


class SystemClient(BaseClient):
    """System 模块 API 客户端 - 引擎管理、用户认证"""

    async def list_engines(self) -> List[Dict[str, Any]]:
        """获取引擎列表"""
        resp = await self.get("/api/v1/system/engines")
        if not isinstance(resp, list):
            raise ValueError("system engines response must be a list")
        return resp

    async def get_authorization_context(self) -> Dict[str, Any]:
        """Resolve the current user access token through System AuthContext."""
        response = await self.get("/api/v1/system/auth/context")
        if not isinstance(response, dict) or response.get("schema_version") != "addp.auth_context/v1":
            raise ValueError("system authorization context must use addp.auth_context/v1")
        return response

    async def create_delegation(
        self,
        *,
        audience: str,
        scopes: List[str],
        agent_run_id: str,
        tool_call_id: str,
    ) -> Dict[str, Any]:
        """Issue one short-lived delegated token for an ADDP Tool call."""
        response = await self.post(
            "/api/v1/system/auth/delegations",
            json={
                "audience": audience,
                "scopes": scopes,
                "agent_run_id": agent_run_id,
                "tool_call_id": tool_call_id,
            },
        )
        if not isinstance(response, dict) or not str(response.get("access_token") or "").startswith("addp_dat_"):
            raise ValueError("system delegation response must contain a delegated access token")
        return response

    async def get_engine(self, engine_id: int) -> Dict[str, Any]:
        """获取引擎详情"""
        return await self.get(f"/api/v1/system/engines/{engine_id}")

    async def list_buckets(self, engine_id: int) -> List[str]:
        """获取对象存储的 Bucket 列表"""
        resp = await self.get(f"/api/v1/system/engines/{engine_id}/buckets")
        return resp.get("buckets", [])

    async def list_objects(
        self, engine_id: int, bucket: str, prefix: Optional[str] = None
    ) -> List[Dict[str, Any]]:
        """获取对象存储的对象列表"""
        params = {"bucket": bucket}
        if prefix:
            params["prefix"] = prefix
        resp = await self.get(f"/api/v1/system/engines/{engine_id}/objects", params=params)
        return resp.get("objects", [])

    async def list_engine_summaries(self, capability: str = "all") -> List[Dict[str, Any]]:
        """读取 owner 数组并生成唯一的智能体引擎选择投影。"""
        if capability not in ("all", "workflow"):
            raise ValueError("unsupported engine summary capability")
        engines = await self.list_engines()
        result = []
        for engine in engines:
            fields = ("id", "name", "engine_type", "lifecycle_state", "connection_status")
            if not isinstance(engine, dict) or any(field not in engine for field in fields):
                raise ValueError("system engine response is missing summary facts")
            summary = {field: engine[field] for field in fields}
            if capability == "workflow" and (
                summary["lifecycle_state"] != "active"
                or not self._supports_workflow(engine.get("capabilities"))
            ):
                continue
            result.append(summary)
        return result

    @staticmethod
    def _supports_workflow(capabilities: Dict[str, Any]) -> bool:
        if not isinstance(capabilities, dict):
            return False
        if capabilities.get("schema_version") != "engine.capabilities/v1":
            return False

        compute = capabilities.get("compute")
        if not isinstance(compute, dict):
            return False

        workflow = compute.get("workflow")
        if not isinstance(workflow, dict):
            return False

        return workflow.get("supported") is True
