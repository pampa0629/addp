#!/usr/bin/env python3
"""Ordinary User FTP catalog, scan, native previews and authorized model GLB."""
from importlib import import_module
from contextlib import contextmanager
import base64
import copy
import hashlib
import io
import json
import math
import os
from pathlib import Path
import re
import subprocess
import struct
import sys
import time
import tempfile
import uuid
import zipfile
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


def model3d_product_identity():
    require(os.environ.get('ADDP_ONLINE_TEST') == '1' and os.environ.get('ADDP_ONLINE_HOSTED') == '1',
            'Product verification requires the existing FTP Hosted admission')
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
    return {'container': container, 'image_id': image_id, 'run_id': run_id, 'default_entry': default_entry}


def verify_ifc_product_profile(root: Path, artifact_dir: Path) -> dict:
    """Called only inside the admitted, already-started FTP Hosted suite."""
    identity = model3d_product_identity()
    container = identity['container']
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
    report.update(image_id=identity['image_id'], default_entry=identity['default_entry'], run_id=identity['run_id'],
                  git_commit=subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip(),
                  fixture_sha256=hashlib.sha256(ifc).hexdigest(),
                  converter_archive_sha256='891d1de019753220e9eff3c82e6262b4e20810974d210469e7cc7abaf3fca32c',
                  scope='Fixed geometry/basic-color IFC product profile only; not all IFC external references or FTP authorization')
    (artifact_dir / 'ifc-external-profile.json').write_text(json.dumps(report, indent=2) + '\n')
    return report


OBJECT_PROBE = r'''
import hashlib, json, sys
from minio import Minio
from minio.error import S3Error
import urllib3
try:
    payload = json.load(sys.stdin)
    client = Minio(payload['endpoint'], access_key=payload['access_key'], secret_key=payload['secret_key'],
                   secure=False, http_client=urllib3.PoolManager(timeout=urllib3.Timeout(connect=5, read=10), retries=False))
    rows = []
    assert 0 < len(payload['objects']) <= 256
    for expected in payload['objects']:
        row = {key: expected[key] for key in ('bucket', 'object', 'exists')}
        try:
            actual = client.stat_object(expected['bucket'], expected['object'])
        except S3Error as error:
            assert not expected['exists'] and error.code == 'NoSuchKey'
        else:
            assert expected['exists'] and actual.size == expected['size']
            assert 0 < actual.size <= 32 * 1024 * 1024
            response = client.get_object(expected['bucket'], expected['object'])
            try:
                raw = response.read(actual.size + 1)
                assert len(raw) == actual.size and hashlib.sha256(raw).hexdigest() == expected['sha256']
            finally:
                response.close()
                response.release_conn()
            row.update(size=len(raw), sha256=hashlib.sha256(raw).hexdigest())
        rows.append(row)
    print(json.dumps(rows))
except Exception:
    # Never print an S3 exception, connection object or credential-bearing input.
    print('exact physical object verification failed', file=sys.stderr)
    sys.exit(1)
'''


def verify_physical_objects(objects):
    identity = model3d_product_identity()
    require(isinstance(objects, list) and 0 < len(objects) <= 256 and
            len({(row['bucket'], row['object']) for row in objects}) == len(objects),
            'FTP physical verification requires a finite unique owner-provided object set')
    subprocess.run(['bash', 'scripts/infra/status.sh'], check=True, capture_output=True, text=True, timeout=30)
    labels = json.loads(subprocess.check_output(['docker', 'inspect', '-f', '{{json .Config.Labels}}', 'addp-minio'], text=True, timeout=10))
    require(labels.get('com.docker.compose.project') == 'addp-infra' and labels.get('com.docker.compose.service') == 'minio',
            'FTP physical verification requires this Hosted Infra MinIO')
    port = subprocess.check_output(['docker', 'port', 'addp-minio', '9000/tcp'], text=True, timeout=10).strip()
    require(re.fullmatch(r'127\.0\.0\.1:[1-9][0-9]{0,4}', port) and int(port.rsplit(':', 1)[1]) <= 65535,
            'FTP physical verifier must use the actual loopback MinIO mapping')
    payload = {'endpoint': port, 'access_key': SUPPORT.required_environment('MINIO_ROOT_USER'),
               'secret_key': SUPPORT.required_environment('MINIO_ROOT_PASSWORD'), 'objects': objects}
    try:
        result = subprocess.run(['docker', 'exec', '-i', identity['container'], 'python', '-c', OBJECT_PROBE],
                                input=json.dumps(payload), text=True, capture_output=True, timeout=60)
    except subprocess.TimeoutExpired:
        raise SuiteError('FTP exact physical verification exceeded its bounded budget') from None
    require(result.returncode == 0 and len(result.stdout) <= 256 * 1024,
            'FTP exact physical object verification failed')
    actual = json.loads(result.stdout)
    require(actual == objects, 'FTP physical object observations differ from the exact owner facts')
    return actual


def object_ref(storage_ref):
    parsed = json.loads(storage_ref)
    require(isinstance(parsed, dict) and set(parsed) == {'type', 'provider', 'bucket', 'object'} and
            parsed['type'] == 'object' and parsed['provider'] == 'addp_object_storage' and
            isinstance(parsed['bucket'], str) and bool(parsed['bucket']) and
            not any(char in parsed['bucket'] for char in '/\\\x00') and
            isinstance(parsed['object'], str) and bool(parsed['object']) and
            all(part and part not in {'.', '..'} for part in parsed['object'].split('/')),
            'FTP physical verification requires the actual canonical Manager object reference')
    return {key: parsed[key] for key in ('bucket', 'object')}


def owned_model_details(client, report, model, published):
    detail = client.request('GET', f"/api/v1/manager/model_3d_glb/{published['result_id']}", (200,)).payload
    require(isinstance(detail, dict) and all(detail.get(key) == value for key, value in {
        'id': published['result_id'], 'tenant_id': report['tenant_id'], 'task_id': published['task_id'],
        'last_execution_id': published['execution_id'], 'item_id': model['item_id'],
        'item_fingerprint': model['fingerprint'], 'source_engine_id': report['engine_id'],
        'source_format': model['format'], 'size_bytes': published['size'], 'status': 'ready'}.items()) and
        detail.get('metadata', {}).get('model3d_output_owner') == 'manager_execution',
        'FTP result owner facts differ from the actually published model')
    ref = object_ref(detail['storage_ref'])
    require(ref['object'].startswith(f"tenant_{report['tenant_id']}/model3d-quick-view/executions/{published['execution_id']}/"),
            'FTP output does not belong to the observed execution')
    return detail


def owned_input_objects(detail, execution_id, composition):
    preparations = detail['metadata'].get('model3d_output_lifecycle', {}).get('input_preparations', [])
    matched = [row for row in preparations if row.get('execution_id') == execution_id]
    require(len(matched) == 1, 'FTP execution requires one persisted exact input preparation')
    preparation = matched[0]
    identifier = preparation.get('preparation_id')
    require(isinstance(identifier, str) and str(uuid.UUID(identifier)) == identifier and
            type(preparation.get('attempt')) is int and preparation['attempt'] > 0 and
            preparation.get('released') is True and preparation.get('watch') is False and preparation.get('cleaned') is True,
            'FTP temporary input preparation has not reached a settled cleanup state')
    objects = preparation.get('objects')
    primary, = [ref for ref in composition if ref['primary']]
    parent = primary['path'].split('/')[:-1]
    prefix = f"temp/model3d-input/tenant_{detail['tenant_id']}/{execution_id}/attempt_{preparation['attempt']}/{identifier}/"
    expected_names = {'/'.join(ref['path'].split('/')[len(parent):]) for ref in composition}
    bucket = object_ref(detail['storage_ref'])['bucket']
    require(isinstance(objects, list) and len(objects) == len(expected_names) and
            all(set(row) == {'bucket', 'object'} and row['bucket'] == bucket and row['object'].startswith(prefix) for row in objects) and
            {row['object'][len(prefix):] for row in objects} == expected_names,
            'FTP persisted input set differs from the exact model composition')
    # Probe the returned keys themselves, never an inferred prefix listing.
    return [{**row, 'exists': False} for row in objects]


