#!/usr/bin/env python3
"""Ordinary User FTP catalog, scan, native previews and authorized model GLB."""
from importlib import import_module
from contextlib import contextmanager
import base64
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
import tempfile
import uuid
from urllib.parse import quote, urlencode

API = import_module('scripts.utils.online-api')
SUPPORT = import_module('scripts.test.security-transfer-protection-online')
SEED = import_module('business.ftp.seed')
MODEL = import_module('scripts.test.manager-internal-artifact-lineage-online')
REGISTRATION = import_module('scripts.test.online-engine-registration')
SuiteError = API.SuiteError
PERMISSIONS = {'system.engine_catalog.read', 'meta.catalog.read', 'meta.scan_task.execute',
               'meta.scan_task.read', 'manager.data_item.read', 'manager.content.read',
               'manager.derived_artifact.create', 'manager.derived_artifact.read',
               'manager.derived_artifact.delete', 'manager.data_item.update'}
ISSUER_PERMISSIONS = REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS | {
    'system.engine_access_grant.revoke', 'system.engine_access_deny.create', 'system.engine_access_deny.release'}


def require(ok, message):
    if not ok:
        raise SuiteError(message)


IFC_PRODUCT_PROFILE_PROBE = r'''
import base64, hashlib, http.server, json, os, platform, stat, struct
import subprocess, sys, tempfile, threading, time
from pathlib import Path
import operators

assert platform.system() == 'Linux' and platform.machine() == 'x86_64'
assert Path.cwd() == Path('/app')
binary = Path('/opt/addp/model3d-workflow/bin/IfcConvert')
assert os.environ.get('MODEL3D_IFC_CONVERTER_BIN') == str(binary)
assert binary.is_file() and os.access(binary, os.X_OK)
with binary.open('rb') as source:
    binary_sha = hashlib.file_digest(source, 'sha256').hexdigest()
version_result = subprocess.run([str(binary), '--version'], capture_output=True,
                                text=True, check=True, timeout=10)
version = (version_result.stdout + version_result.stderr).strip()
assert len(version) < 2048 and '0.8.4-e8eb5e4' in version
payload = sys.stdin.buffer.read(16385)
assert len(payload) <= 16384
payload = json.loads(payload)
assert set(payload) == {'ifc_b64', 'png_b64'}
original = base64.b64decode(payload['ifc_b64'], validate=True)
png = base64.b64decode(payload['png_b64'], validate=True)
assert len(original) == 908
assert hashlib.sha256(original).hexdigest() == '9bc22669f1fed05c7a10af1e9f76469f37dc4d204a048df04844d1c096ef7433'
assert len(png) == 73
assert hashlib.sha256(png).hexdigest() == 'ce3936f332de7b019594fd8792f3960cb10f2c665284db0fb08f798d2973875d'
marker = 'ENDSEC;\nEND-ISO-10303-21;'
assert original.decode('ascii').count(marker) == 1
connections, methods = [], []

class Server(http.server.ThreadingHTTPServer):
    def get_request(self):
        connection, address = super().get_request()
        connection.settimeout(2)
        connections.append(True)
        return connection, address

class Handler(http.server.BaseHTTPRequestHandler):
    def parse_request(self):
        valid = super().parse_request()
        if valid:
            methods.append(self.command)
        return valid
    def do_GET(self):
        self.send_response(200)
        self.send_header('Content-Type', 'image/png')
        self.send_header('Content-Length', str(len(png)))
        self.end_headers()
        self.wfile.write(png)
    def do_HEAD(self):
        self.send_response(200)
        self.send_header('Content-Type', 'image/png')
        self.send_header('Content-Length', str(len(png)))
        self.end_headers()
    def log_message(self, *args):
        pass

server = Server(('127.0.0.1', 0), Handler)
thread = threading.Thread(target=server.serve_forever, daemon=True)
cases = []
try:
    thread.start()
    with tempfile.TemporaryDirectory(prefix='addp-ifc-profile-') as temporary:
        temporary_path = Path(temporary)
        for reference_kind in ('http', 'fifo'):
            if reference_kind == 'http':
                reference = f'http://127.0.0.1:{server.server_port}/not-authorized.png'
            else:
                fifo = temporary_path / 'not-authorized.png'
                os.mkfifo(fifo)
                assert stat.S_ISFIFO(fifo.stat().st_mode)
                reference = str(fifo)
            # Attach the external texture style to the actual solid #12, not
            # to an unused IFC entity. The geometry remains the pinned box.
            resources = (f"#17=IFCIMAGETEXTURE(.T.,.T.,$,$,$,'{reference}');\n"
                         "#18=IFCSURFACESTYLEWITHTEXTURES((#17));\n"
                         "#19=IFCSURFACESTYLE('External texture',.BOTH.,(#18));\n"
                         "#20=IFCSTYLEDITEM(#12,(#19),$);\n")
            source = temporary_path / f'{reference_kind}.ifc'
            source.write_text(original.decode('ascii').replace(marker, resources + marker), encoding='ascii')
            source_sha = hashlib.sha256(source.read_bytes()).hexdigest()
            target = temporary_path / f'{reference_kind}.glb'
            plan = {'schema_version': 'addp.workflow.access-plan/v1',
                    'source': {'kind': 'file', 'format': 'ifc',
                               'access': {'method': 'mounted_path', 'path': str(source)}},
                    'target': {'kind': 'file', 'format': 'glb', 'name': target.name,
                               'write_mode': 'create',
                               'access': {'method': 'mounted_path', 'path': str(target)}}}
            started = time.monotonic()
            # No runner, converter env or options override: use the product
            # binding and the real operator's default --center-model profile.
            result = operators.invoke_operator('ifc_to_glb', {'access_plan': plan}, timeout_seconds=10)
            elapsed = time.monotonic() - started
            assert result['converter'] == str(binary) and result['source_format'] == 'ifc'
            assert '--center-model' in result['command']
            raw = target.read_bytes()
            assert 0 < len(raw) <= 2 * 1024 * 1024
            length, kind = struct.unpack_from('<I4s', raw, 12)
            assert kind == b'JSON'
            document = json.loads(raw[20:20 + length].decode('utf-8').rstrip(' \0'))
            assert document.get('meshes') and document.get('materials')
            assert not document.get('images') and not document.get('textures')
            assert not connections and not methods
            assert hashlib.sha256(source.read_bytes()).hexdigest() == source_sha
            cases.append({'reference_kind': reference_kind, 'duration_seconds': elapsed,
                          'source_sha256': source_sha, 'center_model': True,
                          'http_connections': len(connections), 'http_methods': list(methods),
                          'images': 0, 'textures': 0, 'size': len(raw),
                          'sha256': hashlib.sha256(raw).hexdigest(),
                          'glb_b64': base64.b64encode(raw).decode('ascii')})
    assert not temporary_path.exists()
finally:
    if thread.is_alive():
        server.shutdown()
    server.server_close()
    if thread.ident is not None:
        thread.join(timeout=2)
    assert not thread.is_alive()
print(json.dumps({'platform': 'Linux-x86_64', 'binary': str(binary),
                  'binary_version': version, 'binary_sha256': binary_sha,
                  'operator_sha256': hashlib.sha256(Path(operators.__file__).read_bytes()).hexdigest(),
                  'temporary_root_removed': True, 'sentinel_thread_stopped': True, 'cases': cases}))
'''


