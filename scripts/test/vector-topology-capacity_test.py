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
    def test_chain_plan_is_only_five_new_families_fifteen_unwrapped_measurements(self):
        self.assertEqual(m.SCHEMA,'addp.vector-topology-capacity/v3')
        cases=m.chain_matrix();plan=m.measurement_plan('chains')
        self.assertEqual(len(cases),15);self.assertEqual(len({c.key for c in cases}),15)
        self.assertEqual({c.kind for c in cases},{'chain-open','chain-branch','chain-ring','chain-rings','chain-hub'})
        for kind in {c.kind for c in cases}:
            self.assertEqual([c.size for c in cases if c.kind==kind],[128,512,2048] if kind=='chain-hub' else [1000,10000,100000])
        self.assertEqual([p['case'] for p in plan],[m.asdict(c) for c in cases])
        self.assertTrue(all(p['observer']=='A' and p['repetition'] is None for p in plan))
        self.assertFalse({c.key for c in cases}&{c.key for c in m.matrix()})
        self.assertEqual({m.Case(**p['case']).operator for p in plan},{'chains'})

    def test_chain_closed_form_membership_is_total_exact_and_stable(self):
        for kind,size in [('chain-open',12),('chain-branch',20),('chain-ring',12),('chain-rings',12),('chain-hub',6)]:
            case=m.Case(kind,size,'chains');walks=[list(w) for w in m.chain_walks(case)]
            self.assertEqual(sorted(row for w in walks for row,reverse in w),list(range(size)))
            self.assertEqual(len(walks),m.chain_counts(case)['result'])
            outputs=list(m.chain_expected(case,'result'));members=list(m.chain_expected(case,'members'))
            self.assertEqual(len(members),size);self.assertEqual(sum(p['member_count'] for p,g in outputs),size)
            self.assertEqual([min(row for row,reverse in w) for w in walks],sorted(min(row for row,reverse in w) for w in walks))
            for attrs,points in members:
                row=int(attrs['route_key'].split('-')[1]);self.assertEqual(points,m.chain_part(case,row)[1])
                self.assertIs(type(attrs['is_reversed']),bool)
        branch=list(m.chain_expected(m.Case('chain-branch',10,'chains'),'result'))
        self.assertEqual([p['member_count'] for p,g in branch],[2,1,2,4,1])
        self.assertEqual(branch[3][1],[[10,6],[12,6],[12,8],[10,8],[10,6]])
        case=m.Case('chain-branch',20,'chains')
        self.assertEqual(m.chain_part(case,0)[1][-1],m.chain_part(case,11)[1][0])
        self.assertNotEqual(m.chain_part(case,0)[0]['grade'],m.chain_part(case,11)[0]['grade'])
        for case in [m.Case('chain-ring',5,'chains'),m.Case('chain-branch',11,'chains'),m.Case('chain-hub',2,'chains')]:
            with self.assertRaises(AssertionError):m.chain_counts(case)

    def test_chain_worker_requires_both_full_oracles_identity_and_original_budget(self):
        import copy
        case=m.Case('chain-branch',10,'chains');report={
            'case':m.asdict(case),'observer':'A','measurement_id':case.key,'result':'passed','stage':'complete',
            'duration_seconds':1.,'worker_peak_rss_bytes':1,'source_frame_sha256':'f'*64,
            'timings':{k:0. for k in ['generate','read','source_oracle','compute','save_result','save_members','reload_oracle','source_unchanged']},
            'implementation_identity':{'loaded_merge_matches_source':True,'module_sha256':'b'*64,'merge_line_chains_sha256':'c'*64},
            'physical':{port:{'rows':m.chain_counts(case)[port],'crs':'EPSG:3857','full_result_sha256':'d'*64,'all_coordinates_attributes_membership_verified':True} for port in ['result','members']},
            'source':{'size_bytes':1,'sha256':'e'*64},'outputs':{port:{'size_bytes':1,'sha256':'e'*64} for port in ['result','members']}}
        m.validate_worker(report,case,'A')
        for path,value in [(('duration_seconds',),181),(('duration_seconds',),float('nan')),(('stage',),'save_result'),
                (('physical','members','rows'),9),(('physical','result','crs'),'EPSG:4326'),
                (('physical','result','all_coordinates_attributes_membership_verified'),False),
                (('implementation_identity','loaded_merge_matches_source'),False),
                (('outputs','members','size_bytes'),0),(('outputs','result','sha256'),'bad'),
                (('observer',),'C'),(('measurement_id',),'foreign')]:
            changed=copy.deepcopy(report);target=changed
            for key in path[:-1]:target=target[key]
            target[path[-1]]=value
            with self.subTest(path=path),self.assertRaises(AssertionError):m.validate_worker(changed,case,'A')
        for key in ['members','result']:
            changed=copy.deepcopy(report);del changed['physical'][key]
            with self.assertRaises(AssertionError):m.validate_worker(changed,case,'A')
        changed=copy.deepcopy(report);changed['compute_observation']={}
        with self.assertRaises(AssertionError):m.validate_worker(changed,case,'A')

    def test_chain_actual_loaded_function_is_bound_to_compiled_source(self):
        with tempfile.TemporaryDirectory() as temporary:
            path=Path(temporary)/'geometry.py';source='def merge_line_chains(frame, field, fields):\n    return frame\n'
            path.write_text(source);namespace={};exec(compile(source,str(path),'exec'),namespace)
            module=SimpleNamespace(__file__=str(path),merge_line_chains=namespace['merge_line_chains'])
            self.assertEqual(m.loaded_implementation_identity(module,'chains'),{**m.implementation_identity(source,'chains'),'loaded_merge_matches_source':True})
            module.merge_line_chains=lambda *args:None
            with self.assertRaisesRegex(AssertionError,'loaded merge'):m.loaded_implementation_identity(module,'chains')

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

    def test_comparison_plan_is_balanced_paired_100k_and_unwrapped(self):
        plan=m.measurement_plan('comparison')
        self.assertEqual([p['implementation'] for p in plan],['old','new','new','old','old','new','new','old'])
        self.assertEqual([p['repetition'] for p in plan],[1,1,2,2,3,3,4,4])
        self.assertEqual(len({p['id'] for p in plan}),8)
        self.assertTrue(all(p['case']==m.asdict(m.Case('many',100000,'self')) and p['observer']=='A' for p in plan))

    def test_comparison_sources_change_only_fixed_collectors(self):
        import ast
        repo=Path(__file__).resolve().parents[2]
        variants,manifest=m.comparison_sources(repo)
        self.assertEqual(manifest['legacy_commit'],'8a3006670e7f6cfaddf018253515ccb37ab5bc78')
        self.assertEqual(manifest['batched_commit'],'adb605b67676bc4396307bc9ea7449c63de183bc')
        def rest(text):
            return ast.dump(ast.Module(body=[n for n in ast.parse(text).body if m.definition_name(n) not in m.COMPARISON_DEFINITIONS],type_ignores=[]),include_attributes=False)
        self.assertEqual(rest(variants['old']),rest(variants['new']))
        for name in ['old','new']:
            self.assertEqual(m.implementation_identity(variants[name]),manifest[name])
        self.assertNotIn('def _self_overlap_candidate_batches',variants['old'])
        self.assertIn('shapely.intersection(segment, segments[b])',variants['old'])
        self.assertIn('shapely.intersection(segments[left], segments[right])',variants['new'])
        with self.assertRaises(AssertionError):
            m.comparison_variant(variants['new'].replace('_SELF_OVERLAP_PAIR_LIMIT = 2048','_SELF_OVERLAP_PAIR_LIMIT = 2049'),variants['old'],variants['new'])
        with self.assertRaises(AssertionError):m.comparison_variant(variants['new'],variants['new'],variants['new'])

    def test_comparison_image_layers_and_execution_configuration_fail_closed(self):
        import copy
        base={'Id':'sha256:new','RootFS':{'Layers':['a','b']},'Config':{'Cmd':['sh','container_entrypoint.sh'],'Env':['ONE=1'],'WorkingDir':'/app'}}
        old=copy.deepcopy(base);old['Id']='sha256:old';old['RootFS']['Layers'].append('copy')
        proof=m.comparison_image_proof(base,old)
        self.assertEqual(proof['base_image_id'],base['Id'])
        for path,value in [(('Id',),'sha256:new'),(('RootFS','Layers'),['foreign','copy']), (('Config','Cmd'),['python','other']), (('Config','Env'),['ONE=2'])]:
            changed=copy.deepcopy(old);target=changed
            for key in path[:-1]:target=target[key]
            target[path[-1]]=value
            with self.subTest(path=path),self.assertRaises(AssertionError):m.comparison_image_proof(base,changed)

    def test_comparison_summary_requires_loaded_code_identity_and_exact_oracle(self):
        import copy
        manifest={name:{'module_sha256':name[0]*64,'collector_sha256':name[-1]*64} for name in ['old','new']}
        proof={'base_image_id':'new-image','images':{'old':'old-image','new':'new-image'}}
        groups=[]
        for item in m.measurement_plan('comparison'):
            seconds=10. if item['implementation']=='old' else 6.
            observed={'observer':'A','wrapping_enabled':False,'sampling_enabled':False,'sampling_interval_seconds':None,'samples':0,'observed_memory_current_peak_bytes':{},**{k:None for k in ['collection_seconds','fusion_seconds','collection_calls','fusion_calls','fusion_input_fragments']}}
            worker={'case':item['case'],'observer':'A','measurement_id':item['id'],'source_frame_sha256':'a'*64,'result':'passed','stage':'complete','duration_seconds':seconds,'worker_peak_rss_bytes':10,'implementation_identity':{**manifest[item['implementation']],'loaded_collector_matches_source':True},'compute_observation':observed,'timings':{k:seconds if k=='compute' else 0 for k in ['generate','read','source_oracle','compute','save','reload_oracle']},'versions':{'python':'same'},'physical':{'rows':100000,'crs':'EPSG:3857','all_coordinates_ids_lengths_verified':True,'full_result_sha256':'f'*64},'source':{'size_bytes':10,'sha256':'b'*64},'output':{'size_bytes':10,'sha256':'c'*64}}
            metrics={'cpu_usage_usec':1,'memory_peak_bytes':123,'memory_limit_bytes':m.MEMORY,'cpu_quota':4,'swap_current_bytes':0,'oom':0,'oom_kill':0}
            groups.append({**item,'result':'passed','cleanup':'passed','image_id':proof['images'][item['implementation']],'worker':worker,'wall_seconds':seconds+.5,'resource_samples':10,'resource_before':metrics,'resource_after':{**metrics,'cpu_usage_usec':1000001}})
        result=m.comparison_summary(groups,manifest,proof)
        self.assertEqual(result['pairs'][0]['compute_seconds_saved'],4.)
        self.assertAlmostEqual(result['pairs'][0]['compute_reduction_percent'],40.)
        self.assertAlmostEqual(result['paired_medians']['compute_speedup'],10/6)
        slower=copy.deepcopy(groups)
        for group in slower:
            if group['implementation']=='new':
                group['wall_seconds']=12.5;group['worker']['duration_seconds']=12.
                group['worker']['timings']['compute']=12.
        self.assertEqual(m.comparison_summary(slower,manifest,proof)['paired_medians']['compute_seconds_saved'],-2.)
        for path,value in [(('image_id',),'wrong'),(('implementation',),'new'),(('worker','implementation_identity','collector_sha256'),'x'*64),(('worker','implementation_identity','module_sha256'),'x'*64),(('worker','versions'),{'python':'other'}),(('worker','source_frame_sha256'),'b'*64),(('worker','physical','full_result_sha256'),'e'*64),(('result',),'time_limit'),(('cleanup',),'failed'),(('resource_after','swap_current_bytes'),1),(('resource_after','cpu_usage_usec'),0)]:
            changed=copy.deepcopy(groups);target=changed[0]
            for key in path[:-1]:target=target[key]
            target[path[-1]]=value
            with self.subTest(path=path),self.assertRaises(AssertionError):m.comparison_summary(changed,manifest,proof)
        with self.assertRaises(AssertionError):m.comparison_summary(groups[:-1],manifest,proof)

    def test_comparison_cleanup_removes_old_before_base_and_continues_after_failure(self):
        calls=[]
        def remove(args):
            calls.append(args[-1])
            if args[-1]=='old':raise RuntimeError('old removal failed')
        with patch.object(m.subprocess,'run',side_effect=[SimpleNamespace(returncode=0),SimpleNamespace(returncode=0),SimpleNamespace(returncode=1)]),patch.object(m,'command',side_effect=remove):
            errors=m.cleanup_images(((False,'foreign'),(True,'old'),(True,'new')))
        self.assertEqual(calls,['old','new'])
        self.assertEqual(errors,['old removal failed'])

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
        for case in (m.Case('many',6,'self'),m.Case('compose-open',8,'normalize-chains')):
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
                            (root/case.key/'data/progress.json').write_text(json.dumps({'result':'incomplete','stage':'normalize' if case.operator=='normalize-chains' else 'compute'}))
                        def poll(self):return self.code
                        def kill(self):self.code=-9
                        def wait(self):return self.code
                    metrics={'process_peak_rss_bytes':20,'oom':0,'oom_kill':0,'swap_current_bytes':0,'memory_current_bytes':128}
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

                    if not unknown and case.operator=='normalize-chains':
                        self.assertEqual(result['worker']['stage'],'normalize')
                        self.assertEqual(result['stage_memory_samples']['normalize'],{'samples':1,'observed_memory_current_peak_bytes':128})
                        self.assertEqual(result['stage_memory_samples']['merge'],{'samples':0,'observed_memory_current_peak_bytes':None})
                        sample=json.loads((root/case.key/'resources.jsonl').read_text().splitlines()[0])
                        self.assertEqual(sample['worker_stage'],'normalize')

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
                (files[1],'          - comparison','          - removed'),
                (files[1],'          - chains','          - removed'),
                (files[1],'          - normalize\n','          - removed\n'),
                (files[1],'          - normalize-chains','          - removed'),
                (files[1],'TOPOLOGY_CAPACITY_CASE: ${{ inputs.case }}','REMOVED: all'),
                (files[1],'          - compose-grid-512-normalize-chains','          - removed'),
                (files[2],"'case_selection': case_key","'removed_selection': case_key"),
                (files[2],'ogr.Feature.IsFieldSetAndNotNull','removed_null_safety'),
                (files[2],'encoder = json.JSONEncoder(sort_keys=True).encode','encoder = None'),
                (files[3],'test_composition_capacity_native_indexed_reader_matches_frozen_scalar_full_digest','removed_reader_digest'),
                (files[2],'def composition_matrix():','def removed_composition_matrix():'),
                (files[2],'def verify_composition_output(case, path, port):','def removed_composition_output(case, path, port):'),
                (files[2],'def run_composition_worker(','def removed_composition_worker('),
                (files[3],'test_composition_capacity_native_three_outputs_exact_oracle_unwrapped','removed_composition_native'),
                (files[3],'test_composition_capacity_native_rejects_real_three_output_corruption','removed_composition_corruption'),
                (files[2],'def normalize_matrix():','def removed_normalize_matrix():'),
                (files[2],'def verify_normalize_output(case, path, port):','def removed_normalize_output(case, path, port):'),
                (files[2],"'loaded_normalize_matches_source'","'removed_normalize_binding'"),
                (files[3],'test_normalize_capacity_native_dual_output_exact_oracle_unwrapped','removed_normalize_native'),
                (files[3],'test_normalize_capacity_native_rejects_real_dual_output_corruption','removed_normalize_corruption'),
                (files[1],'timeout-minutes: 90','timeout-minutes: 91'),
                (files[2],"SCHEMA = 'addp.vector-topology-capacity/v3'","SCHEMA = 'removed'"),
                (files[2],"if profile == 'chains':","if profile == 'removed':"),
                (files[2],'def verify_chain_output(case, path, port):','def removed_chain_output(case, path, port):'),
                (files[2],"'loaded_merge_matches_source'","'removed_merge_binding'"),
                (files[3],'test_chain_capacity_native_dual_outputs_and_unwrapped_production','removed_chain_native'),
                (files[3],'test_chain_capacity_native_rejects_real_dual_output_corruption','removed_chain_corruption'),
                (files[1],'fetch-depth: 0','fetch-depth: 1'),
                (files[2],'def comparison_sources(repository):','def removed_sources(repository):'),
                (files[2],"def loaded_implementation_identity(module, operator='self'):",'def removed_identity(module):'),
                (files[3],'test_topology_capacity_native_comparison_variants_keep_exact_oracle','removed_comparison_native'),
                (files[1],'TOPOLOGY_CAPACITY_PROFILE: ${{ inputs.profile }}','REMOVED: observation'),
                (files[3],'test_topology_capacity_native_observer_modes_preserve_exact_result','removed_observer_test'),
            ]:
                p=root/f;original=p.read_text();self.assertIn(old,original);p.write_text(original.replace(old,new))
                with self.subTest(f=f,old=old):self.assertTrue(check.validate_topology_capacity(root))
                p.write_text(original)