def verify_owned_model_storage(client, report):
    proofs = []
    for model, published in zip(report['models'], report['browser']['models']):
        detail = owned_model_details(client, report, model, published)
        output = {**object_ref(detail['storage_ref']), 'exists': True, 'size': published['size'], 'sha256': published['sha256']}
        inputs = owned_input_objects(detail, published['execution_id'], model['composition'])
        proofs.append({'format': model['format'], 'result_id': published['result_id'],
                       'objects': verify_physical_objects([output, *inputs])})
    return proofs


def result_without_housekeeping_clock(detail):
    observed = copy.deepcopy(detail)
    # The existing owner cleanup worker refreshes this clock for retained
    # late-write watches. It does not alter artifact identity or visibility.
    observed.get('metadata', {}).get('model3d_output_lifecycle', {}).pop('checked_at', None)
    return observed


def delete_owned_task_definitions(client, report):
    # Task definitions, execution history and published artifacts have separate
    # lifecycles. Freeze all exact owners before the first destructive request.
    require(exact_user_principal(client, report['tenant_id'], PERMISSIONS) == report['principal_id'],
            'FTP task deletion requires the same ordinary consumer User')
    models, published_models = report['models'], report['browser']['models']
    require(len(models) == len(published_models) and
            len({row['task_id'] for row in published_models}) == len(models) and
            len({row['result_id'] for row in published_models}) == len(models),
            'FTP task deletion requires distinct observed tasks and results')
    plans = []
    for model, published in zip(models, published_models):
        require(model['format'] == published['format'], 'FTP task deletion model order differs')
        path = f"/api/v1/manager/tasks/model_3d_glb_generation/{published['task_id']}"
        task = client.request('GET', path, (200,)).payload
        latest = latest_owner_execution(report, model['format'])
        require(isinstance(task, dict) and all(task.get(key) == value for key, value in {
            'id': published['task_id'], 'tenant_id': report['tenant_id'],
            'task_type': 'model_3d_glb_generation', 'created_by': int(report['principal_id']),
            'last_execution_id': latest['execution_id'], 'last_execution_status': latest['status']}.items()) and
            all(task.get('source', {}).get(key) == value for key, value in {
                'item_id': model['item_id'], 'item_fingerprint': model['fingerprint'],
                'source_engine_id': report['engine_id'], 'format': model['format']}.items()),
            'FTP task deletion refused an unowned or changed task definition')
        detail = owned_model_details(client, report, model, published)
        observed = [published, *[row for row in report.get('denied_generations', [])
                                 if row['format'] == model['format']]]
        if report.get('failed_generation', {}).get('format') == model['format']:
            observed.append(report['failed_generation'])
        history = {}
        for row in observed:
            identifier = row['execution_id']
            execution = client.request('GET', f'/api/v1/manager/executions/{identifier}', (200,)).payload
            require(isinstance(execution, dict) and all(execution.get(key) == value for key, value in {
                'execution_id': identifier, 'module': 'manager', 'source': 'manager',
                'task_type': 'model_3d_glb_generation', 'source_task_id': str(published['task_id']),
                'triggered_by': int(report['principal_id']), 'status': row.get('status', 'success')}.items()) and
                execution.get('completed_at') and execution.get('parent_execution_id') is None,
                'FTP task deletion requires settled owned execution history')
            history[identifier] = execution
        plans.append({'path': path, 'task': task, 'model': model, 'published': published,
                      'detail': detail, 'history': history})
    deleted = []
    for index, plan in enumerate(plans):
        path, published, model = plan['path'], plan['published'], plan['model']
        client.request('DELETE', path, (204,))
        client.request('GET', path, (404,))
        rejection = client.request('POST', path + '/execute', (404,),
                                   {'parameters': {'existing_result_action': 'overwrite'}}).payload
        require(isinstance(rejection, dict) and bool(rejection.get('error')) and
                'execution_id' not in rejection,
                'FTP deleted task must reject execution without creating an execution response')
        for remaining in plans[index + 1:]:
            require(client.request('GET', remaining['path'], (200,)).payload == remaining['task'],
                    'FTP task deletion changed another owned task')
        require(result_without_housekeeping_clock(owned_model_details(client, report, model, published)) ==
                result_without_housekeeping_clock(plan['detail']),
                'FTP task deletion changed the retained result')
        for identifier, before in plan['history'].items():
            require(client.request('GET', f'/api/v1/manager/executions/{identifier}', (200,)).payload == before,
                    'FTP task deletion changed or removed execution history')
        objects = verify_physical_objects([{**object_ref(plan['detail']['storage_ref']), 'exists': True,
                                           'size': published['size'], 'sha256': published['sha256']}])
        deleted.append({'format': model['format'], 'task_id': published['task_id'], 'delete_status': 204,
                        'detail_status': 404, 'execute_status': 404, 'preserved_result_id': published['result_id'],
                        'preserved_execution_ids': sorted(plan['history']),
                        'unchanged_remaining_task_ids': [row['task']['id'] for row in plans[index + 1:]],
                        'objects': objects})
    report['task_deletion_cached_content'] = verify_cached_results(
        client, report, {model['format']: 403 for model in models})
    return deleted


def delete_owned_models(client, report):
    deleted = []
    for model, published in zip(report['models'], report['browser']['models']):
        detail = owned_model_details(client, report, model, published)
        output = {**object_ref(detail['storage_ref']), 'exists': False}
        inputs = owned_input_objects(detail, published['execution_id'], model['composition'])
        failed = report.get('failed_generation', {})
        watched = []
        known = {published['execution_id']}
        if failed.get('format') == model['format']:
            known.add(failed['execution_id'])
            inputs.extend(owned_input_objects(detail, failed['execution_id'], model['composition']))
        lifecycle = detail['metadata']['model3d_output_lifecycle']
        denied = {row['execution_id'] for row in report.get('denied_generations', []) if row['format'] == model['format']}
        require(not denied.intersection(known) and not any(row.get('execution_id') in denied
                for row in [*lifecycle['input_preparations'], *lifecycle.get('garbage', [])]),
                'FTP authorization-denied executions must not own staged inputs or watched outputs')
        require({row['execution_id'] for row in lifecycle['input_preparations']} == known,
                'FTP result cleanup must not act on an unobserved input execution')
        for row in lifecycle.get('garbage', []):
            require(row.get('execution_id') in known and row.get('watch') is True,
                    'FTP result cleanup must not act on an unobserved output execution')
            ref = object_ref(row['storage_ref'])
            require(ref['object'].startswith(f"tenant_{report['tenant_id']}/model3d-quick-view/executions/{row['execution_id']}/"),
                    'FTP watched output must belong to the observed execution')
            watched.append({**ref, 'exists': False})
        path = f"/api/v1/manager/model_3d_glb/{published['result_id']}"
        client.request('DELETE', path, (200,))
        client.request('GET', path, (404,))
        client.request('GET', path + '/content', (404,))
        deleted.append({'format': model['format'], 'result_id': published['result_id'],
                        'objects': verify_physical_objects([output, *inputs, *watched]),
                        'watch_scope': 'Exact objects absent at observation; uncertain late-write watches remain owned'})
    return deleted


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