def verify_ifc_product_profile(root: Path, artifact_dir: Path) -> dict:
    """Called only inside the admitted, already-started FTP Hosted suite."""
    require(os.environ.get('ADDP_ONLINE_TEST') == '1' and os.environ.get('ADDP_ONLINE_HOSTED') == '1',
            'IFC product profile requires the existing FTP Hosted admission')
    container = 'addp-ftp-model3d-runtime'
    image = '127.0.0.1:5001/addp-model3d-workflow-engine:online'

    def docker_value(command):
        return subprocess.check_output(['docker', *command], text=True, timeout=10).strip()

    label = docker_value(['inspect', '-f', '{{index .Config.Labels "com.addp.online-runtime"}}', container])
    run_id = docker_value(['inspect', '-f', '{{index .Config.Labels "com.addp.online-run-id"}}', container])
    image_id = docker_value(['inspect', '-f', '{{.Image}}', container])
    require(label == 'ftp-consumer-flow' and bool(run_id) and run_id == os.environ.get('ADDP_ONLINE_TEST_RUN_ID'),
            'IFC profile must use this exact suite-owned Runtime')
    require(image_id == docker_value(['image', 'inspect', '-f', '{{.Id}}', image]), 'IFC product image identity differs')
    require(docker_value(['image', 'inspect', '-f', '{{.Os}}/{{.Architecture}}', image]) == 'linux/amd64',
            'IFC profile requires the Hosted Linux amd64 product')
    default_entry = json.loads(docker_value(['inspect', '-f', '{{json .Config.Cmd}}', container]))
    require(default_entry == ['python', 'api_server.py'], 'IFC product default entry differs')
    require(docker_value(['inspect', '-f', '{{.State.Running}}', container]) == 'true', 'IFC Runtime is not running')
    fixture_dir = root / 'business/fixtures/manager'
    ifc = (fixture_dir / 'addp_online_model_fixture.ifc').read_bytes()
    png = (fixture_dir / 'addp_online_exchange_texture.png').read_bytes()
    payload = json.dumps({'ifc_b64': base64.b64encode(ifc).decode('ascii'),
                          'png_b64': base64.b64encode(png).decode('ascii')})
    try:
        completed = subprocess.run(['docker', 'exec', '-i', '--workdir', '/app', container,
                                    'python', '-c', IFC_PRODUCT_PROFILE_PROBE],
                                   input=payload, text=True, capture_output=True, timeout=60)
    except subprocess.TimeoutExpired as error:
        raise SuiteError('IFC product profile exceeded its outer 60-second budget') from error
    if completed.returncode != 0:
        # This probe only contains synthetic mounted inputs and never prints
        # Runtime environment/credentials. Keep its bounded diagnostic, too.
        (artifact_dir / 'ifc-external-profile-error.log').write_text(completed.stderr[-16384:])
    require(completed.returncode == 0, 'IFC fixed Linux profile failed; do not downgrade the native timeout or skip it')
    require(len(completed.stdout) <= 6 * 1024 * 1024, 'IFC synthetic profile report exceeds its bounded payload')
    report = json.loads(completed.stdout)
    require(report.get('platform') == 'Linux-x86_64' and
            report.get('binary') == '/opt/addp/model3d-workflow/bin/IfcConvert' and
            isinstance(report.get('binary_version'), str) and len(report['binary_version']) < 2048 and
            '0.8.4-e8eb5e4' in report['binary_version'] and
            isinstance(report.get('binary_sha256'), str) and re.fullmatch('[0-9a-f]{64}', report['binary_sha256']) and
            report.get('temporary_root_removed') is True and report.get('sentinel_thread_stopped') is True,
            'IFC native platform or temporary cleanup proof differs')
    require(report.get('operator_sha256') == hashlib.sha256((root / 'engines/model3d-workflow/operators.py').read_bytes()).hexdigest(),
            'IFC Runtime operator source differs from this checkout')
    require([case['reference_kind'] for case in report['cases']] == ['http', 'fifo'], 'IFC profile cases are incomplete')
    for case in report['cases']:
        require(case['center_model'] is True and case['http_connections'] == 0 and case['http_methods'] == [] and
                case['images'] == 0 and case['textures'] == 0, 'IFC fixed-profile external-resource assertion differs')
        raw = base64.b64decode(case.pop('glb_b64'), validate=True)
        require(len(raw) == case['size'] and hashlib.sha256(raw).hexdigest() == case['sha256'], 'IFC profile GLB bytes differ')
        try:
            case['physical_glb'] = MODEL.validate_model_glb(raw, 'ifc')
        except MODEL.SuiteError as error:
            raise SuiteError(f'IFC fixed-profile physical GLB differs: {error}') from error
        (artifact_dir / f"ifc-external-{case['reference_kind']}.glb").write_bytes(raw)
    report.update(image_id=image_id, default_entry=default_entry, run_id=run_id,
                  git_commit=subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip(),
                  fixture_sha256=hashlib.sha256(ifc).hexdigest(),
                  converter_archive_sha256='891d1de019753220e9eff3c82e6262b4e20810974d210469e7cc7abaf3fca32c',
                  scope='Fixed geometry/basic-color IFC product profile only; not all IFC external references or FTP authorization')
    (artifact_dir / 'ifc-external-profile.json').write_text(json.dumps(report, indent=2) + '\n')
    return report


