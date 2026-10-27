#!/usr/bin/env python3
"""Five Standard identities and domain overview through real owner APIs.

An isolated preparation User publishes an external-domain fixture. The consumer
only reads that standard; its complete response is checked again after cleanup.
Published history is destroyed with the disposable Hosted deployment.
"""
from __future__ import annotations

import hashlib
import json
from datetime import datetime, timedelta, timezone
import os
import sys
import time
from importlib import import_module
from urllib.parse import urlencode

_API = import_module('scripts.utils.online-api')
GatewayClient, SuiteError = _API.GatewayClient, _API.SuiteError
require_positive_int = _API.require_positive_int
REQUIRED_USER_PERMISSIONS = {'catalog.entry.read', 'catalog.inventory.read', 'model.entity.read', 'model.entity.create', 'model.entity.delete',
 'standard.domain.create', 'standard.domain.read', 'standard.domain.delete'}
for _kind in ('glossary', 'element', 'code_set', 'document', 'metric'):
    REQUIRED_USER_PERMISSIONS.update(f'standard.{_kind}.{action}' for action in ('create', 'read', 'delete'))
PREPARER_PERMISSIONS = {'standard.domain.create', 'standard.domain.read', 'standard.element.create', 'standard.element.read', 'standard.element.update', 'standard.element.publish'}


def fixture_prefix(run_id):
    # Domain codes are bounded to 50 characters; preserve a collision-resistant Run ID link.
    return 'cat_' + hashlib.sha256(run_id.encode()).hexdigest()[:24]


def validate_identity(client, tenant, permissions):
    facts = _API.validate_user_identity(client, tenant, required_permissions=permissions)
    context = client.request('GET', '/api/v1/system/auth/context', (200,)).payload
    actual = {permission for assignment in context['authorization']['role_assignments'] for permission in assignment['permissions']}
    if actual != permissions:
        raise SuiteError('Catalog acceptance User must have exactly its declared permissions')
    principal_id = str(context['principal'].get('id', ''))
    if not principal_id.isdecimal() or int(principal_id) <= 0:
        raise SuiteError('Catalog acceptance User must have a stable Principal ID')
    return facts, principal_id


def prepare_reference(client, run_id, *, unit_id=None):
    prefix = fixture_prefix(run_id) + '_source'
    domain = client.request('POST', '/api/v1/standard/domains', (201,), {'name': prefix, 'code': prefix}).payload
    domain_id = require_positive_int(domain, 'id')
    element = client.request('POST', '/api/v1/standard/elements', (201,), {
        'scope_type': 'domain', 'owner_domain_id': domain_id, 'code': prefix, 'name': prefix,
        'definition': 'Disposable external standard for Catalog T4', 'tags': [],
        'data_type': 'string', 'value_domain_kind': 'unrestricted', 'nullable': True,
        **({'unit_id': unit_id} if unit_id is not None else {}),
        'effective_from': (datetime.now(timezone.utc) - timedelta(days=1)).isoformat(),
    }).payload
    element_id = require_positive_int(element, 'id')
    revision_id = require_positive_int(element['latest_revision'], 'id')
    for action in ('submit', 'publish'):
        element = client.request('POST', f'/api/v1/standard/elements/{element_id}/revisions/{revision_id}/{action}', (200,), {'version': require_positive_int(element, 'version')}).payload
    if str(element.get('owner_domain_id')) != str(domain_id) or element.get('current_revision', {}).get('status') != 'published':
        raise SuiteError('external reference fixture is not an effective domain standard')
    return element_id



def check_overview(payload, expected_types):
    if payload.get('view') != 'inventory' or payload.get('total_entries') != len(expected_types):
        raise SuiteError('domain inventory total differs from its exact source identities')
    counts = payload.get('entry_types')
    if not isinstance(counts, list) or {row.get('entry_type'): row.get('count') for row in counts} != {key: 1 for key in expected_types}:
        raise SuiteError('domain inventory type counts differ from source identities')
    if any(type(row.get('count')) is not int for row in counts):
        raise SuiteError('domain inventory counts must be integers')
    coverage = payload.get('coverage')
    if not isinstance(coverage, dict) or coverage.get('total_entries') != len(expected_types):
        raise SuiteError('domain coverage differs from resource counts')


def cleanup(client, paths):
    errors = []
    for path in reversed(paths):
        try:
            _API.cleanup_resource(client, path)
        except Exception as error:
            errors.append(f'{path}: {error}')
    if errors:
        raise SuiteError('cleanup failed: ' + '; '.join(errors))


