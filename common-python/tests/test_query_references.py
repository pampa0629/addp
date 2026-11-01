import pytest
from pydantic import ValidationError

from addp_common.resources import ResourceFact, ResourceQueryReference, query_reference_parts


def test_query_reference_retains_raw_parts_and_never_parses_a_flat_name():
    fact = ResourceFact(role='source', locator='addp://engine/1/path/a/b?type=table&item_id=2',
                        query_references={'sql': {'parts': [' 原.schema" ', ' 订单.table` ']}})
    assert query_reference_parts(fact.model_dump(), 'sql') == (' 原.schema" ', ' 订单.table` ')
    with pytest.raises(ValidationError):
        ResourceFact(role='source', locator=fact.locator, query_names={'sql': 'schema.table'})
    for value in ({'parts': []}, {'parts': ['']}, {'parts': ['x\0']}, {'parts': ['x'], 'connection_info': {}}, 'schema.table'):
        with pytest.raises(ValidationError):
            ResourceQueryReference.model_validate(value)


def test_mql_collection_dot_is_one_exact_part_and_invalid_facts_have_no_fallback():
    assert query_reference_parts({'query_references': {'mql': {'parts': [' Persons.v2 ']}}}, 'mql') == (' Persons.v2 ',)
    assert query_reference_parts({'full_name': 'db.collection', 'query_names': {'mql': 'collection'}}, 'mql') is None
    assert query_reference_parts({'query_references': {'sql': {'parts': 'schema.table'}}}, 'sql') is None
