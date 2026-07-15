"""Prompt builders for Copilot data source understanding."""

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
