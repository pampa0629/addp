"""
ADDP Agent LangChain Tools
将现有的 ADDP HTTP API 客户端包装为 LangChain Tools，供 LLM 调用。
token 通过工厂函数的 closure 注入，不暴露给 LLM。
"""
import json
from typing import List
from langchain_core.tools import tool
from .manager_client import ManagerClient
from .meta_client import MetaClient
from .develop_client import DevelopClient
from .copilot_client import CopilotClient
from .system_client import SystemClient


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
    manager = ManagerClient(token)
    meta = MetaClient(token)
    develop = DevelopClient(token)
    copilot = CopilotClient(token)
    system = SystemClient(token)

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

    @tool
    async def generate_workflow(description: str) -> str:
        """将自然语言描述转换为 GIS 工作流 DAG。
        适用于空间分析任务：缓冲区分析、叠加分析、面积统计、空间关系计算等需要 GIS 算子的场景。
        参数:
          description: 用自然语言描述的空间分析任务
        返回包含 status、workflow 和 engine_id 字段的 JSON：
          status=success 时 workflow 字段为工作流 DAG，engine_id 为选定的执行引擎 ID，
            将整个返回结果传给 run_workflow 执行；
          status=need_clarification 时需告知用户补充信息；
          status=error/validation_failed 时说明生成失败原因。"""
        # 自动从 system 查询并选择合适的工作流引擎
        engine = await system.select_workflow_engine(description)
        if not engine:
            return _to_str({"status": "error", "message": "未找到可用的工作流引擎，请联系管理员检查引擎配置"})

        workflow_engine_id = engine["id"]
        engine_type = engine["engine_type"].replace("_workflow", "")  # python_workflow → python

        result = await copilot.generate_workflow(
            query=description,
            tenant_id=tenant_id,
            user_id=user_id,
            engine_type=engine_type,
            workflow_engine_id=workflow_engine_id,
        )

        # 把 engine_id 注入返回结果，供 run_workflow 使用
        if isinstance(result, dict):
            result["engine_id"] = workflow_engine_id
            result["engine_name"] = engine["name"]

        return _to_str(result)

    @tool
    async def run_workflow(generate_workflow_result: str) -> str:
        """提交工作流到执行引擎运行，并等待结果（最多 120 秒）。
        参数:
          generate_workflow_result: generate_workflow 工具返回的完整 JSON 字符串
            （包含 workflow 和 engine_id 字段，直接传入 generate_workflow 的完整输出即可）
        返回执行结果，包含 status（success/failed/timeout）和 result 字段。"""
        try:
            result = json.loads(generate_workflow_result) if isinstance(generate_workflow_result, str) else generate_workflow_result
        except Exception:
            return _to_str({"error": "参数解析失败，请传入 generate_workflow 返回的完整 JSON 字符串"})

        workflow = result.get("workflow")
        engine_id = result.get("engine_id")

        if not workflow:
            return _to_str({"error": "缺少 workflow 字段，请先调用 generate_workflow 生成工作流"})
        if not engine_id:
            return _to_str({"error": "缺少 engine_id 字段，请使用 generate_workflow 返回的完整结果"})

        start = await develop.run_workflow_content(workflow, engine_id=engine_id)
        execution_id = start.get("execution_id")
        if not execution_id:
            return _to_str({"error": "工作流提交失败", "details": start})

        execution_result = await develop.wait_for_execution(execution_id)
        return _to_str(execution_result)

    return [
        list_engines,
        list_objects,
        list_tables,
        preview_data,
        search_metadata,
        list_metadata,
        generate_workflow,
        run_workflow,
    ]
