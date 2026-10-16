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

    def test_checkpoint_never_claims_final_success_or_failure(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp)
            for result in ('failed','passed'):
                report={'schema_version':'addp.raster-workflow-capacity-online/v1','result':result,
                        'run_id':'run','groups':[{'cache_mib':64,'running_limit':1,'repetition':1}]}
                original=copy.deepcopy(report)
                m.save_progress(root,report)
                checkpoint=json.loads((root/'capacity-progress.json').read_text())
                self.assertNotIn('result',checkpoint)
                self.assertNotIn('schema_version',checkpoint)
                self.assertEqual(checkpoint['completed_groups'],1)
                self.assertEqual(checkpoint['run_id'],'run')
                self.assertEqual(checkpoint['groups'],report['groups'])
                self.assertEqual(report,original)

    def test_strict_three_party_sizes_reject_coercion_mismatch_and_nonpositive(self):
        m.scene.strict_sizes(*[{'size_bytes': 123} for _ in range(3)])
        for index in range(3):
            for value in (123.0, True, '123', 0, -1, 124, None):
                values = [{'size_bytes': 123} for _ in range(3)]
                values[index]['size_bytes'] = value
                with self.subTest(index=index, value=value), self.assertRaises(m.scene.SuiteError):
                    m.scene.strict_sizes(*values)

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

    def test_waits_for_saved_policy_after_submission_with_active_execution(self):
        runtime = self.measurement()[0][0]['runtime']
        platform = SimpleNamespace(request=unittest.mock.Mock(side_effect=[SimpleNamespace(payload={'runtime':None}),
                                    SimpleNamespace(payload={'runtime':runtime})]))
        with patch.object(m.time,'sleep'):
            m.wait_applied(platform,'/policy',64,1,3)
        self.assertEqual(platform.request.call_count, 2)

    def test_only_empty_uninitialized_prefix_is_allowed_before_policy_load(self):
        samples,before,after=self.measurement()
        initial={**samples[0],'runtime':dict(enabled=False,cache_mib=None,applied_version=None,running=0,waiting=0)}
        result=m.validate_measurement([initial,*samples],before,after,64,1,3)
        self.assertEqual(result['max_running'],1)
        for field,value in (('cache_mib',256),('applied_version',2),('running',1),('waiting',1)):
            invalid=copy.deepcopy(initial); invalid['runtime'][field]=value
            with self.subTest(field=field), self.assertRaises(m.scene.SuiteError):
                m.validate_measurement([invalid,*samples],before,after,64,1,3)
        with self.assertRaises(m.scene.SuiteError): m.validate_measurement([*samples,initial],before,after,64,1,3)
        with self.assertRaises(m.scene.SuiteError): m.validate_measurement([initial],before,after,64,1,3)

    def test_sampler_retains_oom_metrics_when_runtime_health_is_lost(self):
        from unittest.mock import Mock
        platform=SimpleNamespace(request=Mock(return_value=SimpleNamespace(payload={'runtime':None})))
        sampler=m.Sampler(Path('/cgroup'),platform,'/policy'); sampler.started=m.time.monotonic()
        with patch.object(m,'read_metrics',return_value={'oom':1,'oom_kill':1,'memory_peak_bytes':2*(1<<30)}):
            with self.assertRaises(m.scene.SuiteError): sampler.sample()
        self.assertEqual(sampler.samples[0]['oom_kill'],1)
        self.assertIsNone(sampler.samples[0]['runtime'])

    def test_sampler_failure_keeps_terminal_metrics_and_specific_reason(self):
        from unittest.mock import Mock
        platform=SimpleNamespace(request=Mock())
        sampler=m.Sampler(Path('/cgroup'),platform,'/policy')
        sampler.thread=Mock();sampler.thread.is_alive.return_value=False
        sampler.error=m.scene.SuiteError('capacity runtime observation is missing')
        terminal={'oom':1,'oom_kill':1,'memory_peak_bytes':2*(1<<30)}
        with patch.object(m,'read_metrics',return_value=terminal):
            with self.assertRaisesRegex(m.scene.SuiteError,'capacity runtime observation is missing'):
                sampler.finish()
        self.assertEqual(sampler.after,terminal)
        platform.request.assert_not_called()

    def test_sampler_terminal_observation_failure_still_keeps_terminal_metrics(self):
        from unittest.mock import Mock
        platform=SimpleNamespace(request=Mock(return_value=SimpleNamespace(payload={'runtime':None})))
        sampler=m.Sampler(Path('/cgroup'),platform,'/policy');sampler.started=m.time.monotonic()
        sampler.thread=Mock();sampler.thread.is_alive.return_value=False
        terminal={'oom':0,'oom_kill':0,'memory_peak_bytes':123}
        with patch.object(m,'read_metrics',return_value=terminal):
            with self.assertRaisesRegex(m.scene.SuiteError,'capacity runtime observation is missing'):
                sampler.finish()
        self.assertEqual(sampler.after,terminal)
        self.assertIsNone(sampler.samples[-1]['runtime'])

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


