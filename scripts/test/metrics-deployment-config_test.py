import ast
import hashlib
import importlib.util
import ipaddress
import io
import json
import os
import signal
import socket
import ssl
import time
import re
import runpy
import sys
from pathlib import Path
import subprocess
import tempfile
import unittest
import urllib.parse
from unittest import mock
from contextlib import redirect_stdout

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('metrics_config', ROOT / 'scripts/infra/generate-metrics-config.py')
config = importlib.util.module_from_spec(spec)
spec.loader.exec_module(config)
source_spec = importlib.util.spec_from_file_location('addp_postgres_source_config', ROOT / 'scripts/infra/postgres-observation/source.py')
postgres_source = importlib.util.module_from_spec(source_spec)
source_spec.loader.exec_module(postgres_source)
auth_spec = importlib.util.spec_from_file_location('addp_postgres_auth_directory', ROOT / 'scripts/infra/postgres-observation/authentication.py')
postgres_auth = importlib.util.module_from_spec(auth_spec)
auth_spec.loader.exec_module(postgres_auth)

probe_spec = importlib.util.spec_from_file_location('postgres_observation_probe', ROOT / 'scripts/test/postgres-observation-probe.py')
postgres_probe = importlib.util.module_from_spec(probe_spec)
probe_spec.loader.exec_module(postgres_probe)

inventory_spec = importlib.util.spec_from_file_location('metrics_config_inventory', ROOT / 'scripts/utils/deployment-inventory.py')
inventory = importlib.util.module_from_spec(inventory_spec)
inventory_spec.loader.exec_module(inventory)


def compose_process_snapshot(process):
    """Inspect only this test's unreaped process and bounded same-session children."""
    if inventory.platform.system() != 'Linux':
        return {'available': False}
    # Do not reap the session leader while its private group still needs cleanup.
    if process.returncode is not None:
        return {'available': True, 'root_exited': True, 'children_view': 'unavailable'}
    pending, nodes, truncated = [(process.pid, os.getpid(), 0)], [], False
    root_exited = False
    while pending and len(nodes) < 8:
        pid, parent, depth = pending.pop(0)
        def identity():
            fields = (inventory.observer_proc_text(pid, 'state') or '').rpartition(')')[2].split()
            if len(fields) < 20 or not all(re.fullmatch(r'[0-9]+', fields[n]) for n in (1, 2, 3, 19)):
                return None
            return tuple(int(fields[n]) for n in (1, 2, 3, 19))
        before = identity()
        if before is None or before[:3] != (parent, process.pid, process.pid):
            nodes.append({'depth': depth, 'identity': 'unavailable'})
            continue
        facts = inventory.observer_exit_diagnostic({'pid': pid}, 'owned')
        try:
            executable = Path(os.readlink(f'/proc/{pid}/exe')).name
        except OSError:
            executable = ''
        children = inventory.observer_proc_text(pid, 'children')
        if identity() != before:
            nodes.append({'depth': depth, 'identity': 'changed'})
            continue
        nodes.append({'depth': depth, 'executable': executable if executable in ('docker', 'docker-compose') else 'other',
                      'state': facts['proc_state'], 'wait': facts['wait'], 'children': facts['children']})
        if depth == 0:
            root_exited = facts['proc_state'] in ('Z', 'X')
        if children and re.fullmatch(r'[0-9]+(?:\s+[0-9]+)*', children.strip()):
            ids = children.split()
            truncated |= len(ids) > 8 or depth >= 2
            if depth < 2:
                pending.extend((int(child), pid, depth+1) for child in ids[:8])
    return {'available': True, 'root_exited': root_exited, 'nodes': nodes, 'truncated': truncated or bool(pending)}


def run_business_postgres_compose(environment, case):
    """Keep the original config deadline; emit closed diagnostic facts on failure only."""
    if case not in ('prepared', 'missing_auth_directory'):
        raise ValueError('Unknown Compose fixture case')
    command = ['docker', 'compose', '--env-file', '/dev/null', '-f', str(ROOT / 'business/docker-compose.yml'),
               'config', '--format', 'json', 'postgres']
    report = {'case': case, 'outcome': 'spawn_error', 'spawn_seconds': 0, 'wait_seconds': 0,
              'cleanup_seconds': 0, 'reaped': False}
    started = time.monotonic()
    try:
        process = subprocess.Popen(command, env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   text=True, start_new_session=True)
    except OSError:
        report['spawn_seconds'] = round(time.monotonic()-started, 6)
        print('Compose config diagnostic '+json.dumps(report, sort_keys=True), file=sys.stderr, flush=True)
        raise RuntimeError('Compose config could not start') from None
    report['spawn_seconds'] = round(time.monotonic()-started, 6)
    waiting = time.monotonic()
    try:
        stdout, stderr = process.communicate(timeout=15)
        report.update(wait_seconds=round(time.monotonic()-waiting, 6), reaped=True,
                      return_code=process.returncode, stdout_characters=len(stdout), stderr_characters=len(stderr))
        if process.returncode:
            report['outcome'] = 'exit_error'
            print('Compose config diagnostic '+json.dumps(report, sort_keys=True), file=sys.stderr, flush=True)
        return subprocess.CompletedProcess(command, process.returncode, stdout, stderr)
    except BaseException as error:
        report.update(outcome='timeout' if isinstance(error, subprocess.TimeoutExpired) else 'cancelled',
                      wait_seconds=round(time.monotonic()-waiting, 6))
        try:
            report['processes'] = compose_process_snapshot(process)
        except (OSError, ValueError, TypeError, KeyError):
            report['processes'] = {'available': False}
        cleanup = time.monotonic()
        try:
            # Popen created this private session/group before exec; never signal the caller's group.
            try:
                os.killpg(process.pid, signal.SIGKILL)
                report['group_stop'] = 'sent'
            except ProcessLookupError:
                report['group_stop'] = 'absent'
            except OSError:
                report['group_stop'] = 'failed'
            try:
                process.wait(timeout=1)
                report['reaped'] = True
            except (subprocess.TimeoutExpired, OSError):
                pass
        finally:
            report['cleanup_seconds'] = round(time.monotonic()-cleanup, 6)
            print('Compose config diagnostic '+json.dumps(report, sort_keys=True), file=sys.stderr, flush=True)
        if isinstance(error, subprocess.TimeoutExpired):
            # Do not retain or print partial config/stderr through the exception.
            raise subprocess.TimeoutExpired('Business PostgreSQL Compose config', 15) from None
        raise
    finally:
        for stream in (process.stdout, process.stderr):
            if stream is not None:
                stream.close()


