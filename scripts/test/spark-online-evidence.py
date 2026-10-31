"""Shared physical Spark and HTTP execution evidence for Hosted Online suites."""
import json
import re
import subprocess
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


def worker_evidence(engine_id, suite, container, master_url):
    with urllib.request.urlopen(master_url.rstrip('/') + '/json/', timeout=10) as response:
        master = json.load(response)
    applications = [app for app in master.get('activeapps', []) if app.get('name') == f'ADDP-Workflow-Engine-{engine_id}']
    if len(applications) != 1 or applications[0].get('state') != 'RUNNING' or applications[0].get('cores', 0) < 1 or master.get('aliveworkers') != 1:
        raise ValueError('Formal workflow must allocate cores on the disposable Standalone Worker')
    app_id = applications[0].get('id', '')
    if not re.fullmatch(r'app-[0-9]+-[0-9]+', app_id):
        raise ValueError('invalid Standalone Application ID')
    owned = subprocess.run(['docker', 'container', 'inspect', '--format', '{{index .Config.Labels "com.addp.online-fixture"}}', container],
                           check=True, capture_output=True, text=True)
    if owned.stdout.strip() != suite:
        raise ValueError('Worker evidence must belong to this disposable fixture')
    logs = subprocess.run(['docker', 'exec', container, 'bash', '-c', 'cat /opt/spark/work/"$1"/*/stderr', '_', app_id],
                          check=True, capture_output=True, text=True)
    tasks = re.findall(r'Finished task ([0-9.]+) in stage ([0-9.]+)', logs.stdout)
    if not tasks:
        raise ValueError('Standalone Application has no completed Executor tasks')
    return {'application_id': app_id, 'application_name': applications[0]['name'],
            'worker_container': container, 'completed_tasks': len(tasks), 'cores': applications[0]['cores']}


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
