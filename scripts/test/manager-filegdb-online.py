"""Bounded NFS FileGDB acceptance; no production recovery or grant policy changes."""
from __future__ import annotations

import importlib
import hashlib
import io
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
from datetime import datetime, timedelta, timezone
from urllib.parse import quote, urlencode
import uuid
import zipfile

API = importlib.import_module('scripts.utils.online-api')
FILES = importlib.import_module('scripts.test.ftp-consumer-flow-online')
REGISTRATION = importlib.import_module('scripts.test.online-engine-registration')
PERMISSIONS = {'manager.content.read', 'manager.data_item.read', 'meta.catalog.read',
               'meta.scan_task.execute', 'meta.scan_task.read', 'monitor.execution.read'}
OUTSIDER_PERMISSIONS = {'manager.content.read', 'manager.data_item.read', 'meta.catalog.read'}
NAME = 'authorization.gdb'
FUTURE_NAME = 'authorization-future.gdb'
EMPTY_NAME = 'authorization-empty'
ROOT_CHECKS = {'nfs_engine_without_grants_hidden', 'nfs_root_grant_covers_future_filegdb',
    'nfs_root_grant_shows_empty_directory', 'nfs_root_withdrawal_retains_independent_item',
    'nfs_last_empty_node_withdrawal_hides_engine', 'nfs_root_natural_expiry_hides_navigation'}
REQUIRED_CHECKS = {'basic_whole_identity', 'ungranted_child_denied', 'initial_child_actual_rows',
    'new_child_same_grant', 'manager_crash_and_source_disconnect_recovered_same_grant',
    'revoked_next_read_denied', 'natural_expiry_next_read_denied', 'read_child_sigkill_observed',
    'ungranted_account_initial_child_denied', 'ungranted_account_new_child_denied',
    'ungranted_raw_members_denied', 'raw_members_same_grant_and_account_isolation',
    'raw_members_revoked_and_expired_denied',
    'ungranted_directory_download_denied', 'directory_download_same_grant_and_new_child',
    'directory_download_account_isolation', 'directory_download_revoke_expiry',
    'directory_download_failure_cleanup_and_recovery', 'real_browser_directory_download_saved',
    'real_browser_ungranted_revoked_expired_denied', 'real_browser_child_cells_and_switch', 'real_browser_child_pagination_and_reset'} | ROOT_CHECKS


def require(value, message):
    if not value:
        raise ValueError(message)


def guard(repository, environment):
    require(environment.get('POSTGRES_DB') == 'addp_online' and not (repository / '.env').exists(),
        'FileGDB faults require the exclusive Hosted case')
    physical = importlib.import_module('scripts.test.manager-filegdb-physical')
    return physical.owned_root(environment)[0]


def target(engine, name=NAME):
    return {'version': 'catalog.path/v1', 'engine_id': engine, 'segments': [
        {'term': 'root', 'kind': 'root', 'name': ''},
        {'term': 'directory', 'kind': 'directory', 'name': name}]}


def find_item(client, engine, name=NAME):
    items = client.request('GET', f'/api/v1/meta/engines/{engine}/items', (200,), response_type=list).payload
    item, = [entry for entry in items if isinstance(entry, dict) and entry.get('full_name') == name]
    require(item.get('item_type') == 'file' and type(item.get('id')) is int and item['id'] > 0
            and re.fullmatch('[0-9a-f]{64}', item.get('fingerprint', '')), 'invalid FileGDB item identity')
    return item


def find_identity(issuer, engine):
    page = issuer.request('GET', f'/api/v1/system/engines/{engine}/data-items?' +
        urlencode({'search': NAME, 'page_size': 100}), (200,)).payload
    require(isinstance(page.get('data'), list) and page.get('total') == len(page['data']),
            'FileGDB handling identity inventory is incomplete')
    row, = [row for row in page['data'] if row.get('full_name') == NAME]
    require(row.get('item_type') == 'file' and re.fullmatch('[0-9a-f]{64}', row.get('item_fingerprint', '')),
            'FileGDB handling identity is invalid')
    return {'fingerprint': row['item_fingerprint']}


