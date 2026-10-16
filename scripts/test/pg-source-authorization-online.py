"""PG-only real System/Meta/Manager source authorization lifecycle."""

from __future__ import annotations

import json
import math
import os
import re
import signal
import socket
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
PREFLIGHT = import_module('scripts.test.online-preflight')
REGISTRATION = import_module('scripts.test.online-engine-registration')
SuiteError = API.SuiteError
SUITE = 'pg-source-authorization'
READER_PERMISSIONS = {'manager.data_item.read', 'meta.catalog.read', 'meta.scan_task.execute', 'meta.scan_task.read'}
OPERATOR_PERMISSIONS = REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS | {
    'system.engine_access_grant.revoke', 'system.engine_access_deny.create', 'system.engine_access_deny.release',
    'catalog.entry.read', 'catalog.inventory.read', 'system.engine_access_fulfillment.create'}
CONFIRMER_PERMISSIONS = {'catalog.entry.read', 'catalog.entry.update', 'catalog.inventory.read',
                        'catalog.sharing_decision.create'}
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


def preview(reader, target, allowed, expected=None, *, columns=('person_id',), values=('A', 'B')):
    result = reader.request('GET', '/api/v1/manager/preview?' + urlencode({
        'locator': target, 'page': 1, 'page_size': 20}), (200,) if allowed else (403,)).payload
    if not allowed:
        require(set(result) <= {'error', 'error_code'} and bool(result.get('error')),
                'denied preview exposed data or metadata')
        require(result.get('error_code') != 'security_protection_required', 'Security failure is not source denial')
        return
    data = result.get('data', {})
    require(result.get('preview_type') == 'table' and data.get('columns') == list(columns)
            and sorted(row.get(columns[0]) for row in data.get('rows', [])) == list(values),
            'Manager did not read the deterministic PG source')
    if expected is not None:
        require(data.get('total') == expected, 'PG source row count changed')