class NormalizeCapacityTest(unittest.TestCase):
    def test_normalize_plan_is_only_fifteen_new_once_unwrapped_measurements(self):
        cases=m.normalize_matrix();plan=m.measurement_plan('normalize')
        self.assertEqual(len(cases),15);self.assertEqual(len({c.key for c in cases}),15)
        expected={'normalize-sparse':[1000,10000,100000],'normalize-duplicates':[128,512,2048],
                  'normalize-parts':[128,512,2048],'normalize-grid':[32,128,512],'normalize-sampling':[32,128,512]}
        self.assertEqual({c.kind for c in cases},set(expected))
        for kind,sizes in expected.items():self.assertEqual([c.size for c in cases if c.kind==kind],sizes)
        self.assertEqual([p['case'] for p in plan],[m.asdict(c) for c in cases])
        self.assertTrue(all(p['observer']=='A' and p['repetition'] is None and p['case']['operator']=='normalize' for p in plan))
        self.assertFalse({c.key for c in cases}&{c.key for c in m.matrix()+m.chain_matrix()})
        self.assertEqual((m.CPUS,m.MEMORY,m.SECONDS,m.NORMALIZE_SCHEMA),(4,2<<30,180,'addp.vector-topology-capacity/v4'))
        self.assertEqual(m.normalize_counts(m.Case('normalize-grid',512,'normalize')),{'sources':1024,'result':525312,'members':525312})
        self.assertEqual(m.normalize_counts(m.Case('normalize-sampling',512,'normalize')),{'sources':512,'result':512,'members':262144})

    def test_normalize_closed_form_counts_membership_direction_and_typed_nulls(self):
        from collections import Counter
        for kind in ['normalize-sparse','normalize-duplicates','normalize-parts','normalize-grid','normalize-sampling']:
            case=m.Case(kind,4,'normalize');edges=list(m.normalize_expected(case,'result'));members=list(m.normalize_expected(case,'members'))
            self.assertEqual(len(edges),m.normalize_counts(case)['result']);self.assertEqual(len(members),m.normalize_counts(case)['members'])
            counts=Counter(p['edge_id'] for p,g in members)
            self.assertEqual(counts,{p['edge_id']:p['occurrence_count'] for p,g in edges})
            for p,g in members:
                row=int(p['source_id'].split('-')[1]);self.assertEqual(p['route_key'],p['source_id'])
                self.assertEqual({k:p[k] for k in m.normalize_attributes(row)},m.normalize_attributes(row))
                canonical=edges[p['edge_id']-1][1]
                self.assertEqual(g,canonical[::-1] if p['is_reversed'] else canonical)
            if kind=='normalize-parts':
                self.assertEqual([p['source_part_no'] for p,g in members],[1,2,3,4]);self.assertEqual(len({p['source_id'] for p,g in members}),1)
        attrs=m.normalize_attributes(1);self.assertIsNone(attrs['grade']);self.assertIsNone(attrs['enabled'])
        self.assertGreater(m.normalize_attributes(0)['grade'],2**53)

    def test_normalize_loaded_identity_rejects_monkeypatch_and_stale_code(self):
        with tempfile.TemporaryDirectory() as temporary:
            path=Path(temporary)/'geometry.py';source='def normalize_line_network(frame, field):\n    return frame\n';path.write_text(source)
            namespace={};exec(compile(source,str(path),'exec'),namespace)
            module=SimpleNamespace(__file__=str(path),normalize_line_network=namespace['normalize_line_network'])
            self.assertEqual(m.loaded_implementation_identity(module,'normalize'),{**m.implementation_identity(source,'normalize'),'loaded_normalize_matches_source':True})
            module.normalize_line_network=lambda *args:None
            with self.assertRaisesRegex(AssertionError,'loaded normalize'):m.loaded_implementation_identity(module,'normalize')

    def test_normalize_worker_validation_requires_both_ports_and_strict_budget(self):
        import copy
        case=m.Case('normalize-sampling',4,'normalize')
        report={'case':m.asdict(case),'observer':'A','measurement_id':case.key,'result':'passed','stage':'complete',
            'duration_seconds':1.,'worker_peak_rss_bytes':1,'source_frame_sha256':'f'*64,
            'timings':{k:0. for k in ['generate','read','source_oracle','compute','save_result','save_members','reload_oracle','source_unchanged']},
            'implementation_identity':{'loaded_normalize_matches_source':True,'module_sha256':'a'*64,'normalize_line_network_sha256':'b'*64},
            'physical':{p:{'rows':m.normalize_counts(case)[p],'crs':'EPSG:3857','full_result_sha256':'c'*64,'all_coordinates_attributes_membership_verified':True} for p in ('result','members')},
            'source':{'size_bytes':1,'sha256':'d'*64},'outputs':{p:{'size_bytes':1,'sha256':'d'*64} for p in ('result','members')}}
        m.validate_worker(report,case,'A')
        for path,value in [(('duration_seconds',),181),(('duration_seconds',),float('nan')),(('observer',),'C'),
            (('physical','members','rows'),4),(('physical','result','all_coordinates_attributes_membership_verified'),False),
            (('implementation_identity','loaded_normalize_matches_source'),False),(('outputs','members','size_bytes'),0),
            (('physical','result','crs'),'EPSG:4326'),(('stage',),'save_members')]:
            changed=copy.deepcopy(report);target=changed
            for key in path[:-1]:target=target[key]
            target[path[-1]]=value
            with self.subTest(path=path),self.assertRaises(AssertionError):m.validate_worker(changed,case,'A')
        for port in ('result','members'):
            changed=copy.deepcopy(report);del changed['outputs'][port]
            with self.assertRaises(AssertionError):m.validate_worker(changed,case,'A')


