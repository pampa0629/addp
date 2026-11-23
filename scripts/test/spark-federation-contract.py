"""Fixed Spark 3.5 native feasibility probe, not the product authorization layer."""
from decimal import Decimal
import json
from pathlib import Path
import socket
import sys

if len(sys.argv) < 2 or sys.argv[1] != "observe-worker":
    from pyspark.sql import SparkSession
    from pyspark.sql.types import DecimalType, LongType
    from query_guard import QueryValidationError, validate_query


def verify_native_guard(spark):
    class ParserOnlySession:
        version = spark.version
        _jsparkSession = spark._jsparkSession

        def sql(self, *args, **kwargs):
            raise AssertionError('validation must not execute SQL')

    session = ParserOnlySession()
    allowed = [
        ('SELECT 1 AS n', set()),
        ('SELECT o.id, abs(o.amount) FROM pg.public.orders o WHERE o.customer_id >= ?', {'pg'}),
        ('SELECT count(*), sum(amount) FROM pg.public.orders GROUP BY customer_id HAVING count(*) > 0', {'pg'}),
        ('SELECT id FROM pg.public.orders UNION ALL SELECT id FROM mysql.addp_test.customers', {'pg', 'mysql'}),
        ('WITH a AS (SELECT id FROM pg.public.orders), b AS (SELECT * FROM a) SELECT * FROM b', {'pg'}),
        ('SELECT (SELECT max(id) FROM pg.public.orders) AS id', {'pg'}),
        ('SELECT id FROM pg.public.orders WHERE EXISTS (SELECT 1 FROM mysql.addp_test.customers)', {'pg', 'mysql'}),
        ('SELECT id FROM pg.public.orders WHERE customer_id IN (SELECT id FROM mysql.addp_test.customers)', {'pg', 'mysql'}),
        ("SELECT CASE WHEN id IS NULL THEN 0 ELSE CAST(id AS BIGINT) END FROM pg.public.orders ORDER BY id DESC LIMIT 2", {'pg'}),
    ]
    for query, expected in allowed:
        try:
            result = validate_query(session, query, {'pg', 'mysql'})
        except QueryValidationError as error:
            # This is a fixed disposable fixture, never a user query. Native
            # class identities make version-specific admission failures clear.
            plan = spark._jsparkSession.sessionState().sqlParser().parsePlan(query)
            raise AssertionError('supported native fixture rejected: ' + plan.toJSON()) from error
        assert result.sql == query and result.source_catalogs == expected, query
    denied = [
        'CREATE TEMP VIEW forbidden_view AS SELECT 1',
        'DROP TABLE pg.public.orders', 'DELETE FROM pg.public.orders',
        'INSERT INTO pg.public.orders VALUES (1,1,1)',
        'UPDATE pg.public.orders SET customer_id = 99',
        "SET spark.sql.shuffle.partitions=999", 'SHOW DATABASES',
        "ADD JAR '/tmp/forbidden.jar'", "SELECT * FROM parquet.`/tmp/forbidden.parquet`",
        'SELECT * FROM spark_catalog.default.forbidden', 'SELECT * FROM public.orders',
        'SELECT * FROM global_temp.forbidden', 'SELECT * FROM range(3)',
        "SELECT reflect('java.lang.System','getenv','PATH')",
        "SELECT java_method('java.lang.System','getenv','PATH')",
        'SELECT pg.public.custom_function(1)', 'SELECT explode(array(1,2))',
        'WITH unused AS (SELECT * FROM unauthorized.public.orders) SELECT 1',
        "WITH unused AS (SELECT reflect('java.lang.System','getenv','PATH')) SELECT 1",
        'SELECT (SELECT count(*) FROM unauthorized.public.orders)',
        'SELECT 1 WHERE EXISTS (SELECT * FROM unauthorized.public.orders)',
        'SELECT 1 WHERE 1 IN (SELECT id FROM unauthorized.public.orders)',
        'SELECT * FROM (SELECT * FROM unauthorized.public.orders) nested',
        'WITH a AS (SELECT * FROM a) SELECT * FROM a',
        'WITH a AS (SELECT * FROM b), b AS (SELECT 1) SELECT * FROM a',
        'SELECT * FROM pg.public.orders VERSION AS OF 1',
        'SELECT 1; DROP TABLE pg.public.orders',
        'SELECT /*+ REPARTITION(1) */ * FROM pg.public.orders',
    ]
    for query in denied:
        try:
            validate_query(session, query, {'pg', 'mysql'})
        except QueryValidationError as error:
            assert query not in str(error), 'diagnostics must not expose SQL'
        else:
            raise AssertionError('native guard admitted forbidden SQL: ' + query)
    assert not spark.catalog.tableExists('forbidden_view')
    assert spark.conf.get('spark.sql.shuffle.partitions') != '999'
    print('SPARK_FEDERATION_NATIVE_GUARD_PASS commands paths udf tvf nested_cte subqueries parameters no_sql_execution', flush=True)


