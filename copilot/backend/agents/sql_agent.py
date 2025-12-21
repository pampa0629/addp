"""
SQL 生成 Agent：基于 LangChain 的 SQL 生成（简化版本）
"""
from typing import List, Dict, Optional
import re
from langchain_core.prompts import ChatPromptTemplate
from langchain_core.messages import HumanMessage, SystemMessage

from services.llm_service import llm_service


class SQLGenerationAgent:
    """
    SQL 生成 Agent（简化版本）

    功能：
    - 理解用户的自然语言需求
    - 生成 SQL 查询语句
    """

    def __init__(self):
        self.llm_service = llm_service
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
        tenant_id: int = 1
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
        # 1. 获取 LLM 实例
        llm = self.llm_service.get_llm(tenant_id=tenant_id)

        # 2. 构建数据源上下文
        context = self._build_context(datasources)

        # 3. 构建消息
        messages = [
            SystemMessage(content=self.system_prompt),
            HumanMessage(content=f"可用的数据源:\n{context}\n\n用户需求:\n{query}")
        ]

        # 添加历史对话（如果有）
        if memory:
            messages.extend(memory)

        # 4. 调用 LLM
        try:
            response = await llm.ainvoke(messages)
            output = response.content

            # 5. 提取 SQL
            sql = self._extract_sql(output)

            return {
                "sql": sql,
                "explanation": "SQL 生成成功"
            }

        except Exception as e:
            print(f"Error generating SQL: {e}")
            raise

    def _build_context(self, datasources: List[Dict]) -> str:
        """
        构建数据源上下文

        Args:
            datasources: 数据源列表

        Returns:
            上下文字符串
        """
        if not datasources:
            return "暂无可用数据源"

        context = ""
        for ds in datasources:
            table_name = f"{ds.get('schema_name', 'public')}.{ds.get('table_name', '')}"
            context += f"- 表: {table_name}\n"

            if ds.get('columns'):
                cols = [f"{c['name']} ({c.get('type', 'unknown')})" for c in ds['columns']]
                context += f"  字段: {', '.join(cols)}\n"

            if ds.get('spatial_metadata'):
                context += f"  空间字段: {ds['spatial_metadata'].get('geometry_column', 'geom')}\n"

        return context

    def _extract_sql(self, output: str) -> str:
        """
        从输出中提取 SQL

        Args:
            output: LLM 输出文本

        Returns:
            SQL 语句
        """
        # 尝试提取 SQL 代码块
        sql_match = re.search(r'```sql\s*\n(.*?)\n```', output, re.DOTALL | re.IGNORECASE)
        if sql_match:
            return sql_match.group(1).strip()

        # 尝试提取 SELECT 开头的语句
        sql_match = re.search(r'(SELECT.*?);?$', output, re.DOTALL | re.IGNORECASE)
        if sql_match:
            return sql_match.group(1).strip()

        # 返回整个输出
        return output.strip()


# 全局单例
sql_agent = SQLGenerationAgent()
