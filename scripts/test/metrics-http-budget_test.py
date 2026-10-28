"""Pure functional-budget contract. Never starts Docker or a workload."""
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import threading
import unittest
from contextlib import redirect_stdout
from unittest.mock import Mock, patch

spec=importlib.util.spec_from_file_location('http_budget',Path(__file__).with_name('metrics-http-budget.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)


def row(cpu=1,memory=512*1024**2):
    return {'HostConfig':{'NanoCpus':int(cpu*1e9),'Memory':memory,'Tmpfs':{'/tmp':'size=16m,mode=1777'}},'State':{'Status':'running','OOMKilled':False},'SizeRw':100}


class HTTPBudget(unittest.TestCase):
    def test_cleanup_cannot_delete_during_an_active_sample(self):
        with tempfile.TemporaryDirectory() as directory:
            receipt=Path(directory)/'ownership.json';owner={'project':'owned','work':directory};receipt.write_text(json.dumps(owner))
            entered=threading.Event();release=threading.Event();cleaning=threading.Event();deleted=threading.Event();errors=[]
            acquire=m.acquire_lock
            def observe(fd,end,stage,stopped):
                if stage=='cleanup_handoff':cleaning.set()
                return acquire(fd,end,stage,stopped)
            def read(*args):
                entered.set()
                if not release.wait(2):raise RuntimeError('test_release_missing')
                return {'duration_seconds':.01}
            def sample():
                try:m.owned_sample(owner,receipt)
                except BaseException as error:errors.append(error)
            def cleanup():
                try:
                    with open(str(receipt)+'.lock','a') as lock:
                        m.begin_cleanup(receipt,lock.fileno());deleted.set()
                except BaseException as error:errors.append(error)
            with patch.object(m,'sample',side_effect=read),patch.object(m,'acquire_lock',side_effect=observe):
                first=threading.Thread(target=sample);second=threading.Thread(target=cleanup)
                try:
                    first.start();self.assertTrue(entered.wait(2));second.start();self.assertTrue(cleaning.wait(2))
                    self.assertFalse(deleted.is_set());self.assertTrue(json.loads(receipt.read_text())['cleanup_started'])
                finally:
                    release.set();first.join(3)
                    if second.ident is not None:second.join(3)
                self.assertFalse(first.is_alive() or second.is_alive());self.assertFalse(errors);self.assertTrue(deleted.is_set())

    def test_cleanup_first_stops_new_sampling_without_waiting_for_deletion(self):
        with tempfile.TemporaryDirectory() as directory:
            receipt=Path(directory)/'ownership.json';owner={'project':'owned','work':directory};receipt.write_text(json.dumps(owner))
            with open(str(receipt)+'.lock','a') as lock,patch.object(m,'sample') as read:
                m.begin_cleanup(receipt,lock.fileno())
                self.assertIsNone(m.owned_sample(owner,receipt));read.assert_not_called()
                with open(str(receipt)+'.lock','a') as other:
                    with self.assertRaises(BlockingIOError):m.fcntl.flock(other,m.fcntl.LOCK_EX|m.fcntl.LOCK_NB)

    def test_waiting_for_lock_reduces_the_real_command_budget(self):
        with tempfile.TemporaryDirectory(prefix='addp-metrics-t2.') as directory:
            receipt=Path(directory)/'ownership.json';owner={'project':'addp-metrics-t2-'+'a'*32,'work':directory};receipt.write_text(json.dumps(owner))
            with patch.object(m.time,'monotonic',return_value=10) as clock:
                def acquire(fd,end,stage,stopped):
                    self.assertEqual(end,14);clock.return_value=11;return True
                with patch.object(m,'acquire_lock',side_effect=acquire),patch.object(m.subprocess,'check_output',side_effect=[b'',b'0\n']) as execute:
                    result=m.owned_sample(owner,receipt)
                self.assertEqual(result['lock_wait_seconds'],1)
                self.assertEqual([c.kwargs['timeout'] for c in execute.call_args_list],[3,3])

    def test_handoff_wait_uses_original_sample_deadline_and_rejects_foreign_owner(self):
        with tempfile.TemporaryDirectory() as directory:
            receipt=Path(directory)/'ownership.json';owner={'project':'owned','work':directory};receipt.write_text(json.dumps(owner))
            with patch.object(m.time,'monotonic',side_effect=[0,4]),patch.object(m,'sample') as read:
                with self.assertRaisesRegex(m.BudgetFailure,'handoff_deadline'):m.owned_sample(owner,receipt)
                read.assert_not_called()
            receipt.write_text(json.dumps({**owner,'project':'foreign'}))
            with self.assertRaisesRegex(RuntimeError,'ownership_invalid'):m.owned_sample(owner,receipt)

    def test_real_read_failure_releases_lock_and_cleanup_keeps_original_120_seconds(self):
        with tempfile.TemporaryDirectory() as directory:
            receipt=Path(directory)/'ownership.json';owner={'project':'owned','work':directory};receipt.write_text(json.dumps(owner))
            failure=m.BudgetFailure('http_budget_sample_command_failed','inspect','command_failed',exit_code=1)
            with patch.object(m,'sample',side_effect=failure):
                with self.assertRaises(m.BudgetFailure) as caught:m.owned_sample(owner,receipt)
            self.assertIs(caught.exception,failure)
            with open(str(receipt)+'.lock','a') as lock,patch.object(m.time,'monotonic',return_value=100):
                value=m.begin_cleanup(receipt,lock.fileno())
                self.assertEqual(value['cleanup_deadline_monotonic'],220)

    def test_cleanup_lock_timeout_is_failed_without_unbounded_wait(self):
        with tempfile.TemporaryDirectory() as directory:
            receipt=Path(directory)/'ownership.json';receipt.write_text(json.dumps({'project':'owned','work':directory}))
            with open(str(receipt)+'.lock','a') as lock,patch.object(m.time,'monotonic',side_effect=[0,120,120]):
                with self.assertRaisesRegex(m.BudgetFailure,'handoff_deadline'):m.begin_cleanup(receipt,lock.fileno())
            value=json.loads(receipt.read_text());self.assertTrue(value['cleanup_overdue']);self.assertFalse(value['cleanup_verified'])

    def test_unlock_failure_does_not_mask_the_first_real_read_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            receipt=Path(directory)/'ownership.json';owner={'project':'owned','work':directory};receipt.write_text(json.dumps(owner))
            failure=m.BudgetFailure('http_budget_sample_command_failed','inspect','command_failed',exit_code=1)
            with patch.object(m,'sample',side_effect=failure),patch.object(m.fcntl,'flock',side_effect=[None,OSError('private-token')]):
                with self.assertRaises(m.BudgetFailure) as caught:m.owned_sample(owner,receipt)
            self.assertIs(caught.exception,failure)

    def test_shell_holds_cleanup_lock_after_python_returns(self):
        with tempfile.TemporaryDirectory() as directory:
            receipt=Path(directory)/'ownership.json';receipt.write_text(json.dumps({'project':'owned','work':directory}))
            script='''exec 9>"$1.lock"
python3 - "$1" "$2" <<'PYLOCK'
import runpy,sys
from pathlib import Path
runpy.run_path(sys.argv[2])['begin_cleanup'](Path(sys.argv[1]),9)
PYLOCK
echo locked
read -r release
'''
            child=subprocess.Popen(['bash','-c',script,'fixture',str(receipt),str(Path(m.__file__))],stdin=subprocess.PIPE,stdout=subprocess.PIPE,text=True)
            try:
                self.assertEqual(child.stdout.readline().strip(),'locked')
                with open(str(receipt)+'.lock','a') as lock:
                    with self.assertRaises(BlockingIOError):m.fcntl.flock(lock,m.fcntl.LOCK_EX|m.fcntl.LOCK_NB)
                child.communicate('release\n',timeout=3);self.assertEqual(child.returncode,0)
                with open(str(receipt)+'.lock','a') as lock:m.fcntl.flock(lock,m.fcntl.LOCK_EX|m.fcntl.LOCK_NB)
            finally:
                if child.poll() is None:child.kill();child.communicate()

    def test_directory_diagnostic_is_closed_bounded_and_never_retains_text(self):
        secret=b'/private/credential-do-not-log'
        cases=[(b'du: '+secret+b': No such file or directory\n','path_disappeared'),
               (b'du: '+secret+b': Permission denied\n','permission_denied'),
               (b'du: '+secret+b': Operation not permitted\n','permission_denied'),
               (b'du: '+secret+b': Input/output error\n','io_error'),
               (b'du: '+secret+b': private failure\n','unclassified'),
               (b'\xff','unclassified'),
               (b'','empty'),(b'x'*4097,'over_budget'),
               (b'du: a: No such file or directory\ndu: b: Permission denied\n','mixed'),
               (b'du: a: No such file or directory\nunknown\n','mixed')]
        for stderr,expected in cases:
            with self.subTest(expected=expected):
                result=m.directory_failure_diagnostic(stderr)
                self.assertEqual(result,{'directory_error':expected,'stderr_bytes':len(stderr),'stderr_over_budget':len(stderr)>4096})
                self.assertNotIn('credential',json.dumps(result))
        suffix=b': No such file or directory\n'
        exact=b'du: '+b'x'*(4096-4-len(suffix))+suffix
        self.assertEqual(len(exact),4096)
        self.assertEqual(m.directory_failure_diagnostic(exact)['directory_error'],'path_disappeared')

    def test_private_directory_failure_preserves_same_deadline_and_separate_timings(self):
        with tempfile.TemporaryDirectory(prefix='addp-metrics-t2.') as directory:
            failure=subprocess.CalledProcessError(1,['du','-sk','private-command'],stderr=b'du: private-token: No such file or directory\n')
            with patch.object(m.time,'monotonic',side_effect=[10,10.2,10.5,11.25]),patch.object(m.subprocess,'check_output',side_effect=[b'',failure]) as command:
                with self.assertRaises(m.BudgetFailure) as caught:m.sample({'project':'addp-metrics-t2-'+'a'*32,'work':directory},14)
            facts=caught.exception.detail
            self.assertEqual(facts['stage'],'private_directory_size')
            self.assertEqual(facts['kind'],'command_failed')
            self.assertEqual(facts['exit_code'],1)
            self.assertEqual(facts['directory_error'],'path_disappeared')
            self.assertEqual((facts['command_seconds'],facts['sample_seconds'],facts['command_budget_seconds']),(.75,1.25,3.5))
            self.assertEqual([c.kwargs['timeout'] for c in command.call_args_list],[4,3.5])
            self.assertNotIn('env',command.call_args_list[0].kwargs)
            self.assertEqual(command.call_args_list[1].kwargs['env']['LC_ALL'],'C')
            self.assertEqual(command.call_count,2)
            self.assertNotIn('private-token',str(caught.exception)+json.dumps(facts))

    def test_directory_timeout_or_spawn_failure_keeps_shared_budget_without_raw_error(self):
        for error,kind,finish in ((subprocess.TimeoutExpired('private-command',3.5,stderr=b'private-token'),'deadline',4),
                                  (OSError('private-token'),'command_unavailable',.75)):
            with self.subTest(kind=kind),tempfile.TemporaryDirectory(prefix='addp-metrics-t2.') as directory:
                with patch.object(m.time,'monotonic',side_effect=[0,.2,.5,finish]),patch.object(m.subprocess,'check_output',side_effect=[b'',error]) as command:
                    with self.assertRaises(m.BudgetFailure) as caught:m.sample({'project':'addp-metrics-t2-'+'a'*32,'work':directory},4)
                facts=caught.exception.detail
                self.assertEqual(facts['kind'],kind)
                self.assertEqual((facts['command_seconds'],facts['sample_seconds'],facts['command_budget_seconds']),(finish-.5,finish,3.5))
                self.assertEqual(command.call_count,2)
                self.assertEqual(command.call_args.kwargs['timeout'],3.5)
                self.assertNotIn('private-token',json.dumps(facts)+str(caught.exception))

    def phase_record(self, limit, healthy=True):
        return {'phase':'healthy' if healthy else 'failure','failure_attempts':limit,'healthy_attempts':limit if healthy else 0,
                'failure_limit':limit,'healthy_limit':limit,'failure_last_ms':178000,'healthy_last_ms':390000 if healthy else None}

    def put_traffic(self, source, key, limit, healthy=True):
        stem={'go':'process-1.json','python_1':'python-1','python_2':'python-2'}[key]
        proof=self.phase_record(limit,healthy)
        (source/(stem+'.http-load.json')).write_text(json.dumps(proof))
        (source/(stem+'.requests')).write_text(str(limit*(2 if healthy else 1)))
        return proof

    def test_early_go_failure_keeps_phase_counts_without_entering_python(self):
        with tempfile.TemporaryDirectory() as directory:
            source=Path(directory);self.put_traffic(source,'go',90,False)
            result=m.request_traffic(source)
            self.assertEqual(result['request_counts'],{'go':90,'python':[0,0],'total':90})
            self.assertIsNone(result['request_phases']['python_1'])
            self.assertFalse(m.complete_request_traffic(result['request_counts'],result['request_phases']))

    def test_three_sources_need_both_phases_with_matching_original_total(self):
        with tempfile.TemporaryDirectory() as directory:
            source=Path(directory)
            for key,limit in (('go',90),('python_1',45),('python_2',15)):self.put_traffic(source,key,limit)
            result=m.request_traffic(source)
            self.assertEqual(result['request_counts'],{'go':180,'python':[90,30],'total':300})
            self.assertTrue(m.complete_request_traffic(result['request_counts'],result['request_phases']))
            result['request_counts']['go']-=1
            self.assertFalse(m.complete_request_traffic(result['request_counts'],result['request_phases']))

    def test_phase_receipts_reject_missing_corrupt_extra_secret_and_mismatched_counts(self):
        with tempfile.TemporaryDirectory() as directory:
            source=Path(directory);(source/'process-1.json').write_text('{}')
            with self.assertRaisesRegex(RuntimeError,'phase_missing'):m.request_traffic(source)
            for change in ('invalid-json','extra','bool','over','clock','mismatch'):
                with self.subTest(change=change):
                    proof=self.put_traffic(source,'go',90)
                    if change=='extra':proof['credential']='never-log-this'
                    if change=='bool':proof['healthy_attempts']=True
                    if change=='over':proof['healthy_attempts']=91
                    if change=='clock':proof['healthy_last_ms']=-1
                    (source/'process-1.json.http-load.json').write_text('never-log-this' if change=='invalid-json' else json.dumps(proof))
                    if change=='mismatch':(source/'process-1.json.requests').write_text('179')
                    with self.assertRaises(RuntimeError) as caught:m.request_traffic(source)
                    self.assertNotIn('never-log-this',str(caught.exception))

    def test_cleanup_collects_actual_phase_files_after_stopping_sources_before_removal(self):
        gate=Path(__file__).with_name('monitor-metrics-gate.sh').read_text()
        body=gate.split("<<'REQUEST_TRAFFIC'\n",1)[1].split('\nREQUEST_TRAFFIC',1)[0]
        self.assertLess(gate.index('compose --profile python-http --profile block-capacity down --volumes'),gate.index("<<'REQUEST_TRAFFIC'"))
        self.assertLess(gate.index("<<'REQUEST_TRAFFIC'"),gate.index('rm -rf "$WORK_DIR"'))
        with tempfile.TemporaryDirectory() as directory:
            work=Path(directory);source=work/'source';source.mkdir();receipt=work/'receipt.json'
            self.put_traffic(source,'go',90,False);receipt.write_text(json.dumps({'work':str(work)}))
            result=subprocess.run(['python3','-',str(receipt),str(Path(m.__file__))],input=body,capture_output=True,text=True,timeout=5)
            self.assertEqual(result.returncode,0,result.stderr)
            self.assertEqual(json.loads(receipt.read_text())['request_counts']['go'],90)
            value={'stage':'imports','status':'failed','error':'module_missing','exit_code':None,'log_bytes':0,'log_over_budget':False,'component':'unknown','checkpoint':'none','versions':{},'dependency_mount':{'noexec':False,'nosuid':True,'nodev':True},'native_imported':False}
            (source/'python-1.startup.json').write_text(json.dumps(value))
            result=subprocess.run(['python3','-',str(receipt),str(Path(m.__file__))],input=body,capture_output=True,text=True,timeout=5)
            self.assertEqual(result.returncode,0,result.stderr)
            self.assertEqual(json.loads(receipt.read_text())['python_sources']['startup']['python_1'],value)
            (source/'process-1.json.http-load.json').write_text('{"credential":"never-log-this"}')
            previous=json.loads(receipt.read_text());previous.pop('python_sources');receipt.write_text(json.dumps(previous))
            result=subprocess.run(['python3','-',str(receipt),str(Path(m.__file__))],input=body,capture_output=True,text=True,timeout=5)
            self.assertEqual(result.returncode,1)
            self.assertEqual(json.loads(receipt.read_text())['request_traffic_error'],'invalid_or_missing_phase_receipt')
            self.assertEqual(json.loads(receipt.read_text())['python_sources']['startup']['python_1'],value)
            self.assertNotIn('never-log-this',result.stdout+result.stderr)

    def test_actual_cleanup_selects_the_same_python_profile_without_starting_services(self):
        gate=Path(__file__).with_name('monitor-metrics-gate.sh').read_text()
        body=gate.split('cleanup(){',1)[1].split('\ntrap cleanup EXIT',1)[0]
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);work=root/'work';work.mkdir();calls=root/'calls'
            script='''compose(){ echo "$*" >> "$CALLS"; }
docker(){ :; }
cleanup(){'''+body+'\nfalse\ncleanup\n'
            result=subprocess.run(['bash','-c',script],env=dict(os.environ,WORK_DIR=str(work),COMPOSE_PROJECT='owned',CALLS=str(calls),ADDP_HTTP_BUDGET_RECEIPT=''),capture_output=True,text=True,timeout=5)
            self.assertEqual(result.returncode,1)
            self.assertEqual([c for c in calls.read_text().splitlines() if ' down ' in ' '+c+' '],['--profile python-http --profile block-capacity down --volumes --remove-orphans'])
            self.assertFalse(work.exists())

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
        with patch.object(m.time,'monotonic',side_effect=[0,4]),patch.object(m.subprocess,'check_output',return_value=b'') as command:
            with self.assertRaisesRegex(RuntimeError,'sample_deadline'):m.sample({'project':'addp-metrics-t2-'+'a'*32,'work':'/tmp/addp-metrics-t2.fixture'},4)
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
                    with self.assertRaises(m.BudgetFailure) as caught:m.sample(ownership,4)
                self.assertEqual(caught.exception.detail,{'stage':stage,'kind':'container_disappeared','exit_code':1,
                                 'command_seconds':0,'sample_seconds':0,'command_budget_seconds':4})
                self.assertEqual(command.call_count,len(outputs)+1)
                self.assertNotIn('private',str(caught.exception)+json.dumps(caught.exception.detail))
                self.assertTrue(all(call.kwargs['timeout']==4 for call in command.call_args_list))
                self.assertTrue(all(call.kwargs['stderr']==subprocess.PIPE for call in command.call_args_list))

    def test_sample_timeout_keeps_step_and_does_not_restart_budget(self):
        ownership={'project':'addp-metrics-t2-'+'a'*32,'work':'/tmp/addp-metrics-t2.fixture'}
        failure=subprocess.TimeoutExpired('private-command',4,stderr=b'private-credential')
        with patch.object(m.time,'monotonic',side_effect=[0,1,1,3]),patch.object(m.subprocess,'check_output',side_effect=[b'owned\n',failure]) as command:
            with self.assertRaises(m.BudgetFailure) as caught:m.sample(ownership,4)
        self.assertEqual(caught.exception.detail,{'stage':'inspect','kind':'deadline','command_seconds':2,
                         'sample_seconds':3,'command_budget_seconds':3})
        self.assertEqual([call.kwargs['timeout'] for call in command.call_args_list],[4,3])
        self.assertNotIn('private',str(caught.exception)+json.dumps(caught.exception.detail))

    def test_invalid_size_is_failed_with_its_stage_without_leaking_payload(self):
        inspected=row();inspected.update(Id='owned',Config={'Labels':{'com.docker.compose.service':'prometheus'}})
        inspected['State']['Running']=True
        with tempfile.TemporaryDirectory(prefix='addp-metrics-t2.') as directory:
            ownership={'project':'addp-metrics-t2-'+'a'*32,'work':directory}
            for stage,outputs in [('volume_size',[b'owned\n',json.dumps([inspected]).encode()]),('private_directory_size',[b''])]:
                with self.subTest(stage=stage),patch.object(m.time,'monotonic',return_value=0),patch.object(m.subprocess,'check_output',side_effect=outputs+[b'private-credential']):
                    with self.assertRaises(m.BudgetFailure) as caught:m.sample(ownership,4)
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
            failure=m.BudgetFailure('http_budget_sample_command_failed','private_directory_size','command_failed',exit_code=1,
                                    command_seconds=.75,sample_seconds=1.25,command_budget_seconds=3.5,
                                    **m.directory_failure_diagnostic(b'du: private-token: No such file or directory\n'))
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
            self.assertNotIn('private-token',stdout.getvalue()+output.read_text())
            self.assertEqual(launch.call_count,1);self.assertEqual(sample.call_count,1)
            stop.assert_called_once_with(1234,m.signal.SIGTERM)

    def test_guard_failure_during_owned_cleanup_does_not_interrupt_cleanup_or_pass(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);work=root/'addp-metrics-t2.fixture';work.mkdir();output=root/'report.json'
            ownership={'project':'addp-metrics-t2-'+'a'*32,'work':str(work)}
            child=Mock(pid=1234);child.poll.return_value=None;receipt=None
            def start(*args,**kwargs):
                nonlocal receipt
                receipt=Path(kwargs['env']['ADDP_HTTP_BUDGET_RECEIPT']);receipt.write_text(json.dumps(ownership))
                def cleanup(**kwargs):
                    shutil.rmtree(work);receipt.write_text(json.dumps({**ownership,'cleanup_started':True,'cleanup_verified':True}))
                    return 1
                child.wait.side_effect=cleanup
                return child
            def sample(*args):
                receipt.write_text(json.dumps({**ownership,'cleanup_started':True}))
                raise m.BudgetFailure('http_budget_sample_command_failed','inspect','command_failed',exit_code=1)
            with patch.object(m.sys,'argv',['budget','bash','scripts/test/monitor-metrics-gate.sh','--case=http-quality']),patch.dict(os.environ,{'ADDP_HTTP_BUDGET_REPORT':str(output)}),patch.object(m,'input_hashes',return_value={}),patch.object(m.subprocess,'Popen',side_effect=start),patch.object(m,'sample',side_effect=sample) as sampled,patch.object(m.os,'killpg') as stop,redirect_stdout(io.StringIO()):
                self.assertEqual(m.main(),1)
            stop.assert_not_called();self.assertEqual(sampled.call_count,1)
            report=json.loads(output.read_text())
            self.assertEqual(report['result'],'failed');self.assertTrue(report['cleanup_verified'])

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

    def test_full_http_zero_exit_needs_three_valid_reserved_recovery_receipts(self):
        phases={key:self.phase_record(limit) for key,limit in (('go',90),('python_1',45),('python_2',15))}
        counts={'go':180,'python':[90,30],'total':300}
        for proof,overdue,expected in ((phases,False,0),(None,False,1),({**phases,'go':self.phase_record(90,False)},False,1),(phases,True,1)):
            with self.subTest(expected=expected,missing=proof is None),tempfile.TemporaryDirectory() as directory:
                root=Path(directory);work=root/'addp-metrics-t2.fixture';work.mkdir();output=root/'report.json'
                ownership={'project':'addp-metrics-t2-'+'a'*32,'work':str(work)}
                child=Mock(pid=1234);child.poll.side_effect=[None,0]
                def start(*args,**kwargs):
                    receipt=Path(kwargs['env']['ADDP_HTTP_BUDGET_RECEIPT']);receipt.write_text(json.dumps(ownership))
                    def cleanup(**kwargs):
                        shutil.rmtree(work)
                        receipt.write_text(json.dumps({**ownership,'cleanup_verified':True,'request_counts':counts,'request_phases':proof,**({'cleanup_deadline_monotonic':0} if overdue else {})}))
                        return 0
                    child.wait.side_effect=cleanup
                    return child
                with patch.object(m.sys,'argv',['budget','bash','scripts/test/monitor-metrics-gate.sh','--case=http-quality']),patch.dict(os.environ,{'ADDP_HTTP_BUDGET_REPORT':str(output)}),patch.object(m,'input_hashes',return_value={}),patch.object(m.subprocess,'Popen',side_effect=start),patch.object(m,'sample',return_value={'duration_seconds':0}),patch.object(m.time,'sleep'),redirect_stdout(io.StringIO()):
                    self.assertEqual(m.main(),expected)
                self.assertEqual(json.loads(output.read_text())['result'],'passed' if expected==0 else 'failed')
                if overdue:self.assertTrue(json.loads(output.read_text())['cleanup_overdue'])

    def test_existing_standard_case_is_wrapped_and_registered(self):
        root=Path(__file__).resolve().parents[2]
        self.assertIn('metrics-http-budget.py bash scripts/test/monitor-metrics-gate.sh $(MONITOR_METRICS_TEST_ARGS)',(root/'Makefile').read_text())
        gate=(root/'scripts/test/monitor-metrics-gate.sh').read_text()
        for name in ('python-http-agent','python-http-copilot','common-python/','metrics-http-budget.py'):
            self.assertIn(name,gate)
        probe=(root/'scripts/test/monitor-metrics-probe.py').read_text()
        self.assertIn("python-http-metrics-probe.py",probe)
