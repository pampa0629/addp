import copy
import importlib
import json
import os
import re
import subprocess
import sys
import urllib.parse
import uuid
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
                with self.assertRaisesRegex(m.scene.SuiteError,'submission rejected'): m.run_capacity(env,Mock(),Mock())
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
        client.context['authorization']['role_assignments'][0]['permissions']=sorted(m.scene.PERMISSIONS)
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
                scope=Mock()
                scope.execution.side_effect=lambda execution,*args:execution
                scope.produced_item.side_effect=lambda item,*args:item
                with patch.object(m,'MosaicScopeFixture'),patch.object(m,'verify_basic_rescans',return_value={'result':'passed'}), \
                     patch.object(m,'physical',side_effect=physical),patch.object(m.scene.support,'wait_for_meta_scan'), \
                     patch.object(m.scene.support,'find_fixture_item',side_effect=lambda client,engine,full_name,label:next(v for v in items.values() if v['full_name']==full_name)),patch.object(m.fixture,'require_disk'), \
                     patch.object(m,'restart_runtime',return_value={'Id':'fresh','State':{'Pid':17}}),patch.object(m,'cgroup_directory'), \
                     patch.object(m,'Sampler',side_effect=sampler),patch.object(m,'wait_applied'),patch.object(m.scene,'wait_execution',side_effect=wait):
                    if fail:
                        message={'parent':'Manager execution failed','physical':'physical pixels differ',
                                 'meta':'Meta execution returned HTTP 404','monitor':'Monitor lineage resource locator is missing'}[fail]
                        with self.assertRaisesRegex(m.scene.SuiteError,message):
                            m.run_directories(env,client,client,'/policy','image',8,9,report,scans,Mock(),scope)
                    else:m.run_directories(env,client,client,'/policy','image',8,9,report,scans,Mock(),scope)
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





