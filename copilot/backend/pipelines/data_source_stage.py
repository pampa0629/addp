"""
数据源理解（Pipeline 阶段）

使用 LLM 提取 + 手动 API 调用的方式，理解用户查询中所指的数据源
"""
import json
from typing import Optional

from langchain_core.messages import SystemMessage
from langchain_core.output_parsers import PydanticOutputParser
from pydantic import BaseModel, Field

from models.workflow_models import DataSourceContext, DataSourceLocation
from tools.develop_tools import EngineTool, SchemaTableTool, ObjectStorageTool
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


class EngineMatch(BaseModel):
    """引擎匹配结果"""
    engine_id: int = Field(description="匹配的引擎 ID")
    engine_name: str = Field(description="引擎名称")
    engine_type: str = Field(description="引擎类型")
    match_score: float = Field(description="匹配分数（0-1）")
    reason: str = Field(description="匹配理由")


class DataSourceStage:
    """
    数据源理解阶段

    流程：
    1. LLM 分析用户查询，提取关键信息
    2. 调用 get_engines API，获取所有引擎
    3. LLM 智能匹配最合适的引擎
    4. 验证表/对象是否存在
    5. 返回 DataSourceContext
    """

    def __init__(
        self,
        llm,
        engine_tool: EngineTool,
        schema_table_tool: SchemaTableTool,
        object_storage_tool: ObjectStorageTool,
        metadata_search_tool: MetadataSearchTool
    ):
        self.llm = llm
        self.engine_tool = engine_tool
        self.schema_table_tool = schema_table_tool
        self.object_storage_tool = object_storage_tool
        self.metadata_search_tool = metadata_search_tool

        self.query_parser = PydanticOutputParser(pydantic_object=QueryAnalysis)
        self.match_parser = PydanticOutputParser(pydantic_object=EngineMatch)

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

            # 步骤 3: LLM 智能匹配引擎
            print(f"\n[DataSourceStage] 步骤 3: 智能匹配引擎...")
            matched_engine = await self._match_engine(query, analysis, engines)

            print(f"[DataSourceStage] 匹配结果:")
            print(f"  引擎 ID: {matched_engine.engine_id}")
            print(f"  引擎名称: {matched_engine.engine_name}")
            print(f"  引擎类型: {matched_engine.engine_type}")
            print(f"  匹配分数: {matched_engine.match_score:.2f}")
            print(f"  匹配理由: {matched_engine.reason}")

            # 步骤 4: 验证数据源是否存在
            print(f"\n[DataSourceStage] 步骤 4: 验证数据源...")
            location, verified = await self._verify_data_source(matched_engine, analysis)

            # 步骤 5: 构造返回结果
            confidence = matched_engine.match_score * (0.9 if verified else 0.7)

            result = DataSourceContext(
                engine_id=matched_engine.engine_id,
                engine_name=matched_engine.engine_name,
                engine_type=matched_engine.engine_type,
                location=location,
                confidence=confidence,
                alternatives=[]
            )

            print(f"\n[DataSourceStage] ===== 数据源理解完成 =====")
            print(f"  引擎: {result.engine_name} ({result.engine_type})")
            print(f"  位置: {location}")
            print(f"  置信度: {result.confidence:.2f}")
            print(f"  验证状态: {'✅ 已验证' if verified else '⚠️ 未验证'}")
            print(f"=" * 60)

            return result

        except Exception as e:
            print(f"[DataSourceStage] ❌ 数据源理解失败: {type(e).__name__}: {e}")
            import traceback
            traceback.print_exc()
            return self._create_default_context()

    async def _analyze_query(self, query: str) -> QueryAnalysis:
        prompt = f"""你是一个数据源分析专家。分析用户的查询，提取数据源相关信息。

用户查询: {query}

请提取以下信息：
1. **引擎关键词**：用户提到的引擎相关词（如 "pg"、"mysql"、"minio"、"对象存储" 等）
2. **引擎类型推测**：根据关键词推测引擎类型（postgresql/mysql/doris/minio/s3 等）
3. **表名**：如果是关系数据库，提取表名
4. **Schema**：如果明确提到 schema，提取；否则默认 "public"
5. **Bucket**：如果是对象存储，提取 bucket 名称
6. **文件路径**：如果是对象存储，提取文件路径
7. **置信度**：你对分析结果的置信度（0-1）

**关键词映射规则**：
- "pg"、"postgres"、"postgresql" → postgresql
- "mysql" → mysql
- "doris" → doris
- "clickhouse" → clickhouse
- "minio"、"对象存储" → minio
- "s3" → s3

{self.query_parser.get_format_instructions()}

请直接返回 JSON，不要有其他内容。"""

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

    async def _match_engine(self, query: str, analysis: QueryAnalysis, engines: list) -> EngineMatch:
        engines_json = json.dumps(engines, ensure_ascii=False, indent=2)

        prompt = f"""你是一个数据源匹配专家。根据用户查询和引擎列表，选择最匹配的引擎。

**用户查询**: {query}

**查询分析结果**:
- 引擎关键词: {analysis.engine_keywords}
- 引擎类型推测: {analysis.engine_type_hint}
- 表名: {analysis.table_name}
- Schema: {analysis.schema_name}

**可用引擎列表**:
{engines_json}

**匹配规则**:
1. **优先精确匹配**：用户明确提到的引擎关键词（如 "pg库"）
2. **类型匹配**：引擎类型与推测类型一致
3. **名称相似度**：引擎名称与关键词的相似度
4. **唯一性**：如果某个类型只有一个引擎，优先选择
5. **描述匹配**：引擎描述与用户意图的匹配度

**输出要求**:
- 选择最匹配的一个引擎
- 给出匹配分数（0-1，越高越好）
- 解释匹配理由

{self.match_parser.get_format_instructions()}

请直接返回 JSON，不要有其他内容。"""

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

        return self.match_parser.parse(content)

    async def _verify_data_source(
        self,
        matched_engine: EngineMatch,
        analysis: QueryAnalysis
    ) -> tuple[DataSourceLocation, bool]:
        engine_type = matched_engine.engine_type

        if engine_type in ["postgresql", "mysql", "doris", "clickhouse"]:
            schema = analysis.schema_name or "public"
            table = analysis.table_name

            if table:
                try:
                    result = await self.schema_table_tool._arun(
                        engine_id=matched_engine.engine_id,
                        namespace=schema
                    )
                    tables = result.get("items", [])
                    table_names = [t.get("name") for t in tables]
                    verified = table in table_names
                    print(f"[DataSourceStage] 表验证: {table} in {schema} -> {'存在' if verified else '不存在'}")
                    return DataSourceLocation(schema=schema, table=table), verified
                except Exception as e:
                    print(f"[DataSourceStage] ⚠️ 表验证失败: {e}")
                    return DataSourceLocation(schema=schema, table=table), False
            else:
                return DataSourceLocation(schema=schema), False

        elif engine_type in ["minio", "s3", "oss"]:
            bucket = analysis.bucket_name
            path = analysis.file_path

            if bucket:
                try:
                    result = await self.object_storage_tool._arun(engine_id=matched_engine.engine_id)
                    buckets = result.get("buckets", [])
                    bucket_names = [b.get("name") for b in buckets]
                    verified = bucket in bucket_names
                    print(f"[DataSourceStage] Bucket 验证: {bucket} -> {'存在' if verified else '不存在'}")
                    return DataSourceLocation(bucket=bucket, path=path), verified
                except Exception as e:
                    print(f"[DataSourceStage] ⚠️ Bucket 验证失败: {e}")
                    return DataSourceLocation(bucket=bucket, path=path), False
            else:
                return DataSourceLocation(), False

        else:
            return DataSourceLocation(), False

    def _create_default_context(self) -> DataSourceContext:
        return DataSourceContext(
            engine_id=0,
            engine_name="unknown",
            engine_type="unknown",
            location=DataSourceLocation(),
            confidence=0.0,
            alternatives=[]
        )


def create_data_source_stage(
    llm,
    engine_tool: EngineTool,
    schema_table_tool: SchemaTableTool,
    object_storage_tool: ObjectStorageTool,
    metadata_search_tool: MetadataSearchTool
) -> DataSourceStage:
    return DataSourceStage(llm, engine_tool, schema_table_tool, object_storage_tool, metadata_search_tool)
