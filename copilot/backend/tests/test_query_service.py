import pytest

from services.query_service import QueryService


def test_parse_output_accepts_structured_query_candidate():
    result = QueryService._parse_output('''```json
    {
      "query": "SELECT SUM(ST_Area(ST_Intersection(r.buffer, f.geometry))) FROM railway r JOIN farmland f ON ST_Intersects(r.buffer, f.geometry)",
      "explanation": "计算相交面积",
      "warnings": []
    }
    ```''')

    assert result["query"].startswith("SELECT SUM")
    assert result["explanation"] == "计算相交面积"


def test_parse_output_rejects_internal_locator():
    with pytest.raises(ValueError, match="internal resource facts"):
        QueryService._parse_output('''{
          "query": "SELECT * FROM 'addp://engine/8/path/public/railway?type=table'",
          "explanation": "",
          "warnings": []
        }''')