def validate_preview(payload):
    data = payload.get('data', {})
    require(payload.get('preview_type') == 'table' and set(data.get('columns', [])) == {'id', 'name', 'amount'},
            'FTP shared table preview must expose the three native columns')
    rows = data.get('rows', [])
    require(isinstance(rows, list) and len(rows) == 3 and data.get('total') == 3,
            'FTP shared preview must contain all three example rows')
    require(all(str(row.get('id')) in {'1', '2', '3'} and not isinstance(row.get('id'), bool) for row in rows), 'FTP sample IDs must remain exact integers')
    try:
        actual = sorted([{'id': int(row['id']), 'name': row['name'], 'amount': float(row['amount'])} for row in rows], key=lambda row: row['id'])
    except (KeyError, TypeError, ValueError):
        raise SuiteError('FTP sample values are malformed') from None
    require(actual == SEED.ROWS, 'FTP preview changed native IDs, Chinese text or amounts')


def validate_item(item, sample):
    require(item.get('item_type') == 'file' and item.get('full_name') == sample['full_name'] and item.get('fingerprint'),
            'FTP Meta item must preserve its exact account-relative file identity')
    attributes = item.get('attributes', {})
    require(attributes.get('schema_version') == 1 and attributes.get('item', {}).get('data_type') == 'table',
            'FTP tabular file must reuse the existing table data type')
    fields = attributes.get('type_info', {}).get('table', {}).get('fields', [])
    require({field.get('name') for field in fields} == {'id', 'name', 'amount'},
            'FTP Meta scan omitted the native file schema')


def file_locator(engine_id, item):
    path = '/'.join(quote(part, safe='') for part in item['full_name'].split('/'))
    return f'addp://engine/{engine_id}/path/{path}?' + urlencode({'type': 'file', 'item_id': item['id']})


def file_target(engine_id, path):
    parts = path.split('/')
    require(type(engine_id) is int and engine_id > 0 and parts and
            all(part and part not in {'.', '..'} and not any(char in part for char in '\\:') for part in parts),
            'FTP approval requires an exact account-relative leaf')
    segments = [{'term': 'root', 'kind': 'root', 'name': ''}]
    segments.extend({'term': 'directory', 'kind': 'directory', 'name': part} for part in parts[:-1])
    segments.append({'term': 'file', 'kind': 'file', 'name': parts[-1]})
    return {'version': 'catalog.path/v1', 'engine_id': engine_id, 'segments': segments}


def exact_user_principal(client, tenant, permissions):
    API.validate_user_identity(client, tenant, permissions)
    current = client.request('GET', '/api/v1/system/auth/context', (200,)).payload
    principal = current.get('principal', {})
    assignments = current.get('authorization', {}).get('role_assignments', [])
    actual_permissions = {key for role in assignments for key in role.get('permissions', [])}
    require(principal.get('type') == 'user' and current.get('context', {}).get('type') == 'tenant' and
            current.get('context', {}).get('tenant_id') == str(tenant) and
            isinstance(principal.get('id'), str) and principal['id'].isascii() and principal['id'].isdecimal() and
            str(int(principal['id'])) == principal['id'] and int(principal['id']) > 0 and
            actual_permissions == permissions and
            not any(role.get('role_key') in API.FORBIDDEN_ADMIN_ROLES for role in assignments),
            'FTP identity must remain the exact ordinary User with only its declared permissions')
    return principal['id']


