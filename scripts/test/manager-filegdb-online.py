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
OUTSIDER_PERMISSIONS = {'manager.content.read', 'manager.data_item.read'}
NAME = 'authorization.gdb'
REQUIRED_CHECKS = {'basic_whole_identity', 'ungranted_child_denied', 'initial_child_actual_rows',
    'new_child_same_grant', 'manager_crash_and_source_disconnect_recovered_same_grant',
    'revoked_next_read_denied', 'natural_expiry_next_read_denied', 'read_child_sigkill_observed',
    'explicit_deny_overrides_whole_grant', 'deny_release_restores_original_grant',
    'ungranted_account_initial_child_denied', 'ungranted_account_new_child_denied',
    'ungranted_raw_members_denied', 'raw_members_same_grant_and_account_isolation',
    'raw_members_revoked_and_expired_denied',
    'ungranted_directory_download_denied', 'directory_download_same_grant_and_new_child',
    'directory_download_account_isolation_and_deny', 'directory_download_revoke_expiry',
    'directory_download_failure_cleanup_and_recovery'}


def require(value, message):
    if not value:
        raise ValueError(message)


def guard(repository, environment):
    require(environment.get('POSTGRES_DB') == 'addp_online' and not (repository / '.env').exists(),
        'FileGDB faults require the exclusive Hosted case')
    physical = importlib.import_module('scripts.test.manager-filegdb-physical')
    return physical.owned_root(environment)[0]


def target(engine):
    return {'version': 'catalog.path/v1', 'engine_id': engine, 'segments': [
        {'term': 'root', 'kind': 'root', 'name': ''},
        {'term': 'directory', 'kind': 'directory', 'name': NAME}]}


def find_item(client, engine):
    items = client.request('GET', f'/api/v1/meta/engines/{engine}/items', (200,), response_type=list).payload
    item, = [entry for entry in items if isinstance(entry, dict) and entry.get('full_name') == NAME]
    require(item.get('item_type') == 'file' and type(item.get('id')) is int and item['id'] > 0
            and re.fullmatch('[0-9a-f]{64}', item.get('fingerprint', '')), 'invalid FileGDB item identity')
    return item


class Authorization(FILES.ModelSourceAuthorization):
    def __init__(self, issuer, consumer, report):
        super().__init__(issuer, consumer, report, source_type='nfs',
                         consumer_permissions=PERMISSIONS,
                         issuer_permissions=FILES.ISSUER_PERMISSIONS)
        self.target = lambda _: target(self.engine_id)
        self.frozen = None

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

    def grant(self, item, expires_at=None):
        facts = self.facts(item)
        self.frozen = facts
        permission = REGISTRATION.ensure_direct_permission(self.issuer, target(self.engine_id), facts)
        command = str(uuid.uuid4())
        body = dict(request_id=command, catalog_path=target(self.engine_id), item_fingerprint=item['fingerprint'],
                    expected_boundary_hash=facts['boundary_hash'], direct_permission_source=permission['source'],
                    direct_permission_version=permission['version'], recipient_type='user', recipient_id=self.recipient,
                    action='read', expiry_mode='at_time' if expires_at else 'until_revoked',
                    reason='Disposable NFS FileGDB acceptance')
        if expires_at:
            body['expires_at'] = expires_at
        self.grants.append(command)  # Own unknown outcomes before sending any write.
        receipt = self.issuer.request('POST', self.base + '/access_grants', (201,), body).payload
        require(all(receipt.get(k) == body[k] for k in ('request_id', 'catalog_path', 'item_fingerprint',
                'recipient_type', 'recipient_id', 'action', 'expiry_mode', 'direct_permission_source', 'direct_permission_version'))
                and receipt.get('boundary_hash') == facts['boundary_hash']
                and receipt.get('scope') == facts['boundary']['scope']
                and receipt.get('operator_principal_id') == self.operator and receipt.get('revocation') is None,
                'whole Grant binding mismatch')
        require((not expires_at and receipt.get('expires_at') is None) or (expires_at
                and datetime.fromisoformat(receipt['expires_at']) == datetime.fromisoformat(expires_at)),
                'Grant expiry changed')
        self.confirmed.add(command)
        return receipt

    def current(self, *, recipient=None):
        response = self.issuer.request('GET', self.base + '/access_grants?' + urlencode({
            'recipient_type': 'user', 'recipient_id': recipient or self.recipient, 'table_search': NAME}), (200,)).payload
        require(response.get('total') in (0, 1), 'unexpected active source count')
        return response

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
        require(self.current()['total'] == 0, 'owned Grant survived cleanup')
        if self.frozen:
            permission = REGISTRATION.set_direct_permission(self.issuer, target(self.engine_id), self.frozen, False)
            require(permission['state'] == 'denied', 'test permission remained open')
            return permission


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
        with zipfile.ZipFile(io.BytesIO(response.raw)) as archive:
            names = archive.namelist()
            require(len(names) == len(set(names)) and set(names) == {NAME + '/', *(NAME + '/' + name for name in source)}
                    and archive.testzip() is None, 'directory package members, root or CRC differ')
            hashes = {name.removeprefix(NAME + '/'): hashlib.sha256(archive.read(name)).hexdigest()
                      for name in names if not name.endswith('/')}
            require(hashes == source, 'directory package changed or omitted source bytes')
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes(response.raw)
        result.update(size=len(response.raw), sha256=hashlib.sha256(response.raw).hexdigest(),
                      members=hashes, archive=destination.name)
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


