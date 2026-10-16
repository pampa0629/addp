"""PG-only real System/Meta/Manager source authorization lifecycle."""

from __future__ import annotations

import json
import math
import os
import re
import signal
import sys
import time
import uuid
from datetime import datetime, timedelta, timezone
from importlib import import_module
from pathlib import Path
from urllib.parse import urlencode

API = import_module('scripts.utils.online-api')
REGISTRATION = import_module('scripts.test.online-engine-registration')
SuiteError = API.SuiteError
SUITE = 'pg-source-authorization'
READER_PERMISSIONS = {'manager.data_item.read', 'meta.catalog.read', 'meta.scan_task.execute', 'meta.scan_task.read'}
OPERATOR_PERMISSIONS = REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS | {
    'system.engine_access_grant.revoke', 'system.engine_access_deny.create', 'system.engine_access_deny.release'}


def require(condition, message):
    if not condition:
        raise SuiteError(message)


def identity(client, tenant, permissions):
    summary = API.validate_user_identity(client, tenant, permissions)
    current = client.request('GET', '/api/v1/system/auth/context', (200,)).payload
    actual = {key for role in current['authorization']['role_assignments'] for key in role['permissions']}
    require(actual == permissions, 'fixture User has unexpected additional permissions')
    return str(current['principal']['id']), summary


def organization_memberships(client, expected):
    current = client.request('GET', '/api/v1/system/auth/context', (200,)).payload['organization']
    actual = {('department', row['department_id']) for row in current['departments']}
    actual |= {('project_group', row['project_group_id']) for row in current['project_groups']}
    require(actual == set(expected.items()), 'fixture organization membership differs from its declared recipients')


def table_path(engine, name):
    return {'version': 'catalog.path/v1', 'engine_id': engine, 'segments': [
        {'term': 'server', 'kind': 'server', 'name': ''},
        {'term': 'schema', 'kind': 'namespace', 'name': 'public'},
        {'term': 'table', 'kind': 'table', 'name': name}]}


def locator(engine, name, item_id=None):
    query = {'type': 'table'}
    if item_id is not None:
        query['item_id'] = item_id
    return f'addp://engine/{engine}/path/public/{name}?' + urlencode(query)


def basic_scan(reader, engine, names):
    created = reader.request('POST', '/api/v1/meta/scan/run/manual', (201,), {
        'engine_id': engine, 'targets': [locator(engine, name) for name in names],
        'scan_depth': 'basic', 'force': True, 'trigger_type': 'manual', 'source': 'meta'}).payload
    execution = created.get('execution_id')
    require(isinstance(execution, str) and str(uuid.UUID(execution)) == execution, 'scan execution UUID is missing')
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        current = reader.request('GET', f'/api/v1/meta/executions/{execution}', (200,)).payload
        require(current.get('execution_id') == execution, 'scan execution identity changed')
        if current.get('status') == 'success':
            require(current.get('scan_context', {}).get('scan_depth') == 'basic', 'scan was not basic')
            metadata = current.get('metadata', {})
            require(metadata.get('items_scanned') == len(names) and metadata.get('catalog_nodes_scanned') == 0,
                    'exact scan expanded beyond its selected leaves')
            return execution
        require(current.get('status') in {'pending', 'running'}, 'basic scan failed')
        time.sleep(1)
    raise SuiteError('basic scan exceeded its deadline')


def item(reader, engine, name):
    values = reader.request('GET', f'/api/v1/meta/engines/{engine}/items?branch=public', (200,), response_type=list).payload
    matches = [row for row in values if row.get('full_name') == 'public.' + name and row.get('item_type') == 'table']
    require(len(matches) == 1 and isinstance(matches[0].get('id'), int) and matches[0]['id'] > 0,
            'one persisted ordinary PG DataItem is required')
    return matches[0]


def preview(reader, target, allowed, expected=None):
    result = reader.request('GET', '/api/v1/manager/preview?' + urlencode({
        'locator': target, 'page': 1, 'page_size': 20}), (200,) if allowed else (403,)).payload
    if not allowed:
        require(set(result) <= {'error', 'error_code'} and bool(result.get('error')),
                'denied preview exposed data or metadata')
        require(result.get('error_code') != 'security_protection_required', 'Security failure is not source denial')
        return
    data = result.get('data', {})
    require(result.get('preview_type') == 'table' and data.get('columns') == ['person_id']
            and sorted(row.get('person_id') for row in data.get('rows', [])) == ['A', 'B'],
            'Manager did not read the deterministic PG source')
    if expected is not None:
        require(data.get('total') == expected, 'PG source row count changed')