def model_source_type():
    return os.environ.get('ADDP_ONLINE_MODEL_FILE_ENGINE_TYPE', 'ftp')

def model_item_type():
    return 'object' if model_source_type() == 'minio' else 'file'

def model_scope(path):
    return 'model-source/' + path if model_source_type() == 'minio' else path

def validate_item(item, sample):
    require(item.get('item_type') == model_item_type() and item.get('full_name') == sample['full_name'] and item.get('fingerprint'),
            'FTP Meta item must preserve its exact account-relative file identity')
    attributes = item.get('attributes', {})
    require(attributes.get('schema_version') == 1 and attributes.get('item', {}).get('data_type') == 'table',
            'FTP tabular file must reuse the existing table data type')
    fields = attributes.get('type_info', {}).get('table', {}).get('fields', [])
    require({field.get('name') for field in fields} == {'id', 'name', 'amount'},
            'FTP Meta scan omitted the native file schema')


def file_locator(engine_id, item):
    path = '/'.join(quote(part, safe='') for part in item['full_name'].split('/'))
    return f'addp://engine/{engine_id}/path/{path}?' + urlencode({'type': model_item_type(), 'item_id': item['id']})


def file_target(engine_id, path, source_type=None):
    parts = path.split('/')
    require(type(engine_id) is int and engine_id > 0 and parts and
            all(part and part not in {'.', '..'} and not any(char in part for char in '\\:') for part in parts),
            'FTP approval requires an exact account-relative leaf')
    if (source_type or model_source_type()) == 'minio':
        require(len(parts) >= 2, 'object target requires bucket and key')
        segments = [{'term':'service','kind':'service','name':''}, {'term':'bucket','kind':'bucket','name':parts[0]}]
        segments.extend({'term':'prefix','kind':'prefix','name':part} for part in parts[1:-1])
        segments.append({'term':'object','kind':'object','name':parts[-1]})
        return {'version':'catalog.path/v1','engine_id':engine_id,'segments':segments}
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

    def __init__(self, issuer, consumer, report, *, source_type=None, consumer_permissions=None):
        self.source_type = source_type or report.get("source_engine_type", model_source_type())
        self.target = lambda name: file_target(self.engine_id, name, self.source_type)
        tenant = report['tenant_id']
        require(type(tenant) is int and tenant > 1, 'FTP grants require the disposable nondefault Tenant')
        self.operator = exact_user_principal(issuer, tenant, ISSUER_PERMISSIONS)
        self.recipient = exact_user_principal(consumer, tenant, consumer_permissions or PERMISSIONS)
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
        expected_refs = [{'target': self.target(ref['path']), 'role': ref['role'],
                          'required': ref['required'], 'primary': ref['primary']} for ref in model['composition']]
        # The fixed fixture's typed targets use the shared boundary's canonical
        # JSON byte order. Primary is a semantic flag, never an ordering rule.
        expected_refs.sort(key=lambda ref: json.dumps(ref['target'], ensure_ascii=False, separators=(',', ':')))
        boundary = {'layout': 'single', 'file_composition': {'format': model['format'], 'refs': expected_refs}}
        require(isinstance(facts, dict) and all(facts.get(key) == value for key, value in {
            'schema_version': 'meta.data_item_authorization_facts/v1', 'engine_id': str(self.engine_id),
            'item_fingerprint': model['fingerprint'], 'full_name': model['full_name'],
            'item_type': 'object' if self.source_type == 'minio' else 'file', 'basis_state': 'complete', 'boundary': boundary}.items()) and
            isinstance(facts.get('boundary_hash'), str) and re.fullmatch('[0-9a-f]{64}', facts['boundary_hash']) and
            bool(facts.get('observed_at')),
            'FTP approval requires matching complete System facts, never a directory or caller-supplied composition')
        return facts

    def grant(self, model, *, initialize=True):
        facts = self.facts(model)
        command = str(uuid.uuid4())
        body = {'request_id': command, 'catalog_path': self.target(model['full_name']),
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
        body = {'deny_id': command, 'catalog_path': self.target(model['full_name']),
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
            require(isinstance(payload, dict) and set(payload) <= {'error', 'error_code'} and bool(payload.get('error')) and
                    payload.get('error_code') != 'security_protection_required',
                    'FTP withdrawal must reject cached content with an explicit source denial')
        else:
            require(isinstance(payload, bytes) and len(payload) == model['size'] and
                    hashlib.sha256(payload).hexdigest() == model['sha256'],
                    'FTP source rule control must preserve the exact already-verified GLB')
        results.append({'format': model['format'], 'result_id': result_id, 'status': status})
    return results


def latest_owner_execution(report, kind):
    for row in reversed(report.get('denied_generations', [])):
        if row['format'] == kind:
            return row
    failed = report.get('failed_generation', {})
    if failed.get('format') == kind:
        return failed
    published, = [row for row in report['browser']['models'] if row['format'] == kind]
    return {'execution_id': published['execution_id'], 'status': 'success'}


def verify_denied_generation(client, report, kind, reason):
    require(kind in {'ifc', '3ds', 'dae', 'obj'} and reason in {'composition_changed', 'source_withdrawn'},
            'FTP denied generation requires its explicit observed source rule phase')
    model, = [row for row in report['models'] if row['format'] == kind]
    published, = [row for row in report['browser']['models'] if row['format'] == kind]
    task_path = f"/api/v1/manager/tasks/model_3d_glb_generation/{published['task_id']}"
    before_task = client.request('GET', task_path, (200,)).payload
    previous = latest_owner_execution(report, kind)
    require(before_task.get('id') == published['task_id'] and before_task.get('task_type') == 'model_3d_glb_generation' and
            before_task.get('last_execution_id') == previous['execution_id'] and
            before_task.get('last_execution_status') == previous['status'],
            'FTP denied generation must use the observed owned task history')
    before = owned_model_details(client, report, model, published)
    denial = client.request('POST', task_path + '/execute', (403,),
                            {'parameters': {'existing_result_action': 'overwrite'}}).payload
    require(isinstance(denial, dict) and set(denial) == {'error'} and
            isinstance(denial['error'], str) and bool(denial['error'].strip()),
            'FTP denied generation must follow the task execute HTTP 403 error contract')
    task = client.request('GET', task_path, (200,)).payload
    identifier = task.get('last_execution_id')
    known = {row['execution_id'] for row in report['browser']['models']}
    known.update(row['execution_id'] for row in report.get('denied_generations', []))
    if report.get('failed_generation'):
        known.add(report['failed_generation']['execution_id'])
    require(task.get('id') == published['task_id'] and task.get('task_type') == 'model_3d_glb_generation' and
            isinstance(identifier, str) and str(uuid.UUID(identifier)) == identifier and identifier not in known and
            task.get('last_execution_status') == 'failed',
            'FTP source denial must leave a distinct settled owner execution audit')
    execution = client.request('GET', f'/api/v1/manager/executions/{identifier}', (200,)).payload
    require(all(execution.get(key) == value for key, value in {
        'execution_id': identifier, 'module': 'manager', 'source': 'manager',
        'task_type': 'model_3d_glb_generation', 'source_task_id': str(published['task_id']),
        'status': 'failed', 'progress': 0, 'trigger_type': 'manual',
        'triggered_by': int(report['principal_id'])}.items()) and
        type(execution.get('attempt')) is int and execution['attempt'] == 0 and
        execution.get('parent_execution_id') is None and
        execution.get('started_at') is None and bool(execution.get('completed_at')) and
        execution.get('error_details', {}).get('code') == 'source_authorization_required' and
        not execution.get('metadata', {}).get('workflow_runtime') and
        not execution.get('metadata', {}).get('glb_facts'),
        'FTP source denial must remain an unclaimed failed attempt, not a conversion')
    after = owned_model_details(client, report, model, published)
    require(after['storage_ref'] == before['storage_ref'], 'FTP source denial changed the published GLB')
    lifecycle = after['metadata']['model3d_output_lifecycle']
    require(not any(row.get('execution_id') == identifier for row in
                    [*lifecycle['input_preparations'], *lifecycle.get('garbage', [])]),
            'FTP source denial must not create input preparation or output watch ownership')
    objects = verify_physical_objects([{**object_ref(after['storage_ref']), 'exists': True,
                                       'size': published['size'], 'sha256': published['sha256']}])
    return {'format': kind, 'task_id': published['task_id'], 'execution_id': identifier,
            'http_status': 403, 'http_error_fields': ['error'],
            'status': 'failed', 'reason': reason, 'attempt': 0, 'started_at': None,
            'input_preparation_created': False, 'preserved_result_id': published['result_id'],
            'preserved_execution_id': published['execution_id'], 'objects': objects,
            'proof_scope': 'Public unclaimed failure and exact result ownership; private EA and lease constraints are owner T1/T2 evidence'}


def owned_ftp_fixture():
    if os.environ.get("ADDP_ONLINE_MODEL_FILE_ENGINE_TYPE", "ftp") == "nfs":
        return import_module("scripts.test.online-file-model-nfs-fixture").owned_root()
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
    return root.resolve()


@contextmanager
def changed_texture_fixture(model):
    """Change only the fixed model in this Hosted run owned read-only mount."""
    if model_source_type() == 'minio':
        with import_module('scripts.test.online-file-model-minio-fixture').changed_texture(model) as changed:
            yield changed
        return
    if model['format'] == 'obj':
        with changed_obj_material_fixture(model) as changed:
            yield changed
        return
    root = owned_ftp_fixture()
    kind = model['format']
    require(kind in {'3ds', 'dae'}, 'composition fixture requires a declared texture model')
    paths = [root / relative for relative in ('data', 'data/models', f'data/models/{kind}',
             f'data/models/{kind}/model.{kind}', f'data/models/{kind}/texture.png')]
    require(all(not path.is_symlink() for path in paths), 'FTP composition fixture must not traverse a link')
    main, texture = paths[-2:]
    original, png = main.read_bytes(), texture.read_bytes()
    fixture = Path(__file__).resolve().parents[2] / 'business/fixtures/manager'
    require(model['full_name'] == f'models/{kind}/model.{kind}' and
            original == (fixture / f'addp_online_model_fixture.{kind}').read_bytes() and
            png == (fixture / 'addp_online_exchange_texture.png').read_bytes() and
            hashlib.sha256(original).hexdigest() == model['sha256'] and original.count(b'texture.png') == 1,
            'FTP composition mutation only accepts the fixed physical model and texture')
    replacement_name = 'changed.png' if kind == 'dae' else ' tex.png   '
    changed = original.replace(b'texture.png', replacement_name.encode())
    replacement = texture.with_name(replacement_name)
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
                ref['path'] = f'models/{kind}/{replacement_name}'
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


@contextmanager
def changed_obj_material_fixture(model):
    root = owned_ftp_fixture() / 'data/models/obj'
    fixture = Path(__file__).resolve().parents[2] / 'business/fixtures/manager'
    material, texture, replacement = root/'paint red.mtl', root/'red/color.png', root/'red/changed.png'
    original = (fixture/'addp_online_multi_material_red.mtl').read_bytes()
    png = (fixture/'addp_online_obj_red.png').read_bytes()
    require(model['full_name'] == 'models/obj/model.obj' and
            all(not leaf.is_symlink() for leaf in [root, root.parent, root.parent.parent, root/'red', root/'model.obj', material, texture]) and
            material.read_bytes() == original and texture.read_bytes() == png and
            (root/'model.obj').read_bytes() == (fixture/'addp_online_multi_material.obj').read_bytes(),
            'OBJ mutation requires the fixed owned model and exact declared MTL')
    changed = original.replace(b'red/color.png', b'red/changed.png')
    descriptor = os.open(replacement, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o644)
    identity = os.fstat(descriptor)
    published = False
    try:
        with os.fdopen(descriptor, 'wb') as stream:
            stream.write(png)
            os.fchmod(stream.fileno(), 0o644)
        published = True
        replace_fixture_file(material, changed)
        projected = copy.deepcopy(model)
        matched = [ref for ref in projected['composition'] if ref['path'] == 'models/obj/red/color.png']
        require(len(matched) == 1, 'OBJ fixture requires one exact red texture member')
        matched[0]['path'] = 'models/obj/red/changed.png'
        yield projected
    finally:
        try:
            require(not material.is_symlink() and material.read_bytes() in (original, changed),
                    'OBJ cleanup refuses an unexpected MTL change')
            if material.read_bytes() != original:
                replace_fixture_file(material, original)
        finally:
            observed = replacement.lstat()
            content = replacement.read_bytes()
            require(not replacement.is_symlink() and replacement.is_file() and (observed.st_dev, observed.st_ino) == (identity.st_dev, identity.st_ino) and
                    (content == png if published else png.startswith(content)), 'OBJ cleanup refuses an unowned texture')
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


@contextmanager
def invalid_ifc_fixture(model):
    if model_source_type() == 'minio':
        with import_module('scripts.test.online-file-model-minio-fixture').invalid_ifc(model):
            yield
        return
    root = owned_ftp_fixture()
    require(model['format'] == 'ifc' and model['full_name'] == 'models/ifc/model.ifc',
            'FTP failure input must be the exact fixed IFC source')
    paths = [root / relative for relative in ('data', 'data/models', 'data/models/ifc', 'data/models/ifc/model.ifc')]
    require(all(not path.is_symlink() for path in paths), 'FTP failure input must not traverse a link')
    target = paths[-1]
    original = target.read_bytes()
    require(original == (Path(__file__).resolve().parents[2] / 'business/fixtures/manager/addp_online_model_fixture.ifc').read_bytes() and
            hashlib.sha256(original).hexdigest() == model['sha256'], 'FTP failure input source differs')
    invalid = b'INVALID_IFC_SOURCE\n'.ljust(len(original), b' ')
    try:
        replace_fixture_file(target, invalid)
        yield
    finally:
        require(not target.is_symlink() and target.read_bytes() in (original, invalid),
                'FTP failure cleanup refuses to overwrite unexpected source changes')
        if target.read_bytes() == invalid:
            replace_fixture_file(target, original)


def verify_failed_generation(client, report, timeout):
    model, = [row for row in report['models'] if row['format'] == 'ifc']
    published, = [row for row in report['browser']['models'] if row['format'] == 'ifc']
    before = owned_model_details(client, report, model, published)
    with invalid_ifc_fixture(model):
        response = client.request('POST', f"/api/v1/manager/tasks/model_3d_glb_generation/{published['task_id']}/execute",
                                  (202,), {'parameters': {'existing_result_action': 'overwrite'}}).payload
        identifier = response.get('execution_id')
        require(isinstance(identifier, str) and str(uuid.UUID(identifier)) == identifier and
                identifier != published['execution_id'] and response.get('status') in {'pending', 'running'},
                'FTP failed conversion requires a new real owner execution')
        deadline = time.monotonic() + min(timeout, 120)
        while time.monotonic() < deadline:
            execution = client.request('GET', f'/api/v1/manager/executions/{identifier}', (200,)).payload
            require(execution.get('execution_id') == identifier, 'FTP execution observation changed its identity')
            if execution.get('status') in {'success', 'failed', 'timeout', 'cancelled'}:
                break
            time.sleep(1)
        else:
            raise SuiteError('FTP invalid IFC conversion did not settle within its budget')
        require(execution.get('status') == 'failed', 'FTP semantic failure must reach failed, not success or timeout')
    after = owned_model_details(client, report, model, published)
    require(after['storage_ref'] == before['storage_ref'], 'FTP failed rebuild replaced the prior published GLB')
    inputs = owned_input_objects(after, identifier, model['composition'])
    garbage = [row for row in after['metadata']['model3d_output_lifecycle'].get('garbage', []) if row.get('execution_id') == identifier]
    require(len(garbage) == 1 and garbage[0].get('watch') is True,
            'FTP failed remote conversion must retain its exact output watch')
    failed_output = object_ref(garbage[0]['storage_ref'])
    require(failed_output != object_ref(after['storage_ref']) and
            failed_output['object'].startswith(f"tenant_{report['tenant_id']}/model3d-quick-view/executions/{identifier}/"),
            'FTP failed output watch must belong to its independent execution')
    objects = verify_physical_objects([
        {**object_ref(after['storage_ref']), 'exists': True, 'size': published['size'], 'sha256': published['sha256']},
        *inputs, {**failed_output, 'exists': False}])
    readable = verify_cached_results(client, report, {model['format']: 200 for model in report['models']})
    return {'format': 'ifc', 'task_id': published['task_id'], 'execution_id': identifier, 'status': 'failed',
            'preserved_result_id': published['result_id'], 'preserved_execution_id': published['execution_id'],
            'preserved_storage_ref': after['storage_ref'], 'objects': objects, 'cached_content': readable,
            'failed_output_watch': True, 'watch_scope': 'Observed absent now; persistent watch remains for uncertain late writes'}


def source_read_control(client, engine_id, model, status):
    response = client.request('POST', '/api/v1/system/engine-access/read-checks/manager-preview', (status,),
                              {'targets': [file_target(engine_id, model['full_name'])]}).payload
    require(isinstance(response, dict) and bool(response.get('observed_at') if status == 200 else response.get('error')),
            'FTP current source rule check omitted its explicit observation or denial')
    return status


def scan_scope(client, engine_id, directories, depth, timeout):
    require(depth in {'basic', 'deep'} and directories and all(isinstance(value, str) and
            all(part and part not in {'.', '..'} for part in value.split('/')) for value in directories),
            'FTP scan must use explicit account-relative directory scopes')
    created = client.request('POST', '/api/v1/meta/scan/run/manual', (201,),
        {'engine_id': engine_id, 'catalog_paths': directories, 'scan_depth': depth,
         'trigger_type': 'manual', 'force': True}).payload
    identifier = created.get('execution_id')
    require(isinstance(identifier, str) and str(uuid.UUID(identifier)) == identifier,
            'FTP scoped scan requires its actual execution identity')
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        current = client.request('GET', f'/api/v1/meta/executions/{identifier}', (200,)).payload
        require(current.get('execution_id') == identifier, 'FTP scan execution identity changed')
        if current.get('status') == 'success':
            require(current.get('scan_context', {}).get('scan_depth') == depth,
                    'FTP scan completed with a different depth')
            return identifier
        require(current.get('status') in {'pending', 'running'}, 'FTP scoped scan failed')
        time.sleep(1)
    raise SuiteError('FTP scoped scan did not complete within its existing budget')


def verify_old_grant_composition(client, authorization, report, source, timeout):
    original_hash = authorization.facts(source)['boundary_hash']
    try:
        with changed_texture_fixture(source) as changed:
            scan = scan_scope(client, report['engine_id'], [model_scope('models/' + source['format'])], 'basic', timeout)
            facts = authorization.facts(changed)
            require(facts['boundary_hash'] != original_hash, 'FTP composition boundary did not change')
            denied = source_read_control(client, report['engine_id'], changed, 403)
            stale = verify_cached_results(client, report, {model['format']: 403 if model['format'] == source['format'] else 200 for model in report['models']})
            generation = verify_denied_generation(client, report, source['format'], 'composition_changed')
            return {'scan_execution_id': scan, 'original_boundary_hash': original_hash,
                    'changed_boundary_hash': facts['boundary_hash'],
                    'old_grant_without_deny': denied, 'old_cached_content': stale, 'denied_generation': generation}
    finally:
        scan_scope(client, report['engine_id'], [model_scope('models/' + source['format'])], 'basic', timeout)


def verify_composition_rules(client, authorization, report, source, deny, timeout):
    prior, = [grant for grant in report['source_authorizations'] if grant['item_fingerprint'] == source['fingerprint']]
    original_hash = authorization.facts(source)['boundary_hash']
    try:
        with changed_texture_fixture(source) as changed:
            scan = scan_scope(client, report['engine_id'], [model_scope('models/' + source['format'])], 'basic', timeout)
            facts = authorization.facts(changed)
            require(facts['boundary_hash'] != original_hash,
                    'FTP changed texture reference must change the complete approval boundary')
            # The production relation admits one active Grant per item/subject.
            authorization.revoke(prior['request_id'])
            new_grant = authorization.grant(changed, initialize=False)
            denied = source_read_control(client, report['engine_id'], changed, 403)
            authorization.release(deny)
            allowed = source_read_control(client, report['engine_id'], changed, 200)
            stale = verify_cached_results(client, report, {model['format']: 403 if model['format'] == source['format'] else 200 for model in report['models']})
            result = {'scan_execution_id': scan, 'original_boundary_hash': original_hash,
                      'changed_boundary_hash': facts['boundary_hash'], 'changed_grant': new_grant,
                      'old_deny_after_new_grant': denied, 'new_grant_after_release': allowed,
                      'old_cached_content': stale}
    finally:
        scan_scope(client, report['engine_id'], [model_scope('models/' + source['format'])], 'basic', timeout)
    authorization.revoke(new_grant['request_id'])
    result['restored_grant'] = authorization.grant(source, initialize=False)
    return result


def prepare_models(items, engine_id, samples):
    expected = {key:(model_scope(relative), source) for key,relative,source in SEED.MODEL_SAMPLE_FILES}
    require(isinstance(samples, list) and len(samples) == len(expected) and
            {sample.get('format') for sample in samples} == set(expected),
            'FTP model manifest requires the exact four model profiles and their declared dependencies')
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
    for kind in ('ifc', '3ds', 'dae', 'obj'):
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
                item.get('item_type') == model_item_type() and attributes.get('schema_version') == 1 and
                all(facts.get(key) == value for key, value in
                    {'data_type': 'model_3d', 'layout': 'single', 'format': kind}.items()) and
                attributes.get('storage', {}).get('physical_path') == sample['full_name'],
                f'FTP {kind} model identity or single-file facts are missing')
        refs = facts.get('refs')
        # IFC's fixed geometry profile has no external inputs; Meta may project
        # its sole primary from the committed single-file identity.
        if kind == 'ifc' and refs is None:
            refs = [{'path': sample['full_name'], 'role': 'model', 'required': True, 'primary': True}]
        expected_refs = [{'path': sample['full_name'], 'role': 'model', 'required': True, 'primary': True}]
        if kind in {'3ds', 'dae'}:
            require(native.get('scan_complete') is True,
                    'FTP texture model requires complete structural native facts')
            require(native.get('texture_refs') == ['texture.png'],
                    'FTP texture model must retain its exact texture declaration')
            expected_refs.append({'path': by_format['dae_png' if kind == 'dae' else 'png']['full_name'], 'role': 'texture', 'required': True, 'primary': False})
        if kind == 'obj':
            require(native.get('composition_complete') is True and native.get('material_libraries') == [
                {'reference':'paint blue.mtl','texture_refs':['blue/color.png']},
                {'reference':'paint red.mtl','texture_refs':['red/color.png']}],
                'OBJ requires the exact complete two-material declaration graph')
            for key,role in [('obj_mtl_blue','material_library'),('obj_mtl_red','material_library_2'),
                             ('obj_png_blue','texture'),('obj_png_red','texture_2')]:
                expected_refs.append({'path':by_format[key]['full_name'],'role':role,'required':True,'primary':False})
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


def run(client, tenant_id, engine_id, samples, model_samples, timeout, source_type="ftp"):
    require(source_type in {"ftp", "nfs", "minio"}, "unadmitted file model source")
    require(tenant_id > 1, 'FTP Online requires a nondefault disposable Tenant')
    API.validate_user_identity(client, tenant_id, PERMISSIONS)
    API.wait_manager_read_boundary(client, engine_id, time.monotonic() + min(timeout, 120))
    context = client.request('GET', '/api/v1/system/auth/context', (200,)).payload
    selectors = client.request('GET', '/api/v1/system/engine-catalog/engines', (200,)).payload
    selected = [entry for entry in selectors if entry.get('id') == engine_id]
    require(len(selected) == 1 and selected[0].get('engine_type') == source_type and 'connection_info' not in selected[0],
            'FTP must be in the ordinary User catalog without credentials')
    capabilities = selected[0].get('capabilities', {})
    store = capabilities.get('storage', {}).get('store', {})
    require(capabilities.get('engine_family') == ('object' if source_type == 'minio' else 'file') and store.get('stream_read') is True and
            store.get('range_read') is True and (source_type in {'nfs','minio'} or (not store.get('stream_write') and not store.get('delete'))),
            'FTP must declare only the admitted read capabilities')
    endpoint = f'/api/v1/system/engines/{engine_id}/catalog/children'
    roots = client.request('POST', endpoint, (200,), {'path': {'version': 'catalog.path/v1', 'segments': []}}).payload['nodes']
    require(len(roots) == 1 and roots[0]['kind'] == ('service' if source_type == 'minio' else 'root'), 'FTP must expose one explicit account root')
    directories = client.request('POST', endpoint, (200,), {'path': roots[0]['path']}).payload['nodes']
    if source_type == 'minio':
        bucket, = [entry for entry in directories if entry['name'] == 'model-source' and entry['kind'] == 'bucket']
        directories = client.request('POST', endpoint, (200,), {'path':bucket['path']}).payload['nodes']
    branches = [entry for entry in directories if entry['name'] == 'examples']
    require(len(branches) == 1 and branches[0]['kind'] == ('prefix' if source_type == 'minio' else 'directory'), 'FTP account root lost the examples directory')
    files = client.request('POST', endpoint, (200,), {'path': branches[0]['path']}).payload['nodes']
    for sample in samples:
        matches = [entry for entry in files if entry['name'] == sample['full_name'].split('/')[-1]]
        require(len(matches) == 1 and matches[0]['kind'] == model_item_type(), 'FTP live catalog normalized or omitted a sample name')
        facts = client.request('POST', f'/api/v1/system/engines/{engine_id}/catalog/facts', (200,), {'path': matches[0]['path']}).payload
        storage = facts.get('storage', {})
        require(storage.get('path') == sample['full_name'] and storage.get('size_bytes') == sample['size'] and (bool(storage.get('etag')) if source_type == 'minio' else not storage.get('etag')),
                'FTP facts must preserve native path/size without inventing an ETag')
    model_scan = scan_scope(client, engine_id, [model_scope('models')], 'basic', timeout)
    execution_id = scan_scope(client, engine_id, [model_scope('examples')], 'deep', timeout)
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
            'source_engine_type': source_type, 'scan_execution_id': execution_id, 'model_scan_execution_id': model_scan, 'samples': result,
            'models': prepare_models(items, engine_id, model_samples)}


