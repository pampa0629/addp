import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')

class HostedApprovedHostedGateTest(base.HostedContourGateTest):
    prefix='focal-percentiles'
    family='raster'
    runtime_family='raster'

    def setUp(self):
        super().setUp()
        fixture=self.host.repository/'business/scripts/online-raster-focal-percentiles-fixture.py'
        fixture.write_text(fixture.read_text().replace("root/'focal-percentiles-","root/'raster-focal-percentiles-"))
        make=self.host.bin/'make';make.write_text(make.read_text().replace('ADDP_ONLINE_FOCAL_PERCENTILES_','ADDP_ONLINE_RASTER_FOCAL_PERCENTILES_'))
        docker = self.host.bin / 'docker'
        log_run = 'echo "docker-run:$name" >> "$ADDP_TEST_GATE_TRACE"'
        docker.write_text(docker.read_text().replace(log_run, log_run + '''
              if [ "$name" = addp-raster-focal-percentiles-runtime ]; then
                echo "runtime-limits:$*" >> "$ADDP_TEST_GATE_TRACE"
              fi'''))
        self.host._executable('timeout', '''
            #!/usr/bin/env python3
            import os, sys, time
            assert sys.argv[1] == '--kill-after=5'
            start = float(os.environ['ADDP_ONLINE_FOCAL_PERCENTILES_STARTED'])
            assert 0 < float(sys.argv[2]) <= 600 and 0 < start <= time.monotonic()
            with open(os.environ['ADDP_TEST_GATE_TRACE'], 'a') as stream:
                stream.write(f'business-clock:{start}\\n')
            os.execvp(sys.argv[3], sys.argv[3:])
        ''')
        self.host._write_repository_script('scripts/utils/online_raster_lineage.py', """
            import os,sys
            assert sys.argv[1:]==['prepare']
            assert os.environ['POSTGRES_DB']=='addp_online'
            with open(os.environ['ADDP_TEST_GATE_TRACE'],'a') as f:f.write('lineage-owner:prepare\\n')
            if os.environ.get('ADDP_TEST_OBSERVER_FAIL')=='1':sys.exit(1)
        """)
        import subprocess
        subprocess.run(['git','add','.'],cwd=self.host.repository,check=True)
        subprocess.run(['git','commit','-qm','owned lineage observer fixture'],cwd=self.host.repository,check=True)

    def test_observer_prepare_failure_cleans_before_business(self):
        result=self.run_gate(ADDP_TEST_OBSERVER_FAIL='1')
        self.assertNotEqual(result.returncode,0)
        trace=self.host.trace.read_text();self.assertIn('lineage-owner:prepare',trace)
        self.assertNotIn('make:test-online',trace);self.assertFalse(self.host.secrets.exists())

    def test_runtime_resources_and_shared_business_clock(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        trace = self.host.trace.read_text().splitlines()
        limits, = [line for line in trace if line.startswith('runtime-limits:')]
        self.assertIn(' --cpus 4 --memory 2g --memory-swap 2g ', limits)
        clocks = [line for line in trace if line.startswith('business-clock:')]
        self.assertEqual(len(clocks), 5)
        self.assertEqual(len(set(clocks)), 1)
        self.assertLess(trace.index(clocks[0]), trace.index('focal-percentiles:seed'))

    def run_gate(self,check=False,**overrides):
        import subprocess
        try:return super().run_gate(check,**overrides)
        except subprocess.TimeoutExpired as error:
            trace=self.host.trace.read_text()if self.host.trace.exists()else'missing lifecycle trace'
            self.fail('Hosted simulation timed out at unchanged 60s limit: '+trace)