class Lifecycle:
    def __init__(self, operator, reader, tenant, engine, recipient, organizations, outsider):
        self.operator, self.reader, self.tenant, self.engine, self.recipient = operator, reader, tenant, engine, recipient
        self.organizations, self.outsider = organizations, outsider
        self.path = table_path(engine, 'metric_people')
        self.base = f'/api/v1/system/engines/{engine}'
        self.grants, self.denies = [], []
        self.recipients = {('user', recipient)}
        self.source_observations = []
        self.expiry_grant = None

    def listing(self, *, history=False, state=None, recipient_type='user', recipient_id=None):
        query = {'catalog_path': json.dumps(self.path, separators=(',', ':')),
                 'recipient_type': recipient_type, 'recipient_id': self.recipient if recipient_id is None else recipient_id,
                 'page_size': 100}
        if state:
            query['state'] = state
        return self.operator.request('GET', self.base + '/access_grants' + ('/history' if history else '')
                                     + '?' + urlencode(query), (200,)).payload

    def grant(self, facts, *, initialize=False, seconds=300, recipient_type='user', recipient_id=None):
        command = str(uuid.uuid4())
        expires = datetime.now(timezone.utc) + timedelta(seconds=seconds)
        body = {'request_id': command, 'catalog_path': self.path, 'item_fingerprint': facts['item_fingerprint'],
                'expected_boundary_hash': facts['boundary_hash'], 'requirement_version': '1',
                'initialize_approval': initialize, 'recipient_type': recipient_type,
                'recipient_id': self.recipient if recipient_id is None else recipient_id,
                'action': 'read', 'expiry_mode': 'at_time', 'expires_at': expires.isoformat(),
                'reason': 'Disposable PG source authorization lifecycle'}
        self.grants.append(command)  # Track before the write, including unknown outcomes.
        self.recipients.add((body['recipient_type'], body['recipient_id']))
        receipt = self.operator.request('POST', self.base + '/access_grants', (201,), body).payload
        for key in ('request_id', 'catalog_path', 'item_fingerprint', 'recipient_type', 'recipient_id', 'action', 'expiry_mode'):
            require(receipt.get(key) == body[key], 'Grant receipt changed its bound target or recipient')
        require(receipt.get('boundary_hash') == facts['boundary_hash'] and receipt.get('revocation') is None,
                'Grant receipt changed its approved boundary')
        require(datetime.fromisoformat(receipt['expires_at'].replace('Z', '+00:00')) == expires, 'Grant expiry changed')
        return command, body

    def revoke(self, command):
        self.operator.request('POST', self.base + f'/access_grants/{command}/revoke', (200,),
                              {'reason': 'Disposable PG lifecycle source withdrawal'})

    def deny(self, facts, *, recipient_type='user', recipient_id=None):
        command = str(uuid.uuid4())
        self.denies.append(command)
        body = {'deny_id': command, 'catalog_path': self.path, 'item_fingerprint': facts['item_fingerprint'],
                'recipient_type': recipient_type, 'recipient_id': self.recipient if recipient_id is None else recipient_id,
                'action': 'read', 'expiry_mode': 'at_time',
                'expires_at': (datetime.now(timezone.utc) + timedelta(seconds=180)).isoformat(),
                'reason': 'Disposable PG lifecycle Deny priority'}
        self.recipients.add((body['recipient_type'], body['recipient_id']))
        receipt = self.operator.request('POST', self.base + '/access_denies', (200,), body).payload
        require(receipt.get('deny_id') == command, 'Deny receipt identity changed')
        return command

    def release(self, command, *, cleanup=False):
        response = self.operator.request('POST', self.base + f'/access_denies/{command}/release', (200, 404, 409) if cleanup else (200,),
                                         {'reason': 'Disposable PG lifecycle Deny release'})
        require(response.status != 200 or response.payload.get('deny_id') == command,
                'Deny release receipt identity changed')
        require(response.status != 409 or response.payload.get('error_code') == 'engine_access_deny_expired',
                'Deny cleanup conflict is not natural expiry')

    def cleanup(self):
        errors = []
        for command in self.denies:
            try:
                self.release(command, cleanup=True)
            except Exception:
                errors.append('Deny release failed')
        for kind, recipient in sorted(self.recipients):
            try:
                active = self.listing(history=True, state='active', recipient_type=kind, recipient_id=recipient)
                for row in active['data']:
                    if row['request_id'] not in self.grants:
                        errors.append('refusing to revoke an unowned Grant')
                        continue
                    self.revoke(row['request_id'])
            except Exception:
                errors.append('Grant cleanup failed')
        for kind, recipient in sorted(self.recipients):
            try:
                require(self.listing(recipient_type=kind, recipient_id=recipient)['total'] == 0,
                        'current source rules remain after cleanup')
            except Exception:
                errors.append('residual verification failed')
        require(not errors, '; '.join(errors))

    def inspect_sources(self, facts, sources, denies, reason='grant'):
        current = self.listing()
        if not sources and not denies:
            require(current['total'] == 0 and current['data'] == [], 'source-free account still has a current relation')
            self.source_observations.append({'reason': 'no_grant', 'sources': [], 'denies': []})
            return
        require(current['total'] == 1 and len(current['data']) == 1, 'current sources were not merged to one DataItem')
        row = current['data'][0]
        require(row['catalog_path'] == self.path and row['item_fingerprint'] == facts['item_fingerprint']
                and row['recipient_type'] == 'user' and row['recipient_id'] == self.recipient
                and row['grant_count'] == len(sources) and 'request_id' not in row,
                'current relation target, recipient or grant count differs')
        observed = row['inspection']
        require(observed['account_id'] == self.recipient and observed['catalog_path'] == self.path
                and observed['item_fingerprint'] == facts['item_fingerprint'] and bool(observed['observed_at'])
                and observed['reason'] == reason and observed['rule_covered'] == (reason == 'grant'),
                'current source verdict differs from the expected rules')
        actual = {(source['recipient_type'], source['recipient_id']): source['request_id'] for source in observed['sources']}
        require(actual == sources and len(observed['sources']) == len(sources)
                and all(source['grant_count'] == 1 for source in observed['sources']), 'Grant source anchors differ')
        actual_denies = {(deny['recipient_type'], deny['recipient_id']): deny['deny_id'] for deny in observed['denies']}
        require(actual_denies == denies and len(observed['denies']) == len(denies), 'Deny source anchors differ')
        require(all(set(deny) == {'deny_id', 'recipient_type', 'recipient_id', 'expiry_mode', 'expires_at'}
                    for deny in observed['denies']), 'Deny summary exposed establishment details')
        self.source_observations.append({key: observed[key] for key in ('reason', 'sources', 'denies')})

    def organization_flow(self, report, facts, target, ungranted):
        report['phase'] = 'organization_sources'
        report['source_observations'] = self.source_observations
        report['organization_recipients'] = self.organizations
        before = self.reader.token
        preview(self.outsider, target, False)
        sources, denies = {}, {}
        personal = ('user', self.recipient)
        department = ('department', self.organizations['department'])
        project = ('project_group', self.organizations['project_group'])
        for source in (department, project, personal):
            sources[source], _ = self.grant(facts, recipient_type=source[0], recipient_id=source[1])
            self.inspect_sources(facts, sources, denies)
            preview(self.reader, target, True, 2)
            preview(self.outsider, target, False)
            preview(self.reader, ungranted, False)
        report['checks'].append('three_sources_merged_and_nonmember_denied')
        personal_id = sources.pop(personal)
        self.revoke(personal_id)
        self.inspect_sources(facts, sources, denies)
        require(self.listing(history=True, state='active')['total'] == 0,
                'history expanded current organization sources into personal Grants')
        preview(self.reader, target, True, 2)
        report['checks'].append('personal_withdrawal_preserves_organizations')
        for source in (personal, project):
            denies[source] = self.deny(facts, recipient_type=source[0], recipient_id=source[1])
            self.inspect_sources(facts, sources, denies, 'explicit_deny')
            preview(self.reader, target, False)
        self.release(denies.pop(personal))
        self.inspect_sources(facts, sources, denies, 'explicit_deny')
        preview(self.reader, target, False)
        report['checks'].append('one_deny_release_preserves_other_deny')
        self.release(denies.pop(project))
        self.inspect_sources(facts, sources, denies)
        preview(self.reader, target, True, 2)
        report['checks'].append('all_denies_released_restore_organization_read')
        self.revoke(sources.pop(department))
        self.inspect_sources(facts, sources, denies)
        preview(self.reader, target, True, 2)
        report['checks'].append('department_withdrawal_preserves_project')
        denies[personal] = self.deny(facts)
        self.revoke(sources.pop(project))
        self.inspect_sources(facts, sources, denies, 'explicit_deny')
        preview(self.reader, target, False)
        report['checks'].append('last_grant_withdrawn_preserves_deny_only_anchor')
        self.release(denies.pop(personal))
        self.inspect_sources(facts, sources, denies)
        preview(self.reader, target, False)
        preview(self.outsider, target, False)
        preview(self.reader, ungranted, False)
        require(self.reader.token == before, 'organization authorization changed the reader credential')
        report['checks'].append('all_sources_withdrawn_and_release_does_not_grant')

    def run(self, report):
        reader_before = self.reader.token
        report['phase'] = 'initial_basic_scan'
        report['initial_scan'] = basic_scan(self.reader, self.engine, ['metric_people', 'metric_events'])
        original = item(self.reader, self.engine, 'metric_people')
        other = item(self.reader, self.engine, 'metric_events')
        target = locator(self.engine, 'metric_people', original['id'])
        ungranted = locator(self.engine, 'metric_events', other['id'])
        preview(self.reader, target, False)
        preview(self.reader, ungranted, False)
        report['phase'] = 'grant_and_duplicate'
        facts = REGISTRATION.read_exact_record_authorization_facts(self.operator, self.path)
        require(original.get('fingerprint') == facts['item_fingerprint'], 'Meta identity differs from trusted boundary')
        first, body = self.grant(facts, initialize=True)
        preview(self.reader, target, True, 2)
        preview(self.reader, ungranted, False)
        duplicate = dict(body, request_id=str(uuid.uuid4()), initialize_approval=False)
        self.grants.append(duplicate['request_id'])
        rejected = self.operator.request('POST', self.base + '/access_grants', (409,), duplicate).payload
        require(rejected.get('error_code') == 'engine_access_grant_relation_exists', 'duplicate rejected for the wrong reason')
        report['phase'] = 'basic_rescan'
        report['basic_rescan'] = basic_scan(self.reader, self.engine, ['metric_people'])
        refreshed = item(self.reader, self.engine, 'metric_people')
        after = REGISTRATION.read_exact_record_authorization_facts(self.operator, self.path)
        require(refreshed['id'] == original['id'] and refreshed['fingerprint'] == original['fingerprint']
                and after['boundary_hash'] == facts['boundary_hash'], 'basic rescan changed the approved DataItem boundary')
        preview(self.reader, target, True, 2)
        report['phase'] = 'deny_release_revoke'
        deny = self.deny(facts)
        preview(self.reader, target, False)
        require(self.listing(history=True, state='active')['total'] == 1, 'Deny test lost its active Grant')
        self.release(deny)
        preview(self.reader, target, True, 2)
        self.revoke(first)
        preview(self.reader, target, False)
        require(self.listing(history=True, state='revoked')['total'] == 1, 'withdrawal history is missing')
        report['phase'] = 'natural_expiry'
        self.expiry_grant, _ = self.grant(facts, seconds=45)
        preview(self.reader, target, True, 2)
        deadline = time.monotonic() + 75
        while time.monotonic() < deadline:
            expired = self.listing(history=True, state='expired')
            if any(row.get('request_id') == self.expiry_grant for row in expired['data']):
                break
            time.sleep(1)
        else:
            raise SuiteError('Grant did not naturally expire within deadline')
        preview(self.reader, target, False)
        preview(self.reader, ungranted, False)
        require(self.reader.token == reader_before and self.listing()['total'] == 0,
                'natural expiry changed the credential or left current authorization')
        row = next(row for row in expired['data'] if row.get('request_id') == self.expiry_grant)
        require(row.get('revocation') is None, 'manual withdrawal replaced natural expiry')
        report.update(grant_commands=self.grants, item_fingerprint=facts['item_fingerprint'], boundary_hash=facts['boundary_hash'],
                      rows=2, same_credential=True, exact_target_only=True,
                      checks=['ungranted_denied', 'granted_readable', 'duplicate_rejected', 'basic_rescan_readable',
                              'deny_overrides_grant', 'release_restores_read', 'revoke_denies_read', 'natural_expiry_denies_read'])
        self.organization_flow(report, facts, target, ungranted)