def validate_source_download(observed, source, artifact_dir):
    bundled = len(source['composition']) > 1
    artifact = f"ftp-{source['format']}-source.{'zip' if bundled else source['format']}"
    filename = Path(source['full_name']).name + ('.zip' if bundled else '')
    require(isinstance(observed, dict) and set(observed) == {
                'artifact', 'status', 'size', 'sha256', 'content_type', 'content_disposition'} and
            observed.get('artifact') == artifact and type(observed.get('status')) is int and
            observed['status'] == 200 and type(observed.get('size')) is int and
            0 < observed['size'] <= 64 * 1024 and
            isinstance(observed.get('content_type'), str) and bool(observed['content_type']) and
            observed.get('content_disposition') == f'attachment; filename="{filename}"',
            'FTP logical source download identity, headers or bounded size differs')
    require(json.loads((artifact_dir / f"ftp-{source['format']}-source.json").read_text()) == observed,
            'FTP logical source response metadata differs from the browser report')
    raw = (artifact_dir / artifact).read_bytes()
    require(len(raw) == observed['size'] and hashlib.sha256(raw).hexdigest() == observed['sha256'],
            'FTP logical source response differs from the captured bytes')
    fixture = Path(__file__).resolve().parents[2] / 'business/fixtures/manager'
    pinned = {model_scope(relative):source for _,relative,source in SEED.MODEL_SAMPLE_FILES}
    require(all(ref['path'] in pinned for ref in source['composition']),
            'FTP source download requires the exact confirmed fixed-fixture composition')
    parent = source['full_name'].rsplit('/',1)[0] + '/'
    expected = {ref['path'][len(parent):]: (fixture / pinned[ref['path']]).read_bytes()
                for ref in source['composition']}
    require(len(expected) == len(source['composition']) and
            len(expected[Path(source['full_name']).name]) == source['size'] and
            hashlib.sha256(expected[Path(source['full_name']).name]).hexdigest() == source['sha256'],
            'FTP source manifest differs from the fixed physical primary')
    if not bundled:
        require(observed['content_type'] != 'application/zip' and raw == next(iter(expected.values())),
                'FTP single-file source differs from the physical primary')
        return
    require(observed['content_type'] == 'application/zip', 'FTP multi-ref source must be a logical ZIP bundle')
    try:
        with zipfile.ZipFile(io.BytesIO(raw)) as archive:
            entries = archive.infolist()
            require(len(entries) == len(expected) and {entry.filename for entry in entries} == set(expected),
                    'FTP source ZIP must contain exactly the confirmed primary and dependencies, without duplicates')
            for entry in entries:
                original = expected[entry.filename]
                require(not entry.is_dir() and not entry.flag_bits & 1 and entry.file_size == len(original),
                        'FTP source ZIP member type, encryption or declared size differs')
                with archive.open(entry) as content:
                    actual = content.read(len(original) + 1)
                require(actual == original and hashlib.sha256(actual).digest() == hashlib.sha256(original).digest(),
                        'FTP source ZIP member bytes or SHA-256 differ from the physical seed')
    except SuiteError:
        raise
    except (zipfile.BadZipFile, RuntimeError, NotImplementedError, EOFError, ValueError, OSError) as error:
        raise SuiteError('FTP logical source ZIP is invalid or corrupt') from error