class ModelSourceAuthorization:
    """Use the formal User APIs; receipts are history, never a cached Allow."""

    def __init__(self, issuer, consumer, report):
        tenant = report['tenant_id']
        require(type(tenant) is int and tenant > 1, 'FTP grants require the disposable nondefault Tenant')
        self.operator = exact_user_principal(issuer, tenant, ISSUER_PERMISSIONS)
        self.recipient = exact_user_principal(consumer, tenant, PERMISSIONS)
        require(self.recipient == report['principal_id'] and self.operator != self.recipient,
                'FTP source issuer must be distinct from the actual browser consumer')
        self.issuer = issuer
        self.engine_id = report['engine_id']
        self.base = f'/api/v1/system/engines/{self.engine_id}'
        self.grants = []
        self.confirmed = set()
        self.denies = []
        self.confirmed_denies = set()

    def facts(self, model):
        facts = self.issuer.request('GET', self.base + f"/data-items/{model['fingerprint']}/authorization-facts", (200,)).payload
        expected_refs = [{'target': file_target(self.engine_id, ref['path']), 'role': ref['role'],
                          'required': ref['required'], 'primary': ref['primary']} for ref in model['composition']]
        boundary = {'layout': 'single', 'file_composition': {'format': model['format'], 'refs': expected_refs}}
        require(isinstance(facts, dict) and all(facts.get(key) == value for key, value in {
            'schema_version': 'meta.data_item_authorization_facts/v1', 'engine_id': str(self.engine_id),
            'item_fingerprint': model['fingerprint'], 'full_name': model['full_name'],
            'item_type': 'file', 'basis_state': 'complete', 'boundary': boundary}.items()) and
            isinstance(facts.get('boundary_hash'), str) and re.fullmatch('[0-9a-f]{64}', facts['boundary_hash']) and
            bool(facts.get('observed_at')),
            'FTP approval requires matching complete System facts, never a directory or caller-supplied composition')
        return facts

    def grant(self, model, *, initialize=True):
        facts = self.facts(model)
        command = str(uuid.uuid4())
        body = {'request_id': command, 'catalog_path': file_target(self.engine_id, model['full_name']),
                'item_fingerprint': model['fingerprint'], 'expected_boundary_hash': facts['boundary_hash'],
                'requirement_version': '1', 'initialize_approval': initialize, 'recipient_type': 'user',
                'recipient_id': self.recipient, 'action': 'read', 'expiry_mode': 'until_revoked',
                'reason': 'Disposable FTP exact model composition approval'}
        # Retain our command before the write, including an unknown outcome.
        self.grants.append(command)
        receipt = self.issuer.request('POST', self.base + '/access_grants', (201,), body).payload
        expected = {key: body[key] for key in ('request_id', 'catalog_path', 'item_fingerprint',
                                             'requirement_version', 'recipient_type', 'recipient_id', 'action', 'expiry_mode')}
        expected.update(engine_id=str(self.engine_id), approval_mode='independent', expires_at=None,
                        boundary_hash=facts['boundary_hash'], file_composition=facts['boundary']['file_composition'],
                        operator_principal_id=self.operator, revocation=None)
        require(isinstance(receipt, dict) and all(receipt.get(key) == value for key, value in expected.items()),
                'FTP Grant receipt changed its issuer, recipient or complete frozen composition')
        self.confirmed.add(command)
        return {'request_id': command, 'item_fingerprint': model['fingerprint'], 'boundary_hash': facts['boundary_hash']}

    def revoke(self, command, *, cleanup=False):
        require(command in self.grants, 'FTP cleanup must not revoke an unowned Grant')
        statuses = (200, 404) if cleanup and command not in self.confirmed else (200,)
        self.issuer.request('POST', self.base + f'/access_grants/{command}/revoke', statuses,
                            {'reason': 'Disposable FTP model source withdrawal'})
        self.grants.remove(command)
        self.confirmed.discard(command)

    def cleanup(self):
        errors = []
        for command in list(self.denies):
            try:
                self.release(command, cleanup=True)
            except Exception:
                errors.append(command)
        for command in list(self.grants):
            try:
                self.revoke(command, cleanup=True)
            except Exception:
                errors.append(command)
        require(not errors, 'FTP owned source rule cleanup failed')

    def deny(self, model):
        command = str(uuid.uuid4())
        body = {'deny_id': command, 'catalog_path': file_target(self.engine_id, model['full_name']),
                'item_fingerprint': model['fingerprint'], 'recipient_type': 'user', 'recipient_id': self.recipient,
                'action': 'read', 'expiry_mode': 'until_revoked', 'reason': 'Disposable FTP stable DataItem Deny'}
        self.denies.append(command)
        receipt = self.issuer.request('POST', self.base + '/access_denies', (200,), body).payload
        expected = {key: body[key] for key in ('deny_id', 'catalog_path', 'item_fingerprint', 'recipient_type',
                                             'recipient_id', 'action', 'expiry_mode')}
        expected.update(engine_id=str(self.engine_id), established_by_principal_id=self.operator)
        require(isinstance(receipt, dict) and all(receipt.get(key) == value for key, value in expected.items()),
                'FTP Deny receipt changed its stable DataItem, subject or actual issuer')
        self.confirmed_denies.add(command)
        return command

    def release(self, command, *, cleanup=False):
        require(command in self.denies, 'FTP cleanup must not release an unowned Deny')
        statuses = (200, 404) if cleanup and command not in self.confirmed_denies else (200,)
        response = self.issuer.request('POST', self.base + f'/access_denies/{command}/release', statuses,
                                       {'reason': 'Disposable FTP model Deny release'})
        require(response.status != 200 or response.payload.get('deny_id') == command,
                'FTP Deny release receipt changed its original command')
        self.denies.remove(command)
        self.confirmed_denies.discard(command)


