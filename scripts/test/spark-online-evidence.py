"""Shared physical Spark and HTTP execution evidence for Hosted Online suites."""
import json
import ipaddress
from pathlib import Path
import socket
import re
import subprocess
import time
import urllib.parse
import urllib.request


def require_compute_cluster(cluster_id, catalog_engines, cluster_descriptors):
    """Independent T4 role oracle: computation must not masquerade as storage."""
    if any(engine.get('id') == cluster_id for engine in catalog_engines):
        raise ValueError('Spark compute target must not appear in the storage catalog')
    matches = [engine for engine in cluster_descriptors if engine.get('id') == cluster_id]
    if len(matches) != 1:
        raise ValueError('exactly one selected Spark compute cluster is required')
    cluster = matches[0]
    caps = cluster.get('capabilities')
    if (cluster.get('engine_type') != 'spark' or cluster.get('engine_origin') != 'general'
            or cluster.get('is_builtin') is not False or cluster.get('lifecycle_state') != 'active'
            or cluster.get('connection_status') != 'online' or not isinstance(caps, dict)
            or caps.get('schema_version') != 'engine.capabilities/v1' or caps.get('engine_type') != 'spark'
            or caps.get('engine_family') != 'compute' or caps.get('storage') is not None
            or 'connection_info' in cluster or cluster.get('runtime_endpoint') is not None):
        raise ValueError('Spark cluster must be an online masked compute target without storage')
    return cluster


def loopback_listener_evidence(tables, ports):
    listeners = {port: [] for port in ports}
    for ipv6, text in tables:
        for line in text.splitlines()[1:]:
            fields = line.split()
            if len(fields) < 4 or fields[3] != '0A':
                continue
            encoded, encoded_port = fields[1].split(':')
            port = int(encoded_port, 16)
            if port not in listeners:
                continue
            raw = bytes.fromhex(encoded)
            address = ipaddress.ip_address(socket.inet_ntop(socket.AF_INET6 if ipv6 else socket.AF_INET,
                b''.join(raw[start:start + 4][::-1] for start in range(0, len(raw), 4))))
            canonical = (address.ipv4_mapped or address) if isinstance(address, ipaddress.IPv6Address) else address
            if canonical != ipaddress.ip_address('127.0.0.1'):
                raise ValueError('disposable Spark listener is not bound to the approved loopback address')
            listeners[port].append(str(canonical))
    if any(len(addresses) != 1 for addresses in listeners.values()):
        raise ValueError('each disposable Spark endpoint must have exactly one actual listener')
    return {str(port): addresses[0] for port, addresses in listeners.items()}


def application_snapshot(master_url):
    with urllib.request.urlopen(master_url.rstrip('/') + '/json/', timeout=10) as response:
        master = json.load(response)
    if master.get('status') != 'ALIVE' or master.get('aliveworkers') != 1:
        raise ValueError('disposable Master must have exactly one alive Worker')
    return master


def application_ids(master):
    return {app['id'] for app in master.get('activeapps', []) + master.get('completedapps', [])}


def completed_application(master, before, name):
    added = application_ids(master) - set(before)
    matches = [app for app in master.get('completedapps', []) if app.get('id') in added]
    if (master.get('activeapps') or len(added) != 1 or len(matches) != 1
            or matches[0].get('name') != name or matches[0].get('state') != 'FINISHED'
            or not re.fullmatch(r'app-[0-9]+-[0-9]+', matches[0].get('id', ''))):
        raise ValueError('invocation must finish exactly its new Application and release the Driver')
    return matches[0]


def worker_application_evidence(app_id, suite, container, require_tasks=True):
    if not re.fullmatch(r'app-[0-9]+-[0-9]+', app_id):
        raise ValueError('invalid Standalone Application ID')
    owned = subprocess.run(['docker', 'container', 'inspect', '--format', '{{index .Config.Labels "com.addp.online-fixture"}}', container],
                           check=True, capture_output=True, text=True)
    if owned.stdout.strip() != suite:
        raise ValueError('Worker evidence must belong to this disposable fixture')
    logs = subprocess.run(['docker', 'exec', container, 'bash', '-c',
                          'for file in /opt/spark/work/"$1"/*/stderr; do [ ! -f "$file" ] || cat "$file"; done', '_', app_id],
                         check=True, capture_output=True, text=True)
    tasks = re.findall(r'Finished task ([0-9.]+) in stage ([0-9.]+)', logs.stdout)
    if require_tasks and not tasks:
        raise ValueError('Standalone Application has no completed Executor tasks')
    return {'application_id': app_id, 'worker_container': container, 'completed_tasks': len(tasks)}