def validate_obj_multi_material_glb(raw):
    doc, view_bytes = MODEL.parse_model_glb(raw)
    fixture = Path(__file__).resolve().parents[2] / 'business/fixtures/manager'
    expected = {}
    for color, rgb, points in [('red', (220, 30, 60), {(0.,0.,0.),(1.,0.,0.),(0.,1.,0.)}),
                               ('blue', (30, 70, 220), {(1.,0.,0.),(2.,0.,0.),(2.,1.,0.)})]:
        png = (fixture / f'addp_online_obj_{color}.png').read_bytes()
        MODEL.validate_fixture_png(png, expected_size=2, expected_rgb=rgb)
        expected[hashlib.sha256(png).hexdigest()] = (png, points)
    images = doc.get('images', [])
    require(len(images) == 2, 'OBJ must retain both distinct embedded textures')
    image_hashes = []
    for image in images:
        require(image.get('mimeType') == 'image/png' and 'uri' not in image,
                'OBJ textures must be embedded PNGs')
        data, _ = view_bytes(image.get('bufferView'))
        digest = hashlib.sha256(data).hexdigest()
        require(digest in expected and data == expected[digest][0], 'OBJ embedded texture differs from its exact source')
        image_hashes.append(digest)
    require(len(set(image_hashes)) == 2, 'OBJ textures with the same basename were merged')
    meshes, accessors = doc.get('meshes', []), doc.get('accessors', [])
    used_images, primitives = set(), 0
    def values(index, width, component, code):
        require(type(index) is int and 0 <= index < len(accessors), 'OBJ accessor index invalid')
        accessor = accessors[index]
        require(accessor.get('componentType') == component and accessor.get('count') == 3 and
                accessor.get('type') == ('SCALAR' if width == 1 else f'VEC{width}') and
                'sparse' not in accessor, 'OBJ triangle or UV accessor differs')
        data, view = view_bytes(accessor.get('bufferView'))
        step = struct.calcsize('<' + code * width)
        stride, start = view.get('byteStride', step), accessor.get('byteOffset', 0)
        require(type(stride) is int and stride >= step and type(start) is int and start >= 0 and
                start + stride * 2 + step <= len(data), 'OBJ accessor exceeds its buffer view')
        return [struct.unpack_from('<' + code * width, data, start + stride * i) for i in range(3)]
    for mesh in meshes:
        for primitive in mesh.get('primitives', []):
            primitives += 1
            require(primitive.get('mode', 4) == 4, 'OBJ primitive must remain a triangle')
            attributes = primitive.get('attributes', {})
            points = set(values(attributes.get('POSITION'), 3, 5126, 'f'))
            uv = values(attributes.get('TEXCOORD_0'), 2, 5126, 'f')
            require(len(set(uv)) == 3 and all(math.isfinite(n) and 0 <= n <= 1 for pair in uv for n in pair),
                    'OBJ UV coordinates must retain a finite triangle')
            material = doc['materials'][primitive['material']]
            pbr = material.get('pbrMetallicRoughness', {})
            texture = doc['textures'][pbr['baseColorTexture']['index']]
            image = texture['source']
            require(type(image) is int and 0 <= image < 2 and image not in used_images and
                    points == expected[image_hashes[image]][1], 'OBJ material bound the wrong colored triangle')
            used_images.add(image)
            index = primitive.get('indices')
            if index is not None:
                component = accessors[index]['componentType']
                require(component in {5121,5123,5125}, 'OBJ triangle index type differs')
                require({value[0] for value in values(index, 1, component, {5121:'B',5123:'H',5125:'I'}[component])} == {0,1,2},
                        'OBJ triangle indices differ')
    require(primitives == 2 and len(used_images) == 2, 'OBJ must retain both textured triangles')
    identity = [1.,0.,0.,0.,0.,1.,0.,0.,0.,0.,1.,0.,0.,0.,0.,1.]
    nodes, seen, rendered = doc.get('nodes', []), set(), set()
    def visit(index):
        require(type(index) is int and 0 <= index < len(nodes) and index not in seen, 'OBJ scene node invalid or reused')
        seen.add(index)
        node = nodes[index]
        require(not any(key in node for key in ('translation','rotation','scale')) and node.get('matrix', identity) == identity,
                'OBJ fixture contains unexpected transforms')
        if 'mesh' in node:
            require(type(node['mesh']) is int and 0 <= node['mesh'] < len(meshes) and node['mesh'] not in rendered,
                    'OBJ scene mesh missing or repeated')
            rendered.add(node['mesh'])
        for child in node.get('children', []):
            visit(child)
    require(len(doc.get('scenes', [])) == 1 and doc.get('scene', 0) == 0, 'OBJ must retain one active scene')
    for index in doc['scenes'][0].get('nodes', []):
        visit(index)
    require(rendered == set(range(len(meshes))), 'OBJ scene omitted a textured mesh')
    return {'vertex_count': 6, 'primitive_count': 2, 'embedded_images': 2, 'extent': [2.,1.,0.],
            'image_sha256': sorted(image_hashes)}


