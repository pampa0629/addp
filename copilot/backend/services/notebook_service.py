"""基于 Session 已验证资源事实生成 Notebook Python 单元。"""

from __future__ import annotations

import json
import re
from typing import Any

from langchain_core.messages import HumanMessage, SystemMessage
from sqlalchemy.orm import Session

from services.inference_service import CopilotInferenceService


class NotebookService:
    _system_prompt = (
        "你是 ADDP Notebook Python 数据分析代码生成助手。只根据当前 Kernel、已验证的 Engine/Catalog 路径、字段和空间事实"
        "生成一个可插入的新代码单元，不执行代码。必须通过 from addp_common.notebook import engines 使用当前 Session 能力；"
        "不得读取环境变量，不得使用 requests/httpx/psycopg2/sqlalchemy 建立旁路连接，不得包含 token、connection_info 或 ADDP URL。"
        "不得假定字段名为 geom/geometry，不得编造字段、路径、CRS 或数据。"
        "Notebook 表分析统一使用 engines.client(engine_id) 的原生 table(...).to_pandas(memory_limit=...)；"
        "空间表统一使用 table(...).to_geopandas(memory_limit=..., geometry_column=..., crs=...)，其中几何列和 CRS 必须来自已验证资源事实。"
        "不得生成 engine.sql(...) 或把分析过程写成 SQL；查询语言生成属于查询工作台。路径参数必须从输入 path_segments 精确推导。"
        "调用具体原生门面时必须遵守其参数契约：PostgreSQL 使用 client.table(schema=<schema segment>, name=<table segment>)，"
        "MySQL/Doris/ClickHouse/Spark 使用 client.table(database=<database segment>, name=<table segment>)；"
        "不得把 path_segments 作为位置参数、使用 *path_segments 或传入完整路径列表。"
        "memory_limit 只允许传整数 bytes 或带 B/KiB/MiB/GiB 后缀的字符串，推荐使用 \"512MiB\" 或 \"1GiB\"，不得使用 MB/GB。"
        "空间距离和面积计算必须使用投影坐标系；源 CRS 为地理坐标系时根据数据范围估算合适的投影 CRS，不得固定假设 EPSG:3857 或其他 CRS。"
        "同一类别图形可能重叠时先 union，避免重复计量。最终 DataFrame 列名、图例、标题和坐标轴等展示标签必须使用 display_language，"
        "面积需求在未指定其他单位时必须同时输出平方米和公顷两列，距离等标签必须写明单位；内部 Python 变量名可以使用英文，"
        "但不得把 area_sqm、area_hectares 等内部标识直接作为展示标签。"
        "display_labels 必须列出代码实际使用的全部用户可见结果标签，且每一项都必须原样出现在 code 中。"
        "只返回结构化 JSON。"
    )

    async def generate(
        self, *, query: str, kernel: str, resources: list[dict[str, Any]], tenant_id: int, db: Session
    ) -> dict[str, Any]:
        if kernel != "python3":
            raise ValueError(f"unsupported notebook kernel: {kernel}")
        llm = CopilotInferenceService.chat_model(
            db, tenant_id=tenant_id, scenario_code="notebook_generation", temperature=0.2, max_output_tokens=3200,
        )
        display_language = self._display_language(query)
        response = await llm.ainvoke([
            SystemMessage(content=(
                self._system_prompt
                + '\n输出格式：{"code":"...","explanation":"...","warnings":[],"display_labels":["..."]}。'
            )),
            HumanMessage(content=json.dumps({
                "kernel": kernel,
                "resources": resources,
                "user_request": query,
                "display_language": display_language,
            }, ensure_ascii=False, default=str)),
        ])
        return self._parse_output(
            str(getattr(response, "content", response)),
            display_language=display_language,
            require_area_units=self._requires_area_units(query),
        )

    @staticmethod
    def _display_language(query: str) -> str:
        if re.search(r"[\u4e00-\u9fff]", query):
            return "zh-CN"
        return "same-as-user-request"

    @staticmethod
    def _requires_area_units(query: str) -> bool:
        return bool(re.search(r"面积|\barea\b", query, re.IGNORECASE))

    @staticmethod
    def _parse_output(
        output: str, *, display_language: str, require_area_units: bool = False
    ) -> dict[str, Any]:
        cleaned = output.strip()
        match = re.search(r"```(?:json)?\s*(.*?)\s*```", cleaned, re.DOTALL | re.IGNORECASE)
        if match:
            cleaned = match.group(1).strip()
        parsed = json.loads(cleaned)
        if not isinstance(parsed, dict) or not isinstance(parsed.get("code"), str) or not parsed["code"].strip():
            raise ValueError("notebook generation response must contain non-empty code")
        code = parsed["code"].strip()
        forbidden = re.compile(
            r"\b(requests|httpx|psycopg2|sqlalchemy)\b|connection_info|ADDP_[A-Z_]*TOKEN|addp://",
            re.IGNORECASE,
        )
        if forbidden.search(code) or "addp_common.notebook" not in code:
            raise ValueError("generated notebook code bypasses the Session-scoped notebook facade")
        if re.search(r"\.sql\s*\(", code, re.IGNORECASE):
            raise ValueError("generated notebook code calls engine.sql instead of Python analysis")
        if re.search(r"\.table\s*\(\s*\*", code, re.IGNORECASE):
            raise ValueError("generated notebook code expands path segments into an invalid table call")
        if re.search(r"memory_limit\s*=\s*[\"'][^\"']*(?:MB|GB)\b", code, re.IGNORECASE):
            raise ValueError("generated notebook code uses an unsupported memory_limit unit")
        labels = parsed.get("display_labels")
        if not isinstance(labels, list) or not labels:
            raise ValueError("notebook generation response must declare display labels")
        normalized_labels = [str(item).strip() for item in labels if str(item).strip()]
        if len(normalized_labels) != len(labels) or any(label not in code for label in normalized_labels):
            raise ValueError("generated notebook display labels must appear in code")
        if display_language == "zh-CN" and any(
            re.search(r"[\u4e00-\u9fff]", label) is None for label in normalized_labels
        ):
            raise ValueError("generated notebook display labels do not match zh-CN")
        if require_area_units and display_language == "zh-CN":
            if not any("平方米" in label for label in normalized_labels) or not any(
                "公顷" in label for label in normalized_labels
            ):
                raise ValueError("area results must include localized square-metre and hectare labels")
        warnings = parsed.get("warnings") if isinstance(parsed.get("warnings"), list) else []
        return {
            "code": code,
            "explanation": str(parsed.get("explanation") or "").strip(),
            "warnings": [str(item).strip() for item in warnings if str(item).strip()],
        }


notebook_service = NotebookService()
