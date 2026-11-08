"""Hosted NFS committed-upload -> Meta -> canonical Grant acceptance."""
from __future__ import annotations
import base64
import hashlib
import importlib
import json
from pathlib import Path
import re
import time
from urllib.parse import urlencode
import uuid

API = importlib.import_module('scripts.utils.online-api')
FILES = importlib.import_module('scripts.test.ftp-consumer-flow-online')
PERMISSIONS = {'manager.data_item.create', 'manager.data_item.read', 'manager.content.read', 'meta.catalog.read'}
DIRECTORY = 'upload-initial'
MODEL = b'v 0 0 0\nv 1 0 0\nv 0 1 0\nf 1 2 3\n'
REQUIRED_CHECKS = {'created_read_and_metadata_only', 'same_tenant_outsider_denied',
                   'existing_and_replaced_never_gain', 'revoked_replay_never_restores', 'logical_identity_never_regains',
                   'partial_failure_settles_without_extra_grant', 'physical_bytes_verified'}


def require(value, message):
    if not value:
        raise API.SuiteError(message)


def owned_directory(repository, environment):
    require(environment.get('POSTGRES_DB') == 'addp_online'
            and environment.get('ADDP_ONLINE_HOSTED') == '1'
            and environment.get('ADDP_ONLINE_MANAGER_CASE') == 'nfs-upload-initial'
            and not (repository / '.env').exists(), 'uploads require the exclusive Hosted case')
    root = Path((Path(environment['ADDP_ONLINE_SECRET_DIR']) / 'nfs-root').read_text().strip())
    require(re.fullmatch(r'/tmp/addp-online-manager-nfs\.[A-Za-z0-9]{6}', str(root))
            and not root.is_symlink()
            and (root / '.addp-online-owner').read_text().strip() == environment['ADDP_ONLINE_TEST_RUN_ID'],
            'NFS fixture ownership mismatch')
    target = root / DIRECTORY
    require(target.is_dir() and not target.is_symlink(), 'owned upload directory missing')
    return target


def multipart(engine, files):
    boundary = 'addp-upload-' + uuid.uuid4().hex
    chunks = [f'--{boundary}\r\nContent-Disposition: form-data; name="target_node_locator"\r\n\r\n'
              f'addp://engine/{engine}/path/{DIRECTORY}?type=directory\r\n'.encode()]
    for name, data in files:
        require(re.fullmatch(r'[a-z0-9_-]+\.(obj|csv)', name) and isinstance(data, bytes), 'invalid upload fixture')
        chunks += [f'--{boundary}\r\nContent-Disposition: form-data; name="files"; filename="{name}"\r\n'
                   'Content-Type: application/octet-stream\r\n\r\n'.encode(), data, b'\r\n']
    chunks.append(f'--{boundary}--\r\n'.encode())
    return b''.join(chunks), 'multipart/form-data; boundary=' + boundary


def runtime_client(environment, tenant):
    secret = environment.get('MANAGER_SERVICE_CLIENT_SECRET')
    require(bool(secret), 'Manager service credential missing')
    client = API.GatewayClient(environment['GATEWAY_URL'], '', 30)
    auth = 'Basic ' + base64.b64encode(('addp-manager:' + secret).encode()).decode()
    response = client.request('POST', '/api/v1/system/oauth/token', (200,), encoded_body=urlencode({
        'grant_type': 'client_credentials', 'scope': 'addp.api', 'audience': 'addp.api',
        'tenant_id': str(tenant)}).encode(),
        content_type='application/x-www-form-urlencoded', authorization_header=auth).payload
    require(isinstance(response.get('access_token'), str) and response['access_token'], 'Runtime token missing')
    runtime = API.GatewayClient(environment['GATEWAY_URL'], response['access_token'], 30)
    identity = runtime.request('GET', '/api/v1/system/auth/context', (200,)).payload
    require(identity.get('principal', {}).get('type') == 'service_principal'
            and identity.get('token', {}).get('type') == 'service_access_token'
            and identity.get('context', {}).get('type') == 'tenant'
            and identity.get('context', {}).get('tenant_id') == str(tenant)
            and identity.get('client', {}).get('client_id') == 'addp-manager', 'Runtime identity mismatch')
    return runtime