def validate_browser_report(actual, expected, artifact_dir):
    proof = {key: expected[key] for key in ('run_id', 'engine_id', 'tenant_id', 'principal_id')}
    proof.update(meta_ui_scan=True, previews=3, downloads=[{key: sample[key] for key in ('format', 'item_id', 'size', 'sha256')} for sample in expected['samples']])
    require({key: value for key, value in actual.items() if key != 'models'} == proof,
            'FTP browser identity, scan, preview or physical download proof differs')
    models = actual.get('models')
    require(isinstance(models, list) and len(models) == len(expected['models']),
            'FTP browser must generate and reopen every declared model format')
    for observed, source in zip(models, expected['models']):
        require(set(observed) == {'format', 'item_id', 'task_id', 'execution_id', 'result_id',
                                 'size', 'sha256', 'source_download', 'rendered', 'reopened',
                                 'cached_without_execution'} and
                observed.get('format') == source['format'] and observed.get('item_id') == source['item_id'] and
                all(type(observed.get(key)) is int and observed[key] > 0 for key in ('task_id', 'result_id', 'size')) and
                isinstance(observed.get('execution_id'), str) and bool(observed['execution_id']) and
                all(observed.get(key) is True for key in ('rendered', 'reopened', 'cached_without_execution')),
                'FTP browser model identity, source integrity, rendering or cache proof differs')
        validate_source_download(observed['source_download'], source, artifact_dir)
        raw = (artifact_dir / f"ftp-{source['format']}.glb").read_bytes()
        require(len(raw) == observed['size'] and hashlib.sha256(raw).hexdigest() == observed.get('sha256'),
                'FTP model report differs from actual downloaded GLB')
        try:
            if source['format'] == 'obj':
                validate_obj_multi_material_glb(raw)
            else:
                MODEL.validate_model_glb(raw, source['format'])
        except MODEL.SuiteError as error:
            raise SuiteError(f"FTP {source['format']} GLB physical verification failed: {error}") from error


