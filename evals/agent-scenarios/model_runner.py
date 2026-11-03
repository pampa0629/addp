"""Real Inference + production Agent routing/Harness, isolated synthetic owners.

Run explicitly through make test-agent-model. Never a T4 or release baseline.
"""
from __future__ import annotations

import argparse
import asyncio
import copy
import hashlib
import json
import os
import sys
import time
import uuid
from contextlib import aclosing
from pathlib import Path
from unittest.mock import patch

from jsonschema import Draft202012Validator
from langchain_core.callbacks import BaseCallbackHandler

REPO_ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(REPO_ROOT / 'common-python'))
sys.path.insert(0, str(REPO_ROOT / 'agent/backend'))

from evaluator import EvaluationFailure, evaluate_trace, load_scenario, phase_from_events
from gate import _external_path, _source_identity, _utc_now

SCHEMA = 'addp.agent-model-regression/v1'


class ModelIdentityRecorder(BaseCallbackHandler):
    """Keep only deployment hashes/version, never messages or provider metadata."""
    def __init__(self):
        self.identities = set()
        self.routing = []

    def on_llm_end(self, response, **kwargs):
        for batch in response.generations:
            for generation in batch:
                metadata = getattr(getattr(generation, 'message', None), 'response_metadata', {})
                for call in getattr(getattr(generation, 'message', None), 'tool_calls', []):
                    if call['name'] == 'RouteDecision':
                        from agents.main_agent import _get_skill_registry
                        arguments = call['args']
                        skill = arguments.get('skill')
                        action = arguments.get('draft_action')
                        self.routing.append({
                            'skill': skill if skill in _get_skill_registry() else ('none' if skill is None else 'unknown'),
                            'draft_action': action if action in {'inspect', 'create', None} else 'unknown',
                            'needs_platform_semantics': arguments.get('needs_platform_semantics') is True,
                            'has_draft_ref': bool(arguments.get('draft_ref')),
                        })
                deployment = metadata.get('deployment_id')
                version = metadata.get('profile_version')
                if isinstance(deployment, str) and deployment and type(version) is int and version > 0:
                    self.identities.add((hashlib.sha256(deployment.encode()).hexdigest(), version))


def _contract_digest(name):
    digest = hashlib.sha256()
    for path in [Path(__file__).with_name(name) / 'scenario.yaml',
                 Path(__file__).with_name('model_cases.py'),
                 REPO_ROOT / 'ontology/backend/internal/platform/transfer.json',
                 REPO_ROOT / 'common-python/addp_common/tools/manifest.json']:
        digest.update(path.read_bytes())
    return digest.hexdigest()


def fixture_executor(fixture, violations):
    """Use production Manifest input/output checks; no real owner handlers survive."""
    from addp_common.tools import ToolExecutionError, ToolExecutor, get_tool, load_manifest

    class FixtureExecutor(ToolExecutor):
        def __init__(self, base_url, source_token):
            super().__init__(base_url, source_token)
            self._handlers = {definition.name: self._response(definition.name)
                              for definition in load_manifest().tools}

        def _response(self, name):
            async def respond(arguments, delegated_token):
                for key, value in fixture['expected'].get(name, {}).items():
                    if arguments.get(key) != value:
                        raise ToolExecutionError('model_parameters_changed', 'fixture parameter mismatch')
                # The check request is compared in full, including omitted/extra fields.
                if name == 'transfer.task.config.check' and arguments != fixture['expected'][name]:
                    raise ToolExecutionError('model_parameters_changed', 'fixture parameter mismatch')
                if name == 'query.draft.generate':
                    from model_cases import QUERY_LOCATOR
                    resources = arguments['resources']
                    if len(resources) != 1 or resources[0]['locator'] != QUERY_LOCATOR or resources[0]['engine_id'] != 321:
                        raise ToolExecutionError('model_parameters_changed', 'fixture resource mismatch')
                if 'source_parent' in fixture and name in {'resource.ancestors.get', 'resource.children.list'}:
                    from model_cases import transfer_facts
                    _, source, target, _, _ = transfer_facts()
                    source_parent = fixture['source_parent']
                    engine_id = arguments['engine_id']
                    if name == 'resource.ancestors.get':
                        expected = {311: source['locator'], 312: target['locator']}
                        if arguments['locator'] != expected.get(engine_id):
                            raise ToolExecutionError('model_parameters_changed', 'fixture ancestor mismatch')
                        return {'locator': arguments['locator'], 'ancestors': [copy.deepcopy(
                            source_parent if engine_id == 311 else target)]}
                    parents = {311: source_parent, 312: target}
                    parent = parents.get(engine_id)
                    if not parent or arguments.get('parent_locator') not in {None, parent['locator']}:
                        raise ToolExecutionError('model_parameters_changed', 'fixture parent mismatch')
                    return copy.deepcopy(parent) if arguments.get('parent_locator') else {'children': [copy.deepcopy(parent)]}
                return copy.deepcopy(fixture['responses'][name])
            return respond

        async def _issue_delegated_token(self, definition, **kwargs):
            # This token is local fixture data and is never sent over HTTP.
            return 'synthetic-fixture-delegation'

        async def call(self, name, arguments=None, **kwargs):
            try:
                definition = get_tool(name)
                if definition.risk == 'write' or name not in fixture['responses']:
                    raise ToolExecutionError('model_tool_blocked', 'tool not admitted by fixture')
                return await super().call(name, arguments, **kwargs)
            except ToolExecutionError as exc:
                violations.append(exc.code)
                raise
            except KeyError:
                violations.append('model_tool_blocked')
                raise ToolExecutionError('model_tool_blocked', 'unknown fixture tool') from None

    return FixtureExecutor