def bind_source(spark, alias, url, driver):
    # A native connection probe distinguishes credential/transport failures from
    # JDBCTableCatalog's generic NoSuchTable wrapper before trying federation.
    properties = spark._jvm.java.util.Properties()
    properties.setProperty('user', 'reader')
    properties.setProperty('password', 'admission-reader-only')
    jdbc_driver = (spark._jvm.org.postgresql.Driver() if alias == 'pg'
                   else spark._jvm.com.mysql.cj.jdbc.Driver())
    connection = jdbc_driver.connect(url, properties)
    try:
        statement = connection.createStatement()
        try:
            result = statement.executeQuery('SELECT 1')
            try:
                assert result.next() and result.getInt(1) == 1
            finally:
                result.close()
            if alias == 'mysql':
                result = statement.executeQuery("SHOW SESSION STATUS LIKE 'Ssl_cipher'")
                try:
                    assert result.next() and result.getString(2), 'MySQL admission must use TLS'
                finally:
                    result.close()
        finally:
            statement.close()
    finally:
        connection.close()
    # Execution-time SQLConf, never submit command arguments or SQL text.
    prefix = 'spark.sql.catalog.' + alias
    for key, value in {
        '': 'org.apache.spark.sql.execution.datasources.v2.jdbc.JDBCTableCatalog',
        '.url': url, '.driver': driver, '.user': 'reader',
        '.password': 'admission-reader-only',
    }.items():
        spark.conf.set(prefix + key, value)


def _jdbc_properties(spark, user, password):
    properties = spark._jvm.java.util.Properties()
    properties.setProperty('user', user)
    properties.setProperty('password', password)
    return properties


