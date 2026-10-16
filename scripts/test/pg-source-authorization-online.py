"""PG-only real System/Meta/Manager source authorization lifecycle."""

from __future__ import annotations

import json
import math
import os
import re
import signal
import subprocess
import sys
import tempfile
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
ORGANIZATION_PERMISSIONS = {'iam.department_membership.read', 'iam.department_membership.close',
                            'iam.project_group_membership.read', 'iam.project_group_membership.close'}
BROWSER_CHECKS = ['department_exit_silent_refresh', 'project_exit_silent_refresh',
                  'same_iframe_without_relogin', 'denied_preview_clears_old_rows']


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


def organization_administrator(client, tenant):
    current = client.request('GET', '/api/v1/system/auth/context', (200,)).payload
    roles = current['authorization']['role_assignments']
    permissions = {key for role in roles for key in role['permissions']}
    require(current['principal']['type'] == 'user' and current['context']['type'] == 'tenant'
            and current['context']['tenant_id'] == str(tenant)
            and current['token']['type'] == 'first_party_access_token'
            and {role['role_key'] for role in roles} == {'tenant.administrator'}
            and ORGANIZATION_PERMISSIONS <= permissions,
            'membership mutations require the existing dedicated fixture Tenant administrator')
    return current['principal']['id'], {'role': 'tenant.administrator', 'tenant_id': str(tenant),
                                       'permissions_verified': sorted(ORGANIZATION_PERMISSIONS)}


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


class BrowserObservation:
    """Keep the real UI open while the owner performs its existing IAM commands."""

    def __init__(self, tenant, principal, target):
        self.binding = {'run_id': os.environ['ADDP_ONLINE_TEST_RUN_ID'], 'tenant_id': str(tenant),
                        'principal_id': str(principal), 'locator': target}
        self.evidence = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']) / 'pg-authorization-browser.json'
        self.secret = Path(os.environ['ADDP_ONLINE_SECRET_DIR'])
        require(self.secret.is_absolute() and self.secret.is_dir(), 'browser requires the Hosted Secret directory')
        self.process = self.output = self.temporary = None

    def __enter__(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='pg-browser-', dir=self.secret)
        self.control = Path(self.temporary.name)
        self.evidence.unlink(missing_ok=True)
        # The browser must log in as the reader, not inherit the API fixture or administrator Token.
        environment = {key: value for key, value in os.environ.items() if key in {
            'PATH', 'HOME', 'TMPDIR', 'TMP', 'TEMP', 'CI', 'PLAYWRIGHT_BROWSERS_PATH', 'NODE_EXTRA_CA_CERTS'}}
        environment.update({key: os.environ[key] for key in (
            'ADDP_ONLINE_HOSTED', 'ADDP_ONLINE_TEST', 'ADDP_ONLINE_ARTIFACT_DIR', 'CONSOLE_URL',
            'ADDP_ONLINE_TEST_USER_USERNAME', 'ADDP_ONLINE_TEST_USER_PASSWORD')})
        environment.update(ADDP_PG_BROWSER_BINDING=json.dumps(self.binding), ADDP_PG_BROWSER_CONTROL=str(self.control))
        try:
            self.output = (self.control / 'output.log').open('w')
            self.process = subprocess.Popen(['npm', 'exec', '--', 'playwright', 'test',
                '--config=playwright.online.config.js', '--output=' + str(self.control / 'playwright'),
                'e2e/online/pg-source-authorization.spec.js'],
                cwd=Path(__file__).resolve().parents[2] / 'console/frontend', env=environment,
                stdout=self.output, stderr=subprocess.STDOUT, start_new_session=True)
            return self
        except Exception:
            self.__exit__(None, None, None)
            raise

    def marker(self, kind):
        require(kind in {'department', 'project_group'}, 'invalid browser member-exit phase')
        return dict(self.binding, phase=kind)

    def wait(self, kind):
        ready = self.control / ('ready-' + kind + '.json')
        deadline = time.monotonic() + 120
        while not ready.exists():
            require(self.process.poll() is None and time.monotonic() < deadline,
                    'browser did not reach its member-exit observation: ' + kind)
            time.sleep(.2)
        require(json.loads(ready.read_text()) == self.marker(kind), 'browser control binding changed')

    def release(self, kind):
        path = self.control / ('release-' + kind + '.tmp')
        path.write_text(json.dumps(self.marker(kind)))
        path.replace(self.control / ('release-' + kind + '.json'))

    def finish(self):
        try:
            require(self.process.wait(timeout=180) == 0, 'PG browser member-exit acceptance failed')
        except subprocess.TimeoutExpired as error:
            raise SuiteError('PG browser member-exit acceptance timed out') from error
        require(self.evidence.is_file(), 'PG browser report missing')
        result = json.loads(self.evidence.read_text())
        anonymous = result.pop('anonymous_refreshes', None)
        expected = dict(self.binding, schema_version='addp.pg-source-authorization-browser/v1', result='passed',
                        checks=BROWSER_CHECKS, login_requests=1,
                        observations=[{'closed_type': 'department', 'refreshes': 1, 'preview_status': 200},
                                      {'closed_type': 'project_group', 'refreshes': 1, 'preview_status': 403}])
        require(result == expected and type(anonymous) is int and anonymous in {0, 1},
                'PG browser evidence does not prove the exact member-exit flow')
        return dict(result, anonymous_refreshes=anonymous)

    def __exit__(self, *_):
        try:
            if self.process is not None and self.process.poll() is None:
                os.killpg(self.process.pid, signal.SIGTERM)
                try:
                    self.process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(self.process.pid, signal.SIGKILL)
                    self.process.wait(timeout=5)
        finally:
            if self.output is not None:
                self.output.close()
            if self.temporary is not None:
                self.temporary.cleanup()


