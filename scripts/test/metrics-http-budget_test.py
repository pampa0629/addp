"""Pure functional-budget contract. Never starts Docker or a workload."""
import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch

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

    def test_existing_standard_case_is_wrapped_and_registered(self):
        root=Path(__file__).resolve().parents[2]
        self.assertIn('metrics-http-budget.py bash scripts/test/monitor-metrics-gate.sh $(MONITOR_METRICS_TEST_ARGS)',(root/'Makefile').read_text())
        gate=(root/'scripts/test/monitor-metrics-gate.sh').read_text()
        for name in ('python-http-agent','python-http-copilot','common-python/','metrics-http-budget.py'):
            self.assertIn(name,gate)
        probe=(root/'scripts/test/monitor-metrics-probe.py').read_text()
        self.assertIn("python-http-metrics-probe.py",probe)