def verify_cached_results(client, report, statuses):
    principal = exact_user_principal(client, report['tenant_id'], PERMISSIONS)
    require(principal == report['principal_id'], 'FTP withdrawal must use the same current User')
    results = []
    for model in report['browser']['models']:
        result_id = model['result_id']
        require(type(result_id) is int and result_id > 0, 'FTP withdrawal requires a real published result')
        status = statuses[model['format']]
        require(status in {200, 403}, 'FTP cache proof requires an explicit allow or deny status')
        payload = client.request('GET', f'/api/v1/manager/model_3d_glb/{result_id}/content', (status,), raw_response=status == 200).payload
        if status == 403:
            require(isinstance(payload, dict) and bool(payload.get('error')),
                    'FTP withdrawal must reject cached content with an explicit source denial')
        else:
            require(isinstance(payload, bytes) and len(payload) == model['size'] and
                    hashlib.sha256(payload).hexdigest() == model['sha256'],
                    'FTP source rule control must preserve the exact already-verified GLB')
        results.append({'format': model['format'], 'result_id': result_id, 'status': status})
    return results


@contextmanager
def changed_texture_fixture(model):
    """Change only the fixed model in this Hosted run's owned, read-only mount."""
    root = Path(SUPPORT.required_environment('ADDP_FTP_ADMISSION_DIR'))
    require(not root.is_symlink() and root.resolve() == Path(SUPPORT.required_environment('ADDP_ONLINE_SECRET_DIR')).resolve()
            and root.is_dir() and not root.stat().st_mode & 0o077,
            'FTP composition mutation requires this Hosted run private fixture')
    state = json.loads((root / 'state.json').read_text())
    name = state.get('name', '')
    require(re.fullmatch(r'addp-ftp-[0-9a-f]{12}', name) and state.get('label') == 'org.addp.ftp.owner=' + name,
            'FTP composition mutation requires the exact admission owner')
    inspected = subprocess.run(['docker', 'inspect', name], check=True, capture_output=True, text=True)
    container, = json.loads(inspected.stdout)
    require(container.get('State', {}).get('Running') is True and
            container.get('Config', {}).get('Labels', {}).get('org.addp.ftp.owner') == name and
            any(mount.get('Source') == str((root / 'data').resolve()) and
                mount.get('Destination') == '/srv/sftpgo/data' and mount.get('RW') is False
                for mount in container.get('Mounts', [])),
            'FTP composition mutation requires the exact running read-only data mount')
    paths = [root / relative for relative in ('data', 'data/models', 'data/models/3ds',
             'data/models/3ds/model.3ds', 'data/models/3ds/texture.png')]
    require(all(not path.is_symlink() for path in paths), 'FTP composition fixture must not traverse a link')
    main, texture = paths[-2:]
    original, png = main.read_bytes(), texture.read_bytes()
    fixture = Path(__file__).resolve().parents[2] / 'business/fixtures/manager'
    require(model['full_name'] == 'models/3ds/model.3ds' and model['format'] == '3ds' and
            original == (fixture / 'addp_online_model_fixture.3ds').read_bytes() and
            png == (fixture / 'addp_online_exchange_texture.png').read_bytes() and
            hashlib.sha256(original).hexdigest() == model['sha256'] and original.count(b'texture.png') == 1,
            'FTP composition mutation only accepts the fixed physical model and texture')
    changed = original.replace(b'texture.png', b'newname.png')
    replacement = texture.with_name('newname.png')
    created = None
    published = False
    try:
        descriptor = os.open(replacement, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o644)
        identity = os.fstat(descriptor)
        created = (identity.st_dev, identity.st_ino)
        with os.fdopen(descriptor, 'wb') as stream:
            stream.write(png)
            os.fchmod(stream.fileno(), 0o644)
        published = True
        replace_fixture_file(main, changed)
        projected = copy.deepcopy(model)
        projected.update(sha256=hashlib.sha256(changed).hexdigest(), size=len(changed))
        for ref in projected['composition']:
            if ref['role'] == 'texture':
                ref['path'] = 'models/3ds/newname.png'
        yield projected
    finally:
        try:
            require(not main.is_symlink() and main.read_bytes() in (original, changed),
                    'FTP composition cleanup refuses to overwrite unexpected main-file changes')
            if main.read_bytes() == changed:
                replace_fixture_file(main, original)
        finally:
            if created is not None:
                identity = replacement.lstat()
                require(not replacement.is_symlink() and replacement.is_file() and
                        (identity.st_dev, identity.st_ino) == created,
                        'FTP composition cleanup refuses to remove a replaced texture')
                content = replacement.read_bytes()
                require(content == png if published else png.startswith(content),
                        'FTP composition cleanup refuses to remove unexpected texture changes')
                replacement.unlink()


