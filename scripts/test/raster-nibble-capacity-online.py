#!/usr/bin/env python3
"""Isolated measured nibble capacity; correctness and resource evidence stay distinct."""
from __future__ import annotations
import importlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time

raster = importlib.import_module('scripts.test.raster-workflow-online')
capacity = importlib.import_module('scripts.test.raster-workflow-capacity-online')
fixture = importlib.import_module('business.scripts.online-raster-nibble-capacity-fixture')
scan_ids = importlib.import_module('scripts.test.vector-pmtiles-workflow-online').scan_ids
SUITE = fixture.SUITE
POLICY_FIELDS = ('running', 'waiting', 'cache_mib', 'default_tenant_running', 'default_tenant_waiting')


def combined_failure(failure, error, cleanup):
    # Only SuiteError is safe to print; unexpected exceptions may contain credentials.
    describe = lambda value: str(value) if isinstance(value, raster.SuiteError) else type(value).__name__
    return raster.SuiteError(describe(failure)+'; '+cleanup+' failed: '+describe(error))


def definition(locator, parent, name):
    if name not in fixture.targets():
        raise raster.SuiteError('unknown exact nibble capacity target')
    return {'tasks': [
        {'id': 'source', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': locator}},
        {'id': 'nibble', 'operator': 'raster_nibble', 'depends_on': ['source'],
         'params': {'input_raster': {'$ref': 'source'}, 'band': 1, 'target_values': [7]}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['nibble'],
         'params': {'input_raster': {'$ref': 'nibble'}, 'target_parent_locator': parent,
                    'target_name': name, 'write_mode': 'create', 'profile': 'cog', 'blocksize': 512}}]}


def disk_sample(root):
    if root.is_symlink() or not root.is_dir():
        raise raster.SuiteError('owned capacity temporary directory is missing')
    logical = allocated = workspace = count = 0
    for directory in root.iterdir():
        if directory.is_symlink() or not directory.name.startswith('addp-raster-'):
            raise raster.SuiteError('unexpected Runtime temporary entry')
        count += 1
        try:
            paths = list(directory.iterdir())
        except FileNotFoundError:
            continue
        for path in paths:
            try:
                info = path.lstat()
            except FileNotFoundError:
                continue
            if path.is_symlink() or not path.is_file():
                if not path.exists():
                    continue
                raise raster.SuiteError('unexpected capacity workspace entry')
            workspace += info.st_size
            if path.suffix in {'.distance', '.sites'}:
                logical += info.st_size; allocated += info.st_blocks*512
    return {'private_logical_bytes': logical, 'private_allocated_bytes': allocated,
            'workspace_logical_bytes': workspace, 'workspace_count': count}


class Sampler(capacity.Sampler):
    def __init__(self, directory, platform, path, scratch):
        super().__init__(directory, platform, path)
        self.scratch = scratch

    def sample(self):
        super().sample()
        try:
            payload = fixture.command(['sudo', '-n', 'python3', '-m', 'scripts.test.raster-nibble-capacity-online',
                                       '--sample-disk', str(self.scratch), os.environ['ADDP_ONLINE_TEST_RUN_ID']])
        except subprocess.CalledProcessError as error:
            raise raster.SuiteError('capacity disk sampler failed: '+error.stderr[-1000:]) from error
        self.samples[-1]['disk'] = json.loads(payload)

    def finish(self, failure=None):
        try:
            return super().finish()
        except Exception as error:
            if failure is None:
                raise
            raise combined_failure(failure, error, 'capacity sampler shutdown') from failure


def owned_runtime(env, image):
    info = json.loads(fixture.command(['docker', 'inspect', fixture.RUNTIME]))[0]
    labels = info['Config']['Labels']; host = info['HostConfig']
    root = fixture.NFSExport(fixture.PREFIX, fixture.command).owned_root(Path(env['ADDP_ONLINE_SECRET_DIR']), env['ADDP_ONLINE_TEST_RUN_ID'])
    if (labels.get('com.addp.online-runtime') != SUITE
        or labels.get('com.addp.online-run-id') != env['ADDP_ONLINE_TEST_RUN_ID']
        or info['Image'] != image or info['Config']['Cmd'] != ['sh', 'container_entrypoint.sh']
        or host['Memory'] != 2*1024**3 or host['MemorySwap'] != 2*1024**3 or host['NanoCpus'] != 4_000_000_000
        or not info['State']['Running'] or f'TMPDIR={root}/runtime-tmp' not in info['Config']['Env']
        or not any(m['Type'] == 'bind' and m['Source'] == str(root) and m['Destination'] == str(root) for m in info['Mounts'])):
        raise raster.SuiteError('capacity Runtime ownership/image/default entry/limits/mount differ')
    return info, root


def restart_runtime(env, image):
    old, root = owned_runtime(env, image)
    if list((root/'runtime-tmp').iterdir()):
        raise raster.SuiteError('previous Runtime temporary files remain')
    fixture.command(['docker', 'rm', '-fv', fixture.RUNTIME])
    fixture.command(['bash', 'scripts/test/online-hosted-raster-nibble-capacity-gate.sh', '--start-runtime'])
    current, _ = owned_runtime(env, image)
    if old['Id'] == current['Id']:
        raise raster.SuiteError('capacity metrics were not reset with a fresh Runtime')
    return current, root


def physical(env, names):
    result = subprocess.run([sys.executable, 'business/scripts/online-raster-nibble-capacity-fixture.py', 'verify', *names],
                            env=dict(env), text=True, capture_output=True, timeout=600)
    if result.returncode:
        raise raster.SuiteError('independent nibble capacity verification failed: '+result.stderr[-1000:])
    return raster.obj(json.loads(result.stdout), 'capacity physical evidence')


def validate_disk(samples, size, running):
    if not samples or any(not isinstance(s.get('disk'), dict) for s in samples):
        raise raster.SuiteError('capacity disk samples missing')
    if any(type(s['disk'].get(k)) is not int or s['disk'][k] < 0 for s in samples
           for k in ('private_logical_bytes', 'private_allocated_bytes', 'workspace_logical_bytes', 'workspace_count')):
        raise raster.SuiteError('capacity disk measurements invalid')
    peak = max(s['disk']['private_logical_bytes'] for s in samples)
    if not 0 < peak <= running*size**2*32:
        raise raster.SuiteError('capacity private disk absent or exceeds analytical bound')
    if any(samples[-1]['disk'][k] for k in ('private_logical_bytes', 'private_allocated_bytes', 'workspace_logical_bytes', 'workspace_count')):
        raise raster.SuiteError('capacity Runtime temporary disk did not drain')
    return {'private_logical_peak_bytes': peak,
            'private_allocated_peak_bytes': max(s['disk']['private_allocated_bytes'] for s in samples),
            'workspace_logical_peak_bytes': max(s['disk']['workspace_logical_bytes'] for s in samples),
            'private_theoretical_upper_bytes': running*size**2*32,
            'sampling_method': 'host-owned-bind-directory-sampled-with-resource-observation; not continuous peak'}


def validate_scan(scanned, scan_id, parent_id, tenant_id, principal_id, engine_id):
    if (scanned.get('execution_id') != scan_id or scanned.get('parent_execution_id') != parent_id
        or (scanned.get('module'), scanned.get('task_type'), scanned.get('status')) != ('meta', 'scan', 'success')
        or type(scanned.get('tenant_id')) is not int or scanned['tenant_id'] != tenant_id
        or type(scanned.get('triggered_by')) is not int or scanned['triggered_by'] != principal_id
        or type(scanned.get('scan_context', {}).get('engine_id')) is not int
        or scanned['scan_context']['engine_id'] != engine_id):
        raise raster.SuiteError('capacity automatic scan identity/parent/engine differs')
    return {**{key: scanned[key] for key in ('execution_id', 'parent_execution_id', 'tenant_id', 'triggered_by', 'module', 'task_type', 'status')},
            'scan_context': {'engine_id': engine_id}}


def restore_policy(platform, path, original, report, failure):
    try:
        current = platform.request('GET', path, (200,)).payload['policy']
        restored = platform.request('PUT', path, (200,), {
            **{key: original[key] for key in POLICY_FIELDS}, 'version': current['version']}).payload['policy']
        if any(restored[key] != original[key] for key in POLICY_FIELDS):
            raise raster.SuiteError('capacity policy restoration failed')
        report['policy_restored'] = True
    except Exception as error:
        if failure is None:
            raise
        raise combined_failure(failure, error, 'policy restoration') from failure


def run(env):
    tenant_id = raster.positive(int(env['ADDP_ONLINE_TEST_TENANT_ID']), 'tenant')
    if tenant_id <= 1:
        raise raster.SuiteError('capacity forbids default tenant')
    client = raster.GatewayClient(env['GATEWAY_URL'], env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30)
    platform = raster.GatewayClient(env['GATEWAY_URL'], env['ADDP_ONLINE_RASTER_POLICY_PLATFORM_TOKEN'], 30)
    identity = raster.validate_identity(client, tenant_id, required_permissions=raster.EDITOR_PERMISSIONS)
    engines = raster.array(platform.request('GET', '/api/v1/system/platform/engine-raster-policies/engines', (200,)).payload, 'policy engines')
    if len(engines) != 1:
        raise raster.SuiteError('capacity requires one registered Runtime')
    engine = raster.positive(engines[0]['id'], 'Runtime engine')
    path = f'/api/v1/system/platform/engine-raster-policies/{engine}'
    client.request('GET', path, (403,))
    original = platform.request('GET', path, (200,)).payload['policy']
    tenant = raster.GatewayClient(env['GATEWAY_URL'], env['ADDP_ONLINE_RASTER_POLICY_TENANT_TOKEN'], 30)
    tenant.request('GET', path, (403,))
    quota = tenant.request('GET', f'/api/v1/system/tenant/engine-raster-policies/{engine}', (200,)).payload['quota']
    if quota.get('running') is not None or quota.get('waiting') is not None:
        raise raster.SuiteError('capacity tenant must inherit defaults')
    nfs = raster.positive(int(env['ADDP_ONLINE_NIBBLE_CAPACITY_NFS_ENGINE_ID']), 'NFS engine')
    scopes = {name: f'addp://engine/{nfs}/path/{name}?type=file' for name in fixture.SOURCES}
    source_scan = raster.support.wait_for_meta_scan(client, nfs, time.monotonic()+600, scope={'targets': list(scopes.values())})
    items = {name: raster.support.find_fixture_item(client, nfs, name, 'capacity source') for name in fixture.SOURCES}
    sources = {name: scopes[name]+f'&item_id={items[name]["id"]}' for name in items}
    before = {name: client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload for name, item in items.items()}
    image = json.loads(fixture.command(['docker', 'image', 'inspect', env['ADDP_ONLINE_NIBBLE_CAPACITY_RUNTIME_IMAGE']]))[0]['Id']
    evidence = Path(env['ADDP_ONLINE_ARTIFACT_DIR'])
    source_evidence = json.loads((Path(env['ADDP_ONLINE_SECRET_DIR'])/'nibble-capacity-source.json').read_text())
    report = {'schema_version': 'addp.raster-nibble-capacity/v1', 'suite': SUITE, 'result': 'failed',
              'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'identity': identity, 'image_id': image,
              'source': source_evidence, 'sources': sources, 'source_scan_execution_id': source_scan, 'groups': [],
              'timing_method': 'submit-two-through-Develop-terminal; excludes-separate-Meta-wait-and-oracle',
              'capacity_scope': 'one measurement per case; synthetic lattice; not arbitrary imagery or UI certification'}
    names = []; scans = []; previous = {}; hashes = {}
    try:
        for size, density, running in fixture.groups():
            current = platform.request('GET', path, (200,)).payload['policy']
            body = {key: current[key] for key in POLICY_FIELDS}
            body.update(version=current['version'], running=running, waiting=2, cache_mib=64,
                        default_tenant_running=running, default_tenant_waiting=2)
            saved = platform.request('PUT', path, (200,), body).payload['policy']
            container, root = restart_runtime(env, image)
            disk = fixture.require_disk(root, size, running)
            group_names = fixture.target_names(size, density, running)
            source_name = f'classes-{size}-{density}.tif'
            sampler = Sampler(capacity.cgroup_directory(container['State']['Pid']), platform, path, root/'runtime-tmp')
            sampler.start(); started = time.monotonic(); executions = []
            try:
                ids = [raster.submit(client, engine, definition(sources[source_name], f'addp://engine/{nfs}/path/outputs?type=directory', name),
                                     execution_timeout=1800) for name in group_names]
                capacity.wait_applied(platform, path, 64, running, saved['version'])
                for identifier in ids:
                    executions.append(raster.wait_execution(client, 'develop', identifier, 2400))
                elapsed = time.monotonic()-started
            finally:
                try:
                    samples = sampler.finish(sys.exc_info()[1])
                finally:
                    (evidence/(group_names[0]+'-measurements.json')).write_text(json.dumps({
                        'before': sampler.before, 'after': getattr(sampler, 'after', None), 'samples': sampler.samples,
                        'policy_version': saved['version'], 'runtime_container_id': container['Id'], 'disk_preflight': disk}, sort_keys=True))
            metrics = capacity.validate_measurement(samples, sampler.before, sampler.after, 64, running, saved['version'])
            metrics.update(validate_disk(samples, size, running))
            names.extend(group_names); native = physical(env, names)
            if native.get('sources') != source_evidence['sources'] or native.get('runtime_workspaces_removed') is not True:
                raise raster.SuiteError('capacity sources changed or workspace cleanup missing')
            if any(native['targets'].get(name) != fact for name, fact in previous.items()):
                raise raster.SuiteError('capacity previous artifacts changed')
            cases = []
            for name, identifier, execution in zip(group_names, ids, executions):
                target = f'addp://engine/{nfs}/path/outputs/{name}?type=file'
                expectation = fixture.artifact_expectation(size)
                facts, scan = raster.validate_success(execution, [sources[source_name]], target, 'create', expectation, resource_type='file')
                if scan in scans or scan == source_scan:
                    raise raster.SuiteError('capacity automatic scan reused')
                scans.append(scan); scanned = raster.wait_execution(client, 'meta', scan, 600)
                scan_observation = validate_scan(scanned, scan, identifier, tenant_id, int(identity['principal_id']), nfs)
                item = raster.support.find_fixture_item(client, nfs, 'outputs/'+name, 'capacity COG')
                item = client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload
                actual = native['targets'][name]
                if (actual.get('checked_pixels') != size**2 or actual.get('full_pixel_and_mask_verification') is not True
                    or actual.get('exact_integer_pixels_verified') is not True or actual.get('cog_valid') is not True or actual.get('has_overviews') is not True):
                    raise raster.SuiteError('capacity complete physical verification missing')
                artifact = execution['metadata']['result']['final_result']
                sizes = raster.strict_sizes(artifact, actual, item, name)
                raster.validate_target_item(item, expectation, actual)
                lineage = raster.wait_lineage(client, items[source_name]['id'], item['id'], identifier, 120)
                monitor = client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload
                if monitor.get('status') != 'success' or monitor.get('module') != 'develop' or monitor.get('metadata', {}).get('lineage_facts') != facts:
                    raise raster.SuiteError('capacity Monitor status/owner/facts differ')
                key = (size, density)
                if key in hashes and hashes[key] != actual['sha256']:
                    raise raster.SuiteError('capacity output bytes differ by task/concurrency')
                hashes[key] = actual['sha256']
                cases.append({'name': name, 'execution_id': identifier, 'automatic_scan_execution_id': scan,
                              'target_item_id': item['id'], 'size_evidence': sizes, 'physical': actual, 'lineage': lineage,
                              'automatic_scan_observation': scan_observation,
                              'monitor_verified': True, 'owner_facts': facts})
            previous = native['targets']
            report['groups'].append({'size': size, 'density': density, 'running_limit': running, 'cache_mib': 64,
                                     'policy_version': saved['version'], 'runtime_container_id': container['Id'],
                                     'elapsed_seconds': elapsed, 'measurement': metrics, 'disk_preflight': disk, 'cases': cases})
            (evidence/'nibble-capacity-progress.json').write_text(json.dumps(report, sort_keys=True))
        if len(report['groups']) != 8 or len(names) != 16 or len(scans) != 16 or len(hashes) != 4:
            raise raster.SuiteError('capacity matrix incomplete')
        if scan_ids(client) != sorted([source_scan, *scans]):
            raise raster.SuiteError('capacity unexpected scans')
        for name, item in items.items():
            if client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload != before[name]:
                raise raster.SuiteError('capacity source metadata changed')
        report.update(result='passed', automatic_scans=16, source_unchanged=True, previous_artifacts_unchanged=True, physical=native)
    finally:
        restore_policy(platform, path, original, report, sys.exc_info()[1])
    return report


def main():
    if sys.argv[1:2] == ['--sample-disk']:
        import re
        root = Path(sys.argv[2]); run_id = sys.argv[3]
        if (len(sys.argv) != 4 or root.name != 'runtime-tmp' or root.parent.parent != Path('/tmp')
            or not re.fullmatch(r'addp-nibble-capacity-[A-Za-z0-9_]+', root.parent.name)
            or root.resolve() != root or not re.fullmatch(r'nibble-capacity-[0-9]+-[0-9]+', run_id)
            or (root.parent/'.addp-online-owner').is_symlink()
            or (root.parent/'.addp-online-owner').read_text() != run_id):
            raise raster.SuiteError('refusing foreign capacity disk sampling')
        print(json.dumps(disk_sample(root))); return
    report = run(dict(os.environ))
    (Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])/'nibble-capacity.json').write_text(json.dumps(report, sort_keys=True))
    print(json.dumps(report, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print('Nibble capacity failed: '+(str(error) if isinstance(error, raster.SuiteError) else type(error).__name__), file=sys.stderr)
        raise SystemExit(1)