async def run_case(name, tenant_id, model, *, timeout=180):
    from agents.main_agent import stream_agent_response
    from model_cases import case_fixture, transfer_facts
    from services.drafts import DRAFT_INSPECT
    from protocol.a2ui import transfer_config_check_presentation

    scenario = load_scenario(Path(__file__).with_name(name))
    fixture = case_fixture(name)
    violations, events, body = [], [], []
    identity_recorder = ModelIdentityRecorder()
    if hasattr(model, 'callbacks'):
        model = model.model_copy(update={'callbacks': [*(model.callbacks or []), identity_recorder]})
    started = time.monotonic()
    first_text_ms = None
    error_code = None
    draft_reads = []
    snapshot = fixture['snapshot']
    prompt = scenario['prompt']
    if name == 'model-transfer-precheck':
        prompt += '\n已确认一文档一行，members整体保留为json，目标覆盖策略已选择。完整原参数：\n'
        prompt += json.dumps(transfer_facts()[3], ensure_ascii=False)

    async def draft_reader(reference):
        draft_reads.append(reference)
        if snapshot is None or reference != snapshot['reference']:
            violations.append('model_draft_reference_changed')
            raise ValueError('model_draft_reference_changed')
        return copy.deepcopy(snapshot)

    async def deny_owner_http(*args, **kwargs):
        violations.append('model_owner_network_blocked')
        raise RuntimeError('model_owner_network_blocked')

    async def collect():
        nonlocal first_text_ms
        async with aclosing(stream_agent_response(
            messages=[{'role': 'user', 'content': prompt}], user_id=1, tenant_id=tenant_id,
            token='synthetic-no-user-access', agent_run_id=str(uuid.uuid4()),
            draft_catalog=([{'name': snapshot['name'], 'reference': snapshot['reference']}]
                           if snapshot else []), draft_reader=draft_reader,
        )) as stream:
            text_index = 0
            async for event in stream:
                events.append(event)
                if event.checkpoint_committed is not None and not event.checkpoint_committed.done():
                    # In-process evaluation acknowledges control only; there is no DB commit.
                    event.checkpoint_committed.set_result(None)
                if event.kind == 'text':
                    text_index += 1
                    # Historical inspect emits a fixed notice and separator before model text.
                    if not snapshot or text_index > 2:
                        body.append(event.payload.get('delta', ''))
                        if first_text_ms is None:
                            first_text_ms = round((time.monotonic() - started) * 1000)

    with (patch('agents.main_agent.get_llm', return_value=model),
          patch('tools.langchain_tools.ToolExecutor', fixture_executor(fixture, violations)),
          patch('addp_common.client.base.BaseClient._request', new=deny_owner_http)):
        try:
            await asyncio.wait_for(collect(), timeout=timeout)
        except TimeoutError:
            error_code = 'model_case_timeout'
        except Exception:
            # Exception text may contain private endpoints or model/owner content.
            error_code = 'model_case_error'

    actual_skill = next((event.payload['skill_name'] for event in events if event.kind == 'run_state'), 'none')
    status = 'failed' if error_code or any(e.kind == 'run_failed' for e in events) else (
        'waiting' if any(e.kind == 'interaction_required' for e in events) else 'completed')
    phase = phase_from_events('model', 'synthetic-run', status, events,
                              owner_effects={'tasks_created': 0, 'executions_created': 0}, persisted_state={})
    try:
        # draft.inspect is a private historical read, not a Manifest Tool.
        evaluate_trace(scenario, {'skill': actual_skill, 'phases': [phase]})
        trace_passed = True
    except EvaluationFailure:
        trace_passed = False
    text = ''.join(body)
    report_preserved = True
    if name in {'model-transfer-precheck', 'model-historical-draft'}:
        expected_report = transfer_config_check_presentation(transfer_facts()[4])['report']
        reports = [e.payload.get('report') for e in events
                   if e.kind == 'presentation' and e.payload.get('kind') == 'transfer_config_check']
        report_preserved = reports == [expected_report]
    assertions = {
        'trace_contract': trace_passed,
        'no_blocked_calls': not violations,
        'model_body_nonempty': bool(text.strip()),
        'body_fact_anchors': all(fact in text for fact in fixture['body_facts']),
        'report_preserved': report_preserved,
        'historical_read_only': (draft_reads == [snapshot['reference']]
                                 and [t['name'] for t in phase['tools']] == [DRAFT_INSPECT]) if snapshot else True,
        'model_identity_observed': bool(identity_recorder.identities),
    }
    return {
        'name': name, 'contract_sha256': _contract_digest(name), 'skill': actual_skill,
        'status': 'passed' if not error_code and all(assertions.values()) else 'failed',
        'error_code': error_code, 'violation_codes': sorted(set(violations)), 'assertions': assertions,
        'tools': _safe_tools(phase['tools']),
        'routing': identity_recorder.routing,
        'model_identities': [{'deployment_sha256': deployment, 'profile_version': version}
                             for deployment, version in sorted(identity_recorder.identities)],
        'duration_ms': round((time.monotonic() - started) * 1000), 'first_text_ms': first_text_ms,
    }