def main():
    report = {'schema_version': 'addp.pg-source-authorization/v1', 'suite': SUITE, 'result': 'failed'}
    lifecycle = None
    evidence = None
    def interrupted(signum, frame):
        raise SuiteError('PG source authorization acceptance interrupted')
    try:
        require(all(os.environ.get(key) == value for key, value in {
            'ADDP_ONLINE_TEST': '1', 'ADDP_ONLINE_HOST': '1', 'ADDP_ONLINE_HOSTED': '1',
            'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'POSTGRES_DB': 'addp_online'}.items()),
            'use the disposable Hosted PG authorization gate')
        tenant, engine = int(os.environ['ADDP_ONLINE_TEST_TENANT_ID']), int(os.environ['ADDP_ONLINE_CONSUMER_ENGINE_ID'])
        require(tenant > 1 and engine > 0, 'nondefault Tenant and positive Engine required')
        directory = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
        require(directory.is_absolute() and not directory.resolve().is_relative_to(Path(__file__).resolve().parents[2]),
                'evidence must be outside checkout')
        directory = directory.resolve()
        run_id = os.environ['ADDP_ONLINE_TEST_RUN_ID']
        require(re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]{0,63}', run_id), 'invalid Run ID')
        directory.mkdir(parents=True, exist_ok=True)
        evidence = directory / (SUITE + '.json')
        timeout = float(os.environ.get('ADDP_ONLINE_TEST_HTTP_TIMEOUT_SECONDS', '10'))
        require(math.isfinite(timeout) and 0 < timeout <= 30, 'invalid HTTP timeout')
        reader = API.GatewayClient(os.environ['GATEWAY_URL'], os.environ['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], timeout)
        operator = API.GatewayClient(os.environ['GATEWAY_URL'], os.environ['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'], timeout)
        outsider = API.GatewayClient(os.environ['GATEWAY_URL'], os.environ['ADDP_ONLINE_FIXTURE_OUTSIDER_ACCESS_TOKEN'], timeout)
        organizations = {kind: os.environ[key] for kind, key in (
            ('department', 'ADDP_ONLINE_FIXTURE_DEPARTMENT_ID'), ('project_group', 'ADDP_ONLINE_FIXTURE_PROJECT_GROUP_ID'))}
        require(all(re.fullmatch(r'[1-9][0-9]*', value) for value in organizations.values()), 'invalid organization fixture IDs')
        recipient, report['reader'] = identity(reader, tenant, READER_PERMISSIONS)
        issuer, report['operator'] = identity(operator, tenant, OPERATOR_PERMISSIONS)
        observer, report['outsider'] = identity(outsider, tenant, READER_PERMISSIONS)
        require(len({issuer, recipient, observer}) == 3 and len({reader.token, operator.token, outsider.token}) == 3,
                'operator, reader and outsider must be distinct Users')
        organization_memberships(reader, organizations)
        organization_memberships(outsider, {})
        for signum in (signal.SIGTERM, signal.SIGINT):
            signal.signal(signum, interrupted)
        report.update(run_id=run_id, tenant_id=str(tenant), engine_id=engine)
        report['principals'] = {'reader': recipient, 'operator': issuer, 'outsider': observer}
        lifecycle = Lifecycle(operator, reader, tenant, engine, recipient, organizations, outsider)
        lifecycle.run(report)
        report['result'] = 'passed'
    except (SuiteError, REGISTRATION.RegistrationError, KeyError, ValueError, TypeError):
        print('PG source authorization acceptance failed; inspect the bounded phase evidence', file=sys.stderr)
    finally:
        if lifecycle is not None:
            report.update(grant_commands=lifecycle.grants, deny_commands=lifecycle.denies)
            try:
                lifecycle.cleanup()
                report['cleanup'] = 'zero_current_source_rules'
            except Exception:
                report.update(result='failed', cleanup='failed')
        if evidence is not None:
            evidence.write_text(json.dumps(report, sort_keys=True) + '\n')
    print(json.dumps(report, sort_keys=True))
    return 0 if report['result'] == 'passed' else 1


if __name__ == '__main__':
    raise SystemExit(main())