class DirectoryCapacityEvidenceTest(unittest.TestCase):
    def test_scan_observation_requires_user_parent_tenant_target_and_exact_reference(self):
        parent=self.execution();parent['triggered_by']=33
        identity={'principal_id':'33','tenant_id':'2'}
        child={'execution_id':'new-scan','parent_execution_id':'parent','triggered_by':33,'tenant_id':2,
               'module':'meta','task_type':'scan','status':'success','scan_context':{'engine_id':9,'scan_depth':'deep'}}
        evidence=m.validate_directory_scan(parent,child,'new-scan',identity,9)
        self.assertEqual(evidence['parent_execution_id'],'parent')
        self.assertEqual(evidence['triggered_by'],33)
        self.assertEqual(evidence['scan_context'],child['scan_context'])
        for field,value in [('execution_id','other'),('parent_execution_id','foreign-parent'),('triggered_by',34),
                            ('triggered_by','33'),('triggered_by',True),('triggered_by',None),('tenant_id',3),
                            ('tenant_id','2'),('module','manager'),('task_type','other'),('status','failed'),
                            ('source_task_id','persisted-task')]:
            invalid=copy.deepcopy(child);invalid[field]=value
            with self.subTest(field=field,value=value),self.assertRaises(m.scene.SuiteError):
                m.validate_directory_scan(parent,invalid,'new-scan',identity,9)
        for field,value in [('engine_id',8),('engine_id','9'),('scan_depth','basic')]:
            invalid=copy.deepcopy(child);invalid['scan_context'][field]=value
            with self.subTest(field=field,value=value),self.assertRaises(m.scene.SuiteError):
                m.validate_directory_scan(parent,invalid,'new-scan',identity,9)
        for value in (34,'33',None,True):
            invalid=copy.deepcopy(parent);invalid['triggered_by']=value
            with self.subTest(parent_actor=value),self.assertRaises(m.scene.SuiteError):
                m.validate_directory_scan(invalid,child,'new-scan',identity,9)

    def test_directory_matrix_and_task_keep_original_scene_and_single_parent_scope(self):
        self.assertEqual(list(m.directory_matrix()),[(1,1),(1,4),(2,4),(2,1)])
        self.assertEqual(len(list(m.matrix())),12)
        body=m.directory_task(8,9,'capacity-mosaic-l4-r1',4);cfg=body['config']
        self.assertEqual(cfg['source']['node_locator'],'addp://engine/8/path/raster-source/mosaic-source?type=prefix')
        self.assertEqual(cfg['target']['storage_locator'],'addp://engine/9/path/raster-target?type=bucket')
        self.assertEqual(cfg['cog']['leaf_concurrency'],4)
        self.assertEqual(cfg['cog']['num_threads'],1)
        self.assertEqual(cfg['overview'],{'enabled':True,'max_pixels':5490**2,'resampling':'NEAREST'})
        self.assertFalse(cfg['tiles']['enabled'])

    def test_capacity_identity_requires_exact_manager_extension_without_escalating_original(self):
        original=importlib.import_module('scripts.test.raster-workflow-online_test')
        client=original.Client()
        permissions=client.context['authorization']['role_assignments'][0]['permissions']
        with self.assertRaises(m.scene.SuiteError):m.scene.validate_identity(client,2,required_permissions=m.scene.PERMISSIONS|m.MANAGER_PERMISSIONS)
        permissions.extend(sorted(m.MANAGER_PERMISSIONS))
        identity=m.scene.validate_identity(client,2,required_permissions=m.scene.PERMISSIONS|m.MANAGER_PERMISSIONS)
        self.assertEqual(set(identity['permissions_verified']),m.scene.PERMISSIONS|m.MANAGER_PERMISSIONS)
        with self.assertRaises(m.scene.SuiteError):m.scene.validate_identity(client,2)
        permissions.append('manager.derived_artifact.delete')
        with self.assertRaises(m.scene.SuiteError):m.scene.validate_identity(client,2,required_permissions=m.scene.PERMISSIONS|m.MANAGER_PERMISSIONS)

    def execution(self):
        return {'execution_id':'parent','module':'manager','task_type':m.MOSAIC_TASK_TYPE,'status':'success',
            'metadata':{'workflow_runtime':{'engine_type':'geopython_workflow','operator':'build_raster_mosaic','mode':'direct'},
                'lineage_facts':{'schema_version':'addp.lineage-facts/v1',
                    'inputs':[{'locator':'addp://engine/8/path/raster-source/mosaic-source?type=prefix'}],
                    'outputs':[{'locator':'addp://engine/9/path/raster-target/capacity-mosaic-l1-r1?item_id=88&type=object',
                                'item_id':88,'item_fingerprint':'whole-fingerprint'}],
                    'operations':[{'kind':'derive','operator':m.MOSAIC_TASK_TYPE}], 'meta_scan_refs':['new-scan']}}}

    def test_directory_owner_runtime_source_output_and_new_automatic_scan_are_required(self):
        original=self.execution();source=original['metadata']['lineage_facts']['inputs'][0]['locator']
        self.assertEqual(m.validate_directory_execution(original,source,'capacity-mosaic-l1-r1',9)[1],'new-scan')
        mutations=[('owner',lambda e:e.update(module='develop')),('status',lambda e:e.update(status='failed')),
            ('runtime',lambda e:e['metadata']['workflow_runtime'].update(mode='workflow')),
            ('operator',lambda e:e['metadata']['workflow_runtime'].update(operator='raster_mosaic')),
            ('source',lambda e:e['metadata']['lineage_facts']['inputs'][0].update(locator='other')),
            ('output',lambda e:e['metadata']['lineage_facts']['outputs'][0].update(locator='addp://engine/9/path/raster-target/other?item_id=88&type=object')),
            ('scan',lambda e:e['metadata']['lineage_facts'].update(meta_scan_refs=[])),
            ('schema',lambda e:e['metadata']['lineage_facts'].update(schema_version='other'))]
        for name,mutate in mutations:
            e=copy.deepcopy(original);mutate(e)
            with self.subTest(name=name),self.assertRaises(m.scene.SuiteError):m.validate_directory_execution(e,source,'capacity-mosaic-l1-r1',9)
        output=original['metadata']['lineage_facts']['outputs'][0]
        for locator in ('/vsis3/raster-target/capacity-mosaic-l1-r1/mosaic.addp.json',
                        'addp://engine/9/path/raster-target/capacity-mosaic-l1-r1/mosaic.addp.json?item_id=88&type=object',
                        output['locator'].replace('/9/path/','/10/path/'),
                        output['locator'].replace('item_id=88','item_id=89'),
                        output['locator'].replace('item_id=88&',''),
                        output['locator'].replace('type=object','type=prefix'),
                        output['locator']+'&private=sentinel',output['locator']+'#fragment'):
            e=copy.deepcopy(original);e['metadata']['lineage_facts']['outputs'][0]['locator']=locator
            with self.subTest(locator=locator),self.assertRaises(m.scene.SuiteError):
                m.validate_directory_execution(e,source,'capacity-mosaic-l1-r1',9)
        for field,value in [('item_id',True),('item_id','88'),('item_id',None),('item_id',0),
                            ('item_fingerprint',''),('item_fingerprint',None)]:
            e=copy.deepcopy(original);e['metadata']['lineage_facts']['outputs'][0][field]=value
            with self.subTest(field=field,value=value),self.assertRaises(m.scene.SuiteError):
                m.validate_directory_execution(e,source,'capacity-mosaic-l1-r1',9)

    def item(self):
        summary=dict(extent=[600000,3890220,709800,4000020],leaf_count=4,source_count=4,failed_count=0,
                     vrt_width=10980,vrt_height=10980,overview_width=5490,overview_height=5490)
        physical={'physical_size_bytes':123,'object_size_bytes':123,'summary':summary}
        attrs={'item':{'data_type':'media','format':'raster_mosaic','layout':'whole'},
               'capabilities':{'spatial':{'srid':32617,'extent':summary['extent']}},
               'format_info':{'raster_mosaic':{**{k:v for k,v in summary.items() if k!='extent'},
                   'manifest_ref':'mosaic.addp.json','index_ref':'index/source-index.json','overview_ref':'overviews/overview.cog.tif'}}}
        return {'id':88,'engine_id':9,'full_name':'raster-target/capacity-mosaic-l1-r1',
                'fingerprint':'whole-fingerprint','size_bytes':123,'attributes':attrs},physical

    def test_whole_identity_matches_actual_meta_item_without_manifest_or_stale_receipt(self):
        item,_=self.item();output=self.execution()['metadata']['lineage_facts']['outputs'][0]
        m.validate_directory_item_identity(item,output,9,'capacity-mosaic-l1-r1')
        for field,value in [('id',89),('id','88'),('id',True),('engine_id',8),('engine_id','9'),
                            ('full_name','raster-target/other'),('full_name',item['full_name']+'/mosaic.addp.json'),
                            ('fingerprint','stale-fingerprint'),('fingerprint',None)]:
            invalid=copy.deepcopy(item);invalid[field]=value
            with self.subTest(field=field,value=value),self.assertRaises(m.scene.SuiteError):
                m.validate_directory_item_identity(invalid,output,9,'capacity-mosaic-l1-r1')

    def test_whole_dataset_sizes_use_physical_and_object_totals_not_manifest_bytes(self):
        item,physical=self.item()
        self.assertEqual(m.validate_directory_item(item,physical),{'physical_total_size_bytes':123,'object_total_size_bytes':123,'data_item_size_bytes':123})
        for key in ('physical_size_bytes','object_size_bytes','size_bytes'):
            for value in (123.0,True,0,-1,124,'123',None):
                item,physical=self.item();(item if key=='size_bytes' else physical)[key]=value
                with self.subTest(key=key,value=value),self.assertRaises(m.scene.SuiteError):m.validate_directory_item(item,physical)
        for key,value in [('layout','single'),('data_type','table'),('format','tiff')]:
            item,physical=self.item();item['attributes']['item'][key]=value
            with self.subTest(key=key),self.assertRaises(m.scene.SuiteError):m.validate_directory_item(item,physical)
        item,physical=self.item();item['attributes']['format_info']['raster_mosaic']['overview_width']=10980
        with self.assertRaises(m.scene.SuiteError):m.validate_directory_item(item,physical)

    def test_single_parent_has_no_fake_queue_requirement_but_still_needs_actual_admission_and_drain(self):
        samples,before,after=CapacityEvidenceTest().measurement()
        samples[0]['runtime']['waiting']=0
        self.assertEqual(m.validate_measurement(samples,before,after,64,1,3,submitted_tasks=1)['max_waiting'],0)
        with self.assertRaises(m.scene.SuiteError):m.validate_measurement(samples,before,after,64,1,3)
        samples[0]['runtime']['running']=0
        with self.assertRaises(m.scene.SuiteError):m.validate_measurement(samples,before,after,64,1,3,submitted_tasks=1)

    def test_checkpoint_keeps_directory_progress_without_final_result(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);group={'leaf_concurrency':4,'repetition':1}
            m.save_progress(root,{'run_id':'run','result':'passed','groups':[{}]*12,'mosaic_groups':[group]})
            report=json.loads((root/'capacity-progress.json').read_text())
            self.assertEqual(report['completed_groups'],12);self.assertEqual(report['completed_mosaic_groups'],1)
            self.assertEqual(report['mosaic_groups'],[group]);self.assertNotIn('result',report)


    def test_real_manager_route_sequence_four_groups_and_failure_measurements(self):
        from unittest.mock import Mock
        timing_method='submit-one-Manager-task-through-parent-terminal-includes-automatic-scan-excludes-oracle'
        for fail in (None,'parent','physical','meta','monitor'):
            with self.subTest(fail=fail),tempfile.TemporaryDirectory() as temporary:
                root=Path(temporary);env={'ADDP_ONLINE_ARTIFACT_DIR':str(root),'ADDP_ONLINE_SECRET_DIR':str(root)}
                report={'identity':{'principal_id':'33','tenant_id':'2'},'run_id':'run','groups':[{}]*12,'mosaic_groups':[],'result':'failed'}
                current=dict(version=1,running=2,waiting=2,cache_mib=256,default_tenant_running=2,default_tenant_waiting=2)
                tasks={};items={};calls=[];executions={};scans=set();verification_order=[]
                item,actual=self.item();actual.update(source_unchanged=True,rasters={'cog':{'sha256':'stable'}})
                def request(method,path,statuses,body=None):
                    calls.append((method,path,statuses,body))
                    if path=='/policy':
                        if method=='PUT':current.update(body);current['version']+=1
                        return SimpleNamespace(payload={'policy':current.copy()})
                    if method=='POST' and path.endswith('/'+m.MOSAIC_TASK_TYPE):
                        identifier=len(tasks)+1;tasks[identifier]=copy.deepcopy(body)
                        return SimpleNamespace(payload={'id':identifier})
                    if method=='DELETE':return SimpleNamespace(payload=None)
                    if method=='POST' and path.endswith('/execute'):
                        task_id=int(path.split('/')[-2]);name=tasks[task_id]['name'];identifier='parent-'+str(task_id)
                        e=self.execution();e['execution_id']=identifier
                        e['triggered_by']=33
                        target={**copy.deepcopy(item),'id':88+task_id,'full_name':'raster-target/'+name,'fingerprint':'whole-'+name}
                        items[target['id']]=target
                        e['metadata']['lineage_facts']['outputs'][0].update(
                            locator=f'addp://engine/9/path/raster-target/{name}?item_id={target["id"]}&type=object',
                            item_id=target['id'],item_fingerprint=target['fingerprint'])
                        e['metadata']['lineage_facts']['meta_scan_refs']=['scan-'+str(task_id)]
                        executions[identifier]=e;return SimpleNamespace(payload={'execution_id':identifier})
                    if '/tasks/' in path:
                        task=tasks[int(path.rsplit('/',1)[1])]
                        return SimpleNamespace(payload={**task,'task_type':m.MOSAIC_TASK_TYPE,'created_by':33})
                    if '/meta/items/' in path:return SimpleNamespace(payload=items[int(path.rsplit('/',1)[1])])
                    if '/monitor/' in path:
                        e=executions[path.rsplit('/',1)[1]]
                        payload=copy.deepcopy({**e,'metadata':{'lineage_facts':{k:v for k,v in e['metadata']['lineage_facts'].items() if k!='operations'}}})
                        if fail=='monitor':payload['metadata']['lineage_facts']['outputs'][0].pop('locator')
                        return SimpleNamespace(payload=payload)
                    raise AssertionError((method,path))
                client=SimpleNamespace(request=request)
                def sampler(*args):
                    samples,before,after=CapacityEvidenceTest().measurement()
                    samples[0]['runtime']['waiting']=0
                    for sample in samples:sample['runtime']['applied_version']=current['version']
                    return SimpleNamespace(samples=samples,before=before,after=after,start=Mock(),finish=Mock(return_value=samples))
                def wait(client,module,identifier,timeout):
                    if module=='meta':
                        verification_order.append('meta')
                        if fail=='meta':raise m.scene.SuiteError('Meta execution returned HTTP 404')
                        return {'execution_id':identifier,'parent_execution_id':identifier.replace('scan-','parent-'),
                                'module':'meta','task_type':'scan','status':'success','triggered_by':33,'tenant_id':2,
                                'scan_context':{'engine_id':9,'scan_depth':'deep'}}
                    if fail=='parent':raise m.scene.SuiteError('Manager execution failed')
                    return executions[identifier]
                def physical(action,names):
                    if action=='seed-mosaic':return {'source_unchanged':True,'tiles':dict.fromkeys(m.fixture.mosaic_windows(),{})}
                    if action=='verify-mosaic':
                        verification_order.append('physical')
                        if fail=='physical':raise m.scene.SuiteError('physical pixels differ')
                        return actual
                    if action=='purge-mosaic':return {'source_unchanged':True}
                    raise AssertionError(action)
                with patch.object(m,'physical',side_effect=physical),patch.object(m.scene.support,'wait_for_meta_scan'), \
                     patch.object(m.scene.support,'find_fixture_item',side_effect=lambda client,engine,full_name,label:next(v for v in items.values() if v['full_name']==full_name)),patch.object(m.fixture,'require_disk'), \
                     patch.object(m,'restart_runtime',return_value={'Id':'fresh','State':{'Pid':17}}),patch.object(m,'cgroup_directory'), \
                     patch.object(m,'Sampler',side_effect=sampler),patch.object(m,'wait_applied'),patch.object(m.scene,'wait_execution',side_effect=wait):
                    if fail:
                        message={'parent':'Manager execution failed','physical':'physical pixels differ',
                                 'meta':'Meta execution returned HTTP 404','monitor':'Monitor lineage resource locator is missing'}[fail]
                        with self.assertRaisesRegex(m.scene.SuiteError,message):
                            m.run_directories(env,client,client,'/policy','image',8,9,report,scans)
                    else:m.run_directories(env,client,client,'/policy','image',8,9,report,scans)
                measurements=list(root.glob('*-measurements.json'))
                self.assertEqual(len(measurements),1 if fail else 4)
                self.assertEqual(len(report['mosaic_groups']),0 if fail else 4)
                self.assertEqual(len(scans),0 if fail in ('parent','physical') else 1 if fail in ('meta','monitor') else 4)
                self.assertEqual(report['result'],'failed')  # Only the outer complete suite may declare success.
                self.assertTrue(all(call[2]==(403,) for call in calls if call[0]=='DELETE'))
                self.assertTrue(all('manager' in call[1] for call in calls if call[0]=='POST'))
                evidence=list(root.glob('*-verification.json'))
                self.assertEqual(len(evidence),0 if fail=='parent' else 1 if fail else 4)
                for path in evidence:
                    snapshot=json.loads(path.read_text())
                    self.assertNotIn('result',snapshot)
                    self.assertEqual(snapshot.get('timing_method'),timing_method)
                    self.assertEqual(snapshot['physical'],None if fail=='physical' else actual)
                    self.assertEqual(snapshot['physical_verified'],fail!='physical')
                    self.assertTrue(snapshot['execution_id'].startswith('parent-'))
                    self.assertIn('resource_observation',snapshot['measurement'])
                    if fail in ('physical','meta'):
                        self.assertNotIn('size_evidence',snapshot)
                        self.assertNotIn('monitor_verified',snapshot)
                    else:
                        self.assertEqual(snapshot['target_item_id'],88+snapshot['task_id'])
                        self.assertEqual(snapshot['target_item_fingerprint'],'whole-'+snapshot['dataset_name'])
                        self.assertEqual(snapshot['size_evidence'],m.validate_directory_item(item,actual))
                        self.assertEqual(snapshot['monitor_verified'],fail!='monitor')
                expected_order=[] if fail=='parent' else ['physical'] if fail=='physical' else ['physical','meta']*(1 if fail in ('meta','monitor') else 4)
                self.assertEqual(verification_order,expected_order)
                if fail:self.assertFalse((root/'capacity-progress.json').exists())
                if not fail:
                    self.assertEqual([g['leaf_concurrency'] for g in report['mosaic_groups']],[1,4,4,1])
                    for group in report['mosaic_groups']:
                        self.assertEqual(group['timing_method'],timing_method)
                        self.assertEqual(group['target_item_fingerprint'],'whole-'+group['dataset_name'])
                        proof=group['automatic_scan_observation']
                        self.assertEqual(proof['parent_execution_id'],group['execution_id'])
                        snapshot=json.loads((root/(group['dataset_name']+'-verification.json')).read_text())
                        self.assertEqual(snapshot['automatic_scan_observation'],proof)
                    self.assertEqual(json.loads((root/'capacity-progress.json').read_text())['completed_mosaic_groups'],4)


if __name__=='__main__': unittest.main()
