"""Synthetic owner facts for real-model regression; no live business identifiers."""
from __future__ import annotations

import copy
import json
import uuid
from pathlib import Path

from agents.checkpoint import capture_owner_facts, new_checkpoint
from agents.platform_conditions import capture_config_check
from models.run_step import AgentRunStep
from services.drafts import draft_snapshot

ROOT = Path(__file__).resolve().parents[2]
CASE_NAMES = (
    'model-engine-inventory', 'model-query-draft',
    'model-transfer-precheck', 'model-historical-draft',
)
QUERY_LOCATOR = 'addp://engine/321/path/public/orders?type=table&item_id=3210'
QUERY_TEXT = 'SELECT status, COUNT(*) AS total FROM public.orders GROUP BY status'


def transfer_facts():
    definition = json.loads((ROOT / 'ontology/backend/internal/platform/transfer.json').read_text())
    definition['digest'] = 'a' * 64
    source = {
        'locator': 'addp://engine/311/path/demo/events?type=collection&item_id=3110',
        'engine_id': 311, 'source_engine_type': 'mongodb', 'item_type': 'collection',
        'item_id': 3110, 'data_type': 'table', 'full_name': 'demo.events',
        'item_fingerprint': 'synthetic-events', 'scanned_depth': 'table',
        'query_references': {'mql': {'parts': ['events']}}, 'schema_coverage': 'sampled',
        'fields': [{'name': '_id', 'type': 'string'}, {'name': 'members', 'type': 'array'}],
    }
    parent = {'locator': 'addp://engine/312/path/demo?type=schema&node_id=3120',
              'type': 'schema', 'children': []}
    arguments = {'name': 'model regression draft', 'config': {
        'runtime': {'boundary': 'bounded'}, 'load': {'mode': 'snapshot'},
        'source': {'locator': source['locator'], 'data_type': 'table', 'representation': 'native',
                   'query': {'language': 'mql', 'statement': json.dumps({'aggregate': 'events', 'pipeline': [
                       {'$project': {'id': '$_id', 'members': 1, '_id': 0}}]})}},
        'target': {'parent_locator': parent['locator'], 'name': 'new_events', 'data_type': 'table',
                   'representation': 'native', 'policy': {'apply_mode': 'replace'}},
        'transforms': [{'type': 'field_mapping', 'version': 'v1', 'mode': 'project', 'fields': [
            {'source': 'id', 'target': 'id', 'target_type': 'string'},
            {'source': 'members', 'target': 'members', 'target_type': 'json'}]}],
    }}
    report = {
        'schema_version': 'addp.transfer_task_config_check/v1', 'tenant_id': 1,
        'principal_id': 'synthetic-principal', 'config_hash': 'c' * 64, 'binding_hash': 'b' * 64,
        'observed_at': '2026-11-03T00:00:00Z', 'status': 'unknown',
        'checked': ['request_shape'], 'not_checked': ['business_rows'],
        'runtime_checks_required': ['per_value_encoding'], 'facts': [],
        'diagnostics': [{'status': 'unknown', 'code': 'source_query_not_checked',
                         'check': 'schema', 'config_path': '/config/source'}],
    }
    return definition, source, parent, arguments, report


def historical_snapshot():
    definition, source, parent, arguments, report = transfer_facts()
    checkpoint = new_checkpoint()
    for name, result in [('platform.capability.context', definition), ('resource.facts.get', source),
                         ('resource.children.list', parent)]:
        capture_owner_facts(name, result, checkpoint)
    capture_config_check(arguments, report, checkpoint)
    return draft_snapshot(AgentRunStep(
        id=uuid.UUID('00000000-0000-4000-8000-000000000001'), status='completed',
        tool_name='transfer.task.config.check', input_data=arguments,
        facts={'config_checks': [checkpoint['observed']['config_checks']['transfer.task.create']]},
    ))


def case_fixture(name):
    """Exact permitted responses and parameter constraints; never a live API fallback."""
    if name not in CASE_NAMES:
        raise ValueError('unknown_model_case')
    definition, source, parent, arguments, report = transfer_facts()
    result = {'responses': {}, 'expected': {}, 'snapshot': None, 'body_facts': []}
    if name == 'model-engine-inventory':
        result['responses'] = {'engine.list': [
            {'id': 301, 'name': 'VECTOR_FIXTURE_ONLINE', 'engine_type': 'geopython_workflow',
             'lifecycle_state': 'active', 'connection_status': 'online'},
            {'id': 302, 'name': 'VECTOR_FIXTURE_OFFLINE', 'engine_type': 'supermap_workflow',
             'lifecycle_state': 'active', 'connection_status': 'offline'},
        ]}
        result['expected'] = {'engine.list': {'capability': 'workflow'}}
        result['body_facts'] = ['VECTOR_FIXTURE_ONLINE', 'VECTOR_FIXTURE_OFFLINE']
    elif name == 'model-query-draft':
        fact = {'locator': QUERY_LOCATOR, 'engine_id': 321, 'item_id': 3210,
                'source_engine_type': 'postgresql', 'item_type': 'table', 'full_name': 'public.orders',
                'data_type': 'table', 'item_fingerprint': 'synthetic-orders', 'scanned_depth': 'table',
                'query_references': {'sql': {'parts': ['public', 'orders']}},
                'fields': [{'name': 'status', 'type': 'string'}], 'schema_coverage': 'complete'}
        result['responses'] = {
            'resource.ancestors.get': {'locator': QUERY_LOCATOR, 'ancestors': []},
            'resource.facts.get': fact,
            'query.draft.generate': {'status': 'ok', 'query': QUERY_TEXT, 'query_language': 'sql',
                                     'query_parameters': []},
        }
        result['expected'] = {
            'resource.ancestors.get': {'engine_id': 321, 'locator': QUERY_LOCATOR},
            'resource.facts.get': {'locator': QUERY_LOCATOR},
            'query.draft.generate': {'engine_id': 321, 'query_language': 'sql'},
        }
        result['body_facts'] = ['public.orders', 'status']
    elif name == 'model-transfer-precheck':
        source_parent = {'locator': 'addp://engine/311/path/demo?type=database&node_id=3111',
                         'type': 'database', 'children': [source]}
        result['responses'] = {
            'platform.capabilities.list': {'schema_version': 'addp.platform-capability-catalog/v1',
                                            'capabilities': [definition]},
            'platform.capability.context': definition, 'resource.facts.get': source,
            'resource.children.list': parent, 'transfer.task.config.check': report,
            'resource.ancestors.get': {'ancestors': [source_parent]},
            'engine.list': [
                {'id': 311, 'name': 'SOURCE_FIXTURE', 'engine_type': 'mongodb',
                 'lifecycle_state': 'active', 'connection_status': 'online'},
                {'id': 312, 'name': 'TARGET_FIXTURE', 'engine_type': 'postgresql',
                 'lifecycle_state': 'active', 'connection_status': 'online'},
            ],
        }
        result['source_parent'] = source_parent
        result['expected'] = {
            'platform.capability.context': {'capability': 'transfer.task.create'},
            'resource.facts.get': {'locator': source['locator']},
            'engine.list': {'capability': 'all'},
            'transfer.task.config.check': arguments,
        }
        result['body_facts'] = ['unknown']
    else:
        result['snapshot'] = historical_snapshot()
        result['body_facts'] = ['members']
    return copy.deepcopy(result)
