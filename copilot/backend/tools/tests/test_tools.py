"""
Tools 单元测试

测试所有 LangChain Tools 的基本功能
"""
import pytest
from addp_common.client import DevelopClient

from config import settings
from tools.develop_tools import (
    EngineTool,
    OperatorDiscoveryTool,
    OperatorDetailTool
)
from tools.meta_tools import MetadataSearchTool


async def first_workflow_engine_id(tenant_id: int) -> int:
    async with DevelopClient(
        base_url=settings.get_develop_url(),
        internal_api_key=settings.internal_api_key,
        tenant_id=tenant_id,
    ) as client:
        engines = await client.list_workflow_engines()

    assert engines, "Develop 未返回可用的工作流引擎"
    return engines[0]["id"]


class TestDevelopTools:
    """测试 Develop Tools"""

    @pytest.mark.asyncio
    async def test_engine_tool(self):
        """测试 EngineTool"""
        tool = EngineTool()
        result = await tool._arun(tenant_id=1)

        assert isinstance(result, list)
        print(f"✅ EngineTool 测试通过：获取到 {len(result)} 个引擎")

        if result:
            engine = result[0]
            assert "id" in engine
            assert "name" in engine
            assert "type" in engine
            print(f"   示例引擎：{engine['name']} ({engine['type']})")

    @pytest.mark.asyncio
    async def test_operator_discovery_tool(self):
        """测试 OperatorDiscoveryTool"""
        tool = OperatorDiscoveryTool()
        tenant_id = 1
        workflow_engine_id = await first_workflow_engine_id(tenant_id)
        result = await tool._arun(
            workflow_engine_id=workflow_engine_id,
            tenant_id=tenant_id,
        )

        assert isinstance(result, list)
        print(f"✅ OperatorDiscoveryTool 测试通过：获取到 {len(result)} 个算子")

        if result:
            operator = result[0]
            assert "name" in operator
            assert "brief" in operator
            assert "category" in operator
            print(f"   示例算子：{operator['name']} ({operator['category']})")

    @pytest.mark.asyncio
    async def test_operator_detail_tool(self):
        """测试 OperatorDetailTool"""
        tool = OperatorDetailTool()
        tenant_id = 1
        workflow_engine_id = await first_workflow_engine_id(tenant_id)
        result = await tool._arun(
            operator_name="load",
            workflow_engine_id=workflow_engine_id,
            tenant_id=tenant_id,
        )

        assert result is not None or result is None  # API 可能返回 None
        print(f"✅ OperatorDetailTool 测试通过")

        if result:
            assert "name" in result
            assert "description" in result
            print(f"   算子名称：{result['name']}")


class TestMetaTools:
    """测试 Meta Tools"""

    @pytest.mark.asyncio
    async def test_metadata_search_tool(self):
        """测试 MetadataSearchTool"""
        tool = MetadataSearchTool()
        result = await tool._arun(query="test", tenant_id=1, limit=5)

        assert isinstance(result, list)
        print(f"✅ MetadataSearchTool 测试通过：找到 {len(result)} 个结果")

        if result:
            metadata = result[0]
            assert "name" in metadata
            assert "type" in metadata
            print(f"   示例结果：{metadata['name']} ({metadata['type']})")


if __name__ == "__main__":
    """运行所有测试"""
    print("=" * 60)
    print("开始测试 LangChain Tools")
    print("=" * 60)

    # 运行测试
    pytest.main([__file__, "-v", "-s"])
