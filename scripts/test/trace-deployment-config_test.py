from pathlib import Path
import importlib.util
import itertools
import json
import os
import subprocess
import tempfile
import unittest
from unittest.mock import patch
ROOT=Path(__file__).resolve().parents[2]
def module(path):
 spec=importlib.util.spec_from_file_location('trace_config_'+Path(path).stem,ROOT/path)
 m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m);return m
class TraceConfigTests(unittest.TestCase):
 def test_explicit_profiles_and_scopes_do_not_activate_disabled_capabilities(self):
  for logs,metrics,traces in itertools.product(['false','true'],repeat=3):
   env=os.environ|{'ADDP_OBSERVABILITY_LOGS_ENABLED':logs,'ADDP_OBSERVABILITY_METRICS_ENABLED':metrics,'ADDP_OBSERVABILITY_TRACES_ENABLED':traces,'COMPOSE_PROFILES':'untrusted'}
   for scope in ['all','core','logs','metrics','traces']:
    output=subprocess.check_output(['bash','-eu','-c','source scripts/infra/ports.sh; addp_observability_profiles; printf "%s\\n" "${COMPOSE_PROFILES:-}"; addp_infra_port_specs'],cwd=ROOT,env=env|{'ADDP_INFRA_PORT_SCOPE':scope},text=True)
    profile,*ports=output.splitlines()
    self.assertEqual('observability-traces' in profile,traces=='true')
    self.assertEqual(any('TRACE_OTLP_PORT' in row for row in ports),traces=='true' and scope in ['all','traces'])
    self.assertEqual(any(row.startswith('postgres ') for row in ports),scope in ['all','core'])
    self.assertNotIn('untrusted',profile)
 def test_one_policy_renders_shared_logs_and_traces_and_private_query_projection(self):
  renderer=module('scripts/infra/generate-trace-config.py')
  platform=module('scripts/prod/generate-trace-platform.py')
  with tempfile.TemporaryDirectory(prefix='addp-trace-config-') as directory:
   p=Path(directory);p.chmod(0o700)
   for name in renderer.FILES:(p/name).write_text('synthetic certificate fixture')
   (p/'server.key').chmod(0o600)
   renderer.render(p,False);text=(p/'runtime.alloy').read_text()
   self.assertNotIn('loki.write',text);self.assertIn('decision_wait = "120s"',text);self.assertIn('num_traces = 10000',text);self.assertIn('sampling_percentage = 5',text);self.assertIn('max_total_spans_per_second = 100',text);self.assertIn('end_time_unix_nano - start_time_unix_nano >= 1000000000',text)
   renderer.render(p,True);self.assertEqual((p/'runtime.alloy').read_text().count('loki.source.file "runtime"'),1)
   self.assertIn('block_retention: "72h"',(p/'tempo.yml').read_text());self.assertIn('ingestion_time_range_slack: "300s"',(p/'tempo.yml').read_text());self.assertEqual((p/'tempo.yml').stat().st_mode&0o777,0o644)
   tls=p/'source';tls.mkdir(mode=0o700)
   for name in ['ca.crt','client.crt','client.key']:(tls/name).write_text(name)
   (tls/'client.key').chmod(0o600)
   deployment=[{'module_name':'gateway','role':'ingress','endpoint':'https://alloy:4318/v1/traces','tls_dir':str(tls)}]
   with patch.dict(os.environ,{'ADDP_TRACE_DEPLOYMENT_DIR':str(p),'ADDP_TRACE_DEPLOYMENTS':json.dumps(deployment),'MONITOR_TEMPO_URL':''}):platform.render()
   projected=json.loads((p/'platform-traces.json').read_text());self.assertEqual(set(projected['services']),{'gateway'})
   self.assertEqual(json.loads(projected['services']['gateway']['environment']['ADDP_TRACE_DEPLOYMENTS'])[0]['tls_dir'],'/etc/addp/traces/source')
   p.chmod(0o755)
   with self.assertRaises(ValueError):renderer.render(p,True)
 def test_unselected_preflight_ignores_stale_private_inputs(self):
  env=os.environ|{'ADDP_OBSERVABILITY_TRACES_ENABLED':'false','ADDP_TRACE_DEPLOYMENT_DIR':'/nonexistent','TEMPO_S3_SECRET_KEY':'invalid'}
  subprocess.run(['bash','-eu','-c','PROJECT_ROOT="$PWD"; source scripts/utils/observability-env.sh; addp_traces_preflight'],cwd=ROOT,env=env,check=True)
if __name__=='__main__':unittest.main()