class Authorization(FILES.ModelSourceAuthorization):
    def __init__(self, issuer, consumer, report):
        super().__init__(issuer, consumer, report, source_type='nfs',
                         consumer_permissions=PERMISSIONS,
                         issuer_permissions=REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS |
                            {'meta.scan_task.execute', 'meta.scan_task.read'})
        self.target = lambda _: target(self.engine_id)
        self.frozen = None
        self.exact_records = {}

    def facts(self, item):
        facts = self.issuer.request('GET', self.base + f"/data-items/{item['fingerprint']}/authorization-facts", (200,)).payload
        expected = {'layout': 'whole', 'scope': {'format': 'filegdb', 'rule_version': 1, 'root': target(self.engine_id)}}
        require(all(facts.get(k) == v for k, v in {
            'schema_version': 'meta.data_item_authorization_facts/v1', 'engine_id': str(self.engine_id),
            'item_fingerprint': item['fingerprint'], 'item_type': 'file', 'full_name': NAME,
            'basis_state': 'complete', 'boundary': expected}.items())
            and re.fullmatch('[0-9a-f]{64}', facts.get('boundary_hash', ''))
            and bool(facts.get('observed_at')), 'incomplete or changed whole identity')
        if self.frozen:
            require(facts['boundary_hash'] == self.frozen['boundary_hash'], 'internal child changed whole boundary')
        return facts

    def grant(self, item, expires_at=None, *, action='read'):
        require(action in {'metadata_read', 'read'}, 'unsupported FileGDB action')
        facts = self.facts(item)
        self.frozen = facts

        command = str(uuid.uuid4())
        body = dict(request_id=command, catalog_path=target(self.engine_id), item_fingerprint=item['fingerprint'],
                    expected_boundary_hash=facts['boundary_hash'], recipient_type='user', recipient_id=self.recipient,
                    action=action, expiry_mode='at_time' if expires_at else 'until_revoked',
                    reason='Disposable NFS FileGDB acceptance')
        if expires_at:
            body['expires_at'] = expires_at
        self.grants.append(command)  # Own unknown outcomes before sending any write.
        self.exact_records[command] = dict(body, boundary_hash=facts['boundary_hash'],
            scope=facts['boundary']['scope'], operator_principal_id=self.operator)
        receipt = self.issuer.request('POST', self.base + '/access_grants', (201,), body).payload
        require(all(receipt.get(k) == body[k] for k in ('request_id', 'catalog_path', 'item_fingerprint',
                'recipient_type', 'recipient_id', 'action', 'expiry_mode'))
                and receipt.get('boundary_hash') == facts['boundary_hash']
                and receipt.get('scope') == facts['boundary']['scope']
                and receipt.get('operator_principal_id') == self.operator and receipt.get('revocation') is None,
                'whole Grant binding mismatch')
        require((not expires_at and receipt.get('expires_at') is None) or (expires_at
                and datetime.fromisoformat(receipt['expires_at']) == datetime.fromisoformat(expires_at)),
                'Grant expiry changed')
        self.confirmed.add(command)
        return receipt

    def current(self, *, recipient=None, action='read'):
        response = self.issuer.request('GET', self.base + '/access_grants?' + urlencode({
            'recipient_type': 'user', 'recipient_id': recipient or self.recipient, 'page_size': 100}), (200,)).payload
        require(isinstance(response.get('data'), list) and response.get('total') == len(response['data']) <= 100,
                'incomplete current FileGDB source relations')
        if action is None:
            return response
        rows = [row for row in response['data'] if row.get('action') == action]
        return dict(response, data=rows, total=len(rows))

    def history(self, command):
        payload = self.issuer.request('GET', self.base + '/access_grants/history?' + urlencode({
            'recipient_type': 'user', 'recipient_id': self.recipient, 'table_search': NAME}), (200,)).payload
        row, = [g for g in payload['data'] if g['request_id'] == command]
        require(row['item_fingerprint'] == self.frozen['item_fingerprint']
                and row['boundary_hash'] == self.frozen['boundary_hash']
                and row['scope'] == self.frozen['boundary']['scope'] and row['recipient_id'] == self.recipient,
                'Grant history binding changed')
        return row

    def close(self):
        self.cleanup()
        require(self.current(action=None)['total'] == 0, 'owned Grant survived cleanup')
        return 'zero_current_source_rules'

    def cleanup(self):
        # Expired Grants are historical facts; never invent a first revocation.
        if any(row['expiry_mode'] == 'at_time' for row in self.exact_records.values()):
            page = self.issuer.request('GET', self.base + '/access_grants/history?' + urlencode({
                'recipient_type': 'user', 'recipient_id': self.recipient, 'state': 'expired',
                'page_size': 100}), (200,)).payload
            require(isinstance(page.get('data'), list) and page.get('total') == len(page['data']) <= 100,
                    'expired FileGDB history is incomplete')
            for row in page['data']:
                command = row.get('request_id')
                if command not in self.grants or command not in self.exact_records:
                    continue
                expected = self.exact_records[command]
                require(row.get('revocation') is None and all(row.get(k) == expected[k] for k in (
                    'catalog_path', 'item_fingerprint', 'boundary_hash', 'scope', 'recipient_type',
                    'recipient_id', 'action', 'expiry_mode', 'operator_principal_id')) and
                    datetime.fromisoformat(row['expires_at']) == datetime.fromisoformat(expected['expires_at']),
                    'expired FileGDB history changed its owned identity')
                self.grants.remove(command)
                self.confirmed.discard(command)
        super().cleanup()


def preview(client, locator, child, status, rows=None):
    statuses = (status,) if isinstance(status, int) else status
    response = client.request('GET', '/api/v1/manager/preview?' + urlencode({
        'locator': locator, 'child_name': child, 'page': 1, 'page_size': 20}), statuses)
    if response.status == 200:
        data = response.payload.get('data', {})
        require(len(data.get('rows', [])) == rows and data.get('total') == rows, 'actual FileGDB child rows differ')
        require([row.get('label') for row in data['rows']] == [child + '_' + str(i) for i in range(rows)]
                and [row.get('value') for row in data['rows']] == list(range(1, rows + 1)),
                'actual FileGDB scalar values differ')
    else:
        require('data' not in response.payload and 'rows' not in response.payload, 'failed read leaked content')
        if response.status == 403:
            require(set(response.payload) == {'error'} and isinstance(response.payload['error'], str)
                    and bool(response.payload['error'].strip()), 'source denial response differs from the formal contract')
    return {'status': response.status, 'child': child, 'rows': rows}


