import asyncio

import pytest

from services import notebook_service as notebook_service_module
from services.notebook_service import NotebookService


class CapturingLLM:
    def __init__(self, content):
        self.content = content
        self.messages = None

    async def ainvoke(self, messages):
        self.messages = messages
        return type("Response", (), {"content": self.content})()


def test_notebook_parse_rejects_engine_sql():
    with pytest.raises(ValueError, match="engine.sql"):
        NotebookService._parse_output('''{
          "code": "from addp_common.notebook import engines\\nresult = engines.client(8).sql('SELECT 1', max_rows=1, timeout=30)",
          "explanation": "",
          "warnings": [],
          "display_labels": ["结果"]
        }''', display_language="zh-CN")


def test_notebook_parse_rejects_positional_path_expansion():
    with pytest.raises(ValueError, match="invalid table call"):
        NotebookService._parse_output('''{
          "code": "from addp_common.notebook import engines\\nresult = engines.client(8).table(*path_segments)",
          "explanation": "",
          "warnings": [],
          "display_labels": ["结果"]
        }''', display_language="zh-CN")


def test_notebook_parse_rejects_unsupported_memory_limit_unit():
    with pytest.raises(ValueError, match="memory_limit unit"):
        NotebookService._parse_output('''{
          "code": "from addp_common.notebook import engines\\nresult = engines.client(8).table(schema='public', name='roads').to_pandas(memory_limit='1GB')\\nresult = {'结果': [1]}",
          "explanation": "",
          "warnings": [],
          "display_labels": ["结果"]
        }''', display_language="zh-CN")


def test_notebook_parse_rejects_display_labels_missing_from_code():
    with pytest.raises(ValueError, match="display labels"):
        NotebookService._parse_output('''{
          "code": "from addp_common.notebook import engines\\nresult = engines.client(8)",
          "explanation": "",
          "warnings": [],
          "display_labels": ["耕地占用面积（平方米）"]
        }''', display_language="zh-CN")


def test_notebook_parse_requires_square_metre_and_hectare_for_area_request():
    with pytest.raises(ValueError, match="square-metre and hectare"):
        NotebookService._parse_output('''{
          "code": "from addp_common.notebook import engines\\nresult = {'铁路占用面积（平方米）': [1]}",
          "explanation": "",
          "warnings": [],
          "display_labels": ["铁路占用面积（平方米）"]
        }''', display_language="zh-CN", require_area_units=True)


def test_notebook_generation_requires_geopandas_and_localized_result_labels(monkeypatch):
    llm = CapturingLLM('''{
      "code": "from addp_common.notebook import engines\\nimport pandas as pd\\nrailway = engines.client(8).table(schema='public', name='railway').to_geopandas(memory_limit='1GiB', geometry_column='shape', crs='EPSG:32650')\\nfarmland = engines.client(8).table(schema='public', name='farmland').to_geopandas(memory_limit='1GiB', geometry_column='land_shape', crs='EPSG:32650')\\nresult = pd.DataFrame({'耕地占用面积（平方米）': [1.0], '耕地占用面积（公顷）': [0.0001]})\\nresult",
      "explanation": "使用 GeoPandas 计算",
      "warnings": [],
      "display_labels": ["耕地占用面积（平方米）", "耕地占用面积（公顷）"]
    }''')
    monkeypatch.setattr(
        notebook_service_module.CopilotInferenceService,
        "chat_model",
        lambda *_args, **_kwargs: llm,
    )

    result = asyncio.run(NotebookService().generate(
        query="计算铁路两边宽度50米所占用的耕地面积",
        kernel="python3",
        resources=[{
            "role": "铁路",
            "engine_id": 8,
            "path_segments": [
                {"term": "schema", "name": "public"},
                {"term": "table", "name": "railway"},
            ],
            "geometry_column": "shape",
            "crs": "EPSG:32650",
        }],
        tenant_id=1,
        db=None,
    ))

    assert ".sql(" not in result["code"]
    assert ".to_geopandas(" in result["code"]
    assert "耕地占用面积（平方米）" in result["code"]
    assert '"display_language": "zh-CN"' in llm.messages[1].content
    assert "不得生成 engine.sql" in llm.messages[0].content