def verify_deny(authorization, client, item, locator, grant, report, download):
    """A Deny blocks a live Grant; release restores that same rule without signing."""
    physical = importlib.import_module('scripts.test.manager-filegdb-physical')
    token = client.token
    before = authorization.current()
    history = authorization.history(grant['request_id'])
    require(history['revocation'] is None, 'Deny control requires the original active Grant')
    evidence = report['deny_priority'] = {'grant_id': grant['request_id'], 'before': before,
        'history_before': history, 'denied_reads': [], 'restored_reads': []}
    command = authorization.deny(item)  # Existing owner ledger registers unknown outcomes before POST.
    evidence['deny_id'] = command
    denied = evidence['during'] = authorization.current()
    expected = physical.rule_snapshot(before)
    require(expected['total'] == 1 and len(expected['data']) == 1, 'Deny control requires one source relation')
    row = expected['data'][0]
    require(row['grant_count'] == 1 and row['inspection']['reason'] == 'grant'
            and row['inspection']['rule_covered'] is True and row['inspection']['denies'] == []
            and len(row['inspection']['sources']) == 1
            and row['inspection']['sources'][0]['request_id'] == grant['request_id'],
            'Deny control requires the exact original Grant')
    row['inspection'].update(reason='explicit_deny', rule_covered=False, denies=[{
        'deny_id': command, 'recipient_type': 'user', 'recipient_id': authorization.recipient,
        'expiry_mode': 'until_revoked', 'expires_at': None}])
    require(physical.rule_snapshot(denied) == expected, 'Deny changed Grant sources or did not take priority')
    for child in ('initial_points', 'added_points'):
        evidence['denied_reads'].append(preview(client, locator, child, 403))
    evidence['raw_denied'] = raw_member(client, locator, report['engine_id'], report['raw_member'], 403)
    evidence['directory_denied'] = download(client, 403, 'deny')
    evidence['history_during'] = authorization.history(grant['request_id'])
    require(evidence['history_during'] == history, 'Deny secretly revoked or changed the Grant')
    authorization.release(command)
    evidence['after'] = authorization.current()
    require(physical.rule_snapshot(evidence['after']) == physical.rule_snapshot(before),
            'Deny release did not restore the original source relation')
    for child, rows in (('initial_points', 2), ('added_points', 1)):
        evidence['restored_reads'].append(preview(client, locator, child, 200, rows))
    evidence['raw_restored'] = raw_member(client, locator, report['engine_id'], report['raw_member'], 200)
    evidence['directory_restored'] = download(client, 200, 'deny-release')
    evidence['history_after'] = authorization.history(grant['request_id'])
    require(evidence['history_after'] == history and client.token == token,
            'Deny release replaced the Grant or reader credential')
    require(FILES.exact_user_principal(client, report['tenant_id'], PERMISSIONS) == authorization.recipient,
            'Deny control changed the reader identity or permissions')
    evidence.update(same_grant=True, same_credential=True, deny_released=True)
    return evidence


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
    try:
        outsider = API.GatewayClient(environment['GATEWAY_URL'], environment['ADDP_ONLINE_FIXTURE_OUTSIDER_ACCESS_TOKEN'], 30)
        outsider_id = FILES.exact_user_principal(outsider, tenant, OUTSIDER_PERMISSIONS)
        require(outsider_id not in {principal, authorization.operator}, 'outsider must be a separate User')
        physical('seed')
        API.wait_manager_read_boundary(client, engine, time.monotonic() + 120)
        owner.wait_for_meta_scan(client, engine, time.monotonic() + 180, depth='basic', scope={'catalog_paths': [NAME]})
        item = find_item(client, engine)
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
        first = authorization.grant(item)
        report['first_grant'] = first
        owner.wait_for_meta_scan(client, engine, time.monotonic() + 180, scope={'catalog_paths': [NAME]})
        preview(client, locator, 'initial_points', 200, 2)
        check('initial_child_actual_rows')
        initial_archive = download(client, 200, 'initial')
        verify_account_isolation(authorization, client, outsider, outsider_id, locator, first,
            [('initial_points', 2)], report)
        check('ungranted_account_initial_child_denied')
        physical('add')
        owner.wait_for_meta_scan(client, engine, time.monotonic() + 180, scope={'catalog_paths': [NAME]})
        authorization.facts(item)
        preview(client, locator, 'added_points', 200, 1)
        check('new_child_same_grant')
        added_archive = download(client, 200, 'new-child')
        require(added_archive['members'] != initial_archive['members'], 'new child did not enter the current whole archive')
        check('directory_download_same_grant_and_new_child')
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
        verify_deny(authorization, client, item, locator, first, report, download)
        check('explicit_deny_overrides_whole_grant')
        check('deny_release_restores_original_grant')
        check('directory_download_account_isolation_and_deny')
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
        third = authorization.grant(item)
        report['crash_grant'] = third
        report['child_crash'] = kill_read_child(environment, locator, secret)
        check('read_child_sigkill_observed')
        primary_completed = True
    except BaseException as error:
        report['error_type'] = type(error).__name__
        raise
    finally:
        failures = []
        try:
            report['direct_permission_after'] = authorization.close()
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