def validate_withdrawal_browser_report(actual, expected):
    models = []
    for source, published in zip(expected['models'], expected['browser']['models']):
        latest = latest_owner_execution(expected, source['format'])
        models.append({'format': source['format'], 'item_id': source['item_id'], 'result_id': published['result_id'],
                       'status': 403, 'error_visible': True, 'content_successes': 0, 'generation_requests': 0,
                       'unchanged_execution_id': latest['execution_id'], 'unchanged_execution_status': latest['status']})
    require(actual == {**{key: expected[key] for key in ('run_id', 'engine_id', 'tenant_id', 'principal_id')},
                       'browser_phase': 'withdrawn', 'models': models},
            'FTP withdrawal browser must reject all original models without content or new execution')


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


def save_completed_phase(report, phase):
    # Persist only the existing suite report, never a client, credential or raw
    # failure response. A checkpoint is partial evidence, not a passing verdict.
    Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'), 'ftp-consumer-flow-progress.json').write_text(
        json.dumps({'completed_phase': phase, 'observations': report}, ensure_ascii=False, indent=2))


def run_profile(client, tenant, engine, samples, model_samples, timeout, source_type):
    report = run(client, tenant, engine, samples, model_samples, timeout, source_type)
    report['ifc_product_profile'] = verify_ifc_product_profile(Path(__file__).resolve().parents[2],
        Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR')))
    save_completed_phase(report, 'source_facts_and_ifc_profile')
    issuer = SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'),
                                   SUPPORT.required_environment('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'), min(timeout, 30))
    authorization = ModelSourceAuthorization(issuer, client, report)
    try:
        report['source_authorizations'] = [authorization.grant(model) for model in report['models']]
        report['browser'] = run_browser(report)
        save_completed_phase(report, 'browser_generation_cache_and_source_downloads')
        report['physical_model_storage'] = verify_owned_model_storage(client, report)
        save_completed_phase(report, 'physical_model_storage')
        report['failed_generation'] = verify_failed_generation(client, report, timeout)
        save_completed_phase(report, 'failed_generation_preserves_output')
        report['denied_generations'] = []
        report['composition_checks'] = {}
        for source in report['models']:
            if source['format'] == 'ifc':
                continue
            checks = {}
            report['composition_checks'][source['format']] = checks
            checks['old_grant_composition'] = verify_old_grant_composition(client, authorization, report, source, timeout)
            report['denied_generations'].append(checks['old_grant_composition']['denied_generation'])
            checks['restored_before_deny'] = verify_cached_results(client, report, {model['format']: 200 for model in report['models']})
            deny = authorization.deny(source)
            checks['source_deny_id'] = deny
            checks['deny_cached_content'] = verify_cached_results(client, report, {model['format']: 403 if model['format'] == source['format'] else 200 for model in report['models']})
            checks['composition_rules'] = verify_composition_rules(client, authorization, report, source, deny, timeout)
            checks['released_cached_content'] = verify_cached_results(client, report, {model['format']: 200 for model in report['models']})
            save_completed_phase(report, source['format'] + '_composition_rules_and_restoration')
    finally:
        authorization.cleanup()
    report['revoked_cached_content'] = verify_cached_results(client, report, {model['format']: 403 for model in report['models']})
    report['denied_generations'].extend(verify_denied_generation(client, report, model['format'], 'source_withdrawn')
                                        for model in report['models'])
    save_completed_phase(report, 'source_withdrawal_denials')
    report['withdrawn_browser'] = run_browser(report, withdrawn=True)
    save_completed_phase(report, 'withdrawn_browser')
    report['deleted_task_definitions'] = delete_owned_task_definitions(client, report)
    save_completed_phase(report, 'task_definition_deletion')
    report['deleted_models'] = delete_owned_models(client, report)
    save_completed_phase(report, 'model_deletion')
    Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'), 'ftp-consumer-flow.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
    return report