class Lifecycle:
    def __init__(self, operator, reader, tenant, engine, recipient, organizations, outsider,
                 organization_operator, login, username, password):
        self.operator, self.reader, self.tenant, self.engine, self.recipient = operator, reader, tenant, engine, recipient
        self.organizations, self.outsider = organizations, outsider
        self.organization_operator, self.login = organization_operator, login
        require(username == 'external-online-consumer' and bool(password), 'dedicated reader login required')
        self.username, self.password = username, password
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

    def close_membership(self, kind, before):
        collection = 'departments' if kind == 'department' else 'project_groups'
        base = f'/api/v1/system/tenant/{collection}/{self.organizations[kind]}/memberships'
        listing = self.organization_operator.request('GET', base + '?page_size=100&status=active', (200,)).payload
        rows = listing['data']
        require(listing['total'] == 1 and len(rows) == 1, 'fixture organization must contain only its reader')
        row = rows[0]
        membership_id = row.get('id')
        version = row.get('version')
        require(isinstance(membership_id, str) and re.fullmatch(r'[1-9][0-9]*', membership_id)
                and type(version) is int and version > 0 and row.get('status') == 'active'
                and row.get('principal_id') == self.recipient
                and row.get('tenant_membership_id') == before['context']['tenant_membership_id'],
                'organization mutation does not identify the current reader membership')
        response = self.organization_operator.request('POST', base + f'/{membership_id}/close', (200,), {
            'version': version, 'reason': 'Disposable PG member-exit acceptance'}).payload
        require(all(response.get(key) == row[key] for key in ('id', 'principal_id', 'tenant_membership_id'))
                and response.get('status') == 'ended' and response.get('ended_at')
                and response.get('version') == version + 1, 'membership close receipt changed its binding')
        remaining = self.organization_operator.request('GET', base + '?page_size=100&status=active', (200,)).payload
        require(remaining['total'] == 0 and remaining['data'] == [], 'closed membership remains active')

    def relogin_reader(self, before, target, expected_organizations):
        # Deliberately keep the old token for both probes; do not auto-refresh it.
        for path in ('/api/v1/system/auth/context', '/api/v1/manager/preview?' + urlencode({
                'locator': target, 'page': 1, 'page_size': 20})):
            denied = self.reader.request('GET', path, (401,)).payload
            require(set(denied) <= {'error', 'error_code'} and bool(denied.get('error')),
                    'stale credential exposed content or identity')
        issued = self.login.request('POST', '/api/v1/system/login', (200,), {
            'username': self.username, 'password': self.password}).payload
        token = (issued.get('session') or {}).get('access_token')
        require(issued.get('next_action') == 'session_issued' and isinstance(token, str)
                and token and token != self.reader.token, 'formal reader login did not issue a fresh credential')
        fresh = API.GatewayClient(self.reader.base_url, token, self.reader.timeout)
        principal, _ = identity(fresh, self.tenant, READER_PERMISSIONS)
        current = fresh.request('GET', '/api/v1/system/auth/context', (200,)).payload
        require(principal == self.recipient and current['principal'] == before['principal']
                and current['context'] == before['context']
                and current['authorization']['role_assignments'] == before['authorization']['role_assignments']
                and int(current['authorization']['authorization_version']) > int(before['authorization']['authorization_version']),
                'reader login changed identity, roles, membership or failed to advance authorization version')
        organization_memberships(fresh, expected_organizations)
        self.reader = fresh

    def membership_flow(self, report, facts, target, ungranted):
        report['phase'] = 'organization_membership_changes'
        sources = {}
        for kind, recipient in self.organizations.items():
            sources[(kind, recipient)], _ = self.grant(facts, recipient_type=kind, recipient_id=recipient)
        issued_sources = dict(sources)
        self.inspect_sources(facts, sources, {})
        preview(self.reader, target, True, 2)
        remaining = dict(self.organizations)
        observations = report['membership_observations'] = []
        with BrowserObservation(self.tenant, self.recipient, target) as browser:
            for kind in ('department', 'project_group'):
                browser.wait(kind)
                before = self.reader.request('GET', '/api/v1/system/auth/context', (200,)).payload
                self.close_membership(kind, before)
                remaining.pop(kind)
                self.relogin_reader(before, target, remaining)
                sources.pop((kind, self.organizations[kind]))
                self.inspect_sources(facts, sources, {})
                preview(self.reader, target, bool(sources), 2)
                preview(self.reader, ungranted, False)
                preview(self.outsider, target, False)
                # Leaving an organization removes inheritance, not that organization's Grant.
                for recipient_type, recipient_id in self.organizations.items():
                    history = self.listing(history=True, state='active', recipient_type=recipient_type, recipient_id=recipient_id)
                    require(history['total'] == 1 and len(history['data']) == 1
                            and history['data'][0]['request_id'] == issued_sources[(recipient_type, recipient_id)]
                            and history['data'][0].get('revocation') is None,
                            'member exit revoked or lost an organization Grant')
                    require(self.listing(recipient_type=recipient_type, recipient_id=recipient_id)['total'] == 1,
                            'member exit removed the direct organization relation')
                observations.append({'closed_type': kind, 'old_token_status': 401,
                                     'remaining_sources': len(sources), 'preview_status': 200 if sources else 403})
                report['checks'].append('department_exit_invalidates_old_token_and_preserves_project_read' if sources else
                                        'last_organization_exit_invalidates_old_token_and_denies_read')
                browser.release(kind)
            report['browser'] = browser.finish()
            report['checks'].extend(BROWSER_CHECKS)
        report['checks'].append('organization_grants_and_history_survive_member_exit')

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
                      rows=2, same_credential_through_grant_expiry=True, exact_target_only=True,
                      checks=['ungranted_denied', 'granted_readable', 'duplicate_rejected', 'basic_rescan_readable',
                              'deny_overrides_grant', 'release_restores_read', 'revoke_denies_read', 'natural_expiry_denies_read'])
        self.organization_flow(report, facts, target, ungranted)
        self.membership_flow(report, facts, target, ungranted)


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
        organization_operator = API.GatewayClient(os.environ['GATEWAY_URL'], os.environ['ADDP_ONLINE_ADMIN_USER_ACCESS_TOKEN'], timeout)
        login = API.GatewayClient(os.environ['GATEWAY_URL'], '', timeout)
        organizations = {kind: os.environ[key] for kind, key in (
            ('department', 'ADDP_ONLINE_FIXTURE_DEPARTMENT_ID'), ('project_group', 'ADDP_ONLINE_FIXTURE_PROJECT_GROUP_ID'))}
        require(all(re.fullmatch(r'[1-9][0-9]*', value) for value in organizations.values()), 'invalid organization fixture IDs')
        recipient, report['reader'] = identity(reader, tenant, READER_PERMISSIONS)
        issuer, report['operator'] = identity(operator, tenant, OPERATOR_PERMISSIONS)
        observer, report['outsider'] = identity(outsider, tenant, READER_PERMISSIONS)
        coordinator, report['organization_operator'] = organization_administrator(organization_operator, tenant)
        require(len({issuer, recipient, observer, coordinator}) == 4
                and len({reader.token, operator.token, outsider.token, organization_operator.token}) == 4,
                'source operator, reader, outsider and membership operator must be distinct Users')
        organization_memberships(reader, organizations)
        organization_memberships(outsider, {})
        for signum in (signal.SIGTERM, signal.SIGINT):
            signal.signal(signum, interrupted)
        report.update(run_id=run_id, tenant_id=str(tenant), engine_id=engine)
        report['principals'] = {'reader': recipient, 'operator': issuer, 'outsider': observer, 'organization_operator': coordinator}
        lifecycle = Lifecycle(operator, reader, tenant, engine, recipient, organizations, outsider,
                              organization_operator, login, os.environ['ADDP_ONLINE_TEST_USER_USERNAME'],
                              os.environ['ADDP_ONLINE_TEST_USER_PASSWORD'])
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
