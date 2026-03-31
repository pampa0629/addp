"""
ADDP Agent LangChain Tools
将现有的 ADDP HTTP API 客户端包装为 LangChain Tools，供 LLM 调用。
token 通过工厂函数的 closure 注入，不暴露给 LLM。
"""
import json
from typing import List, Optional
from langchain_core.tools import tool
from addp_common.client import MetaClient, ManagerClient, DevelopClient
from .copilot_client import CopilotClient
from .system_client import SystemClient
from config import settings


def _to_str(result) -> str:
    """将 API 返回值统一转为 JSON 字符串"""
    if isinstance(result, (dict, list)):
        return json.dumps(result, ensure_ascii=False, indent=2)
    return str(result)


def create_agent_tools(token: str, tenant_id: int = 1, user_id: int = 1) -> List:
    """
    工厂函数：注入认证 token 和用户上下文，返回 LangChain Tool 列表。
    生成的 tools 通过 closure 持有 token，LLM 调用时无需传入认证信息。
    """
    manager = ManagerClient(base_url=settings.get_manager_url(), user_token=token)
    meta = MetaClient(base_url=settings.get_meta_url(), user_token=token)
    develop = DevelopClient(base_url=settings.get_develop_url(), user_token=token)
    copilot = CopilotClient(base_url=settings.get_copilot_url(), user_token=token)
    system = SystemClient(base_url=settings.get_system_url(), user_token=token)

    @tool
    async def list_workflow_engines() -> str:
        """列出平台中所有支持工作流执行的引擎（python_workflow、spark_workflow 等）。
        调用 generate_workflow 前，先调用此工具了解可用的工作流引擎，
        根据任务规模和数据特征决定使用哪个引擎。
        返回每个引擎的 id、name、engine_type 和连接状态。"""
        result = await system.get_workflow_engines()
        return _to_str(result)

    @tool
    async def list_engines() -> str:
        """列出当前用户可访问的所有存储引擎（数据库、对象存储等）。
        返回每个引擎的 ID、名称、类型（PostgreSQL/MySQL/MinIO 等）和连接状态。
        需要 engine_id 时，先调用此工具获取。"""
        result = await manager.get_engines()
        return _to_str(result)

    @tool
    async def list_objects(engine_id: int) -> str:
        """列出指定存储引擎下的所有资源（schema、表、bucket、文件等），以树形结构返回。
        参数:
          engine_id: 存储引擎 ID，从 list_engines 获取。
        返回结果包含每个节点的 locator URI，可直接用于 preview_data。"""
        result = await manager.get_engine_tree(engine_id=engine_id, expand_depth=3)
        return _to_str(result)

    @tool
    async def list_tables(engine_id: int) -> str:
        """列出指定存储引擎中的所有数据表（表名清单）。
        适用于"有哪些表"、"列出所有表"、"这个库有几张表"等场景。
        参数:
          engine_id: 存储引擎 ID，从 list_engines 获取。
        返回每张表的名称、所属 schema 和 locator URI，不含 bucket/文件等非表资源。"""
        result = await manager.get_engine_tree(engine_id=engine_id, expand_depth=3)

        tables = []

        def extract_tables(node):
            if not isinstance(node, dict):
                return
            if node.get("type") == "table":
                tables.append({
                    "name": node.get("label", ""),
                    "locator": node.get("locator", ""),
                })
            for child in node.get("children") or []:
                extract_tables(child)

        if isinstance(result, dict):
            extract_tables(result)
        elif isinstance(result, list):
            for node in result:
                extract_tables(node)

        return _to_str({"count": len(tables), "tables": tables})

    @tool
    async def preview_data(locator: str) -> str:
        """预览数据对象的内容（表格字段、样本数据行等）。
        参数:
          locator: ResourceLocator URI，格式为
            addp://engine/{engine_id}/path/{schema}/{table}?type=table （数据库表）
            addp://engine/{engine_id}/path/{bucket}/{file_path}?type=object （对象存储文件）
          可从 list_objects 返回的树节点中获取 locator 字段。"""
        result = await manager.preview_by_locator(locator)
        return _to_str(result)

    @tool
    async def search_metadata(keyword: str, limit: int = 10) -> str:
        """搜索平台元数据资产（表、文件、数据集等）。
        参数:
          keyword: 搜索关键词（名称、描述、标签等）
          limit: 最多返回结果数，默认 10"""
        result = await meta.search_metadata(keyword=keyword, limit=limit)
        return _to_str(result)

    @tool
    async def list_metadata(page: int = 1, size: int = 20) -> str:
        """列出平台中所有已注册的元数据资产，支持分页。
        参数:
          page: 页码，默认 1
          size: 每页数量，默认 20"""
        result = await meta.list_metadata(page=page, size=size)
        return _to_str(result)

    async def _get_python_workflow_engine():
        """内部辅助：获取在线的 python_workflow 引擎实例"""
        engines = await system.get_workflow_engines()
        online = [e for e in engines if e.get("connection_status") == "online" and e.get("is_active")]
        return next(
            (e for e in online if e["engine_type"] == "python_workflow"),
            next((e for e in engines if e["engine_type"] == "python_workflow"), None)
        )

    @tool
    async def generate_workflow(description: str) -> str:
        """将自然语言描述转换为 GIS 工作流 DAG。
        适用于空间分析任务：缓冲区分析、叠加分析、面积统计、空间关系计算等需要 GIS 算子的场景。
        参数:
          description: 用自然语言描述的空间分析任务
        返回包含 status 和 workflow 字段的 JSON：
          status=success 时，将 workflow 字段的值（JSON 字符串）传给 run_workflow 执行；
          status=need_clarification 时需告知用户补充信息；
          status=error/validation_failed 时说明生成失败原因。"""
        engine = await _get_python_workflow_engine()
        if not engine:
            return _to_str({"status": "error", "message": "未找到可用的 Python 工作流引擎，请联系管理员检查引擎配置"})

        result = await copilot.generate_workflow(
            query=description,
            tenant_id=tenant_id,
            user_id=user_id,
            engine_type=engine["engine_type"],
            workflow_engine_id=engine["id"],
        )

        return _to_str(result)

    @tool
    async def run_workflow(workflow_json: str) -> str:
        """提交工作流到执行引擎运行，并等待结果（最多 120 秒）。
        参数:
          workflow_json: generate_workflow 返回结果中 workflow 字段的 JSON 字符串
            （注意：是 workflow 字段的值，不是整个返回结果）
        返回执行结果，包含 status（success/failed/timeout）和 result 字段。"""
        try:
            workflow = json.loads(workflow_json) if isinstance(workflow_json, str) else workflow_json
        except Exception:
            return _to_str({"error": "workflow_json 解析失败，请传入 generate_workflow 返回结果中 workflow 字段的 JSON 字符串"})

        if not workflow or not isinstance(workflow, dict):
            return _to_str({"error": "workflow_json 无效，请先调用 generate_workflow 生成工作流"})

        engine = await _get_python_workflow_engine()
        if not engine:
            return _to_str({"error": "未找到可用的 Python 工作流引擎"})

        start = await develop.run_workflow_content(workflow, engine_id=engine["id"])
        execution_id = start.get("execution_id")
        if not execution_id:
            return _to_str({"error": "工作流提交失败", "details": start})

        execution_result = await develop.wait_for_execution(execution_id)
        return _to_str(execution_result)

    @tool
    async def execute_sql(sql: str, engine_id: int) -> str:
        """在指定存储引擎上执行 SQL 查询，返回结果表格。
        适用于：查看工作流写入数据库的计算结果、验证数据、统计分析等。
        参数:
          sql: SQL 查询语句（SELECT）
          engine_id: 数据存储引擎 ID（PostgreSQL/MySQL 等），从 list_engines 获取
        返回包含 columns（列名）和 rows（数据行）的 JSON。"""
        result = await develop.execute_sql(sql=sql, engine_id=engine_id)
        return _to_str(result)

    return [
        list_workflow_engines,
        list_engines,
        list_objects,
        list_tables,
        preview_data,
        search_metadata,
        list_metadata,
        generate_workflow,
        run_workflow,
        execute_sql,
    ]