def verify_multicluster():
    """Real production Drivers/HTTP handlers; identity transport is a T2 fixture."""
    from concurrent.futures import ThreadPoolExecutor
    from datetime import datetime, timedelta, timezone
    import os
    import tempfile
    import time
    from unittest.mock import AsyncMock, patch
    from urllib.request import urlopen
    from addp_common.auth import AuthorizationContext
    import api_server
    import query_http
    from uuid import uuid4

    hosts = {21: 'spark-master', 22: 'spark-master-b'}
    workers = {21: 'spark-worker', 22: 'spark-worker-b'}
    engines = {key: {'id': key, 'tenant_id': 7, 'engine_type': 'spark', 'name': 'Cluster ' + str(key),
                    'connection_info': {'host': host, 'port': 7077, 'web_ui_url': 'http://' + host + ':8080'}}
               for key, host in hosts.items()}
    engines[11] = {'id': 11, 'tenant_id': 7, 'engine_type': 'postgresql', 'name': 'Business PostgreSQL',
                   'connection_info': {'host': 'postgres', 'port': 5432, 'database': 'addp_test',
                                       'user': 'reader', 'password': 'admission-reader-only'}}
    context = AuthorizationContext(principal_id=19, principal_type='service_principal',
        token_type='service_access_token', client_id='addp-develop', tenant_id=7,
        expires_at=datetime.now(timezone.utc)+timedelta(hours=1))
    observations = []

    def master(key):
        with urlopen('http://' + hosts[key] + ':8080/json/', timeout=5) as response:
            state = json.load(response)
        assert state['status'] == 'ALIVE' and any(w['state'] == 'ALIVE' for w in state['workers']), state
        return state

    def apps(key, active_only=False):
        state = master(key)
        return {a['id'] for a in state.get('activeapps', []) + ([] if active_only else state.get('completedapps', []))}

    def snapshot():
        return {key: apps(key) for key in hosts}

    def wait(predicate, description):
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            value = predicate()
            if value:
                return value
            time.sleep(.1)
        raise AssertionError(description)

    def new_app(before, key):
        added = wait(lambda: apps(key) - before[key], 'selected Master never observed the invocation')
        assert len(added) == 1, added
        return next(iter(added))

    def finished(key, app):
        folder = Path('/evidence/' + ('worker-a' if key == 21 else 'worker-b')) / app
        def read():
            for path in folder.glob('*/stderr'):
                if 'Finished task' in path.read_text(errors='replace'):
                    return True
            return False
        wait(read, 'selected Worker has no completed task for ' + app)
        wait(lambda: app not in apps(key, True), 'Driver application remains active')
        observations.append({'cluster_id': key, 'master': hosts[key], 'worker': workers[key], 'application_id': app})

    def workflow(key, failing=False):
        query = ('SELECT * FROM nonexistent_invocation_view' if failing else
                 "SELECT id, reflect('java.net.InetAddress', 'getLocalHost') AS worker_host FROM range(0,4,1,2)")
        definition = {'tasks': [{'id': 'clean', 'operator': 'sql', 'params': {'query': "SHOW TABLES LIKE 'private_invocation_view'"}, 'depends_on': []},
            {'id': 'q', 'operator': 'sql', 'params': {'query': query}, 'depends_on': ['clean']},
            {'id': 'v', 'operator': 'create_temp_view', 'params': {'input_df': {'$ref': 'q', 'port': 'default'},
                'view_name': 'private_invocation_view'}, 'depends_on': ['q']},
            {'id': 'again', 'operator': 'sql', 'params': {'query': 'SELECT * FROM private_invocation_view ORDER BY id'}, 'depends_on': ['v']}]}
        with api_server.app.test_client() as client:
            response = client.post('/api/workflow', headers={'Authorization':'Bearer native-fixture'}, json={'engine_id': key, 'workflow_def': definition,
                'runtime': {'tenant_id': 7, 'execution_authorization': {'id': 8, 'effects': ['read']},
                            'spark_cluster': {'execution_id':str(uuid4()), 'expires_at':context.expires_at.isoformat(),
                                              'host':hosts[key], 'port':7077}}})
        assert response.status_code == (500 if failing else 200), response.json
        result = response.json
        if failing:
            assert result['status'] == 'failed' and result['error_code'] == 'SQL_TABLE_OR_VIEW_NOT_FOUND', result
            with api_server.app.test_client() as client:
                terminal = client.get('/api/executions/' + result['execution_id']).json
            assert terminal['error_code'] == result['error_code'], terminal
            assert 'nonexistent_invocation_view' not in json.dumps([result, terminal])
        else:
            assert result['status'] == 'success', result
            assert result['all_results']['clean']['preview_rows'] == [], 'previous invocation view leaked'
            rows = result['final_result']['preview_rows']
            assert [r['id'] for r in rows] == [0, 1, 2, 3], rows
            assert all(r['worker_host'].split('/')[0] == workers[key] for r in rows), rows
        return result

    def grant(auth, execution, identifier, tenant, **kwargs):
        assert auth == '8' and tenant == 7 and identifier in engines
        return {'engine': engines[identifier], 'expires_at': (datetime.now(timezone.utc) + timedelta(minutes=10)).isoformat()}

    headers = {'Authorization': 'Bearer owned-t2-fixture'}
    def prepare(key, counterexample=None):
        from uuid import uuid4
        queries = {
            None: 'SELECT id FROM Business_PostgreSQL.public.orders ORDER BY id',
            'column': 'SELECT nonexistent_test_column FROM Business_PostgreSQL.public.orders',
            'table': 'SELECT id FROM Business_PostgreSQL.public.missing_table',
        }
        body = {'target_engine_id': key, 'source_engine_ids': [11],
                'execution_id': str(uuid4()), 'execution_authorization_id': '8',
                'query': queries[counterexample],
                'language': 'sql', 'options': {'read_only': True, 'limit': 10}}
        with api_server.app.test_client() as client:
            response = client.post('/api/v1/queries', json=body, headers=headers)
        if counterexample == 'table':
            assert response.status_code == 400 and response.json['error_code'] == 'query_source_not_supported', response.json
            return None
        assert response.status_code == 201, response.json
        assert set(response.json) == {'query_id', 'sources', 'engine_read_scope', 'read_set'}, response.json
        return response.json['query_id']

    def execute(query_id):
        with api_server.app.test_client() as client:
            response = client.post('/api/v1/queries/' + query_id + '/execute', data='{}', content_type='application/json', headers=headers)
            assert response.status_code == 200, response.json
            assert response.json['rows'] == [{'id': '-9007199254740993'}, {'id': '9007199254740993'}], response.json
            assert client.delete('/api/v1/queries/' + query_id, headers=headers).status_code == 204

    before_dirs = set(Path(tempfile.gettempdir()).glob('addp-spark-query-*'))
    with patch('api_server.resolve_authorization_context', new=AsyncMock(return_value=context)), \
         patch('query_http.resolve_authorization_context', new=AsyncMock(return_value=context)), \
         patch('query_http.get_execution_engine_access', side_effect=grant):
        for key in (21, 22, 21):
            before = snapshot()
            workflow(key)
            app = new_app(before, key)
            assert apps(43 - key) == before[43 - key], 'workflow was submitted to the other cluster'
            finished(key, app)
        print('SPARK_WORKFLOW_MULTI_CLUSTER_SEQUENCE_PASS a_b_a actual_master worker private_view', flush=True)
        before = snapshot()
        with ThreadPoolExecutor(max_workers=2) as pool:
            list(pool.map(workflow, [21, 22]))
        for key in hosts:
            finished(key, new_app(before, key))
        before = snapshot()
        with ThreadPoolExecutor(max_workers=2) as pool:
            failed = pool.submit(workflow, 21, True)
            passed = pool.submit(workflow, 22)
            failed.result(); passed.result()
        finished(22, new_app(before, 22))
        wait(lambda: not apps(21, True), 'failed workflow left active application')
        print('SPARK_WORKFLOW_MULTI_CLUSTER_CONCURRENT_PASS failure_isolation', flush=True)

        for key in (21, 22, 21):
            before = snapshot()
            handle = prepare(key)
            app = new_app(before, key)
            assert app in apps(key, True)
            assert apps(43 - key) == before[43 - key], 'query was submitted to the other cluster'
            execute(handle)
            finished(key, app)
        before = snapshot()
        with ThreadPoolExecutor(max_workers=2) as pool:
            handles = list(pool.map(prepare, [21, 22]))
        bound = {key: new_app(before, key) for key in hosts}
        with ThreadPoolExecutor(max_workers=2) as pool:
            list(pool.map(execute, handles))
        for key in hosts:
            finished(key, bound[key])
        # Cancelling A and an admitted A query that fails SQL analysis must leave B usable.
        before = snapshot()
        a, b = prepare(21, counterexample='column'), prepare(22)
        app_a, app_b = new_app(before, 21), new_app(before, 22)
        with api_server.app.test_client() as client:
            assert client.delete('/api/v1/queries/' + a, headers=headers).status_code == 204
        wait(lambda: app_a not in apps(21, True), 'cancelled A Driver remains active')
        prepare(21, counterexample='table')
        missing = prepare(21, counterexample='column')
        with api_server.app.test_client() as client:
            failure = client.post('/api/v1/queries/' + missing + '/execute', data='{}',
                                  content_type='application/json', headers=headers)
        assert failure.status_code == 400 and failure.json['error_code'] == 'query_column_not_found', failure.json
        assert 'nonexistent_test_column' not in json.dumps(failure.json), failure.json
        assert app_b in apps(22, True), 'A failure stopped the B Driver'
        execute(b)
        finished(22, app_b)
        assert not query_http._queries
        wait(lambda: not apps(21, True) and not apps(22, True), 'active cluster applications remain')
    assert set(Path(tempfile.gettempdir()).glob('addp-spark-query-*')) == before_dirs, 'private Driver directories remain'
    print('SPARK_QUERY_MULTI_CLUSTER_PASS a_b_a concurrent cancel_failure_isolation same_runtime', flush=True)
    print('SPARK_MULTI_CLUSTER_EVIDENCE ' + json.dumps(observations, sort_keys=True), flush=True)