class MetricsDeploymentConfigTest(unittest.TestCase):
    def test_python_startup_diagnostics_are_closed_and_bounded(self):
        ns=runpy.run_path(str(ROOT/'scripts/test/python-http-metrics-source.py'))
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'diagnostic'
            for payload,error in ((b'ResolutionImpossible private-token','dependency_conflict'),(b'No matching distribution found private-token','distribution_missing'),(b'x'*65537,'log_over_budget')):
                path.write_bytes(payload)
                result,size=ns['install_diagnostic'](path)
                self.assertEqual((result,size),(error,len(payload)))
            value=ns['startup_record']('imports','failed','module_missing')
            path.write_text(json.dumps(value));self.assertEqual(ns['read_startup'](path),value)
            value['credential']='private-token';path.write_text(json.dumps(value))
            result=ns['read_startup'](path)
            self.assertEqual(result,{'stage':'unknown','status':'unavailable'})
            self.assertNotIn('private-token',json.dumps(result))

    def test_python_cleanup_checks_both_sources_and_preserves_first_failure(self):
        ns=runpy.run_path(str(ROOT/'scripts/test/python-http-metrics-probe.py'))
        with tempfile.TemporaryDirectory() as directory:
            work=Path(directory);source=work/'source';source.mkdir();(source/'discovery.json').write_text('[]')
            first=RuntimeError('first controlled startup failure')
            def compose(*args):
                if args[:3]==('--profile','python-http','up'):raise first
                if args[:2]==('ps','-aq'):return args[-1]+'-owned'
                raise AssertionError(args)
            discovery=mock.Mock()
            with mock.patch.object(ns['run'].__globals__['shutil'],'copy2'),mock.patch.object(ns['run'].__globals__['shutil'],'copytree'),mock.patch.object(ns['run'].__globals__['subprocess'],'check_output',side_effect=['1','0']) as wait:
                with self.assertRaises(RuntimeError) as caught:ns['run']({'WORK':work,'compose':compose,'port':mock.Mock(),'query':mock.Mock(),'eventually':mock.Mock(),'discovery_text':discovery})
            self.assertIs(caught.exception,first)
            self.assertEqual(wait.call_count,2)
            discovery.assert_called_once_with('[]')

    def test_python_bootstrap_reports_failed_substage_without_install_or_private_text(self):
        ns=runpy.run_path(str(ROOT/'scripts/test/python-http-metrics-source.py'))
        for failing in ('copy','dependencies','imports','configuration','metrics','http_ready','identity'):
            with self.subTest(stage=failing),tempfile.TemporaryDirectory() as directory:
                root=Path(directory)
                def path(value):return root/'install.log' if value=='/tmp/install.log' else root
                def produce(index,source,stage):
                    if failing!='imports':stage(failing)
                    raise ModuleNotFoundError('private-token')
                run=mock.Mock(return_value=subprocess.CompletedProcess([],1 if failing=='dependencies' else 0))
                copy=mock.Mock(side_effect=PermissionError('private-token') if failing=='copy' else None)
                globals_=ns['main'].__globals__
                with mock.patch.dict(globals_,{'Path':path,'produce':produce}),mock.patch.object(globals_['shutil'],'copytree',copy),mock.patch.object(globals_['subprocess'],'run',run),mock.patch.dict(os.environ,HTTP_T2_INDEX='1'):
                    self.assertEqual(ns['main'](),1)
                facts=ns['read_startup'](root/'python-1.startup.json')
                self.assertEqual(facts['stage'],failing)
                self.assertEqual(facts['status'],'failed')
                self.assertNotIn('private-token',json.dumps(facts))
                self.assertEqual(run.call_count,0 if failing=='copy' else 1)

    def test_python_cleanup_timeout_and_restore_failure_still_check_second_source(self):
        ns=runpy.run_path(str(ROOT/'scripts/test/python-http-metrics-probe.py'))
        with tempfile.TemporaryDirectory() as directory:
            source=Path(directory)
            compose=mock.Mock(side_effect=['owned-first','owned-second'])
            discovery=mock.Mock(side_effect=RuntimeError('private-token'))
            with mock.patch.object(ns['finish_sources'].__globals__['subprocess'],'check_output',side_effect=[subprocess.TimeoutExpired('private-token',10),'0']) as wait,redirect_stdout(io.StringIO()) as stdout:
                failed=ns['finish_sources'](source,('python-http-agent','python-http-copilot'),compose,discovery,'[]','producer_start')
            self.assertTrue(failed);self.assertEqual(wait.call_count,2)
            self.assertTrue((source/'python-1.stop').exists() and (source/'python-2.stop').exists())
            result=ns['python_evidence'](source)['cleanup']
            self.assertTrue(result['sources']['python_1']['wait_error'])
            self.assertEqual(result['sources']['python_2']['waited'],1)
            self.assertFalse(result['discovery_restored'])
            self.assertNotIn('private-token',stdout.getvalue()+json.dumps(result))

    def test_python_runtime_shutdown_preserves_primary_and_closes_source_after_join_error(self):
        stop=runpy.run_path(str(ROOT/'scripts/test/python-http-metrics-source.py'))['stop_runtime']
        for primary in (True,False):
            server=mock.Mock();thread=mock.Mock();source=mock.Mock()
            thread.join.side_effect=RuntimeError('private-token')
            if primary:stop(server,thread,source,True)
            else:
                with self.assertRaisesRegex(RuntimeError,'python_http_shutdown_failed'):stop(server,thread,source,False)
            source.close.assert_called_once_with()

    def python_traffic(self, index):
        source = ast.parse((ROOT / 'scripts/test/python-http-metrics-source.py').read_text())
        node = next(item for item in source.body if isinstance(item, ast.ClassDef) and item.name == 'HTTPFixtureTraffic')
        namespace = {}
        exec(compile(ast.Module(body=[node], type_ignores=[]), '<actual Python traffic planner>', 'exec'), namespace)
        return namespace['HTTPFixtureTraffic'](index, 1000)

    def test_python_http_traffic_reserves_recovery_within_original_total(self):
        for index, limit, interval in ((1, 45, 4), (2, 15, 12)):
            with self.subTest(index=index):
                traffic = self.python_traffic(index)
                for i in range(limit):
                    self.assertEqual(traffic.take(1000+i*interval, False), ('/items/fixture-id','/slow','/failure','/missing/fixture-id')[i%4])
                    self.assertIsNone(traffic.take(1000+i*interval+interval/2, False))
                self.assertIsNone(traffic.take(1300, False))
                self.assertEqual(traffic.snapshot()['healthy_attempts'], 0)
                for i in range(limit):
                    self.assertIsNotNone(traffic.take(1301+i*interval/2, i == 0))
                    self.assertIsNone(traffic.take(1301+i*interval/2+.1, True))
                self.assertIsNone(traffic.take(1450, True))
                proof = traffic.snapshot()
                self.assertEqual(proof['failure_attempts'], limit)
                self.assertEqual(proof['healthy_attempts'], limit)
                self.assertEqual(proof['phase'], 'healthy')
                self.assertEqual(proof['failure_last_ms'], (limit-1)*interval*1000)
                self.assertEqual(proof['healthy_last_ms'], (301+(limit-1)*interval/2)*1000)

    def test_python_http_phase_switch_does_not_burst_or_replay_missed_slots(self):
        for index in (1,2):
            traffic = self.python_traffic(index)
            self.assertIsNotNone(traffic.take(1000, False))
            self.assertIsNone(traffic.take(1000.001, True))
            self.assertIsNotNone(traffic.take(1200, False))
            self.assertIsNone(traffic.take(1200, True))
            self.assertEqual(traffic.snapshot()['healthy_attempts'], 1)

    def test_initial_scrape_diagnostic_reports_only_bounded_safe_facts(self):
        path = ROOT / 'scripts/test/monitor-metrics-probe.py'
        tree = ast.parse(path.read_text())
        functions = [node for node in tree.body if isinstance(node, ast.FunctionDef)
                     and node.name in {'fixture_scrape_diagnostic','tls_failure_kind'}]
        secret = 'do-not-log-this-credential'
        namespace = {'json': json, 're': re,
                     'query': lambda expression: [] if expression.startswith('addp_fixture') else [{'value': [1, '2']}],
                     'api': lambda _: {'activeTargets': [
                         {'labels': {'job': 'addp_nodes'}, 'health': 'down',
                          'scrapeUrl': 'https://user:' + secret + '@private:9443/metrics',
                          'lastError': 'x509: certificate invalid ' + secret},
                         {'labels': {'job': 'other'}, 'health': 'up', 'lastError': secret}]}}
        exec(compile(ast.Module(body=functions, type_ignores=[]), str(path), 'exec'), namespace)
        output = io.StringIO()
        with redirect_stdout(output):
            namespace['fixture_scrape_diagnostic']()
        text = output.getvalue()
        for forbidden in (secret, 'private:9443', 'scrapeUrl', 'x509:'):
            self.assertNotIn(forbidden, text)
        result = json.loads(text.split('diagnostic ', 1)[1])
        self.assertEqual(result, {'fixture_series_count': 0, 'fixture_value_matches': False,
                                 'targets': [{'health': 'down', 'error_kind': 'tls_unknown'}],
                                 'http_sd_failure_observed': True})

    def source_tls_functions(self,work):
        path=ROOT/'scripts/test/monitor-metrics-probe.py';tree=ast.parse(path.read_text())
        functions=[node for node in tree.body if isinstance(node,ast.FunctionDef) and node.name in {'tls_failure_kind','source_certificate_readiness','finish_public_preflight'}]
        namespace={'WORK':work,'ssl':ssl,'socket':socket,'hashlib':hashlib,'ipaddress':ipaddress,'json':json,'time':time,'re':re,'os':os,'port':lambda *_:'1234','compose':mock.Mock(return_value='')}
        exec(compile(ast.Module(body=functions,type_ignores=[]),str(path),'exec'),namespace)
        return namespace

    def prepare_source_tls_fixture(self,work):
        (work/'source').mkdir();leaf=b'public-test-leaf'
        (work/'source/server.crt').write_text(ssl.DER_cert_to_PEM_cert(leaf))
        (work/'source-server-final.crt').write_text(ssl.DER_cert_to_PEM_cert(leaf))
        (work/'source/discovery.json').write_text(json.dumps([{'targets':['192.0.2.4:9443']}]))
        (work/'source-tls-publication.json').write_text(json.dumps({'atomic_identity_changed':True,'same_second_mtime':True,'prior_leaf_sha256':'a'*64,'expected_leaf_sha256':hashlib.sha256(leaf).hexdigest()}))
        return leaf

    def test_verified_leaf_and_ip_are_distinct_probes_with_one_budget_each(self):
        with tempfile.TemporaryDirectory() as directory:
            work=Path(directory);leaf=self.prepare_source_tls_fixture(work);ns=self.source_tls_functions(work)
            context=mock.Mock(check_hostname=True,verify_mode=ssl.CERT_REQUIRED)
            secured=mock.MagicMock();secured.__enter__.return_value=secured;secured.getpeercert.return_value=leaf
            context.wrap_socket.return_value=secured
            raw=mock.MagicMock();raw.__enter__.return_value=raw
            with mock.patch.object(socket,'create_connection',return_value=raw) as connect,mock.patch.object(time,'monotonic',return_value=0),redirect_stdout(io.StringIO()):
                ns['source_certificate_readiness'](context)
            proof=json.loads((work/'source-tls-proof.json').read_text())
            self.assertTrue(proof['passed']);self.assertEqual(len(proof['checks']),2)
            self.assertEqual([call.kwargs['server_hostname'] for call in context.wrap_socket.call_args_list],['localhost','192.0.2.4'])
            self.assertEqual(connect.call_count,2)
            self.assertTrue(all(call.kwargs['timeout']==5 for call in connect.call_args_list))

    def test_stale_leaf_name_failure_and_expired_budget_stop_without_retry(self):
        for mode in ('stale_leaf','name_rejected','deadline'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as directory:
                work=Path(directory);leaf=self.prepare_source_tls_fixture(work);ns=self.source_tls_functions(work)
                context=mock.Mock(check_hostname=True,verify_mode=ssl.CERT_REQUIRED)
                secured=mock.MagicMock();secured.__enter__.return_value=secured;secured.getpeercert.return_value=b'stale' if mode=='stale_leaf' else leaf
                context.wrap_socket.return_value=secured
                if mode=='name_rejected':context.wrap_socket.side_effect=[secured,ssl.SSLCertVerificationError(1,'IP address mismatch private-credential')]
                raw=mock.MagicMock();raw.__enter__.return_value=raw
                clock=mock.patch.object(time,'monotonic',side_effect=[0,1,6,6]) if mode=='deadline' else mock.patch.object(time,'monotonic',return_value=0)
                output=io.StringIO()
                with mock.patch.object(socket,'create_connection',return_value=raw) as connect,clock,redirect_stdout(output):
                    with self.assertRaisesRegex(AssertionError,'source_tls_readiness_failed'):ns['source_certificate_readiness'](context)
                proof=json.loads((work/'source-tls-proof.json').read_text());self.assertFalse(proof['passed'])
                self.assertEqual(connect.call_count,2 if mode=='name_rejected' else 1)
                self.assertEqual(proof['checks'][-1]['error_kind'],{'stale_leaf':'certificate_fingerprint_mismatch','name_rejected':'certificate_name_mismatch','deadline':'timeout'}[mode])
                self.assertNotIn('private-credential',json.dumps(proof)+output.getvalue())
                if mode=='deadline':raw.settimeout.assert_called_once_with(4)

    def test_certificate_publication_is_vm_local_and_refreshes_restart_bootstrap(self):
        gate=(ROOT/'scripts/test/monitor-metrics-gate.sh').read_text()
        program=gate.split("<<'CERTIFICATE'\n",1)[1].split('\nCERTIFICATE\n',1)[0]
        transfer=gate.split("publication_state=$(compose exec -T metrics-source sh -ec '\n",1)[1].split("\n' _ /tmp/addp-source-tls",1)[0]
        with tempfile.TemporaryDirectory() as directory:
            work=Path(directory);guest=work/'guest';guest.mkdir();old=work/'bootstrap.crt';final=work/'final.crt';proof=work/'proof.json'
            old.write_text(ssl.DER_cert_to_PEM_cert(b'prior'));final.write_text(ssl.DER_cert_to_PEM_cert(b'new'))
            (guest/'server.crt').write_text(old.read_text());before=old.stat().st_ino
            # Model GNU stat in the VM, including equal second-resolution mtimes.
            stat=work/'stat'
            stat.write_text('#!'+sys.executable+'\nimport os,sys\ns=os.stat(sys.argv[-1]);print(str(s.st_dev)+":"+str(s.st_ino)+" 100")\n');stat.chmod(0o755)
            result=subprocess.run(['sh','-ec',transfer,'_',str(guest)],input=final.read_text(),text=True,capture_output=True,check=True,env=dict(os.environ,PATH=str(work)+os.pathsep+os.environ['PATH']))
            self.assertEqual((guest/'server.crt').read_text(),final.read_text())
            self.assertEqual((guest/'server.crt').stat().st_mode & 0o777,0o600)
            self.assertEqual(old.stat().st_ino,before);self.assertEqual(old.read_text(),ssl.DER_cert_to_PEM_cert(b'prior'))
            with mock.patch.object(sys,'argv',['fixture',str(old),str(final),str(proof),result.stdout]):exec(compile(program,'publication','exec'),{})
            result=json.loads(proof.read_text())
            self.assertTrue(result['atomic_identity_changed'] and result['same_second_mtime'])
            self.assertFalse((guest/'server.crt.next').exists())
            # Later source stop/start must initialize the same final certificate.
            final_bytes=final.read_bytes();old.write_bytes(final_bytes)
            self.assertEqual(old.stat().st_ino,before)
            self.assertEqual(old.read_bytes(),(guest/'server.crt').read_bytes())
        self.assertNotIn('ssl_certificate /source/server.crt;',gate)
        self.assertIn('cp "$WORK_DIR/source-server-final.crt" "$WORK_DIR/source/server.crt"',gate)

    def test_public_preflight_stops_after_zero_business_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            work=Path(directory);(work/'source').mkdir();ns=self.source_tls_functions(work)
            with mock.patch.dict(os.environ,ADDP_METRICS_T2_CASE='mtls-preflight'),redirect_stdout(io.StringIO()):
                with self.assertRaises(SystemExit) as result:ns['finish_public_preflight']()
            self.assertEqual(result.exception.code,0)
            traffic=runpy.run_path(str(ROOT/'scripts/test/metrics-http-budget.py'))['request_traffic'](work/'source')
            self.assertEqual(traffic['request_counts'],{'go':0,'python':[0,0],'total':0})
            self.assertTrue(all(value is None for value in traffic['request_phases'].values()))
            ns['compose'].return_value='unexpected-python-container'
            with mock.patch.dict(os.environ,ADDP_METRICS_T2_CASE='mtls-preflight'):
                with self.assertRaisesRegex(AssertionError,'started_python_source'):ns['finish_public_preflight']()

    def test_tls_error_categories_never_return_peer_text(self):
        classify=self.source_tls_functions(Path('/unused'))['tls_failure_kind']
        for text,expected in [('x509: certificate signed by unknown authority private-token','certificate_untrusted'),('x509: certificate has expired or is not yet valid private-token','certificate_time_invalid'),('tls: certificate required private-token','peer_certificate_required'),('remote error: tls: bad certificate private-token','peer_certificate_rejected'),('tls: unknown private-token','tls_unknown')]:
            self.assertEqual(classify(text),expected)

    def cgroup_probe(self, mismatch=False):
        path = ROOT / 'scripts/test/monitor-metrics-probe.py'
        tree = ast.parse(path.read_text())
        functions = [node for node in tree.body if isinstance(node, ast.FunctionDef)
                     and node.name in ('check_self_cgroup_samples', 'cgroup_source_diagnostics')]
        records = [{'instance_id': 'native-'+str(i), 'instance': 'source:'+str(i)} for i in (1, 2, 3)]
        queried = []

        def query(expression):
            queried.append(expression)
            return [{'metric': dict(record, addp_instance_id=record['instance_id'],
                                    kernel_boot_id='10000000-0000-4000-8000-000000000001',
                                    cgroup_id='999' if mismatch else '101', pid_namespace_id='202'),
                     'value': [1, '1']} for record in records]

        compose = mock.Mock(return_value='10000000-0000-4000-8000-000000000001\n101\npid:[202]\n')
        namespace = {'compose': compose, 'query': query, 'math': __import__('math'), 're': re, 'json': json}
        exec(compile(ast.Module(body=functions, type_ignores=[]), str(path), 'exec'), namespace)
        with redirect_stdout(io.StringIO()):
            namespace['check_self_cgroup_samples'](records)
        self.assertEqual(len(queried), 3)
        self.assertTrue(all(expression.startswith('addp_cgroup_') for expression in queried))
        self.assertEqual(compose.call_count, 1)
        self.assertNotIn('/proc/1/', compose.call_args.args[-1])

    def test_self_cgroup_probe_matches_independent_kernel_identity_without_inventing_quota(self):
        self.cgroup_probe()
        source = (ROOT / 'scripts/test/docker-compose.monitor-metrics-t2.yml').read_text()
        self.assertIn('cgroup: private', source)

    def test_self_cgroup_probe_rejects_mismatched_namespace_evidence(self):
        with self.assertRaisesRegex(AssertionError, 'cgroup evidence must match'):
            self.cgroup_probe(mismatch=True)

    def test_cgroup_diagnostic_identifies_missing_source_without_logging_body_or_endpoint(self):
        path = ROOT / 'scripts/test/monitor-metrics-probe.py'
        function = next(node for node in ast.parse(path.read_text()).body
                        if isinstance(node, ast.FunctionDef) and node.name == 'cgroup_source_diagnostics')
        secret = 'do-not-log-private-value'
        records = [{'instance_id': 'native-1', 'instance': 'private.internal:9443',
                    'cgroup_diagnostic': {'self_sample_valid': False, 'root_usage_parse_valid': False,
                                          'core_sched_field_present': True, 'raw': secret}}]
        body = b'# secret '+secret.encode()+b'\nprocess_resident_memory_bytes 4096\n'
        response = mock.MagicMock()
        response.__enter__.return_value.read.return_value = body
        namespace = {'json': json, 're': re, 'urllib': __import__('urllib'), 'source_context': None}
        exec(compile(ast.Module(body=[function], type_ignores=[]), str(path), 'exec'), namespace)
        output = io.StringIO()
        with mock.patch('urllib.request.urlopen', return_value=response) as opened, redirect_stdout(output):
            namespace['cgroup_source_diagnostics'](records, {'addp_cgroup_identity_info': []}, {})
        response.__enter__.return_value.read.assert_called_once_with((256 << 10) + 1)
        self.assertEqual(opened.call_args.kwargs['timeout'], 5)
        text = output.getvalue()
        self.assertNotIn(secret, text)
        self.assertNotIn('private.internal', text)
        result = json.loads(text.split('diagnostic ', 1)[1])['sources'][0]
        self.assertEqual(result['ordinal'], 1)
        self.assertEqual(result['direct_source'], {'state': 'read', 'counts': {'addp_cgroup_identity_info': 0}})
        self.assertTrue(result['sdk']['core_sched_field_present'])

    def resource_window_check(self, work):
        path = ROOT / 'scripts/test/monitor-metrics-probe.py'
        tree = ast.parse(path.read_text())
        function = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'check_resource_windows')
        namespace = {'WORK': work, 'Path': Path, '__file__': str(path), 'os': os, 'subprocess': subprocess}
        exec(compile(ast.Module(body=[function], type_ignores=[]), str(path), 'exec'), namespace)
        return namespace['check_resource_windows']

    def test_cold_resource_window_build_does_not_consume_execution_deadline(self):
        calls = []

        def run(command, **options):
            calls.append((command, options))
            # A cold preparation takes 70s; the eight real checks take 8s.
            elapsed = 70 if command[0] == 'go' else 8
            if elapsed > options['timeout']:
                raise subprocess.TimeoutExpired(command, options['timeout'])
            return subprocess.CompletedProcess(command, 0)

        with tempfile.TemporaryDirectory() as directory, mock.patch.object(subprocess, 'run', side_effect=run):
            work = Path(directory)
            self.resource_window_check(work)()
        self.assertEqual(len(calls), 2)
        self.assertIn('-c', calls[0][0])
        self.assertEqual(calls[0][1]['timeout'], 180)
        self.assertEqual(calls[1][1]['timeout'], 60)
        self.assertEqual(Path(calls[1][0][0]).parent, work)
        self.assertIn('-test.timeout=60s', calls[1][0])
        self.assertIn('-test.count=1', calls[1][0])
        self.assertIn('-test.run=^TestIntegrationMetrics(CPUWindow|ProcessWindow|HTTPWindow|DiskWindow|DiskIOWindow|NetworkWindow|Filesystem|FilesystemInodes)$', calls[1][0])
        self.assertEqual(calls[1][1]['cwd'], ROOT / 'monitor/backend/internal/resourcequery')
        self.assertTrue(all(options['check'] for _, options in calls))
        self.assertEqual(calls[1][1]['env']['ADDP_METRICS_QUERY_INTEGRATION'], '1')

    def test_failed_resource_window_build_never_runs_a_stale_binary(self):
        for failure in [subprocess.CalledProcessError(1, ['go', 'test', '-c']), subprocess.TimeoutExpired(['go', 'test', '-c'], 180)]:
            with self.subTest(failure=type(failure).__name__), tempfile.TemporaryDirectory() as directory:
                work = Path(directory)
                (work / 'resource-windows.test').write_text('stale executable')
                with mock.patch.object(subprocess, 'run', side_effect=failure) as run:
                    with self.assertRaises(type(failure)):
                        self.resource_window_check(work)()
                self.assertEqual(run.call_count, 1)
                self.assertIn('-c', run.call_args.args[0])

    def test_resource_window_execution_timeout_stays_failed_without_retry(self):
        calls = []

        def run(command, **options):
            calls.append(command)
            if len(calls) == 1:
                return subprocess.CompletedProcess(command, 0)
            raise subprocess.TimeoutExpired(command, options['timeout'])

        with tempfile.TemporaryDirectory() as directory, mock.patch.object(subprocess, 'run', side_effect=run):
            with self.assertRaises(subprocess.TimeoutExpired) as error:
                self.resource_window_check(Path(directory))()
        self.assertEqual(error.exception.timeout, 60)
        self.assertEqual(len(calls), 2)

    def test_cold_admission_build_does_not_consume_the_test_execution_deadline(self):
        calls = []

        def run(command, **options):
            calls.append((command, options))
            # A 70-second cold preparation is valid; the actual admission is 1s.
            elapsed = 70 if command[0] == 'go' else 1
            if elapsed > options['timeout']:
                raise subprocess.TimeoutExpired(command, options['timeout'])
            return subprocess.CompletedProcess(command, 0)

        with tempfile.TemporaryDirectory() as directory, mock.patch.object(postgres_probe.subprocess, 'run', side_effect=run):
            work = Path(directory)
            postgres_probe.check_source_admission(work, '19401')
        self.assertEqual(len(calls), 2)
        self.assertIn('-c', calls[0][0])
        self.assertEqual(calls[0][1]['timeout'], 180)
        self.assertEqual(calls[1][1]['timeout'], 45)
        self.assertIn('-test.timeout=30s', calls[1][0])
        self.assertIn('-test.run=^TestIntegrationMetricsPostgreSQLAdmission$', calls[1][0])
        self.assertIn('-test.count=1', calls[1][0])
        self.assertTrue(all(options['check'] for _, options in calls))
        self.assertEqual(calls[1][1]['env']['ADDP_POSTGRES_ADMISSION_ENDPOINT'], 'https://127.0.0.1:19401/metrics')

    def test_failed_admission_build_never_runs_a_stale_test_binary(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'postgres-admission.test').write_text('stale test executable')
            failure = subprocess.CalledProcessError(1, ['go', 'test', '-c'])
            with mock.patch.object(postgres_probe.subprocess, 'run', side_effect=failure) as run:
                with self.assertRaises(subprocess.CalledProcessError):
                    postgres_probe.check_source_admission(work, '19401')
            self.assertEqual(run.call_count, 1)
            self.assertIn('-c', run.call_args.args[0])

    def test_admission_execution_timeout_is_not_retried_or_promoted_to_success(self):
        calls = []

        def run(command, **options):
            calls.append(command)
            if len(calls) == 1:
                return subprocess.CompletedProcess(command, 0)
            raise subprocess.TimeoutExpired(command, options['timeout'])

        with tempfile.TemporaryDirectory() as directory, mock.patch.object(postgres_probe.subprocess, 'run', side_effect=run):
            with self.assertRaises(subprocess.TimeoutExpired) as error:
                postgres_probe.check_source_admission(Path(directory), '19402')
        self.assertEqual(error.exception.timeout, 45)
        self.assertEqual(len(calls), 2)

    def test_stopped_process_waits_for_failure_at_the_consumers_whole_second(self):
        tree = ast.parse((ROOT / 'scripts/test/monitor-metrics-probe.py').read_text())
        function = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'query')
        check = next(node.args[0] for node in ast.walk(tree)
                     if isinstance(node, ast.Call) and isinstance(node.func, ast.Name) and node.func.id == 'eventually'
                     and len(node.args) > 1 and isinstance(node.args[1], ast.Constant)
                     and node.args[1].value == 'stopped real process becomes a failed source')
        clock = mock.Mock()
        clock.time.return_value = 100.576
        requests = []

        def api(path):
            params = urllib.parse.parse_qs(urllib.parse.urlsplit(path).query)
            requests.append(params)
            # The failed scrape at 100.147 is visible now, but not at t=100,
            # the evaluation time used by both stopped-source Go checks.
            failed = 'time' not in params or float(params['time'][0]) >= 100.147
            return {'result': [{'value': [100.576, '0']}] if failed else []}

        namespace = {'api': api, 'urllib': __import__('urllib'), 'math': __import__('math'), 'time': clock}
        exec(compile(ast.Module(body=[function], type_ignores=[]), '<probe query>', 'exec'), namespace)
        ready = eval(compile(ast.Expression(check), '<stopped source precondition>', 'eval'), namespace)
        self.assertFalse(ready())
        self.assertEqual(requests[-1]['time'], ['100'])
        clock.time.return_value = 101.001
        self.assertTrue(ready())
        self.assertEqual(requests[-1]['time'], ['101'])
        namespace['query']('up')
        self.assertNotIn('time', requests[-1])

    def native_cpu_history_check(self, query, name='native_process_cpu_history_ready'):
        path = ROOT / 'scripts/test/monitor-metrics-probe.py'
        tree = ast.parse(path.read_text())
        function = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == name)
        namespace = {'query': query, 'math': __import__('math'), 'time': mock.Mock(time=lambda: 100.9), 'json': json, 'datetime': __import__('datetime')}
        exec(compile(ast.Module(body=[function], type_ignores=[]), str(path), 'exec'), namespace)
        return namespace[name]

    def test_native_cpu_history_accepts_genuine_zero_only_for_all_exact_sources(self):
        records = [{'instance_id': f'native-{i}', 'instance': f'127.0.0.1:{19440+i}'} for i in (1, 2, 3)]
        rows = [{'metric': {'addp_instance_id': r['instance_id'], 'instance': r['instance']}, 'value': [100, '0']} for r in records]
        query = mock.Mock(return_value=rows)
        self.assertTrue(self.native_cpu_history_check(query)(records))
        expression = query.call_args.args[0]
        self.assertIn('count_over_time(process_cpu_seconds_total', expression)
        self.assertIn('[1m])>=4', expression)
        self.assertIn('offset 1m', expression)
        self.assertEqual(query.call_args.kwargs, {'at': 100})

    def test_native_cpu_history_rejects_missing_duplicate_and_replaced_sources(self):
        records = [{'instance_id': f'native-{i}', 'instance': f'127.0.0.1:{19440+i}'} for i in (1, 2, 3)]
        rows = [{'metric': {'addp_instance_id': r['instance_id'], 'instance': r['instance']}} for r in records]
        for response in ([], rows[:2], [rows[0], rows[0], rows[2]],
                         rows[:2]+[{'metric': {'addp_instance_id': 'old-worker', 'instance': records[2]['instance']}}],
                         rows[:2]+[{'metric': {'addp_instance_id': records[2]['instance_id'], 'instance': '127.0.0.1:19999'}}]):
            with self.subTest(response=response):
                self.assertFalse(self.native_cpu_history_check(mock.Mock(return_value=response))(records))

    def test_native_cpu_history_diagnostics_keep_numeric_windows_without_private_labels(self):
        records = [{'instance_id': 'native-3', 'instance': 'private-host:19443', 'started_at': '2026-10-23T05:00:00Z'}]
        query = mock.Mock(side_effect=[
            [{'value': [100, '101.25']}],
            [{'metric': {'addp_probe': 'cpu_window_samples', 'instance': 'private-host:19443'}, 'value': [100, '2']},
             {'metric': {'addp_probe': 'untrusted', 'secret': 'never-log-this'}, 'value': [100, 'not-a-number']}],
        ])
        result = self.native_cpu_history_check(query, 'native_process_history_diagnostics')(records)
        self.assertEqual(result['center_unix'], [101.25])
        self.assertEqual(result['processes'][0]['cpu_window_samples'], [2.0])
        self.assertEqual(result['processes'][0]['cpu_previous_age'], [])
        self.assertNotIn('private-host', json.dumps(result))
        self.assertNotIn('never-log-this', json.dumps(result))
        self.assertEqual(query.call_count, 2)

    def test_http_quality_uses_native_source_and_standard_lifecycle_registration(self):
        probe = (ROOT / 'scripts/test/monitor-metrics-probe.py').read_text()
        selected = re.findall(r"'\^TestIntegrationMetrics\(([^)]+)\)\$'", probe)
        self.assertEqual(len(selected), 1)
        self.assertIn('HTTPWindow', selected[0].split('|'))
        self.assertIn('^TestIntegrationMetricsHTTPRealSource$', probe)
        self.assertIn('^TestIntegrationMetricsHTTPAlertLifecycle$', probe)
        self.assertIn("'ADDP_PROCESS_T2_ROLE': 'backend' if index == 1 else 'worker'", probe)
        gate = (ROOT / 'scripts/test/monitor-metrics-gate.sh').read_text()
        self.assertIn('common/httpmetrics/ common/modulelifecycle/', gate)
        self.assertIn('--case=http-quality) metrics_case=http-quality ;;', gate)
        self.assertIn('export ADDP_METRICS_T2_CASE="$metrics_case"', gate)
        self.assertIn("os.environ.get('ADDP_METRICS_T2_CASE') in ('http-quality', 'process-alerts', 'cgroup-source')", probe)
        pg_scope = re.search(r'(?ms)^if \[\[ "\$metrics_case" == all \|\| "\$metrics_case" == postgres-source \]\]; then\n  export METRICS_T2_POSTGRES_SOURCE_ENABLED=true\n.*?^fi$', gate)
        self.assertIsNotNone(pg_scope)
        self.assertIn('postgres-observation-probe.py', pg_scope.group())
        self.assertIn('postgres_directory_probe', pg_scope.group())
        stub = 'python3(){ printf "probe\\n"; }; postgres_directory_probe(){ printf "directory\\n"; }; metrics_case="$1"\n'
        for case in ('http-quality', 'process-alerts', 'host-resources', 'all', 'postgres-source', 'cgroup-source'):
            completed = subprocess.run(['bash', '-c', stub + pg_scope.group(), '_', case], capture_output=True, text=True, timeout=5)
            self.assertEqual(completed.returncode, 0)
            expected = ['probe'] * pg_scope.group().count('\n  python3 ') + ['directory']
            self.assertEqual(completed.stdout.splitlines(), expected if case in ('all', 'postgres-source') else [])

    def test_metrics_cases_partition_native_and_host_work_without_shortening_full_gate(self):
        probe = ast.parse((ROOT / 'scripts/test/monitor-metrics-probe.py').read_text())
        selector = next(index for index, node in enumerate(probe.body)
                        if isinstance(node, ast.If) and any(isinstance(item, ast.Name)
                        and item.id == 'check_native_process_sources' for item in ast.walk(node)))
        selection = compile(ast.Module(body=probe.body[selector:selector+5], type_ignores=[]), '<metrics case selection>', 'exec')
        for case in ('all', 'http-quality', 'process-alerts', 'host-resources', 'cgroup-source'):
            native = mock.Mock()
            environment = mock.Mock(environ={'ADDP_METRICS_T2_CASE': case})
            python_http = mock.Mock()
            namespace = {'os': environment, 'check_native_process_sources': native, 'check_python_http_sources': python_http, 'prepare_http_test_binaries': mock.Mock(), 'print': mock.Mock()}
            if case in ('http-quality', 'process-alerts', 'cgroup-source'):
                with self.assertRaises(SystemExit) as stopped:
                    exec(selection, namespace)
                self.assertEqual(stopped.exception.code, 0)
            else:
                exec(selection, namespace)
            self.assertEqual(python_http.call_count, 1 if case in ('all','http-quality') else 0)
            self.assertEqual(namespace['prepare_http_test_binaries'].call_count, 1 if case in ('all','http-quality') else 0)
            self.assertEqual(native.call_args_list, {
                'all': [mock.call(), mock.call(process_alerts=True)],
                'http-quality': [mock.call()],
                'process-alerts': [mock.call(process_alerts=True)],
                'host-resources': [],
                'cgroup-source': [mock.call(cgroup_only=True)],
            }[case])
        gate = (ROOT / 'scripts/test/monitor-metrics-gate.sh').read_text()
        self.assertIn("''|--case=all) metrics_case=all ;;", gate)
        self.assertIn('--case=host-resources) metrics_case=host-resources ;;', gate)
        self.assertIn('--case=process-alerts) metrics_case=process-alerts ;;', gate)
        self.assertIn('--case=cgroup-source) metrics_case=cgroup-source ;;', gate)
        self.assertRegex(gate, r'(?s)if \[\[ "\$metrics_case" != host-resources && "\$metrics_case" != mtls-preflight \]\]; then.*?go test -c ./processmetrics.*?\nfi\npython3 "\$ROOT_DIR/scripts/test/monitor-metrics-probe.py"')

    def test_ci_requires_every_isolated_metrics_case_and_preserves_budget(self):
        workflow = (ROOT / '.github/workflows/release-and-t2-gates.yml').read_text()
        cases = re.search(r'^  monitor-metrics-cases:\n(.*?)(?=^  \S|\Z)', workflow, re.M | re.S).group(1)
        self.assertIn('fail-fast: false', cases)
        self.assertIn('case: [postgres-source, http-quality, process-alerts, host-resources, cgroup-source]', cases)
        self.assertIn('timeout-minutes: 20', cases)
        self.assertIn('MONITOR_METRICS_TEST_ARGS: --case=${{ matrix.case }}', cases)
        self.assertEqual(cases.count('run: make test-monitor-metrics\n'), 1)
        result = re.search(r'^  monitor-metrics:\n(.*?)(?=^  \S|\Z)', workflow, re.M | re.S).group(1)
        self.assertIn('name: Monitor metrics infrastructure\n', result)
        self.assertIn('needs: [selection, monitor-metrics-cases]', result)
        self.assertIn("if: always() && needs.selection.result != 'skipped'", result)
        self.assertIn('METRICS_CASES_RESULT: ${{ needs.monitor-metrics-cases.result }}', result)
        self.assertIn('bash scripts/ci/report-selected-gate.sh', result)
        for selected, selection, verification in [('true', 'success', 'success'), ('true', 'success', 'failure'),
                                                     ('true', 'success', 'cancelled'), ('true', 'success', 'skipped'),
                                                     ('true', 'failure', 'skipped'), ('false', 'success', 'skipped')]:
            completed = subprocess.run(['bash', str(ROOT / 'scripts/ci/report-selected-gate.sh'), 'Metrics', selected, selection, verification],
                                       capture_output=True, text=True, timeout=5)
            self.assertEqual(completed.returncode == 0,
                             selection == 'success' and ((selected == 'true' and verification == 'success')
                                                       or (selected == 'false' and verification == 'skipped')))

    def test_postgres_auth_directory_reuses_runtime_and_preserves_managed_policy(self):
        directory = self.directory.resolve() / 'authentication'
        baseline = 'local all all trust\nhost all all 0.0.0.0/0 scram-sha-256\n'
        runtime = postgres_auth.prepare_business(directory, baseline)
        hba = runtime / 'pg_hba.conf'
        original_stat = hba.stat()
        policy = postgres_source.authentication_configuration(baseline, 'business', ['172.20.0.0/16'])
        postgres_auth.replace(directory, baseline, policy)
        self.assertEqual(hba.read_text(), policy)
        self.assertEqual((hba.stat().st_uid, hba.stat().st_gid, hba.stat().st_mode),
                         (original_stat.st_uid, original_stat.st_gid, original_stat.st_mode))
        unchanged = hba.stat()
        self.assertEqual(postgres_auth.prepare_business(directory, baseline), runtime)
        self.assertEqual(hba.stat().st_ino, unchanged.st_ino)
        updated = baseline + '# deployment owner updated ordinary-user policy\n'
        postgres_auth.prepare_business(directory, updated)
        self.assertEqual(hba.read_text(), policy[:-len(baseline)] + updated)
        self.assertEqual(directory.stat().st_mode & 0o777, 0o700)
        self.assertEqual(runtime.stat().st_mode & 0o777, 0o755)
        self.assertEqual((directory / 'owner.json').stat().st_mode & 0o777, 0o600)
        self.assertEqual(hba.stat().st_mode & 0o777, 0o644)

    def test_postgres_auth_directory_rejects_stale_inputs_and_external_drift(self):
        directory = self.directory.resolve() / 'authentication'
        baseline = 'local all all trust\n'
        hba = postgres_auth.prepare_business(directory, baseline) / 'pg_hba.conf'
        policy = postgres_source.authentication_configuration(baseline, 'business', ['127.0.0.1/32'])
        postgres_auth.replace(directory, baseline, policy)
        with self.assertRaises(ValueError):
            postgres_auth.replace(directory, baseline, policy)
        self.assertEqual(hba.read_text(), policy)
        with self.assertRaises(ValueError):
            postgres_auth.replace(directory, policy, policy + '# unowned modification\n')
        hba.write_text(policy + '# external operator drift\n')
        with self.assertRaises(ValueError):
            postgres_auth.prepare_business(directory, baseline)
        self.assertTrue(hba.read_text().endswith('# external operator drift\n'))

    def test_postgres_auth_directory_rejects_foreign_workspace_and_scope(self):
        directory = self.directory.resolve() / 'authentication'
        baseline = 'local all all trust\n'
        hba = postgres_auth.prepare_business(directory, baseline) / 'pg_hba.conf'
        with self.assertRaises(ValueError):
            postgres_auth.prepare_business(directory, baseline, workspace=ROOT / 'other-workspace')
        with self.assertRaises(ValueError):
            postgres_auth.replace(directory, baseline, baseline, workspace=ROOT / 'other-workspace')
        hba.write_text(postgres_source.authentication_configuration(baseline, 'infra', ['127.0.0.1/32']))
        with self.assertRaises(ValueError):
            postgres_auth.prepare_business(directory, baseline)

    def test_postgres_auth_directory_rejects_links_modes_and_lock_contention(self):
        directory = self.directory.resolve() / 'authentication'
        baseline = 'local all all trust\n'
        hba = postgres_auth.prepare_business(directory, baseline) / 'pg_hba.conf'
        with postgres_auth.locked(directory):
            with self.assertRaises(ValueError):
                postgres_auth.prepare_business(directory, baseline)
        hba.chmod(0o666)
        with self.assertRaises(ValueError):
            postgres_auth.prepare_business(directory, baseline)
        hba.chmod(0o644)
        extra = self.directory / 'extra-link'
        os.link(hba, extra)
        with self.assertRaises(ValueError):
            postgres_auth.prepare_business(directory, baseline)
        extra.unlink()
        hba.unlink()
        hba.symlink_to(extra)
        with self.assertRaises(OSError):
            postgres_auth.prepare_business(directory, baseline)
        linked = self.directory.resolve() / 'linked'
        linked.symlink_to(directory, target_is_directory=True)
        with self.assertRaises(ValueError):
            postgres_auth.prepare_business(linked, baseline)

    def test_postgres_auth_directory_rejects_unowned_files_and_invalid_declarations(self):
        directory = self.directory.resolve() / 'authentication'
        directory.mkdir(mode=0o700)
        (directory / 'runtime').mkdir(mode=0o755)
        (directory / 'runtime/pg_hba.conf').write_text('unowned')
        with self.assertRaises(ValueError):
            postgres_auth.prepare_business(directory, 'local all all trust\n')
        for value in ('', 'local all addp_business_observer trust\n', '\x00', '#' * postgres_source.HBA_LIMIT):
            with self.subTest(value=value[:25]), self.assertRaises(ValueError):
                postgres_auth.prepare_business(directory, value)
        for path in ('relative', str(ROOT / '.dev-state/postgres-auth')):
            with self.assertRaises(ValueError):
                postgres_auth.state_directory({'ADDP_POSTGRES_AUTH_STATE_DIR': path})

    def test_postgres_auth_directory_interrupted_template_update_fails_closed(self):
        directory = self.directory.resolve() / 'authentication'
        baseline = 'local all all trust\n'
        hba = postgres_auth.prepare_business(directory, baseline) / 'pg_hba.conf'
        changed = baseline + '# next deployment template\n'
        atomic = postgres_auth._atomic

        def interrupt_owner(path, *args, **kwargs):
            if path.name == 'owner.json':
                raise OSError('controlled fixture interruption')
            return atomic(path, *args, **kwargs)

        with mock.patch.object(postgres_auth, '_atomic', side_effect=interrupt_owner):
            with self.assertRaises(OSError):
                postgres_auth.prepare_business(directory, changed)
        self.assertEqual(hba.read_text(), changed)
        with self.assertRaises(ValueError):
            postgres_auth.prepare_business(directory, changed)
        self.assertEqual(hba.read_text(), changed)

    def test_postgres_auth_directory_standard_entrypoint_uses_generated_mount(self):
        directory = self.directory.resolve() / 'authentication'
        environment = dict(os.environ, ADDP_POSTGRES_AUTH_STATE_DIR=str(directory))
        environment.pop('ADDP_BUSINESS_POSTGRES_AUTH_DIR', None)
        command = [sys.executable, str(ROOT / 'scripts/infra/postgres-observation/authentication.py'), 'prepare-business']
        completed = subprocess.run(command, env=environment, capture_output=True, text=True, timeout=5)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(completed.stdout.strip(), str(directory / 'runtime'))
        environment['ADDP_BUSINESS_POSTGRES_AUTH_DIR'] = str(self.directory)
        refused = subprocess.run(command, env=environment, capture_output=True, text=True, timeout=5)
        self.assertNotEqual(refused.returncode, 0)
        self.assertNotIn(str(self.directory), refused.stderr)
        business = (ROOT / 'business/docker-compose.yml').read_text().split('\n  postgres:\n', 1)[1].split('\n  oracle:', 1)[0]
        self.assertIn('hba_file=/etc/addp-postgres-auth/pg_hba.conf', business)
        self.assertNotIn('./postgres/pg_hba.conf:/etc/postgresql/pg_hba.conf', business)

    def test_business_postgres_compose_mount_is_readonly_and_requires_preparation(self):
        environment = {name: os.environ[name] for name in ('PATH', 'HOME') if name in os.environ}
        environment['ADDP_BUSINESS_POSTGRES_AUTH_DIR'] = str(self.directory.resolve() / 'prepared')
        rendered = run_business_postgres_compose(environment, 'prepared')
        self.assertEqual(rendered.returncode, 0, 'Compose config failed; see safe diagnostic')
        service = json.loads(rendered.stdout)['services']['postgres']
        mounts = [value for value in service['volumes'] if value['target'] == '/etc/addp-postgres-auth']
        self.assertEqual(len(mounts), 1)
        self.assertEqual(mounts[0]['source'], environment['ADDP_BUSINESS_POSTGRES_AUTH_DIR'])
        self.assertTrue(mounts[0]['read_only'])
        # Compose JSON omits the false long-syntax default; true is explicit.
        self.assertFalse(mounts[0].get('bind', {}).get('create_host_path', False))
        self.assertIn('hba_file=/etc/addp-postgres-auth/pg_hba.conf', service['command'])
        environment.pop('ADDP_BUSINESS_POSTGRES_AUTH_DIR')
        rendered = run_business_postgres_compose(environment, 'missing_auth_directory')
        self.assertEqual(rendered.returncode, 0, 'Compose config failed; see safe diagnostic')
        mount = next(value for value in json.loads(rendered.stdout)['services']['postgres']['volumes']
                     if value['target'] == '/etc/addp-postgres-auth')
        self.assertEqual(mount['source'], '/nonexistent/addp-business-postgres-auth')
        self.assertFalse(mount.get('bind', {}).get('create_host_path', False))

    def test_postgres_source_configuration_is_fixed_and_bounded(self):
        source = postgres_source
        infra = source.configuration('infra', 'postgres', 5432, 'a' * 64)
        business = source.configuration('business', 'business-postgres', 5432, 'b' * 64)
        self.assertEqual(infra['collectors'], business['collectors'])
        self.assertNotEqual(infra['target']['data_source_name'], business['target']['data_source_name'])
        self.assertNotIn('name', infra['target'])
        self.assertFalse(infra['target']['enable_ping'])
        self.assertEqual(infra['global']['max_connections'], 1)
        self.assertEqual(infra['global']['min_interval'], '0s')
        for host, port, password in [('127.0.0.1', 5432, 'a' * 64), ('8.8.8.8', 5432, 'a' * 64),
                                     ('postgres/path', 5432, 'a' * 64), ('postgres', True, 'a' * 64),
                                     ('postgres', 5432, 'short')]:
            with self.subTest(host=host, port=port), self.assertRaises(ValueError):
                source.configuration('infra', host, port, password)
        with self.assertRaises(ValueError):
            source.configuration('tenant', 'postgres', 5432, 'a' * 64)

    def test_postgres_authentication_preserves_other_users_and_is_idempotent(self):
        original = '# addp_infra_observer is documented only\nlocal all all trust\nhost all all 0.0.0.0/0 scram-sha-256\nhost "db#name" ordinary_user 10.0.0.0/8 scram-sha-256\n'
        for scope in ('infra', 'business'):
            rendered = postgres_source.authentication_configuration(original, scope, ['172.20.0.0/16', '127.0.0.1/32'])
            self.assertTrue(rendered.endswith(original))
            self.assertEqual(postgres_source.authentication_configuration(rendered, scope, ['127.0.0.1/32', '172.20.0.0/16']), rendered)
            self.assertIn(f'host "postgres" addp_{scope}_observer 172.20.0.0/16 scram-sha-256\n', rendered)
            self.assertIn(f'local all addp_{scope}_observer reject\n', rendered)
            for network in ('0.0.0.0/0', '::/0'):
                self.assertIn(f'host all addp_{scope}_observer {network} reject\n', rendered)
                self.assertIn(f'host replication addp_{scope}_observer {network} reject\n', rendered)
            changed = postgres_source.authentication_configuration(rendered, scope, ['192.168.10.0/24'])
            self.assertTrue(changed.endswith(original))
            self.assertNotIn('172.20.0.0/16', changed)
        literal = postgres_source.authentication_configuration(original, 'infra', ['::1/128', 'fd00::/64'], database='all')
        self.assertIn('host "all" addp_infra_observer ::1/128 scram-sha-256', literal)

    def test_postgres_authentication_rejects_ambiguous_or_excessive_policy(self):
        render = postgres_source.authentication_configuration
        valid = render('local all all trust\n', 'infra', ['172.20.0.0/16'])
        for original in (valid.replace('scram-sha-256', 'trust'), valid.replace('"database":', '"extra":true,"database":'),
                         '# prior operator policy\n'+valid, valid.replace(postgres_source.HBA_END, ''), valid+valid,
                         'local all addp_infra_observer trust\n', 'host all addp_\\\ninfra_observer all trust\n',
                         'host "db#name" addp_infra_observer 10.0.0.0/8 trust\n', 'host "unterminated all all trust\n',
                         '\x00', '#' * postgres_source.HBA_LIMIT):
            with self.subTest(original=original[:50]), self.assertRaises(ValueError):
                render(original, 'infra', ['172.20.0.0/16'])
        for networks in ([], ['0.0.0.0/0'], ['::/0'], ['8.8.8.8/32'], ['169.254.0.0/16'],
                         ['172.20.0.1/16'], ['172.20.0.0/16']*2, ['10.0.0.0/8', '10.1.0.0/16'],
                         ['127.0.0.1'], ['172.20.0.0/16\nlocal all all trust'], [False], ['10.0.0.0/32']*17):
            with self.subTest(networks=networks), self.assertRaises(ValueError):
                render('', 'infra', networks)
        for scope, database in [('tenant', 'postgres'), ('infra', 'postgres\nlocal'), ('infra', '')]:
            with self.assertRaises(ValueError):
                render('', scope, ['172.20.0.0/16'], database=database)
        with self.assertRaises(ValueError):
            render(valid, 'business', ['172.20.0.0/16'])
        with self.assertRaises(ValueError):
            render(valid, 'infra', ['172.20.0.0/16'], database='other')

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='addp-metrics-config-')
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.secret = 'test-prometheus-' + 'a' * 32
        for name in config.FILES:
            (self.directory / name).write_text(self.secret if name == 'prometheus-client-secret' else name)
        self.env = dict(ADDP_METRICS_DEPLOYMENT_DIR=str(self.directory),
                        PROMETHEUS_SERVICE_CLIENT_SECRET=self.secret,
                        PROMETHEUS_SYSTEM_URL='https://system.internal:8443',
                        PROMETHEUS_MONITOR_URL='https://monitor.internal:8443')

    def test_query_native_probe_has_registered_inputs_and_hosted_go_runtime(self):
        gate_spec = importlib.util.spec_from_file_location('metrics_module_gate', ROOT / 'scripts/test/module-gate.py')
        gate_module = importlib.util.module_from_spec(gate_spec)
        sys.modules[gate_spec.name] = gate_module
        gate_spec.loader.exec_module(gate_module)
        script = (ROOT / 'scripts/test/monitor-metrics-gate.sh').read_text()
        inputs = re.search(r'^# ADDP_T2_INPUT_FILES=(.+)$', script, re.M).group(1).split()
        for path in ('common/processmetrics/sample_darwin.go',
                     'scripts/infra/postgres-observation/000001_init.sql',
                     'scripts/infra/postgres-observation/000002_lifecycle.sql',
                     'scripts/infra/postgres-observation/lifecycle.py',
                     'scripts/test/postgres-metrics-lifecycle_test.py',
                     'scripts/test/postgres-lifecycle-probe.py',
                     'business/scripts/stop.sh',
                     'scripts/test/postgres-observation-probe.py',
                     'scripts/infra/postgres-metrics.yml',
                     'scripts/infra/postgres-observation/source.py',
                     'scripts/infra/postgres-observation/source-web.conf',
                     'scripts/dev/process-metrics-launch.py',
                     'scripts/test/dev-process-metrics-plan_test.py',
                     'common/processmetrics/sample_linux.go',
                     'common/config/process_metrics.go',
                     'common/models/process_metrics.go',
                     'common/client/system_service.go',
                     'common/client/system_runtime_instances.go',
                     'common/client/system_observability_identity.go',
                     'common/client/system_engine_deployments.go',
                     'common/models/postgresql_metrics.go',
                     'common/client/system_host_node.go',
                     'common/client/system_host_node_scope.go',
                     'common/models/module_runtime_query.go',
                     'monitor/backend/internal/metricsdiscovery/node_projection.go',
                     'monitor/backend/internal/resourcequery/process.go',
                     'monitor/backend/internal/resourcequery/postgresql_query.go',
                     'monitor/backend/internal/resourcequery/postgresql_integration_test.go',
                     'monitor/backend/internal/resourcequery/client.go',
                     'monitor/backend/internal/resourcequery/device_counter.go',
                     'monitor/backend/internal/resourcequery/disk_test.go',
                     'monitor/backend/internal/resourcequery/disk_io_test.go',
                     'monitor/backend/internal/resourcequery/deleted-or-future.go',
                     'monitor/backend/internal/config/config.go',
                     'monitor/backend/internal/service/resource_observation.go',
                     'monitor/backend/internal/service/resource_observation_test.go',
                     'monitor/backend/internal/service/resource_summary.go',
                     'monitor/backend/internal/service/resource_alert_evaluation.go',
                     'monitor/backend/internal/service/resource_pressure_evaluator.go',
                     'monitor/backend/internal/service/resource_pressure_prometheus_test.go',
                     'monitor/backend/internal/service/resource_alert_lifecycle_prometheus_test.go',
                     'monitor/backend/internal/service/platform_log_retry_postgres_test.go',
                     'monitor/backend/internal/service/platform_alert_notifications.go',
                     'monitor/backend/internal/service/webhook_sender.go',
                     'monitor/backend/internal/models/resource_alert_rule.go',
                     'monitor/backend/internal/models/resource_query_policy.go',
                     'monitor/backend/internal/repository/resource_query_policy.go',
                     'monitor/backend/internal/service/resource_overview.go',
                     'monitor/backend/internal/service/resource_overview_test.go',
                     'monitor/backend/internal/service/resource_overview_prometheus_test.go',
                     'monitor/backend/internal/service/monitoring_target_test.go',
                     'scripts/prod/metrics-query.yml',
                     'scripts/infra/node-metrics-desktop.yml'):
            self.assertTrue(gate_module.gate_input_covers(inputs, path), path)
        workflow = (ROOT / '.github/workflows/release-and-t2-gates.yml').read_text()
        job = re.search(r'^  monitor-metrics-cases:\n(.*?)(?=^  \S|\Z)', workflow, re.M | re.S).group(1)
        self.assertIn('uses: actions/setup-go@', job)
        self.assertIn('go-version-file: common/go.mod', job)
        self.assertLess(job.index('uses: actions/setup-go@'), job.index('run: make test-monitor-metrics'))
        probe = (ROOT / 'scripts/test/monitor-metrics-probe.py').read_text()
        selected = re.findall(r"'\^TestIntegrationMetrics\(([^)]+)\)\$'", probe)
        self.assertEqual(len(selected), 1)
        self.assertTrue({'CPUWindow', 'ProcessWindow', 'DiskWindow', 'DiskIOWindow', 'NetworkWindow',
                         'Filesystem', 'FilesystemInodes'}.issubset(set(selected[0].split('|'))))
        self.assertIn("'^TestIntegrationMetricsResourceOverview$'", probe)
        self.assertEqual(probe.count("'^TestIntegrationMetricsResourcePressureEvidence$'"), 2)
        self.assertIn("'^TestIntegrationMetricsResourceAlertLifecycle$'", probe)
        self.assertIn('postgres', re.search(r'^# ADDP_T2_OWNED_SERVICES=(.+)$', script, re.M).group(1).split(','))

    def test_sole_config_uses_native_platform_oauth_and_independent_tls(self):
        config.render(self.env)
        data = (self.directory / 'prometheus.yml').read_text()
        self.assertNotIn(self.secret, data)
        self.assertNotIn('@@', data)
        self.assertIn('https://system.internal:8443/api/v1/system/oauth/token', data)
        self.assertIn('https://monitor.internal:8443/api/v1/monitor/platform/metrics_discovery', data)
        for value in ('client_id: addp-prometheus', 'context_type: platform', 'audience: addp.api',
                      'refresh_interval: 30s', 'target_limit: 9', 'sample_limit: 20000',
                      'collector.key', 'control-ca.crt', 'source-ca.crt'):
            self.assertIn(value, data)
        config.render(self.env)
        self.assertEqual((self.directory / 'prometheus.yml').read_text(), data)
        self.assertEqual(sorted(p.name for p in self.directory.iterdir()), sorted((*config.FILES, 'prometheus.yml')))

    def test_desktop_loopback_rules_keep_the_original_identity_and_exact_ports(self):
        env = dict(self.env, ADDP_METRICS_DESKTOP_LOOPBACK_PORTS='19102,19091,19101')
        replies = [json.dumps([{'Endpoints': {'docker': {'Host': 'unix:///local/docker.sock'}}}]),
                   json.dumps({'OperatingSystem': 'Docker Desktop', 'OSType': 'linux'})]
        with mock.patch.object(config.platform, 'system', return_value='Darwin'), mock.patch.object(config.subprocess, 'check_output', side_effect=replies):
            config.render(env)
        data = (self.directory / 'prometheus.yml').read_text()
        self.assertIn('target_label: instance', data)
        for port in ('19091', '19101', '19102'):
            self.assertIn('host.docker.internal:' + port, data)
            self.assertIn('127\\\\.0\\\\.0\\\\.1:' + port, data)
        self.assertEqual(data.count('target_label: instance'), 1)
        self.assertEqual(data.count('target_label: __address__'), 3)
        rules = [json.loads(line.strip().removeprefix('regex: ')) for line in data.splitlines()
                 if line.strip().startswith('regex: "127')]
        for address in ('127.0.0.1:19091', '127.0.0.1:19101', '127.0.0.1:19102'):
            self.assertEqual(sum(re.fullmatch(rule, address) is not None for rule in rules), 1)
        for address in ('127.0.0.1:19103', '127.0.0.11:19101', '192.168.1.33:19101',
                        '[::1]:19101', 'host.docker.internal:19101', '127.0.0.1:191010'):
            self.assertFalse(any(re.fullmatch(rule, address) for rule in rules), address)
        self.assertEqual(data.count('job_name: addp_nodes'), 1)
        self.assertNotIn('server_name:', data)
        before = data
        with mock.patch.object(config.platform, 'system', return_value='Darwin'), mock.patch.object(config.subprocess, 'check_output', side_effect=replies):
            config.render(dict(env, ADDP_METRICS_DESKTOP_LOOPBACK_PORTS='19091,19101,19102'))
        self.assertEqual((self.directory / 'prometheus.yml').read_text(), before)
        for ports in ('019091', '0', '65536', '19091\n', '19091:123', '19091,19091',
                      ',19091', '19091,', '19091,,19101', '19091, 19101',
                      ','.join(str(i) for i in range(1, 66))):
            with self.subTest(ports=ports), self.assertRaises(ValueError):
                config.render(dict(env, ADDP_METRICS_DESKTOP_LOOPBACK_PORTS=ports))
            self.assertEqual((self.directory / 'prometheus.yml').read_text(), before)
        with mock.patch.object(config.platform, 'system', return_value='Linux'), self.assertRaises(ValueError): config.render(env)
        with mock.patch.object(config.platform, 'system', return_value='Darwin'), self.assertRaises(ValueError): config.render(dict(env, DOCKER_HOST='tcp://remote:2376'))
        self.assertEqual((self.directory / 'prometheus.yml').read_text(), before)
        config.render(self.env)
        self.assertNotIn('relabel_configs:\n      - source_labels: [__address__]', (self.directory / 'prometheus.yml').read_text())

    def test_desktop_transport_accepts_64_ports_and_rejects_non_desktop_engine(self):
        env = dict(self.env, ADDP_METRICS_DESKTOP_LOOPBACK_PORTS=','.join(str(i) for i in range(1, 65)))
        replies = [json.dumps([{'Endpoints': {'docker': {'Host': 'unix:///local/docker.sock'}}}]),
                   json.dumps({'OperatingSystem': 'Docker Desktop', 'OSType': 'linux'})]
        with mock.patch.object(config.platform, 'system', return_value='Darwin'), mock.patch.object(config.subprocess, 'check_output', side_effect=replies):
            config.render(env)
        before = (self.directory / 'prometheus.yml').read_text()
        self.assertEqual(before.count('target_label: __address__'), 64)
        for info in ({'OperatingSystem': 'Linux', 'OSType': 'linux'},
                     {'OperatingSystem': 'Docker Desktop', 'OSType': 'windows'}):
            with mock.patch.object(config.platform, 'system', return_value='Darwin'), mock.patch.object(config.subprocess, 'check_output', return_value=json.dumps(info)), self.assertRaises(ValueError):
                config.render(dict(env, DOCKER_HOST='unix:///local/docker.sock'))
            self.assertEqual((self.directory / 'prometheus.yml').read_text(), before)

    def test_rejects_ambiguous_origins_without_replacing_valid_config(self):
        config.render(self.env)
        before = (self.directory / 'prometheus.yml').read_bytes()
        for value in ('http://host:80', 'https://host', 'https://host:0443', 'https://host:443/',
                      'https://host:443?x=1', 'https://host:443#x', 'https://user:secret@host:443',
                      'https://HOST:443', 'https://bad..host:443', 'https://0.0.0.0:443',
                      'https://169.254.169.254:443', 'https://[::]:443', 'https://host:65536',
                      'https://host:443\nmalicious'):
            with self.subTest(value=value), self.assertRaises(ValueError):
                config.render(dict(self.env, PROMETHEUS_MONITOR_URL=value))
            self.assertEqual((self.directory / 'prometheus.yml').read_bytes(), before)

    def control_inputs(self):
        # Different mount directory; only centre files belong in deployment.
        directory = Path(self.temp.name + '-control')
        directory.mkdir()
        self.addCleanup(lambda: __import__('shutil').rmtree(directory))
        for name in ('server.crt', 'server.key'):
            (directory / name).write_text(name)
        return dict(self.env, ADDP_METRICS_CONTROL_ENABLED='true',
                    ADDP_METRICS_CONTROL_DIR=str(directory),
                    ADDP_METRICS_CONTROL_GATEWAY_URL='http://host.docker.internal:8000',
                    PROMETHEUS_SYSTEM_URL='https://metrics-control:9444',
                    PROMETHEUS_MONITOR_URL='https://metrics-control:9444')

    def test_private_control_selection_generation_and_rejection_are_atomic(self):
        env = self.control_inputs()
        config.render(env)
        control = Path(env['ADDP_METRICS_CONTROL_DIR']) / 'nginx.conf'
        original = control.read_bytes()
        prometheus = (self.directory / 'prometheus.yml').read_bytes()
        text = original.decode()
        self.assertNotIn(self.secret, text)
        self.assertIn('proxy_set_header Authorization $http_authorization;', text)
        self.assertIn('access_log off;', text)
        self.assertEqual(text.count('proxy_pass http://host.docker.internal:8000;'), 2)
        self.assertIn('location / { return 404; }', text)
        for values in ({'ADDP_METRICS_CONTROL_ENABLED': 'yes'},
                       {'ADDP_METRICS_CONTROL_DIR': str(ROOT)},
                       {'ADDP_METRICS_CONTROL_GATEWAY_URL': 'http://host:80/path'},
                       {'ADDP_METRICS_CONTROL_GATEWAY_URL': 'http://HOST:80'},
                       {'ADDP_METRICS_CONTROL_GATEWAY_URL': 'http://user:pass@host:80'},
                       {'PROMETHEUS_MONITOR_URL': 'https://other:9444'}):
            with self.subTest(values=values), self.assertRaises(ValueError):
                config.render(dict(env, **values))
            self.assertEqual(control.read_bytes(), original)
            self.assertEqual((self.directory / 'prometheus.yml').read_bytes(), prometheus)
        (control.parent / 'ca.key').write_text('private-signing-key')
        with self.assertRaises(ValueError):
            config.render(env)
        config.render(dict(self.env, ADDP_METRICS_CONTROL_ENABLED='false',
                           ADDP_METRICS_CONTROL_DIR='invalid', ADDP_METRICS_CONTROL_GATEWAY_URL='invalid'))
        self.assertEqual(control.read_bytes(), original)

    def test_control_profile_is_explicit_and_has_no_host_port_or_business_dependency(self):
        root = (ROOT / 'docker-compose.infra.yml').read_text()
        service = root.split('  metrics-control:\n', 1)[1].split('  prometheus:', 1)[0]
        self.assertNotIn('ports:', service)
        self.assertIn('profiles: [observability-metrics-control]', service)
        template = (ROOT / 'scripts/infra/metrics-control.yml').read_text()
        self.assertNotIn('ports:', template)
        self.assertNotIn('depends_on:', template)
        for key in ('65534:65534', 'read_only: true', 'cap_drop: [ALL]', 'cpus: 0.25', 'mem_limit: 128m'):
            self.assertIn(key, template)
        for metrics, control, selected in (('false', 'true', False), ('true', 'false', False), ('true', 'true', True)):
            env = dict(os.environ, ADDP_OBSERVABILITY_LOGS_ENABLED='false',
                       ADDP_OBSERVABILITY_METRICS_ENABLED=metrics, ADDP_METRICS_CONTROL_ENABLED=control)
            result = subprocess.run(['bash', '-c', 'source scripts/utils/observability-env.sh; addp_observability_profiles; printf "%s" "${COMPOSE_PROFILES-}"'],
                                    cwd=ROOT, env=env, capture_output=True, text=True, check=True)
            self.assertEqual('observability-metrics-control' in result.stdout, selected)
        gate = (ROOT / 'scripts/test/monitor-metrics-gate.sh').read_text()
        for name in ('scripts/infra/metrics-control.conf', 'scripts/infra/metrics-control.yml'):
            self.assertIn(name, gate.splitlines()[3])
        down = (ROOT / 'scripts/infra/down.sh').read_text()
        self.assertIn('--profile observability-metrics-control', down)

    def test_secret_missing_mismatched_or_shared_is_rejected(self):
        for values in ({'PROMETHEUS_SERVICE_CLIENT_SECRET': ''},
                       {'PROMETHEUS_SERVICE_CLIENT_SECRET': 'b' * 32},
                       {'MONITOR_SERVICE_CLIENT_SECRET': self.secret}):
            with self.subTest(values=list(values)), self.assertRaises(ValueError):
                config.render(dict(self.env, **values))

    def test_repository_directory_extra_private_key_and_missing_file_are_rejected(self):
        with self.assertRaises(ValueError):
            config.render(dict(self.env, ADDP_METRICS_DEPLOYMENT_DIR=str(ROOT)))
        extra = self.directory / 'signing-ca.key'
        extra.write_text('do-not-mount')
        with self.assertRaises(ValueError):
            config.render(self.env)
        extra.unlink()
        (self.directory / 'collector.key').unlink()
        with self.assertRaises(ValueError):
            config.render(self.env)

    def test_health_certificate_reuse_is_rejected(self):
        # Health directory is intentionally outside the mounted deployment directory.
        with tempfile.TemporaryDirectory() as health:
            Path(health, 'health.crt').write_bytes((self.directory / 'collector.crt').read_bytes())
            with self.assertRaises(ValueError):
                config.render(dict(self.env, ADDP_METRICS_TLS_DIR=health))

    def platform_phase_inputs(self):
        source = (ROOT / 'scripts/prod/start.sh').read_text()
        phase = source.split('bash scripts/prod/wait-infra.sh\n', 1)[1].split('# Containers use', 1)[0]
        script = ('source scripts/utils/observability-env.sh\n'
                  'PROJECT_ROOT="$PWD"\n'
                  'PLATFORM_COMPOSE_FILES=(-f docker-compose.yml)\ninfra_result=0\n'
                  + phase + '\nprintf "%s\n" "$infra_result" "${PLATFORM_COMPOSE_FILES[*]}"\n')
        env = {key: value for key, value in os.environ.items()
               if not key.startswith(('ADDP_METRICS_', 'MONITOR_METRICS_', 'MONITOR_PROMETHEUS_',
                                      'ADDP_PROCESS_METRICS_', 'ADDP_DEPLOYMENT_DISCOVERY_'))}
        discovery = self.directory / 'deployment-discovery'
        discovery.mkdir(mode=0o700)
        env.update(ADDP_METRICS_DEPLOYMENT_DIR=str(self.directory),
                   ADDP_DEPLOYMENT_DISCOVERY_DIR=str(discovery),
                   ADDP_PROCESS_METRICS_DEPLOYMENTS='[]')
        return script, env

    def test_query_certificate_mount_is_independent_and_optional(self):
        script, env = self.platform_phase_inputs()
        env.update(ADDP_OBSERVABILITY_METRICS_ENABLED='true',
                   MONITOR_METRICS_CA_FILE=str(self.directory / 'source-ca.crt'),
                   MONITOR_METRICS_CLIENT_CERT_FILE=str(self.directory / 'collector.crt'),
                   MONITOR_METRICS_CLIENT_KEY_FILE=str(self.directory / 'collector.key'))
        for origin, complete in (('', False), ('https://prometheus:9090', False),
                                 ('https://prometheus:9090', True)):
            values = dict(env, MONITOR_PROMETHEUS_URL=origin)
            if complete:
                for key in ('CA_FILE', 'CLIENT_CERT_FILE', 'CLIENT_KEY_FILE'):
                    values['MONITOR_PROMETHEUS_' + key] = str(self.directory / 'control-ca.crt')
            result = subprocess.run(['bash', '-c', script], cwd=ROOT, env=values,
                                    text=True, capture_output=True, check=True)
            self.assertEqual('metrics-query.yml' in result.stdout, bool(origin) and complete)
            self.assertEqual(result.stdout.splitlines()[0], '1' if origin and not complete else '0')
        text = (ROOT / 'scripts/prod/metrics-query.yml').read_text()
        self.assertEqual(text.count('read_only: true'), 3)
        self.assertNotIn('health.', text)
        self.assertNotIn('collector.', text)
        self.assertNotIn('depends_on', text)

    def test_platform_certificate_mounts_are_selected_only_with_complete_inputs(self):
        script, env = self.platform_phase_inputs()
        for selected, valid, expected in (('false', False, '0'), ('true', False, '1'), ('true', True, '0')):
            with self.subTest(selected=selected, valid=valid):
                values = dict(env, ADDP_OBSERVABILITY_METRICS_ENABLED=selected)
                if valid:
                    values.update(MONITOR_METRICS_CA_FILE=str(self.directory / 'source-ca.crt'),
                                  MONITOR_METRICS_CLIENT_CERT_FILE=str(self.directory / 'collector.crt'),
                                  MONITOR_METRICS_CLIENT_KEY_FILE=str(self.directory / 'collector.key'))
                result = subprocess.run(['bash', '-eu', '-c', script], cwd=ROOT, env=values,
                                        capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout.splitlines()[0], expected)
                self.assertEqual('metrics-platform.yml' in result.stdout, selected == 'true' and valid)
                self.assertEqual('platform-process-metrics.json' in result.stdout, selected == 'true' and valid)

    def test_real_compose_passes_optional_identity_and_readonly_admission_files(self):
        env = dict(os.environ, ADDP_OBSERVABILITY_METRICS_ENABLED='true',
                   PROMETHEUS_SERVICE_CLIENT_SECRET=self.secret,
                   MONITOR_METRICS_ALLOWED_CIDRS='192.0.2.0/24', MONITOR_METRICS_ALLOWED_PORTS='9443',
                   MONITOR_METRICS_CA_FILE=str(self.directory / 'source-ca.crt'),
                   MONITOR_METRICS_CLIENT_CERT_FILE=str(self.directory / 'collector.crt'),
                   MONITOR_METRICS_CLIENT_KEY_FILE=str(self.directory / 'collector.key'))
        command = ['docker', 'compose', '--env-file', '/dev/null', '-f', 'docker-compose.yml',
                   '-f', 'scripts/prod/metrics-platform.yml', 'config', '--format', 'json']
        result = subprocess.run(command, cwd=ROOT, env=env, capture_output=True, text=True, timeout=15)
        self.assertEqual(result.returncode, 0, 'Platform Compose render failed')
        services = json.loads(result.stdout)['services']
        system, monitor = services['system-backend'], services['monitor-backend']
        self.assertEqual(system['environment']['PROMETHEUS_SERVICE_CLIENT_SECRET'], self.secret)
        self.assertEqual(system['environment']['ADDP_OBSERVABILITY_METRICS_ENABLED'], 'true')
        self.assertEqual(monitor['environment']['MONITOR_METRICS_ALLOWED_CIDRS'], '192.0.2.0/24')
        mounts = [mount for mount in monitor['volumes'] if mount['target'].startswith('/etc/addp/metrics/')]
        self.assertEqual(len(mounts), 3)
        self.assertTrue(all(mount['read_only'] and not mount['bind'].get('create_host_path', False) for mount in mounts))
        self.assertNotIn('prometheus', monitor.get('depends_on', {}))