def run_suite(client, reader, tenant_id, run_id, reference_element_id):
    prefix = fixture_prefix(run_id)
    paths = []
    reference_path = f'/api/v1/standard/elements/{reference_element_id}'
    reference = client.request('GET', reference_path, (200,)).payload
    if reference.get('lifecycle_state') != 'active' or not isinstance(reference.get('current_revision'), dict):
        raise SuiteError('configured reference Element must have a current effective revision')
    _API.assert_tenant(reference, tenant_id, 'read-only reference Element')
    expected_types = {'glossary', 'element', 'code_set', 'standard_document', 'metric', 'business_entity'}
    identity_bindings = []
    report = None
    try:
        domain = client.request('POST', '/api/v1/standard/domains', (201,), {'name': prefix, 'code': prefix, 'description': 'Run-scoped domain overview acceptance', 'sort_order': 0}).payload
        domain_id = require_positive_int(domain, 'id')
        paths.append(f'/api/v1/standard/domains/{domain_id}')
        if reference.get('scope_type') != 'domain' or not reference.get('owner_domain_id') or str(reference['owner_domain_id']) == str(domain_id):
            raise SuiteError('reference Element must belong to a different exact domain')
        for kind, route in [('glossary', 'glossaries'), ('element', 'elements'), ('code_set', 'code-sets'), ('document', 'documents'), ('metric', 'metrics')]:
            body = {'scope_type': 'domain', 'owner_domain_id': domain_id, 'code': prefix + '_' + kind, 'name': prefix + ' ' + kind, 'tags': []}
            if kind in ('glossary', 'element', 'metric'):
                body['definition'] = 'Run-scoped standard definition'
            if kind == 'element':
                body.update(data_type='string', value_domain_kind='unrestricted', nullable=True)
            if kind == 'code_set':
                body.update(description='Run-scoped code set', value_type='string')
            if kind == 'document':
                body.update(doc_type='internal', description='Run-scoped document without uploaded content')
            if kind == 'metric':
                body.update(metric_type='atomic', statistical_caliber='Acceptance fixture only')
            payload = client.request('POST', '/api/v1/standard/' + route, (201,), body).payload
            resource_id = require_positive_int(payload, 'id')
            paths.append(f'/api/v1/standard/{route}/{resource_id}')
            identity_bindings.append({'source_module': 'standard', 'source_type': kind, 'source_identity': str(resource_id)})
        entity = client.request('POST', '/api/v1/model/entities', (201,), {'domain_id': domain_id, 'code': prefix, 'name': prefix, 'description': 'Run-scoped standard consumer'}).payload
        entity_id = require_positive_int(entity, 'id')
        entity_path = f'/api/v1/model/entities/{entity_id}'
        paths.append(entity_path)
        client.request('POST', entity_path + '/attributes', (201,), {'version': require_positive_int(entity, 'version'), 'element_id': reference_element_id, 'name': 'Reference attribute', 'column_name': 'reference_attribute', 'data_type': 'string', 'is_pk': False, 'nullable': True, 'sort_order': 0})
        overview_path = f'/api/v1/catalog/domains/{domain_id}/overview?view=inventory'
        deadline = time.monotonic() + 90
        while True:
            # Enroll this precise Tenant in the existing background source synchronizer.
            client.request('GET', '/api/v1/catalog/entries?' + urlencode({'view': 'inventory', 'primary_domain_id': domain_id}), (200,))
            overview = client.request('GET', overview_path, (200,)).payload
            if overview.get('total_entries') == 6:
                break
            if time.monotonic() >= deadline:
                raise SuiteError('Standard/Model catalog source synchronization did not converge')
            time.sleep(.5)
        check_overview(overview, expected_types)
        first = client.request('POST', '/api/v1/catalog/entries/resolve-sources', (200,), {'references': identity_bindings}).payload
        second = client.request('POST', '/api/v1/catalog/entries/resolve-sources', (200,), {'references': identity_bindings}).payload
        def stable_ids(payload):
            rows = payload.get('results', [])
            if len(rows) != 5 or any(not row.get('found') or not row.get('entry', {}).get('id') for row in rows):
                raise SuiteError('five Standard identities were not resolved')
            return [row['entry']['id'] for row in rows]
        if stable_ids(first) != stable_ids(second):
            raise SuiteError('source identity resolution was not stable')
        references = client.request('GET', '/api/v1/model/entities/standard_references?' + urlencode({'domain_id': domain_id, 'page': 1, 'page_size': 20}), (200,)).payload
        if references.get('total') != 1 or references['data'][0]['source_identity'] != str(reference_element_id):
            raise SuiteError('Model did not expose its explicit standard reference')
        evidence = references['data'][0].get('evidence', [])
        if not any(row.get('resource_id') == str(entity_id) and row.get('owner_module') == 'model' and row.get('resource_type') == 'entity' and row.get('position') == 'Reference attribute' and row.get('relation_kind') == 'model.entity.element' and row.get('state') == 'draft' and not row.get('revision_id') for row in evidence):
            raise SuiteError('Model draft reference was presented as a frozen adoption')
        external_reference = {'source_module': 'standard', 'source_type': 'element', 'source_identity': str(reference_element_id)}
        resolved = client.request('POST', '/api/v1/catalog/entries/resolve-sources', (200,), {'references': [external_reference]}).payload
        targets = resolved.get('results', [])
        if len(targets) != 1 or not targets[0].get('found') or targets[0].get('scope_type') != 'domain' or str(targets[0].get('owner_domain_id')) != str(reference['owner_domain_id']):
            raise SuiteError('Catalog did not resolve the exact external standard ownership')
        reader.request('GET', '/api/v1/catalog/entries?' + urlencode({'view': 'inventory', 'primary_domain_id': domain_id}), (403,))
        invisible = reader.request('POST', '/api/v1/catalog/entries/resolve-sources', (200,), {'references': identity_bindings + [external_reference]}).payload
        if len(invisible.get('results', [])) != 6 or any(row.get('found') or row.get('entry') for row in invisible['results']):
            raise SuiteError('directory-only reader discovered inventory source identities')
        for route in ('model/entities', 'model/logical-tables', 'model/metric-implementations', 'standard/glossaries', 'standard/elements', 'standard/metrics'):
            reader.request('GET', '/api/v1/' + route + '/standard_references?' + urlencode({'domain_id': domain_id, 'page': 1, 'page_size': 20}), (403,))
        own_references = reader.request('GET', '/api/v1/catalog/domain_standard_references?' + urlencode({'domain_id': domain_id, 'view': 'governance', 'page': 1, 'page_size': 20}), (200,)).payload
        if own_references.get('total') != 0 or own_references.get('data'):
            raise SuiteError('governance references leaked inventory-only facts')
        reader.request('GET', overview_path, (403,))
        governed = reader.request('GET', f'/api/v1/catalog/domains/{domain_id}/overview?view=governance', (200,)).payload
        if governed.get('total_entries') != 0 or governed.get('coverage') is not None:
            raise SuiteError('ordinary directory reader discovered inventory-only drafts')
        report = {'schema_version': 'addp.online-suite/v1', 'suite': 'catalog-domain-overview', 'run_id': run_id, 'tenant_id': str(tenant_id), 'five_standard_types': True, 'stable_source_identities': True, 'model_reference_evidence': True, 'reader_inventory_denied': True, 'reader_professional_references_denied': True, 'reader_source_resolution_hidden': True, 'external_domain_ownership': True, 'reference_element_unchanged': False, 'cleanup': 'pending'}
    finally:
        try:
            cleanup(client, paths)
        finally:
            current = client.request('GET', reference_path, (200,)).payload
            if current != reference:
                raise SuiteError('read-only configured reference Element changed during acceptance')
    report.update(reference_element_unchanged=True, cleanup='passed', residual_professional_resources=0, catalog_history='retained')
    return report