def worker_evidence(engine_id, suite, container, master_url, before):
    deadline = time.monotonic() + 30
    master = application_snapshot(master_url)
    # Spark.stop() sends unregister before the Driver exits. Observe the Master
    # finishing that exact invocation, without accepting another cached app.
    while master.get('activeapps') and time.monotonic() < deadline:
        added = application_ids(master) - set(before)
        pending = [app for app in master['activeapps'] if app.get('id') in added]
        if len(added) != 1 or len(pending) != 1 or pending[0].get('name') != f'ADDP-Workflow-Engine-{engine_id}':
            raise ValueError('unexpected active Application while waiting for invocation cleanup')
        time.sleep(.1)
        master = application_snapshot(master_url)
    app = completed_application(master, before, f'ADDP-Workflow-Engine-{engine_id}')
    return dict(worker_application_evidence(app['id'], suite, container), application_name=app['name'], state=app['state'])


def runtime_cleanup_evidence(suite, container):
    owned = subprocess.run(['docker', 'container', 'inspect', '--format', '{{index .Config.Labels "com.addp.online-runtime"}}', container],
                           check=True, capture_output=True, text=True)
    if owned.stdout.strip() != suite:
        raise ValueError('Runtime cleanup evidence must belong to this disposable fixture')
    script = """import json
import ipaddress
from pathlib import Path
import socket
from pathlib import Path
drivers = 0
for process in Path('/proc').glob('[0-9]*'):
    try:
        args = (process / 'cmdline').read_bytes().split(bytes([0]))
    except (FileNotFoundError, ProcessLookupError):
        continue
    if len(args) > 1 and ((Path(args[0].decode()).name.startswith('python') and Path(args[1].decode()).name in ('workflow_driver.py', 'query_driver.py')) or (Path(args[0].decode()).name == 'java' and any(b'org.apache.spark.deploy.SparkSubmit' in item for item in args))):
        drivers += 1
print(json.dumps({'driver_processes': drivers, 'private_directories': len(list(Path('/tmp').glob('addp-spark-query-*')))}))
"""
    deadline = time.monotonic() + 30
    while True:
        result = subprocess.run(['docker', 'exec', container, 'python', '-c', script], check=True, capture_output=True, text=True)
        evidence = json.loads(result.stdout)
        if evidence == {'driver_processes': 0, 'private_directories': 0}:
            return evidence
        if time.monotonic() >= deadline:
            raise ValueError('invocation Driver or private directory remains after both page flows')
        time.sleep(.2)


def runtime_status_evidence(base, execution_id, final_result, tasks, validate_nodes=None, node_types=None):
    snapshots = []
    for _ in range(8):
        with urllib.request.urlopen(base.rstrip('/') + '/api/executions/' + urllib.parse.quote(execution_id), timeout=10) as response:
            snapshots.append(json.load(response))
    for snapshot in snapshots:
        if (snapshot.get('execution_id') != execution_id or snapshot.get('status') != 'success'
                or snapshot.get('progress') != 100 or snapshot.get('result') != final_result
                or len(snapshot.get('task_order', [])) != tasks or len(snapshot.get('all_results', {})) != tasks
                or (node_types is None and any(value.get('type') != 'spark_dataframe' for value in snapshot['all_results'].values()))
                or (node_types is not None and (set(snapshot['all_results']) != set(node_types)
                    or any((value.get('status') != 'success' if node_types[name] == 'save' else value.get('type') != node_types[name])
                           for name, value in snapshot['all_results'].items())))):
            raise ValueError('Runtime status must preserve the exact formal execution and node summaries across HTTP requests')
        if validate_nodes is not None:
            validate_nodes(snapshot['all_results'])
    return {'execution_id': execution_id, 'status': 'success', 'queries': len(snapshots), 'tasks': tasks}