class DependencyOutage:
    """Fault one declared PG dependency in the existing disposable Hosted deployment."""

    def __init__(self, module):
        require(module in {'meta', 'catalog'}, 'PG outage only permits Meta or Catalog')
        self.module = module
        self.port = {'meta': 8082, 'catalog': 8192}[module]
        self.repository = Path(__file__).resolve().parents[2]
        required = {'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'ADDP_ONLINE_HOSTED': '1',
                    'ADDP_ONLINE_HOST': '1', 'ADDP_ONLINE_TEST': '1', 'POSTGRES_DB': 'addp_online',
                    'META_URL': 'http://127.0.0.1:8082', 'MANAGER_URL': 'http://127.0.0.1:8081',
                    'CATALOG_URL': 'http://127.0.0.1:8192',
                    'SYSTEM_URL': 'http://127.0.0.1:8180', 'GATEWAY_URL': 'http://127.0.0.1:8000'}
        require(all(os.environ.get(key) == value for key, value in required.items()) and sys.platform == 'linux'
                and os.uname().machine == 'x86_64' and not (self.repository / '.env').exists(),
                'PG dependency outage requires the isolated Hosted deployment')
        self.secret = Path(os.environ['ADDP_ONLINE_SECRET_DIR'])
        require(self.secret.is_absolute() and self.secret.is_dir() and not self.secret.resolve().is_relative_to(self.repository),
                'PG dependency outage requires an external Hosted Secret directory')
        self.secret = self.secret.resolve()
        self.commit = PREFLIGHT.git(self.repository, 'rev-parse', 'HEAD')
        self.stopped = False

    def snapshot(self, module):
        service = PREFLIGHT.parse_service(module + '=' + os.environ[module.upper() + '_URL'])
        payload = API.GatewayClient(service.base_url, '', 10).request('GET', '/health/ready', (200,)).payload
        PREFLIGHT.validate_health(service, payload, self.commit, 'ready')
        fields = ('started_at', 'build_id', 'git_commit', 'source_fingerprint')
        # System/Gateway standalone health publishes build/start identity, not registration identity.
        if module in {'meta', 'manager', 'catalog'}:
            require(payload.get('role') == 'backend' and payload.get('registration_state') == 'registered'
                    and str(uuid.UUID(payload['instance_id'])) == payload['instance_id'], 'invalid Hosted backend identity')
            fields += ('instance_id',)
        return {key: payload[key] for key in fields}

    def run(self, operation):
        require(operation in {'stop', 'start'}, 'PG outage permits only standard stop/start')
        environment = {key: value for key, value in os.environ.items() if not key.startswith((
            'ADDP_ONLINE_TEST_USER_', 'ADDP_ONLINE_FIXTURE_', 'ADDP_ONLINE_ADMIN_USER_'))}
        environment['SKIP_MODTIDY'] = '1'
        with (self.secret / f'pg-{self.module}-lifecycle.log').open('a') as log:
            os.chmod(log.name, 0o600)
            try:
                result = subprocess.run(['bash', f'scripts/dev/{operation}.sh', '-' + self.module], cwd=self.repository,
                    env=environment, stdout=log, stderr=subprocess.STDOUT, timeout=120, check=False)
            except subprocess.TimeoutExpired as error:
                raise SuiteError('PG dependency lifecycle exceeded its bounded deadline') from error
        require(result.returncode == 0, 'PG dependency lifecycle failed')

    def __enter__(self):
        self.before = {module: self.snapshot(module) for module in ('meta', 'system', 'gateway', 'manager', 'catalog')}
        self.stopped = True  # Also restore an unknown stop outcome.
        try:
            self.run('stop')
            try:
                with socket.create_connection(('127.0.0.1', self.port), timeout=3):
                    raise SuiteError('PG dependency Backend still accepts connections')
            except ConnectionRefusedError:
                pass
            return self
        except Exception:
            self.__exit__(None, None, None)
            raise

    def __exit__(self, *_):
        if self.stopped:
            self.run('start')
            self.stopped = False
            self.after = {module: self.snapshot(module) for module in self.before}
            before, after = self.before[self.module], self.after[self.module]
            require(before['instance_id'] != after['instance_id']
                    and datetime.fromisoformat(after['started_at']) > datetime.fromisoformat(before['started_at'])
                    and all(before[key] == after[key] for key in ('build_id', 'git_commit', 'source_fingerprint')),
                    'PG dependency recovery did not prove a new process with identical build inputs')
            require(all(self.before[module] == self.after[module] for module in self.before if module != self.module),
                    'PG dependency outage changed another running owner')


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
        self.paths = [self.path]
        self.base = f'/api/v1/system/engines/{engine}'
        self.grants, self.denies = [], []
        self.recipients = {('user', recipient)}
        self.source_observations = []
        self.expiry_grant = None

    def listing(self, *, history=False, state=None, recipient_type='user', recipient_id=None, catalog_path=None):
        query = {'catalog_path': json.dumps(self.path if catalog_path is None else catalog_path, separators=(',', ':')),
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

    def deny(self, facts, *, recipient_type='user', recipient_id=None, catalog_path=None):
        command = str(uuid.uuid4())
        self.denies.append(command)
        body = {'deny_id': command, 'catalog_path': self.path if catalog_path is None else catalog_path, 'item_fingerprint': facts['item_fingerprint'],
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
        for path, kind, recipient in ((path, kind, recipient) for path in self.paths for kind, recipient in sorted(self.recipients)):
            try:
                active = self.listing(history=True, state='active', recipient_type=kind, recipient_id=recipient, catalog_path=path)
                for row in active['data']:
                    if row['request_id'] not in self.grants:
                        errors.append('refusing to revoke an unowned Grant')
                        continue
                    self.revoke(row['request_id'])
            except Exception:
                errors.append('Grant cleanup failed')
        for path, kind, recipient in ((path, kind, recipient) for path in self.paths for kind, recipient in sorted(self.recipients)):
            try:
                require(self.listing(recipient_type=kind, recipient_id=recipient, catalog_path=path)['total'] == 0,
                        'current source rules remain after cleanup')
            except Exception:
                errors.append('residual verification failed')
        require(not errors, '; '.join(errors))

    def catalog_flow(self, report, business, confirmer):
        report['phase'] = 'catalog_source_convergence'
        token = self.reader.token
        path = table_path(self.engine, 'metric_events')
        self.paths.append(path)  # Cleanup owns every target before any possible write.
        facts = REGISTRATION.read_exact_record_authorization_facts(self.operator, path)
        row = item(self.reader, self.engine, 'metric_events')
        require(row['fingerprint'] == facts['item_fingerprint'], 'business target differs from persisted DataItem')
        target = locator(self.engine, 'metric_events', row['id'])
        def read(allowed):
            preview(self.reader, target, allowed, 4 if allowed else None,
                    columns=('event_id', 'event_date'), values=('e1', 'e2', 'e3', 'old'))
        read(False)
        query = urlencode({'view': 'inventory', 'source_identity': facts['item_fingerprint'], 'page_size': 2})
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            listing = business.request('GET', '/api/v1/catalog/entries?' + query, (200,)).payload
            require(isinstance(listing.get('data'), list) and len(listing['data']) <= 1,
                    'one DataItem must resolve to at most one Catalog entry')
            if listing['data']:
                break
            time.sleep(1)
        else:
            raise SuiteError('Catalog did not consume the basic scan before its deadline')
        entry_id = listing['data'][0]['id']
        require(str(uuid.UUID(entry_id)) == entry_id, 'Catalog entry identity is not canonical')
        route = '/api/v1/catalog/entries/' + entry_id
        entry = business.request('GET', route, (200,)).payload
        require(entry.get('governance_status') == 'discovered' and not entry.get('responsibilities')
                and not entry.get('semantic_links'), 'refusing to replace pre-existing curation')
        report['phase'] = 'catalog_business_responsibility'
        entry = business.request('PUT', route, (200,), {
            'version': entry['version'], 'business_name': 'Disposable PG business approval',
            'business_description': 'Single DataItem authorization acceptance', 'governance_status': 'discovered',
            'visibility': entry['visibility'], 'domains': [], 'glossary_ids': [],
            'responsibilities': [{'role': 'business_owner', 'subject_type': 'user', 'subject_id': confirmer}]}).payload
        require(any(value.get('role') == 'business_owner' and value.get('subject_id') == confirmer
                    for value in entry.get('responsibilities', [])), 'business ownership was not saved')
        boundary = business.request('GET', route + '/sharing_boundary', (200,)).payload
        require(boundary == {'item_fingerprint': facts['item_fingerprint'], 'boundary_hash': facts['boundary_hash'],
                             'native_target': path}, 'Catalog and System disagree on the Meta boundary')
        requirement = self.operator.request('POST', self.base + '/access_approval_requirements', (201,), {
            'catalog_path': path, 'item_fingerprint': facts['item_fingerprint'],
            'expected_boundary_hash': facts['boundary_hash'], 'mode': 'catalog',
            'reason': 'Disposable PG explicit business approval'}).payload
        require(requirement.get('mode') == 'catalog' and requirement.get('catalog_path') == path
                and str(requirement.get('version')) == '1', 'business approval requirement changed')
        decision_id, request_id = str(uuid.uuid4()), str(uuid.uuid4())
        evidence = report['catalog_approval'] = {'entry_id': entry_id, 'decision_id': decision_id,
            'request_id': request_id, 'item_fingerprint': facts['item_fingerprint'], 'boundary_hash': facts['boundary_hash']}
        decision_body = {'decision_id': decision_id, 'version': str(entry['version']), 'recipient_type': 'user',
            'recipient_id': self.recipient, 'expiry_mode': 'at_time',
            'expires_at': (datetime.now(timezone.utc) + timedelta(minutes=5)).isoformat(),
            'expected_boundary_hash': facts['boundary_hash'], 'reason': 'Disposable PG business read approval'}
        report['phase'] = 'catalog_confirmation_does_not_grant'
        decision = business.request('POST', route + '/sharing_decisions', (201,), decision_body).payload
        require(decision.get('id') == decision_id and decision.get('confirmed_by') == confirmer
                and decision.get('recipient_id') == self.recipient and decision.get('self_beneficiary') is False
                and decision.get('item_fingerprint') == facts['item_fingerprint']
                and decision.get('boundary_hash') == facts['boundary_hash'], 'business confirmation binding changed')
        read(False)
        require(self.listing(catalog_path=path)['total'] == 0, 'confirmation alone created a source Grant')
        self.grants.append(request_id)  # Includes an unknown outcome after acceptance or automatic issuance.
        command = {'request_id': request_id, 'decision_id': decision_id, 'requirement_version': '1'}
        report['phase'] = 'catalog_automatic_issuance'
        first = self.operator.request('POST', route + '/sharing_fulfillments', (200, 202), command).payload
        require(first.get('request_id') == request_id and first.get('state') in {'pending', 'accepted'},
                'Catalog did not retain the exact fulfillment request')
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            history = self.listing(history=True, state='active', catalog_path=path)
            if history['total']:
                break
            time.sleep(1)
        else:
            raise SuiteError('Catalog did not automatically issue the PG Grant')
        require(history['total'] == 1 and len(history['data']) == 1, 'business approval duplicated source Grants')
        grant = history['data'][0]
        require(grant.get('request_id') == request_id and grant.get('catalog_request_id') == request_id
                and grant.get('approval_mode') == 'catalog' and grant.get('catalog_path') == path
                and grant.get('engine_id') == str(self.engine) and grant.get('action') == 'read'
                and grant.get('requirement_version') == '1' and bool(grant.get('granted_at'))
                and grant.get('operator_principal_id') == report['principals']['operator']
                and grant.get('item_fingerprint') == facts['item_fingerprint'] and grant.get('boundary_hash') == facts['boundary_hash']
                and grant.get('recipient_type') == 'user' and grant.get('recipient_id') == self.recipient
                and grant.get('expiry_mode') == 'at_time' and grant.get('revocation') is None
                and datetime.fromisoformat(grant['expires_at'].replace('Z', '+00:00')) == datetime.fromisoformat(decision_body['expires_at']),
                'business Grant does not preserve its decision and Meta boundary')
        settled = self.operator.request('GET', route + '/sharing_fulfillments/' + request_id, (200,)).payload
        require(settled.get('request_id') == request_id and settled.get('state') == 'accepted'
                and settled.get('decision_id') == decision_id and settled.get('requirement_version') == '1'
                and settled.get('recipient_id') == self.recipient and settled.get('action') == 'read'
                and settled.get('granted_at') == grant.get('granted_at'), 'Catalog cannot observe the original issuance')
        self.operator.request('POST', route + '/sharing_fulfillments', (200,), command)
        require(self.listing(history=True, state='active', catalog_path=path) == history,
                'same-parameter fulfillment replay changed issuance history')
        read(True)
        preview(self.outsider, target, False)
        self.catalog_outage_flow(report, path=path, facts=facts, route=route, command=command,
                                 history=history, read=read, target=target)
        self.operator.request('POST', route + '/sharing_fulfillments', (200,), command)
        read(False)
        revoked = self.listing(history=True, state='revoked', catalog_path=path)
        require(revoked['total'] == 1 and revoked['data'][0]['request_id'] == request_id
                and revoked['data'][0]['granted_at'] == grant['granted_at'] and self.reader.token == token,
                'historical replay restored, renewed or replaced the revoked Grant')
        evidence.update({'rows': 4, 'approval_mode': 'catalog', 'same_credential': True,
                         'grant_replay_unchanged': True, 'revoked_replay_denied': True})
        report['checks'].extend(['catalog_confirmation_alone_denied', 'catalog_automatic_grant_bound_to_dataitem',
            'catalog_grant_actual_preview_readable', 'catalog_replay_preserves_issuance',
            'catalog_grant_deny_release_and_withdrawal', 'catalog_replay_does_not_restore_revoked_grant'])

    def catalog_outage_flow(self, report, *, path, facts, route, command, history, read, target):
        report['phase'] = 'catalog_outage_existing_grant'
        token = self.reader.token
        outsider = self.outsider.request('GET', '/api/v1/system/auth/context', (200,)).payload['principal']['id']
        self.recipients.add(('user', outsider))
        blocked_request, direct_request = str(uuid.uuid4()), str(uuid.uuid4())
        self.grants.extend((blocked_request, direct_request))  # Own even an unexpected issuance before sending.
        evidence = report['catalog_outage'] = {'unaccepted_request_id': blocked_request,
                                             'direct_request_id': direct_request}
        with DependencyOutage('catalog') as outage:
            read(True)
            require(self.listing(history=True, state='active', catalog_path=path) == history,
                    'Catalog outage changed the existing business Grant')
            report['phase'] = 'catalog_outage_new_fulfillment_blocked'
            unavailable = self.operator.request('POST', route + '/sharing_fulfillments', (502, 503),
                dict(command, request_id=blocked_request))
            require(set(unavailable.payload) == {'error'} and bool(unavailable.payload['error']),
                    'offline business request did not return the Gateway dependency error')
            # Use an ungranted recipient so a duplicate-relation conflict cannot mask the approval check.
            direct = self.operator.request('POST', self.base + '/access_grants', (409,), {
                'request_id': direct_request, 'catalog_path': path, 'item_fingerprint': facts['item_fingerprint'],
                'expected_boundary_hash': facts['boundary_hash'], 'requirement_version': '1',
                'initialize_approval': False, 'recipient_type': 'user', 'recipient_id': outsider,
                'action': 'read', 'expiry_mode': 'at_time',
                'expires_at': (datetime.now(timezone.utc) + timedelta(minutes=3)).isoformat(),
                'reason': 'Disposable PG no implicit Catalog approval fallback'}).payload
            require(set(direct) == {'error', 'error_code'} and bool(direct['error'])
                    and direct['error_code'] == 'engine_access_grant_approval_changed',
                    'direct approval bypassed or failed to check the business approval requirement')
            requirement = self.operator.request('POST', self.base + '/access_handling_requirement', (200,),
                {key: path[key] for key in ('version', 'segments')}).payload
            require(requirement.get('mode') == 'catalog' and requirement.get('requirement_version') == '1',
                    'Catalog outage implicitly changed the approval requirement')
            require(self.listing(history=True, state='active', recipient_id=outsider, catalog_path=path)['total'] == 0
                    and self.listing(history=True, state='active', catalog_path=path) == history,
                    'offline request silently issued a new Grant')
            preview(self.outsider, target, False)
            report['phase'] = 'catalog_outage_deny_release_and_withdrawal'
            deny = self.deny(facts, catalog_path=path)
            read(False)
            self.release(deny)
            read(True)
            self.revoke(command['request_id'])
            read(False)
        report['phase'] = 'catalog_recovery_preserves_withdrawal'
        for recipient in (self.recipient, outsider):
            require(self.listing(history=True, state='active', recipient_id=recipient, catalog_path=path)['total'] == 0,
                    'Catalog recovery created or restored an active Grant')
            all_history = self.listing(history=True, recipient_id=recipient, catalog_path=path)
            require(not any(row.get('request_id') in {blocked_request, direct_request} for row in all_history['data']),
                    'a rejected offline command left a source Grant history')
        absent = self.operator.request('GET', route + '/sharing_fulfillments/' + blocked_request, (404,)).payload
        require(bool(absent.get('error')) and not any(key in absent for key in ('state', 'granted_at')),
                'offline new request unexpectedly became a Catalog fulfillment')
        require(self.reader.token == token, 'Catalog outage changed the consumer credential')
        evidence.update(unavailable_status=unavailable.status, direct_error_code=direct['error_code'],
                        same_credential=True, before=outage.before, after=outage.after)
        report['checks'].extend(['existing_grant_readable_without_catalog', 'catalog_offline_new_fulfillment_unavailable',
            'catalog_offline_no_direct_approval_fallback', 'catalog_offline_deny_release_withdrawal',
            'catalog_recovery_preserves_original_rules'])

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

    def meta_recovery_flow(self, report, facts, target, ungranted):
        report['phase'] = 'meta_dependency_recovery'
        token = self.reader.token
        grant, _ = self.grant(facts)
        evidence = report['meta_recovery'] = {'grant_id': grant}
        preview(self.reader, target, True, 2)
        with DependencyOutage('meta') as outage:
            evidence['before'] = outage.before
            report['phase'] = 'meta_outage_facts_probe'
            response = self.operator.request('GET', self.base + '/data-items/' + facts['item_fingerprint']
                                             + '/authorization-facts', (503,)).payload
            require(set(response) == {'error', 'error_code'} and response['error_code'] == 'authorization_facts_unavailable',
                    'Meta outage was not a bounded facts dependency failure')
            report['phase'] = 'meta_outage_preview_probe'
            response = self.reader.request('GET', '/api/v1/manager/preview?' + urlencode({
                'locator': target, 'page': 1, 'page_size': 20}), (503,)).payload
            require(set(response) == {'error', 'error_code'} and response['error_code'] == 'authorization_service_unavailable',
                    'Meta outage leaked content or masqueraded as an absent scan')
            report['phase'] = 'meta_outage_known_target_deny'
            deny = self.deny(facts)
            evidence['deny_id'] = deny
            report['phase'] = 'meta_dependency_restart'
        # No scan, re-login, new Grant, or rule repair is permitted during recovery.
        evidence['after'] = outage.after
        report['phase'] = 'meta_recovery_binding_and_history'
        after = REGISTRATION.read_exact_record_authorization_facts(self.operator, self.path)
        require(after['item_fingerprint'] == facts['item_fingerprint'] and after['boundary_hash'] == facts['boundary_hash']
                and self.reader.token == token, 'Meta recovery changed the resource binding or reader credential')
        active = self.listing(history=True, state='active')['data']
        require(len(active) == 1 and active[0]['request_id'] == grant and active[0].get('revocation') is None
                and active[0]['boundary_hash'] == facts['boundary_hash'], 'recovery altered the original Grant')
        report['phase'] = 'meta_recovered_deny_still_blocks'
        preview(self.reader, target, False)
        preview(self.reader, ungranted, False)
        report['phase'] = 'meta_recovery_explicit_release_and_withdrawal'
        self.release(deny)
        preview(self.reader, target, True, 2)
        self.revoke(grant)
        preview(self.reader, target, False)
        evidence.update({
            'facts_status': 503, 'unavailable_preview_status': 503, 'recovered_preview_status': 403,
            'released_preview_status': 200, 'same_credential': True, 'verified_without_rescan': True,
            'grant_id': grant, 'deny_id': deny})
        report['checks'].extend(['meta_outage_blocks_existing_grant', 'known_target_deny_created_offline',
            'meta_recovery_preserves_identity_boundary_and_grant', 'deny_survives_meta_recovery_without_rescan',
            'explicit_release_restores_original_grant_then_withdrawal_denies'])

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
        self.meta_recovery_flow(report, facts, target, ungranted)


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
        report['phase'] = 'fixture_identity_validation'
        business = API.GatewayClient(os.environ['GATEWAY_URL'], os.environ['ADDP_ONLINE_FIXTURE_CONFIRMER_ACCESS_TOKEN'], timeout)
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
        confirmer, report['confirmer'] = identity(business, tenant, CONFIRMER_PERMISSIONS)
        coordinator, report['organization_operator'] = organization_administrator(organization_operator, tenant)
        require(len({issuer, recipient, observer, coordinator, confirmer}) == 5
                and len({reader.token, operator.token, outsider.token, organization_operator.token, business.token}) == 5,
                'source operator, reader, outsider, membership operator and confirmer must be distinct Users')
        organization_memberships(reader, organizations)
        organization_memberships(outsider, {})
        for signum in (signal.SIGTERM, signal.SIGINT):
            signal.signal(signum, interrupted)
        report.update(run_id=run_id, tenant_id=str(tenant), engine_id=engine)
        report['principals'] = {'reader': recipient, 'operator': issuer, 'outsider': observer,
                                'organization_operator': coordinator, 'confirmer': confirmer}
        lifecycle = Lifecycle(operator, reader, tenant, engine, recipient, organizations, outsider,
                              organization_operator, login, os.environ['ADDP_ONLINE_TEST_USER_USERNAME'],
                              os.environ['ADDP_ONLINE_TEST_USER_PASSWORD'])
        lifecycle.run(report)
        lifecycle.catalog_flow(report, business, confirmer)
        report['result'] = 'passed'
    except (SuiteError, PREFLIGHT.PreflightError, REGISTRATION.RegistrationError, KeyError, ValueError, TypeError, OSError):
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
