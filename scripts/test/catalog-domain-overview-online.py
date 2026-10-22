#!/usr/bin/env python3
"""Five Standard identities and domain overview through real owner APIs.

Only run-scoped drafts and a Model entity are created. The configured published
reference Element is read-only and is checked again after cleanup.
"""
from __future__ import annotations

import json
import os
import sys
import time
from functools import partial
from importlib import import_module
from urllib.parse import urlencode

_API = import_module('scripts.utils.online-api')
GatewayClient, SuiteError = _API.GatewayClient, _API.SuiteError
require_positive_int = _API.require_positive_int
REQUIRED_USER_PERMISSIONS = {'catalog.entry.read', 'catalog.inventory.read', 'model.entity.read', 'model.entity.create', 'model.entity.delete',
 'standard.domain.create', 'standard.domain.read', 'standard.domain.delete'}
for _kind in ('glossary', 'element', 'code_set', 'document', 'metric'):
    REQUIRED_USER_PERMISSIONS.update(f'standard.{_kind}.{action}' for action in ('create', 'read', 'delete'))
validate_user_identity = partial(_API.validate_user_identity, required_permissions=REQUIRED_USER_PERMISSIONS)


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
    prefix = 'domain_overview_' + run_id.replace('-', '_').replace('.', '_')
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
        if not any(row.get('resource_id') == str(entity_id) and row.get('state') == 'draft' and not row.get('revision_id') for row in evidence):
            raise SuiteError('Model draft reference was presented as a frozen adoption')
        reader.request('GET', overview_path, (403,))
        governed = reader.request('GET', f'/api/v1/catalog/domains/{domain_id}/overview?view=governance', (200,)).payload
        if governed.get('total_entries') != 0 or governed.get('coverage') is not None:
            raise SuiteError('ordinary directory reader discovered inventory-only drafts')
        report = {'schema_version': 'addp.online-suite/v1', 'suite': 'catalog-domain-overview', 'run_id': run_id, 'tenant_id': str(tenant_id), 'five_standard_types': True, 'stable_source_identities': True, 'model_reference_evidence': True, 'reader_inventory_denied': True, 'reference_element_unchanged': False, 'cleanup': 'pending'}
    finally:
        cleanup(client, paths)
        current = client.request('GET', reference_path, (200,)).payload
        if current != reference:
            raise SuiteError('read-only configured reference Element changed during acceptance')
    report.update(reference_element_unchanged=True, cleanup='passed', residual_professional_resources=0, catalog_history='retained')
    return report


def main():
    try:
        if os.environ.get('ADDP_ONLINE_TEST') != '1':
            raise SuiteError('ADDP_ONLINE_TEST must be exactly 1')
        tenant = int(os.environ['ADDP_ONLINE_TEST_TENANT_ID'])
        timeout = float(os.environ.get('ADDP_ONLINE_TEST_HTTP_TIMEOUT_SECONDS', '10'))
        if tenant <= 0 or timeout <= 0:
            raise SuiteError('Tenant and HTTP timeout must be positive')
        token = os.environ['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN']
        reader_token = os.environ['ADDP_ONLINE_CATALOG_READER_ACCESS_TOKEN']
        identity = validate_user_identity(GatewayClient(os.environ['SYSTEM_URL'], token, timeout, 'SYSTEM_URL'), tenant)
        _API.validate_user_identity(GatewayClient(os.environ['SYSTEM_URL'], reader_token, timeout, 'SYSTEM_URL'), tenant, required_permissions={'catalog.entry.read'})
        report = run_suite(GatewayClient(os.environ['GATEWAY_URL'], token, timeout), GatewayClient(os.environ['GATEWAY_URL'], reader_token, timeout), tenant, os.environ['ADDP_ONLINE_TEST_RUN_ID'], int(os.environ['ADDP_ONLINE_CATALOG_REFERENCE_ELEMENT_ID']))
        report['identity'] = identity
        json.dump(report, sys.stdout, ensure_ascii=False, sort_keys=True); sys.stdout.write('\n')
        return 0
    except (KeyError, ValueError, SuiteError) as error:
        print(f'Catalog domain overview Online failed: {error}', file=sys.stderr)
        return 1

if __name__ == '__main__':
    raise SystemExit(main())
