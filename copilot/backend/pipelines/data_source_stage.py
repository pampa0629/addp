"""
数据源理解（Pipeline 阶段）

使用 LLM 提取 + 手动 API 调用的方式，理解用户查询中所指的数据源
"""
from typing import Optional

from langchain_core.messages import SystemMessage
from langchain_core.output_parsers import PydanticOutputParser
from pydantic import BaseModel, Field

from models.workflow_models import DataSourceContext, DataSourceLocation
from pipelines.data_source_candidates import (
    build_data_source_candidates,
    metadata_search_query,
    metadata_type_filter,
)
from pipelines.data_source_prompts import build_query_analysis_prompt
from tools.develop_tools import EngineTool
from tools.meta_tools import MetadataSearchTool


class QueryAnalysis(BaseModel):
    """用户查询分析结果"""
    engine_keywords: list[str] = Field(description="引擎关键词（如 pg, mysql, minio）")
    engine_type_hint: Optional[str] = Field(description="推测的引擎类型（postgresql, mysql, minio 等）")
    table_name: Optional[str] = Field(description="表名")
    schema_name: Optional[str] = Field(default="public", description="Schema 名称")
    bucket_name: Optional[str] = Field(description="Bucket 名称")
    file_path: Optional[str] = Field(description="文件路径")
    confidence: float = Field(description="分析置信度（0-1）")


class DataSourceStage:
    """
    数据源理解阶段

    流程：
    1. LLM 分析用户查询，提取关键信息
    2. 调用 get_engines API，获取可作为数据源的通用引擎
    3. 通过 Manager 语义检索匹配已有资源
    4. 通过 Meta ancestors 校验 locator 事实
    5. 仅在唯一高置信候选时返回已解析上下文
    """

    def __init__(
        self,
        llm,
        engine_tool: EngineTool,
        metadata_search_tool: MetadataSearchTool
    ):
        self.llm = llm
        self.engine_tool = engine_tool
        self.metadata_search_tool = metadata_search_tool

        self.query_parser = PydanticOutputParser(pydantic_object=QueryAnalysis)

    async def understand(self, query: str, tenant_id: int = 1) -> DataSourceContext:
        """
        理解用户查询中的数据源

        Args:
            query: 用户查询
            tenant_id: 租户 ID

        Returns:
            DataSourceContext: 数据源上下文
        """
        print(f"\n[DataSourceStage] ===== 开始数据源理解 =====")
        print(f"  用户查询: {query}")
        print(f"  租户 ID: {tenant_id}")

        try:
            # 步骤 1: LLM 分析用户查询
            print(f"\n[DataSourceStage] 步骤 1: 分析用户查询...")
            analysis = await self._analyze_query(query)

            print(f"[DataSourceStage] 分析结果:")
            print(f"  引擎关键词: {analysis.engine_keywords}")
            print(f"  引擎类型推测: {analysis.engine_type_hint}")
            print(f"  表名: {analysis.table_name}")
            print(f"  Schema: {analysis.schema_name}")
            print(f"  分析置信度: {analysis.confidence:.2f}")

            # 步骤 2: 获取租户所有引擎
            print(f"\n[DataSourceStage] 步骤 2: 获取租户引擎列表...")
            engines = await self.engine_tool._arun(tenant_id=tenant_id)
            print(f"[DataSourceStage] 获取到 {len(engines)} 个引擎:")
            for eng in engines:
                print(f"  - ID:{eng['id']}, 名称:{eng['name']}, 类型:{eng['type']}")

            if not engines:
                print(f"[DataSourceStage] ⚠️ 租户没有任何引擎，返回默认值")
                return self._create_default_context()

            # 步骤 3-4: 语义检索并校验 Meta locator 事实
            print(f"\n[DataSourceStage] 步骤 3: 搜索并验证数据源...")
            candidates = await self._build_alternatives(query, analysis, engines, tenant_id)
            selected = self._select_candidate(candidates, metadata_search_query(query, analysis))

            if selected is None:
                result = self._create_default_context(candidates)
            else:
                result = DataSourceContext(
                    engine_id=selected.engine_id,
                    engine_name=selected.engine_name or "unknown",
                    engine_type=selected.engine_type,
                    location=selected.location,
                    confidence=selected.confidence or 0.0,
                    alternatives=[],
                )

            print(f"\n[DataSourceStage] ===== 数据源理解完成 =====")
            print(f"  引擎: {result.engine_name} ({result.engine_type})")
            print(f"  位置: {result.location}")
            print(f"  置信度: {result.confidence:.2f}")
            print(f"  候选项数量: {len(result.alternatives)}")
            print(f"  验证状态: {'✅ 已验证' if selected else '⚠️ 需要确认'}")
            print(f"=" * 60)

            return result

        except Exception as e:
            print(f"[DataSourceStage] ❌ 数据源理解失败: {type(e).__name__}: {e}")
            import traceback
            traceback.print_exc()
            raise

    async def _analyze_query(self, query: str) -> QueryAnalysis:
        prompt = build_query_analysis_prompt(query, self.query_parser.get_format_instructions())

        messages = [SystemMessage(content=prompt)]
        response = await self.llm.ainvoke(messages)

        content = response.content if hasattr(response, 'content') else str(response)
        content = content.strip()
        if content.startswith("```json"):
            content = content[7:]
        if content.startswith("```"):
            content = content[3:]
        if content.endswith("```"):
            content = content[:-3]
        content = content.strip()

        return self.query_parser.parse(content)

    async def _build_alternatives(
        self,
        query: str,
        analysis: QueryAnalysis,
        engines: list,
        tenant_id: int,
    ):
        search_query = metadata_search_query(query, analysis)
        type_filter = metadata_type_filter(analysis)

        results = await self.metadata_search_tool._arun(
            query=search_query,
            metadata_type=type_filter,
            tenant_id=tenant_id,
            limit=5,
        )

        candidates = build_data_source_candidates(
            results,
            engines,
            default_namespace=analysis.schema_name,
            max_candidates=5,
        )
        print(f"[DataSourceStage] 元数据候选项: {len(candidates)} 个")
        return candidates

    @staticmethod
    def _select_candidate(candidates, search_query):
        normalized_query = str(search_query or "").strip().casefold()
        exact_matches = [
            candidate for candidate in candidates
            if candidate.resource_name.strip().casefold() == normalized_query
        ]
        if len(exact_matches) == 1:
            return exact_matches[0].model_copy(update={
                "confidence": 1.0,
                "reason": "资源名称精确匹配",
            })
        if len(candidates) != 1:
            return None
        candidate = candidates[0]
        if (candidate.confidence or 0.0) < 0.8:
            return None
        return candidate

    def _create_default_context(self, alternatives=None) -> DataSourceContext:
        return DataSourceContext(
            engine_id=0,
            engine_name="unknown",
            engine_type="unknown",
            location=DataSourceLocation(),
            confidence=0.0,
            alternatives=alternatives or []
        )


def create_data_source_stage(
    llm,
    engine_tool: EngineTool,
    metadata_search_tool: MetadataSearchTool
) -> DataSourceStage:
    return DataSourceStage(llm, engine_tool, metadata_search_tool)
