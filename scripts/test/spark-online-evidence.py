"""Shared physical Spark and HTTP execution evidence for Hosted Online suites."""
import json
import re
import subprocess
import urllib.parse
import urllib.request


def worker_evidence(engine_id, suite, container, master_url):
    with urllib.request.urlopen(master_url.rstrip('/') + '/json', timeout=10) as response:
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
