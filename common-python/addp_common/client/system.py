"""System 模块客户端"""
import json
from typing import List, Dict, Any, Optional
from .base import BaseClient


class SystemClient(BaseClient):
    """System 模块 API 客户端 - 引擎管理、用户认证"""

    async def list_engines(self, tenant_id: Optional[int] = None) -> List[Dict[str, Any]]:
        """获取引擎列表"""
        params = {"tenant_id": tenant_id} if tenant_id else None
        resp = await self.get("/api/v1/system/engines", params=params)
        return resp if isinstance(resp, list) else resp.get("engines", [])

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

    async def get_workflow_engines(self) -> List[Dict[str, Any]]:
        """获取所有支持工作流执行的引擎（dev_modes 包含 workflow）"""
        resp = await self.get("/api/v1/system/engines")
        engines = resp if isinstance(resp, list) else resp.get("data", [])
        result = []
        for e in engines:
            caps_raw = e.get("capabilities", "{}")
            caps = json.loads(caps_raw) if isinstance(caps_raw, str) else caps_raw
            modes = [m for c in caps.get("compute", []) for m in c.get("dev_modes", [])]
            if "workflow" in modes:
                result.append({
                    "id": e["id"],
                    "name": e["name"],
                    "engine_type": e["engine_type"],
                    "is_active": e.get("is_active", True),
                    "connection_status": e.get("connection_status", "unknown"),
                })
        return result