def observe_worker_processes():
    """Fixture-only /proc evidence, written outside application work folders."""
    import os
    import time
    output = Path('/work/executor-processes.json')
    while True:
        processes = []
        for folder in Path('/proc').iterdir():
            if not folder.name.isdecimal():
                continue
            try:
                args = (folder / 'cmdline').read_bytes().split(b'\0')
                if b'org.apache.spark.executor.CoarseGrainedExecutorBackend' not in args:
                    continue
                app = args[args.index(b'--app-id') + 1].decode()
                stat = (folder / 'stat').read_text().rsplit(')', 1)[1].split()
                processes.append({'pid': int(folder.name), 'start_ticks': stat[19],
                                  'state': stat[0], 'application_id': app})
            except (OSError, ValueError, IndexError):
                continue  # A process can exit during this /proc observation.
        temporary = output.with_suffix('.tmp')
        temporary.write_text(json.dumps({'observed_at': time.time(), 'processes': processes}))
        os.replace(temporary, output)
        time.sleep(.1)


def verify_query_cleanup():
    """Real HTTP handlers and Driver/JVM/Worker; IAM transport remains a T2 fixture."""
    from concurrent.futures import ThreadPoolExecutor
    from datetime import datetime, timedelta, timezone
    import os
    import tempfile
    import time
    from unittest.mock import AsyncMock, patch
    from urllib.request import urlopen
    from uuid import uuid4
    from addp_common.auth import AuthorizationContext
    import api_server
    import query_http

    context = AuthorizationContext(principal_id=19, principal_type='service_principal',
        token_type='service_access_token', client_id='addp-develop', tenant_id=7)
    headers = {'Authorization': 'Bearer owned-t2-fixture'}
    clusters = {21: 'spark-master', 22: 'spark-master-b'}
    engines = {key: {'id': key, 'tenant_id': 7, 'engine_type': 'spark', 'name': str(key),
        'connection_info': {'host': host, 'port': 7077, 'web_ui_url': 'http://' + host + ':8080'}}
        for key, host in clusters.items()}
    engines[11] = {'id': 11, 'tenant_id': 7, 'engine_type': 'postgresql', 'name': 'Business PostgreSQL',
        'connection_info': {'host': 'postgres', 'port': 5432, 'database': 'addp_test',
                            'user': 'reader', 'password': 'admission-reader-only'}}

    def grant(auth, execution, identifier, tenant, **kwargs):
        assert auth == '8' and tenant == 7 and identifier in engines
        return {'engine': engines[identifier], 'expires_at':
            (datetime.now(timezone.utc) + timedelta(minutes=10)).isoformat()}

    def state(host, port=8080):
        with urlopen('http://%s:%s/json/' % (host, port), timeout=3) as response:
            return json.load(response)

    def wait(check, description, seconds=30):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            value = check()
            if value:
                return value
            time.sleep(.1)
        raise AssertionError(description)

    def active(key):
        return {app['id'] for app in state(clusters[key]).get('activeapps', [])}

    def worker():
        s = state('spark-worker', 8081)
        return {'cores': s['coresused'], 'memory_mb': s['memoryused'],
                'applications': sorted({e['appid'] for e in s['executors']})}

    def processes():
        path = Path('/evidence/worker-a/executor-processes.json')
        value = json.loads(path.read_text())
        assert 0 <= time.time() - value['observed_at'] < 3, 'Worker /proc observer is stale'
        return value

    def request(method, path, **kwargs):
        with api_server.app.test_client() as client:
            response = client.open(path, method=method, headers=headers, **kwargs)
            return response.status_code, response.get_json(silent=True)

    def prepare(key, query, seconds=120):
        code, result = request('POST', '/api/v1/queries', json={
            'target_engine_id': key, 'source_engine_ids': [11], 'execution_id': str(uuid4()),
            'execution_authorization_id': '8', 'query': query, 'language': 'sql',
            'options': {'read_only': True, 'limit': 10, 'timeout': int(seconds * 10**9)}})
        assert code == 201, (code, result)
        identifier = result['query_id']
        driver = query_http._queries[identifier]['driver']
        before = active(key)
        # Each phase starts with only its own application on this cluster.
        assert len(before) == 1, before
        return identifier, next(iter(before)), driver

    def execute(identifier):
        return request('POST', '/api/v1/queries/' + identifier + '/execute',
                       data='{}', content_type='application/json')

    def gone(pid):
        try:
            os.kill(pid, 0)
            return False
        except ProcessLookupError:
            return True

    def local_group(group):
        members = []
        for path in Path('/proc').iterdir():
            if not path.name.isdecimal():
                continue
            try:
                fields = (path / 'stat').read_text().rsplit(')', 1)[1].split()
                if int(fields[2]) == group and fields[0] != 'Z':
                    members.append({'pid': int(path.name), 'start_ticks': fields[19]})
            except (OSError, ValueError, IndexError):
                continue
        return members

    def busy(app):
        folder = Path('/evidence/worker-a') / app
        for stderr in folder.glob('*/stderr'):
            data = stderr.read_text(errors='replace')
            if data.rfind('Running task') > data.rfind('Finished task'):
                pids = [p for p in processes()['processes'] if p['application_id'] == app and p['state'] != 'Z']
                if pids:
                    return pids
        return False

    normal = 'SELECT id FROM Business_PostgreSQL.public.orders ORDER BY id'
    # 2^36 combinations, with an aggregate depending on every input. No test
    # sleep/UDF, source-side wait, product guard bypass or writes are needed.
    aliases = ['t%d' % i for i in range(36)]
    sources = ' CROSS JOIN '.join('Business_PostgreSQL.public.orders ' + name for name in aliases)
    expression = '+'.join('CAST(%s.customer_id AS BIGINT)*%d' % (name, 2**i)
                          for i, name in enumerate(aliases))
    slow = 'SELECT sum(sqrt(' + expression + ')) AS total FROM ' + sources
    before_dirs = set(Path(tempfile.gettempdir()).glob('addp-spark-query-*'))
    evidence = []
    with patch.object(query_http, 'resolve_authorization_context', AsyncMock(return_value=context)), \
         patch.object(query_http, 'get_execution_engine_access', side_effect=grant):
        baseline = worker()
        assert baseline == {'cores': 0, 'memory_mb': 0, 'applications': []}, baseline
        try:
            for phase in ('cancel', 'timeout', 'prepare-expiry'):
                sibling, sibling_app, sibling_driver = prepare(22, normal)
                identifier, app, driver = prepare(21, slow if phase != 'prepare-expiry' else normal,
                                                   120 if phase == 'cancel' else 45)
                pid, directory = driver.process.pid, driver.directory.name
                with ThreadPoolExecutor(max_workers=1) as pool:
                    future = None
                    if phase != 'prepare-expiry':
                        future = pool.submit(execute, identifier)
                        pids = wait(lambda: busy(app), 'real Executor never started query tasks: ' + phase)
                        assert not future.done(), 'long query completed before stop trigger'
                    else:
                        pids = wait(lambda: [p for p in processes()['processes'] if p['application_id'] == app],
                                    'prepared Driver has no remote Executor')
                    local_members = local_group(pid)
                    assert len(local_members) >= 2, 'Python Driver and real JVM were not observed'
                    allocated = worker()
                    assert allocated['cores'] > 0 and allocated['memory_mb'] > 0
                    assert allocated['applications'] == [app], allocated
                    triggered_at = time.monotonic()
                    if phase == 'cancel':
                        assert request('DELETE', '/api/v1/queries/' + identifier)[0] == 204
                    else:
                        # Runtime owns the deadline set by the real request.
                        wait(lambda: identifier not in query_http._queries,
                             'Runtime deadline did not release query', seconds=50)
                        assert time.monotonic() >= driver.deadline, 'query failed before its actual timeout'
                        triggered_at = driver.deadline
                    if future is not None:
                        code, result = future.result(timeout=10)
                        assert code != 200 and result['error_code'] in {
                            'query_cancelled', 'query_timeout', 'query_driver_failed'}, (code, result)
                    after = wait(lambda: worker() if worker() == baseline and app not in active(21)
                        and not any(p['application_id'] == app for p in processes()['processes'])
                        and gone(pid) and not local_group(pid) and not Path(directory).exists() else None,
                        'local/remote cleanup did not finish within 30 seconds')
                    elapsed = time.monotonic() - triggered_at
                    assert elapsed <= 30, elapsed
                    assert sibling_app in active(22) and not sibling_driver.closed, 'other cluster was stopped'
                    evidence.append({'phase': phase, 'query_id': identifier, 'application_id': app,
                        'driver_pid': pid, 'local_processes_before': local_members,
                        'executor_processes_before': pids, 'worker_before': allocated,
                        'worker_after': after, 'executor_processes_after': processes(),
                        'local_processes_after': local_group(pid), 'driver_pid_gone': gone(pid),
                        'private_directory_exists': Path(directory).exists(),
                        'master_active_after': sorted(active(21)),
                        'release_seconds': elapsed, 'other_application_active': sibling_app,
                        'work_directory_retained': (Path('/evidence/worker-a') / app).exists()})
                    print('SPARK_QUERY_REMOTE_STOP_CASE ' + json.dumps(evidence[-1], sort_keys=True), flush=True)
                # A new production query must work after every stop, not only
                # after the fixture cluster is torn down and recreated.
                recovery, recovered_app, recovered_driver = prepare(21, normal)
                recovery_code, recovery_result = execute(recovery)
                assert recovery_code == 200 and recovery_result['columns'] == ['id'], recovery_result
                assert [(f['name'], f['type']) for f in recovery_result['fields']] == [('id', 'bigint')]
                assert recovery_result['rows'] == [{'id': '-9007199254740993'}, {'id': '9007199254740993'}]
                wait(lambda: not active(21) and worker() == baseline, 'recovery resources not released')
                sibling_code, sibling_result = execute(sibling)
                assert sibling_code == 200 and sibling_result['rows'] == recovery_result['rows'], sibling_result
                wait(lambda: not active(22), 'other cluster did not release its completed query')
                evidence[-1]['recovery'] = {'query_id': recovery, 'application_id': recovered_app,
                                            'rows': recovery_result['rows']}
                evidence[-1]['other_query_completed'] = {'query_id': sibling, 'application_id': sibling_app,
                                                        'rows': sibling_result['rows']}
        finally:
            for identifier in list(query_http._queries):
                request('DELETE', '/api/v1/queries/' + identifier)
        wait(lambda: not active(21) and not active(22) and worker() == baseline,
             'final owned applications remain before fixture destruction')
    assert set(Path(tempfile.gettempdir()).glob('addp-spark-query-*')) == before_dirs
    print('SPARK_QUERY_REMOTE_CLEANUP_PASS ' + json.dumps({'cases': evidence,
          'scope': 'native T2; fixture identity transport; no IAM/page acceptance'}), flush=True)


