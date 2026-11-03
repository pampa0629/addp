import contextlib
import copy
import importlib.util
import io
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('metrics_sources', ROOT/'scripts/infra/metrics-sources.py')
manager = importlib.util.module_from_spec(spec)
spec.loader.exec_module(manager)


class MetricsSourcesTest(unittest.TestCase):
    def invoke(self, action, scopes=(), kinds=(), env=None, **patches):
        output = io.StringIO()
        with contextlib.ExitStack() as stack:
            stack.enter_context(contextlib.redirect_stdout(output))
            stack.enter_context(patch.object(manager, 'environment', return_value=env or {}))
            mocks = {name: stack.enter_context(patch.object(manager, name, **config)) for name, config in patches.items()}
            code = manager.run(action, scopes, kinds, inherited={})
        return code, output.getvalue(), mocks

    def test_exact_selection_deduplicates_and_never_expands_scope(self):
        self.assertEqual(len(manager.selection((), ())), 7)
        self.assertEqual(manager.selection(['business', 'business'], ['postgresql', 'postgresql']), [('business', 'postgresql')])
        self.assertEqual(manager.selection(['host'], ['postgresql']), [])
        self.assertEqual(manager.selection(['infra'], ['node']), [])
        for scopes in (['host'], ['infra'], ['business']):
            self.assertTrue(all(scope in scopes for scope, _ in manager.selection(scopes, ())))

    def test_configuration_files_do_not_inherit_selections_or_credentials(self):
        with tempfile.TemporaryDirectory() as folder, patch.object(manager, 'ROOT', Path(folder)):
            root = Path(folder); (root/'business').mkdir()
            (root/'.env').write_text('ADDP_POSTGRES_METRICS_ENABLED=true\nADDP_POSTGRES_METRICS_LISTEN=127.0.0.1:9443\nMINIO_ROOT_PASSWORD=infra-secret\n')
            (root/'business/.env').write_text('ADDP_POSTGRES_METRICS_ENABLED=false\n')
            inherited = dict(os.environ, ADDP_POSTGRES_METRICS_ENABLED='true', ADDP_POSTGRES_METRICS_LISTEN='127.0.0.1:8888', MINIO_ROOT_PASSWORD='parent-secret')
            infra = manager.environment('infra', inherited)
            business = manager.environment('business', inherited)
            self.assertEqual(infra['ADDP_POSTGRES_METRICS_LISTEN'], '127.0.0.1:9443')
            self.assertEqual(business['ADDP_POSTGRES_METRICS_ENABLED'], 'false')
            self.assertNotIn('ADDP_POSTGRES_METRICS_LISTEN', business)
            self.assertNotIn('MINIO_ROOT_PASSWORD', business)
            (root/'business/.env').unlink()
            with self.assertRaises(ValueError): manager.environment('business', inherited)

    def test_bad_config_is_private_and_does_not_read_another_domain(self):
        with tempfile.TemporaryDirectory() as folder, patch.object(manager, 'ROOT', Path(folder)):
            root = Path(folder); (root/'business').mkdir()
            (root/'.env').write_text('echo private-password >&2\nfalse\n')
            (root/'business/.env').write_text('ADDP_MINIO_METRICS_ENABLED=false\n')
            with self.assertRaises(ValueError): manager.environment('infra', os.environ)
            self.assertEqual(manager.environment('business', os.environ)['ADDP_MINIO_METRICS_ENABLED'], 'false')
            with patch.object(manager, 'environment', side_effect=ValueError('private-password')), contextlib.redirect_stdout(io.StringIO()) as output:
                self.assertEqual(manager.run('up', ['infra'], ['postgresql']), 1)
            self.assertNotIn('private-password', output.getvalue())

    def test_hosted_cannot_read_personal_configuration(self):
        with patch.object(manager.subprocess, 'run') as run:
            for filename in ('', 'relative.env', '/nonexistent/addp.env'):
                with self.assertRaises(ValueError): manager.environment('business', {'ADDP_ONLINE_HOST':'1', 'ADDP_ONLINE_ENV_FILE':filename})
            run.assert_not_called()
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder)/'isolated.env'; path.write_text('ADDP_MINIO_METRICS_ENABLED=false\n')
            for scope in manager.SCOPES:
                env = manager.environment(scope, {'PATH':os.environ['PATH'], 'ADDP_ONLINE_HOST':'1', 'ADDP_ONLINE_ENV_FILE':str(path)})
                self.assertEqual(env['ADDP_MINIO_METRICS_ENABLED'], 'false')

    def test_all_sources_are_attempted_after_failure_without_secret_logs(self):
        env = {value[1]+'_ENABLED':'true' for value in manager.PROVIDERS.values()}
        failure = subprocess.CalledProcessError(1, ['secret-command'], stderr='secret-password')
        code, output, mocks = self.invoke('up', env=env, apply={'side_effect':[failure]+['已应用']*6})
        self.assertEqual(code, 1)
        self.assertEqual(mocks['apply'].call_count, 7)
        self.assertNotIn('secret-password', output)
        self.assertNotIn('secret-command', output)

    def test_disabled_checks_and_recovery_do_not_start_or_read_docker(self):
        for action in ('check', 'recover'):
            code, output, mocks = self.invoke(action, apply={}, check={}, status={})
            self.assertEqual(code, 0)
            for value in mocks.values(): value.assert_not_called()
        code, _, mocks = self.invoke('up', ['host'], apply={})
        self.assertEqual(code, 0); mocks['apply'].assert_not_called()

    def test_recovery_never_uses_installation_for_unsupported_kinds(self):
        env = {value[1]+'_ENABLED':'true' for value in manager.PROVIDERS.values()}
        code, output, mocks = self.invoke('recover', env=env, apply={'return_value':'已恢复'})
        self.assertEqual(code, 1)
        self.assertEqual([(call.args[0],call.args[1],call.args[2]) for call in mocks['apply'].call_args_list], [('recover','postgresql','infra'),('recover','postgresql','business')])
        self.assertIn('尚不支持无账号修改恢复', output)

    def test_invalid_selection_never_blocks_shutdown_or_other_sources(self):
        env = {'ADDP_POSTGRES_METRICS_ENABLED':'invalid'}
        code, _, mocks = self.invoke('down', ['infra'], env=env, apply={'return_value':'已停用'})
        self.assertEqual(code, 0)
        self.assertEqual(mocks['apply'].call_count, 3)
        code, output, mocks = self.invoke('check', ['infra'], env=env, check={})
        self.assertEqual(code, 1)
        self.assertIn('失败 1 项', output)
        code, output, _ = self.invoke('up', ['infra'], ['redis'], apply={'return_value':'已应用'})
        self.assertEqual(code, 0)
        self.assertIn('未启用（已核对停用）', output)

    def test_timeout_reports_unknown_result_without_retry_or_private_output(self):
        failure = subprocess.TimeoutExpired(['private-command'], 600, output='private-password')
        code, output, mocks = self.invoke('up', ['infra'], ['redis'], env={'ADDP_REDIS_METRICS_ENABLED':'true'}, apply={'side_effect':failure})
        self.assertEqual(code, 1)
        mocks['apply'].assert_called_once()
        self.assertIn('结果未知', output)
        self.assertNotIn('private', output)

    def test_check_only_calls_existing_transport_validators(self):
        for kind in manager.PROVIDERS:
            provider = Mock()
            with patch.object(manager, 'load', return_value=provider): manager.check(kind, 'host' if kind=='node' else 'business', {})
            if kind=='node': provider.deployment.assert_called_once(); provider.capacity_enabled.assert_called_once()
            elif kind=='minio': provider.sources.transport.assert_called_once()
            else: provider.transport.assert_called_once()
            provider.run.assert_not_called()

    def test_provider_application_uses_one_closed_command_and_safe_output(self):
        for action in ('up','down','recover'):
            completed = subprocess.CompletedProcess([], 0, 'healthy; source unchanged' if action=='recover' else 'secret-password', '')
            with patch.object(manager.subprocess, 'run', return_value=completed) as run:
                text = manager.apply(action, 'postgresql', 'business', {'PATH':'fixture'})
            self.assertEqual(run.call_args.args[0], [manager.sys.executable,str(ROOT/'scripts/infra/postgres-observation/lifecycle.py'),action,'--scope','business'])
            self.assertEqual(run.call_args.kwargs['env'], {'PATH':'fixture'})
            self.assertNotIn('secret-password', text)
        result = subprocess.CompletedProcess([], 1, 'secret-password', '[stage=authentication-update] [cleanup=failed] secret-password')
        with patch.object(manager.subprocess, 'run', return_value=result), self.assertRaisesRegex(ValueError, 'authentication-update.*清理未确认'):
            manager.apply('up','postgresql','business',{})

    def test_standard_lifecycles_use_one_coordinator(self):
        infra = (ROOT/'scripts/infra/up.sh').read_text()
        self.assertEqual(infra.count('metrics-sources.py" up --resolved-env --scope infra'), 2)
        business = (ROOT/'business/scripts/start.sh').read_text().split('observation_kinds=()')[-1]
        self.assertIn('metrics-sources.py" up --resolved-env --scope business', business)
        self.assertNotIn('lifecycle.py" up --scope business', business)
        recovery = (ROOT/'scripts/utils/observability-env.sh').read_text().split('addp_recover_postgres_sources() {')[1]
        self.assertIn('metrics-sources.py" recover --scope infra --scope business --kind postgresql', recovery)
        self.assertNotIn('source "$config"', recovery)
        for path in ('scripts/infra/up.sh','scripts/infra/down.sh','business/scripts/start.sh','business/scripts/stop.sh','scripts/utils/observability-env.sh'):
            subprocess.run(['bash','-n',str(ROOT/path)],check=True,capture_output=True)

    def test_resolved_environment_is_single_scope_and_not_silently_reloaded(self):
        for scopes in ((), ['infra','business']):
            with self.assertRaises(ValueError): manager.run('check',scopes,inherited={},resolved=True)
        with patch.object(manager,'environment') as reload, patch.object(manager,'check') as check, contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(manager.run('check',['infra'],['postgresql'],{'ADDP_POSTGRES_METRICS_ENABLED':'true'},resolved=True),0)
            reload.assert_not_called()
            check.assert_called_once_with('postgresql','infra',{'ADDP_POSTGRES_METRICS_ENABLED':'true'})

    def test_business_service_selection_preserves_optional_failure_and_core_stop(self):
        for service,kind in [('postgres','postgresql'),('redis','redis'),('minio','minio')]:
            source=(ROOT/'business/scripts/stop.sh').read_text()
            block=source[source.index('postgres_metrics_result=0'):source.index('\nif [ "$HAS_ARGS" = false ] || [ ${#SERVICES[@]} -eq 0 ]; then',source.index('postgres_metrics_result=0'))]
            command='python3() { printf "%s\\n" "$@"; return 1; }; HAS_ARGS=true; SERVICES=('+service+'); PROJECT_ROOT=/fixture/business;\n'+block+'\necho CORE_STOP; exit "$postgres_metrics_result"'
            result=subprocess.run(['bash','-eu','-c',command],capture_output=True,text=True,timeout=5)
            self.assertEqual(result.returncode,1)
            self.assertEqual(result.stdout.splitlines(),['/fixture/business/../scripts/infra/metrics-sources.py','down','--scope','business','--kind',kind,'CORE_STOP'])
        for name in ('scripts/infra/down.sh','business/scripts/stop.sh'):
            for line in (ROOT/name).read_text().splitlines():
                if 'metrics-sources.py' in line: self.assertNotIn('--resolved-env',line)

    def test_status_uses_only_local_metadata_and_rejects_foreign_or_changed_identity(self):
        deployment = Mock(); docker = deployment.Docker.return_value
        project = 'addp-postgres-metrics-business'; node='a'*64; source='b'*64; web='c'*64
        deployment.inventory.SCOPES={'business':('business',ROOT/'business')}
        database={'running':True,'started_at':'2026-11-03T00:00:00Z','labels':{'com.docker.compose.project':'business','com.docker.compose.service':'postgres','com.docker.compose.project.working_dir':str(ROOT/'business')}}
        labels={'com.docker.compose.project':project,'io.addp.postgres-metrics.owner':str(ROOT),'io.addp.postgres-metrics.scope':'business','io.addp.postgres-metrics.database-id':node,'io.addp.postgres-metrics.database-started-at':database['started_at']}
        objects={node:database,source:{'running':True,'labels':dict(labels,**{'com.docker.compose.service':'postgres-metrics'})},web:{'running':True,'labels':dict(labels,**{'com.docker.compose.service':'postgres-metrics-web'})}}
        commands=[]
        def read(args):
            commands.append(args);self.assertEqual(args[0],'ps');return source+'\n'+web
        docker.run.side_effect=read; docker.fact.side_effect=lambda identity:copy.deepcopy(objects[identity])
        with patch.object(manager,'load',return_value=deployment):
            self.assertEqual(manager.status('postgresql','business',{}),('运行中',True))
            objects[web]['running']=False;self.assertEqual(manager.status('postgresql','business',{}),('已停止',False))
            objects[web]['labels']['io.addp.postgres-metrics.owner']='/foreign';self.assertEqual(manager.status('postgresql','business',{}),('归属不符',False))
            objects[web]['labels']['io.addp.postgres-metrics.owner']=str(ROOT);objects[node]['started_at']='changed'
            self.assertEqual(manager.status('postgresql','business',{}),('数据库运行身份已变化',False))
            objects[node]['started_at']=labels['io.addp.postgres-metrics.database-started-at'];objects[node]['labels']=None
            self.assertEqual(manager.status('postgresql','business',{}),('数据库运行身份已变化',False))
            objects[source]['labels']=None
            self.assertEqual(manager.status('postgresql','business',{}),('归属不符',False))
        self.assertTrue(commands)


if __name__ == '__main__': unittest.main()