def replace_fixture_file(path, content):
    # A failed write or chmod must leave the original main file intact.
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(dir=path.parent, prefix='.addp-model-', delete=False) as stream:
            temporary = Path(stream.name)
            stream.write(content)
            os.fchmod(stream.fileno(), 0o644)
        os.replace(temporary, path)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def source_read_control(client, engine_id, model, status):
    response = client.request('POST', '/api/v1/system/engine-access/read-checks/manager-preview', (status,),
                              {'targets': [file_target(engine_id, model['full_name'])]}).payload
    require(isinstance(response, dict) and bool(response.get('observed_at') if status == 200 else response.get('error')),
            'FTP current source rule check omitted its explicit observation or denial')
    return status


def verify_old_grant_composition(client, authorization, report, timeout):
    source = next(model for model in report['models'] if model['format'] == '3ds')
    original_hash = authorization.facts(source)['boundary_hash']
    try:
        with changed_texture_fixture(source) as changed:
            scan = SUPPORT.wait_for_scan(client, report['engine_id'], time.monotonic() + timeout)
            facts = authorization.facts(changed)
            require(facts['boundary_hash'] != original_hash, 'FTP composition boundary did not change')
            denied = source_read_control(client, report['engine_id'], changed, 403)
            stale = verify_cached_results(client, report, {'ifc': 200, '3ds': 403})
            return {'scan_execution_id': scan, 'original_boundary_hash': original_hash,
                    'changed_boundary_hash': facts['boundary_hash'],
                    'old_grant_without_deny': denied, 'old_cached_content': stale}
    finally:
        SUPPORT.wait_for_scan(client, report['engine_id'], time.monotonic() + timeout)


def verify_composition_rules(client, authorization, report, deny, timeout):
    source = next(model for model in report['models'] if model['format'] == '3ds')
    prior, = [grant for grant in report['source_authorizations'] if grant['item_fingerprint'] == source['fingerprint']]
    original_hash = authorization.facts(source)['boundary_hash']
    try:
        with changed_texture_fixture(source) as changed:
            scan = SUPPORT.wait_for_scan(client, report['engine_id'], time.monotonic() + timeout)
            facts = authorization.facts(changed)
            require(facts['boundary_hash'] != original_hash,
                    'FTP changed texture reference must change the complete approval boundary')
            # The production relation admits one active Grant per item/subject.
            authorization.revoke(prior['request_id'])
            new_grant = authorization.grant(changed, initialize=False)
            denied = source_read_control(client, report['engine_id'], changed, 403)
            authorization.release(deny)
            allowed = source_read_control(client, report['engine_id'], changed, 200)
            stale = verify_cached_results(client, report, {'ifc': 200, '3ds': 403})
            result = {'scan_execution_id': scan, 'original_boundary_hash': original_hash,
                      'changed_boundary_hash': facts['boundary_hash'], 'changed_grant': new_grant,
                      'old_deny_after_new_grant': denied, 'new_grant_after_release': allowed,
                      'old_cached_content': stale}
    finally:
        SUPPORT.wait_for_scan(client, report['engine_id'], time.monotonic() + timeout)
    authorization.revoke(new_grant['request_id'])
    result['restored_grant'] = authorization.grant(source, initialize=False)
    return result


def prepare_models(items, engine_id, samples):
    expected = {'ifc': ('models/ifc/model.ifc', 'addp_online_model_fixture.ifc'),
                '3ds': ('models/3ds/model.3ds', 'addp_online_model_fixture.3ds'),
                'png': ('models/3ds/texture.png', 'addp_online_exchange_texture.png')}
    require(isinstance(samples, list) and len(samples) == 3 and
            {sample.get('format') for sample in samples} == set(expected),
            'FTP model manifest requires exactly IFC, 3DS and its PNG')
    fixture = Path(__file__).resolve().parents[2] / 'business/fixtures/manager'
    by_format = {}
    for sample in samples:
        relative, source = expected[sample['format']]
        raw = (fixture / source).read_bytes()
        require(sample.get('full_name') == relative and type(sample.get('size')) is int and
                sample['size'] == len(raw) and sample.get('sha256') == hashlib.sha256(raw).hexdigest(),
                'FTP model manifest differs from the fixed physical fixture')
        by_format[sample['format']] = sample
    result = []
    for kind in ('ifc', '3ds'):
        sample = by_format[kind]
        matches = [item for item in items if item.get('full_name') == sample['full_name']]
        require(len(matches) == 1, 'FTP model scan must retain one exact primary DataItem')
        item = matches[0]
        attributes = item.get('attributes', {})
        facts = attributes.get('item', {})
        native = attributes.get('format_info', {}).get(kind, {})
        require(type(item.get('id')) is int and item['id'] > 0 and
                type(item.get('engine_id')) is int and item['engine_id'] == engine_id and
                item.get('fingerprint') == hashlib.sha256(f"{engine_id}:{sample['full_name']}".encode()).hexdigest() and
                item.get('item_type') == 'file' and attributes.get('schema_version') == 1 and
                all(facts.get(key) == value for key, value in
                    {'data_type': 'model_3d', 'layout': 'single', 'format': kind}.items()) and
                attributes.get('storage', {}).get('physical_path') == sample['full_name'] and
                native.get('scan_complete') is True,
                'FTP model identity, single layout or complete native facts are missing')
        refs = facts.get('refs')
        # IFC's fixed geometry profile has no external inputs; Meta may project
        # its sole primary from the committed single-file identity.
        if kind == 'ifc' and refs is None:
            refs = [{'path': sample['full_name'], 'role': 'model', 'required': True, 'primary': True}]
        expected_refs = [{'path': sample['full_name'], 'role': 'model', 'required': True, 'primary': True}]
        if kind == '3ds':
            require(native.get('texture_refs') == ['texture.png'],
                    'FTP 3DS must retain its exact texture declaration')
            expected_refs.append({'path': by_format['png']['full_name'], 'role': 'texture', 'required': True, 'primary': False})
        require(isinstance(refs, list) and len(refs) == len(expected_refs) and
                all(isinstance(ref, dict) and type(ref.get('required')) is bool and
                    type(ref.get('primary', False)) is bool for ref in refs) and
                sorted([{key: ref.get(key, False) for key in ('path', 'role', 'required', 'primary')}
                        for ref in refs], key=lambda ref: ref['path']) ==
                sorted(expected_refs, key=lambda ref: ref['path']),
                'FTP model composition must contain only its primary and confirmed dependencies')
        result.append({**sample, 'item_id': item['id'], 'fingerprint': item['fingerprint'],
                       'locator': file_locator(engine_id, item), 'composition': expected_refs})
    return result