class CompositionCapacityTest(unittest.TestCase):
    def test_composition_plan_is_fifteen_new_measurements_and_original_budget(self):
        cases=m.composition_matrix(); plan=m.measurement_plan('normalize-chains')
        self.assertEqual({k:[c.size for c in cases if c.kind==k] for k in {c.kind for c in cases}}, {
            'compose-sparse':[1000,10000,100000], 'compose-open':[1000,10000,100000],
            'compose-sampling':[32,128,512], 'compose-grid':[32,128,512], 'compose-ring':[1000,10000,100000]})
        self.assertEqual(len(cases),15); self.assertEqual(len({c.key for c in cases}),15)
        self.assertTrue(all(p['observer']=='A' and p['repetition'] is None and p['case']['operator']=='normalize-chains' for p in plan))
        self.assertEqual([p['case'] for p in plan],[m.asdict(c) for c in cases])
        self.assertFalse({c.key for c in cases}&{c.key for c in m.matrix()+m.chain_matrix()+m.normalize_matrix()})
        self.assertEqual((m.CPUS,m.MEMORY,m.SECONDS,m.COMPOSITION_SCHEMA),(4,2<<30,180,'addp.vector-topology-capacity/v5'))
        self.assertEqual(m.composition_counts(m.Case('compose-grid',512,'normalize-chains')),{'sources':1024,'chains':525312,'chain_edges':525312,'edge_sources':525312})
        self.assertEqual(m.composition_counts(m.Case('compose-sampling',512,'normalize-chains')),{'sources':512,'chains':1,'chain_edges':512,'edge_sources':262144})

    def test_composition_single_case_selector_is_exact_and_fails_before_mutation(self):
        key='compose-grid-512-normalize-chains'
        expected=[v for v in m.measurement_plan('normalize-chains') if v['id']==key]
        self.assertEqual(m.measurement_plan('normalize-chains',key),expected)
        self.assertEqual(len(expected),1); self.assertEqual(m.measurement_plan('normalize-chains','all'),m.measurement_plan('normalize-chains'))
        with patch.object(m,'command') as docker:
            for profile,case in [('normalize',key),('matrix',key),('normalize-chains','compose-grid-128-normalize-chains'),('normalize-chains','unknown'),('normalize-chains','')]:
                with self.subTest(profile=profile,case=case),self.assertRaises(ValueError):m.run_gate(profile,case)
            docker.assert_not_called()
        for cases in ([],[m.Case('compose-grid',512,'normalize-chains')]*2):
            with patch.object(m,'composition_matrix',return_value=cases),self.assertRaises(ValueError):m.measurement_plan('normalize-chains',key)

    def test_source_expected_attributes_are_built_once_per_original_source(self):
        for kind in ('normalize-grid','normalize-sampling','normalize-parts'):
            case=m.Case(kind,4,'normalize')
            with patch.object(m,'normalize_attributes',wraps=m.normalize_attributes) as build:
                rows=list(m.normalize_expected(case,'members'))
            self.assertEqual(len(rows),m.normalize_counts(case)['members'])
            self.assertEqual(build.call_count,m.normalize_counts(case)['sources'])

    def test_composition_closed_forms_exact_relation_and_direction_xor(self):
        from collections import Counter
        for kind in ('compose-sparse','compose-open','compose-sampling','compose-grid','compose-ring'):
            case=m.Case(kind,8,'normalize-chains'); counts=m.composition_counts(case)
            rows={p:list(m.composition_expected(case,p)) for p in m.COMPOSITION_PORTS}
            self.assertEqual({p:len(v) for p,v in rows.items()},{p:counts[p] for p in m.COMPOSITION_PORTS})
            edges={p['edge_id']:(p,g) for p,g in rows['chain_edges']}
            occurrence=Counter(p['edge_id'] for p,g in rows['edge_sources'])
            self.assertEqual(occurrence,{p['edge_id']:p['occurrence_count'] for p,g in rows['chain_edges']})
            self.assertEqual(sum(p['member_count'] for p,g in rows['chains']),len(edges))
            chains={p['chain_id']:(p,g) for p,g in rows['chains']}
            for chain_id,(chain,coordinates) in chains.items():
                rebuilt=[]; selected=[(p,g) for p,g in rows['chain_edges'] if p['chain_id']==chain_id]
                self.assertEqual([p['member_no'] for p,g in selected],list(range(1,len(selected)+1)))
                for p,g in selected:
                    oriented=g[::-1] if p['is_reversed'] else g
                    if rebuilt:self.assertEqual(rebuilt[-1],oriented[0])
                    rebuilt.extend(oriented if not rebuilt else oriented[1:])
                self.assertEqual(rebuilt,coordinates)
            for p,g in rows['edge_sources']:
                edge,canonical=edges[p['edge_id']]; original_reversed=p['is_reversed'] ^ edge['is_reversed']
                along_chain=canonical[::-1] if edge['is_reversed'] else canonical
                self.assertEqual(g,along_chain[::-1] if original_reversed else along_chain)
                attrs=m.normalize_attributes(int(p['source_id'].split('-')[1]))
                self.assertEqual({k:p[k] for k in attrs},attrs)
            self.assertGreater(m.normalize_attributes(0)['grade'],2**53)

    def test_composition_identity_binds_both_unmodified_loaded_functions(self):
        with tempfile.TemporaryDirectory() as temp:
            path=Path(temp)/'geometry.py'; source='def normalize_line_network(frame, field):\n    return frame\ndef merge_line_chains(frame, field, match):\n    return frame\n'
            path.write_text(source); namespace={}; exec(compile(source,str(path),'exec'),namespace)
            module=SimpleNamespace(__file__=str(path),**{k:v for k,v in namespace.items() if not k.startswith('__')})
            expected={**m.implementation_identity(source,'normalize-chains'),'loaded_normalize_matches_source':True,'loaded_merge_matches_source':True}
            self.assertEqual(m.loaded_implementation_identity(module,'normalize-chains'),expected)
            for name in ('normalize_line_network','merge_line_chains'):
                original=getattr(module,name); setattr(module,name,lambda *a:None)
                with self.assertRaises(AssertionError):m.loaded_implementation_identity(module,'normalize-chains')
                setattr(module,name,original)

    def test_composition_report_rejects_missing_ports_identity_types_counts_and_budget(self):
        import copy
        case=m.Case('compose-open',8,'normalize-chains'); counts=m.composition_counts(case)
        report={'case':m.asdict(case),'observer':'A','measurement_id':case.key,'result':'passed','stage':'complete',
            'duration_seconds':1.,'worker_peak_rss_bytes':1,'source_frame_sha256':'f'*64,
            'timings':dict.fromkeys(m.composition_timings(),0.),
            'implementation_identity':{'loaded_normalize_matches_source':True,'loaded_merge_matches_source':True,'module_sha256':'a'*64,'normalize_line_network_sha256':'b'*64,'merge_line_chains_sha256':'c'*64},
            'physical':{p:{'rows':counts[p],'crs':'EPSG:3857','full_result_sha256':'c'*64,'all_coordinates_attributes_membership_verified':True} for p in m.COMPOSITION_PORTS},
            'source':{'size_bytes':1,'sha256':'d'*64},'outputs':{p:{'size_bytes':1,'sha256':'d'*64} for p in m.COMPOSITION_PORTS},
            'relations':{'unique_edges':8,'original_occurrences':8,'exact_membership_and_two_level_directions_verified':True}}
        m.validate_worker(report,case,'A')
        changes=[(('duration_seconds',),181),(('duration_seconds',),float('nan')),(('observer',),'C'),(('stage',),'merge'),
                 (('implementation_identity','loaded_merge_matches_source'),False),(('implementation_identity','loaded_normalize_matches_source'),False),
                 (('relations','unique_edges'),7),(('relations','exact_membership_and_two_level_directions_verified'),False)]
        for p in m.COMPOSITION_PORTS:
            changes.extend([(('outputs',p,'size_bytes'),0),(('outputs',p,'size_bytes'),True),(('physical',p,'rows'),0),
                            (('physical',p,'crs'),'EPSG:4326'),(('physical',p,'all_coordinates_attributes_membership_verified'),False)])
            for field in ('physical','outputs'):
                changed=copy.deepcopy(report); del changed[field][p]
                with self.assertRaises(AssertionError):m.validate_worker(changed,case,'A')
        for path,value in changes:
            changed=copy.deepcopy(report); target=changed
            for key in path[:-1]:target=target[key]
            target[path[-1]]=value
            with self.subTest(path=path),self.assertRaises(AssertionError):m.validate_worker(changed,case,'A')


if __name__=='__main__':unittest.main()