def main(phase):
    if phase == "observe-worker":
        observe_worker_processes()
        return
    if phase == "query-cleanup":
        verify_query_cleanup()
        return
    if phase == 'multicluster':
        verify_multicluster()
        return
    if phase == 'driver':
        from query_process import PreparedDriver, QueryDriverError
        request = {
            'master_url': 'spark://spark-master:7077', 'driver_host': 'spark-master',
            'catalogs': [
                {'engine_id': 11, 'engine_type': 'postgresql', 'alias': 'Business_PostgreSQL', 'url': 'jdbc:postgresql://postgres:5432/addp_test',
                 'driver': 'org.postgresql.Driver', 'user': 'reader', 'password': 'admission-reader-only'},
                {'engine_id': 12, 'engine_type': 'mysql', 'alias': 'Business_MySQL', 'url': 'jdbc:mysql://mysql:3306/addp_test?sslMode=REQUIRED',
                 'driver': 'com.mysql.cj.jdbc.Driver', 'user': 'reader', 'password': 'admission-reader-only'},
            ],
            'query': '''SELECT o.id, o.amount, c.`中文``label` AS label
                FROM `Business_PostgreSQL`.` schema.with dot `.` 中文"table`` ` o JOIN `Business_MySQL`.addp_test.`custom``ers` c
                ON o.` customer"id ` = c.`customer``id` WHERE o.` customer"id ` >= ? ORDER BY o.id''',
            'args': [1], 'limit': 10,
        }
        # A single MySQL table can push Top-N into JDBC. The exact-identifier
        # extension must preserve native MySQL null ordering as well as quotes;
        # a cross-source JOIN does not exercise this path.
        for sql, expected in [
            ('SELECT id, label FROM Business_MySQL.addp_test.customers ORDER BY id LIMIT 1',
             [{'id': 1, 'label': '中文  '}]),
            ('SELECT `customer``id` AS id, `中文``label` AS label FROM Business_MySQL.addp_test.`custom``ers` '
             'WHERE `customer``id` >= ? ORDER BY `中文``label` DESC NULLS FIRST LIMIT 1',
             [{'id': 2, 'label': None}]),
        ]:
            mysql_request = dict(request, catalogs=[request['catalogs'][1]], query=sql,
                                 args=[1] if '?' in sql else [])
            mysql_prepared = PreparedDriver([sys.executable, '/addp/query_driver.py'], mysql_request, timeout=120)
            try:
                assert mysql_prepared.facts['engine_read_scope'] == {'engine_ids': [12]}
                assert not mysql_prepared.path.exists()
                mysql_result = mysql_prepared.execute()
                assert mysql_result.get('rows') == expected, mysql_result
            finally:
                mysql_prepared.close()
        print('SPARK_MYSQL_NATIVE_TOP_N_PASS ordinary_order_limit quoted_predicate nullable_order same_prepared_driver', flush=True)
        prepared = PreparedDriver([sys.executable, '/addp/query_driver.py'], request, timeout=120)
        try:
            assert prepared.facts['engine_read_scope'] == {'engine_ids': [11, 12]}, prepared.facts
            paths = prepared.facts['read_set']['paths']
            assert [(p['engine_id'], p['segments'][1]['name'], p['segments'][2]['name']) for p in paths] == [
                (11, ' schema.with dot ', ' 中文"table` '), (12, 'addp_test', 'custom`ers')], paths
            assert not prepared.path.exists(), 'prepare cannot materialize results'
            result = prepared.execute()
        finally:
            prepared.close()
        assert result.get('rows') == [
            {'id': '-9007199254740993', 'amount': '-200.987654321098765432', 'label': None},
            {'id': '9007199254740993', 'amount': '100.123456789012345678', 'label': '中文  '},
        ], result
        assert [(field['name'], field['type']) for field in result['fields']] == [
            ('id', 'bigint'), ('amount', 'decimal'), ('label', 'string')], result
        assert result['fields'][1]['precision'] == 38 and result['fields'][1]['scale'] == 18
        # While the same Driver is waiting for permission, attach a child
        # with a sentinel row. SELECT parent sees both rows, ONLY must see one.
        admin_spark = SparkSession.builder.master('local[1]').appName('ADDP-read-plan-ddl-fixture').getOrCreate()
        admin = admin_spark._jvm.org.postgresql.Driver().connect('jdbc:postgresql://postgres:5432/addp_test',
            _jdbc_properties(admin_spark, 'fixture_admin', 'owned-test-only'))
        try:
            # A source role can put public before pg_catalog. Spark pure
            # built-ins must not become shadowed database aggregates.
            statement = admin.createStatement()
            try:
                statement.execute("CREATE FUNCTION public.read_plan_shadow_sum(numeric,numeric) RETURNS numeric LANGUAGE SQL IMMUTABLE AS 'SELECT 999::numeric'")
                statement.execute("CREATE AGGREGATE public.sum(numeric) (sfunc=public.read_plan_shadow_sum, stype=numeric, initcond='0')")

                statement.execute("CREATE FUNCTION public.read_plan_equal(integer,integer) RETURNS boolean LANGUAGE SQL IMMUTABLE AS 'SELECT true'")
                statement.execute('CREATE OPERATOR public.= (FUNCTION=public.read_plan_equal, LEFTARG=integer, RIGHTARG=integer)')
                statement.execute('ALTER ROLE reader SET search_path=public,pg_catalog')
                statement.execute('SET search_path=public,pg_catalog')
                rs = statement.executeQuery('SELECT sum(amount) FROM public.orders')
                assert rs.next() and str(rs.getBigDecimal(1)) == '999'
                rs.close()
            finally:
                statement.close()
            original = dict(request)
            original['catalogs'][0] = dict(original['catalogs'][0], url=original['catalogs'][0]['url']+'?currentSchema=public')
            request = dict(original, catalogs=[original['catalogs'][0]],
                query='SELECT id FROM Business_PostgreSQL.public.read_plan_parent ORDER BY id', args=[])
            prepared = PreparedDriver([sys.executable, '/addp/query_driver.py'], request, timeout=120)
            try:
                assert prepared.facts['engine_read_scope'] == {'engine_ids': [11]}, prepared.facts
                assert not prepared.path.exists()
                statement = admin.createStatement()
                try:
                    statement.setQueryTimeout(10)
                    statement.execute('ALTER TABLE read_plan_child INHERIT read_plan_parent')
                    rs = statement.executeQuery('SELECT (SELECT count(*) FROM read_plan_parent), (SELECT count(*) FROM ONLY read_plan_parent)')
                    assert rs.next() and rs.getInt(1) == 2 and rs.getInt(2) == 1
                    rs.close()
                finally:
                    statement.close()
                assert prepared.execute()['rows'] == [{'id': 1}], 'concurrent inherited row escaped ONLY boundary'
            finally:
                prepared.close()
            aggregate = dict(original, catalogs=[original['catalogs'][0]],
                query='SELECT sum(amount) AS total FROM Business_PostgreSQL.public.orders', args=[])
            prepared = PreparedDriver([sys.executable, '/addp/query_driver.py'], aggregate, timeout=120)
            try:
                assert prepared.execute()['rows'] == [{'total': '-100.864197532086419754'}], 'source role shadowed a Spark built-in'
            finally:
                prepared.close()
            predicate = dict(original, catalogs=[original['catalogs'][0]],
                query='SELECT id FROM Business_PostgreSQL.public.orders WHERE customer_id=999', args=[])
            prepared = PreparedDriver([sys.executable, '/addp/query_driver.py'], predicate, timeout=120)
            try:
                assert prepared.execute()['rows'] == [], 'source role shadowed an operator in a JDBC predicate'
            finally:
                prepared.close()
            # Initial inheritance and a MySQL View never become complete facts.
            for denied_request in [request, dict(original, catalogs=[original['catalogs'][1]],
                    query='SELECT id FROM Business_MySQL.addp_test.read_plan_view', args=[])]:
                try:
                    PreparedDriver([sys.executable, '/addp/query_driver.py'], denied_request, timeout=120)
                    raise AssertionError('unsupported relation yielded prepared read facts')
                except QueryDriverError as error:
                    assert error.code == 'query_source_not_supported', error.code
        finally:
            admin.close()
            admin_spark.stop()
        request = original
        print('SPARK_READ_PLAN_ONLY_PASS same_driver concurrent_inheritance isolated_table no_extra_reader_privileges unknown_view_refused pinned_builtin_operator_path', flush=True)
        request.update(query='WITH hidden AS (SELECT * FROM unauthorized.public.orders) SELECT 1', args=[])
        try:
            PreparedDriver([sys.executable, '/addp/query_driver.py'], request, timeout=120)
            raise AssertionError('unauthorized source prepared')
        except QueryDriverError as error:
            assert error.code == 'query_source_not_authorized', error.code
        print('SPARK_FEDERATION_PRODUCTION_DRIVER_PASS native_guard_jdbc_join typed_bigint_decimal exact_quoted_names private_process', flush=True)
        return
    if phase == 'supervised':
        from query_process import run_driver
        result = run_driver([sys.executable, __file__, 'child'], {'value': 9007199254740993}, timeout=90)
        assert result['value'] == 9007199254740993
        assert result['distributed'] is True
        assert not Path(result['directory']).exists()
        print('SPARK_FEDERATION_SUPERVISED_DRIVER_PASS real_jvm worker private_stdin cleanup', flush=True)
        return
    if phase == 'child':
        request = json.load(sys.stdin)
        spark = (SparkSession.builder.appName('ADDP-supervised-query-admission')
                 .master('spark://spark-master:7077')
                 .config('spark.driver.host', 'spark-master')
                 .config('spark.executor.memory', '512m').config('spark.cores.max', '2')
                 .config('spark.ui.enabled', 'false').getOrCreate())
        try:
            workers = spark.sparkContext.parallelize([1], 1).map(lambda _: socket.gethostname()).collect()
            rows = spark.sql('SELECT CAST(? AS BIGINT) AS value', args=[request['value']]).collect()
            Path(sys.argv[2]).write_text(json.dumps({'value': rows[0].value,
                'distributed': bool(workers) and all(host != socket.gethostname() for host in workers),
                'directory': str(Path.cwd())}))
        finally:
            spark.stop()
        return
    if phase not in {'query', 'guard', 'fresh'}:
        raise ValueError('unknown admission phase')
    spark = SparkSession.builder.appName('ADDP-federation-admission-' + phase).getOrCreate()
    spark.sparkContext.setLogLevel('ERROR')
    try:
        if phase == 'guard':
            verify_native_guard(spark)
            return
        workers = spark.sparkContext.parallelize([1, 2], 2).map(lambda _: socket.gethostname()).collect()
        assert workers and all(host != socket.gethostname() for host in workers), workers
        if phase == 'fresh':
            assert spark.conf.get('spark.sql.catalog.pg', None) is None
            assert spark.conf.get('spark.sql.catalog.mysql', None) is None
            assert not spark.catalog.tableExists('admission_previous_query')
            print('SPARK_FEDERATION_FRESH_DRIVER_PASS', flush=True)
            return
        bind_source(spark, 'pg', 'jdbc:postgresql://postgres:5432/addp_test', 'org.postgresql.Driver')
        bind_source(spark, 'mysql', 'jdbc:mysql://mysql:3306/addp_test?sslMode=REQUIRED', 'com.mysql.cj.jdbc.Driver')
        query = '''SELECT o.id, o.amount, c.label
            FROM pg.public.orders o JOIN mysql.addp_test.customers c
            ON o.customer_id = c.id WHERE o.customer_id >= ? ORDER BY o.id'''
        validated = validate_query(spark, query, {'pg', 'mysql'})
        assert validated.source_catalogs == {'pg', 'mysql'}
        result = spark.sql(query, args=[1])
        assert result.schema['id'].dataType == LongType(), result.schema
        assert result.schema['amount'].dataType == DecimalType(38, 18), result.schema
        rows = [tuple(row) for row in result.collect()]
        assert rows == [
            (-9007199254740993, Decimal('-200.987654321098765432'), None),
            (9007199254740993, Decimal('100.123456789012345678'), '中文  '),
        ], rows
        filtered = spark.sql(query, args=[2]).collect()
        assert len(filtered) == 1 and filtered[0].id == -9007199254740993
        result.createOrReplaceTempView('admission_previous_query')
        print('SPARK_FEDERATION_JOIN_PASS worker bigint decimal null trailing_spaces parameters', flush=True)
    finally:
        spark.stop()


if __name__ == '__main__':
    main(sys.argv[1])