def run(client, tenant_id, engine_id, samples, model_samples, timeout):
    require(tenant_id > 1, 'FTP Online requires a nondefault disposable Tenant')
    API.validate_user_identity(client, tenant_id, PERMISSIONS)
    context = client.request('GET', '/api/v1/system/auth/context', (200,)).payload
    selectors = client.request('GET', '/api/v1/system/engine-catalog/engines', (200,)).payload
    selected = [entry for entry in selectors if entry.get('id') == engine_id]
    require(len(selected) == 1 and selected[0].get('engine_type') == 'ftp' and 'connection_info' not in selected[0],
            'FTP must be in the ordinary User catalog without credentials')
    capabilities = selected[0].get('capabilities', {})
    store = capabilities.get('storage', {}).get('store', {})
    require(capabilities.get('engine_family') == 'file' and store.get('stream_read') is True and
            store.get('range_read') is True and not store.get('stream_write') and not store.get('delete'),
            'FTP must declare only the admitted read capabilities')
    endpoint = f'/api/v1/system/engines/{engine_id}/catalog/children'
    roots = client.request('POST', endpoint, (200,), {'path': {'version': 'catalog.path/v1', 'segments': []}}).payload['nodes']
    require(len(roots) == 1 and roots[0]['kind'] == 'root', 'FTP must expose one explicit account root')
    directories = client.request('POST', endpoint, (200,), {'path': roots[0]['path']}).payload['nodes']
    branches = [entry for entry in directories if entry['name'] == 'examples']
    require(len(branches) == 1 and branches[0]['kind'] == 'directory', 'FTP account root lost the examples directory')
    files = client.request('POST', endpoint, (200,), {'path': branches[0]['path']}).payload['nodes']
    for sample in samples:
        matches = [entry for entry in files if entry['name'] == sample['full_name'].split('/')[-1]]
        require(len(matches) == 1 and matches[0]['kind'] == 'file', 'FTP live catalog normalized or omitted a sample name')
        facts = client.request('POST', f'/api/v1/system/engines/{engine_id}/catalog/facts', (200,), {'path': matches[0]['path']}).payload
        storage = facts.get('storage', {})
        require(storage.get('path') == sample['full_name'] and storage.get('size_bytes') == sample['size'] and not storage.get('etag'),
                'FTP facts must preserve native path/size without inventing an ETag')
    execution_id = SUPPORT.wait_for_scan(client, engine_id, time.monotonic() + timeout)
    items = client.request('GET', f'/api/v1/meta/engines/{engine_id}/items', (200,)).payload
    result = []
    for sample in samples:
        matches = [item for item in items if item.get('full_name') == sample['full_name']]
        require(len(matches) == 1, 'FTP scan must create one exact DataItem for each sample')
        item = matches[0]
        validate_item(item, sample)
        locator = file_locator(engine_id, item)
        validate_preview(client.request('GET', '/api/v1/manager/preview?' + urlencode({'locator': locator}), (200,)).payload)
        binary = client.request('GET', '/api/v1/manager/downloads/file?' + urlencode({'locator': locator}), (200,), raw_response=True).payload
        require(len(binary) == sample['size'] and hashlib.sha256(binary).hexdigest() == sample['sha256'],
                'FTP API download differs from the physical seed file')
        result.append({**sample, 'item_id': item['id'], 'locator': locator})
    require({sample['format'] for sample in result} == {'csv', 'json', 'parquet'} and len(result) == 3,
            'FTP acceptance requires all three formats')
    return {'run_id': SUPPORT.required_environment('ADDP_ONLINE_TEST_RUN_ID'), 'engine_id': engine_id,
            'tenant_id': tenant_id, 'principal_id': str(context['principal']['id']),
            'scan_execution_id': execution_id, 'samples': result,
            'models': prepare_models(items, engine_id, model_samples)}


