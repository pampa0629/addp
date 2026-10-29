"""Graph cancellation/expiry acceptance against real Hosted Backends.

Uses two pinned Graph Backends and the existing controlled Copilot transport.
Native graph storage and PostgreSQL are real; model extraction is a fixture.
No unknown effect is reconciled by injecting database state or human evidence.
"""
from importlib import import_module
import json
import os
import sys
from pathlib import Path
import uuid
from urllib.parse import urlsplit

S = import_module('scripts.test.execution-online-support')
GRAPH = import_module('scripts.test.graph-protection-online')
REVIEW = import_module('scripts.test.graph-review-online')
EXTRACTOR = import_module('scripts.test.graph-review-extractor')
require = S.require
PERMISSIONS = set(S.CONTRACT['graph_permissions'])


class GraphFaults:
    def __init__(self, deployment):
        self.deployment, self.keys = deployment, set()
        for name in ('graph-backend', 'graph-backend-peer'):
            row = deployment.inspect(name)
            env = dict(v.split('=', 1) for v in row['Config']['Env'] if '=' in v)
            require(env.get('GRAPH_BUILD_LEASE_SECONDS') == '30' and env.get('GRAPH_BUILD_HEARTBEAT_SECONDS') == '5',
                    'Graph fault profile requires the exact bounded lease settings')
        deployment.inspect('addp-graph-online-review-extractor')

    def bind_client(self, name, client):
        require(name in {'graph-backend', 'graph-backend-peer'}, 'invalid Graph runtime binding')
        row = self.deployment.inspect(name)
        url = urlsplit(client.base_url)
        bindings = row.get('NetworkSettings', {}).get('Ports', {}).get('8186/tcp') or []
        require(url.scheme == 'http' and url.hostname == '127.0.0.1' and not url.username and not url.path
                and any(x.get('HostIp') == '127.0.0.1' and x.get('HostPort') == str(url.port) for x in bindings),
                'Graph direct client is not bound to its pinned Backend')

    def control(self, action, key):
        S.identifier(key)
        require(action in {'arm', 'read', 'release'}, 'unsupported Graph fixture action')
        require(action == 'arm' or key in self.keys, 'Graph fixture call is not owned by this runner')
        if action == 'arm':
            require(key not in self.keys, 'Graph fixture call is already armed')
            self.keys.add(key)  # Retain uncertain arm for finally recovery.
        row = self.deployment.inspect('addp-graph-online-review-extractor')
        program = """import json,sys,urllib.request
req=urllib.request.Request('http://127.0.0.1:8087/fixture/'+sys.argv[1],data=json.dumps({'key':sys.argv[2]}).encode(),headers={'Content-Type':'application/json'},method='POST')
with urllib.request.urlopen(req,timeout=5) as response: print(response.read().decode())
"""
        value = json.loads(self.deployment.command(['docker', 'exec', row['Id'], 'python', '-c', program, action, key]))
        require(type(value) is dict and set(value) == {'entered', 'returned', 'released'}
                and type(value['entered']) is int and type(value['returned']) is int and type(value['released']) is bool
                and 0 <= value['returned'] <= value['entered'] <= 1, 'invalid controlled extraction evidence')
        return value

    def stale_runtime_rejected(self, execution_id):
        S.identifier(execution_id)
        row = self.deployment.inspect('graph-backend-peer')
        # Raw logs remain private and are discarded. An exact execution and a
        # lease rejection are both required, not just absence of later writes.
        output = self.deployment.command(['docker', 'logs', '--since', row['State']['StartedAt'], '--tail', '1000', row['Id']], include_stderr=True)
        require(len(output) <= 4 * 1024 * 1024, 'Graph private log probe exceeded its budget')
        prefixes = (f'graph cancellation remains unresolved for execution {execution_id}:',
                    f'failed to finish graph execution {execution_id}:',
                    f'failed to persist graph execution {execution_id} failure:')
        return any(any(prefix in line for prefix in prefixes)
                   and ('execution lease is no longer current' in line or 'execution lease validity changed' in line)
                   for line in output.splitlines())

    def close(self):
        failures = []
        for key in self.keys:
            try:
                self.control('release', key)
            except Exception:
                failures.append('release')
        try:
            self.deployment.close()
        except Exception:
            failures.append('resume')
        require(not failures, 'Graph controls did not settle; Hosted destruction remains mandatory')


def assert_enqueued(value):
    require(type(value) is dict and set(value) == {'execution_id', 'status'} and value['status'] == 'pending',
            'Graph response did not acknowledge durable pending admission')
    return S.identifier(value['execution_id'])


