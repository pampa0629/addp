"""Prompt builders for Copilot data source understanding."""

import json
from typing import Any


def build_query_analysis_prompt(query: str, format_instructions: str) -> str:
    return f"""你是一个数据源分析专家。分析用户的查询，提取数据源相关信息。

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

{format_instructions}

请直接返回 JSON，不要有其他内容。"""


def build_engine_match_prompt(
    query: str,
    analysis: Any,
    engines: list[dict[str, Any]],
    format_instructions: str,
) -> str:
    engines_json = json.dumps(engines, ensure_ascii=False, indent=2)

    return f"""你是一个数据源匹配专家。根据用户查询和引擎列表，选择最匹配的引擎。

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

{format_instructions}

请直接返回 JSON，不要有其他内容。"""