def _safe_tools(tools):
    from addp_common.tools import load_manifest
    known = {t.name for t in load_manifest().tools} | {'draft.inspect', 'request_clarification', 'write_todos'}
    codes = {code for t in load_manifest().tools for code in t.errors} | {
        'invalid_tool_arguments', 'model_parameters_changed', 'model_tool_blocked',
        'platform_condition_unsatisfied', 'tool_not_allowed', 'draft_reference_unavailable',
    }
    return [{'name': t['name'] if t['name'] in known else 'unknown', 'status': t['status'],
             'error_code': t['error_code'] if t['error_code'] in codes or t['error_code'] is None else 'runtime_error'}
            for t in tools]


def validate_report(report):
    """Exact schema prevents accidental inclusion of prompts, arguments or raw errors."""
    def obj(properties):
        return {'type': 'object', 'properties': properties, 'required': list(properties),
                'additionalProperties': False}
    from addp_common.tools import load_manifest
    from model_cases import CASE_NAMES
    from agents.main_agent import _get_skill_registry
    string = {'type': 'string'}
    digest = {'type': 'string', 'pattern': '^[a-f0-9]{64}$'}
    duration = {'type': 'integer', 'minimum': 0}
    error = {'enum': [None, 'model_case_error', 'model_case_timeout']}
    tool_names = [t.name for t in load_manifest().tools] + ['draft.inspect', 'request_clarification', 'write_todos', 'unknown']
    error_codes = sorted({c for t in load_manifest().tools for c in t.errors} | {
        'invalid_tool_arguments', 'model_parameters_changed', 'model_tool_blocked',
        'platform_condition_unsatisfied', 'tool_not_allowed', 'draft_reference_unavailable', 'runtime_error',
    })
    sample = obj({
        'name': {'enum': list(CASE_NAMES)}, 'sample': {'type': 'integer', 'minimum': 1, 'maximum': 3},
        'contract_sha256': digest, 'skill': {'enum': [*_get_skill_registry(), 'none']},
        'status': {'enum': ['passed', 'failed']}, 'error_code': error,
        'violation_codes': {'type': 'array', 'items': {'enum': [*error_codes, 'model_owner_network_blocked', 'model_draft_reference_changed']}},
        'assertions': obj({key: {'type': 'boolean'} for key in (
            'trace_contract', 'no_blocked_calls', 'model_body_nonempty', 'body_fact_anchors',
            'report_preserved', 'historical_read_only', 'model_identity_observed')}),
        'tools': {'type': 'array', 'items': obj({'name': {'enum': tool_names},
                  'status': {'enum': ['started', 'succeeded', 'failed']}, 'error_code': {'enum': [None, *error_codes]}})},
        'model_identities': {'type': 'array', 'items': obj({'deployment_sha256': digest,
                             'profile_version': {'type': 'integer', 'minimum': 1}})},
        'routing': {'type': 'array', 'items': obj({
            'skill': {'enum': [*_get_skill_registry(), 'none', 'unknown']},
            'draft_action': {'enum': [None, 'inspect', 'create', 'unknown']},
            'needs_platform_semantics': {'type': 'boolean'}, 'has_draft_ref': {'type': 'boolean'},
        })},
        'duration_ms': duration, 'first_text_ms': {'anyOf': [duration, {'type': 'null'}]},
    })
    schema = obj({
        'schema': {'const': SCHEMA}, 'created_at': string,
        'source': obj({'revision': {'type': 'string', 'pattern': '^[a-f0-9]{40}$'}, 'worktree_dirty': {'type': 'boolean'}}),
        'mode': {'const': 'real_model_synthetic_owners'}, 'status': {'enum': ['passed', 'failed']},
        'repetitions': {'type': 'integer', 'minimum': 1, 'maximum': 3},
        'answer_review': {'const': 'required'}, 'error_code': {'enum': [None, 'model_runtime_unavailable']},
        'samples': {'type': 'array', 'items': sample},
    })
    if list(Draft202012Validator(schema).iter_errors(report)):
        raise ValueError('invalid_model_report')
    if report['status'] == 'passed' and (report['error_code'] or not report['samples']
            or any(s['status'] != 'passed' or not all(s['assertions'].values()) for s in report['samples'])):
        raise ValueError('invalid_model_report_status')
    if report['status'] == 'passed':
        expected = {(name, repetition) for name in CASE_NAMES for repetition in range(1, report['repetitions'] + 1)}
        actual = [(s['name'], s['sample']) for s in report['samples']]
        if set(actual) != expected or len(actual) != len(expected):
            raise ValueError('incomplete_model_report')