def raw_member(client, locator, engine, member, status):
    """Consume the existing owner binary client, with the same current User."""
    owner = importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')
    binary = owner.GatewayClient(client.base_url, client.token, client.timeout)
    path = NAME + '/' + member['name']
    routes = [('stream', '/api/v1/manager/storage-stream?' + urlencode({
        'locator': locator, 'storage_ref': path})),
        ('asset', f'/api/v1/manager/storage-assets/{engine}/items/' +
         str(member['item_id']) + '/' + quote(path, safe='/'))]
    evidence = []
    for exit_name, url in routes:
        response = binary.request('GET', url, (status,))
        row = {'exit': exit_name, 'status': response.status, 'member': member['name']}
        if status == 200:
            require(bool(response.raw) and hashlib.sha256(response.raw).hexdigest() == member['sha256'],
                    'raw FileGDB member differs from physical source')
            ranged = binary.request('GET', url, (206,), headers={'Range': 'bytes=0-3'})
            require(ranged.raw == response.raw[:4] and len(ranged.raw) == 4,
                    'FileGDB Range differs from the same source member')
            row.update(sha256=member['sha256'], size=len(response.raw), range_status=ranged.status)
        else:
            require(set(response.payload) == {'error'} and isinstance(response.payload['error'], str)
                    and bool(response.payload['error'].strip()), 'raw denial is not an explicit source decision')
            ranged = binary.request('GET', url, (status,), headers={'Range': 'bytes=0-3'})
            require(set(ranged.payload) == {'error'} and isinstance(ranged.payload['error'], str)
                    and bool(ranged.payload['error'].strip()), 'raw Range denial leaked content or used a different gate')
            row['range_status'] = ranged.status
        evidence.append(row)
    return evidence


def validate_directory_zip(raw, source):
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        names = archive.namelist()
        require(len(names) == len(set(names)) and set(names) == {NAME + '/', *(NAME + '/' + name for name in source)}
                and archive.testzip() is None, 'directory package members, root or CRC differ')
        hashes = {name.removeprefix(NAME + '/'): hashlib.sha256(archive.read(name)).hexdigest()
                  for name in names if not name.endswith('/')}
        require(hashes == source, 'directory package changed or omitted source bytes')
    return {'size': len(raw), 'sha256': hashlib.sha256(raw).hexdigest(), 'members': hashes}


def validate_browser_report(payload, context, artifact):
    require(payload.get('schema_version') == 'addp.manager-filegdb-browser/v1' and payload.get('result') == 'passed',
            'FileGDB browser did not pass')
    for name in ('run_id', 'phase', 'locator', 'identity', 'status'):
        require(payload.get(name) == context[name], 'FileGDB browser identity or phase differs: ' + name)
    require(all(payload.get(name) is True for name in ('actual_button_clicked', 'current_user_bearer', 'cookie_absent'))
            and payload.get('credential_in_url') is False, 'FileGDB browser bypassed current-user button transport')
    require(all(payload.get(name) == [] for name in ('business_writes', 'unexpected_responses', 'warnings', 'page_errors')),
            'FileGDB browser has unexpected activity or diagnostics')
    anonymous = payload.get('anonymous_refresh_401')
    require(type(anonymous) is int and 0 <= anonymous <= 1, 'FileGDB browser refresh boundary differs')
    require(type(payload.get('download_requests')) is int and payload['download_requests'] == 1,
            'FileGDB browser must make exactly one download request')
    require(payload.get('screenshot') == context['screenshot'] and (artifact / context['screenshot']).is_file(),
            'FileGDB browser screenshot missing')
    destination = artifact / context['download_file']
    response_file = context['download_file'] + '.response'
    response_path = artifact / response_file
    if context['status'] == 200:
        preview = payload.get('child_preview', {})
        for phase, child, rows, previous in (
                ('initial', 'initial_points', [{'label': 'initial_points_0', 'value': '1'},
                                              {'label': 'initial_points_1', 'value': '2'}], []),
                ('added', 'added_points', [{'label': 'added_points_0', 'value': '1'}],
                 ['initial_points_0', 'initial_points_1'])):
            actual = preview.get(phase, {})
            screenshot = 'manager-filegdb-preview-' + phase + '.png'
            require(actual == dict(child=child, current_rows=len(rows), total_rows=len(rows), rows=rows,
                                   previous_labels_absent=previous, screenshot=screenshot),
                    'FileGDB browser child cells or switch differ: ' + phase)
            require((artifact / screenshot).is_file(), 'FileGDB browser child screenshot missing: ' + phase)
        require(preview.get('requests') == [dict(child=child, locator=context['locator'], status=200,
                    current_user_bearer=True, cookie_absent=True, credential_in_url=False)
                    for child in ('initial_points', 'added_points')],
                'FileGDB browser child requests or identity differ')
        pagination = payload.get('child_pagination', {})
        first = [dict(label='paging_points_' + str(i), value=str(i + 1)) for i in range(20)]
        last = [dict(label='paging_points_' + str(i), value=str(i + 1)) for i in range(20, 23)]
        states = pagination.get('states', [])
        expected = (
            ('first', 'paging_points', first, ['added_points_0'], 23, 1),
            ('next', 'paging_points', last, [row['label'] for row in first], 23, 2),
            ('previous', 'paging_points', first, [row['label'] for row in last], 23, 1),
            ('next-again', 'paging_points', last, [row['label'] for row in first], 23, 2),
            ('switch-small', 'added_points', [dict(label='added_points_0', value='1')], [row['label'] for row in last], 1, 1),
            ('switch-back', 'paging_points', first, ['added_points_0'], 23, 1))
        require(len(states) == len(expected), 'FileGDB browser pagination states missing or duplicated')
        for actual, (key, child, rows, previous, total, page) in zip(states, expected):
            screenshot = 'manager-filegdb-pagination-' + key + '.png'
            require(actual == dict(key=key, child=child, current_rows=len(rows), total_rows=total, rows=rows,
                    previous_labels_absent=previous, page=page, page_size=20, previous_disabled=page == 1,
                    next_disabled=page == (total + 19) // 20, screenshot=screenshot),
                    'FileGDB browser pagination cells, page or reset differ: ' + key)
            require((artifact / screenshot).is_file(), 'FileGDB browser pagination screenshot missing: ' + key)
        require(pagination.get('requests') == [dict(child=child, page=page, page_size=20, locator=context['locator'],
                    status=200, current_user_bearer=True, cookie_absent=True, credential_in_url=False)
                    for _, child, _, _, _, page in expected], 'FileGDB browser pagination requests or identity differ')
        require(payload.get('download_events') == 1 and payload.get('suggested_filename') == NAME + '.zip',
                'FileGDB browser did not save the original attachment')
        raw = destination.read_bytes()
        require(payload.get('response_capture') == 'same_response_clone' and payload.get('response_file') == response_file
                and response_path.is_file(), 'FileGDB original response copy is missing')
        response_bytes = response_path.read_bytes()
        require(raw == response_bytes and payload.get('response_size') == len(response_bytes)
                and payload.get('response_sha256') == hashlib.sha256(response_bytes).hexdigest(),
                'FileGDB browser download differs from the same response copy')
        physical = validate_directory_zip(raw, context['source'])
        require(payload.get('size') == physical['size'] and payload.get('sha256') == physical['sha256'],
                'FileGDB browser saved bytes differ from its proof')
        return dict(payload, physical=physical)
    require(payload.get('download_events') == 0 and payload.get('error_visible') is True
            and payload.get('success_visible') is False and not destination.exists() and not response_path.exists(),
            'FileGDB rejected browser request saved a file or claimed success')
    return payload


def run_browser(repository, environment, context):
    artifact = Path(environment['ADDP_ONLINE_ARTIFACT_DIR'])
    report_path = artifact / ('manager-filegdb-browser-' + context['phase'] + '.json')
    require(not report_path.exists() and not (artifact / context['download_file']).exists()
            and not (artifact / (context['download_file'] + '.response')).exists(),
            'FileGDB browser phase evidence already exists')
    browser_env = {key: value for key, value in environment.items()
                   if not key.startswith(('ADDP_ONLINE_FIXTURE_', 'ADDP_ONLINE_ADMIN_USER_'))
                   and key != 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'}
    browser_env['ADDP_ONLINE_MANAGER_FILEGDB_JSON'] = json.dumps(context)
    subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/manager-filegdb.spec.js',
                    '--config=playwright.online.config.js'], cwd=repository / 'console/frontend',
                   env=browser_env, check=True)
    return validate_browser_report(json.loads(report_path.read_text()), context, artifact)


