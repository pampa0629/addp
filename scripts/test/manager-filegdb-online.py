"""Bounded NFS FileGDB acceptance; no production recovery or grant policy changes."""
from __future__ import annotations

import importlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
from datetime import datetime, timedelta, timezone
from urllib.parse import urlencode
import uuid

API = importlib.import_module('scripts.utils.online-api')
FILES = importlib.import_module('scripts.test.ftp-consumer-flow-online')
REGISTRATION = importlib.import_module('scripts.test.online-engine-registration')
PERMISSIONS = {'manager.content.read', 'manager.data_item.read', 'meta.catalog.read',
               'meta.scan_task.execute', 'meta.scan_task.read', 'monitor.execution.read'}
NAME = 'authorization.gdb'
REQUIRED_CHECKS = {'basic_whole_identity', 'ungranted_child_denied', 'initial_child_actual_rows',
    'new_child_same_grant', 'manager_crash_and_source_disconnect_recovered_same_grant',
    'revoked_next_read_denied', 'natural_expiry_next_read_denied', 'read_child_sigkill_observed'}


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


class Authorization(FILES.ModelSourceAuthorization):
    def __init__(self, issuer, consumer, report):
        super().__init__(issuer, consumer, report, source_type='nfs',
                         consumer_permissions=PERMISSIONS,
                         issuer_permissions=REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS)
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

    def current(self):
        response = self.issuer.request('GET', self.base + '/access_grants?' + urlencode({
            'recipient_type': 'user', 'recipient_id': self.recipient, 'table_search': NAME}), (200,)).payload
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
    return {'status': response.status, 'child': child, 'rows': rows}


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
    locator = None
    primary_completed = False
    fixture = importlib.import_module('scripts.test.manager-filegdb-physical')
    def physical(action):
        return fixture.physical(action, root)
    def check(name):
        report['checks'].append(name)
    try:
        physical('seed')
        API.wait_manager_read_boundary(client, engine, time.monotonic() + 120)
        owner.wait_for_meta_scan(client, engine, time.monotonic() + 180, depth='basic', scope={'catalog_paths': [NAME]})
        item = owner.find_fixture_item(client, engine, NAME, 'FileGDB')
        facts = authorization.facts(item)
        report['identity'] = dict(item_id=item['id'], fingerprint=item['fingerprint'], boundary_hash=facts['boundary_hash'])
        check('basic_whole_identity')
        locator = f"addp://engine/{engine}/path/{NAME}?type=file&item_id={item['id']}"
        preview(client, locator, 'initial_points', 403)
        check('ungranted_child_denied')
        first = authorization.grant(item)
        report['first_grant'] = first
        owner.wait_for_meta_scan(client, engine, time.monotonic() + 180, scope={'catalog_paths': [NAME]})
        preview(client, locator, 'initial_points', 200, 2)
        check('initial_child_actual_rows')
        physical('add')
        owner.wait_for_meta_scan(client, engine, time.monotonic() + 180, scope={'catalog_paths': [NAME]})
        authorization.facts(item)
        preview(client, locator, 'added_points', 200, 1)
        check('new_child_same_grant')
        report['source_snapshot'] = physical('snapshot')
        report['original_current'] = authorization.current()
        # Product lifetime and source-connectivity faults have no authority to revoke a Grant.
        fault = importlib.import_module('scripts.test.manager-filegdb-physical')
        report['outages'] = fault.verify_outages(repository, environment, client, locator,
            lambda: authorization.current(), lambda: physical('snapshot'), preview)
        check('manager_crash_and_source_disconnect_recovered_same_grant')
        authorization.revoke(first['request_id'])
        preview(client, locator, 'added_points', 403)
        report['revoked_history'] = authorization.history(first['request_id'])
        require(report['revoked_history']['revocation'] is not None and authorization.current()['total'] == 0,
                'withdrawal history or active state differs')
        check('revoked_next_read_denied')
        expires = datetime.now(timezone.utc) + timedelta(seconds=45)
        second = authorization.grant(item, expires.isoformat())
        report['expiry_grant'] = second
        preview(client, locator, 'added_points', 200, 1)
        while datetime.now(timezone.utc) <= expires:
            time.sleep(.2)
        preview(client, locator, 'added_points', 403)
        expired = authorization.history(second['request_id'])
        report['expired_history'] = expired
        require(expired['revocation'] is None and authorization.current()['total'] == 0, 'expiry was replaced by withdrawal')
        check('natural_expiry_next_read_denied')
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
            if 'crash_grant' in report:
                report['crash_grant_history'] = authorization.history(report['crash_grant']['request_id'])
                require(report['crash_grant_history']['revocation'] is not None, 'crash Grant withdrawal history missing')
            if locator:
                preview(client, locator, 'added_points', 403)
            report['cleanup'] = 'zero_current_source_rules'
        except Exception as error:
            failures.append({'stage': 'authorization', 'error_type': type(error).__name__})
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
