import asyncio

from langchain_core.messages import AIMessage

from chains.resource_intent_chain import ResourceIntent, ResourceIntentChain, ResourceIntentScope


class FakeLLM:
    async def ainvoke(self, messages, *, response_schema):
        assert response_schema.strict is True
        assert "距离、单位、算子" in messages[0].content
        assert "查询条件中的实体值或属性值也不是独立数据项" in messages[0].content
        assert "最常见的英文单词形式" in messages[0].content
        assert messages[1].content == "计算铁路两边宽度50米所占用的耕地面积"
        return AIMessage(content='''{
            "resources": [
                {"role": "铁路", "search_queries": ["铁路", " railway "]},
                {"role": "耕地", "search_queries": ["耕地"]},
                {"role": "铁路", "search_queries": ["铁路"]}
            ]
        }''')


def test_resource_intent_chain_extracts_each_input_and_merges_duplicate_roles():
    intents = asyncio.run(ResourceIntentChain(FakeLLM()).extract(
        "计算铁路两边宽度50米所占用的耕地面积"
    ))

    assert [intent.model_dump() for intent in intents] == [
        {"role": "铁路", "search_queries": ["铁路", "railway"]},
        {"role": "耕地", "search_queries": ["耕地"]},
    ]


class ExpansionLLM:
    async def ainvoke(self, messages, *, response_schema):
        assert response_schema.strict is True
        assert "此前检索词均未召回候选" in messages[0].content
        assert '"role": "耕地"' in messages[1].content
        assert '"agricultural land"' in messages[1].content
        return AIMessage(content='''{
            "resources": [{
                "role": "耕地",
                "search_queries": ["agricultural land", "farmland", "farm land"]
            }]
        }''')


def test_resource_intent_chain_expands_missing_role_with_only_new_queries():
    expanded = asyncio.run(ResourceIntentChain(ExpansionLLM()).expand_missing(
        "计算铁路两边宽度50米所占用的耕地面积",
        [ResourceIntent(
            role="耕地",
            search_queries=["耕地", "cultivated land", "cropland", "agricultural land"],
        )],
    ))

    assert [intent.model_dump() for intent in expanded] == [{
        "role": "耕地",
        "search_queries": ["farmland", "farm land"],
    }]


class TransferSourceOnlyLLM:
    async def ainvoke(self, messages, *, response_schema):
        assert response_schema.strict is True
        assert "这是 Transfer 任务描述，只提取‘从/源’一侧" in messages[0].content
        return AIMessage(content='''{
            "resources": [{"role": "farmland", "search_queries": ["farmland"]}]
        }''')


def test_resource_intent_chain_can_extract_only_transfer_source():
    intents = asyncio.run(ResourceIntentChain(TransferSourceOnlyLLM()).extract(
        "从 pg 到 mysql，同步 farmland",
        scope=ResourceIntentScope.TRANSFER_SOURCE,
    ))

    assert [intent.model_dump() for intent in intents] == [{
        "role": "farmland",
        "search_queries": ["farmland"],
    }]
