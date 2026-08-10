import asyncio
import json

from chains.notebook_resource_recommendation_chain import NotebookResourceRecommendationChain


class CapturingLLM:
    def __init__(self):
        self.messages = None

    async def ainvoke(self, messages):
        self.messages = messages
        return type("Response", (), {"content": (
            '{"recommendations":[{"role":"耕地空间范围",'
            '"ranked_candidate_ids":["candidate-0"],'
            '"recommended_candidate_id":"candidate-0"}]}'
        )})()


def test_recommendation_limits_model_shortlist_but_keeps_all_candidates_known():
    llm = CapturingLLM()
    candidates = [
        {
            "candidate_id": f"candidate-{index}",
            "role": "耕地空间范围",
            "name": f"farmland_{index}",
            "engine_name": "Business PostgreSQL",
            "engine_type": "postgresql",
            "path_names": ["public"],
            "term": "table",
            "kind": "table",
        }
        for index in range(80)
    ]

    recommendations = asyncio.run(
        NotebookResourceRecommendationChain(llm).recommend("计算耕地面积", candidates)
    )

    payload = json.loads(llm.messages[1].content)
    assert len(payload["candidates"]["耕地空间范围"]) == 32
    assert recommendations["耕地空间范围"].ranked_candidate_ids == [
        f"candidate-{index}" for index in range(32)
    ]


def test_recommendation_replaces_obvious_derived_output_with_base_resource():
    class DerivedChoosingLLM:
        async def ainvoke(self, _messages):
            return type("Response", (), {"content": (
                '{"recommendations":[{"role":"铁路线路范围",'
                '"ranked_candidate_ids":["derived", "railway"],'
                '"recommended_candidate_id":"derived",'
                '"recommendation_reason":"名称匹配"}]}'
            )})()

    candidates = [
        {
            "candidate_id": "derived",
            "role": "铁路线路范围",
            "name": "railway_buffer50_farmland_area",
            "engine_name": "Business PostgreSQL",
            "engine_type": "postgresql",
            "path_names": ["public", "railway_buffer50_farmland_area"],
            "term": "table",
            "kind": "table",
        },
        {
            "candidate_id": "railway",
            "role": "铁路线路范围",
            "name": "railway",
            "engine_name": "Business PostgreSQL",
            "engine_type": "postgresql",
            "path_names": ["public", "railway"],
            "term": "table",
            "kind": "table",
        },
    ]
    recommendations = asyncio.run(
        NotebookResourceRecommendationChain(DerivedChoosingLLM()).recommend(
            "计算铁路两边宽度50米所占用的耕地面积", candidates
        )
    )
    recommendation = recommendations["铁路线路范围"]
    assert recommendation.recommended_candidate_id == "railway"
    assert recommendation.recommendation_reason


def test_recommendation_preserves_role_omitted_by_model():
    class RailwayOnlyLLM:
        async def ainvoke(self, _messages):
            return type("Response", (), {"content": (
                '{"recommendations":[{"role":"铁路线路范围",'
                '"ranked_candidate_ids":["railway"],'
                '"recommended_candidate_id":"railway"}]}'
            )})()

    candidates = [
        {
            "candidate_id": "railway",
            "role": "铁路线路范围",
            "name": "railway",
            "engine_name": "Business PostgreSQL",
            "engine_type": "postgresql",
            "path_names": ["public", "railway"],
            "term": "table",
            "kind": "table",
        },
        {
            "candidate_id": "farmland-derived",
            "role": "耕地范围",
            "name": "railway_50m_farmland_area",
            "engine_name": "Business PostgreSQL",
            "engine_type": "postgresql",
            "path_names": ["public", "railway_50m_farmland_area"],
            "term": "table",
            "kind": "table",
        },
        {
            "candidate_id": "farmland",
            "role": "耕地范围",
            "name": "farmland",
            "engine_name": "Business PostgreSQL",
            "engine_type": "postgresql",
            "path_names": ["public", "farmland"],
            "term": "table",
            "kind": "table",
        },
    ]

    recommendations = asyncio.run(
        NotebookResourceRecommendationChain(RailwayOnlyLLM()).recommend(
            "计算铁路两边宽度50米所占用的耕地面积", candidates
        )
    )

    farmland = recommendations["耕地范围"]
    assert farmland.ranked_candidate_ids == ["farmland-derived", "farmland"]
    assert farmland.recommended_candidate_id == "farmland"
    assert farmland.recommendation_reason