def assert_cancel(value, execution_id, attempt, running):
    require(type(value) is dict and set(value) == {'execution_id', 'attempt', 'status', 'cancellation_requested'}
            and value['execution_id'] == execution_id and type(value['attempt']) is int and value['attempt'] == attempt
            and value['status'] == ('running' if running else 'cancelled') and value['cancellation_requested'] is True,
            'Graph cancellation confused request acceptance with runtime termination')


def run(client, primary_client, peer_client, reader, tenant, engine_id, deployment, report, checkpoint=lambda: None):
    principal = S.user_identity(client, tenant, PERMISSIONS)
    require(primary_client.token == peer_client.token == client.token, 'pinned Backends changed the request credential')
    S.user_identity(reader, tenant, {'graph.graph.read'})
    report.update(schema_version='addp.graph-execution-online/v1', result='failed', tenant_id=str(tenant),
                  scope='pending-cancel-cross-backend-cancel-expiry-stale-write-restart-fencing', engine_type=GRAPH.engine_type(),
                  controlled_extraction=True, real_model=False, graph_id=None, tasks=[], checks=[],
                  control_cleanup='pending', hosted_destruction='required', reopen_success='blocked_provider_identity')
    faults = GraphFaults(deployment)
    faults.bind_client('graph-backend', primary_client)
    faults.bind_client('graph-backend-peer', peer_client)
    try:
        report['stage'] = 'prepare_graph_materials'
        ontology_id = GRAPH.create_ontology(client)
        GRAPH.create_schema(client, ontology_id)
        target = REVIEW.target_path(GRAPH, engine_id)
        graph = client.request('POST', '/api/v1/graph/graphs', (201,), {
            'name': 'Hosted execution ' + deployment.run_id, 'ontology_id': ontology_id,
            'engine_id': target['engine_id'], 'catalog_path': target, 'index_management': 'managed'}).payload
        gid = S.positive(graph['id'])
        report.update(graph_id=gid, ontology_id=ontology_id)
        root = f'/api/v1/graph/graphs/{gid}'
        checkpoint()
        paths, keys = [], []
        for index in range(3):
            task = client.request('POST', root + '/build/tasks', (201,), {'name': f'Hosted lifecycle {index}'}).payload
            tid = S.positive(task['id'])
            paths.append(root + f'/build/tasks/{tid}')
            key = str(uuid.uuid4())
            keys.append(key)
            faults.control('arm', key)
            REVIEW.upload(client, paths[-1] + '/materials', GRAPH, EXTRACTOR.LIFECYCLE_MATERIAL + key)
            report['tasks'].append({'task_id': tid})
            checkpoint()

        def enqueue(index, caller):
            eid = assert_enqueued(caller.request('POST', paths[index] + '/run', (202,)).payload)
            report['tasks'][index]['execution_id'] = eid
            checkpoint()
            return eid

        def snap(eid):
            return deployment.execution(tenant, eid, 'graph', 'kg_build')

        # Pin which Backend owns each call without interpreting random lease
        # owner strings. A Graph supervisor executes one build at a time.
        with deployment.paused_owner('graph-backend-peer'):
            report['stage'] = 'first_backend_claim'
            first = enqueue(0, primary_client)
            S.wait_for(lambda: faults.control('read', keys[0]), lambda x: x['entered'] == 1,
                       'first Backend never reached the controlled extraction')
            require(snap(first)['status'] == 'running', 'first Graph execution is not running')
        second = enqueue(1, peer_client)
        S.wait_for(lambda: faults.control('read', keys[1]), lambda x: x['entered'] == 1,
                   'peer Backend never reached the controlled extraction')
        third = enqueue(2, client)
        report['stage'] = 'pending_cancel'
        pending = snap(third)
        require(pending['status'] == 'pending' and pending['attempt'] == 0 and pending['started_at'] is None,
                'third Graph execution was not held pending by the two real runtimes')
        response = client.request('POST', paths[2] + '/cancel', (200,), {'execution_id': third, 'attempt': 0}).payload
        assert_cancel(response, third, 0, False)
        stopped = snap(third)
        require(stopped['status'] == 'cancelled' and stopped['attempt'] == 0 and stopped['started_at'] is None
                and stopped['execution_time_ms'] is None and stopped['event_count'] == 0
                and deployment.graph_operations(tenant, gid, third) == []
                and faults.control('read', keys[2])['entered'] == 0, 'unclaimed cancellation fabricated runtime work')
        report['checks'].append('pending_cancel_without_runtime')
        checkpoint()

        # Send to the non-owning Backend while the owning process is paused;
        # an accepted response must still say running, never cancelled.
        with deployment.paused_owner('graph-backend'):
            report['stage'] = 'cross_backend_cancel'
            response = peer_client.request('POST', paths[0] + '/cancel', (202,), {'execution_id': first, 'attempt': 1}).payload
            assert_cancel(response, first, 1, True)
            require(snap(first)['status'] == 'running', 'non-owner cancellation forged a terminal state')
        report['checks'].append('cross_backend_cancel_durable_intent')
        checkpoint()

        with deployment.paused_owner('graph-backend-peer'):
            report['stage'] = 'real_lease_expiry'
            expired = S.wait_for(lambda: snap(second), lambda x: x['expired'] is True or x['status'] == 'failed',
                                'Graph lease did not expire in real time', timeout=60)
            terminal = S.wait_for(lambda: snap(second), lambda x: x['status'] == 'failed',
                                 'remaining Backend did not fail the expired Graph build', timeout=60)
            require(terminal['attempt'] == 1 and terminal['started_at'] and terminal['completed_at'],
                    'expired Graph build was replayed or lost real run times')
            ops = deployment.graph_operations(tenant, gid, second)
            require(ops and any(o['state'] == 'uncertain' for o in ops), 'expiry erased an unresolved external operation')
            faults.control('release', keys[1])
        S.wait_for(lambda: faults.control('read', keys[1]), lambda x: x['returned'] == 1,
                   'old external extraction did not return')
        S.wait_for(lambda: faults.stale_runtime_rejected(second), bool,
                   'old Graph runtime did not prove its lease rejection', timeout=60)
        # Repeated authenticated calls must leave terminal and private holding
        # facts intact, including after the old runtime is resumed.
        for phase in ('late_return', 'process_restarted'):
            report['stage'] = phase
            if phase == 'process_restarted':
                deployment.crash_and_start_graph_peer()
            current = snap(second)
            require(current == terminal, 'old Graph runtime changed terminal state or events')
            current_graph = client.request('GET', root, (200,)).payload
            S.safe_public(current_graph)
            require(current_graph.get('admission', {}).get('has_unresolved_operations') is True,
                    'Graph stopped exposing unresolved effects')
            version = S.positive(current_graph['version'])
            for action in ('reconcile', 'reopen'):
                reader.request('POST', root + '/' + action, (403,), {'version': version})
                client.request('POST', root + '/' + action, (409,), {'version': version})
            client.request('POST', paths[1] + '/rerun', (409,))
            require(faults.control('read', keys[1])['entered'] == 1, 'unknown Graph effect replayed')
        report['checks'].extend(['real_expiry_without_replay', 'old_runtime_terminal_fenced',
                                 'unknown_reconcile_reopen_denied', 'unknown_hold_survives_process_crash'])
        report['result'] = 'passed'
    except Exception as error:
        report.update(result='failed', failure_stage=report.get('stage'), error_type=type(error).__name__)
        raise
    finally:
        primary_failure = sys.exc_info()[0] is not None
        try:
            faults.close()
            report['control_cleanup'] = 'passed'
        except Exception as error:
            report.update(result='failed', control_cleanup='failed', cleanup_error_type=type(error).__name__)
            if not primary_failure:
                raise
        finally:
            # Unknown domain evidence is intentionally not deleted via APIs.
            # The outer Hosted trap must destroy the whole owned deployment.
            checkpoint()
    return report


def main():
    report = {'result': 'failed', 'hosted_destruction': 'required'}
    output = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']) / 'graph-execution.json'
    def checkpoint():
        output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
    try:
        deployment = S.OwnedDeployment()
        require(os.environ.get('ADDP_ONLINE_GRAPH_CASE') == 'execution-lifecycle', 'wrong Graph acceptance profile')
        token = os.environ['ADDP_ONLINE_GRAPH_LIFECYCLE_ACCESS_TOKEN']
        run(S.local_client(os.environ['GATEWAY_URL'], token), S.local_client(os.environ['GRAPH_URL'], token),
            S.local_client(os.environ['ADDP_ONLINE_GRAPH_PEER_URL'], token),
            S.local_client(os.environ['GATEWAY_URL'], os.environ['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN']),
            int(os.environ['ADDP_ONLINE_TEST_TENANT_ID']), int(os.environ['ADDP_ONLINE_TEST_ENGINE_ID']), deployment, report, checkpoint)
        return 0
    except Exception as error:
        report.update(result='failed', error_type=type(error).__name__)
        return 1
    finally:
        checkpoint()
        print(json.dumps(report, sort_keys=True))


if __name__ == '__main__':
    raise SystemExit(main())
