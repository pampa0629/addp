import json
from typing import Any, Dict, List, Optional
from .base_client import ADDPBaseClient


class SystemClient(ADDPBaseClient):
    """System 模块 API 客户端 - 引擎管理"""

    def __init__(self, token: str, base_url: str = "http://localhost:8000"):
        super().__init__(base_url, token)

    async def get_workflow_engines(self) -> List[Dict[str, Any]]:
        """获取所有支持工作流执行的引擎（dev_modes 包含 workflow）"""
        resp = await self.get("/api/system/engines")
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

    async def select_workflow_engine(self, task_description: str = "") -> Optional[Dict[str, Any]]:
        """
        根据任务描述自动选择最合适的工作流引擎。

        规则：
        - Spark 任务（大数据、分布式）→ spark_workflow
        - 数学/统计任务（无空间）→ math_workflow
        - 其余（GIS、空间分析、默认）→ python_workflow
        """
        engines = await self.get_workflow_engines()
        # 只考虑在线引擎
        online = [e for e in engines if e.get("connection_status") == "online" and e.get("is_active")]
        if not online:
            online = engines  # 降级：全部候选

        desc_lower = task_description.lower()
        spark_keywords = ["spark", "大数据", "分布式", "海量", "tb", "亿"]
        math_keywords = ["数学", "统计", "回归", "方程", "积分"]

        preferred_type = "python_workflow"
        if any(k in desc_lower for k in spark_keywords):
            preferred_type = "spark_workflow"
        elif any(k in desc_lower for k in math_keywords):
            preferred_type = "math_workflow"

        # 按优先类型查找，找不到就用 python_workflow，再找不到取第一个
        for engine_type in [preferred_type, "python_workflow"]:
            for e in online:
                if e["engine_type"] == engine_type:
                    return e

        return online[0] if online else None