def main():
    require(os.environ.get('ADDP_ONLINE_TEST') == '1' and os.environ.get('ADDP_ONLINE_HOSTED') == '1', 'FTP suite requires the Hosted Online entry')
    tenant = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_TEST_TENANT_ID'), 'tenant')
    engine = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_FTP_ENGINE_ID'), 'engine')
    timeout = float(os.environ.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '900'))
    samples = json.loads((Path(SUPPORT.required_environment('ADDP_FTP_ADMISSION_DIR')) / 'ftp-samples.json').read_text())
    model_samples = json.loads((Path(SUPPORT.required_environment('ADDP_FTP_ADMISSION_DIR')) / 'ftp-model-samples.json').read_text())
    client = SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'), SUPPORT.required_environment('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'), min(timeout, 30))
    root_artifacts = Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'))
    report = run_profile(client, tenant, engine, samples, model_samples, timeout, 'ftp')
    nfs_engine = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_NFS_ENGINE_ID'), 'NFS engine')
    require(nfs_engine != engine, 'FTP and NFS must use distinct actual Engines')
    nfs_models = json.loads((Path(SUPPORT.required_environment('ADDP_ONLINE_SECRET_DIR')) / 'nfs-model-samples.json').read_text())
    nfs_artifacts = root_artifacts / 'nfs'
    nfs_artifacts.mkdir()
    try:
        os.environ['ADDP_ONLINE_MODEL_FILE_ENGINE_TYPE'] = 'nfs'
        os.environ['ADDP_ONLINE_ARTIFACT_DIR'] = str(nfs_artifacts)
        report['nfs_profile'] = run_profile(client, tenant, nfs_engine, samples, nfs_models, timeout, 'nfs')
    finally:
        os.environ.pop('ADDP_ONLINE_MODEL_FILE_ENGINE_TYPE', None)
        os.environ['ADDP_ONLINE_ARTIFACT_DIR'] = str(root_artifacts)
    minio_engine = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_MINIO_ENGINE_ID'), 'MinIO engine')
    require(len({engine,nfs_engine,minio_engine}) == 3, 'model profiles require distinct Engines')
    private = Path(SUPPORT.required_environment('ADDP_ONLINE_SECRET_DIR'))
    minio_artifacts = root_artifacts / 'minio'
    minio_artifacts.mkdir()
    try:
        os.environ['ADDP_ONLINE_MODEL_FILE_ENGINE_TYPE'] = 'minio'
        os.environ['ADDP_ONLINE_ARTIFACT_DIR'] = str(minio_artifacts)
        report['minio_profile'] = run_profile(client, tenant, minio_engine,
            json.loads((private/'minio-samples.json').read_text()),
            json.loads((private/'minio-model-samples.json').read_text()),timeout,'minio')
    finally:
        os.environ.pop('ADDP_ONLINE_MODEL_FILE_ENGINE_TYPE', None)
        os.environ['ADDP_ONLINE_ARTIFACT_DIR'] = str(root_artifacts)
    (root_artifacts / 'ftp-consumer-flow.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))



if __name__ == '__main__':
    try:
        main()
    except (SuiteError, SUPPORT.SuiteError, OSError, ValueError, KeyError) as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