def validate_browser_report(actual, expected, artifact_dir):
    proof = {key: expected[key] for key in ('run_id', 'engine_id', 'tenant_id', 'principal_id')}
    proof.update(meta_ui_scan=True, previews=3, downloads=[{key: sample[key] for key in ('format', 'item_id', 'size', 'sha256')} for sample in expected['samples']])
    require({key: value for key, value in actual.items() if key != 'models'} == proof,
            'FTP browser identity, scan, preview or physical download proof differs')
    models = actual.get('models')
    require(isinstance(models, list) and len(models) == 2,
            'FTP browser must generate and reopen both model formats')
    for observed, source in zip(models, expected['models']):
        require(set(observed) == {'format', 'item_id', 'task_id', 'execution_id', 'result_id',
                                 'size', 'sha256', 'source_sha256', 'rendered', 'reopened',
                                 'cached_without_execution'} and
                observed.get('format') == source['format'] and observed.get('item_id') == source['item_id'] and
                all(type(observed.get(key)) is int and observed[key] > 0 for key in ('task_id', 'result_id', 'size')) and
                isinstance(observed.get('execution_id'), str) and bool(observed['execution_id']) and
                observed.get('source_sha256') == source['sha256'] and
                all(observed.get(key) is True for key in ('rendered', 'reopened', 'cached_without_execution')),
                'FTP browser model identity, source integrity, rendering or cache proof differs')
        raw = (artifact_dir / f"ftp-{source['format']}.glb").read_bytes()
        require(len(raw) == observed['size'] and hashlib.sha256(raw).hexdigest() == observed.get('sha256'),
                'FTP model report differs from actual downloaded GLB')
        try:
            MODEL.validate_model_glb(raw, source['format'])
        except MODEL.SuiteError as error:
            raise SuiteError(f"FTP {source['format']} GLB physical verification failed: {error}") from error


def validate_withdrawal_browser_report(actual, expected):
    models = []
    for source, published in zip(expected['models'], expected['browser']['models']):
        models.append({'format': source['format'], 'item_id': source['item_id'], 'result_id': published['result_id'],
                       'status': 403, 'error_visible': True, 'content_successes': 0, 'generation_requests': 0,
                       'unchanged_execution_id': published['execution_id']})
    require(actual == {**{key: expected[key] for key in ('run_id', 'engine_id', 'tenant_id', 'principal_id')},
                       'browser_phase': 'withdrawn', 'models': models},
            'FTP withdrawal browser must reject both original models without content or new execution')


def run_browser(report, *, withdrawn=False):
    output = Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR')) / ('ftp-console-withdrawn.json' if withdrawn else 'ftp-console.json')
    output.unlink(missing_ok=True)
    environment = {key: value for key, value in os.environ.items() if key not in {
        'ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'}}
    expected = {**report, 'browser_phase': 'withdrawn'} if withdrawn else report
    environment.update(ADDP_ONLINE_FTP_EXPECTATIONS=json.dumps(expected), ADDP_ONLINE_FTP_BROWSER_REPORT=str(output))
    result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/ftp-consumer-flow.spec.js', '--config=playwright.online.config.js'],
                            cwd=Path(__file__).resolve().parents[2] / 'console/frontend', env=environment)
    require(result.returncode == 0 and output.is_file(), 'Real Console FTP acceptance failed or omitted evidence')
    actual = json.loads(output.read_text())
    if withdrawn:
        validate_withdrawal_browser_report(actual, report)
    else:
        validate_browser_report(actual, report, output.parent)
    return actual


def main():
    require(os.environ.get('ADDP_ONLINE_TEST') == '1' and os.environ.get('ADDP_ONLINE_HOSTED') == '1', 'FTP suite requires the Hosted Online entry')
    tenant = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_TEST_TENANT_ID'), 'tenant')
    engine = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_FTP_ENGINE_ID'), 'engine')
    timeout = float(os.environ.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '900'))
    samples = json.loads((Path(SUPPORT.required_environment('ADDP_FTP_ADMISSION_DIR')) / 'ftp-samples.json').read_text())
    model_samples = json.loads((Path(SUPPORT.required_environment('ADDP_FTP_ADMISSION_DIR')) / 'ftp-model-samples.json').read_text())
    client = SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'), SUPPORT.required_environment('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'), min(timeout, 30))
    report = run(client, tenant, engine, samples, model_samples, timeout)
    report['ifc_product_profile'] = verify_ifc_product_profile(Path(__file__).resolve().parents[2],
        Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR')))
    issuer = SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'),
                                   SUPPORT.required_environment('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'), min(timeout, 30))
    authorization = ModelSourceAuthorization(issuer, client, report)
    try:
        report['source_authorizations'] = [authorization.grant(model) for model in report['models']]
        report['browser'] = run_browser(report)
        source = next(model for model in report['models'] if model['format'] == '3ds')
        report['old_grant_composition'] = verify_old_grant_composition(client, authorization, report, timeout)
        report['restored_before_deny'] = verify_cached_results(client, report, {'ifc': 200, '3ds': 200})
        deny = authorization.deny(source)
        report['source_deny_id'] = deny
        report['deny_cached_content'] = verify_cached_results(client, report, {'ifc': 200, '3ds': 403})
        report['composition_rules'] = verify_composition_rules(client, authorization, report, deny, timeout)
        report['released_cached_content'] = verify_cached_results(client, report, {'ifc': 200, '3ds': 200})
    finally:
        authorization.cleanup()
    report['revoked_cached_content'] = verify_cached_results(client, report, {'ifc': 403, '3ds': 403})
    report['withdrawn_browser'] = run_browser(report, withdrawn=True)
    Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'), 'ftp-consumer-flow.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except (SuiteError, SUPPORT.SuiteError, OSError, ValueError, KeyError) as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
