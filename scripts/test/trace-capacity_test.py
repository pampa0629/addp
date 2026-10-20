from pathlib import Path
import copy
import importlib.util
import unittest
import unittest.mock
import os
import subprocess
import sys
import tempfile

spec=importlib.util.spec_from_file_location('trace_capacity',Path(__file__).with_name('trace-capacity.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)

class CapacityAcceptanceTests(unittest.TestCase):
 def phases(self):
  return [dict(retained_spans=n, active_block_traces=n//5,
    physical_trace_bytes=n*250, load=dict(duration_seconds=601), verified=dict(queried_complete_traces=20,query_seconds=2.5),
    stage_retained_spans_per_second=rate, workspace_peak_bytes=100000000)
    for n,rate in [(20000,20000/601),(80000,60000/601)]]
 def test_budget_rate_and_two_full_physical_copies_with_safety_margin(self):
  r=m.forecast(self.phases())
  self.assertEqual(r['budget_rate'],100)
  self.assertEqual(r['projected_72h_bytes'],250*100*259200*2)
  self.assertTrue(r['capacity_pass'])
  self.assertFalse(r['actual_72h_retention_verified'])
 def test_increased_span_size_fails_even_when_less_than_raw_bucket_limit(self):
  phases=self.phases()
  for p in phases:p['physical_trace_bytes']=p['retained_spans']*350
  r=m.forecast(phases)
  self.assertGreater(r['projected_72h_bytes'],16*m.GIB)
  self.assertLess(r['projected_72h_bytes'],20*m.GIB)
  self.assertFalse(r['capacity_pass'])
 def test_missing_phase_small_sample_or_unflushed_trace_cannot_pass(self):
  with self.assertRaises(ValueError):m.forecast(self.phases()[:1])
  for field,value in [('active_block_traces',0),('retained_spans',9000),('physical_trace_bytes',0)]:
   phases=copy.deepcopy(self.phases());phases[0][field]=value
   with self.assertRaises(ValueError):m.forecast(phases)
 def test_query_timeout_cannot_be_reported_as_capacity_success(self):
  phases=self.phases();phases[0]['verified']={}
  with self.assertRaises(ValueError):m.forecast(phases)
  phases=self.phases();phases[0]['verified']['query_seconds']=5.1
  with self.assertRaises(ValueError):m.forecast(phases)
 def test_workspace_and_actual_rate_cannot_hide_behind_nominal_budget(self):
  phases=self.phases();phases[1]['workspace_peak_bytes']=6*m.GIB
  self.assertFalse(m.forecast(phases)['capacity_pass'])
  phases=self.phases();phases[1]['stage_retained_spans_per_second']=125
  self.assertEqual(m.forecast(phases)['budget_rate'],125)
 def test_resource_or_volume_stop_line_aborts_before_further_queries(self):
  for condition in ['oom','cpu','memory','unbounded','volume']:
   c=m.Capacity.__new__(m.Capacity)
   state=dict(Config={'Labels':{'com.docker.compose.service':'test-owned'}},
     State=dict(OOMKilled=condition=='oom',Status='running',ExitCode=0,Running=True),
     HostConfig=dict(NanoCpus=7000000000 if condition=='cpu' else 1000000000,
       Memory=0 if condition=='unbounded' else 7*m.GIB if condition=='memory' else m.GIB))
   c.run=lambda *args,**kwargs: 'owned-id' if args[1]=='ps' else m.json.dumps([state])
   c.minio=lambda command: '\n'.join(str(size)+' '+path for path,size in [
      ('/data',2*m.GIB//1024),('/capacity-tempo',0),('/capacity-loki',0),
      ('/capacity-alloy',0),('/data/addp-runtime-traces-test',0),('/data/.minio.sys',0)])
   with unittest.mock.patch.dict(m.os.environ,{'TRACE_T2_PROJECT':'owned-fixture'}):
    with self.subTest(condition=condition),self.assertRaises(RuntimeError):c.sample()
 def test_interrupted_gate_removes_only_owned_oneoff_before_compose_down(self):
  for removal_ok in [True,False]:
   with self.subTest(removal_ok=removal_ok),tempfile.TemporaryDirectory(prefix='trace-cleanup-contract-') as directory:
    w=Path(directory);binpath=w/'bin';binpath.mkdir();marker=w/'owned-load';marker.touch();log=w/'docker.jsonl'
    wrapper=binpath/'python3'
    wrapper.write_text('#!'+sys.executable+'\n'+"""import os,sys
from pathlib import Path
if len(sys.argv)>1 and Path(sys.argv[1]).name=='trace-deployment-fixture.py':sys.exit(79)
os.execv(sys.executable,[sys.executable,*sys.argv[1:]])
""")
    docker=binpath/'docker'
    docker.write_text('#!'+sys.executable+'\n'+"""import json,os,sys
from pathlib import Path
a=sys.argv[1:]
with open(os.environ['TEST_DOCKER_LOG'],'a') as f:f.write(json.dumps(a)+'\\n')
p=Path(os.environ['TEST_OWNED_LOAD'])
if a and a[0]=='ps' and p.exists():print('synthetic-owned-load')
if a[:2]==['container','rm']:
 if os.environ['TEST_REMOVE_OK']=='1':p.unlink()
 else:sys.exit(1)
""")
    wrapper.chmod(0o755);docker.chmod(0o755)
    env=os.environ|dict(PATH=str(binpath)+os.pathsep+os.environ['PATH'],TEST_DOCKER_LOG=str(log),TEST_OWNED_LOAD=str(marker),TEST_REMOVE_OK='1' if removal_ok else '0')
    result=subprocess.run(['bash',str(m.ROOT/'scripts/test/monitor-traces-gate.sh'),'--capacity'],cwd=m.ROOT,env=env,capture_output=True,text=True,timeout=30)
    self.assertEqual(result.returncode,79 if removal_ok else 1,result.stdout+result.stderr)
    calls=[m.json.loads(row) for row in log.read_text().splitlines()]
    owned=next(a for a in calls if 'label=com.docker.compose.service=trace-load' in a)
    self.assertTrue(any(a.startswith('label=com.docker.compose.project=addp-traces-t2-') for a in owned))
    remove=next(i for i,a in enumerate(calls) if a[:2]==['container','rm'])
    down=next(i for i,a in enumerate(calls) if 'down' in a)
    self.assertLess(remove,down)
    self.assertEqual(calls[remove],['container','rm','-f','-v','synthetic-owned-load'])
    self.assertEqual(marker.exists(),not removal_ok)
 def test_metric_labels_and_missing_required_counter_are_not_silent_zero(self):
  v=m.metrics('tempo_request_duration_seconds_bucket{route="compactor loop",le="1"} 4\notelcol_exporter_sent_spans_total{exporter="tempo"} 150\notelcol_processor_tail_sampling_count_spans_sampled{sampled="false"} 900\notelcol_processor_tail_sampling_count_spans_sampled{sampled="true"} 150\n')
  self.assertEqual(m.total(v,'tempo_request_duration_seconds_bucket'),4)
  self.assertEqual(m.total(v,'otelcol_exporter_sent_spans',required=True),150)
  self.assertEqual(m.total(v,'otelcol_processor_tail_sampling_count_spans_sampled',{'sampled':'true'}),150)
  with self.assertRaises(ValueError):m.total(v,'tempo_distributor_spans_received_total',required=True)

if __name__=='__main__':unittest.main()