node_spec = importlib.util.spec_from_file_location('node_metrics', ROOT / 'scripts/infra/node-metrics.py')
node_config = importlib.util.module_from_spec(node_spec)
node_spec.loader.exec_module(node_config)


class NodeMetricsDeploymentTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='addp-node-config-')
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        for name in ('ca.crt', 'server.crt', 'server.key'):
            (self.directory / name).write_text(name)
        self.env = dict(ADDP_NODE_METRICS_ENABLED='true',
                        ADDP_NODE_METRICS_TLS_DIR=str(self.directory),
                        ADDP_NODE_METRICS_LISTEN='192.0.2.10:9100')

    def test_disabled_does_not_access_docker_or_validate_source_secrets(self):
        from unittest.mock import patch
        with patch.object(node_config, 'docker_json', side_effect=AssertionError('Docker called')):
            node_config.run('up', {'ADDP_NODE_METRICS_ENABLED': 'false'})
        with self.assertRaises(ValueError):
            node_config.deployment(dict(self.env, ADDP_NODE_METRICS_ENABLED='1'))

    def test_admission_export_is_a_secret_free_proposal_without_host_identity(self):
        value = node_config.admission_document(dict(self.env, PASSWORD='secret-never-export', ADDP_HOST_NODE_ID='not-this-vm'), 'desktop')
        self.assertEqual(value, {'schema': 'addp.host-admission/v2', 'host': {
            'display_name': 'Docker Desktop Linux VM', 'node_kind': 'virtual', 'addresses': [], 'allowed_module_bindings': []},
            'collection': {'monitor_kind': 'host_resources', 'source': {'type': 'node_exporter', 'endpoint': 'https://192.0.2.10:9100/metrics'}}})
        serialized = json.dumps(value)
        for forbidden in ('secret-never-export', 'not-this-vm', str(self.directory), 'server.key', 'node_id', 'PASSWORD'):
            self.assertNotIn(forbidden, serialized)
        value = node_config.admission_document(self.env, 'linux', 'physical', 'Linux host', ['192.0.2.10', 'host.example'])
        self.assertEqual(value['host']['addresses'], ['192.0.2.10', 'host.example'])
        self.assertEqual(value['host']['node_kind'], 'physical')

    def test_admission_host_kind_and_attributes_must_be_explicit_and_bounded(self):
        for arguments in [(self.env, 'desktop', 'physical'), (self.env, 'linux'), (self.env, 'other', 'virtual'),
                          (self.env, 'desktop', 'virtual', ''), (self.env, 'linux', 'physical', ''),
                          (self.env, 'linux', 'virtual', 'name\ncontrol'), (self.env, 'linux', 'physical', 'x' * 256),
                          (self.env, 'linux', 'physical', 'host', ['same', 'same']),
                          (self.env, 'linux', 'physical', 'host', ['https://not-an-address']),
                          (self.env, 'linux', 'physical', 'host', [f'host-{i}' for i in range(33)])]:
            with self.subTest(arguments=arguments[1:]), self.assertRaises(ValueError):
                node_config.admission_document(*arguments)

    def test_native_host_only_export_has_no_docker_or_collection_dependency(self):
        output = io.StringIO()
        with mock.patch.object(node_config.platform, 'system', return_value='Darwin'), mock.patch.object(node_config, 'docker_json', side_effect=AssertionError('Docker called')), redirect_stdout(output):
            node_config.run('export-admission', {'PASSWORD': 'never-export'}, host_only=True, node_kind='physical', display_name='Native Mac', modules=['monitor', 'meta'])
        value = json.loads(output.getvalue())
        self.assertEqual(value['schema'], 'addp.host-admission/v2')
        self.assertNotIn('collection', value)
        self.assertEqual(value['host']['allowed_module_bindings'], [{'module_name': n, 'client_id': 'addp-'+n} for n in ['monitor', 'meta']])
        self.assertNotIn('never-export', output.getvalue())
        for modules in (['monitor', 'monitor'], ['bad/name'], ['x'+str(i) for i in range(65)]):
            with self.assertRaises(ValueError): node_config.admission_document({}, 'native', 'physical', modules=modules)
        with self.assertRaises(ValueError): node_config.run('up', {}, host_only=True)

    def test_admission_export_only_reads_the_owned_local_running_source(self):
        context = [{'Endpoints': {'docker': {'Host': 'unix:///local/docker.sock'}}}]
        info = {'KernelVersion': '6.10.0-linuxkit', 'OperatingSystem': 'Docker Desktop', 'OSType': 'linux'}
        item = {'Config': {'Labels': {'io.addp.node-metrics.owner': str(ROOT), 'com.docker.compose.service': 'node-exporter'}}, 'State': {'Running': True}}
        for change in (None, 'owner', 'stopped', 'service'):
            inspected = json.loads(json.dumps(item))
            if change == 'owner': inspected['Config']['Labels']['io.addp.node-metrics.owner'] = '/another-workspace'
            if change == 'stopped': inspected['State']['Running'] = False
            if change == 'service': inspected['Config']['Labels']['com.docker.compose.service'] = 'unrelated'
            output = io.StringIO()
            with self.subTest(change=change), mock.patch.object(node_config.platform, 'system', return_value='Darwin'), \
                    mock.patch.object(node_config, 'docker_json', side_effect=[context, info, [inspected]]), \
                    mock.patch.object(node_config.subprocess, 'check_output', return_value='source-id\n'), \
                    mock.patch.object(node_config.subprocess, 'run') as mutate, redirect_stdout(output):
                if change is None:
                    node_config.run('export-admission', self.env)
                    self.assertEqual(json.loads(output.getvalue()), node_config.admission_document(self.env, 'desktop'))
                else:
                    with self.assertRaises(ValueError): node_config.run('export-admission', self.env)
                    self.assertEqual(output.getvalue(), '')
                mutate.assert_not_called()
        with mock.patch.object(node_config, 'docker_json') as docker:
            for env in ({'ADDP_NODE_METRICS_ENABLED': 'false'}, dict(self.env, DOCKER_HOST='ssh://remote')):
                with self.assertRaises(ValueError): node_config.run('export-admission', env)
            docker.assert_not_called()

    def test_only_canonical_ip_listen_and_source_files_are_admitted(self):
        self.assertEqual(node_config.deployment(self.env), self.directory)
        for listen in ('0.0.0.0:9100', '[::]:9100', ':9100', 'host:9100', '192.0.2.10',
                       '192.0.2.10:09100', '192.0.2.10:65536', '169.254.1.1:9100',
                       '224.0.0.1:9100', '192.0.2.10:9100/metrics'):
            with self.subTest(listen=listen), self.assertRaises(ValueError):
                node_config.deployment(dict(self.env, ADDP_NODE_METRICS_LISTEN=listen))
        extra = self.directory / 'ca.key'
        extra.write_text('do-not-mount')
        with self.assertRaises(ValueError):
            node_config.deployment(self.env)
        extra.unlink()
        (self.directory / 'server.key').unlink()
        (self.directory / 'server.key').symlink_to(self.directory / 'ca.crt')
        with self.assertRaises(ValueError):
            node_config.deployment(self.env)
        with self.assertRaises(ValueError):
            node_config.deployment(dict(self.env, ADDP_NODE_METRICS_TLS_DIR=str(ROOT)))

    def test_environment_selection_uses_engine_facts_and_rejects_unknown_remote_old_kernel(self):
        self.assertEqual(node_config.deployment_runtime('Linux', '6.8.0', 'unix:///var/run/docker.sock', 'Ubuntu 24.04'), 'linux')
        self.assertEqual(node_config.deployment_runtime('Darwin', '6.10.14-linuxkit', 'unix:///Users/owner/docker.sock', 'Docker Desktop'), 'desktop')
        for args in (('Darwin', '6.8.0', 'unix:///var/run/docker.sock', 'Ubuntu'),
                     ('Linux', '6.8.0', 'unix:///var/run/docker.sock', 'Docker Desktop'),
                     ('Windows', '6.8.0', 'unix:///var/run/docker.sock', 'Docker Desktop'),
                     ('Darwin', '5.11.0', 'unix:///var/run/docker.sock', 'Docker Desktop'),
                     ('Darwin', '6.10.0', 'ssh://owner@node', 'Docker Desktop'),
                     ('Linux', '6.8.0', 'ssh://owner@node', 'Ubuntu'),
                     ('Linux', '5.11.0', 'unix:///var/run/docker.sock', 'Ubuntu')):
            with self.subTest(args=args), self.assertRaises(ValueError):
                node_config.deployment_runtime(*args)

    def test_context_precedence_ownership_and_certificate_independent_stop(self):
        from unittest.mock import patch
        context = [{'Endpoints': {'docker': {'Host': 'ssh://owner@remote'}}}]
        values = dict(self.env, DOCKER_CONTEXT='remote', DOCKER_HOST='unix:///var/run/docker.sock')
        with patch.object(node_config, 'docker_json', return_value=context), \
                patch.object(node_config.subprocess, 'run') as execute:
            with self.assertRaises(ValueError):
                node_config.run('up', values)
            execute.assert_not_called()
        local = [{'Endpoints': {'docker': {'Host': 'unix:///var/run/docker.sock'}}}]
        foreign = [{'Config': {'Labels': {'io.addp.node-metrics.owner': '/another/workspace'}}}]
        with patch.object(node_config, 'docker_json', side_effect=[local, foreign]), \
                patch.object(node_config.subprocess, 'check_output', return_value='other-container'), \
                patch.object(node_config.subprocess, 'run') as execute:
            with self.assertRaises(ValueError):
                node_config.run('down', {})
            execute.assert_not_called()
        with patch.object(node_config, 'docker_json', return_value=local), \
                patch.object(node_config.subprocess, 'check_output', return_value=''), \
                patch.object(node_config.subprocess, 'run') as execute:
            node_config.run('down', {'ADDP_NODE_METRICS_ENABLED': 'false'})
            self.assertEqual(execute.call_args.args[0][-1], 'down')
            self.assertNotIn('--volumes', execute.call_args.args[0])

    def test_native_launch_overrides_untrusted_root_paths_and_profiles(self):
        from unittest.mock import patch
        values = dict(self.env, DOCKER_HOST='unix:///var/run/docker.sock',
                      ADDP_NODE_METRICS_ROOTFS='/wrong', ADDP_NODE_METRICS_NAMESPACED_COLLECTOR_PREFIX='--no-collector',
                      COMPOSE_PROFILES='unexpected')
        info = {'KernelVersion': '6.8.0', 'OperatingSystem': 'Ubuntu', 'OSType': 'linux'}
        with patch.object(node_config, 'docker_json', return_value=info), \
                patch.object(node_config.platform, 'system', return_value='Linux'), \
                patch.object(node_config.platform, 'release', return_value='6.8.0'), \
                patch.object(node_config.subprocess, 'check_output', return_value=''), \
                patch.object(node_config.subprocess, 'run') as execute:
            node_config.run('up', values)
            self.assertIn('--force-recreate', execute.call_args.args[0])
            env = execute.call_args.kwargs['env']
            self.assertEqual(env['ADDP_NODE_METRICS_ROOTFS'], '/host')
            self.assertEqual(env['ADDP_NODE_METRICS_PROCFS'], '/host/proc')
            self.assertEqual(env['ADDP_NODE_METRICS_NAMESPACED_COLLECTOR_PREFIX'], '--collector')
            self.assertNotIn('COMPOSE_PROFILES', env)

    def test_desktop_launch_overrides_paths_collectors_and_maps_explicit_ipv6_port(self):
        from unittest.mock import patch
        values = dict(self.env, DOCKER_HOST='unix:///Users/owner/docker.sock',
                      ADDP_NODE_METRICS_LISTEN='[::1]:19100',
                      ADDP_NODE_METRICS_ROOTFS='/host', ADDP_NODE_METRICS_PROCFS='/host/proc',
                      ADDP_NODE_METRICS_NAMESPACED_COLLECTOR_PREFIX='--collector', ADDP_NODE_METRICS_PUBLISH_PORT='9999',
                      COMPOSE_PROFILES='unexpected')
        info = {'KernelVersion': '6.10.14-linuxkit', 'OperatingSystem': 'Docker Desktop', 'OSType': 'linux'}
        with patch.object(node_config, 'docker_json', return_value=info), \
                patch.object(node_config.platform, 'system', return_value='Darwin'), \
                patch.object(node_config.subprocess, 'check_output', return_value=''), \
                patch.object(node_config.subprocess, 'run') as execute:
            node_config.run('up', values)
            self.assertIn(str(ROOT / 'scripts/infra/node-metrics-desktop.yml'), execute.call_args.args[0])
            env = execute.call_args.kwargs['env']
            self.assertEqual(env['ADDP_NODE_METRICS_LISTEN'], ':9100')
            self.assertEqual(env['ADDP_NODE_METRICS_PUBLISH_IP'], '::1')
            self.assertEqual(env['ADDP_NODE_METRICS_PUBLISH_PORT'], '19100')
            self.assertEqual(env['ADDP_NODE_METRICS_ROOTFS'], '/')
            self.assertEqual(env['ADDP_NODE_METRICS_PROCFS'], '/proc')
            self.assertEqual(env['ADDP_NODE_METRICS_SYSFS'], '/sys')
            self.assertEqual(env['ADDP_NODE_METRICS_NAMESPACED_COLLECTOR_PREFIX'], '--no-collector')
            self.assertNotIn('COMPOSE_PROFILES', env)

    def test_desktop_stop_does_not_require_valid_listen_or_certificates(self):
        from unittest.mock import patch
        context = [{'Endpoints': {'docker': {'Host': 'unix:///Users/owner/docker.sock'}}}]
        with patch.object(node_config, 'docker_json', return_value=context), \
                patch.object(node_config.platform, 'system', return_value='Darwin'), \
                patch.object(node_config.subprocess, 'check_output', return_value=''), \
                patch.object(node_config.subprocess, 'run') as execute:
            node_config.run('down', {'ADDP_NODE_METRICS_LISTEN': 'invalid', 'ADDP_NODE_METRICS_TLS_DIR': '/missing'})
            self.assertEqual(execute.call_args.args[0][-1], 'down')
            self.assertNotIn('--volumes', execute.call_args.args[0])

    def test_native_kernel_mismatch_and_non_linux_engine_do_not_launch(self):
        from unittest.mock import patch
        for system, engine_os, kernel, kind in (
                ('Linux', 'Ubuntu', '6.8.1', 'linux'),
                ('Darwin', 'Docker Desktop', '6.10.14-linuxkit', 'windows')):
            with self.subTest(system=system), \
                    patch.object(node_config, 'docker_json', return_value={
                        'OperatingSystem': engine_os, 'KernelVersion': kernel, 'OSType': kind}), \
                    patch.object(node_config.platform, 'system', return_value=system), \
                    patch.object(node_config.platform, 'release', return_value='6.8.0'), \
                    patch.object(node_config.subprocess, 'run') as execute:
                with self.assertRaises(ValueError):
                    node_config.run('up', dict(self.env, DOCKER_HOST='unix:///var/run/docker.sock'))
                execute.assert_not_called()

    def test_desktop_compose_has_only_kernel_global_collectors_and_no_host_privileges(self):
        env = dict(os.environ, **self.env)
        env.update(ADDP_NODE_METRICS_OWNER=str(ROOT),
                   ADDP_NODE_METRICS_LISTEN=':9100', ADDP_NODE_METRICS_PUBLISH_IP='127.0.0.1',
                   ADDP_NODE_METRICS_PUBLISH_PORT='19100', ADDP_NODE_METRICS_NAMESPACED_COLLECTOR_PREFIX='--no-collector',
                   ADDP_NODE_METRICS_ROOTFS='/', ADDP_NODE_METRICS_PROCFS='/proc',
                   ADDP_NODE_METRICS_SYSFS='/sys')
        value = subprocess.run(['docker', 'compose', '--env-file', '/dev/null',
                                '-f', str(ROOT / 'scripts/infra/node-metrics.yml'),
                                '-f', str(ROOT / 'scripts/infra/node-metrics-desktop.yml'),
                                'config', '--format', 'json'], env=env, capture_output=True,
                               text=True, check=True, timeout=15)
        services = json.loads(value.stdout)['services']
        self.assertEqual(set(services), {'node-exporter'})
        source = services['node-exporter']
        self.assertNotEqual(source.get('network_mode'), 'host')
        self.assertNotEqual(source.get('pid'), 'host')
        self.assertFalse(source.get('privileged', False))
        self.assertEqual(source['cap_drop'], ['ALL'])
        self.assertEqual(source['security_opt'], ['no-new-privileges:true'])
        self.assertTrue(source['read_only'])
        self.assertEqual(source['user'], '65534:65534')
        self.assertEqual(len(source['ports']), 1)
        self.assertEqual(source['ports'][0]['host_ip'], '127.0.0.1')
        self.assertEqual(source['ports'][0]['published'], '19100')
        self.assertEqual(source['ports'][0]['target'], 9100)
        self.assertEqual(len(source['volumes']), 4)
        self.assertTrue(all(m['read_only'] and m['source'] != '/' for m in source['volumes']))
        for collector in ('filesystem', 'netdev', 'netstat'):
            self.assertIn('--no-collector.' + collector, source['command'])
        for collector in ('cpu', 'meminfo', 'loadavg', 'diskstats', 'stat', 'uname', 'time'):
            self.assertIn('--collector.' + collector, source['command'])

    def test_production_compose_has_sole_host_source_and_readonly_mounts(self):
        env = dict(os.environ, **self.env, ADDP_NODE_METRICS_OWNER=str(ROOT),
                   ADDP_NODE_METRICS_NAMESPACED_COLLECTOR_PREFIX='--collector',
                   ADDP_NODE_METRICS_ROOTFS='/host', ADDP_NODE_METRICS_PROCFS='/host/proc',
                   ADDP_NODE_METRICS_SYSFS='/host/sys')
        value = subprocess.run(['docker', 'compose', '--env-file', '/dev/null',
                                '-f', str(ROOT / 'scripts/infra/node-metrics.yml'),
                                '-f', str(ROOT / 'scripts/infra/node-metrics-linux.yml'),
                                'config', '--format', 'json'], env=env, capture_output=True,
                               text=True, check=True, timeout=15)
        services = json.loads(value.stdout)['services']
        self.assertEqual(set(services), {'node-exporter'})
        source = services['node-exporter']
        self.assertEqual(source['network_mode'], 'host')
        self.assertEqual(source['pid'], 'host')
        self.assertEqual(source['user'], '65534:65534')
        self.assertTrue(source['read_only'])
        self.assertEqual(source['cap_drop'], ['ALL'])
        self.assertEqual(source['cpus'], 0.25)
        self.assertEqual(source['mem_limit'], '268435456')
        self.assertFalse(source.get('privileged', False))
        self.assertNotIn('ports', source)
        volumes = source['volumes']
        self.assertTrue(all(v['read_only'] for v in volumes))
        host = next(v for v in volumes if v['target'] == '/host')
        self.assertEqual(host['source'], '/')
        self.assertEqual(host['bind']['propagation'], 'rslave')
        self.assertEqual(set(v['target'] for v in volumes), {
            '/host', '/etc/addp/node-metrics/web.yml', '/etc/addp/node-metrics/tls/ca.crt',
            '/etc/addp/node-metrics/tls/server.crt', '/etc/addp/node-metrics/tls/server.key'})
        self.assertIn('--collector.disable-defaults', source['command'])
        collectors = {c for c in source['command'] if c.startswith('--collector.')
                      and c != '--collector.disable-defaults' and '=' not in c}
        self.assertEqual(collectors, {'--collector.'+name for name in (
            'cpu', 'meminfo', 'loadavg', 'filesystem', 'diskstats', 'netdev',
            'netstat', 'stat', 'uname', 'time')})
        self.assertIn('--collector.filesystem.mount-points-exclude=^/(dev|proc|run/credentials/.+|sys|var/lib/docker/.+|var/lib/containers/storage/.+)($$|/)', source['command'])
        self.assertIn('--collector.filesystem.fs-types-exclude=^(autofs|binfmt_misc|bpf|cgroup2?|configfs|debugfs|devpts|devtmpfs|fusectl|hugetlbfs|iso9660|mqueue|nsfs|overlay|proc|procfs|pstore|rpc_pipefs|securityfs|selinuxfs|squashfs|erofs|sysfs|tracefs)$$', source['command'])

class ServiceExpectationPublicationTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='addp-expectations-test-')
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        self.path = self.directory / 'service-expectations.json'

    def group(self, module='meta', purpose='scan', count=2):
        return {'module_name': module, 'role': 'worker' if purpose else 'backend',
                'runtime_name': purpose, 'expected_replicas': count}

    def test_same_purpose_replicas_are_one_group_and_distinct_purposes_stay_separate(self):
        rows = [self.group(), self.group('transfer', 'sync', 1), self.group('transfer', 'continuous_sync')]
        result = inventory.service_expectations(self.directory, rows)
        self.assertEqual([(x['module_name'], x['runtime_name'], x['expected_replicas']) for x in result['groups']],
                         [('meta', 'scan', 2), ('transfer', 'continuous_sync', 2), ('transfer', 'sync', 1)])
        self.assertEqual(self.path.stat().st_mode & 0o777, 0o600)

    def test_repeating_a_start_preserves_the_revision_and_intent_time(self):
        first = inventory.service_expectations(self.directory, [self.group()])
        self.assertEqual(inventory.service_expectations(self.directory, [self.group()]), first)
        second = inventory.service_expectations(self.directory, [self.group(count=1)])
        self.assertEqual(second['revision'], 2)
        self.assertEqual(second['groups'][0]['expected_replicas'], 1)

    def test_partial_stop_preserves_other_groups_and_restart_only_changes_selected_groups(self):
        original = inventory.service_expectations(self.directory, [self.group(), self.group('transfer', 'sync')])
        stopped = inventory.service_expectations(self.directory, [], stop_modules=['meta'])
        self.assertEqual([x['desired_state'] for x in stopped['groups']], ['planned_stopped', 'running'])
        self.assertEqual(stopped['groups'][1], original['groups'][1])
        self.assertEqual(inventory.service_expectations(self.directory, [], stop_modules=['meta']), stopped)
        restarted = inventory.service_expectations(self.directory, [self.group()])
        self.assertEqual(restarted['revision'], 3)
        self.assertEqual([x['desired_state'] for x in restarted['groups']], ['running', 'running'])
        self.assertTrue(all(x['desired_state'] == 'planned_stopped' for x in
                            inventory.service_expectations(self.directory, [], stop_all=True)['groups']))

    def test_same_second_stop_restart_and_resize_have_distinct_real_group_change_times(self):
        stamps = ['2020-01-01T00:00:00.000001Z', '2020-01-01T00:00:00.000002Z',
                  '2020-01-01T00:00:00.000003Z', '2020-01-01T00:00:00.000004Z']
        with mock.patch.object(inventory, 'observed_at', return_value='2020-01-01T00:00:00Z'), \
             mock.patch.object(inventory, 'service_intent_time', side_effect=stamps, create=True):
            started = inventory.service_expectations(self.directory, [self.group()])
            stopped = inventory.service_expectations(self.directory, [], stop_modules=['meta'])
            restarted = inventory.service_expectations(self.directory, [self.group()])
            resized = inventory.service_expectations(self.directory, [self.group(count=1)])
        documents = [started, stopped, restarted, resized]
        self.assertEqual([d['groups'][0]['changed_at'] for d in documents], stamps)
        self.assertEqual([d['revision'] for d in documents], [1,2,3,4])
        self.assertEqual(started['groups'][0]['expected_replicas'], restarted['groups'][0]['expected_replicas'])
        self.assertEqual(started['groups'][0]['desired_state'], restarted['groups'][0]['desired_state'])

    def test_non_advancing_clock_cannot_mutate_intent_or_manufacture_future_time(self):
        first_time = '2020-01-01T00:00:00.000002Z'
        with mock.patch.object(inventory, 'service_intent_time', return_value=first_time, create=True):
            first = inventory.service_expectations(self.directory, [self.group()])
        before = self.path.read_bytes()
        for stamp in [first_time, '2020-01-01T00:00:00.000001Z']:
            with self.subTest(stamp=stamp), mock.patch.object(inventory, 'service_intent_time', return_value=stamp, create=True):
                for operation in [lambda: inventory.service_expectations(self.directory, [], stop_all=True),
                                  lambda: inventory.service_expectations(self.directory, [self.group(count=1)]),
                                  lambda: inventory.service_expectations(self.directory, [], replace_running=True)]:
                    with self.assertRaises(inventory.InventoryError): operation()
                    self.assertEqual(self.path.read_bytes(), before)
        with mock.patch.object(inventory, 'service_intent_time', return_value=first_time, create=True):
            self.assertEqual(inventory.service_expectations(self.directory, [self.group()]), first)
        self.assertFalse(list(self.directory.glob('.service-expectations.json-*')))

    def test_unrelated_group_change_does_not_erase_the_original_group_change_marker(self):
        with mock.patch.object(inventory, 'service_intent_time', return_value='2020-01-01T00:00:00.000001Z', create=True):
            first = inventory.service_expectations(self.directory, [self.group(), self.group('transfer', 'sync')])
        with mock.patch.object(inventory, 'service_intent_time', return_value='2020-01-01T00:00:00.000002Z', create=True):
            next_document = inventory.service_expectations(self.directory, [self.group('transfer', 'sync', count=1)])
        self.assertEqual(next_document['groups'][0], first['groups'][0])
        self.assertNotEqual(next_document['groups'][1]['changed_at'], first['groups'][1]['changed_at'])

    def test_invalid_batch_never_partially_publishes_or_replaces_prior_intent(self):
        inventory.service_expectations(self.directory, [self.group()])
        before = self.path.read_bytes()
        for bad in [self.group(purpose=''), dict(self.group(), expected_replicas=True),
                    dict(self.group(), expected_replicas=0), dict(self.group(), role='scheduler'),
                    dict(self.group(), runtime_name='private\nvalue'), dict(self.group(), extra='unknown')]:
            # A Backend with an empty purpose is valid; this failure is a Worker without a purpose.
            if bad['runtime_name'] == '': bad['role'] = 'worker'
            with self.subTest(bad=bad), self.assertRaises(inventory.InventoryError):
                inventory.service_expectations(self.directory, [self.group('quality', 'quality-bounded'), bad])
            self.assertEqual(self.path.read_bytes(), before)
        with self.assertRaises(inventory.InventoryError):
            inventory.service_expectations(self.directory, [self.group(), self.group()])
        self.assertEqual(self.path.read_bytes(), before)

    def test_foreign_invalid_duplicate_and_oversized_documents_are_not_repaired(self):
        document = inventory.service_expectations(self.directory, [self.group()])
        damaged = [json.dumps(dict(document, deployment_id='f'*64)), json.dumps(dict(document, revision=True)),
                   '{"schema":"a","schema":"b"}', 'x'*(inventory.DISCOVERY_LIMIT+1)]
        for stamp in ['2026-01-01 01:02:03Z', 'not-a-time', None]:
            invalid = dict(document, groups=[dict(document['groups'][0], changed_at=stamp)])
            damaged.append(json.dumps(invalid))
        for raw in damaged:
            self.path.write_text(raw)
            with self.subTest(size=len(raw)), self.assertRaises((inventory.InventoryError, ValueError)):
                inventory.service_expectations(self.directory, [self.group()])
            self.assertEqual(self.path.read_text(), raw)

    def test_symlinks_and_public_files_cannot_authorize_writes(self):
        outside = self.directory / 'outside'
        outside.write_text('unchanged')
        self.path.symlink_to(outside)
        with self.assertRaises(OSError):
            inventory.service_expectations(self.directory, [self.group()])
        self.assertEqual(outside.read_text(), 'unchanged')
        self.path.unlink()
        inventory.service_expectations(self.directory, [self.group()])
        self.path.chmod(0o644)
        with self.assertRaises(inventory.InventoryError):
            inventory.service_expectations(self.directory, [self.group()])

    def test_failed_atomic_replace_preserves_prior_document_and_removes_temporary_output(self):
        inventory.service_expectations(self.directory, [self.group()])
        before = self.path.read_bytes()
        with mock.patch.object(inventory.os, 'replace', side_effect=OSError('fixture')), self.assertRaises(OSError):
            inventory.service_expectations(self.directory, [], stop_all=True)
        self.assertEqual(self.path.read_bytes(), before)
        self.assertFalse(list(self.directory.glob('.service-expectations.json-*')))

    def test_concurrent_updates_do_not_lose_other_selected_groups(self):
        from concurrent.futures import ThreadPoolExecutor
        with ThreadPoolExecutor(max_workers=4) as pool:
            list(pool.map(lambda i: inventory.service_expectations(self.directory,
                      [self.group('module'+str(i), 'scan')]), range(12)))
        self.assertEqual(len(json.loads(self.path.read_text())['groups']), 12)

    def test_standard_publisher_cli_uses_explicit_input_and_never_echoes_invalid_body(self):
        command = [sys.executable, str(ROOT/'scripts/utils/deployment-inventory.py'), 'service-expectations',
                   '--directory', str(self.directory)]
        started = subprocess.run(command+['--state', 'running'], input=json.dumps([self.group()]),
                                 text=True, capture_output=True, timeout=5)
        self.assertEqual(started.returncode, 0, started.stderr)
        stopped = subprocess.run(command+['--state', 'planned_stopped', '--modules', 'meta'],
                                 text=True, capture_output=True, timeout=5)
        self.assertEqual(stopped.returncode, 0, stopped.stderr)
        self.assertEqual(json.loads(self.path.read_text())['groups'][0]['desired_state'], 'planned_stopped')
        invalid = subprocess.run(command+['--state', 'running'], input='private-invalid-body',
                                 text=True, capture_output=True, timeout=5)
        self.assertNotEqual(invalid.returncode, 0)
        self.assertNotIn('private-invalid-body', invalid.stdout+invalid.stderr)

    def test_native_launch_units_count_replicas_without_mixing_purposes(self):
        groups = inventory.service_groups_from_units(['meta-backend', 'meta-worker-1', 'meta-worker-2',
                 'transfer-bounded-worker-1', 'transfer-continuous-worker-1', 'quality-worker-1'])
        self.assertEqual({(x['module_name'], x['runtime_name']): x['expected_replicas'] for x in groups},
                         {('meta', ''): 1, ('meta', 'scan'): 2, ('transfer', 'sync'): 1,
                          ('transfer', 'continuous_sync'): 1, ('quality', 'quality-bounded'): 1})
        for bad in [[], ['meta-worker-1', 'meta-worker-1'], ['meta-worker'], ['security-worker'], ['meta-worker-3'], ['meta-frontend']]:
            with self.subTest(units=bad), self.assertRaises(inventory.InventoryError):
                inventory.service_groups_from_units(bad)

    def test_compose_intent_uses_declared_replicas_profiles_and_retires_unselected_groups(self):
        inventory.service_expectations(self.directory, [self.group(), self.group('transfer', 'sync'), self.group('quality', 'quality-bounded')])
        document = {'services': {'meta-worker': {'deploy': {'replicas': 2}},
                    'transfer-bounded-worker': {'deploy': {'replicas': 0}},
                    'quality-worker': {'profiles': ['optional']}, 'security-worker': {}, 'nginx': {}}}
        groups = inventory.service_groups_from_compose(document, set())
        result = inventory.service_expectations(self.directory, groups, replace_running=True)
        self.assertEqual([(x['module_name'], x['desired_state']) for x in result['groups']],
                         [('meta', 'running'), ('quality', 'planned_stopped'), ('transfer', 'planned_stopped')])
        self.assertEqual(len(inventory.service_groups_from_compose(document, {'optional'})), 2)
        self.assertEqual(len(inventory.service_groups_from_compose(document, {'*'})), 2)
        empty = inventory.service_expectations(self.directory, [], replace_running=True)
        self.assertTrue(all(x['desired_state'] == 'planned_stopped' for x in empty['groups']))
        for count in [-1, 1001, True, '2']:
            with self.subTest(count=count), self.assertRaises(inventory.InventoryError):
                inventory.service_groups_from_compose({'services': {'meta-worker': {'deploy': {'replicas': count}}}}, set())

    def test_production_overlay_is_readonly_independent_of_metrics_and_does_not_copy_secrets(self):
        compose = self.directory/'platform.yml'
        compose.write_text('services: {}')
        raw = json.dumps({'services': {'meta-backend': {'environment': {'PASSWORD': 'never-copy-secret'}},
                         'meta-worker': {'deploy': {'replicas': 2}}}}).encode()
        with mock.patch.object(inventory.subprocess, 'run', return_value=mock.Mock(stdout=raw)) as reader:
            first = inventory.publish_platform_service_expectations(self.directory, [str(compose)])
            self.assertEqual(inventory.publish_platform_service_expectations(self.directory, [str(compose)]), first)
        self.assertEqual(reader.call_args.args[0], ['docker', 'compose', '-f', str(compose), 'config', '--format', 'json'])
        overlay = json.loads((self.directory/'platform-service-expectations.json').read_text())
        system = overlay['services']['system-backend']
        self.assertEqual(system['environment'], {'ADDP_DEPLOYMENT_DISCOVERY_DIR': '/etc/addp/deployment-discovery'})
        self.assertEqual(system['volumes'], [{'type': 'bind', 'source': str(self.directory),
                         'target': '/etc/addp/deployment-discovery', 'read_only': True, 'bind': {'create_host_path': False}}])
        for path in (self.path, self.directory/'platform-service-expectations.json'):
            self.assertNotIn('never-copy-secret', path.read_text())
        # The metrics renderer no longer owns a second copy of the System mount.
        source = (ROOT/'scripts/prod/generate-process-metrics-platform.py').read_text()
        self.assertNotIn('/etc/addp/deployment-discovery', source)

    def test_real_compose_configuration_projects_only_declared_groups_and_replicas(self):
        compose = self.directory/'actual-compose.yml'
        compose.write_text("services:\n  meta-backend:\n    image: busybox\n  meta-worker:\n    image: busybox\n    deploy:\n      replicas: 2\n  quality-worker:\n    image: busybox\n    profiles: [optional]\n")
        with mock.patch.dict(os.environ, {'COMPOSE_PROFILES': '', 'ADDP_OBSERVABILITY_METRICS_ENABLED': 'false'}):
            document = inventory.publish_platform_service_expectations(self.directory, [str(compose)])
        self.assertEqual([(row['module_name'],row['runtime_name'],row['expected_replicas']) for row in document['groups']],
                         [('meta','',1),('meta','scan',2)])
        self.assertTrue((self.directory/'platform-service-expectations.json').is_file())

    def test_metrics_renderer_only_owns_sdk_mounts_and_has_no_discovery_dependency(self):
        spec = importlib.util.spec_from_file_location('service_intent_metrics_render', ROOT/'scripts/prod/generate-process-metrics-platform.py')
        renderer = importlib.util.module_from_spec(spec); spec.loader.exec_module(renderer)
        directory = self.directory/'sdk'; directory.mkdir(mode=0o700)
        with mock.patch.dict(os.environ, {'ADDP_METRICS_DEPLOYMENT_DIR':str(directory), 'ADDP_PROCESS_METRICS_DEPLOYMENTS':'[]'}):
            renderer.render()
        self.assertEqual(json.loads((directory/'platform-process-metrics.json').read_text()), {'services':{}})

    def test_standard_production_start_adds_the_single_overlay_only_after_success(self):
        import shlex
        source = (ROOT/'scripts/prod/start.sh').read_text()
        phase = source[source.index('# Publish desired services'):source.index('# 第二步：System')]
        self.assertLess(source.index('addp_acquire_lifecycle_lock prod-start'), source.index('bash scripts/prod/setup-env.sh'))
        root = self.directory/'start-prod'
        (root/'scripts/utils').mkdir(parents=True)
        import shutil
        shutil.copy2(ROOT/'scripts/utils/deployment-inventory.py', root/'scripts/utils/deployment-inventory.py')
        compose = root/'platform.yml'; compose.write_text('services:\n  meta-worker:\n    image: busybox\n')
        target = root/'private-intent'
        env = dict(os.environ, ADDP_DEPLOYMENT_DISCOVERY_DIR=str(target), ADDP_OBSERVABILITY_METRICS_ENABLED='false')
        script = 'set -euo pipefail\ninfra_result=0\nPLATFORM_COMPOSE_FILES=(-f '+shlex.quote(str(compose))+')\n'+phase+'\nprintf "result=%s\\n" "$infra_result"\nprintf "%s\\n" "${PLATFORM_COMPOSE_FILES[@]}"\n'
        result = subprocess.run(['bash','-c',script], cwd=root, env=env, capture_output=True,text=True,timeout=20)
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertIn('result=0',result.stdout)
        self.assertIn(str(target/'platform-service-expectations.json'),result.stdout)
        path = target/'service-expectations.json'
        path.write_text('{"invalid":"must-not-echo"}')
        result = subprocess.run(['bash','-c',script], cwd=root, env=env, capture_output=True,text=True,timeout=20)
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertIn('result=1',result.stdout)
        self.assertNotIn(str(target/'platform-service-expectations.json'),result.stdout)
        self.assertNotIn('must-not-echo',result.stdout+result.stderr)
        self.assertIn('business startup continues',result.stderr)

    def test_failed_compose_render_preserves_the_prior_intent_and_does_not_echo_source(self):
        inventory.service_expectations(self.directory, [self.group()])
        before = self.path.read_bytes()
        compose = self.directory/'platform.yml'; compose.write_text('services: {}')
        for output in [b'private-invalid-config', b'x'*(4*1024*1024+1), b'{"services":{"meta-worker":{"deploy":{"replicas":true}}}}']:
            with mock.patch.object(inventory.subprocess, 'run', return_value=mock.Mock(stdout=output)), self.assertRaises(ValueError):
                inventory.publish_platform_service_expectations(self.directory, [str(compose)])
            self.assertEqual(self.path.read_bytes(), before)
        with mock.patch.object(inventory.subprocess, 'run', side_effect=subprocess.TimeoutExpired('private-source', 15)), self.assertRaises(inventory.InventoryError) as caught:
            inventory.publish_platform_service_expectations(self.directory, [str(compose)])
        self.assertNotIn('private-source', str(caught.exception))
        self.assertEqual(self.path.read_bytes(), before)

    def test_native_selected_plan_does_not_require_metrics_or_probe_processes(self):
        import shlex
        body = 'ROOT_DIR='+shlex.quote(str(ROOT))+'\nsource "$ROOT_DIR/scripts/dev/ports.sh"\nsource "$ROOT_DIR/scripts/dev/worker-units.sh"\n'
        body += 'START_META_BACKEND=true\nSTART_META_WORKER=true\nSTART_TRANSFER_BOUNDED_WORKER=true\nSTART_TRANSFER_CONTINUOUS_WORKER=true\naddp_dev_publish_service_expectations\n'
        env = {k:v for k,v in os.environ.items() if not k.startswith('START_')}
        env.update(ADDP_DEPLOYMENT_DISCOVERY_DIR=str(self.directory), ADDP_DEV_META_WORKER_REPLICAS='2',
                   ADDP_DEV_TRANSFER_BOUNDED_WORKER_REPLICAS='1', ADDP_DEV_TRANSFER_CONTINUOUS_WORKER_REPLICAS='2',
                   TRANSFER_CONTINUOUS_WORKER_INSTANCE_ID='', ADDP_OBSERVABILITY_METRICS_ENABLED='false')
        result = subprocess.run(['bash', '-eu', '-c', body], env=env, capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual({(x['module_name'], x['runtime_name']): x['expected_replicas'] for x in json.loads(self.path.read_text())['groups']},
                         {('meta', ''):1, ('meta','scan'):2, ('transfer','sync'):1, ('transfer','continuous_sync'):2})

    def stop_fixture(self, kind):
        import shutil
        root = self.directory/('stop-'+kind)
        for folder in ('scripts/dev', 'scripts/utils', 'scripts/prod'):
            (root/folder).mkdir(parents=True)
        for name in ('lifecycle-lock.sh', 'ports.sh'):
            shutil.copy2(ROOT/'scripts/dev'/name, root/'scripts/dev'/name)
        shutil.copy2(ROOT/'scripts'/kind/'stop.sh', root/'scripts'/kind/'stop.sh')
        shutil.copy2(ROOT/'scripts/utils/deployment-inventory.py', root/'scripts/utils/deployment-inventory.py')
        (root/'scripts/utils/colors.sh').write_text('RED= GREEN= YELLOW= NC=\n')
        target = root/'private-intent'
        (root/'.env').write_text('ADDP_DEPLOYMENT_DISCOVERY_DIR='+str(target)+'\n')
        command = [sys.executable, str(root/'scripts/utils/deployment-inventory.py'), 'service-expectations',
                   '--directory', str(target), '--state', 'running', '--units', 'meta-worker-1', 'transfer-bounded-worker-1']
        subprocess.run(command, check=True, capture_output=True, timeout=5)
        fake = root/'tools'; fake.mkdir()
        for name in ('docker', 'launchctl', 'lsof'):
            path = fake/name
            path.write_text('#!/bin/sh\nprintf "%s\\n" "'+name+' $*" >> "$STOP_EVENTS"\nexit 0\n')
            path.chmod(0o755)
        env = dict(os.environ, PATH=str(fake)+os.pathsep+os.environ['PATH'], STOP_EVENTS=str(root/'events'))
        for key in ('ADDP_LIFECYCLE_LOCK_HELD','ADDP_LIFECYCLE_OWNER_PID','ADDP_LIFECYCLE_LOCK_ROOT'):
            env.pop(key, None)
        return root, target/'service-expectations.json', env

    def test_standard_dev_and_prod_stop_refuse_all_actions_when_intent_cannot_be_published(self):
        for kind in ('dev', 'prod'):
            root, path, env = self.stop_fixture(kind)
            path.write_text('{"untrusted":"do-not-echo"}')
            before = path.read_bytes()
            (root/'.dev-pids').mkdir()
            pid = root/'.dev-pids/meta.pid'; pid.write_text('999999991')
            result = subprocess.run(['bash', str(root/'scripts'/kind/'stop.sh')], cwd=root, env=env,
                                    capture_output=True, text=True, timeout=8)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('尚未停止服务', result.stderr)
            self.assertNotIn('do-not-echo', result.stdout+result.stderr)
            self.assertFalse((root/'events').exists(), 'stop action preceded planned-stop publication')
            self.assertEqual(path.read_bytes(), before)
            self.assertEqual(pid.read_text(), '999999991')
            self.assertFalse((root/'.dev-state/lifecycle.lock').exists())

    def test_standard_stop_publishes_selected_scope_before_the_first_stop_action(self):
        root, path, env = self.stop_fixture('dev')
        import shlex
        check = 'import json,os; data=json.load(open(os.environ["CHECK_INTENT"])); assert [x["desired_state"] for x in data["groups"]]==["planned_stopped","running"]; open(os.environ["STOP_EVENTS"],"a").write("checked-lsof\\n")'
        (root/'tools/lsof').write_text('#!/bin/sh\nexec '+shlex.quote(sys.executable)+' -c '+shlex.quote(check)+'\n')
        env['CHECK_INTENT'] = str(path)
        result = subprocess.run(['bash', str(root/'scripts/dev/stop.sh'), '-meta'], cwd=root, env=env,
                                capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual([x['desired_state'] for x in json.loads(path.read_text())['groups']], ['planned_stopped','running'])
        self.assertIn('checked-lsof', (root/'events').read_text())
        root, path, env = self.stop_fixture('prod')
        docker = root/'tools/docker'
        docker.write_text('#!/bin/sh\npython3 - "$CHECK_INTENT" <<\'CHECK\'\nimport json,sys\nassert all(x["desired_state"]=="planned_stopped" for x in json.load(open(sys.argv[1]))["groups"])\nCHECK\n[ "$?" = 0 ] || exit 77\nprintf "%s\\n" "$*" >> "$STOP_EVENTS"\n')
        env['CHECK_INTENT'] = str(path)
        result = subprocess.run(['bash', str(root/'scripts/prod/stop.sh')], cwd=root, env=env,
                                capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len((root/'events').read_text().splitlines()), 2)
        self.assertTrue(all(x['desired_state']=='planned_stopped' for x in json.loads(path.read_text())['groups']))



class ComposeConfigDiagnosticTest(unittest.TestCase):
    def process(self, code=0):
        process = mock.Mock(pid=12345, returncode=code)
        process.communicate.return_value = ('{"services":{}}', '')
        process.poll.return_value = code
        process.wait.return_value = code
        return process

    def test_proc_reader_is_loaded_from_this_checkout_not_a_cached_namespace(self):
        foreign = mock.Mock(__file__='/foreign/reader.py')
        with mock.patch.dict(sys.modules, {'scripts.utils.deployment-inventory': foreign}):
            spec = importlib.util.spec_from_file_location('metrics_config_copy_reader_test', __file__)
            loaded = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(loaded)
        self.assertIsNot(loaded.inventory, foreign)
        self.assertEqual(Path(loaded.inventory.__file__).resolve(), ROOT / 'scripts/utils/deployment-inventory.py')

    def test_success_keeps_the_15_second_command_and_is_silent(self):
        process = self.process()
        output = io.StringIO()
        with mock.patch.object(subprocess, 'Popen', return_value=process) as opened, \
                mock.patch('sys.stderr', output):
            value = run_business_postgres_compose({'PATH': '/fixture'}, 'prepared')
        self.assertEqual(value.stdout, '{"services":{}}')
        self.assertEqual(value.returncode, 0)
        process.communicate.assert_called_once_with(timeout=15)
        self.assertTrue(opened.call_args.kwargs['start_new_session'])
        self.assertEqual(opened.call_args.args[0], ['docker', 'compose', '--env-file', '/dev/null', '-f',
                         str(ROOT / 'business/docker-compose.yml'), 'config', '--format', 'json', 'postgres'])
        self.assertEqual(output.getvalue(), '')

    def test_timeout_preserves_failure_and_never_prints_partial_streams(self):
        process = self.process(None)
        sentinel = 'private-url-and-password-do-not-print'
        process.communicate.side_effect = subprocess.TimeoutExpired(['docker'], 15, sentinel, sentinel)
        output = io.StringIO()
        with mock.patch.object(subprocess, 'Popen', return_value=process) as opened, \
                mock.patch.object(os, 'killpg') as killed, mock.patch('sys.stderr', output), \
                mock.patch.dict(globals(), compose_process_snapshot=mock.Mock(return_value={'available': False})), \
                self.assertRaises(subprocess.TimeoutExpired) as error:
            run_business_postgres_compose({'PRIVATE': sentinel}, 'prepared')
        self.assertEqual(error.exception.timeout, 15)
        self.assertIsNone(error.exception.output)
        self.assertIsNone(error.exception.stderr)
        self.assertNotIn(sentinel, output.getvalue())
        self.assertNotIn('PRIVATE', output.getvalue())
        self.assertIn('"outcome": "timeout"', output.getvalue())
        self.assertEqual(opened.call_count, 1)
        killed.assert_called_once_with(12345, signal.SIGKILL)
        process.wait.assert_called_once_with(timeout=1)
        process.stdout.close.assert_called_once()
        process.stderr.close.assert_called_once()

    def test_exit_error_and_spawn_error_only_report_closed_facts(self):
        sentinel = 'private-compose-config-content'
        process = self.process(7)
        process.communicate.return_value = (sentinel, sentinel)
        for phase, effect in [('missing_auth_directory', None), ('prepared', OSError(sentinel))]:
            output = io.StringIO()
            with self.subTest(phase=phase), mock.patch('sys.stderr', output), \
                    mock.patch.object(subprocess, 'Popen', return_value=process, side_effect=effect):
                if effect:
                    with self.assertRaisesRegex(RuntimeError, '^Compose config could not start$'):
                        run_business_postgres_compose({}, phase)
                else:
                    result = run_business_postgres_compose({}, phase)
                    self.assertEqual(result.returncode, 7)
                    self.assertEqual(result.stdout, sentinel)
            self.assertNotIn(sentinel, output.getvalue())
            self.assertIn('"case": "'+phase+'"', output.getvalue())

    def test_cancellation_and_unconfirmed_cleanup_cannot_succeed(self):
        for problem in [KeyboardInterrupt(), subprocess.TimeoutExpired(['docker'], 15)]:
            process = self.process(None)
            process.communicate.side_effect = problem
            process.wait.side_effect = subprocess.TimeoutExpired(['docker'], 1)
            output = io.StringIO()
            with self.subTest(kind=type(problem).__name__), mock.patch.object(subprocess, 'Popen', return_value=process), \
                    mock.patch.object(os, 'killpg') as killed, mock.patch('sys.stderr', output), \
                    mock.patch.dict(globals(), compose_process_snapshot=mock.Mock(return_value={'available': False})), \
                    self.assertRaises(type(problem)):
                run_business_postgres_compose({}, 'prepared')
            killed.assert_called_once_with(12345, signal.SIGKILL)
            self.assertIn('"reaped": false', output.getvalue())

    def test_unknown_case_is_rejected_before_starting_a_command(self):
        with mock.patch.object(subprocess, 'Popen') as opened, self.assertRaises(ValueError):
            run_business_postgres_compose({}, 'private-input')
        opened.assert_not_called()

    def test_snapshot_projects_only_bounded_same_session_process_facts(self):
        process = self.process(None)
        sentinel = 'private-path-command-or-environment'
        def state(pid, parent, group, start=10):
            return f'{pid} ({sentinel}) '+' '.join(['S', str(parent), str(group), str(group)]+['0']*15+[str(start)])
        def read(pid, name):
            if name == 'state': return state(pid, os.getpid() if pid == 12345 else 12345, 12345)
            if name == 'children': return '12346 12347' if pid == 12345 else ''
            if name == 'wait': return 'do_wait' if pid == 12345 else sentinel
            return ''
        def link(path):
            return '/'+sentinel+('/docker' if '/12345/' in path else '/docker-compose' if '/12346/' in path else '/'+sentinel)
        with mock.patch.object(inventory.platform, 'system', return_value='Linux'), \
                mock.patch.object(inventory, 'observer_proc_text', side_effect=read), \
                mock.patch.object(os, 'readlink', side_effect=link):
            facts = compose_process_snapshot(process)
        self.assertEqual([row['executable'] for row in facts['nodes']], ['docker', 'docker-compose', 'other'])
        self.assertEqual([row['wait'] for row in facts['nodes']], ['do_wait', 'other', 'other'])
        self.assertFalse(facts['truncated'])
        for value in (sentinel, '/proc/', '12345', '12346', 'term_pending'):
            self.assertNotIn(value, json.dumps(facts))
        with mock.patch.object(inventory.platform, 'system', return_value='Linux'), \
                mock.patch.object(inventory, 'observer_proc_text', side_effect=read), \
                mock.patch.object(inventory, 'observer_exit_diagnostic', return_value={'proc_state':'Z', 'wait':'0', 'children':0}), \
                mock.patch.object(os, 'readlink', side_effect=link):
            self.assertTrue(compose_process_snapshot(process)['root_exited'])
        process.poll.assert_not_called()

    def test_snapshot_refuses_foreign_group_and_changed_process_identity(self):
        process = self.process(None)
        def text(parent, group, start):
            return '12345 (discard)'+' '+' '.join(['S', str(parent), str(group), str(group)]+['0']*15+[str(start)])
        for mode in ('foreign', 'changed', 'malformed'):
            calls = 0
            def read(pid, name):
                nonlocal calls
                if name != 'state': return ''
                calls += 1
                if mode == 'malformed': return 'invalid private process text'
                return text(os.getpid(), 999 if mode == 'foreign' else 12345, calls)
            with self.subTest(mode=mode), mock.patch.object(inventory.platform, 'system', return_value='Linux'), \
                    mock.patch.object(inventory, 'observer_proc_text', side_effect=read), \
                    mock.patch.object(inventory, 'observer_exit_diagnostic', return_value={'proc_state':'S', 'wait':'0', 'children':0}), \
                    mock.patch.object(os, 'readlink', return_value='/private/docker'):
                facts = compose_process_snapshot(process)
            self.assertEqual(facts['nodes'], [{'depth':0, 'identity':'changed' if mode == 'changed' else 'unavailable'}])
            self.assertNotIn('private', json.dumps(facts))

    def test_snapshot_is_capped_and_distinguishes_exited_root_from_missing_linux_view(self):
        process = self.process(None)
        def read(pid, name):
            if name == 'state':
                return f'{pid} (discard) '+' '.join(['S', str(os.getpid() if pid == 12345 else 12345), '12345', '12345']+['0']*15+['10'])
            if name == 'children': return ' '.join(str(pid) for pid in range(12346, 13346)) if pid == 12345 else ''
            return ''
        with mock.patch.object(inventory.platform, 'system', return_value='Linux'), \
                mock.patch.object(inventory, 'observer_proc_text', side_effect=read), \
                mock.patch.object(os, 'readlink', return_value='/fixture/docker'):
            facts = compose_process_snapshot(process)
        self.assertEqual(len(facts['nodes']), 8)
        self.assertTrue(facts['truncated'])
        with mock.patch.object(inventory.platform, 'system', return_value='Darwin'):
            self.assertEqual(compose_process_snapshot(process), {'available':False})
        process.returncode = 0
        with mock.patch.object(inventory.platform, 'system', return_value='Linux'), \
                mock.patch.object(inventory, 'observer_proc_text') as read:
            self.assertEqual(compose_process_snapshot(process), {'available':True, 'root_exited':True, 'children_view':'unavailable'})
        read.assert_not_called()
        process.poll.assert_not_called()

    def test_timeout_terminates_a_real_private_parent_and_its_child(self):
        real_popen = subprocess.Popen
        created = []
        child_pid = None
        def launch(command, **options):
            nonlocal child_pid
            script = 'import subprocess,sys,time; child=subprocess.Popen([sys.executable,"-c","import time;time.sleep(60)"]);print(child.pid,flush=True);time.sleep(60)'
            process = real_popen([sys.executable, '-c', script], **options)
            created.append(process)
            import selectors
            with selectors.DefaultSelector() as selector:
                selector.register(process.stdout, selectors.EVENT_READ)
                self.assertTrue(selector.select(2), 'Owned process did not become ready')
                child_pid = int(process.stdout.readline(32))
            process.communicate = mock.Mock(side_effect=subprocess.TimeoutExpired(['docker'], 15))
            return process
        try:
            with mock.patch.object(subprocess, 'Popen', side_effect=launch), \
                    mock.patch('sys.stderr', io.StringIO()), self.assertRaises(subprocess.TimeoutExpired):
                run_business_postgres_compose({}, 'prepared')
            self.assertEqual(created[0].returncode, -signal.SIGKILL)
            # A killed adopted child can briefly be a zombie; it must not remain running.
            state = subprocess.run(['ps', '-p', str(child_pid), '-o', 'stat='],
                                   capture_output=True, text=True, timeout=1).stdout.strip()
            self.assertTrue(not state or state[0] in ('Z', 'X'), 'Owned child is still running')
            created[0].communicate.assert_called_once_with(timeout=15)
        finally:
            for process in created:
                try:
                    if process.poll() is None or (child_pid is not None and os.getpgid(child_pid) == process.pid and os.getsid(child_pid) == process.pid):
                        os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait(timeout=1)


if __name__ == '__main__':
    unittest.main()