def directory_download(client, locator, source, status, destination):
    owner = importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')
    binary = owner.GatewayClient(client.base_url, client.token, client.timeout)
    statuses = (status,) if isinstance(status, int) else status
    response = binary.request('GET', '/api/v1/manager/downloads/file?' + urlencode({'locator': locator}), statuses)
    result = {'status': response.status}
    if response.status == 200:
        headers = {k.lower(): v for k, v in response.headers.items()}
        require(headers.get('content-type') == 'application/zip'
                and headers.get('content-disposition') == 'attachment; filename=authorization.gdb.zip'
                and headers.get('content-length') == str(len(response.raw)), 'directory attachment headers differ')
        physical = validate_directory_zip(response.raw, source)
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes(response.raw)
        result.update(physical, archive=destination.name)
    else:
        require(isinstance(response.payload, dict) and set(response.payload) <= {'error', 'error_code'}
                and isinstance(response.payload.get('error'), str)
                and bool(response.payload['error'].strip()) and not response.raw.startswith(b'PK'),
                'failed directory download leaked an archive or lacked an explicit error')
        if response.status == 403:
            require(set(response.payload) == {'error'}, 'source denial was replaced by another protection gate')
    return result


def verify_account_isolation(authorization, client, outsider, outsider_id, locator, grant, children, report):
    """A same-Tenant reader with functional permission must not inherit another User's Grant."""
    physical = importlib.import_module('scripts.test.manager-filegdb-physical')
    require(outsider_id not in {authorization.recipient, authorization.operator}
            and outsider.token not in {client.token, authorization.issuer.token}, 'isolation requires distinct Users and credentials')
    credentials = (client.token, outsider.token)
    before = authorization.current()
    history = authorization.history(grant['request_id'])
    require(history['revocation'] is None, 'account isolation requires the original active Grant')
    observation = {'before': before, 'history_before': history, 'reads': []}
    evidence = report.setdefault('account_isolation', {'principal_id': outsider_id,
        'permissions': sorted(OUTSIDER_PERMISSIONS), 'observations': []})
    evidence['observations'].append(observation)
    observation['outsider_before'] = authorization.current(recipient=outsider_id)
    require(observation['outsider_before']['total'] == 0 and observation['outsider_before']['data'] == [],
            'outsider unexpectedly has a Grant or Deny relation')
    for child, rows in children:
        observation['reads'].append({'allowed_before': preview(client, locator, child, 200, rows),
            'outsider_denied': preview(outsider, locator, child, 403),
            'allowed_after': preview(client, locator, child, 200, rows)})
    observation['after'] = authorization.current()
    observation['history_after'] = authorization.history(grant['request_id'])
    observation['outsider_after'] = authorization.current(recipient=outsider_id)
    require(physical.rule_snapshot(observation['after']) == physical.rule_snapshot(before)
            and observation['history_after'] == history, 'outsider read changed the original Grant')
    require(observation['outsider_after']['total'] == 0 and observation['outsider_after']['data'] == [],
            'outsider read created an authorization relation')
    require(FILES.exact_user_principal(outsider, report['tenant_id'], OUTSIDER_PERMISSIONS) == outsider_id
            and (client.token, outsider.token) == credentials, 'outsider identity or credentials changed')
    observation.update(grant_id=grant['request_id'], same_grant=True, same_credentials=True)
    return observation


