import asyncio

from langchain_core.messages import AIMessage

from chains.resource_recommendation_chain import ResourceRecommendationChain


class FakeLLM:
    async def ainvoke(self, messages):
        assert "不得仅凭 object 或 array 容器字段推断未列出的嵌套字段" in messages[0].content
        assert "只能排序和推荐" in messages[0].content
        assert "必须先理解用户需求" in messages[0].content
        assert '"user_query": "分析耕地候选"' in messages[1].content
        assert '"search_queries": ["耕地", "farmland"]' in messages[1].content
        assert '"score": 0.95' in messages[1].content
        return AIMessage(content='''{
            "recommendations": [{
                "role": "耕地",
                "ranked_locators": [
                    "addp://engine/9/path/public/farmland?type=table&item_id=71",
                    "addp://engine/99/path/public/farmland?type=table&item_id=999"
                ],
                "recommended_locator": "addp://engine/9/path/public/farmland?type=table&item_id=71",
                "recommendation_reason": "名称精确匹配且具有 Polygon 几何列"
            }]
        }''')


def test_resource_recommendation_ranks_known_candidates_without_dropping_others():
    candidates = [
        {
            "role": "耕地",
            "name": "farmland",
            "locator": "addp://engine/8/path/public/farmland?type=table&item_id=61",
            "engine_name": "spatial-a",
            "asset_type": "table",
            "score": 0.95,
            "fields": [],
        },
        {
            "role": "耕地",
            "name": "farmland",
            "locator": "addp://engine/9/path/public/farmland?type=table&item_id=71",
            "engine_name": "spatial-b",
            "asset_type": "table",
            "score": 0.94,
            "fields": [],
        },
        {
            "role": "耕地",
            "name": "farmland_history",
            "locator": "addp://engine/10/path/public/farmland_history?type=table&item_id=81",
            "engine_name": "archive",
            "asset_type": "table",
            "score": 0.8,
            "fields": [],
        },
    ]

    recommendations = asyncio.run(ResourceRecommendationChain(FakeLLM()).recommend(
        candidates,
        query="分析耕地候选",
        search_queries=["耕地", "farmland"],
    ))

    recommendation = recommendations["耕地"]
    assert recommendation.ranked_locators == [
        "addp://engine/9/path/public/farmland?type=table&item_id=71",
        "addp://engine/8/path/public/farmland?type=table&item_id=61",
        "addp://engine/10/path/public/farmland_history?type=table&item_id=81",
    ]
    assert recommendation.recommended_locator == (
        "addp://engine/9/path/public/farmland?type=table&item_id=71"
    )
    assert recommendation.recommendation_reason == "名称精确匹配且具有 Polygon 几何列"
