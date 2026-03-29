from typing import Any, Dict, Optional
from addp_common.client.system import SystemClient as _SystemClient


class SystemClient(_SystemClient):
    """System 模块 API 客户端 - 继承 common-python SystemClient，添加 agent 特有逻辑"""

    async def select_workflow_engine(self, task_description: str = "") -> Optional[Dict[str, Any]]:
        """根据任务描述自动选择最合适的工作流引擎"""
        engines = await self.get_workflow_engines()
        online = [e for e in engines if e.get("connection_status") == "online" and e.get("is_active")]
        if not online:
            online = engines

        desc_lower = task_description.lower()
        spark_keywords = ["spark", "大数据", "分布式", "海量", "tb", "亿"]
        math_keywords = ["数学", "统计", "回归", "方程", "积分"]

        preferred_type = "python_workflow"
        if any(k in desc_lower for k in spark_keywords):
            preferred_type = "spark_workflow"
        elif any(k in desc_lower for k in math_keywords):
            preferred_type = "math_workflow"

        for engine_type in [preferred_type, "python_workflow"]:
            for e in online:
                if e["engine_type"] == engine_type:
                    return e

        return online[0] if online else None
