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
   self.assertIn('addp.trace.policy/v2; definition version 2',text)
   self.assertIn('instrumentation_scope.name == \"addp.execution\"',text.replace('\\\"','\"'))
   self.assertIn('attributes[\"addp.execution.outcome\"] == \"failed\"',text.replace('\\\"','\"'))
   self.assertIn('attributes[\"addp.execution.outcome\"] == \"timeout\"',text.replace('\\\"','\"'))
   self.assertNotIn('== \"cancelled\"',text.replace('\\\"','\"'))
   self.assertNotIn('== \"rejected\"',text.replace('\\\"','\"'))
   self.assertIn('instrumentation_scope.name == \"addp.http\" and kind == SPAN_KIND_SERVER',text.replace('\\\"','\"'))
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
 def test_platform_projection_includes_only_explicit_agent_and_copilot_backends(self):
  platform=module('scripts/prod/generate-trace-platform.py')
  with tempfile.TemporaryDirectory(prefix='addp-trace-python-config-') as directory:
   p=Path(directory);p.chmod(0o700)
   tls=p/'source';tls.mkdir(mode=0o700)
   for name in ['ca.crt','client.crt','client.key']:(tls/name).write_text(name)
   (tls/'client.key').chmod(0o600)
   def declaration(name,role='backend'):
    return {'module_name':name,'role':role,'endpoint':'https://alloy:4318/v1/traces','tls_dir':str(tls)}
   deployment=[declaration('agent'),declaration('copilot'),declaration('monitor')]
   def render(values):
    with patch.dict(os.environ,{'ADDP_TRACE_DEPLOYMENT_DIR':str(p),'ADDP_TRACE_DEPLOYMENTS':json.dumps(values),'MONITOR_TEMPO_URL':''}):platform.render()
   render(deployment)
   output=p/'platform-traces.json'
   projected=json.loads(output.read_text())
   self.assertEqual(set(projected['services']),{'agent-backend','copilot-backend','monitor-backend'})
   self.assertEqual(output.stat().st_mode&0o777,0o600)
   for original in deployment:
    row=projected['services'][original['module_name']+'-backend']
    self.assertEqual(row['environment']['ADDP_OBSERVABILITY_TRACES_ENABLED'],'true')
    self.assertEqual(json.loads(row['environment']['ADDP_TRACE_DEPLOYMENTS']),[original|{'tls_dir':'/etc/addp/traces/source'}])
    self.assertEqual(row['volumes'],[{'type':'bind','source':str(tls),'target':'/etc/addp/traces/source','read_only':True,'bind':{'create_host_path':False}}])
   unchanged=output.read_bytes()
   rejected=[
    [declaration('agent','worker')],
    [declaration('copilot','ingress')],
    [declaration('geopython-workflow')],
    [declaration('not-a-platform-module')],
    [declaration('agent'),declaration('agent')],
    [declaration('agent')|{'extra':'not-a-four-field-declaration'}],
    [declaration('copilot')|{'endpoint':'http://alloy:4318/v1/traces'}],
    [declaration('copilot')|{'endpoint':'https://alloy:4318/v1/traces?private=1'}],
    [declaration('agent')|{'endpoint':'https://user:password@alloy:4318/v1/traces'}],
   ]
   for invalid in rejected:
    with self.subTest(deployment=invalid):
     with self.assertRaises(ValueError):render(invalid)
     self.assertEqual(output.read_bytes(),unchanged)
   (tls/'client.key').chmod(0o644)
   with self.assertRaises(ValueError):render([declaration('agent')])
   self.assertEqual(output.read_bytes(),unchanged)
 def test_unselected_preflight_ignores_stale_private_inputs(self):
  env=os.environ|{'ADDP_OBSERVABILITY_TRACES_ENABLED':'false','ADDP_TRACE_DEPLOYMENT_DIR':'/nonexistent','TEMPO_S3_SECRET_KEY':'invalid'}
  subprocess.run(['bash','-eu','-c','PROJECT_ROOT="$PWD"; source scripts/utils/observability-env.sh; addp_traces_preflight'],cwd=ROOT,env=env,check=True)
 def test_development_preparation_uses_real_private_certificates_without_activation(self):
  renderer=module('scripts/infra/generate-trace-config.py')
  with tempfile.TemporaryDirectory(prefix='addp-trace-preparation-') as directory:
   root=Path(directory);destination=root/'traces'
   protected=root/'personal.env';protected.write_text('UNRELATED=preserved\n')
   previous=dict(os.environ);calls=[];run=subprocess.run
   def observe(args,**kwargs):
    self.assertEqual(args[0],'openssl');calls.append(args)
    self.assertEqual(kwargs['env']['OPENSSL_CONF'],str(destination/'authorities/openssl.cnf'))
    self.assertLessEqual(kwargs['timeout'],10)
    if args[1]=='req':self.assertEqual(args[2:4],['-config',str(destination/'authorities/openssl.cnf')])
    return run(args,**kwargs)
   producers=['gateway:ingress','system:backend','monitor:backend','agent:backend','copilot:backend']
   with patch.dict(os.environ,{'ENV':'development','OPENSSL_CONF':'/nonexistent/private-global-config'}),patch.object(renderer.subprocess,'run',side_effect=observe):
    renderer.prepare_development(destination,producers,14318,14320)
   self.assertEqual(os.environ,previous)
   self.assertEqual(len(calls),17)  # Three roots and seven leaves (CSR + signing).
   self.assertEqual(protected.read_text(),'UNRELATED=preserved\n')
   for p in destination.rglob('*'):
    self.assertEqual(p.stat().st_mode&0o777,0o700 if p.is_dir() else 0o644 if p.name=='tempo.yml' else 0o600)
   env=run(['bash','-eu','-c','set -a; source "$1"; env -0','trace',str(destination/'deployment.env')],capture_output=True,check=True).stdout
   values=dict(v.decode().split('=',1) for v in env.split(b'\0') if v)
   deployments=json.loads(values['ADDP_TRACE_DEPLOYMENTS'])
   self.assertEqual([(d['module_name'],d['role']) for d in deployments],[tuple(v.split(':')) for v in producers])
   self.assertEqual(values['MONITOR_TEMPO_URL'],'https://127.0.0.1:14320')
   self.assertNotIn('TEMPO_S3_SECRET_KEY=',(destination/'deployment.env').read_text())
   def verify(ca,cert,purpose,expected=True):
    result=run(['openssl','verify','-CAfile',str(ca),'-purpose',purpose,str(cert)],capture_output=True,
     env=os.environ|{'OPENSSL_CONF':str(destination/'authorities/openssl.cnf')},timeout=5)
    self.assertEqual(result.returncode==0,expected,result.stderr.decode())
   ca=destination/'authorities';center=destination/'center'
   verify(ca/'server.crt',center/'server.crt','sslserver')
   for d in deployments:
    tls=Path(d['tls_dir']);self.assertEqual(set(p.name for p in tls.iterdir()),{'ca.crt','client.crt','client.key'})
    verify(center/'source-ca.crt',tls/'client.crt','sslclient')
    verify(center/'query-ca.crt',tls/'client.crt','sslclient',False)
   verify(center/'query-ca.crt',destination/'query/client.crt','sslclient')
   verify(center/'source-ca.crt',destination/'query/client.crt','sslclient',False)
   for days,expected in [(29,0),(31,1)]:
    result=run(['openssl','x509','-in',str(center/'server.crt'),'-checkend',str(days*86400),'-noout'],capture_output=True,
     env=os.environ|{'OPENSSL_CONF':str(ca/'openssl.cnf')},timeout=5)
    self.assertEqual(result.returncode,expected)
   unchanged=(destination/'deployment.env').read_bytes()
   with self.assertRaises(ValueError):renderer.prepare_development(destination,producers,14318,14320)
   self.assertEqual((destination/'deployment.env').read_bytes(),unchanged)
 def test_development_preparation_rejects_invalid_inputs_before_writing(self):
  renderer=module('scripts/infra/generate-trace-config.py')
  with tempfile.TemporaryDirectory() as directory:
   p=Path(directory)/'new'
   for producers,otlp,query in [([],1,2),(['monitor:worker'],1,2),(['monitor:backend']*2,1,2),(['monitor:backend'],1,1),(['monitor:backend'],0,2)]:
    with self.subTest(producers=producers,otlp=otlp),self.assertRaises(ValueError):renderer.prepare_development(p,producers,otlp,query)
    self.assertFalse(p.exists())
   with patch.dict(os.environ,{'ENV':'production'}),self.assertRaises(ValueError):renderer.prepare_development(p,['monitor:backend'],1,2)
   with self.assertRaises(ValueError):renderer.prepare_development(ROOT/'not-a-trace-fixture',['monitor:backend'],1,2)
 def test_development_preparation_failure_cleans_only_its_own_new_directory(self):
  renderer=module('scripts/infra/generate-trace-config.py')
  with tempfile.TemporaryDirectory() as directory:
   p=Path(directory)/'new';sibling=Path(directory)/'preserved';sibling.write_text('existing')
   for error in (subprocess.CalledProcessError(1,['openssl']),subprocess.TimeoutExpired(['openssl'],10)):
    with patch.dict(os.environ,{'ENV':'development'}),patch.object(renderer.subprocess,'run',side_effect=error),self.assertRaises(type(error)):
     renderer.prepare_development(p,['monitor:backend'],1,2)
    self.assertFalse(p.exists());self.assertEqual(sibling.read_text(),'existing')
   with patch.dict(os.environ,{'ENV':'development'}),patch.object(renderer.time,'monotonic',side_effect=[0,61]),self.assertRaises(TimeoutError):
    renderer.prepare_development(p,['monitor:backend'],1,2)
   self.assertFalse(p.exists());self.assertEqual(sibling.read_text(),'existing')
 def test_explicit_cli_prepares_inputs_and_default_cli_remains_the_sole_renderer(self):
  with tempfile.TemporaryDirectory(prefix='addp-trace-cli-') as directory:
   p=Path(directory)/'new';env=os.environ|{'ENV':'development','OPENSSL_CONF':'/nonexistent/global-config'}
   script=str(ROOT/'scripts/infra/generate-trace-config.py')
   result=subprocess.run([os.sys.executable,script,'prepare-development','--directory',str(p),
    '--otlp-port','14318','--query-port','14320','--producer','monitor:backend'],env=env,capture_output=True,timeout=60)
   self.assertEqual(result.returncode,0,result.stderr.decode());self.assertTrue((p/'deployment.env').is_file())
   self.assertIn(b'no environment or service was activated',result.stdout)
   rendered=p/'center/runtime.alloy';before=rendered.read_bytes()
   result=subprocess.run([os.sys.executable,script],env=env|{'ADDP_TRACE_DEPLOYMENT_DIR':str(p/'center')},capture_output=True,timeout=10)
   self.assertEqual(result.returncode,0,result.stderr.decode());self.assertEqual(rendered.read_bytes(),before)
if __name__=='__main__':unittest.main()