def write_report(path, report):
    """Create each sample artifact once; never erase a previous failed run."""
    path = _external_path(str(path))
    validate_report(report)
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(f'.{path.name}.{uuid.uuid4().hex}.tmp')
    try:
        temporary.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
        temporary.chmod(0o600)
        # Hard link publishes a complete file atomically and refuses existing targets.
        os.link(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


async def run_suite(tenant_id, *, repeat=1, timeout=180):
    from model_cases import CASE_NAMES
    from utils.llm import AgentInferenceService, get_llm
    report = {'schema': SCHEMA, 'created_at': _utc_now(), 'source': _source_identity(),
              'mode': 'real_model_synthetic_owners', 'status': 'failed',
              'repetitions': repeat,
              'answer_review': 'required', 'error_code': None, 'samples': []}
    try:
        from config import settings
        if not settings.AGENT_SERVICE_CLIENT_SECRET:
            raise ValueError('missing_agent_service_credentials')
        AgentInferenceService.initialize()
        for repetition in range(1, repeat + 1):
            for name in CASE_NAMES:
                print(json.dumps({'case': name, 'sample': repetition, 'status': 'running'}), flush=True)
                sample = await run_case(name, tenant_id, get_llm(tenant_id, 'reasoning'), timeout=timeout)
                sample['sample'] = repetition
                report['samples'].append(sample)
                print(json.dumps({'case': name, 'sample': repetition, 'status': sample['status']}), flush=True)
        report['status'] = 'passed' if all(s['status'] == 'passed' for s in report['samples']) else 'failed'
    except Exception:
        report['error_code'] = 'model_runtime_unavailable'
    finally:
        await AgentInferenceService.close()
        from database import engine
        await engine.dispose()
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True)
    parser.add_argument('--tenant-id', type=int, required=True)
    parser.add_argument('--repeat', type=int, default=1, choices=range(1, 4))
    args = parser.parse_args()
    try:
        path = _external_path(args.output)
        if path.exists() or args.tenant_id <= 0:
            raise ValueError('model_output_exists_or_invalid_tenant')
        # Only the explicit live entry loads configuration. Tests never load personal .env.
        from dotenv import dotenv_values
        if not os.environ.get('POSTGRES_PORT'):
            raise ValueError('verified_postgres_port_required')
        values = {**dotenv_values(REPO_ROOT / '.env'),
                  **dotenv_values(REPO_ROOT / '.dev-state/ports.env')}
        for key, value in values.items():
            if value is not None:
                os.environ.setdefault(key, value)
        report = asyncio.run(run_suite(args.tenant_id, repeat=args.repeat))
        write_report(path, report)
        print(json.dumps({'status': report['status'], 'samples': len(report['samples'])}), flush=True)
        return 0 if report['status'] == 'passed' else 1
    except Exception:
        print(json.dumps({'status': 'failed', 'error_code': 'model_regression_setup_failed'}))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
