import importlib
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

m = importlib.import_module('scripts.test.vector-topology-capacity')
resources = importlib.import_module('scripts.utils.capacity_resources')


class CapacityTest(unittest.TestCase):
    def test_exact_matrix_and_resource_contract(self):
        cases = m.matrix()
        self.assertEqual(len(cases), 15)
        self.assertEqual(len({c.key for c in cases}), 15)
        self.assertEqual({c.size for c in cases if c.kind == 'many'}, {1000,10000,100000})
        self.assertEqual({c.size for c in cases if c.kind == 'long'}, {1000,10000,100000})
        self.assertEqual({c.size for c in cases if c.kind == 'dense'}, {100,300,1000})
        self.assertTrue(all(c.operator == 'self' for c in cases if c.kind == 'long'))
        self.assertEqual((m.CPUS, m.MEMORY, m.SECONDS), (4, 2 << 30, 180))
        self.assertEqual(m.expected_rows(m.Case('dense',1000,'overlap')),499500)

    def test_controlled_plan_is_exact_balanced_and_separate_from_matrix(self):
        plan=m.measurement_plan('observation')
        self.assertEqual(''.join(p['observer'] for p in plan),'ABCB CACAB'.replace(' ',''))
        self.assertEqual(len({p['id'] for p in plan}),9)
        self.assertTrue(all(all(c in 'abcdefghijklmnopqrstuvwxyz0123456789-' for c in p['id']) for p in plan))
        self.assertTrue(all(p['case']==m.asdict(m.Case('many',100000,'self')) for p in plan))
        self.assertEqual([p['repetition'] for p in plan],[1]*3+[2]*3+[3]*3)
        self.assertEqual(len(m.measurement_plan('matrix')),15)
        self.assertTrue(all(p['observer']=='C' for p in m.measurement_plan('matrix')))
        with self.assertRaises(ValueError):m.measurement_plan('unexpected')

    def test_observer_modes_change_only_wrapping_and_internal_sampling(self):
        import sys
        from types import ModuleType
        operators=ModuleType('operators');geometry=ModuleType('operators.geometric_operators')
        def collect(parts):yield from parts
        def merge(parts):return parts
        geometry._self_overlap_intersections=collect;geometry.shapely=SimpleNamespace(union_all=merge)
        operators.geometric_operators=geometry
        for mode in 'ABC':
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as temporary,patch.dict(sys.modules,{'operators':operators,'operators.geometric_operators':geometry}):
                root=Path(temporary);report={}
                with self.assertRaisesRegex(RuntimeError,'original'):
                    with m.observe_compute(root,report,root/'progress.json',mode):
                        if mode=='A':self.assertIs(geometry._self_overlap_intersections,collect)
                        self.assertEqual(geometry.shapely.union_all(list(geometry._self_overlap_intersections([1,2]))),[1,2])
                        raise RuntimeError('original')
                observed=report['compute_observation']
                self.assertIs(geometry._self_overlap_intersections,collect);self.assertIs(geometry.shapely.union_all,merge)
                self.assertEqual((root/'compute-resources.jsonl').exists(),mode=='C')
                self.assertEqual((root/'progress.json').exists(),mode=='C')
                self.assertEqual(observed['collection_calls'],None if mode=='A' else 1)
                self.assertEqual(observed['fusion_input_fragments'],None if mode=='A' else 2)
                self.assertTrue(observed['samples']>0 if mode=='C' else observed['samples']==0)
        with self.assertRaises(ValueError):
            with m.observe_compute(Path('/unused'),{},Path('/unused/progress'),'wrong'):pass

    def test_summary_requires_all_same_source_image_and_oracle_with_signed_deltas(self):
        import copy
        groups=[]
        for item in m.measurement_plan('observation'):
            mode=item['observer'];seconds={'A':3.,'B':2.,'C':4.}[mode];count=100000
            observed={'observer':mode,'wrapping_enabled':mode!='A','sampling_enabled':mode=='C',
                'sampling_interval_seconds':.25 if mode=='C' else None,'samples':2 if mode=='C' else 0,
                'observed_memory_current_peak_bytes':{},'collection_seconds':None if mode=='A' else .5,
                'fusion_seconds':None if mode=='A' else .1,'collection_calls':None if mode=='A' else count,
                'fusion_calls':None if mode=='A' else count,'fusion_input_fragments':None if mode=='A' else 8*count}
            worker={'case':item['case'],'observer':mode,'measurement_id':item['id'],'source_frame_sha256':'a'*64,
                'result':'passed','stage':'complete','duration_seconds':seconds,'worker_peak_rss_bytes':10,
                'compute_observation':observed,'timings':{k:seconds if k=='compute' else 0 for k in ['generate','read','source_oracle','compute','save','reload_oracle']},
                'versions':{'python':'same'},'physical':{'rows':count,'crs':'EPSG:3857','all_coordinates_ids_lengths_verified':True,'full_result_sha256':'f'*64},
                'source':{'size_bytes':10,'sha256':'b'*64},'output':{'size_bytes':10,'sha256':'c'*64}}
            groups.append({**item,'result':'passed','cleanup':'passed','image_id':'same-image','worker':worker,
                'wall_seconds':seconds+.5,'resource_samples':10,
                'resource_before':{'cpu_usage_usec':1,'memory_peak_bytes':123,'memory_limit_bytes':m.MEMORY,'cpu_quota':m.CPUS,'swap_current_bytes':0,'oom':0,'oom_kill':0},
                'resource_after':{'cpu_usage_usec':1000001,'memory_peak_bytes':123,'memory_limit_bytes':m.MEMORY,'cpu_quota':m.CPUS,'swap_current_bytes':0,'oom':0,'oom_kill':0}})
        summary=m.observation_summary(groups)
        self.assertEqual(summary['median_differences']['wrapping_B_minus_A']['compute_seconds'],-1)
        self.assertEqual(summary['median_differences']['sampling_writes_C_minus_B']['compute_seconds'],2)
        for path,value in [(('image_id',),'different'),(('result',),'time_limit'),(('id',),'other'),
                           (('worker','source_frame_sha256'),'b'*64),(('worker','observer'),'B'),
                           (('worker','compute_observation','collection_calls'),0),
                           (('worker','physical','full_result_sha256'),'e'*64),
                           (('resource_after','swap_current_bytes'),1), (('resource_after','cpu_quota'),2)]:
            changed=copy.deepcopy(groups);target=changed[0]
            for key in path[:-1]:target=target[key]
            target[path[-1]]=value
            with self.subTest(path=path),self.assertRaises(AssertionError):m.observation_summary(changed)
        with self.assertRaises(AssertionError):m.observation_summary(groups[:-1])

    def test_closed_form_oracle_has_exact_order_and_unique_length(self):
        for c in [m.Case('many',6,'overlap'),m.Case('many',6,'self'),m.Case('long',1000,'self'),m.Case('dense',4,'overlap'),m.Case('dense',1000,'self')]:
            rows=list(m.expected(c));self.assertEqual(len(rows),m.expected_rows(c))
            self.assertTrue(all(r[0]['overlap_length'] == (1 if c.kind=='long' else 8) for r in rows))
        self.assertEqual([v[0]['source_id_a'] for v in m.expected(m.Case('many',6,'overlap'))],[m.source_id(i) for i in (0,2,4)])

    def test_limit_and_failure_are_never_passed_or_retried(self):
        state={'Running':True,'OOMKilled':False}
        self.assertEqual(m.classify(0,False,state,{}),'passed')
        self.assertEqual(m.classify(1,False,state,{}),'failed')
        self.assertEqual(m.classify(0,True,state,{}),'time_limit')
        self.assertEqual(m.classify(137,False,state,{'oom_kill':1}),'memory_limit')
        self.assertEqual(m.classify(137,False,{'OOMKilled':True},{}),'memory_limit')
        self.assertEqual(m.classify(0,False,{'Running':False},{}),'failed')

    def test_foreign_container_refused_before_delete(self):
        with patch.object(m,'inspect',return_value={'Config':{'Labels':{'com.addp.capacity-run':'other'}}}),patch.object(m,'command',return_value='candidate\n') as execute:
            with self.assertRaisesRegex(RuntimeError,'foreign'):m.owned_remove('candidate','ours')
            self.assertTrue(all(call.args[0][1:3] == ['container','ls'] for call in execute.call_args_list))

    def test_cleanup_requires_positive_docker_absence_not_an_inspect_error(self):
        with patch.object(m,'command',return_value='') as execute,patch.object(m,'inspect') as inspect:
            m.owned_remove('missing','ours');inspect.assert_not_called()
        with patch.object(m,'command',side_effect=RuntimeError('daemon unavailable')):
            with self.assertRaisesRegex(RuntimeError,'daemon'):m.owned_remove('missing','ours')

    def test_incomplete_wrong_case_or_over_budget_worker_report_never_passes(self):
        import copy
        case=m.Case('dense',100,'self')
        report={'case':m.asdict(case),'observer':'C','measurement_id':case.key,'source_frame_sha256':'c'*64,'result':'passed','stage':'complete','duration_seconds':1,
            'worker_peak_rss_bytes':1,
            'compute_observation':{'observer':'C','wrapping_enabled':True,'sampling_enabled':True,
                'sampling_interval_seconds':.25,'observed_memory_current_peak_bytes':{},'fusion_calls':1,'fusion_input_fragments':1,'collection_calls':1,
                'samples':2,'collection_seconds':0,'fusion_seconds':0},
            'timings':{k:0 for k in ['generate','read','source_oracle','compute','save','reload_oracle']},
            'physical':{'rows':1,'all_coordinates_ids_lengths_verified':True,'crs':'EPSG:3857'},
            'source':{'size_bytes':1,'sha256':'a'*64},'output':{'size_bytes':1,'sha256':'b'*64}}
        m.validate_worker(report,case)
        for field,value in [('stage','compute'),('case',m.asdict(m.Case('dense',300,'self'))),('duration_seconds',181)]:
            changed=copy.deepcopy(report);changed[field]=value
            with self.subTest(field=field),self.assertRaises(AssertionError):m.validate_worker(changed,case)
        for field,value in [('fusion_input_fragments',4950),('collection_calls',0),('samples',0),('fusion_seconds',181)]:
            changed=copy.deepcopy(report);changed['compute_observation'][field]=value
            with self.subTest(field=field),self.assertRaises(AssertionError):m.validate_worker(changed,case)


    def test_compute_observer_restores_production_functions_on_failure(self):
        import sys
        from types import ModuleType
        operators = ModuleType('operators'); geometry = ModuleType('operators.geometric_operators')
        def collect(segments): yield from segments
        def merge(parts): return parts
        geometry._self_overlap_intersections = collect
        geometry.shapely = SimpleNamespace(union_all=merge)
        operators.geometric_operators = geometry
        with tempfile.TemporaryDirectory() as temporary, patch.dict(sys.modules, {'operators':operators, 'operators.geometric_operators':geometry}):
            root = Path(temporary); report = {'result':'incomplete'}
            with self.assertRaisesRegex(RuntimeError, 'original failure'):
                with m.observe_compute(root, report, root/'progress.json'):
                    self.assertEqual(list(geometry._self_overlap_intersections([1])), [1])
                    self.assertEqual(geometry.shapely.union_all([1]), [1])
                    raise RuntimeError('original failure')
            self.assertIs(geometry._self_overlap_intersections, collect)
            self.assertIs(geometry.shapely.union_all, merge)
            self.assertGreater(report['compute_observation']['samples'], 0)
            self.assertEqual(report['compute_observation']['fusion_input_fragments'], 1)



    def test_compute_observer_memory_is_stage_sample_not_cumulative_peak(self):
        import sys
        from types import ModuleType
        operators=ModuleType('operators');geometry=ModuleType('operators.geometric_operators')
        geometry._self_overlap_intersections=lambda parts:iter(parts)
        geometry.shapely=SimpleNamespace(union_all=lambda parts:parts)
        operators.geometric_operators=geometry
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);group=root/'cgroup';group.mkdir()
            for name,value in [('memory.current','128'),('memory.peak','256'),('memory.max',str(2<<30))]:
                (group/name).write_text(value)
            with patch.dict(sys.modules,{'operators':operators,'operators.geometric_operators':geometry}), \
                 patch.object(m.sys,'platform','linux'),patch.object(m,'Path',return_value=group):
                report={}
                with m.observe_compute(root,report,root/'progress.json'):
                    list(geometry._self_overlap_intersections([1]))
                    geometry.shapely.union_all([1])
            samples=[json.loads(line) for line in (root/'compute-resources.jsonl').read_text().splitlines()]
            self.assertTrue(all(s['memory_current_bytes']==128 and s['memory_peak_bytes']==256 for s in samples))
            self.assertTrue(all(v==128 for v in report['compute_observation']['observed_memory_current_peak_bytes'].values()))


    def test_manual_gate_refuses_personal_environment_before_docker(self):
        with patch.dict(m.os.environ,{},clear=True),patch.object(m,'command') as execute:
            with self.assertRaisesRegex(RuntimeError,'manual isolated'):m.run_gate()
            execute.assert_not_called()

    def test_timeout_and_unknown_create_both_clean_exact_owned_container_and_workspace(self):
        case=m.Case('many',6,'self')
        for unknown in (False,True):
            with self.subTest(unknown=unknown),tempfile.TemporaryDirectory() as temporary:
                root=Path(temporary);name=f'addp-topology-capacity-ours-{case.key}';exists=False;running=False;calls=[]
                def execute(args,**kwargs):
                    nonlocal exists,running
                    calls.append(args)
                    if args[1]=='create':
                        exists=True
                        if unknown:raise RuntimeError('unknown CLI outcome after create')
                    if args[1]=='start':running=True
                    if args[1]=='kill':running=False
                    if args[1]=='rm':exists=False
                    if args[1:3]==['container','ls']:return name+'\n' if exists else ''
                    return ''
                def inspect(_):
                    return {'Config':{'Cmd':['sh','container_entrypoint.sh'],'Labels':{'com.addp.capacity-run':'ours'}},
                        'HostConfig':{'NanoCpus':4_000_000_000,'Memory':2<<30,'MemorySwap':2<<30},
                        'State':{'Pid':42,'Running':running,'OOMKilled':False},'Image':'sha256:our-image'}
                class Process:
                    code=None
                    def __init__(self,*args,**kwargs):
                        (root/case.key/'data/progress.json').write_text(json.dumps({'result':'incomplete','stage':'compute'}))
                    def poll(self):return self.code
                    def kill(self):self.code=-9
                    def wait(self):return self.code
                metrics={'process_peak_rss_bytes':20,'oom':0,'oom_kill':0,'swap_current_bytes':0}
                with patch.object(m,'command',side_effect=execute),patch.object(m,'inspect',side_effect=inspect),\
                     patch.object(m.subprocess,'run',return_value=SimpleNamespace(returncode=0,stdout='',stderr='')),\
                     patch.object(m.subprocess,'Popen',side_effect=Process) as process,\
                     patch.object(resources,'cgroup_directory',return_value=root),patch.object(resources,'read_metrics',return_value=metrics),\
                     patch.object(m.time,'monotonic',side_effect=[0,1,181,182]):
                    result=m.run_case(case,'our-image','ours',root)
                self.assertFalse(exists);self.assertEqual(result['cleanup'],'passed')
                self.assertFalse((root/case.key/'data').exists())
                self.assertEqual(sum(c[1]=='create' for c in calls),1)
                self.assertEqual(result['result'],'failed' if unknown else 'time_limit')
                self.assertEqual(process.call_count,0 if unknown else 1)

    def test_cgroup_limits_and_path_are_fail_closed(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);group=root/'group';group.mkdir();proc=root/'proc';(proc/'42').mkdir(parents=True)
            (proc/'42/cgroup').write_text('0::/group\n')
            self.assertEqual(resources.cgroup_directory(42,proc,root),group)
            for key,value in {'memory.events':'oom 0\noom_kill 0\nmax 0\n','cpu.stat':'usage_usec 12\n','memory.max':str(2<<30),'cpu.max':'400000 100000','memory.swap.max':'0','cgroup.procs':'','memory.current':'10','memory.peak':'20','memory.swap.current':'0'}.items():(group/key).write_text(value)
            self.assertEqual(resources.read_metrics(group,proc)['cpu_quota'],4)
            for key,value in [('memory.max','1024'),('cpu.max','200000 100000'),('memory.swap.max','1024')]:
                original=(group/key).read_text();(group/key).write_text(value)
                with self.assertRaises(resources.ResourceError):resources.read_metrics(group,proc)
                (group/key).write_text(original)
            (proc/'42/cgroup').write_text('0::/../foreign\n')
            with self.assertRaises(resources.ResourceError):resources.cgroup_directory(42,proc,root)

    def test_ci_registration_rejects_missing_gate_limits_and_scheduled_capacity(self):
        import importlib.util
        repository=Path(__file__).resolve().parents[2]
        spec=importlib.util.spec_from_file_location('capacity_registration',repository/'scripts/ci/check-python-ci-registration.py')
        check=importlib.util.module_from_spec(spec);spec.loader.exec_module(check)
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)
            files=['Makefile','.github/workflows/vector-topology-capacity.yml','scripts/test/vector-topology-capacity.py','engines/geopython-workflow/test_topology_capacity.py']
            for f in files:
                p=root/f;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes((repository/f).read_bytes())
            self.assertEqual(check.validate_topology_capacity(root),[])
            for f,old,new in [
                ('Makefile','test_topology_capacity.py','not_registered.py'),
                (files[1],'  workflow_dispatch:','  push:'),
                (files[1],'run: make test-vector-topology-capacity\n','run: true\n'),
                (files[2],'SECONDS = 180','SECONDS = 181'),
                (files[2],"'--memory-swap'","'--removed-swap'"),
                (files[1],'default: matrix','default: observation'),
                (files[1],'TOPOLOGY_CAPACITY_PROFILE: ${{ inputs.profile }}','REMOVED: observation'),
                (files[3],'test_topology_capacity_native_observer_modes_preserve_exact_result','removed_observer_test'),
            ]:
                p=root/f;original=p.read_text();self.assertIn(old,original);p.write_text(original.replace(old,new))
                with self.subTest(f=f,old=old):self.assertTrue(check.validate_topology_capacity(root))
                p.write_text(original)


if __name__=='__main__':unittest.main()