def verify_engine_root(authorization, client, outsider, report, physical, owner):
    """One recursive root rule; precise and empty-node sources remain independent."""
    engine, issuer = authorization.engine_id, authorization.issuer
    require(authorization.current(action=None)['total'] == 0, 'NFS root stage requires zero current rules')
    credential = client.token
    root = target(engine)
    root['segments'] = root['segments'][:1]
    proof = report['engine_navigation'] = {'observations': []}
    report['phase'] = 'nfs_engine_root_navigation'

    def history(command):
        page = issuer.request('GET', authorization.base + '/access_grants/history?' + urlencode({
            'recipient_type': 'user', 'recipient_id': authorization.recipient, 'page_size': 100}), (200,)).payload
        require(isinstance(page.get('data'), list) and page.get('total') == len(page['data']) <= 100,
                'NFS root history is incomplete')
        row, = [row for row in page['data'] if row.get('request_id') == command]
        return row

    def navigation(actor, visible, leaves=(), empty=False):
        engines = actor.request('GET', '/api/v1/manager/engines', (200,)).payload.get('data')
        require(isinstance(engines, list) and sum(row.get('id') == engine for row in engines) == int(visible),
                'NFS Manager engine visibility differs')
        tree = actor.request('GET', f'/api/v1/meta/resource-tree/{engine}?expand_depth=3',
                             (200,) if visible else (404,)).payload
        found, empty_visible = [], False
        if visible:
            require(tree.get('metadata', {}).get('engine_id') == engine, 'NFS tree changed engine')
            pending = [tree]
            while pending:
                row = pending.pop()
                meta = row.get('metadata', {})
                if meta.get('item_id'): found.append(meta.get('full_name'))
                if meta.get('full_name') == EMPTY_NAME:
                    require(row.get('type') == 'directory' and row.get('children') == []
                            and row.get('hasChildren') is False and meta.get('item_count') == 0,
                            'authorized NFS empty directory lost its identity')
                    empty_visible = True
                pending.extend(row.get('children', []))
            require(sorted(found) == sorted(leaves) and empty_visible == empty,
                    'NFS tree omitted authorized data or exposed another target')
        else:
            require(set(tree) <= {'error', 'error_code'} and bool(tree.get('error')), 'hidden NFS tree leaked identity')
        proof['observations'].append(dict(principal='reader' if actor is client else 'outsider',
            visible=visible, leaf_names=found, empty_visible=empty_visible))

    navigation(client, False)
    navigation(outsider, False)
    report['checks'].append('nfs_engine_without_grants_hidden')
    physical('root-seed')
    scan = owner.wait_for_meta_scan(issuer, engine, time.monotonic() + 180,
        depth='basic', scope={'catalog_paths': [EMPTY_NAME]})
    execution = issuer.request('GET', f'/api/v1/meta/executions/{scan}', (200,)).payload
    require(execution.get('status') == 'success' and execution.get('metadata', {}).get('items_scanned') == 0
            and execution.get('metadata', {}).get('catalog_nodes_scanned') == 1,
            'NFS empty directory identification differs')
    proof['empty_scan_execution'] = scan
    inventory = issuer.request('GET', authorization.base + '/data-items?page_size=100', (200,)).payload
    require(isinstance(inventory.get('data'), list) and inventory.get('total') == len(inventory['data']) <= 100,
            'NFS root inventory is incomplete')
    names = [row['full_name'] for row in inventory['data']]
    require(NAME in names and FUTURE_NAME not in names, 'future FileGDB already identified')
    original = find_identity(issuer, engine)
    root_grant = authorization.node_grant(root, action='read', recursive=True, seconds=None)
    proof['root_grant'] = root_grant['request_id']
    original_item = find_item(client, engine)
    original_locator = f"addp://engine/{engine}/path/{NAME}?type=file&item_id={original_item['id']}"
    navigation(client, True, names, True)
    report['checks'].append('nfs_root_grant_shows_empty_directory')
    physical('root-future')
    proof['future_basic_scan'] = owner.wait_for_meta_scan(issuer, engine, time.monotonic() + 180,
        depth='basic', scope={'catalog_paths': [FUTURE_NAME]})
    future = find_item(client, engine, FUTURE_NAME)
    future_locator = f"addp://engine/{engine}/path/{FUTURE_NAME}?type=file&item_id={future['id']}"
    preview(client, future_locator, 'initial_points', 200, 2)
    records = issuer.request('GET', authorization.base + '/access_grants/history?page_size=100', (200,)).payload
    require(records.get('total') == len(records.get('data', [])) <= 100
            and not any(row.get('item_fingerprint') == future['fingerprint'] for row in records['data']),
            'future FileGDB acquired a precise Grant')
    proof.update(future_item_id=future['id'], future_item_rows=2, no_leaf_grants=True)
    names.append(FUTURE_NAME)
    navigation(client, True, names, True)
    navigation(outsider, False)
    report['checks'].append('nfs_root_grant_covers_future_filegdb')
    empty_grant = authorization.node_grant(target(engine, EMPTY_NAME), action='metadata_read', recursive=False, seconds=None)
    leaf = authorization.grant(original)
    leaf_history = authorization.history(leaf['request_id'])
    proof.update(empty_grant=empty_grant['request_id'], independent_grant=leaf['request_id'])
    authorization.revoke(root_grant['request_id'])
    require(history(root_grant['request_id']).get('revocation') is not None, 'root withdrawal history missing')
    navigation(client, True, [NAME], True)
    proof['independent_item_read'] = preview(client, original_locator, 'initial_points', 200, 2)
    require(authorization.history(leaf['request_id']) == leaf_history,
            'root withdrawal changed the independent item Grant')
    preview(client, future_locator, 'initial_points', 403)
    report['checks'].append('nfs_root_withdrawal_retains_independent_item')
    authorization.revoke(leaf['request_id'])
    navigation(client, True, [], True)
    authorization.revoke(empty_grant['request_id'])
    navigation(client, False)
    report['checks'].append('nfs_last_empty_node_withdrawal_hides_engine')
    expiry = authorization.node_grant(root, action='read', recursive=True, seconds=45)
    proof['expiry_grant'] = expiry['request_id']
    navigation(client, True, names, True)
    preview(client, future_locator, 'initial_points', 200, 2)
    expires = datetime.fromisoformat(expiry['expires_at'])
    while datetime.now(timezone.utc) <= expires:
        time.sleep(.2)
    preview(client, future_locator, 'initial_points', 403)
    expired = history(expiry['request_id'])
    require(expired.get('revocation') is None and expired.get('catalog_path') == root
            and expired.get('node_recursive') is True and expired.get('action') == 'read'
            and expired.get('recipient_id') == authorization.recipient
            and expired.get('operator_principal_id') == authorization.operator
            and datetime.fromisoformat(expired['expires_at']) == expires,
            'root expiry changed its original scope or became a withdrawal')
    navigation(client, False)
    authorization.cleanup()
    require(authorization.current(action=None)['total'] == 0 and client.token == credential,
            'NFS root stage changed credential or retained a rule')
    proof.update(result='passed', same_credential=True, natural_expiry_without_revocation=True)
    report['checks'].append('nfs_root_natural_expiry_hides_navigation')


