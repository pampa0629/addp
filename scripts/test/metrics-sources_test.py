import contextlib
import copy
import importlib.util
import io
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('metrics_sources', ROOT/'scripts/infra/metrics-sources.py')
manager = importlib.util.module_from_spec(spec)
spec.loader.exec_module(manager)


class MetricsSourcesTest(unittest.TestCase):
    def test_host_entry_delegates_only_host_actions_from_another_directory(self):
        with tempfile.TemporaryDirectory(prefix='addp host metrics ') as folder:
            root = Path(folder)
            entry = root/'scripts/infra/host-metrics.sh'
            entry.parent.mkdir(parents=True)
            shutil.copy2(ROOT/'scripts/infra/host-metrics.sh', entry)
            entry.with_name('metrics-sources.py').write_text(
                'import sys\nprint(" ".join(sys.argv[1:]))\nsys.exit(19)\n')
            for action in ('check', 'up', 'status', 'down', 'recover'):
                with self.subTest(action=action):
                    result = subprocess.run([entry, action], cwd=root, capture_output=True, text=True, timeout=5)
                    self.assertEqual(result.returncode, 19, result.stderr)
                    self.assertEqual(result.stdout.strip(), f'{action} --scope host --kind node')
            result = subprocess.run([entry, 'up', '--scope', 'infra'], cwd=root,
                                    capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 2)
            self.assertEqual(result.stdout, '')

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

    def test_recovery_requires_explicit_host_selection(self):
        env = {value[1]+'_ENABLED':'true' for value in manager.PROVIDERS.values()}
        code, output, mocks = self.invoke('recover', env=env, apply={'return_value':'已恢复'})
        self.assertEqual(code, 0)
        self.assertEqual([(call.args[0],call.args[1],call.args[2]) for call in mocks['apply'].call_args_list], [('recover',kind,scope) for scope,kind in manager.selection((),()) if scope!='host'])
        self.assertIn('主机须显式指定 --scope host',output)

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
        with patch.object(manager.subprocess, 'run', return_value=result), self.assertRaisesRegex(ValueError, '更新 PostgreSQL 认证规则阶段失败.*清理未确认'):
            manager.apply('up','postgresql','business',{})

    def test_engine_failure_stages_are_chinese_bounded_and_keep_unknown_results(self):
        cases = {
            'session-check':'核对采集会话', 'transport-check':'校验采集端口与 TLS',
            'private-state':'核对私有安装状态', 'authentication-read':'读取 PostgreSQL 认证规则',
            'account-install':'安装采集账号', 'authentication-update':'更新 PostgreSQL 认证规则',
            'account-enable':'启用采集账号', 'connection-check':'验证采集连接',
            'source-files':'准备采集来源文件', 'identity-recheck':'重新核对引擎身份',
            'source-start':'部署采集来源', 'source-health':'检查来源容器运行状态',
            'deployment-check':'核对引擎部署', 'account-plan':'准备采集账号配置',
            'account-update':'核对及更新采集账号', 'source-check':'核验既有采集来源',
            'source-stop':'移除采集来源', 'source-build':'构建采集来源镜像',
            'source-bootstrap-image':'核对 MinIO 容器原镜像',
        }
        for kind in ('postgresql','redis','minio'):
            for stage,label in cases.items():
                result=subprocess.CompletedProcess([],1,'private-output','[stage='+stage+'] [cleanup=failed] private-password /private/path')
                with patch.object(manager.subprocess,'run',return_value=result),self.assertRaises(ValueError) as error:
                    manager.apply('up',kind,'infra',{})
                text=str(error.exception)
                self.assertIn(label,text);self.assertIn('阶段失败',text);self.assertIn('请核对',text)
                self.assertTrue(text.endswith('清理未确认'))
                self.assertNotIn('private',text);self.assertNotIn(stage,text)
        for stage in ('private-password','unknown-stage'):
            result=subprocess.CompletedProcess([],1,'private-output','[stage='+stage+'] [cleanup=failed] private-password')
            with patch.object(manager.subprocess,'run',return_value=result),self.assertRaises(ValueError) as error:
                manager.apply('recover','minio','business',{})
            self.assertEqual(str(error.exception),'操作失败；清理未确认')

    def test_tls_reason_is_identical_for_check_and_application_without_private_output(self):
        for reason,phrase in {'chain':'证书链','not-yet-valid':'尚未生效','expired':'已过期',
                'purpose':'服务器认证','name':'IP SAN','key':'私钥不匹配','unknown':'原因未能确认'}.items():
            for kind in manager.PROVIDERS:
                failure=ValueError('private-password /private/server.crt')
                failure.source_tls_reason=reason;failure.observation_stage='transport-check'
                scope='host' if kind=='node' else 'infra'
                code,output,_=self.invoke('check',[scope],[kind],env={manager.PROVIDERS[kind][1]+'_ENABLED':'true'},check={'side_effect':failure})
                self.assertEqual(code,1);self.assertIn(phrase,output);self.assertNotIn('private',output)
                for action in ('up','recover'):
                    completed=subprocess.CompletedProcess([],1,'private-output',
                        '[stage=transport-check] [tls-reason='+reason+'] [cleanup=failed] private-password /private/server.crt')
                    with patch.object(manager.subprocess,'run',return_value=completed),self.assertRaises(ValueError) as error:
                        manager.apply(action,kind,'infra',{})
                    self.assertIn(phrase,str(error.exception));self.assertTrue(str(error.exception).endswith('清理未确认'))
                    self.assertNotIn('private',str(error.exception))

    def test_tls_reason_requires_unique_known_transport_evidence(self):
        for markers in ('[tls-reason=private-password]','[tls-reason=chain] [tls-reason=name]',
                '[tls-reason=chain] [tls-reason=chain]',''):
            result=subprocess.CompletedProcess([],1,'','[stage=transport-check] '+markers+' /private/path')
            with patch.object(manager.subprocess,'run',return_value=result),self.assertRaises(ValueError) as error:
                manager.apply('up','minio','business',{})
            self.assertEqual(str(error.exception),'操作失败：'+manager.FAILURE_STAGES['transport-check'])
        result=subprocess.CompletedProcess([],1,'','[stage=source-build] [tls-reason=chain] private-password')
        with patch.object(manager.subprocess,'run',return_value=result),self.assertRaises(ValueError) as error:
            manager.apply('up','redis','infra',{})
        self.assertEqual(str(error.exception),'操作失败：'+manager.FAILURE_STAGES['source-build'])

    def test_image_recovery_failure_is_actionable_without_echoing_provider_output(self):
        for kind in ('redis','minio','node'):
            result=subprocess.CompletedProcess([],1,'private-output','[reason=source-image-unavailable] [cleanup=failed] private-password')
            with patch.object(manager.subprocess,'run',return_value=result),self.assertRaisesRegex(ValueError,'原采集镜像不可用.*重新构建') as error:
                manager.apply('recover',kind,'infra',{})
            self.assertNotIn('private',str(error.exception))
            self.assertIn('清理未确认',str(error.exception))
        result=subprocess.CompletedProcess([],1,'','[reason=private-password]')
        with patch.object(manager.subprocess,'run',return_value=result),self.assertRaises(ValueError) as error:
            manager.apply('recover','minio','infra',{})
        self.assertEqual(str(error.exception),'操作失败')

    def test_host_recovery_uses_provider_and_closed_proof_diagnosis(self):
        code, output, calls = self.invoke('recover',['host'],['node'],env={'ADDP_NODE_METRICS_ENABLED':'true'},apply={'return_value':'已检查／恢复'})
        self.assertEqual(code,0)
        calls['apply'].assert_called_once_with('recover','node','host',{'ADDP_NODE_METRICS_ENABLED':'true'})
        reasons = {
            'node-proof-missing':'安装证明缺失', 'node-proof-disabled':'安装已停用',
            'node-proof-invalid':'安装证明读取或校验失败', 'node-source-incomplete':'来源组件不完整',
            'node-ownership-rejected':'资源归属校验失败', 'node-inputs-changed':'部署输入已变化',
            'node-installation-changed':'安装身份或安全配置已变化',
        }
        for reason,message in reasons.items():
            result = subprocess.CompletedProcess([],1,'private-output','[reason='+reason+'] private-password')
            with patch.object(manager.subprocess,'run',return_value=result),self.assertRaisesRegex(ValueError,message) as error:
                manager.apply('recover','node','host',{})
            self.assertNotIn('private',str(error.exception))

    def test_unknown_failure_never_invents_a_diagnosis_and_preserves_cleanup_warning(self):
        for reason in ('unknown-secret','node-proof-unavailable'):
            result = subprocess.CompletedProcess([],1,'private-output','[reason='+reason+'] [cleanup=failed] /private/secret')
            with patch.object(manager.subprocess,'run',return_value=result),self.assertRaises(ValueError) as error:
                manager.apply('recover','node','host',{})
            self.assertEqual(str(error.exception),'操作失败；清理未确认')

    def test_standard_lifecycles_use_one_coordinator(self):
        infra = (ROOT/'scripts/infra/up.sh').read_text()
        self.assertEqual(infra.count('metrics-sources.py" up --resolved-env --scope infra'), 2)
        business = (ROOT/'business/scripts/start.sh').read_text().split('observation_kinds=()')[-1]
        self.assertIn('metrics-sources.py" up --resolved-env --scope business', business)
        self.assertNotIn('lifecycle.py" up --scope business', business)
        recovery = (ROOT/'scripts/utils/observability-env.sh').read_text().split('addp_recover_metrics_sources() {')[1]
        self.assertIn('metrics-sources.py" recover --scope host --scope infra --scope business --kind node --kind postgresql --kind redis --kind minio', recovery)
        self.assertNotIn('source "$config"', recovery)
        for path in ('scripts/infra/up.sh','scripts/infra/down.sh','business/scripts/start.sh','business/scripts/stop.sh','scripts/utils/observability-env.sh'):
            subprocess.run(['bash','-n',str(ROOT/path)],check=True,capture_output=True)

    def test_startup_summary_is_read_only_and_preserves_independent_failures(self):
        library = ROOT/'scripts/utils/observability-env.sh'
        command = '''source "$1"
addp_infra_verify_container() { printf '%s\\n' "verify:$*" >> "$CALLS"; [[ "$MODE" != foreign ]]; }
docker() {
  [[ "$1" == inspect && "$2" == --format && $# == 4 ]] || exit 88
  printf '%s\\n' "inspect:${4}" >> "$CALLS"
  case "$MODE" in stopped) echo false:healthy ;; unhealthy) echo true:unhealthy ;; *) echo true:healthy ;; esac
}
python3() {
  [[ "$*" == "$PROJECT_ROOT/scripts/infra/metrics-sources.py status" ]] || exit 89
  echo sources-status >> "$CALLS"
  echo 'fixture sources status'
  [[ "$MODE" != sources-failed ]]
}
addp_startup_metrics_summary
'''
        for mode in ('healthy','stopped','unhealthy','foreign','sources-failed','disabled','invalid'):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as directory:
                calls=Path(directory)/'calls'
                env=dict(os.environ,PROJECT_ROOT='/fixture',CALLS=str(calls),MODE=mode,
                         ADDP_OBSERVABILITY_METRICS_ENABLED='false' if mode=='disabled' else 'invalid' if mode=='invalid' else 'true',
                         ADDP_METRICS_CONTROL_MTLS_ENABLED='true')
                result=subprocess.run(['bash','-eu','-c',command,'test',str(library)],env=env,capture_output=True,text=True,timeout=5)
                self.assertEqual(result.returncode,int(mode not in ('healthy','disabled')),result.stdout+result.stderr)
                self.assertIn('fixture sources status',result.stdout)
                self.assertIn('采样',result.stdout)
                self.assertIn('Monitor',result.stdout)
                values=calls.read_text().splitlines()
                self.assertEqual(values.count('sources-status'),1)
                if mode in ('disabled','invalid'):
                    self.assertEqual(values,['sources-status'])
                elif mode=='foreign':
                    self.assertFalse(any(x.startswith('inspect:') for x in values))
                else:
                    self.assertIn('inspect:addp-prometheus',values)

    def test_full_start_summary_is_guarded_at_end_and_does_not_mask_failure(self):
        source=(ROOT/'scripts/dev/start.sh').read_text()
        block=source[source.index('# Full local startup monitoring summary'):]
        self.assertGreater(source.index('# Full local startup monitoring summary'),source.rindex('require_started_backends || exit 1'))
        for full,online,selected in [('true','0',True),('false','0',False),('true','1',False)]:
            for previous in (0,1):
                for failure in (0,1):
                    with self.subTest(full=full,online=online,previous=previous,failure=failure):
                        reasons="('采集来源恢复')" if previous else '()'
                        command=f'infra_optional_result={previous}; infra_optional_failures={reasons}; addp_startup_metrics_summary() {{ echo SUMMARY; return {failure}; }};\n'+block
                        result=subprocess.run(['bash','-eu','-c',command],env=dict(os.environ,START_ALL=full,ADDP_ONLINE_HOST=online),capture_output=True,text=True,timeout=5)
                        expected=int(bool(previous or selected and failure))
                        self.assertEqual(result.returncode,expected,result.stdout+result.stderr)
                        self.assertEqual('SUMMARY' in result.stdout,selected)
                        self.assertEqual('业务已启动' in result.stderr,bool(selected and expected))
                        self.assertEqual('启动命令因上述步骤返回非零退出码' in result.stderr,bool(selected and expected))
                        self.assertEqual('采集来源恢复' in result.stderr,bool(selected and previous))
                        self.assertEqual('末尾监控摘要' in result.stderr,bool(selected and failure))

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
        group=manager.load(ROOT/'scripts/utils/metrics-project.py');deployment.inventory.metrics_project=group
        project = group.PROJECT; node='a'*64; source='b'*64; web='c'*64
        deployment.inventory.SCOPES={'business':('business',ROOT/'business')}
        database={'running':True,'started_at':'2026-11-03T00:00:00Z','labels':{'com.docker.compose.project':'business','com.docker.compose.service':'postgres','com.docker.compose.project.working_dir':str(ROOT/'business')}}
        labels={'com.docker.compose.project':project,'io.addp.postgres-metrics.owner':str(ROOT),'io.addp.postgres-metrics.scope':'business','io.addp.postgres-metrics.database-id':node,'io.addp.postgres-metrics.database-started-at':database['started_at']}
        objects={node:database,source:{'running':True,'labels':dict(labels,**{'com.docker.compose.service':'business-postgres-metrics'})},web:{'running':True,'labels':dict(labels,**{'com.docker.compose.service':'business-postgres-metrics-web'})}}
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

        for state, running, expected in (('运行中', True, 0), ('已停止', False, 1)):
            with self.subTest(state=state):
                code, output, mocks = self.invoke(
                    'status', ['business'], ['redis'], env={'ADDP_REDIS_METRICS_ENABLED':'true'},
                    status={'return_value':(state, running)}, apply={})
                self.assertEqual(code, expected)
                self.assertIn(f'当前运行状态检查：共 1 项，异常 {expected} 项', output)
                mocks['apply'].assert_not_called()


if __name__ == '__main__': unittest.main()