def main():
    try:
        if os.environ.get('ADDP_ONLINE_TEST') != '1':
            raise SuiteError('ADDP_ONLINE_TEST must be exactly 1')
        for key, value in {'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'ADDP_ONLINE_HOSTED': '1', 'POSTGRES_DB': 'addp_online'}.items():
            if os.environ.get(key) != value:
                raise SuiteError('Catalog T4 requires the disposable Hosted profile: ' + key)
        tenant = int(os.environ['ADDP_ONLINE_TEST_TENANT_ID'])
        timeout = float(os.environ.get('ADDP_ONLINE_TEST_HTTP_TIMEOUT_SECONDS', '10'))
        if tenant <= 0 or timeout <= 0:
            raise SuiteError('Tenant and HTTP timeout must be positive')
        token = os.environ['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN']
        reader_token = os.environ['ADDP_ONLINE_CATALOG_READER_ACCESS_TOKEN']
        preparer_token = os.environ['ADDP_ONLINE_CATALOG_PREPARER_ACCESS_TOKEN']
        identities = [validate_identity(GatewayClient(os.environ['SYSTEM_URL'], value, timeout, 'SYSTEM_URL'), tenant, permissions)
                      for value, permissions in ((token, REQUIRED_USER_PERMISSIONS), (reader_token, {'catalog.entry.read'}), (preparer_token, PREPARER_PERMISSIONS))]
        if len({principal for _, principal in identities}) != 3:
            raise SuiteError('consumer, reader and preparer must be three distinct Users')
        identity = identities[0][0]
        reference_id = prepare_reference(GatewayClient(os.environ['GATEWAY_URL'], preparer_token, timeout), os.environ['ADDP_ONLINE_TEST_RUN_ID'])
        report = run_suite(GatewayClient(os.environ['GATEWAY_URL'], token, timeout), GatewayClient(os.environ['GATEWAY_URL'], reader_token, timeout), tenant, os.environ['ADDP_ONLINE_TEST_RUN_ID'], reference_id)
        report['three_minimal_users_verified'] = True
        report['retained_source_history'] = 'destroy_with_hosted_deployment'
        report['identity'] = identity
        json.dump(report, sys.stdout, ensure_ascii=False, sort_keys=True); sys.stdout.write('\n')
        return 0
    except (KeyError, ValueError, SuiteError) as error:
        print(f'Catalog domain overview Online failed: {error}', file=sys.stderr)
        return 1

if __name__ == '__main__':
    raise SystemExit(main())