def kill_read_child(client_environment, locator, secret):
    """The parent owns all commands; a killed read-only child owns no cleanup."""
    marker = secret / 'filegdb-child-ready.json'
    marker.unlink(missing_ok=True)
    env = {key: value for key, value in client_environment.items() if key in {
        'PATH', 'PYTHONPATH', 'GATEWAY_URL', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN', 'ADDP_ONLINE_SECRET_DIR'}}
    env['ADDP_FILEGDB_CHILD_LOCATOR'] = locator
    child = subprocess.Popen([sys.executable, '-m', 'scripts.test.manager-filegdb-online', '--read-child'],
                             env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        deadline = time.monotonic() + 30
        while not marker.exists():
            require(child.poll() is None and time.monotonic() < deadline, 'read child failed before confirmed read')
            time.sleep(.1)
        evidence = json.loads(marker.read_text())
        require(evidence == {'pid': child.pid, 'status': 200, 'rows': 1}, 'read child confirmation mismatch')
        child.kill()
        require(child.wait(timeout=10) == -signal.SIGKILL, 'child was not forcibly terminated')
        return dict(evidence, killed=True)
    finally:
        if child.poll() is None:
            child.kill()
            child.wait(timeout=10)
        marker.unlink(missing_ok=True)


def run(repository, environment, owner):
    root = guard(repository, environment)
    artifact, secret = Path(environment['ADDP_ONLINE_ARTIFACT_DIR']), Path(environment['ADDP_ONLINE_SECRET_DIR'])
    client = API.GatewayClient(environment['GATEWAY_URL'], environment['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30)
    issuer = API.GatewayClient(environment['GATEWAY_URL'], environment['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'], 30)
    engine, tenant = int(environment['ADDP_ONLINE_MANAGER_NFS_ENGINE_ID']), int(environment['ADDP_ONLINE_TEST_TENANT_ID'])
    principal = FILES.exact_user_principal(client, tenant, PERMISSIONS)
    report = dict(schema_version='addp.manager-filegdb/v1', suite='manager-internal-artifact-lineage',
                  case='filegdb-authorization', run_id=environment['ADDP_ONLINE_TEST_RUN_ID'],
                  tenant_id=tenant, principal_id=principal, engine_id=engine, source_engine_type='nfs',
                  result='failed', checks=[], cleanup='pending')
    authorization = Authorization(issuer, client, report)
    outsider = None
    outsider_id = None
    locator = None
    primary_completed = False
    fixture = importlib.import_module('scripts.test.manager-filegdb-physical')
    def physical(action):
        return fixture.physical(action, root)
    def check(name):
        report['checks'].append(name)
    def download(actor, status, phase):
        require(re.fullmatch('[a-z-]+', phase), 'invalid directory evidence name')
        records = report.setdefault('directory_downloads', {})
        require(phase not in records, 'directory evidence phase repeated')
        records[phase] = {'result': 'pending'}  # Even an unexpected first response must reach final archive cleanup.
        result = directory_download(actor, locator, physical('snapshot'), status,
            artifact / 'directory-downloads' / (phase + '.zip'))
        if phase != 'fault-manager':
            result['temporary_archives_after'] = fixture.archive_state(repository, environment)
            require(result['temporary_archives_after']['open_archives'] == 0, 'request left a temporary archive open')
        records[phase] = result
        return result
    def browser(phase, status, grant=None):
        records = report.setdefault('browser', {})
        require(phase not in records, 'FileGDB browser phase repeated')
        records[phase] = {'result': 'pending'}
        before, source = authorization.current(), physical('snapshot')
        history = authorization.history(grant['request_id']) if grant else None
        context = {'run_id': report['run_id'], 'phase': phase, 'status': status, 'locator': locator,
            'identity': {'principal_id': principal, 'tenant_id': str(tenant), 'permissions_verified': sorted(PERMISSIONS)},
            'source': source, 'screenshot': 'manager-filegdb-' + phase + '.png',
            'download_file': 'manager-filegdb-' + phase + '.zip'}
        result = run_browser(repository, environment, context)
        after = authorization.current()
        require(fixture.rule_snapshot(before) == fixture.rule_snapshot(after)
                and physical('snapshot') == source
                and (authorization.history(grant['request_id']) if grant else None) == history,
                'FileGDB browser changed source or authorization')
        result.update(current_before=before, current_after=after, history_before=history,
                      history_after=history, temporary_archives_after=fixture.archive_state(repository, environment))
        require(result['temporary_archives_after']['open_archives'] == 0, 'browser left a temporary archive open')
        records[phase] = result
    try:
        outsider = API.GatewayClient(environment['GATEWAY_URL'], environment['ADDP_ONLINE_FIXTURE_OUTSIDER_ACCESS_TOKEN'], 30)
        outsider_id = FILES.exact_user_principal(outsider, tenant, OUTSIDER_PERMISSIONS)
        require(outsider_id not in {principal, authorization.operator}, 'outsider must be a separate User')
        physical('seed')
        API.wait_manager_read_boundary(client, engine, time.monotonic() + 120, target_visible=False)
        owner.wait_for_meta_scan(issuer, engine, time.monotonic() + 180, depth='basic', scope={'catalog_paths': [NAME]})
        seed_identity = find_identity(issuer, engine)
        metadata_grant = authorization.grant(seed_identity, action='metadata_read')
        report['metadata_visibility_grant'] = metadata_grant
        item = find_item(client, engine)
        require(item['fingerprint'] == seed_identity['fingerprint'], 'visible FileGDB changed handling identity')
        facts = authorization.facts(item)
        report['identity'] = dict(item_id=item['id'], fingerprint=item['fingerprint'], boundary_hash=facts['boundary_hash'])
        check('basic_whole_identity')
        locator = f"addp://engine/{engine}/path/{NAME}?type=file&item_id={item['id']}"
        actions = client.request('GET', '/api/v1/manager/resource-actions?' + urlencode({'locator': locator}), (200,)).payload
        require(actions['actions']['download'].get('supported') is True
                and actions['actions']['download'].get('requires_current_user') is True,
                'FileGDB capability does not match actual authenticated directory download')
        report['directory_download_capability'] = actions['actions']['download']
        snapshot = physical('snapshot')
        member, = sorted(name for name in snapshot if name == 'a00000001.gdbtable')
        report['raw_member'] = {'name': member, 'sha256': snapshot[member], 'item_id': item['id']}
        preview(client, locator, 'initial_points', 403)
        check('ungranted_child_denied')
        report['raw_ungranted'] = raw_member(client, locator, engine, report['raw_member'], 403)
        check('ungranted_raw_members_denied')
        download(client, 403, 'ungranted')
        check('ungranted_directory_download_denied')
        browser('ungranted', 403)
        first = authorization.grant(item)
        report['first_grant'] = first
        owner.wait_for_meta_scan(issuer, engine, time.monotonic() + 180, scope={'catalog_paths': [NAME]})
        preview(client, locator, 'initial_points', 200, 2)
        check('initial_child_actual_rows')
        initial_archive = download(client, 200, 'initial')
        verify_account_isolation(authorization, client, outsider, outsider_id, locator, first,
            [('initial_points', 2)], report)
        check('ungranted_account_initial_child_denied')
        physical('add')
        owner.wait_for_meta_scan(issuer, engine, time.monotonic() + 180, scope={'catalog_paths': [NAME]})
        authorization.facts(item)
        preview(client, locator, 'added_points', 200, 1)
        check('new_child_same_grant')
        added_archive = download(client, 200, 'new-child')
        require(added_archive['members'] != initial_archive['members'], 'new child did not enter the current whole archive')
        check('directory_download_same_grant_and_new_child')
        browser('granted', 200, first)
        check('real_browser_directory_download_saved')
        check('real_browser_child_cells_and_switch')
        check('real_browser_child_pagination_and_reset')
        verify_account_isolation(authorization, client, outsider, outsider_id, locator, first,
            [('initial_points', 2), ('added_points', 1)], report)
        check('ungranted_account_new_child_denied')
        report['raw_member']['sha256'] = physical('snapshot')[member]
        raw_before, raw_history = authorization.current(), authorization.history(first['request_id'])
        report['raw_isolation'] = {
            'allowed_before': raw_member(client, locator, engine, report['raw_member'], 200),
            'outsider_denied': raw_member(outsider, locator, engine, report['raw_member'], 403),
            'allowed_after': raw_member(client, locator, engine, report['raw_member'], 200)}
        download(client, 200, 'account-before')
        download(outsider, 403, 'outsider')
        download(client, 200, 'account-after')
        raw_outsider = authorization.current(recipient=outsider_id)
        raw_after, raw_history_after = authorization.current(), authorization.history(first['request_id'])
        require(fixture.rule_snapshot(raw_after) == fixture.rule_snapshot(raw_before)
                and raw_history_after == raw_history
                and raw_outsider.get('data') == [] and raw_outsider.get('total') == 0,
                'raw account isolation changed the original Grant or created an outsider relation')
        report['raw_isolation'].update(before=raw_before, after=raw_after, history_before=raw_history,
            history_after=raw_history_after, outsider_after=raw_outsider, grant_id=first['request_id'], same_grant=True)
        check('raw_members_same_grant_and_account_isolation')
        check('directory_download_account_isolation')
        report['source_snapshot'] = physical('snapshot')
        report['original_current'] = authorization.current()
        # Product lifetime and source-connectivity faults have no authority to revoke a Grant.
        fault = importlib.import_module('scripts.test.manager-filegdb-physical')
        report['outages'] = fault.verify_outages(repository, environment, client, locator,
            lambda: authorization.current(), lambda: physical('snapshot'), preview, download)
        check('manager_crash_and_source_disconnect_recovered_same_grant')
        check('directory_download_failure_cleanup_and_recovery')
        authorization.revoke(first['request_id'])
        preview(client, locator, 'added_points', 403)
        report['raw_revoked'] = raw_member(client, locator, engine, report['raw_member'], 403)
        download(client, 403, 'revoked')
        report['revoked_history'] = authorization.history(first['request_id'])
        require(report['revoked_history']['revocation'] is not None and authorization.current()['total'] == 0,
                'withdrawal history or active state differs')
        check('revoked_next_read_denied')
        browser('revoked', 403, first)
        expires = datetime.now(timezone.utc) + timedelta(seconds=45)
        second = authorization.grant(item, expires.isoformat())
        report['expiry_grant'] = second
        preview(client, locator, 'added_points', 200, 1)
        report['raw_expiry_before'] = raw_member(client, locator, engine, report['raw_member'], 200)
        download(client, 200, 'expiry-before')
        while datetime.now(timezone.utc) <= expires:
            time.sleep(.2)
        preview(client, locator, 'added_points', 403)
        report['raw_expired'] = raw_member(client, locator, engine, report['raw_member'], 403)
        download(client, 403, 'expired')
        expired = authorization.history(second['request_id'])
        report['expired_history'] = expired
        require(expired['revocation'] is None and authorization.current()['total'] == 0, 'expiry was replaced by withdrawal')
        check('natural_expiry_next_read_denied')
        check('raw_members_revoked_and_expired_denied')
        check('directory_download_revoke_expiry')
        # Preserve the natural-expiry receipt; do not issue an artificial revoke for it.
        authorization.grants.remove(second['request_id'])
        authorization.confirmed.remove(second['request_id'])
        browser('expired', 403, second)
        require(set(report['browser']) == {'ungranted', 'granted', 'revoked', 'expired'}, 'missing FileGDB browser phase')
        check('real_browser_ungranted_revoked_expired_denied')
        third = authorization.grant(item)
        report['crash_grant'] = third
        report['child_crash'] = kill_read_child(environment, locator, secret)
        check('read_child_sigkill_observed')
        authorization.cleanup()
        verify_engine_root(authorization, client, outsider, report, physical, owner)
        primary_completed = True
    except BaseException as error:
        report['error_type'] = type(error).__name__
        raise
    finally:
        failures = []
        try:
            report['authorization_cleanup'] = authorization.close()
            report['final_current'] = authorization.current()
            if outsider_id:
                report['final_outsider_current'] = authorization.current(recipient=outsider_id)
                require(report['final_outsider_current']['total'] == 0 and report['final_outsider_current']['data'] == [],
                        'outsider retained an authorization relation')
            if 'crash_grant' in report:
                report['crash_grant_history'] = authorization.history(report['crash_grant']['request_id'])
                require(report['crash_grant_history']['revocation'] is not None, 'crash Grant withdrawal history missing')
            if locator:
                preview(client, locator, 'added_points', 403)
            report['cleanup'] = 'zero_current_source_rules'
        except Exception as error:
            failures.append({'stage': 'authorization', 'error_type': type(error).__name__})
        if report.get('directory_downloads'):
            try:
                report['directory_archive_cleanup'] = fixture.archive_state(repository, environment)
                require(report['directory_archive_cleanup']['open_archives'] == 0,
                        'temporary directory archive survived failure cleanup')
            except Exception as error:
                failures.append({'stage': 'directory_archive_cleanup', 'error_type': type(error).__name__})
        try:
            report['source_after'] = physical('snapshot')
            if 'source_snapshot' in report:
                require(report['source_after'] == report['source_snapshot'], 'read or fault modified source')
        except Exception as error:
            failures.append({'stage': 'source_snapshot', 'error_type': type(error).__name__})
        try:
            report['root_source_cleanup'] = physical('root-delete')
            if report.get('engine_navigation', {}).get('result') == 'passed':
                require(report['root_source_cleanup'] == {'root_source_cleanup': 'passed'}, 'root source cleanup not confirmed')
        except Exception as error:
            failures.append({'stage': 'root_source_delete', 'error_type': type(error).__name__})
        try:
            report['physical_cleanup'] = physical('delete')
            require(report['physical_cleanup'] == {'source_removed': True}, 'test source removal not confirmed')
        except Exception as error:
            failures.append({'stage': 'source_delete', 'error_type': type(error).__name__})
        report['cleanup_errors'] = failures
        report['result'] = 'passed' if primary_completed and not failures and len(report['checks']) == len(REQUIRED_CHECKS) and set(report['checks']) == REQUIRED_CHECKS else 'failed'
        (artifact / 'manager-filegdb.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
        require(not failures, 'FileGDB cleanup failed')
    require(report['result'] == 'passed', 'FileGDB acceptance incomplete')
    return report


if __name__ == '__main__':
    require(sys.argv[1:] == ['--read-child'], 'only the private read child is directly callable')
    client = API.GatewayClient(os.environ['GATEWAY_URL'], os.environ['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30)
    preview(client, os.environ['ADDP_FILEGDB_CHILD_LOCATOR'], 'added_points', 200, 1)
    path = Path(os.environ['ADDP_ONLINE_SECRET_DIR']) / 'filegdb-child-ready.json'
    path.write_text(json.dumps({'pid': os.getpid(), 'status': 200, 'rows': 1}))
    while True:
        time.sleep(1)
