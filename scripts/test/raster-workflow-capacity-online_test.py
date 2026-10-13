import copy
import importlib
import json
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

m = importlib.import_module('scripts.test.raster-workflow-capacity-online')


class CapacityEvidenceTest(unittest.TestCase):
    def measurement(self, running=1):
        metrics = dict(memory_peak_bytes=500_000_000, process_peak_rss_bytes=450_000_000,
                       cpu_usage_usec=10_000_000, oom=0, oom_kill=0)
        runtime = dict(enabled=True, cache_mib=64, pending_restart=False, applied_version=3,
                       running_limit=running, waiting_limit=2, running=running, waiting=2-running,
                       memory_limit_bytes=2*(1<<30), effective_cpu=4,
                       advice=dict(running=2, waiting=2, cache_mib=64, basis='cpu-and-memory-budget-heuristic'))
        samples = [{**metrics, 'runtime': runtime}, {**metrics, 'runtime': {**runtime, 'running': 0, 'waiting': 0}}]
        return samples, {**metrics, 'cpu_usage_usec': 1_000_000}, metrics

    def test_strict_three_party_sizes_reject_coercion_mismatch_and_nonpositive(self):
        m.strict_sizes(*[{'size_bytes': 123} for _ in range(3)])
        for index in range(3):
            for value in (123.0, True, '123', 0, -1, 124, None):
                values = [{'size_bytes': 123} for _ in range(3)]
                values[index]['size_bytes'] = value
                with self.subTest(index=index, value=value), self.assertRaises(m.scene.SuiteError):
                    m.strict_sizes(*values)

    def test_profile_requires_actual_concurrency_queue_drain_no_oom_and_applied_cache(self):
        for limit in (1, 2):
            samples, before, after = self.measurement(limit)
            result = m.validate_measurement(samples, before, after, 64, limit, 3)
            self.assertEqual(result['max_running'], limit)
            self.assertEqual(result['cpu_seconds'], 9)
        for key, value in (('enabled', False), ('cache_mib', 256), ('pending_restart', True),
                           ('applied_version', 2), ('running_limit', 2), ('waiting_limit', 3),
                           ('running', 2), ('running', True), ('waiting', 3), ('waiting', -1),
                           ('memory_limit_bytes', 1<<30), ('effective_cpu', 2), ('advice', None)):
            samples, before, after = self.measurement()
            samples[-1]['runtime'][key] = value
            with self.subTest(key=key, value=value), self.assertRaises(m.scene.SuiteError):
                m.validate_measurement(samples, before, after, 64, 1, 3)
        for location in ('before', 'sample', 'after'):
            samples, before, after = self.measurement()
            {'before': before, 'sample': samples[0], 'after': after}[location]['oom_kill'] = 1
            with self.subTest(location=location), self.assertRaises(m.scene.SuiteError):
                m.validate_measurement(samples, before, after, 64, 1, 3)
        samples, before, after = self.measurement()
        after['memory_peak_bytes'] = 2*(1<<30)+4096
        self.assertTrue(m.validate_measurement(samples,before,after,64,1,3)['peak_exceeded_configured_limit'])
        for failure in ('never_queued', 'never_parallel', 'never_drained', 'impossible_peak', 'counter_reset'):
            limit = 2 if failure == 'never_parallel' else 1
            samples, before, after = self.measurement(limit)
            if failure == 'never_queued': samples[0]['runtime']['waiting'] = 0
            if failure == 'never_parallel': samples[0]['runtime']['running'] = 1
            if failure == 'never_drained': samples[-1]['runtime']['waiting'] = 1
            if failure == 'impossible_peak': after['memory_peak_bytes'] = 0
            if failure == 'counter_reset': after['cpu_usage_usec'] = 0
            with self.subTest(failure=failure), self.assertRaises(m.scene.SuiteError):
                m.validate_measurement(samples, before, after, 64, limit, 3)

    def test_reads_realistic_cgroup_bytes_cpu_and_process_hwm_including_exit_race(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp); proc = root/'proc'; group = root/'cgroup'
            (proc/'17').mkdir(parents=True); group.mkdir()
            (proc/'17/cgroup').write_text('0::/docker/test\n')
            self.assertEqual(m.cgroup_directory(17, proc, group), group/'docker/test')
            for name, content in {'memory.events':'low 0\nhigh 0\noom 0\noom_kill 0\n',
                    'cpu.stat':'usage_usec 123456\n', 'memory.max':str(2*(1<<30)),
                    'cpu.max':'400000 100000', 'cgroup.procs':'17\n18\n',
                    'memory.current':'4321', 'memory.peak':'5432', 'memory.swap.max':'0', 'memory.swap.current':'0'}.items():
                (group/name).write_text(content)
            (proc/'17/status').write_text('Name:\tworker\nVmHWM:\t1234 kB\n')
            facts = m.read_metrics(group, proc)
            self.assertEqual(facts['process_peak_rss_bytes'], 1234*1024)
            self.assertEqual(facts['cpu_usage_usec'], 123456)
            self.assertEqual(facts['memory_peak_bytes'], 5432)
            for wrong in ('max 100000', '200000 100000'):
                (group/'cpu.max').write_text(wrong)
                with self.assertRaises((m.scene.SuiteError, ValueError)): m.read_metrics(group, proc)
            for ambiguous in ('0::/../foreign\n', '0::/a\n0::/b\n', '2:cpu:/a\n'):
                (proc/'17/cgroup').write_text(ambiguous)
                with self.assertRaises(m.scene.SuiteError): m.cgroup_directory(17, proc, group)

    def test_matrix_repeats_each_pair_and_reverses_order_without_changing_workload(self):
        groups = list(m.matrix())
        self.assertEqual(len(groups), 12)
        self.assertEqual(groups[:2], [(1,64,1), (1,64,2)])
        self.assertEqual(groups[6:8], [(2,256,2), (2,256,1)])
        self.assertEqual({(cache, limit):sum((c,n)==(cache,limit) for _,c,n in groups)
                          for cache in (64,128,256) for limit in (1,2)},
                         {(c,n):2 for c in (64,128,256) for n in (1,2)})
        flow = m.workflow('addp://engine/1/path/raster-source/source.tif?type=object', 2, 'result.cog.tif')
        tasks = {task['id']:task for task in flow['tasks']}
        self.assertEqual(tasks['math']['params']['expression'], '(b1+b2+b3+b4)*2+1')
        self.assertEqual(tasks['resample']['params']['resampling'], 'average')
        self.assertEqual(tasks['resample']['params']['size'], [5490,5490])
        self.assertEqual(tasks['save']['params']['write_mode'], 'create')
        self.assertNotIn('access_plan', json.dumps(flow))

    def test_foreign_runtime_is_never_removed_and_resource_mismatch_fails(self):
        container = dict(Config=dict(Labels={'com.addp.online-runtime':m.SUITE,
                        'com.addp.online-run-id':'run'}, Cmd=['sh','container_entrypoint.sh']),
                         Image='sha256:ours', State={'Running':True},
                         HostConfig=dict(Memory=2*(1<<30), MemorySwap=2*(1<<30), NanoCpus=4_000_000_000))
        for key in ('suite', 'run', 'image', 'command'):
            foreign = copy.deepcopy(container)
            if key=='suite': foreign['Config']['Labels']['com.addp.online-runtime']='foreign'
            if key=='run': foreign['Config']['Labels']['com.addp.online-run-id']='foreign'
            if key=='image': foreign['Image']='sha256:foreign'
            if key=='command': foreign['Config']['Cmd']=['python','other.py']
            with self.subTest(key=key), patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='run'), patch.object(m.fixture.shared,'command',return_value=json.dumps([foreign])) as command:
                with self.assertRaises(m.scene.SuiteError): m.restart_runtime('sha256:ours')
                self.assertEqual(command.call_count, 1)

    def test_waits_for_saved_policy_before_starting_measurement(self):
        runtime = self.measurement()[0][-1]['runtime']
        platform = SimpleNamespace(request=unittest.mock.Mock(side_effect=[SimpleNamespace(payload={'runtime':None}),
                                    SimpleNamespace(payload={'runtime':runtime})]))
        with patch.object(m.time,'sleep'):
            m.wait_applied(platform,'/policy',64,1,3)
        self.assertEqual(platform.request.call_count, 2)

    def test_sampler_retains_oom_metrics_when_runtime_health_is_lost(self):
        from unittest.mock import Mock
        platform=SimpleNamespace(request=Mock(return_value=SimpleNamespace(payload={'runtime':None})))
        sampler=m.Sampler(Path('/cgroup'),platform,'/policy'); sampler.started=m.time.monotonic()
        with patch.object(m,'read_metrics',return_value={'oom':1,'oom_kill':1,'memory_peak_bytes':2*(1<<30)}):
            with self.assertRaises(m.scene.SuiteError): sampler.sample()
        self.assertEqual(sampler.samples[0]['oom_kill'],1)
        self.assertIsNone(sampler.samples[0]['runtime'])

    def test_execution_failure_preserves_samples_and_restores_platform_policy(self):
        from unittest.mock import Mock
        original=dict(version=10,running=2,waiting=2,cache_mib=256,default_tenant_running=2,default_tenant_waiting=2)
        current=original.copy(); writes=[]
        def request(method,path,statuses,body=None):
            if path.endswith('/engines'): return SimpleNamespace(payload=[{'id':7}])
            if '/tenant/' in path: return SimpleNamespace(payload={'quota':{'running':None,'waiting':None}})
            if method=='PUT':
                self.assertEqual(body['version'],current['version'])
                current.update(body); current['version']+=1; writes.append(current.copy())
            return SimpleNamespace(payload={'policy':current.copy()})
        client=SimpleNamespace(request=request)
        samples,before,after=self.measurement()
        sampler=SimpleNamespace(before=before,after=after,samples=samples,start=Mock(),finish=Mock(return_value=samples))
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary); (root/'capacity-source-evidence.json').write_text('{}')
            env=dict(GATEWAY_URL='http://gateway',ADDP_ONLINE_TEST_USER_ACCESS_TOKEN='consumer',
                     ADDP_ONLINE_RASTER_POLICY_PLATFORM_TOKEN='platform',ADDP_ONLINE_RASTER_POLICY_TENANT_TOKEN='tenant',
                     ADDP_ONLINE_TEST_TENANT_ID='2',ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID='8',ADDP_ONLINE_RASTER_TARGET_ENGINE_ID='9',
                     ADDP_ONLINE_RASTER_RUNTIME_IMAGE='image',ADDP_ONLINE_TEST_RUN_ID='run',
                     ADDP_ONLINE_SECRET_DIR=str(root),ADDP_ONLINE_ARTIFACT_DIR=str(root))
            with patch.object(m.scene,'GatewayClient',return_value=client), patch.object(m.scene,'validate_identity',return_value={}), \
                 patch.object(m.scene.support,'wait_for_meta_scan'), patch.object(m.scene.support,'find_fixture_item',return_value={'id':8}), \
                 patch.object(m.scene.support,'build_item_locator',return_value='addp://engine/8/path/source?type=object'), \
                 patch.object(m.fixture.shared,'command',return_value='[{"Id":"image"}]'), patch.object(m.fixture,'require_disk'), \
                 patch.object(m,'restart_runtime',return_value={'Id':'container','State':{'Pid':17}}), \
                 patch.object(m,'cgroup_directory'), patch.object(m,'wait_applied'), patch.object(m,'Sampler',return_value=sampler), \
                 patch.object(m.scene,'submit',side_effect=m.scene.SuiteError('submission rejected')):
                with self.assertRaisesRegex(m.scene.SuiteError,'submission rejected'): m.run(env)
            evidence=json.loads((root/'capacity-c64-n1-r1-measurements.json').read_text())
            self.assertEqual(evidence['samples'],samples)
            self.assertEqual(evidence['before'],before)
            self.assertEqual(evidence['after'],after)
            self.assertEqual(len(writes),2)
            self.assertEqual({k:v for k,v in current.items() if k!='version'},
                             {k:v for k,v in original.items() if k!='version'})


if __name__=='__main__': unittest.main()
