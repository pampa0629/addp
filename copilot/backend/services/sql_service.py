"""
SQL 生成服务：基于 LangChain 的 SQL 生成
"""
from typing import List, Dict, Optional
import re
from langchain_core.messages import HumanMessage, SystemMessage
from sqlalchemy.orm import Session

from services.inference_service import CopilotInferenceService


class SQLService:
    """
    SQL 生成服务

    功能：
    - 理解用户的自然语言需求
    - 生成 SQL 查询语句
    """

    def __init__(self):
        self.system_prompt = """你是一个专业的 SQL 查询生成助手。
根据用户的自然语言需求和提供的数据源信息，生成标准的 SQL 查询语句。

要求：
1. 生成的 SQL 必须符合 PostgreSQL 语法
2. 只返回 SQL 语句，不要有其他解释
3. 如果需要多个查询，用分号分隔
"""

    async def generate(
        self,
        query: str,
        datasources: List[Dict],
        memory=None,
        tenant_id: int = 1,
        db: Session | None = None,
    ) -> Dict:
        """
        生成 SQL

        Args:
            query: 用户需求描述
            datasources: 数据源列表
            memory: 对话记忆（LangChain Messages 列表，可选）
            tenant_id: 租户 ID

        Returns:
            包含 sql 和 explanation 的字典
        """
        if db is None:
            raise ValueError("database session is required to resolve the nl2sql scenario")
        llm = CopilotInferenceService.chat_model(
            db,
            tenant_id=tenant_id,
            scenario_code="nl2sql",
            temperature=0.2,
            max_output_tokens=2000,
        )
        context = self._build_context(datasources)

        messages = [
            SystemMessage(content=self.system_prompt),
            HumanMessage(content=f"可用的数据源:\n{context}\n\n用户需求:\n{query}")
        ]

        if memory:
            messages.extend(memory)

        try:
            response = await llm.ainvoke(messages)
            output = response.content
            sql = self._extract_sql(output)
            return {"sql": sql, "explanation": "SQL 生成成功"}
        except Exception as e:
            print(f"Error generating SQL: {e}")
            raise

    def _build_context(self, datasources: List[Dict]) -> str:
        if not datasources:
            return "暂无可用数据源"

        context = ""
        for ds in datasources:
            table_name = f"{ds.get('schema_name', 'public')}.{ds.get('table_name', '')}"
            context += f"- 表: {table_name}\n"

            if ds.get('columns'):
                cols = [f"{c['name']} ({c.get('type', 'unknown')})" for c in ds['columns']]
                context += f"  字段: {', '.join(cols)}\n"

            spatial = self._spatial_capability(ds)
            if spatial:
                geometry_column = spatial.get('primary_geometry_column') or self._first_geometry_column_name(spatial) or 'geometry'
                context += f"  空间字段: {geometry_column}\n"

        return context

    def _spatial_capability(self, datasource: Dict) -> Dict:
        capabilities = datasource.get('capabilities') or {}
        if isinstance(capabilities, dict) and isinstance(capabilities.get('spatial'), dict):
            return capabilities['spatial']

        attributes = datasource.get('attributes') or {}
        if isinstance(attributes, dict):
            attr_capabilities = attributes.get('capabilities') or {}
            if isinstance(attr_capabilities, dict) and isinstance(attr_capabilities.get('spatial'), dict):
                return attr_capabilities['spatial']

        return {}

    def _first_geometry_column_name(self, spatial: Dict) -> str:
        columns = spatial.get('geometry_columns') or []
        if isinstance(columns, list) and columns:
            first = columns[0]
            if isinstance(first, dict):
                return first.get('name', '')
        return ''

    def _extract_sql(self, output: str) -> str:
        sql_match = re.search(r'```sql\s*\n(.*?)\n```', output, re.DOTALL | re.IGNORECASE)
        if sql_match:
            return sql_match.group(1).strip()

        sql_match = re.search(r'(SELECT.*?);?$', output, re.DOTALL | re.IGNORECASE)
        if sql_match:
            return sql_match.group(1).strip()

        return output.strip()


# 全局单例
sql_service = SQLService()