def wait_upload(client, upload_id, deadline):
    while time.monotonic() < deadline:
        state = client.request('GET', '/api/v1/manager/uploads/' + upload_id, (200,)).payload
        require(state.get('upload_id') == upload_id, 'upload status identity mismatch')
        if state.get('initial_status') in {'settled', 'denied'}:
            return state
        require(state.get('initial_status') in {'pending', 'pending_receipt'}, 'unknown upload continuation state')
        time.sleep(1)
    raise API.SuiteError('upload identification/Grant continuation budget exceeded')


def verify_outcome(outcomes, fingerprint, *, action=None, reason=None):
    matches = [row for row in outcomes if row.get('item_fingerprint') == fingerprint]
    require(len(matches) == 1, 'upload outcome missing or duplicated')
    row = matches[0]
    if action:
        require(row.get('outcome') == 'issued' and row.get('action') == action
                and str(uuid.UUID(row['request_id'])) == row['request_id'], 'initial Grant outcome mismatch')
    else:
        require(row.get('outcome') == 'denied' and row.get('reason') == reason
                and not row.get('request_id'), 'replacement unexpectedly gained access')
    return row


def run(repository, environment, owner):
    directory = owned_directory(repository, environment)
    engine = int(environment['ADDP_ONLINE_MANAGER_NFS_ENGINE_ID'])
    tenant = int(environment['ADDP_ONLINE_TEST_TENANT_ID'])
    client = API.GatewayClient(environment['GATEWAY_URL'], environment['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30)
    issuer = API.GatewayClient(environment['GATEWAY_URL'], environment['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'], 30)
    outsider = API.GatewayClient(environment['GATEWAY_URL'], environment['ADDP_ONLINE_FIXTURE_OUTSIDER_ACCESS_TOKEN'], 30)
    principal = FILES.exact_user_principal(client, tenant, PERMISSIONS)
    other = FILES.exact_user_principal(outsider, tenant, PERMISSIONS)
    require(principal != other and tenant > 1, 'isolated upload users required')
    runtime = runtime_client(environment, tenant)
    base = f'/api/v1/system/engines/{engine}/access_grants'
    report = dict(schema_version='addp.manager-upload-initial/v1', suite='manager-internal-artifact-lineage', case='nfs-upload-initial',
                  run_id=environment['ADDP_ONLINE_TEST_RUN_ID'], tenant_id=tenant, engine_id=engine,
                  principal_id=principal, outsider_principal_id=other, result='failed', cleanup='pending',
                  checks=[], uploads=[], grants=[])
    artifact = Path(environment['ADDP_ONLINE_ARTIFACT_DIR']) / 'manager-upload-initial.json'
    names = {'created.obj', 'metadata.csv', 'existing.obj', 'partial.obj', 'blocked.obj'}
    deadline = time.monotonic() + 180
    pending_uploads = set()
    uncertain_upload = False
    def fp(name):
        return hashlib.sha256(f'{engine}:{DIRECTORY}/{name}'.encode()).hexdigest()
    def history():
        result = issuer.request('GET', base + '/history?' + urlencode({
            'page_size': 100}), (200,)).payload
        rows = result.get('data')
        require(isinstance(rows, list) and result.get('total') == len(rows), 'upload history truncated')
        require(all(row.get('item_fingerprint') in {fp(name) for name in names}
                    and row.get('approval_mode') == 'upload_creation'
                    and row.get('recipient_type') == 'user' and str(row.get('recipient_id')) == str(principal)
                    and row.get('expiry_mode') == 'until_revoked' and row.get('expires_at') is None
                    for row in rows), 'unowned Grant in disposable upload engine')
        return rows
    def revoke(row):
        issuer.request('POST', base + '/' + row['request_id'] + '/revoke', (200,),
                       {'reason': 'Disposable upload initial authorization withdrawal'})
    def upload(files):
        nonlocal uncertain_upload
        # Physical target ownership is registered before the POST, even if the response is lost.
        require(all(name in names for name, _ in files), 'unowned upload file')
        raw, content_type = multipart(engine, files)
        uncertain_upload = True
        value = client.request('POST', '/api/v1/manager/uploads', (201,),
                               encoded_body=raw, content_type=content_type).payload
        upload_id = str(uuid.UUID(value['upload_id']))
        report['uploads'].append({'upload_id': upload_id, 'names': [n for n, _ in files]})
        pending_uploads.add(upload_id)
        uncertain_upload = False
        require(upload_id == value['upload_id'], 'noncanonical upload identity')
        state = wait_upload(client, upload_id, deadline)
        pending_uploads.remove(upload_id)
        return value, state
    def metadata(actor, name, status):
        return actor.request('GET', '/api/v1/meta/items/by-catalog-path?' + urlencode({
            'engine_id': engine, 'catalog_path': DIRECTORY + '/' + name}), (status,)).payload
    def content(actor, name, status, expected=None):
        locator = f'addp://engine/{engine}/path/{DIRECTORY}/{name}?type=file'
        result = actor.request('GET', '/api/v1/manager/downloads/file?' + urlencode({'locator': locator}),
                               (status,), raw_response=True)
        if status == 200:
            require(result.payload == expected, 'download bytes differ from this uploaded response')
        else:
            require(isinstance(result.payload, dict) and set(result.payload) <= {'error', 'error_code'},
                    'denied download leaked content')
        return result.status
    try:
        require(history() == [], 'upload fixture starts with Grant history')
        API.wait_manager_read_boundary(client, engine, deadline)
        metadata(client, 'existing.obj', 404)
        value, state = upload([('created.obj', MODEL), ('metadata.csv', b'id,name\n1,A\n')])
        require(state.get('status') == 'success' and not value.get('failed_files'), 'new upload did not physically succeed')
        issued = verify_outcome(state['outcomes'], fp('created.obj'), action='read')
        verify_outcome(state['outcomes'], fp('metadata.csv'), action='metadata_read')
        for name in ('created.obj', 'metadata.csv'):
            require(metadata(client, name, 200).get('fingerprint') == fp(name), 'Meta identity mismatch')
        content(client, 'created.obj', 200, MODEL)
        content(client, 'metadata.csv', 403)
        report['checks'].append('created_read_and_metadata_only')
        outsider.request('GET', '/api/v1/manager/uploads/' + value['upload_id'], (404,))
        metadata(outsider, 'created.obj', 404)
        content(outsider, 'created.obj', 403)
        report['checks'].append('same_tenant_outsider_denied')
        initial_history = history()
        require(len(initial_history) == 2, 'new upload did not create exactly two canonical Grants')
        for grant in initial_history:
            require(grant.get('upload_id') == value['upload_id']
                    and re.fullmatch('[0-9a-f]{64}', grant.get('boundary_hash', '')),
                    'initial Grant is not bound to this upload and a proven Meta boundary')
        _, replaced = upload([('existing.obj', MODEL + b'# replacement\n')])
        verify_outcome(replaced['outcomes'], fp('existing.obj'), reason='not_new')
        metadata(client, 'existing.obj', 404)
        content(client, 'existing.obj', 403)
        require(history() == initial_history, 'existing physical target acquired a Grant')
        row, = [row for row in history() if row['request_id'] == issued['request_id']]
        revoke(row)
        withdrawn_history = history()
        content(client, 'created.obj', 403)
        replay = runtime.request('POST', '/api/v1/system/runtime/initial-upload-grants/' + value['upload_id']
                                 + '/items/' + fp('created.obj'), (200,)).payload
        require(replay == issued and history() == withdrawn_history, 'replayed upload restored or replaced Grant history')
        metadata(client, 'created.obj', 404)
        content(client, 'created.obj', 403)
        report['checks'].append('revoked_replay_never_restores')
        _, overwritten = upload([('created.obj', MODEL + b'# overwritten\n')])
        verify_outcome(overwritten['outcomes'], fp('created.obj'), reason='not_new')
        require(history() == withdrawn_history, 'overwrite renewed a withdrawn Grant')
        content(client, 'created.obj', 403)
        report['checks'].append('existing_and_replaced_never_gain')
        # A missing physical file is not a new logical DataItem: the Meta identity already exists.
        (directory / 'created.obj').unlink()
        _, recreated = upload([('created.obj', MODEL + b'# recreated\n')])
        verify_outcome(recreated['outcomes'], fp('created.obj'), reason='not_new')
        require(history() == withdrawn_history, 'recreated physical file bypassed existing logical identity')
        content(client, 'created.obj', 403)
        report['checks'].append('logical_identity_never_regains')
        partial_value, partial = upload([('partial.obj', MODEL), ('blocked.obj', MODEL)])
        require(partial_value.get('failed_files') == ['blocked.obj'] and partial.get('status') == 'failed',
                'partial upload did not preserve the physical failure')
        verify_outcome(partial['outcomes'], fp('partial.obj'), action='read')
        require(len(partial['outcomes']) == 1 and not any(r['item_fingerprint'] == fp('blocked.obj') for r in history()),
                'failed file obtained an outcome or Grant')
        content(client, 'partial.obj', 200, MODEL)
        report['checks'].append('partial_failure_settles_without_extra_grant')
        expected = {'created.obj': MODEL + b'# recreated\n', 'metadata.csv': b'id,name\n1,A\n',
                    'existing.obj': MODEL + b'# replacement\n', 'partial.obj': MODEL}
        require(all((directory / name).read_bytes() == data for name, data in expected.items())
                and (directory / 'blocked.obj').is_dir(), 'NFS RPC writes or failed target differ')
        report['physical'] = {name: {'size': len(data), 'sha256': hashlib.sha256(data).hexdigest()}
                              for name, data in expected.items()}
        report['checks'].append('physical_bytes_verified')
        require(set(report['checks']) == REQUIRED_CHECKS, 'upload checks incomplete')
        report['result'] = 'passed'
    except Exception as error:
        report['failure'] = {'type': type(error).__name__, 'message_sha256': hashlib.sha256(str(error).encode()).hexdigest()}
        raise
    finally:
        try:
            rows = history()
            report['grants'] = [{k: row.get(k) for k in ('request_id', 'item_fingerprint', 'action', 'approval_mode')} for row in rows]
            for row in rows:
                if row.get('revocation') is None:
                    revoke(row)
            active = issuer.request('GET', base, (200,)).payload
            require(active.get('total') == 0 and active.get('data') == [], 'initial upload Grant cleanup has residuals')
            for name in names - {'blocked.obj'}:
                path = directory / name
                require(not path.is_symlink(), 'unexpected fixture symlink')
                path.unlink(missing_ok=True)
            (directory / 'blocked.obj').rmdir()
            require(list(directory.iterdir()) == [], 'upload file cleanup has residuals')
            directory.rmdir()
            # An unknown/pending receipt can still be handled by the background worker.
            # Immediate zero active Grants is not proof of no late issuance; outer Hosted teardown remains required.
            require(not uncertain_upload and not pending_uploads, 'upload outcome unsettled; application zero-residual proof unavailable')
            report['cleanup'] = 'zero_current_source_rules_and_files'
        except Exception as cleanup_error:
            previously_passed = report['result'] == 'passed'
            report['result'] = 'failed'
            report['cleanup'] = 'failed'
            report['cleanup_failure'] = {'type': type(cleanup_error).__name__, 'message_sha256': hashlib.sha256(str(cleanup_error).encode()).hexdigest()}
            if previously_passed:
                raise
        finally:
            artifact.write_text(json.dumps(report, ensure_ascii=False, sort_keys=True, indent=2) + '\n')
    return report
