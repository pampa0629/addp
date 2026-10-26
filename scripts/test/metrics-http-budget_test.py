"""Pure functional-budget contract. Never starts Docker or a workload."""
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from contextlib import redirect_stdout
from unittest.mock import Mock, patch

spec=importlib.util.spec_from_file_location('http_budget',Path(__file__).with_name('metrics-http-budget.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)


def row(cpu=1,memory=512*1024**2):
    return {'HostConfig':{'NanoCpus':int(cpu*1e9),'Memory':memory,'Tmpfs':{'/tmp':'size=16m,mode=1777'}},'State':{'Status':'running','OOMKilled':False},'SizeRw':100}


class HTTPBudget(unittest.TestCase):
    def test_cpu_memory_write_reservations_and_stop_lines(self):
        self.assertEqual(m.resource_totals([row()],200,300)['private_write_upper_bytes'],16*1024**2+600)
        m.resource_totals([row(4,4*1024**3)],0,0)
        for rows,private,volume in [([row(4.01)],0,0),([row(1,4*1024**3+1)],0,0),([row()],2*1024**3,0),([row(0)],0,0),([row(1,0)],0,0)]:
            with self.assertRaises(RuntimeError):m.resource_totals(rows,private,volume)
        r=row();r['State']['OOMKilled']=True
        with self.assertRaises(RuntimeError):m.resource_totals([r],0,0)
        r=row();r['HostConfig']['Tmpfs']['/tmp']='mode=1777'
        with self.assertRaises(RuntimeError):m.resource_totals([r],0,0)

    def test_sample_keeps_four_second_absolute_deadline(self):
        with patch.object(m.time,'monotonic',side_effect=[0,0,4]),patch.object(m.subprocess,'check_output',return_value=b'') as command:
            with self.assertRaisesRegex(RuntimeError,'sample_deadline'):m.sample({'project':'addp-metrics-t2-'+'a'*32,'work':'/tmp/addp-metrics-t2.fixture'})
        self.assertEqual(command.call_args.kwargs['timeout'],4)

    def test_disappearing_container_reports_exact_step_without_retry_or_raw_error(self):
        ownership={'project':'addp-metrics-t2-'+'a'*32,'work':'/tmp/addp-metrics-t2.fixture'}
        inspected=row();inspected.update(Id='owned',Config={'Labels':{'com.docker.compose.service':'prometheus'}})
        inspected['State']['Running']=True
        secret=b'No such container: private-credential-do-not-log'
        for stage,outputs in [('inspect',[b'owned\n']),('volume_size',[b'owned\n',json.dumps([inspected]).encode()])]:
            with self.subTest(stage=stage):
                failure=subprocess.CalledProcessError(1,['private-command'],stderr=secret)
                with patch.object(m.time,'monotonic',return_value=0),patch.object(m.subprocess,'check_output',side_effect=outputs+[failure]) as command:
                    with self.assertRaises(m.BudgetFailure) as caught:m.sample(ownership)
                self.assertEqual(caught.exception.detail,{'stage':stage,'kind':'container_disappeared','exit_code':1})
                self.assertEqual(command.call_count,len(outputs)+1)
                self.assertNotIn('private',str(caught.exception)+json.dumps(caught.exception.detail))
                self.assertTrue(all(call.kwargs['timeout']==4 for call in command.call_args_list))
                self.assertTrue(all(call.kwargs['stderr']==subprocess.PIPE for call in command.call_args_list))

    def test_sample_timeout_keeps_step_and_does_not_restart_budget(self):
        ownership={'project':'addp-metrics-t2-'+'a'*32,'work':'/tmp/addp-metrics-t2.fixture'}
        failure=subprocess.TimeoutExpired('private-command',4,stderr=b'private-credential')
        with patch.object(m.time,'monotonic',side_effect=[0,0,1,1]),patch.object(m.subprocess,'check_output',side_effect=[b'owned\n',failure]) as command:
            with self.assertRaises(m.BudgetFailure) as caught:m.sample(ownership)
        self.assertEqual(caught.exception.detail,{'stage':'inspect','kind':'deadline'})
        self.assertEqual([call.kwargs['timeout'] for call in command.call_args_list],[4,3])
        self.assertNotIn('private',str(caught.exception)+json.dumps(caught.exception.detail))

    def test_invalid_size_is_failed_with_its_stage_without_leaking_payload(self):
        inspected=row();inspected.update(Id='owned',Config={'Labels':{'com.docker.compose.service':'prometheus'}})
        inspected['State']['Running']=True
        with tempfile.TemporaryDirectory(prefix='addp-metrics-t2.') as directory:
            ownership={'project':'addp-metrics-t2-'+'a'*32,'work':directory}
            for stage,outputs in [('volume_size',[b'owned\n',json.dumps([inspected]).encode()]),('private_directory_size',[b''])]:
                with self.subTest(stage=stage),patch.object(m.time,'monotonic',return_value=0),patch.object(m.subprocess,'check_output',side_effect=outputs+[b'private-credential']):
                    with self.assertRaises(m.BudgetFailure) as caught:m.sample(ownership)
                self.assertEqual(caught.exception.detail,{'stage':stage,'kind':'invalid_size'})
                self.assertNotIn('credential',str(caught.exception)+json.dumps(caught.exception.detail))

    def test_resource_failure_preserves_exceeded_dimension_and_measurements(self):
        # A 2 GiB promtool one-off cannot be hidden beside the 2.75 GiB stack.
        with self.assertRaises(m.BudgetFailure) as caught:
            m.resource_totals([row(2.25,int(2.75*1024**3)),row(1,2*1024**3)],0,0)
        detail=caught.exception.detail
        self.assertEqual(detail['violations'],['memory'])
        self.assertEqual(detail['measurements']['memory_hard_limit_bytes'],int(4.75*1024**3))
        self.assertEqual(detail['measurements']['cpu_hard_limit'],3.25)

    def test_guard_failure_remains_failed_after_cleanup_and_is_visible_in_log_and_file(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);work=root/'addp-metrics-t2.fixture';work.mkdir()
            output=root/'report.json';ownership={'project':'addp-metrics-t2-'+'a'*32,'work':str(work)}
            child=Mock(pid=1234);child.poll.return_value=None
            def start(*args,**kwargs):
                receipt=Path(kwargs['env']['ADDP_HTTP_BUDGET_RECEIPT'])
                receipt.write_text(json.dumps(ownership))
                def cleanup(**kwargs):
                    shutil.rmtree(work)
                    receipt.write_text(json.dumps({**ownership,'cleanup_started':True,'cleanup_verified':True}))
                    return 143
                child.wait.side_effect=cleanup
                return child
            failure=m.BudgetFailure('http_budget_sample_command_failed','inspect','container_disappeared',exit_code=1)
            stdout=io.StringIO()
            with patch.object(m.sys,'argv',['budget','bash','scripts/test/monitor-metrics-gate.sh','--case=http-quality']),patch.dict(os.environ,{'ADDP_HTTP_BUDGET_REPORT':str(output)}),patch.object(m,'input_hashes',return_value={}),patch.object(m.subprocess,'Popen',side_effect=start) as launch,patch.object(m,'sample',side_effect=failure) as sample,patch.object(m.os,'killpg') as stop,redirect_stdout(stdout):
                self.assertEqual(m.main(),1)
            report=json.loads(output.read_text())
            self.assertEqual(report['result'],'failed')
            self.assertTrue(report['cleanup_verified'] and report['private_directory_removed'])
            self.assertEqual(report['failure_detail'],failure.detail)
            summary=json.loads(stdout.getvalue().split('HTTP functional budget result: ',1)[1].splitlines()[0])
            self.assertEqual(summary['failure_detail'],failure.detail)
            self.assertEqual(summary['error_code'],'http_budget_sample_command_failed')
            self.assertNotIn('input_sha256',summary)
            self.assertEqual(launch.call_count,1);self.assertEqual(sample.call_count,1)
            stop.assert_called_once_with(1234,m.signal.SIGTERM)

    def test_preflight_zero_exit_needs_verified_tls_and_exact_zero_business_counts(self):
        for tls_ok,total,expected in [(True,0,0),(False,0,1),(True,1,1)]:
            with self.subTest(tls=tls_ok,total=total),tempfile.TemporaryDirectory() as directory:
                root=Path(directory);work=root/'addp-metrics-t2.fixture';work.mkdir();output=root/'report.json'
                ownership={'project':'addp-metrics-t2-'+'a'*32,'work':str(work)}
                child=Mock(pid=1234);child.poll.side_effect=[None,0]
                def start(*args,**kwargs):
                    receipt=Path(kwargs['env']['ADDP_HTTP_BUDGET_RECEIPT']);receipt.write_text(json.dumps(ownership))
                    def cleanup(**kwargs):
                        shutil.rmtree(work)
                        receipt.write_text(json.dumps({**ownership,'cleanup_started':True,'cleanup_verified':True,'source_tls_proof':{'passed':tls_ok},'request_counts':{'go':total,'python':[0,0],'total':total}}))
                        return 0
                    child.wait.side_effect=cleanup
                    return child
                with patch.object(m.sys,'argv',['budget','bash','scripts/test/monitor-metrics-gate.sh','--case=mtls-preflight']),patch.dict(os.environ,{'ADDP_HTTP_BUDGET_REPORT':str(output)}),patch.object(m,'input_hashes',return_value={}),patch.object(m.subprocess,'Popen',side_effect=start),patch.object(m,'sample',return_value={'duration_seconds':0}),patch.object(m.time,'sleep'),redirect_stdout(io.StringIO()):
                    self.assertEqual(m.main(),expected)
                report=json.loads(output.read_text())
                self.assertEqual(report['case'],'mtls-preflight')
                self.assertEqual(report['result'],'passed' if expected==0 else 'failed')

    def test_existing_standard_case_is_wrapped_and_registered(self):
        root=Path(__file__).resolve().parents[2]
        self.assertIn('metrics-http-budget.py bash scripts/test/monitor-metrics-gate.sh $(MONITOR_METRICS_TEST_ARGS)',(root/'Makefile').read_text())
        gate=(root/'scripts/test/monitor-metrics-gate.sh').read_text()
        for name in ('python-http-agent','python-http-copilot','common-python/','metrics-http-budget.py'):
            self.assertIn(name,gate)
        probe=(root/'scripts/test/monitor-metrics-probe.py').read_text()
        self.assertIn("python-http-metrics-probe.py",probe)