class MosaicBasicRescanTest(unittest.TestCase):
    def setUp(self):
        self.item = {'id': 88, 'node_id': 55, 'engine_id': 9, 'name': 'capacity-mosaic-l1-r1',
                     'full_name': 'raster-target/capacity-mosaic-l1-r1', 'fingerprint': 'a'*64}
        self.identity = {'principal_id': '33', 'tenant_id': '2'}
        self.parent = '11111111-1111-4111-8111-111111111111'
        self.ids = ['22222222-2222-4222-8222-222222222222', '33333333-3333-4333-8333-333333333333']
        self.target = 'addp://engine/9/path/' + self.item['full_name'] + '?type=prefix'
        self.state = {'identity': self.item.copy(), 'internal_items': 0, 'internal_nodes': 0,
                      'seed_items_remaining': 0, 'seed_nodes_remaining': 0,
                      'scan': None, 'outside_items': 'stable-items', 'outside_nodes': 'stable-nodes'}
        self.tree = {'id': 'root', 'locator': 'addp://engine/9/path/?type=root',
                     'children': [{'id': 'leaf', 'label': self.item['name'], 'type': 'object',
                     'locator': 'addp://engine/9/path/' + self.item['full_name'] + '?type=object&item_id=88',
                     'hasChildren': False, 'children': [], 'metadata': {'item_id': 88}}]}

    def execution(self, identifier):
        return {'execution_id': identifier, 'module': 'meta', 'task_type': 'scan', 'status': 'success',
                'tenant_id': 2, 'triggered_by': 33, 'scan_context': {'engine_id': 9,
                    'scan_depth': 'basic', 'force': False, 'targets': [self.target]}}

    def test_two_real_basic_scans_clear_seeded_records_and_preserve_current_scope(self):
        from unittest.mock import Mock
        seed = {'nodes': [101], 'items': [102, 103]}
        seeded = {**self.state, 'internal_items': 2, 'internal_nodes': 1,
                  'seed_items_remaining': 2, 'seed_nodes_remaining': 1}
        for fault in (None, 'automatic_fragments', 'seed', 'seed_boundary', 'soft_deleted_item', 'orphan_node',
                      'recreated_item', 'identity', 'outside', 'tree', 'scope', 'deep', 'force', 'user', 'tenant', 'reused_scan', 'failed'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as directory:
                before = copy.deepcopy(self.state); after = copy.deepcopy(self.state)
                states = [before, copy.deepcopy(seed), copy.deepcopy(seeded), after, copy.deepcopy(self.state)]
                trees = [copy.deepcopy(self.tree) for _ in range(3)]
                executions = [self.execution(v) for v in self.ids]
                for state, execution in zip((after, states[-1]), executions):
                    state['scan'] = {'execution_id': execution['execution_id'], 'status': 'success',
                                     'config': execution['scan_context']}
                if fault == 'automatic_fragments': before['internal_items'] = 1
                if fault == 'seed': states[1]['items'] = []
                if fault == 'seed_boundary': states[2]['outside_items'] = 'modified'
                if fault == 'soft_deleted_item': after['seed_items_remaining'] = 1
                if fault == 'orphan_node': after['seed_nodes_remaining'] = 1
                if fault == 'recreated_item': states[-1]['internal_items'] = 1
                if fault == 'identity': after['identity']['node_id'] = 99
                if fault == 'outside': after['outside_items'] = 'modified'
                if fault == 'tree': trees[1]['children'].append(copy.deepcopy(trees[1]['children'][0]))
                if fault == 'scope': executions[0]['scan_context']['targets'] = []
                if fault == 'deep': executions[0]['scan_context']['scan_depth'] = 'deep'
                if fault == 'force': executions[0]['scan_context']['force'] = True
                if fault == 'user': executions[0]['triggered_by'] = 44
                if fault == 'tenant': executions[0]['tenant_id'] = 3
                if fault == 'reused_scan': executions[1] = copy.deepcopy(executions[0])
                if fault == 'failed': executions[0]['status'] = 'failed'
                # A real public execution exposes engine/depth only; the fixed DB read proves force/targets.
                for execution in executions:
                    execution['scan_context'] = {k: v for k, v in execution['scan_context'].items() if k in ('engine_id', 'scan_depth')}
                fixture = SimpleNamespace(state=Mock(side_effect=states))
                client = SimpleNamespace(request=Mock(side_effect=[SimpleNamespace(payload=t) for t in trees]))
                scan_ids = self.ids if fault != 'reused_scan' else [self.ids[0]]*2
                with patch.object(m.scene.support, 'wait_for_meta_scan', side_effect=scan_ids) as submit, \
                     patch.object(m.scene, 'wait_execution', side_effect=executions):
                    args = ({'ADDP_ONLINE_ARTIFACT_DIR': directory}, client, self.item, self.parent, self.identity, fixture)
                    if fault:
                        with self.assertRaises(m.scene.SuiteError): m.verify_basic_rescans(*args)
                    else:
                        proof = m.verify_basic_rescans(*args)
                        self.assertEqual(proof['result'], 'passed')
                        self.assertEqual(len(proof['scans']), 2)
                        for call in submit.call_args_list:
                            self.assertEqual(call.kwargs['depth'], 'basic')
                            self.assertIs(call.kwargs['force'], False)
                            self.assertEqual(call.kwargs['scope'], {'targets': [self.target], 'source': 'meta'})
                proof = json.loads((Path(directory)/(self.item['name']+'-basic-rescans.json')).read_text())
                self.assertEqual(proof.get('result'), None if fault else 'passed')
                if fault == 'automatic_fragments': self.assertEqual(fixture.state.call_count, 1)

    def test_tree_rejects_wrong_leaf_identity_directory_or_component_expansion(self):
        for fault in (None, 'node', 'id', 'label', 'directory', 'children', 'hidden', 'component', 'locator'):
            tree = copy.deepcopy(self.tree); leaf = tree['children'][0]
            if fault == 'node': leaf['metadata']['node_id'] = 55
            if fault == 'id': leaf['metadata']['item_id'] = 99
            if fault == 'label': leaf['label'] = 'mosaic.addp.json'
            if fault == 'directory': leaf['type'] = 'prefix'
            if fault == 'children': leaf['hasChildren'] = True
            if fault == 'hidden': tree['children'] = []
            if fault == 'component': leaf['children'] = [{'locator': leaf['locator'].split('?')[0]+'/index', 'children': []}]
            if fault == 'locator': leaf['locator'] = leaf['locator'].replace('item_id=88', 'item_id=99')
            client = SimpleNamespace(request=lambda *args: SimpleNamespace(payload=tree))
            with self.subTest(fault=fault):
                if fault:
                    with self.assertRaises(m.scene.SuiteError): m.whole_tree(client, self.item)
                else: self.assertEqual(m.whole_tree(client, self.item), tree)
        changed = copy.deepcopy(self.tree); changed['metadata'] = {'scanned_at': 'new'}
        self.assertEqual(m.tree_identity_shape(self.tree), m.tree_identity_shape(changed))

class MosaicScopeFixtureTest(unittest.TestCase):
    def setUp(self):
        MosaicBasicRescanTest.setUp(self)
        self.temp = tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.env = {'ONLINE_SUITE': m.SUITE, 'ADDP_ONLINE_TEST_RUN_ID': 'raster-capacity-123-1',
                    'ADDP_ONLINE_SECRET_DIR': str(self.root/'secret'), 'ADDP_ONLINE_ARTIFACT_DIR': str(self.root),
                    'GITHUB_RUN_ID': '123', 'GITHUB_RUN_ATTEMPT': '1'}
        self.base = importlib.import_module('scripts.test.execution-retention-readonly-online')
        self.container = {'Id': 'b'*64, 'Config': {'Labels': {'com.docker.compose.project': 'addp-infra',
            'com.docker.compose.service': 'postgres', 'com.docker.compose.project.working_dir': str(self.base.ROOT)},
            'Env': ['POSTGRES_DB=addp_online', 'POSTGRES_USER=addp', 'POSTGRES_PASSWORD=private']},
            'State': {'Running': True, 'StartedAt': '2026-11-04T00:00:00Z'}}

    def test_personal_environment_rejected_before_inspection_or_query(self):
        with patch.object(m.subprocess, 'run') as command, patch.dict(os.environ, {}, clear=True):
            with self.assertRaises((ValueError, m.scene.SuiteError)): m.MosaicScopeFixture(self.env)
            command.assert_not_called()

    def test_fixed_seed_and_readonly_sql_bind_real_parent_scope_and_original_seed_ids(self):
        for seed in (False, True):
            with self.subTest(seed=seed), patch.dict(os.environ, self.env, clear=True), \
                 patch.object(self.base, 'boundary'), patch.object(m.MosaicScopeFixture, 'inspect', return_value=self.container), \
                 patch.object(m.subprocess, 'run', return_value=SimpleNamespace(returncode=0, stdout=json.dumps({'nodes':[], 'items':[]} if seed else self.state), stderr='')) as command:
                fixture = m.MosaicScopeFixture(self.env)
                fixture.state(self.item, self.parent, self.identity, seed=seed, seeds={'nodes': [101], 'items': [102, 103]})
                args = command.call_args.args[0]; sql = command.call_args.kwargs['input']
                for text in ("e.actor_principal_id=:'principal'", "e.triggered_by=:'principal'", "e.tenant_id=i.tenant_id",
                             "e.created_at>=:'started_at'", "e.status='success'", "e.module='manager'",
                             "i.node_id=:'node'", "i.fingerprint=:'fingerprint'", "n.full_name=i.full_name",
                             "e.metadata->'lineage_facts'->'outputs'", "'format'='raster_mosaic'"):
                    self.assertIn(text, sql)
                self.assertIn('item=88', args); self.assertIn('node=55', args)
                self.assertIn('seed_items={102,103}', args); self.assertIn('seed_nodes={101}', args)
                self.assertEqual(args[args.index('-d')+1], 'addp_online')
                self.assertNotIn('private', ' '.join(args)+sql)
                self.assertEqual(command.call_args.kwargs['timeout'], 8)
                self.assertNotIn('DELETE', sql); self.assertNotIn('UPDATE ', sql)
                if seed:
                    self.assertIn('INSERT INTO meta.meta_node', sql)
                    self.assertIn('INSERT INTO meta.meta_item', sql)
                    self.assertIn('FROM owned', sql)
                else:
                    self.assertIn('BEGIN READ ONLY;', sql)
                    self.assertNotIn('INSERT', sql)
                    self.assertIn('JOIN descendants d ON n.parent_node_id=d.id', sql)
                    self.assertNotIn('LIKE', sql)
                    # No deleted_at predicate may hide seeded soft-deleted or orphaned rows.
                    self.assertIn("FROM meta.meta_item WHERE id=ANY(:'seed_items'::integer[])", sql)
                receipt = (self.root/'mosaic-scope-owner.json').read_text()
                self.assertNotIn('private', receipt); self.assertNotIn('POSTGRES', receipt)

    def test_run_container_scope_and_query_failures_are_closed(self):
        for fault in ('run', 'id', 'restart', 'database', 'labels', 'stopped', 'name', 'item',
                      'parent', 'fingerprint', 'seed_ids', 'sql', 'empty', 'oversize', 'invalid_json'):
            with self.subTest(fault=fault), patch.dict(os.environ, self.env, clear=True), \
                 patch.object(self.base, 'boundary'), patch.object(m.MosaicScopeFixture, 'inspect', return_value=self.container), \
                 patch.object(m.subprocess, 'run') as command:
                fixture = m.MosaicScopeFixture(self.env)
                changed = copy.deepcopy(self.container); item = self.item.copy(); parent = self.parent
                seeds = None
                if fault == 'run': fixture.env = {**self.env, 'ADDP_ONLINE_TEST_RUN_ID': 'raster-capacity-999-1'}
                if fault == 'id': changed['Id'] = 'c'*64
                if fault == 'restart': changed['State']['StartedAt'] = 'new'
                if fault == 'database': changed['Config']['Env'][0] = 'POSTGRES_DB=addp'
                if fault == 'labels': changed['Config']['Labels']['com.docker.compose.project'] = 'personal'
                if fault == 'stopped': changed['State']['Running'] = False
                if fault == 'name': item['name'] = '../another'
                if fault == 'item': item['id'] = True
                if fault == 'parent': parent = "'; DROP TABLE meta.meta_item;--"
                if fault == 'fingerprint': item['fingerprint'] = 'invalid'
                if fault == 'seed_ids': seeds = {'nodes': [True], 'items': [102, 103]}
                command.return_value = SimpleNamespace(returncode=1 if fault == 'sql' else 0,
                    stdout={'empty': '', 'oversize': ' '*65537, 'invalid_json': 'invalid'}.get(fault, '{}'), stderr='ERROR:  42601' if fault=='sql' else '')
                with patch.object(fixture, 'inspect', return_value=changed):
                    with self.assertRaises((m.scene.SuiteError, ValueError)):
                        fixture.state(item, parent, self.identity, seeds=seeds)
                if fault not in ('sql', 'empty', 'oversize', 'invalid_json'): command.assert_not_called()

class CapacityAuthorizationBoundaryTest(unittest.TestCase):
    def test_only_fixed_precise_sources_and_results_gain_expiring_metadata(self):
        from unittest.mock import Mock
        registration = importlib.import_module('scripts.test.online-engine-registration')
        with tempfile.TemporaryDirectory() as directory:
            env = {'GATEWAY_URL':'http://gateway','ADDP_ONLINE_TEST_USER_ACCESS_TOKEN':'user-secret',
                'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN':'issuer-secret','ADDP_ONLINE_TEST_TENANT_ID':'2',
                'ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID':'8','ADDP_ONLINE_RASTER_TARGET_ENGINE_ID':'9',
                'ADDP_ONLINE_ARTIFACT_DIR':directory}
            helper = Mock(); helper.grant.return_value={'request_id':'grant','action':'metadata_read'}
            helper.metadata_facts.side_effect=lambda engine,name,storage,**kwargs:{'boundary':{'layout':kwargs['layouts'][0]}}
            helper.cleanup.return_value={'grants_revoked':True,'metadata_hidden':True}
            factory=Mock(return_value=helper)
            factory.target=registration.ExactFileMetadataGrants.target
            consumer=Mock()
            consumer.request.side_effect=lambda method,path,statuses,body: SimpleNamespace(payload={
                'observed_at':'now','targets':[{'target':body['targets'][0],'visible':False}]})
            with patch.object(registration,'ExactFileMetadataGrants',factory),patch.object(m.scene,'GatewayClient',return_value=consumer):
                authorization = m.CapacityMetadataGrants(env)
            expected = [(8,'raster-source/'+m.fixture.SOURCE_NAME,'single')]+[
                (8,'raster-source/'+m.fixture.MOSAIC_SOURCE_PREFIX+'/'+name,'single') for name in m.fixture.mosaic_windows()]+[
                (9,'raster-target/capacity-c64-n1-r1-0.cog.tif','single'),
                (9,'raster-target/capacity-mosaic-l1-r1','whole')]
            for engine,name,layout in expected: authorization.ensure(engine,name)
            self.assertEqual(helper.grant.call_count,7)
            for call in helper.grant.call_args_list:
                self.assertEqual(call.args[2],'minio'); self.assertEqual(call.kwargs['expires_at'],authorization.expires_at)
                self.assertEqual(call.kwargs['data_type'],'media')
            before=helper.grant.call_count
            for engine,name,layout in [(8,'raster-source/neighbor.tif','single'),(9,'raster-target','whole'),
                (9,'raster-target/capacity-mosaic-l1-r1/index/source-index.json','single'),
                (10,expected[0][1],'single'),(9,'raster-target/capacity-c64-n1-r3-0.cog.tif','single')]:
                with self.subTest(name=name),self.assertRaises(m.registration.RegistrationError):authorization.ensure(engine,name)
            self.assertEqual(helper.grant.call_count,before)
            consumer.request.side_effect=lambda method,path,statuses,body: SimpleNamespace(payload={
                'observed_at':'later','targets':[{'target':body['targets'][0],'visible':True}]})
            authorization.ensure(9,'raster-target/capacity-mosaic-l1-r1')
            self.assertEqual(helper.grant.call_count,before)
            self.assertEqual(len(authorization.report['already_visible']),1)
            authorization.cleanup()
            receipt=(Path(directory)/'capacity-metadata-grants.json').read_text()
            self.assertNotIn('user-secret',receipt);self.assertNotIn('issuer-secret',receipt)
            self.assertEqual(json.loads(receipt)['cleanup'],helper.cleanup.return_value)

    def test_cleanup_runs_for_success_scope_rejection_and_failed_business(self):
        from unittest.mock import Mock
        for failure in (None,'scope','business','cleanup','both'):
            with self.subTest(failure=failure):
                auth=Mock();auth.report={'issued':[]};auth.cleanup.return_value={'grants_revoked':True}
                if failure in ('cleanup','both'):auth.cleanup.side_effect=m.scene.SuiteError('cleanup failed')
                with patch.object(m,'CapacityMetadataGrants',return_value=auth), \
                     patch.object(m,'MosaicScopeFixture',side_effect=m.scene.SuiteError('scope failed') if failure=='scope' else None), \
                     patch.object(m,'run_capacity',return_value={'result':'passed'},
                        side_effect=m.scene.SuiteError('business failed') if failure in ('business','both') else None):
                    if failure:
                        with self.assertRaises(m.scene.SuiteError):m.run({})
                    else:self.assertEqual(m.run({})['metadata_authorization']['cleanup'],{'grants_revoked':True})
                auth.cleanup.assert_called_once()

    def test_private_snapshots_bind_public_user_parent_scan_item_and_preserve_public_projection(self):
        from unittest.mock import Mock
        scope=object.__new__(m.MosaicScopeFixture)
        scope.owner={'State':{'StartedAt':'2026-11-04T00:00:00Z'}}
        identifier='12345678-1234-4234-8234-123456789abc'
        scan='22345678-1234-4234-8234-123456789abc'
        identity={'tenant_id':'2','principal_id':'33'}
        facts={'schema_version':'addp.lineage-facts/v1','inputs':[],'outputs':[],'operations':[{'kind':'derive'}]}
        public={'execution_id':identifier,'module':'develop','task_type':'workflow','status':'success','triggered_by':33,
            'execution_config':{'content':{'workflow_definition':{}}},'metadata':{'result':{'final_result':{'size_bytes':123}},'lineage_facts':{**facts,'operations':None}}}
        internal={**public,'metadata':{'lineage_facts':facts}}
        scope.query=Mock(return_value=internal)
        scope.archive_projection=Mock()
        original=copy.deepcopy(public)
        result=scope.execution(public,identity,'develop')
        self.assertEqual(public,original)
        self.assertEqual(scope.archive_projection.call_args.args[0]['result'],'passed')
        self.assertEqual(result['metadata']['lineage_facts']['operations'],[{'kind':'derive'}])
        self.assertIsNone(public['metadata']['lineage_facts']['operations'])
        sql,values=scope.query.call_args.args
        self.assertIn('BEGIN READ ONLY;',sql);self.assertIn("actor_principal_id=:'principal'",sql)
        self.assertIn("created_at>=:'started_at'",sql);self.assertIn('octet_length',sql)
        self.assertEqual(values['execution'],identifier)
        item={'id':88,'engine_id':9,'node_id':55,'name':'capacity-c64-n1-r1-0.cog.tif',
              'full_name':'raster-target/capacity-c64-n1-r1-0.cog.tif','fingerprint':'a'*64,'attributes':{'item':{'format':'tiff'}}}
        raw={**copy.deepcopy(item),'size_bytes':123,'attributes':{'format_info':{'tiff':{'is_tiled':True}}}}
        scope.query.return_value=raw
        self.assertEqual(scope.produced_item(item,result,scan,identity),raw)
        sql,values=scope.query.call_args.args
        self.assertIn('s.parent_execution_id=e.execution_id',sql)
        self.assertIn("s.actor_principal_id=e.actor_principal_id",sql)
        self.assertIn("i.fingerprint=:'fingerprint'",sql)
        self.assertIn('lineage_facts',sql);self.assertIn('octet_length(i.attributes::text)<=32768',sql)
        self.assertEqual(values['scan'],scan)
        for key,value in [('size_bytes',123),('attributes',{'format_info':{}}),('fingerprint','b'*64)]:
            wrong={**item,key:value}
            with self.subTest(key=key),self.assertRaises(m.scene.SuiteError):scope.produced_item(wrong,result,scan,identity)


class CapacityObservedLayoutRegressionTest(unittest.TestCase):
    def test_capacity_grants_observed_multi_tiff_and_archives_authorization_facts(self):
        builder=importlib.import_module('scripts.test.online-engine-registration_test').OnlineEngineRegistrationTest()
        name='raster-source/'+m.fixture.SOURCE_NAME
        issuer,consumer,facts,state=builder.metadata_clients(engine=8,full_name=name,storage='minio',
            data_type='media',format_name='tiff',layout='multi')
        with tempfile.TemporaryDirectory() as directory:
            env={'GATEWAY_URL':'http://gateway','ADDP_ONLINE_TEST_USER_ACCESS_TOKEN':'user-secret',
                 'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN':'issuer-secret','ADDP_ONLINE_TEST_TENANT_ID':'2',
                 'ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID':'8','ADDP_ONLINE_RASTER_TARGET_ENGINE_ID':'9',
                 'ADDP_ONLINE_ARTIFACT_DIR':directory}
            with patch.object(m.scene,'GatewayClient',side_effect=[consumer,issuer]):
                authorization=m.CapacityMetadataGrants(env)
            receipt=authorization.ensure(8,name)
            self.assertEqual(receipt['metadata_target']['layout'],'multi')
            self.assertEqual(receipt['action'],'metadata_read')
            authorization.cleanup()
            evidence=json.loads(authorization.path.read_text())
            observed=[row for row in evidence['facts'] if row['stage']=='authorization_facts']
            self.assertEqual(len(observed),2)
            self.assertTrue(all(row['facts']['boundary']['layout']=='multi' for row in observed))
            self.assertFalse(evidence['pending'])
            self.assertNotIn('issuer-secret',authorization.path.read_text())


class CapacityAuthorizationDiagnosticTest(unittest.TestCase):
    def clients(self, layout='multi'):
        builder=importlib.import_module('scripts.test.online-engine-registration_test').OnlineEngineRegistrationTest()
        self.name='raster-source/'+m.fixture.SOURCE_NAME
        return builder.metadata_clients(engine=8,full_name=self.name,storage='minio',
            data_type='media',format_name='tiff',layout=layout)

    def run_fixture(self, directory, issuer, consumer):
        from unittest.mock import Mock
        env={'GATEWAY_URL':'http://gateway','ADDP_ONLINE_TEST_USER_ACCESS_TOKEN':'user-secret',
             'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN':'issuer-secret','ADDP_ONLINE_TEST_TENANT_ID':'2',
             'ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID':'8','ADDP_ONLINE_RASTER_TARGET_ENGINE_ID':'9',
             'ADDP_ONLINE_ARTIFACT_DIR':directory,'ADDP_ONLINE_TEST_RUN_ID':'raster-capacity-123-1'}
        def business(env,authorization,scope):
            return {'result':'passed','receipt':authorization.ensure(8,self.name)}
        with patch.object(m.scene,'GatewayClient',side_effect=[consumer,issuer]), \
             patch.object(m,'MosaicScopeFixture',return_value=Mock()),patch.object(m,'run_capacity',side_effect=business):
            return m.run(env)

    def test_actual_single_and_multi_tiff_layouts_are_used_without_conversion(self):
        for layout in ('single','multi'):
            with self.subTest(layout=layout),tempfile.TemporaryDirectory() as directory:
                issuer,consumer,facts,state=self.clients(layout)
                result=self.run_fixture(directory,issuer,consumer)
                self.assertEqual(result['receipt']['metadata_target']['layout'],layout)
                self.assertEqual(result['receipt']['action'],'metadata_read')
                self.assertEqual(result['receipt']['expiry_mode'],'at_time')
                self.assertFalse(result['metadata_authorization']['pending'])
                self.assertFalse(result['metadata_authorization']['failures'])
                observed=[e for e in result['metadata_authorization']['facts'] if e['stage']=='authorization_facts']
                self.assertEqual([e['facts']['boundary']['layout'] for e in observed],[layout,layout])

    def test_incomplete_malformed_or_mismatched_facts_are_archived_before_cleanup(self):
        for fault,code in (('basis','basis_not_complete'),('layout','layout_not_allowed'),
            ('inner_layout','boundary_layout_mismatch'),('fingerprint','identity_mismatch'),
            ('engine','identity_mismatch'),('target','target_mismatch'),('format','item_type_mismatch'),
            ('digest','composition_digest_invalid'),('hash','boundary_hash_invalid'),
            ('time','observed_at_invalid'),('extra_boundary','boundary_fields_mismatch')):
            with self.subTest(fault=fault),tempfile.TemporaryDirectory() as directory:
                issuer,consumer,facts,state=self.clients()
                if fault=='basis':facts['basis_state']='needs_refresh'
                if fault=='layout':facts['boundary']['layout']='whole'
                if fault=='inner_layout':facts['boundary']['metadata_target']['layout']='single'
                if fault=='fingerprint':facts['item_fingerprint']='b'*64
                if fault=='engine':facts['engine_id']='9'
                if fault=='target':facts['boundary']['metadata_target']['target']['segments'][-1]['name']='neighbor.tif'
                if fault=='format':facts['boundary']['metadata_target']['format']='jpeg'
                if fault=='digest':facts['boundary']['metadata_target']['composition_digest']='bad'
                if fault=='hash':facts['boundary_hash']=None
                if fault=='time':facts['observed_at']='not-a-time'
                if fault=='extra_boundary':facts['boundary']['scope']={'private':'must-not-escape'}
                with self.assertRaises(m.registration.MetadataGrantError) as raised:
                    self.run_fixture(directory,issuer,consumer)
                self.assertEqual(raised.exception.code,code)
                report=json.loads((Path(directory)/'capacity-metadata-grants.json').read_text())
                failure=json.loads((Path(directory)/'capacity-failure.json').read_text())
                self.assertEqual(failure['failures'][0]['code'],code)
                self.assertEqual(failure['failures'][0]['stage'],'facts_validate')
                self.assertTrue(any(e['stage']=='authorization_facts' for e in report['facts']))
                self.assertFalse(report['issued']);self.assertFalse(report['pending'])
                self.assertTrue(report['cleanup']['grants_revoked'])
                self.assertFalse(any(e[0]=='POST' for e in state['events']))
                self.assertNotIn('must-not-escape',json.dumps(report))

    def test_second_fact_read_detects_layout_drift_without_granting_or_rewriting(self):
        with tempfile.TemporaryDirectory() as directory:
            issuer,consumer,facts,state=self.clients();original=issuer.request.side_effect;reads=0
            def request(method,path,statuses,body=None):
                nonlocal reads
                result=original(method,path,statuses,body)
                if path.endswith('/authorization-facts'):
                    reads+=1; result=SimpleNamespace(payload=copy.deepcopy(result.payload))
                    if reads==2:
                        result.payload['boundary']['layout']='single'
                        result.payload['boundary']['metadata_target']['layout']='single'
                return result
            issuer.request.side_effect=request
            with self.assertRaises(m.registration.MetadataGrantError) as raised:self.run_fixture(directory,issuer,consumer)
            self.assertEqual(raised.exception.code,'layout_not_allowed')
            report=json.loads((Path(directory)/'capacity-metadata-grants.json').read_text())
            self.assertEqual([e['facts']['boundary']['layout'] for e in report['facts'] if e['stage']=='authorization_facts'],['multi','single'])
            self.assertFalse(any(e[0]=='POST' for e in state['events']))

    def test_transport_and_unknown_write_errors_are_safe_and_cleanup_failure_is_separate(self):
        marker='private-Bearer-token-postgres-password'
        for failure in ('facts_read','write','write_and_cleanup'):
            with self.subTest(failure=failure),tempfile.TemporaryDirectory() as directory:
                issuer,consumer,facts,state=self.clients();original=issuer.request.side_effect
                def request(method,path,statuses,body=None):
                    if failure=='facts_read' and path.endswith('/authorization-facts'):
                        raise m.scene.SuiteError('GET '+marker+' returned HTTP 503 (unavailable)')
                    if failure=='write_and_cleanup' and path.endswith('/revoke'):raise OSError(marker)
                    result=original(method,path,statuses,body)
                    if path.endswith('/access_grants'):raise TimeoutError(marker)
                    return result
                issuer.request.side_effect=request
                with self.assertRaises(Exception):self.run_fixture(directory,issuer,consumer)
                report=json.loads((Path(directory)/'capacity-metadata-grants.json').read_text())
                self.assertNotIn(marker,json.dumps(report))
                saved=(Path(directory)/'capacity-failure.json').read_text()
                self.assertNotIn(marker,saved);self.assertNotIn('issuer-secret',saved)
                self.assertEqual(report['failures'][0]['role'],'business')
                if failure=='facts_read':
                    self.assertEqual(report['failures'][0]['stage'],'facts_read')
                    event=next(e for e in report['facts'] if e.get('error_stage')=='facts_read')
                    self.assertEqual(event['error']['http_status'],503)
                else:self.assertEqual(report['failures'][0]['stage'],'grant_create')
                if failure=='write_and_cleanup':
                    self.assertEqual([e['role'] for e in report['failures']],['business','cleanup'])
                    self.assertEqual(report['failures'][1]['code'],'revoke_or_visibility_failed')
                    self.assertEqual(len(report['pending']),1);self.assertEqual(report['cleanup'],'failed')
                else:self.assertFalse(report['pending']);self.assertFalse(state['visible'])

    def test_fact_evidence_omits_unknown_fields_and_hashes_unexpected_identities(self):
        marker='secret://user:password@private-host'
        with tempfile.TemporaryDirectory() as directory:
            issuer,consumer,facts,state=self.clients()
            facts['credentials']=marker;facts['full_name']=marker
            facts['boundary']['metadata_target']['private']=marker
            facts['observed_at']=marker
            with self.assertRaises(m.registration.MetadataGrantError):self.run_fixture(directory,issuer,consumer)
            report=json.loads((Path(directory)/'capacity-metadata-grants.json').read_text())
            self.assertNotIn(marker,json.dumps(report))
            observed=next(e['facts'] for e in report['facts'] if e['stage']=='authorization_facts')
            self.assertEqual(observed['unrecognized_field_count'],1)
            self.assertIn('sha256',observed['full_name'])
            self.assertEqual(observed['boundary']['metadata_target']['unrecognized_field_count'],1)
            self.assertNotIn('credentials',observed)

    def test_cli_errors_have_stable_stage_code_and_no_remote_exception_text(self):
        error=m.registration.MetadataGrantError('facts_validate','layout_not_allowed')
        message=m.capacity_error_message(error)
        self.assertIn('facts_validate',message);self.assertIn('layout_not_allowed',message)
        unknown=m.capacity_error_message(TimeoutError('Bearer private-token password=private'))
        self.assertIn('TimeoutError',unknown);self.assertNotIn('private-token',unknown);self.assertNotIn('password=',unknown)


class CapacityAdditionalAuthorizationTest(unittest.TestCase):
    clients=CapacityAuthorizationDiagnosticTest.clients
    run_fixture=CapacityAuthorizationDiagnosticTest.run_fixture

    def test_fact_archive_failure_prevents_grant_creation(self):
        from unittest.mock import Mock
        issuer,consumer,facts,state=self.clients()
        grants=m.registration.ExactFileMetadataGrants(issuer,consumer,2,
            on_evidence=Mock(side_effect=OSError('archive unavailable')))
        with self.assertRaises(OSError):
            grants.grant(8,self.name,'minio',data_type='media',format_name='tiff',layout='multi')
        self.assertFalse(grants.pending)
        self.assertFalse(any(e[0]=='POST' for e in state['events']))

    def test_successful_authorization_does_not_mislabel_later_business_failure(self):
        from unittest.mock import Mock
        issuer,consumer,facts,state=self.clients()
        with tempfile.TemporaryDirectory() as directory:
            env={'GATEWAY_URL':'http://gateway','ADDP_ONLINE_TEST_USER_ACCESS_TOKEN':'user-secret',
                 'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN':'issuer-secret','ADDP_ONLINE_TEST_TENANT_ID':'2',
                 'ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID':'8','ADDP_ONLINE_RASTER_TARGET_ENGINE_ID':'9',
                 'ADDP_ONLINE_ARTIFACT_DIR':directory}
            def business(env,authorization,scope):
                authorization.ensure(8,self.name)
                raise m.scene.SuiteError('workflow stopped')
            with patch.object(m.scene,'GatewayClient',side_effect=[consumer,issuer]), \
                 patch.object(m,'MosaicScopeFixture',return_value=Mock()),patch.object(m,'run_capacity',side_effect=business):
                with self.assertRaisesRegex(m.scene.SuiteError,'workflow stopped'):m.run(env)
            report=json.loads((Path(directory)/'capacity-metadata-grants.json').read_text())
            self.assertEqual(report['failures'][0]['stage'],'business')
            self.assertFalse(report['pending']);self.assertFalse(state['visible'])

    def test_whole_remains_exact_metadata_item_and_rejects_single_layout(self):
        builder=importlib.import_module('scripts.test.online-engine-registration_test').OnlineEngineRegistrationTest()
        name='raster-target/capacity-mosaic-l1-r1'
        for layout in ('whole','single'):
            with self.subTest(layout=layout),tempfile.TemporaryDirectory() as directory:
                issuer,consumer,facts,state=builder.metadata_clients(engine=9,full_name=name,storage='minio',
                    data_type='media',format_name='raster_mosaic',layout=layout)
                env={'GATEWAY_URL':'http://gateway','ADDP_ONLINE_TEST_USER_ACCESS_TOKEN':'user-secret',
                     'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN':'issuer-secret','ADDP_ONLINE_TEST_TENANT_ID':'2',
                     'ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID':'8','ADDP_ONLINE_RASTER_TARGET_ENGINE_ID':'9',
                     'ADDP_ONLINE_ARTIFACT_DIR':directory}
                with patch.object(m.scene,'GatewayClient',side_effect=[consumer,issuer]):
                    authorization=m.CapacityMetadataGrants(env)
                if layout=='single':
                    with self.assertRaises(m.registration.MetadataGrantError) as raised:authorization.ensure(9,name)
                    self.assertEqual(raised.exception.code,'layout_not_allowed')
                else:
                    receipt=authorization.ensure(9,name)
                    self.assertEqual(receipt['metadata_target']['layout'],'whole')
                    self.assertEqual(receipt['action'],'metadata_read')
                    self.assertEqual(receipt['catalog_path']['segments'][-1]['term'],'prefix')
                    self.assertIn('item_fingerprint',receipt)
                    self.assertNotIn('node_recursive',receipt)
                authorization.cleanup()
                self.assertFalse(authorization.report['pending'])


class SnapshotDiagnosticsTest(unittest.TestCase):
    setUp = MosaicScopeFixtureTest.setUp

    def scope(self):
        self.enterContext(patch.dict(os.environ,self.env,clear=True))
        self.enterContext(patch.object(self.base,'boundary'))
        self.enterContext(patch.object(m.MosaicScopeFixture,'inspect',return_value=self.container))
        return m.MosaicScopeFixture(self.env)

    def test_sqlstate_response_shapes_and_sensitive_output_are_classified(self):
        scope=self.scope()
        payload={field:None for field in m.SNAPSHOT_FIELDS['execution']}
        cases=[
            (3,'', 'psql:<stdin>:4: ERROR:  42601\nprivate connection SQL','execute','sql_failed','42601',None),
            (2,'private output','password=private','execute','process_failed','unavailable',None),
            (0,'','', 'response_bounds','empty',None,None),
            (0,' '*65537,'', 'response_bounds','oversize',None,None),
            (0,'{}\n{}','', 'response_bounds','multiple_rows',None,None),
            (0,'private invalid JSON','', 'decode','invalid_json',None,None),
            (0,'{"metadata":NaN}','', 'decode','invalid_json',None,None),
            (0,'null','', 'shape','not_object',None,'null'),
            (0,'[]','', 'shape','not_object',None,'array'),
            (0,'true','', 'shape','not_object',None,'boolean'),
            (0,'1','', 'shape','not_object',None,'number'),
            (0,'"private"','', 'shape','not_object',None,'string'),
            (0,'{}','', 'shape','fields_mismatch',None,'object'),
            (0,json.dumps({**payload,'private-key':'password'}),'', 'shape','fields_mismatch',None,'object'),
        ]
        for returncode,stdout,stderr,phase,code,sqlstate,json_type in cases:
            with self.subTest(code=code,json_type=json_type),patch.object(m.subprocess,'run',
                    return_value=SimpleNamespace(returncode=returncode,stdout=stdout,stderr=stderr)):
                with self.assertRaises(m.SnapshotQueryError) as raised:
                    scope.query('SELECT private_parameter',{'secret':'private'},operation='execution')
                error=raised.exception
                self.assertEqual(error.stage,'snapshot.execution.'+phase)
                self.assertEqual(error.code,code)
                evidence=m.capacity_error_evidence(error)
                self.assertEqual(evidence['diagnostics']['sqlstate'],sqlstate)
                self.assertEqual(evidence['diagnostics'].get('json_type'),json_type)
                self.assertNotIn('private',json.dumps(evidence))
                self.assertNotIn('password',m.capacity_error_message(error))
        archive=json.loads((self.root/'capacity-snapshots.json').read_text())
        self.assertEqual(len(archive['queries']),len(cases))
        self.assertNotIn('private',json.dumps(archive))
        self.assertNotIn('password',json.dumps(archive))

    def test_success_receipts_are_bounded_and_keep_only_shape_and_digests(self):
        scope=self.scope()
        for operation,fields in m.SNAPSHOT_FIELDS.items():
            payload={field:'private' for field in fields}
            with patch.object(m.subprocess,'run',return_value=SimpleNamespace(
                    returncode=0,stdout=json.dumps(payload),stderr='private-warning')) as run:
                self.assertEqual(scope.query('SELECT private',{},operation=operation),payload)
                self.assertEqual(run.call_args.kwargs['timeout'],8)
                self.assertIn('addp_online',run.call_args.args[0])
        archive=(self.root/'capacity-snapshots.json').read_text()
        self.assertNotIn('private',archive)
        self.assertTrue(all(x['result']=='passed' for x in json.loads(archive)['queries']))
        scope.query_report['queries']=[{}]*128
        with patch.object(m.subprocess,'run') as run,self.assertRaises(m.SnapshotQueryError) as raised:
            scope.query('SELECT private',{},operation='execution')
        self.assertEqual(raised.exception.code,'query_limit_exceeded');run.assert_not_called()

    def test_owner_transport_timeout_archive_and_identity_failures_are_distinct(self):
        scope=self.scope()
        for response in (None,SimpleNamespace(returncode=0,stdout='{}'),
                         SimpleNamespace(returncode=True,stdout='{}',stderr=''),
                         SimpleNamespace(returncode=0,stdout=b'{}',stderr='')):
            with self.subTest(response_type=type(response).__name__),patch.object(m.subprocess,'run',return_value=response):
                with self.assertRaises(m.SnapshotQueryError) as raised:
                    scope.query('SELECT private',{},operation='execution')
                self.assertEqual(raised.exception.code,'transport_shape_invalid')
        with patch.object(scope,'inspect',side_effect=m.scene.SuiteError('private owner')),\
                patch.object(m.subprocess,'run') as run:
            with self.assertRaises(m.SnapshotQueryError) as raised:
                scope.query('SELECT private',{},operation='whole_state')
            self.assertEqual(raised.exception.stage,'snapshot.whole_state.ownership');run.assert_not_called()
        for failure,code in [(subprocess.TimeoutExpired('private command',8,output='private',stderr='private'),'timeout'),
                              (OSError('private command'),'command_failed')]:
            with self.subTest(code=code),patch.object(m.subprocess,'run',side_effect=failure):
                with self.assertRaises(m.SnapshotQueryError) as raised:
                    scope.query('SELECT private',{},operation='seed_fragments')
                self.assertEqual(raised.exception.code,code)
                self.assertEqual(raised.exception.diagnostics['sqlstate'],'unavailable')
                self.assertNotIn('private',m.capacity_error_message(raised.exception))
        with patch.object(m.subprocess,'run',return_value=SimpleNamespace(returncode=0,stdout='',stderr='')),\
                patch.object(Path,'write_text',side_effect=OSError('private path')):
            with self.assertRaises(m.SnapshotQueryError) as raised:
                scope.query('SELECT private',{},operation='produced_item')
            self.assertEqual(raised.exception.stage,'snapshot.produced_item.archive')
            self.assertEqual(raised.exception.diagnostics['primary']['code'],'empty')
            self.assertEqual(raised.exception.diagnostics['primary']['phase'],'response_bounds')
        with patch.object(scope,'query',return_value={}),self.assertRaises(m.SnapshotQueryError) as raised:
            scope.execution({'execution_id':self.parent,'status':'success'},self.identity,'develop')
        self.assertEqual(raised.exception.stage,'snapshot.execution.identity')

    def test_snapshot_failure_is_retained_before_cleanup_and_cli_uses_same_stage(self):
        from unittest.mock import Mock
        scope=self.scope()
        failure=m.SnapshotQueryError('execution','execute','sql_failed',{'sqlstate':'42601'})
        auth=object.__new__(m.CapacityMetadataGrants)
        auth.env=self.env;auth.path=self.root/'capacity-metadata-grants.json'
        auth.phase='idle';auth.operation_active=False;auth.report={'failures':[]}
        auth.record_failure('business',failure)
        auth.record_failure('cleanup',m.scene.SuiteError('private cleanup'))
        detail=json.loads((self.root/'capacity-failure.json').read_text())['failures']
        self.assertEqual(detail[0]['stage'],'snapshot.execution.execute')
        self.assertEqual(detail[0]['diagnostics']['sqlstate'],'42601')
        self.assertEqual(detail[1]['role'],'cleanup')
        self.assertNotIn('private',json.dumps(detail))
        self.assertIn('snapshot.execution.execute',m.capacity_error_message(failure))
        self.assertIn('42601',m.capacity_error_message(failure))
        auth=Mock();auth.report={'issued':[]}
        with patch.object(m,'CapacityMetadataGrants',return_value=auth),patch.object(m,'MosaicScopeFixture'),\
                patch.object(m,'run_capacity',side_effect=failure):
            with self.assertRaises(m.SnapshotQueryError):m.run({})
        self.assertEqual(auth.record_failure.call_args.args,('business',failure))
        auth.cleanup.assert_called_once()


class LineageProjectionDiagnosticsTest(unittest.TestCase):
    setUp = MosaicScopeFixtureTest.setUp
    scope = SnapshotDiagnosticsTest.scope

    def pair(self):
        source={'port':'input.load','locator':'addp://engine/8/path/raster-source/sample.tif?type=object&item_id=101',
                'item_id':101,'item_fingerprint':'a'*64,'schema_snapshot':{'private':'PRIVATE_SCHEMA_VALUE'}}
        target={'port':'output.save','locator':'addp://engine/9/path/raster-target/result.tif?item_id=102&type=object',
                'item_id':102,'write_mode':'create'}
        owner={'schema_version':'addp.lineage-facts/v1','inputs':[source],'outputs':[target],
               'operations':[{'kind':'derive','operator':'PRIVATE_OPERATOR'}]}
        public={**copy.deepcopy(owner),'operations':None}
        public['inputs'][0].pop('schema_snapshot')
        # Same canonical locator with a different query ordering is valid.
        public['outputs'][0]['locator']='addp://engine/9/path/raster-target/result.tif?type=object&item_id=102'
        observation={'execution_id':self.parent,'module':'develop','task_type':'workflow',
            'tenant_id':2,'triggered_by':33,'status':'success',
            'metadata':{'result':{'final_result':{'private':'PRIVATE_RESULT'}},'lineage_facts':public}}
        internal={k:v for k,v in observation.items() if k!='metadata'}
        internal['metadata']={'lineage_facts':owner}
        return observation,internal

    def test_professional_result_is_accepted_without_weakening_full_monitor_guard(self):
        scope=self.scope();public,internal=self.pair();before=copy.deepcopy(public)
        with patch.object(scope,'query',return_value=internal) as query:
            self.assertEqual(scope.execution(public,self.identity,'develop')['metadata']['lineage_facts'],internal['metadata']['lineage_facts'])
        query.assert_called_once();self.assertEqual(public,before)
        with self.assertRaisesRegex(m.scene.SuiteError,'Monitor exposes private execution metadata'):
            m.scene.support.validate_monitor_lineage(public,internal['metadata']['lineage_facts'])
        proof=scope.query_report['projections'][0]
        self.assertEqual(proof['result'],'passed');self.assertEqual(proof['differences'],[])
        body=(self.root/'capacity-snapshots.json').read_text()
        for private in ('PRIVATE_','addp://','sample.tif','result.tif','input.load','output.save'):
            self.assertNotIn(private,body)

    def test_resource_schema_order_and_private_field_mismatches_remain_strict_and_safe(self):
        scope=self.scope()
        for fault,reason,field in [
            ('schema','schema_version_mismatch','schema_version'),
            ('operations','private_operations_exposed','operations'),
            ('private_field','private_resource_fields_exposed','unrecognized_fields'),
            ('missing_locator','locator_missing','locator'),
            ('private_query','locator_private_context','locator'),
            ('credentials','locator_private_context','locator'),
            ('item_id','resource_mismatch','item_id'),
            ('missing_fingerprint','resource_mismatch','item_fingerprint'),
            ('different_path','resource_mismatch','locator'),
            ('cardinality','input_cardinality_mismatch','cardinality'),
            ('shape','shape_or_comparison_failed','shape'),
            ('missing_facts','shape_or_comparison_failed','shape'),
            ('missing_owner','shape_or_comparison_failed','shape'),
            ('both_missing_locator','locator_missing','locator'),
            ('malformed_locator','locator_invalid','locator'),
        ]:
            with self.subTest(fault=fault):
                public,internal=self.pair();facts=public['metadata']['lineage_facts']
                if fault=='schema':facts['schema_version']='PRIVATE_SCHEMA'
                if fault=='operations':facts['operations']=[{'private':'PRIVATE_OPERATION'}]
                if fault=='private_field':facts['inputs'][0]['PRIVATE_FIELD_NAME']='PRIVATE_FIELD_VALUE'
                if fault=='missing_locator':facts['inputs'][0].pop('locator')
                if fault=='private_query':facts['inputs'][0]['locator']+='&token=PRIVATE_TOKEN'
                if fault=='credentials':facts['inputs'][0]['locator']='addp://user:PRIVATE_PASSWORD@engine/8/path/private?type=object'
                if fault=='item_id':facts['inputs'][0]['item_id']=999
                if fault=='missing_fingerprint':facts['inputs'][0].pop('item_fingerprint')
                if fault=='different_path':facts['inputs'][0]['locator']='addp://engine/8/path/PRIVATE_PATH?type=object'
                if fault=='cardinality':facts['inputs']=[]
                if fault=='shape':facts['inputs']=None
                if fault=='missing_facts':public['metadata'].pop('lineage_facts')
                if fault=='missing_owner':internal['metadata']['lineage_facts']=None
                if fault=='both_missing_locator':
                    facts['inputs'][0].pop('locator');internal['metadata']['lineage_facts']['inputs'][0].pop('locator')
                if fault=='malformed_locator':facts['inputs'][0]['locator']='addp://[PRIVATE_INVALID/path?type=object'
                with patch.object(scope,'query',return_value=internal) as query:
                    with self.assertRaises(m.SnapshotQueryError) as raised:scope.execution(public,self.identity,'develop')
                query.assert_called_once()
                proof=raised.exception.diagnostics
                self.assertEqual(proof['reason'],reason)
                self.assertIn(field,[d['field'] for d in proof['differences']])
                self.assertEqual(scope.query_report['projections'][-1],proof)
                body=json.dumps(proof)+m.capacity_error_message(raised.exception)
                for private in ('PRIVATE_','addp://','input.load','sample.tif'):
                    self.assertNotIn(private,body)
        self.assertNotIn('PRIVATE_', (self.root/'capacity-snapshots.json').read_text())

    def test_diagnostics_are_bounded_and_keep_missing_type_and_order_information(self):
        scope=self.scope();public,internal=self.pair()
        expected=internal['metadata']['lineage_facts']
        expected['inputs']*=20
        public['metadata']['lineage_facts']['inputs']=[{'port':'PRIVATE_MISMATCH'} for _ in range(20)]
        with patch.object(scope,'query',return_value=internal),self.assertRaises(m.SnapshotQueryError) as raised:
            scope.execution(public,self.identity,'develop')
        proof=raised.exception.diagnostics
        self.assertEqual(len(proof['differences']),16);self.assertTrue(proof['differences_truncated'])
        self.assertTrue(any(not d['actual']['present'] for d in proof['differences']))
        self.assertNotIn('PRIVATE_',json.dumps(proof))
        public,internal=self.pair()
        internal['metadata']['lineage_facts']['inputs'].append({'port':'input.other','locator':'addp://engine/8/path/other?type=object'})
        public['metadata']['lineage_facts']['inputs'].append({'port':'input.other','locator':'addp://engine/8/path/other?type=object'})
        public['metadata']['lineage_facts']['inputs'].reverse()
        with patch.object(scope,'query',return_value=internal),self.assertRaises(m.SnapshotQueryError) as raised:
            scope.execution(public,self.identity,'develop')
        self.assertEqual(raised.exception.diagnostics['reason'],'resource_mismatch')
        self.assertEqual(raised.exception.diagnostics['differences'][0]['index'],0)

    def test_projection_failure_is_saved_before_metadata_cleanup_and_secondary_failure(self):
        from unittest.mock import Mock
        scope=self.scope();public,internal=self.pair();public['metadata']['lineage_facts']['inputs'][0]['item_id']=999
        with patch.object(scope,'query',return_value=internal):
            with self.assertRaises(m.SnapshotQueryError) as raised:scope.execution(public,self.identity,'develop')
        error=raised.exception
        auth=object.__new__(m.CapacityMetadataGrants)
        auth.env=self.env;auth.path=self.root/'capacity-metadata-grants.json';auth.phase='idle';auth.operation_active=False
        auth.report={'failures':[]};auth.cleanup=Mock(side_effect=m.scene.SuiteError('PRIVATE_CLEANUP'))
        def run_capacity(*_):
            self.assertEqual(json.loads((self.root/'capacity-snapshots.json').read_text())['projections'][-1]['result'],'failed')
            raise error
        with patch.object(m,'CapacityMetadataGrants',return_value=auth),patch.object(m,'MosaicScopeFixture',return_value=scope),\
                patch.object(m,'run_capacity',side_effect=run_capacity):
            with self.assertRaises(m.scene.SuiteError):m.run(self.env)
        failures=json.loads((self.root/'capacity-failure.json').read_text())['failures']
        self.assertEqual(failures[0]['stage'],'snapshot.execution.public_projection')
        self.assertEqual(failures[0]['diagnostics']['reason'],'resource_mismatch')
        self.assertEqual(failures[1]['role'],'cleanup');auth.cleanup.assert_called_once()
        self.assertNotIn('PRIVATE_',json.dumps(failures))


def postgres_snapshot_suite():
    """T2 only: the Common gate validates the DSN before enabling this suite."""
    class PostgresSnapshotTest(unittest.TestCase):
        def setUp(self):
            self.base = MosaicScopeFixtureTest()
            self.base.setUp(); self.addCleanup(self.base.doCleanups)
            self.item = {**self.base.item, 'item_type':'object', 'size_bytes':123,
                'scanned_depth':'deep','attributes':{'item':{'layout':'whole','format':'raster_mosaic'}}}
            self.parent = self.base.parent; self.scan = self.base.ids[0]
            self.identity = self.base.identity
            self.module = 'develop'
            self.raw_run = subprocess.run
            self.dsn = os.environ['ADDP_CAPACITY_SNAPSHOT_POSTGRES_DSN']
            self.sqlstate = None
            self.poison=False
            self.table_prefix='cap_'+uuid.uuid4().hex[:12]+'_'
            self.enterContext(patch.dict(os.environ,self.base.env))
            self.enterContext(patch.object(self.base.base,'boundary'))
            self.enterContext(patch.object(m.MosaicScopeFixture,'inspect',return_value=self.base.container))
            self.scope = m.MosaicScopeFixture(self.base.env)
            self.enterContext(patch.object(m.subprocess,'run',side_effect=self.transport))

        def metadata(self):
            return {'lineage_facts':{'schema_version':'addp.lineage-facts/v1','inputs':[],
                'outputs':[{'item_id':88,'item_fingerprint':'a'*64,
                    'locator':'addp://engine/9/path/'+self.item['full_name']+'?type=object'}],
                'operations':[{'kind':'derive'}]},
                'result':{'final_result':{'size_bytes':123},'meta_scan_runs':[]},
                'workflow_runtime':{},'outputs':[]}

        def setup_sql(self):
            # These relations vanish when psql disconnects, including on failure.
            literal=lambda value:"'"+str(value).replace("'","''")+"'"
            name=self.item['name']; full=self.item['full_name']
            return """
CREATE TEMP TABLE task_executions(execution_id text, module text, task_type text, status text,
 tenant_id integer, triggered_by integer, actor_principal_id bigint, created_at timestamptz,
 metadata jsonb, execution_config jsonb, parent_execution_id text);
CREATE TEMP TABLE meta_node(id serial PRIMARY KEY, tenant_id integer, engine_id integer,
 parent_node_id integer, node_type text, name text, depth integer, full_name text, path text,
 scanned_depth text, deleted_at timestamptz, created_at timestamptz);
CREATE TEMP TABLE meta_item(id serial PRIMARY KEY, tenant_id integer, engine_id integer,
 node_id integer, item_type text, name text, full_name text, fingerprint text, size_bytes bigint,
 scanned_depth text, attributes jsonb, deleted_at timestamptz, created_at timestamptz);
"""+f"""
INSERT INTO task_executions VALUES ({literal(self.parent)},{literal(self.module)},
 {literal('workflow' if self.module=='develop' else 'raster_mosaic_generation')},'success',2,33,33,
 now(),{literal(json.dumps(self.metadata()))}::jsonb,'{{}}',NULL),
 ({literal(self.scan)},'meta','scan','success',2,33,33,now(),'{{}}','{{"engine_id":9}}',{literal(self.parent)});
INSERT INTO meta_node VALUES(55,2,9,NULL,'prefix',{literal(name)},1,{literal(full)},{literal(full)},'basic',NULL,now());
INSERT INTO meta_item VALUES(88,2,9,55,'object',{literal(name)},{literal(full)},'{self.item['fingerprint']}',123,
 'deep',{literal(json.dumps(self.item['attributes']))}::jsonb,NULL,now());
"""

        def transport(self, command, *, input, **kwargs):
            # Preserve the exact query/quoted bindings; adapt only relation names and
            # test transport. The product path remains Hosted-only addp_online.
            uri=urllib.parse.urlsplit(self.dsn)
            config=urllib.parse.parse_qs(uri.query)
            target=['psql','-X','-qAt','-h',uri.hostname,'-p',str(uri.port or 5432),
                '-U',urllib.parse.unquote(uri.username or ''),'-d',uri.path.lstrip('/')]
            source=command[command.index('psql')+1:]
            for index,value in enumerate(source):
                if value=='-v':target.extend(source[index:index+2])
            statement=input
            if self.poison:
                old="'meta_scan_runs')),";new="'meta_scan_runs'))),"
                self.assertIn(old,statement);statement=statement.replace(old,new,1)
            prefix=self.setup_sql()
            for table in ('common.task_executions','meta.meta_node','meta.meta_item'):
                name=table.split('.')[1];temporary=self.table_prefix+name
                statement=statement.replace(table,'pg_temp.'+temporary)
                prefix=prefix.replace('CREATE TEMP TABLE '+name+'(', 'CREATE TEMP TABLE '+temporary+'(')
                prefix=prefix.replace('INSERT INTO '+name+' ', 'INSERT INTO '+temporary+' ')
            env={**os.environ,'PGPASSWORD':urllib.parse.unquote(uri.password or ''),
                'PGSSLMODE':config.get('sslmode',['disable'])[0],'PGCONNECT_TIMEOUT':'5'}
            self.target,self.connection_env=target[:],env
            result=self.raw_run(target,input=prefix+statement,env=env,**kwargs)
            match=re.search(r'ERROR:\s+([0-9A-Z]{5})\b',result.stderr)
            self.sqlstate=match.group(1) if match else None
            return result

        def tearDown(self):
            if not hasattr(self,'target'):return
            statement=("SELECT count(*) FROM pg_class WHERE left(relname,"+str(len(self.table_prefix))+")='"+
                       self.table_prefix+"';")
            result=self.raw_run(self.target,input=statement,env=self.connection_env,
                                capture_output=True,text=True,timeout=8)
            self.assertEqual(result.returncode,0,'temporary relation cleanup query failed')
            self.assertEqual(result.stdout.strip(),'0','connection-local fixture relations leaked')

        def test_original_syntax_defect_has_safe_sqlstate_and_operation(self):
            self.poison=True
            with self.assertRaises(m.SnapshotQueryError) as raised:
                self.scope.execution({'execution_id':self.parent},self.identity,'develop')
            self.assertEqual(raised.exception.stage,'snapshot.execution.execute')
            self.assertEqual(raised.exception.code,'sql_failed')
            self.assertEqual(raised.exception.diagnostics['sqlstate'],'42601')
            receipt=json.loads((self.base.root/'capacity-snapshots.json').read_text())['queries'][-1]
            self.assertEqual(receipt['sqlstate'],'42601')
            self.assertNotIn('SELECT',json.dumps(receipt))
            self.assertNotIn('private',json.dumps(receipt))

        def test_unmatched_identity_and_original_size_limits_fail_closed(self):
            for fault in ('tenant','principal','size'):
                with self.subTest(fault=fault):
                    identity={**self.identity,'tenant_id':'3'} if fault=='tenant' else self.identity.copy()
                    if fault=='principal':identity['principal_id']='34'
                    metadata=self.metadata()
                    if fault=='size':metadata['workflow_runtime']={'large':'x'*4097}
                    with patch.object(self,'metadata',return_value=metadata):
                        with self.assertRaises(m.SnapshotQueryError) as raised:
                            self.scope.execution({'execution_id':self.parent},identity,'develop')
                    self.assertEqual(raised.exception.code,'empty')
                    self.assertEqual(raised.exception.stage,'snapshot.execution.response_bounds')
                    self.assertEqual(raised.exception.diagnostics['stdout_rows'],0)

        def test_execution_query_runs_on_real_postgres(self):
            for module in ('develop','manager'):
                with self.subTest(module=module):
                    self.module=module
                    observation={'execution_id':self.parent,'module':module,
                        'task_type':'workflow' if module=='develop' else 'raster_mosaic_generation',
                        'tenant_id':2,'triggered_by':33,'status':'success',
                        'metadata':{'result':self.metadata()['result'],'lineage_facts':{**self.metadata()['lineage_facts'],'operations':None}}}
                    try:actual=self.scope.execution(observation,self.identity,module)
                    except m.SnapshotQueryError as error:
                        self.fail(error.stage+'.'+error.code+'; SQLSTATE='+str(self.sqlstate))
                    self.assertEqual(actual['metadata']['result']['final_result'],{'size_bytes':123})
                    self.assertEqual(actual['outputs'],[])
                    self.assertEqual(actual['metadata']['lineage_facts']['operations'],[{'kind':'derive'}])
                    self.assertIsNone(observation['metadata']['lineage_facts']['operations'])

        def test_produced_item_query_runs_on_real_postgres(self):
            for module in ('develop','manager'):
                with self.subTest(module=module):
                    self.module=module
                    name='capacity-c64-n1-r1-0.cog.tif' if module=='develop' else self.base.item['name']
                    self.item.update(name=name,full_name='raster-target/'+name)
                    item={k:v for k,v in self.item.items() if k!='size_bytes'}
                    result=self.scope.produced_item(item,{'module':module,'execution_id':self.parent},self.scan,self.identity)
                    self.assertEqual(result['id'],88);self.assertEqual(result['size_bytes'],123)

        def test_whole_state_and_seed_queries_run_on_real_postgres(self):
            self.module='manager'
            for seed in (False,True):
                with self.subTest(seed=seed):
                    result=self.scope.state(self.item,self.parent,self.identity,seed=seed)
                    if seed:
                        self.assertEqual(len(result['nodes']),1);self.assertEqual(len(result['items']),2)
                    else:
                        self.assertEqual(result['internal_nodes'],0);self.assertEqual(result['internal_items'],0)
                        self.assertEqual(result['identity']['id'],88)

    return unittest.defaultTestLoader.loadTestsFromTestCase(PostgresSnapshotTest)


if __name__=='__main__':
    if sys.argv[1:]==['--postgres']:
        result=unittest.TextTestRunner(verbosity=2).run(postgres_snapshot_suite())
        raise SystemExit(not result.wasSuccessful() or bool(result.skipped) or result.testsRun!=5)
    unittest.main()
