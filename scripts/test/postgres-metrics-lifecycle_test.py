"""Deployment lifecycle contracts without a running database or Docker daemon."""
import importlib.util
import json
import os
from pathlib import Path
import shlex
import sys
import tempfile
import unittest
import subprocess
import io
import copy
from contextlib import contextmanager
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('postgres_metrics_lifecycle', ROOT / 'scripts/infra/postgres-observation/lifecycle.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class LifecycleTest(unittest.TestCase):
    @contextmanager
    def recovery_fixture(self, *, running=True, restarted=False):
        with tempfile.TemporaryDirectory() as temporary:
            state = Path(temporary).resolve()/'state'
            module.auth._directory(state, 0o700)
            tls = Path(temporary).resolve()/'tls'
            tls.mkdir()
            for name in ('ca.crt', 'server.crt', 'server.key'):
                (tls/name).write_text('fixture-'+name)
            database = mock.Mock(spec=module.ObservationDatabase)
            database.project, database.maintenance, database.address, database.network = 'fixture', 'postgres', '10.2.3.4', 'fixture-network'
            database.identity = 'a'*64
            database.facts = {'id':database.identity, 'running':True, 'started_at':'2026-11-01T00:00:00Z'}
            if restarted:
                database.facts['started_at'] = '2026-11-01T01:00:00Z'
            objects = {}
            commands = []
            docker = mock.Mock(info={'id':'fixture-engine'})
            manager = module.SourceLifecycle(docker, 'infra', state, lambda:database)
            document = manager.source_document(database, '127.0.0.1', 9443, started_at='2026-11-01T00:00:00Z')
            def populate(document):
                for index, service in enumerate(sorted(manager.services)):
                    identity = str(index+1)*64
                    labels = dict(document['services'][service]['labels'], **{
                        'com.docker.compose.project':manager.project,'com.docker.compose.service':service,'com.docker.compose.oneoff':'False'})
                    objects[identity] = {'id':identity,'labels':labels,'running':running,
                        'network_mode':database.network if service==manager.exporter else 'container:'+'1'*64,
                        'ports':{'9443/tcp':[{'HostIp':'127.0.0.1','HostPort':'9443'}]} if service==manager.exporter else {}}
            populate(document)
            def run(args, **kwargs):
                commands.append(args)
                if args[0]=='ps':return '\n'.join(objects)
                if args[0]=='rm':objects.pop(args[-1]);return ''
                if args[0]=='compose':
                    objects.clear();populate(module.read_json(state/'compose.json'))
                    for obj in objects.values():obj['running']=True
                    return ''
                raise AssertionError('Unexpected Docker command '+repr(args))
            docker.run.side_effect = run
            docker.fact.side_effect = lambda identity:copy.deepcopy(database.facts if identity==database.identity else objects[identity])
            module.file_write(state/'owner.json',json.dumps({'schema':'addp.postgres-metrics/v1','workspace':str(ROOT),'scope':'infra',
                'source_key':{'docker_engine_id':'fixture-engine','project':'fixture','service':'postgres'}}),0o600)
            module.file_write(state/'password','b'*64,0o600)
            module.file_write(state/'compose.json',json.dumps(document),0o600)
            for directory in (state/'runtime',state/'runtime/source',state/'runtime/web'):
                module.auth._directory(directory,0o755)
            module.file_write(state/'runtime/source/sql-exporter.yml',json.dumps(module.source.configuration('infra',database.address,5432,'b'*64)),0o644)
            for name in ('ca.crt','server.crt','server.key'):
                module.file_write(state/'runtime/web'/name,(tls/name).read_text(),0o644)
            with mock.patch.object(module,'transport',return_value=(tls,'127.0.0.1',9443)), mock.patch.object(module.time,'sleep'):
                yield manager,database,docker,objects,commands

    def test_healthy_recovery_does_not_recreate_or_rewrite_sources(self):
        with self.recovery_fixture() as (manager,db,docker,objects,commands):
            paths=[p for p in manager.state.rglob('*') if p.is_file()]
            before={p:(p.read_bytes(),p.stat().st_ino,p.stat().st_mtime_ns) for p in paths}
            self.assertTrue(manager.recover({}).startswith('healthy'))
            self.assertFalse(any(c[0] in ('rm','compose') for c in commands))
            self.assertEqual(before,{p:(p.read_bytes(),p.stat().st_ino,p.stat().st_mtime_ns) for p in paths})
            db.authenticate.assert_called_once_with('b'*64)
            self.assertFalse(hasattr(db,'sql'))

    def test_stopped_or_restarted_source_uses_shared_creation_and_current_boot(self):
        for running,restarted in ((False,False),(True,True)):
            with self.subTest(running=running,restarted=restarted), self.recovery_fixture(running=running,restarted=restarted) as (manager,db,docker,objects,commands):
                self.assertTrue(manager.recover({}).startswith('source recovered'))
                self.assertEqual(sum(c[0]=='compose' for c in commands),1)
                self.assertTrue(all(o['running'] for o in objects.values()))
                self.assertTrue(all(o['labels']['io.addp.postgres-metrics.database-started-at']==db.facts['started_at'] for o in objects.values()))
                self.assertEqual((manager.state/'password').read_text(),'b'*64)
                self.assertFalse(hasattr(db,'disable'))

    def test_missing_source_pair_recovers_but_failed_readonly_check_never_mutates(self):
        with self.recovery_fixture() as (manager,db,docker,objects,commands):
            objects.clear()
            manager.recover({})
            self.assertEqual(sum(c[0]=='compose' for c in commands),1)
        with self.recovery_fixture(running=False) as (manager,db,docker,objects,commands):
            db.authenticate.side_effect=module.LifecycleError('disabled or drifted observer')
            with self.assertRaises(module.LifecycleError):manager.recover({})
            self.assertFalse(any(c[0] in ('rm','compose') for c in commands))

    def test_recovery_refuses_foreign_missing_drifted_or_replaced_installation(self):
        for problem in ('new-database','foreign-source','lost-password','tls-drift','config-drift','duplicate-json','foreign-engine'):
            with self.subTest(problem=problem), self.recovery_fixture() as (manager,db,docker,objects,commands):
                if problem=='new-database':db.identity='c'*64
                if problem=='foreign-source':objects['1'*64]['labels'][module.OWNER]='/foreign'
                if problem=='lost-password':(manager.state/'password').unlink()
                if problem=='tls-drift':(manager.state/'runtime/web/server.crt').write_text('drift')
                if problem=='config-drift':(manager.state/'runtime/source/sql-exporter.yml').write_text('{}')
                if problem=='duplicate-json':(manager.state/'compose.json').write_text('{"services":{},"services":{}}')
                if problem=='foreign-engine':docker.info={'id':'foreign'}
                with self.assertRaises((ValueError,OSError,KeyError)):manager.recover({})
                self.assertFalse(any(c[0] in ('rm','compose') for c in commands))

    def test_recovery_cleans_new_sources_after_racing_database_change_without_admin(self):
        with self.recovery_fixture(running=False) as (manager,db,docker,objects,commands):
            # Capture startup facts separately; changing the live fact must not change the expected snapshot.
            live=copy.deepcopy(db.facts)
            docker.fact.side_effect=lambda identity:copy.deepcopy(live if identity==db.identity else objects[identity])
            def changed(_):
                if db.authenticate.call_count==2:live['started_at']='2026-11-01T02:00:00Z'
            db.authenticate.side_effect=changed
            with self.assertRaisesRegex(module.LifecycleError,'facts changed'):manager.recover({})
            self.assertFalse(objects)
            self.assertFalse(hasattr(db,'sql'))

    def test_recovery_lock_contention_refuses_before_database_access(self):
        with self.recovery_fixture() as (manager,db,docker,objects,commands):
            factory=mock.Mock(side_effect=AssertionError('database read under competing lock'))
            manager.database_factory=factory
            with module.auth.locked(manager.state), self.assertRaisesRegex(ValueError,'busy'):
                manager.recover({})
            factory.assert_not_called()
            self.assertFalse(commands)

    def test_unselected_or_uninstalled_recovery_never_constructs_docker(self):
        with tempfile.TemporaryDirectory() as temporary, mock.patch.object(module,'Docker') as docker:
            for enabled in ('false','true'):
                module.run('recover','infra',{'ADDP_POSTGRES_METRICS_ENABLED':enabled,'ADDP_POSTGRES_METRICS_STATE_DIR':str(Path(temporary).resolve()/'absent')})
            docker.assert_not_called()

    def test_full_start_recovery_selection_and_optional_failure_are_executable(self):
        script=(ROOT/'scripts/dev/start.sh').read_text()
        block=script[script.index('# Only full local startup may recover'):script.index('# Refresh deployment-side facts')]
        for full,online,selected in [('true','0',True),('false','0',False),('true','1',False)]:
            command="infra_optional_result=0; infra_optional_failures=(); source() { :; }; addp_recover_metrics_sources() { echo RECOVER; return 1; };\n"+block+'\necho CONTINUED; printf "%s\\n" "${infra_optional_failures[*]-}"; exit "$infra_optional_result"'
            result=subprocess.run(['bash','-eu','-c',command],env=dict(os.environ,START_ALL=full,ADDP_ONLINE_HOST=online,ROOT_DIR='/fixture'),capture_output=True,text=True,timeout=5)
            self.assertEqual('RECOVER' in result.stdout,selected)
            self.assertIn('CONTINUED',result.stdout)
            self.assertEqual(result.returncode,int(selected))
            self.assertEqual('采集来源恢复' in result.stdout,selected)

    def test_recovery_uses_unified_entry_for_both_scopes_and_preserves_failure(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);(root/'business').mkdir();(root/'tools').mkdir()
            stub=root/'tools/python3'
            stub.write_text('#!/bin/bash\nprintf \'%s\\n\' "$@"\nexit 1\n')
            stub.chmod(0o700)
            result=subprocess.run(['bash','-eu','-c','source "$1"; addp_recover_metrics_sources','test',str(ROOT/'scripts/utils/observability-env.sh')],env=dict(os.environ,PROJECT_ROOT=str(root),PATH=str(root/'tools')+os.pathsep+os.environ['PATH'],ADDP_POSTGRES_METRICS_ENABLED='true',ADDP_POSTGRES_METRICS_LISTEN='127.0.0.1:8888'),capture_output=True,text=True,timeout=5)
            self.assertEqual(result.returncode,1)
            self.assertEqual(result.stdout.splitlines(),[str(root/'scripts/infra/metrics-sources.py'),'recover','--scope','host','--scope','infra','--scope','business','--kind','node','--kind','postgresql','--kind','redis','--kind','minio'])

    def test_activation_failure_reports_only_closed_stage_and_keeps_cleanup(self):
        from contextlib import nullcontext, redirect_stderr
        with tempfile.TemporaryDirectory() as temporary:
            database = mock.Mock(project='business', maintenance='postgres', networks=['127.0.0.1/32'])
            database.locked.return_value = nullcontext()
            database.read_hba.return_value = 'local all all trust\n'
            database.sql.return_value = '0'
            database.write_hba.side_effect = OSError('private-hba-password-sentinel')
            docker = mock.Mock(info={'id': 'fixture'})
            docker.run.return_value = ''
            lifecycle = module.SourceLifecycle(docker, 'business', Path(temporary).resolve() / 'state', lambda: database)
            with mock.patch.object(module, 'transport', return_value=(Path(temporary), '127.0.0.1', 9443)):
                with self.assertRaises(OSError) as error:
                    lifecycle.up({})
            database.disable.assert_called()
            self.assertFalse(any(call.args[0][0] == 'compose' for call in docker.run.call_args_list))
            output = io.StringIO()
            with mock.patch.object(sys, 'argv', ['lifecycle.py', 'up', '--scope', 'business']), \
                    mock.patch.object(module, 'run', side_effect=error.exception), redirect_stderr(output):
                self.assertEqual(module.main(), 1)
            self.assertIn('stage=authentication-update', output.getvalue())
            self.assertNotIn('sentinel', output.getvalue())

    def test_main_reports_closed_lifecycle_reason_without_raw_exception_values(self):
        from contextlib import redirect_stderr
        for error, expected in [(module.LifecycleError('PostgreSQL authentication reload timed out'), 'reload timed out'),
                                (ValueError('private-json-sentinel'), 'configuration failed'),
                                (OSError('private-path-sentinel'), 'configuration failed')]:
            with self.subTest(error=type(error).__name__):
                output = io.StringIO()
                with mock.patch.object(sys, 'argv', ['lifecycle.py', 'up', '--scope', 'business']), \
                        mock.patch.object(module, 'run', side_effect=error), redirect_stderr(output):
                    self.assertEqual(module.main(), 1)
                self.assertIn(expected, output.getvalue())
                self.assertNotIn('sentinel', output.getvalue())

    def test_cleanup_failure_does_not_replace_primary_activation_failure(self):
        from contextlib import nullcontext, redirect_stderr
        with tempfile.TemporaryDirectory() as temporary:
            database = mock.Mock()
            database.locked.return_value = nullcontext()
            database.disable.side_effect = [None, OSError('private-cleanup-sentinel')]
            docker = mock.Mock(info={'id': 'fixture'})
            docker.run.return_value = ''
            manager = module.SourceLifecycle(docker, 'infra', Path(temporary).resolve() / 'state', lambda: database)
            original = module.LifecycleError('PostgreSQL source certificate verification failed')
            with mock.patch.object(module, 'transport', side_effect=original):
                with self.assertRaises(module.LifecycleError) as error:
                    manager.up({})
            self.assertIs(error.exception, original)
            self.assertTrue(original.observation_cleanup_failed)
            output = io.StringIO()
            with mock.patch.object(sys, 'argv', ['lifecycle.py', 'up', '--scope', 'infra']), \
                    mock.patch.object(module, 'run', side_effect=original), redirect_stderr(output):
                self.assertEqual(module.main(), 1)
            self.assertIn('stage=transport-check', output.getvalue())
            self.assertIn('cleanup=failed', output.getvalue())
            self.assertNotIn('sentinel', output.getvalue())

    def test_authentication_read_diagnostics_never_echo_private_output(self):
        database = module.Database.__new__(module.Database)
        database.identity = 'a' * 64
        database.settings = {'hba': '/fixed/pg_hba.conf'}
        database.docker = mock.Mock()
        for code, reason in [(41, 'symbolic link'), (42, 'not a regular file'),
                             (45, 'content read failed'), (1, 'container command failed')]:
            database.docker.run.return_value = subprocess.CompletedProcess([], code, 'private-hba-sentinel', 'private-error-sentinel')
            with self.assertRaises(module.LifecycleError) as error:
                database.read_hba()
            self.assertIn(reason, str(error.exception))
            self.assertNotIn('sentinel', str(error.exception))
        database.docker.run.return_value = subprocess.CompletedProcess([], 0, 'local all all trust\n', '')
        self.assertEqual(database.read_hba(), 'local all all trust\n')

    def test_authentication_read_is_bounded_and_missing_has_closed_diagnosis(self):
        database = module.Database.__new__(module.Database)
        database.identity = 'owned'
        database.settings = {'hba': '/fixed/pg_hba.conf'}
        database.docker = mock.Mock()
        for code, stderr, reason in [(45, 'head: No such file or directory', 'not visible'),
                                     (45, 'head: Permission denied', 'content read failed'),
                                     (41, '', 'symbolic link'), (42, '', 'not a regular file')]:
            database.docker.run.return_value = subprocess.CompletedProcess([], code, '', stderr)
            with self.assertRaises(module.LifecycleError) as error:
                database.read_hba()
            self.assertIn(reason, str(error.exception))
        database.docker.run.return_value = subprocess.CompletedProcess([], 0, 'a' * (module.source.HBA_LIMIT + 1), '')
        with self.assertRaisesRegex(module.LifecycleError, 'size budget exceeded'):
            database.read_hba()
        command = database.docker.run.call_args.args[0]
        self.assertIn('head -c 262145', command[command.index('-c') + 1])
        self.assertNotIn('wc -c', command[command.index('-c') + 1])

    def test_authentication_reader_shell_rejects_special_files_and_bounds_real_reads(self):
        database = module.Database.__new__(module.Database)
        database.identity = 'owned'
        database.settings = {'hba': '/fixed/pg_hba.conf'}
        database.docker = mock.Mock()
        database.docker.run.return_value = subprocess.CompletedProcess([], 0, '', '')
        database.read_hba()
        command = database.docker.run.call_args.args[0]
        script = command[command.index('-c') + 1]
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            regular = root / 'hba'
            regular.write_text('local all all trust\n')
            link = root / 'linked'
            link.symlink_to(regular)
            fifo = root / 'fifo'
            os.mkfifo(fifo)
            for path, expected in [(regular, 0), (link, 41), (root, 42), (fifo, 42), (root/'missing', 45)]:
                result = subprocess.run(['sh', '-c', script, 'reader-fixture', str(path)],
                    capture_output=True, text=True, timeout=3, env=dict(os.environ, LC_ALL='C'))
                self.assertEqual(result.returncode, expected, str(path))
            regular.write_text('a' * (module.source.HBA_LIMIT * 2))
            result = subprocess.run(['sh', '-c', script, 'reader-fixture', str(regular)],
                capture_output=True, timeout=3)
            self.assertEqual(len(result.stdout), module.source.HBA_LIMIT + 1)

    def test_host_publication_refuses_missing_drift_and_permission_without_reload(self):
        for problem in ['third-version', module.LifecycleError('not visible'), module.LifecycleError('permission denied'), 'old']:
            database = module.Database.__new__(module.Database)
            database.authentication = Path('/private/owned-auth')
            database.read_hba = mock.Mock(side_effect=problem) if isinstance(problem, Exception) else mock.Mock(return_value=problem)
            database.sql = mock.Mock()
            with mock.patch.object(module.auth, 'replace'):
                with self.assertRaises(module.LifecycleError):
                    database.write_hba('old', 'new')
            database.sql.assert_not_called()

    def test_unchanged_authentication_keeps_inode_after_all_version_checks(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary).resolve() / 'auth'
            baseline = 'local all all trust\n'
            runtime = module.auth.prepare_business(directory, baseline)
            value = module.source.authentication_configuration(baseline, 'business', ['127.0.0.1/32'])
            module.auth.replace(directory, baseline, value)
            path = runtime / 'pg_hba.conf'
            before = path.stat()
            with mock.patch.object(module.auth.private.tempfile, 'mkstemp', side_effect=AssertionError('Unchanged policy must not be republished')):
                module.auth.replace(directory, value, value)
                stale = module.source.authentication_configuration(baseline, 'business', ['127.0.0.1/32', '::1/128'])
                with self.assertRaisesRegex(module.auth.AuthenticationError, 'version changed'):
                    module.auth.replace(directory, stale, value)
                path.chmod(0o600)
                try:
                    with self.assertRaises(module.auth.AuthenticationError):
                        module.auth.replace(directory, value, value)
                finally:
                    path.chmod(0o644)
            after = path.stat()
            self.assertEqual((after.st_ino, after.st_mtime_ns), (before.st_ino, before.st_mtime_ns))
            self.assertEqual(path.read_text(), value)

    def test_source_queries_are_project_scoped(self):
        docker = mock.Mock()
        docker.run.return_value = ''
        manager = module.SourceLifecycle(docker, 'infra', Path('/private/unused'), mock.Mock(), project='owned-t2')
        self.assertFalse(manager.source_containers())
        self.assertIn('label=com.docker.compose.project=owned-t2', docker.run.call_args.args[0])
        probe = (ROOT / 'scripts/test/postgres-lifecycle-probe.py').read_text()
        self.assertNotIn("docker.run(['ps', '-q', '--filter', 'label='+module.OWNER", probe)

    def test_business_start_configuration_does_not_inherit_infra_inputs(self):
        keys = ['ADDP_POSTGRES_METRICS_'+suffix for suffix in ('ENABLED', 'TLS_DIR', 'LISTEN', 'STATE_DIR')]
        parent = dict(os.environ, **dict(zip(keys, ['true', '/infra-tls', '127.0.0.1:9999', '/infra-state'])))
        script = (ROOT / 'business/scripts/start.sh').read_text()
        block = script[script.index('unset ADDP_POSTGRES_METRICS_ENABLED'):]
        block = block[:block.index('if [ "$ENABLE_FALKORDB"')]
        for content, expected in [('', [None]*4), ('POSTGRES_USER=business\n', [None]*4),
                                  ('ADDP_POSTGRES_METRICS_ENABLED=true\nADDP_POSTGRES_METRICS_LISTEN=127.0.0.1:9449\n', ['true', None, '127.0.0.1:9449', None])]:
            with self.subTest(content=content), tempfile.TemporaryDirectory() as temporary:
                if content:
                    (Path(temporary)/'.env').write_text(content)
                command = block + '\n' + shlex.join([sys.executable, '-c',
                    'import json,os; print(json.dumps([os.environ.get(key) for key in '+repr(keys)+']))'])
                result = subprocess.run(['bash', '-eu', '-c', command], cwd=temporary, env=parent,
                                        capture_output=True, text=True, check=True, timeout=5)
                self.assertEqual(json.loads(result.stdout), expected)

    def test_docker_mount_order_is_not_an_identity_change(self):
        docker = module.Docker.__new__(module.Docker)
        mounts = [{'Destination': '/a', 'Type': 'bind', 'Source': '/host/a', 'RW': False},
                  {'Destination': '/b', 'Type': 'bind', 'Source': '/host/b', 'RW': False}]
        docker.run = mock.Mock(side_effect=[json.dumps({'mounts': mounts}),
                                           json.dumps({'mounts': list(reversed(mounts))}),
                                           json.dumps({'mounts': [dict(mounts[0], RW=True), mounts[1]]})])
        original = docker.fact('owned')
        self.assertEqual(original, docker.fact('owned'))
        self.assertNotEqual(original, docker.fact('owned'))

    def test_installation_has_one_active_revision_and_explicit_login_intent(self):
        for scope in ('infra', 'business'):
            for enabled in (False, True):
                value = module.install_input(scope, '1' * 64, enabled)
                self.assertIn("\\set observer_enabled '"+str(enabled).lower()+"'", value)
                self.assertNotIn('1' * 64, value)
                self.assertIn('CREATE ROLE %I NOLOGIN', value)
        for scope, enabled in [('tenant', True), ('infra', 'true')]:
            with self.assertRaises(ValueError):
                module.install_input(scope, '1' * 64, enabled)

    def test_state_is_external_and_scope_separated(self):
        with tempfile.TemporaryDirectory() as temporary:
            env = {'ADDP_POSTGRES_METRICS_STATE_DIR': str(Path(temporary).resolve())}
            self.assertNotEqual(module.state_directory(env, 'infra'), module.state_directory(env, 'business'))
        for value in ('relative', str(ROOT / '.dev-state/metrics')):
            with self.assertRaises(ValueError):
                module.state_directory({'ADDP_POSTGRES_METRICS_STATE_DIR': value}, 'infra')

    def test_foreign_source_is_never_stopped_or_replaced(self):
        docker = mock.Mock()
        docker.run.return_value = 'a' * 64
        docker.fact.return_value = {'labels': {'com.docker.compose.project': module.group.project(ROOT),
            'com.docker.compose.service': 'postgres-metrics', module.OWNER: '/foreign', module.SCOPE: 'infra'}}
        lifecycle = module.SourceLifecycle(docker, 'infra', Path('/private/unused'), mock.Mock())
        with self.assertRaises(ValueError):
            lifecycle.stop_sources()
        self.assertFalse(any(call.args[0][0] == 'rm' for call in docker.run.call_args_list))

    def test_disabled_uninstalled_source_needs_no_database_or_credentials(self):
        docker = mock.Mock()
        docker.run.return_value = ''
        factory = mock.Mock(side_effect=AssertionError('Unselected source must not read database'))
        with tempfile.TemporaryDirectory() as temporary:
            state = Path(temporary) / 'uncreated'
            module.SourceLifecycle(docker, 'infra', state, factory).down()
            self.assertFalse(state.exists())
            factory.assert_not_called()

    def test_failure_closes_our_account_and_keeps_source_stopped(self):
        with tempfile.TemporaryDirectory() as temporary:
            database = mock.Mock()
            from contextlib import nullcontext
            database.locked.return_value = nullcontext()
            docker = mock.Mock(info={'id': 'fixture'})
            docker.run.return_value = ''
            lifecycle = module.SourceLifecycle(docker, 'infra', Path(temporary).resolve() / 'state', lambda: database)
            with mock.patch.object(module, 'transport', side_effect=ValueError('invalid TLS')):
                with self.assertRaises(ValueError):
                    lifecycle.up({})
            self.assertGreaterEqual(database.disable.call_count, 1)
            self.assertFalse(any(call.args[0][0] == 'compose' for call in docker.run.call_args_list))

    def test_publication_rejects_automatic_or_public_addresses(self):
        for listen in ('', ':9443', '0.0.0.0:9443', '[::]:9443', '8.8.8.8:9443', '127.0.0.1:0', '127.0.0.1:09443'):
            with self.subTest(listen=listen), self.assertRaises(ValueError):
                module.transport({'ADDP_POSTGRES_METRICS_LISTEN': listen})


if __name__ == '__main__':
    unittest.main()
