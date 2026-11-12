from pathlib import Path
import importlib.util
import itertools
import json
import os
import subprocess
import tempfile
import unittest
import shlex
import shutil
from datetime import datetime, timezone
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
class ManagerTraceProducerTests(unittest.TestCase):
 @classmethod
 def setUpClass(cls):
  cls.renderer=module('scripts/infra/generate-trace-config.py')
  cls.workspace=tempfile.TemporaryDirectory(prefix='addp-manager-trace-tests-')
  cls.base=Path(cls.workspace.name)/'original'
  with patch.dict(os.environ,{'ENV':'development'}):
   cls.renderer.prepare_development(cls.base,['gateway:ingress','system:backend','monitor:backend','agent:backend','copilot:backend'],14318,14320)
 @classmethod
 def tearDownClass(cls): cls.workspace.cleanup()
 def setUp(self):
  self.temp=tempfile.TemporaryDirectory(prefix='addp-manager-trace-append-');self.addCleanup(self.temp.cleanup)
  self.root=Path(self.temp.name)/'traces';shutil.copytree(self.base,self.root)
  # Relocate only this test's prepared deployment paths.
  f=self.root/'deployment.env';f.write_text(f.read_text().replace(str(self.base),str(self.root)))
  self.original=f.read_bytes();self.target=self.root/'sources/manager-backend'
 def append(self):
  with patch.dict(os.environ,{'ENV':'development','OPENSSL_CONF':'/nonexistent/global-openssl-config'}):
   self.renderer.prepare_development_producer(self.root,'manager:backend')
 def unchanged(self, expected=None):
  self.assertEqual((self.root/'deployment.env').read_bytes(),self.original if expected is None else expected)
  self.assertFalse(self.target.exists());self.assertEqual(list(self.root.glob('.producer-*')),[])
 def test_append_keeps_existing_material_and_only_updates_source_array(self):
  snapshot={p.relative_to(self.root):(p.read_bytes(),p.stat().st_mode&0o777) for p in self.root.rglob('*') if p.is_file()}
  before=dict(os.environ);calls=[];run=subprocess.run
  def observe(args,**kwargs):
   self.assertEqual(args[0],'openssl');self.assertLessEqual(kwargs['timeout'],10)
   config=Path(kwargs['env']['OPENSSL_CONF']);self.assertTrue(config.is_relative_to(self.root));self.assertEqual(config.stat().st_mode&0o777,0o600)
   self.assertNotIn('-CAcreateserial',args);calls.append(args)
   return run(args,**kwargs)
  with patch.object(self.renderer.subprocess,'run',side_effect=observe):self.append()
  self.assertEqual(before,os.environ);self.assertEqual(len(calls),4)
  for rel,(body,mode) in snapshot.items():
   if str(rel)!='deployment.env':self.assertEqual((self.root/rel).read_bytes(),body)
   self.assertEqual((self.root/rel).stat().st_mode&0o777,mode)
  old=self.original.decode().splitlines();new=(self.root/'deployment.env').read_text().splitlines()
  self.assertEqual([v for v in old if not v.startswith('ADDP_TRACE_DEPLOYMENTS=')],[v for v in new if not v.startswith('ADDP_TRACE_DEPLOYMENTS=')])
  declaration=json.loads(shlex.split(next(v for v in new if v.startswith('ADDP_TRACE_DEPLOYMENTS=')))[0].split('=',1)[1])
  self.assertEqual(declaration[-1],{'module_name':'manager','role':'backend','endpoint':'https://127.0.0.1:14318/v1/traces','tls_dir':str(self.target)})
  self.assertEqual(len(declaration),6);self.assertEqual(self.target.stat().st_mode&0o777,0o700)
  self.assertEqual({p.name for p in self.target.iterdir()},{'ca.crt','client.crt','client.key'})
  for p in self.target.iterdir():self.assertEqual(p.stat().st_mode&0o777,0o600)
  env=os.environ|{'OPENSSL_CONF':str(self.root/'authorities/openssl.cnf')}
  for ca,purpose,ok in [('source','sslclient',True),('query','sslclient',False),('source','sslserver',False)]:
   r=run(['openssl','verify','-CAfile',str(self.root/f'center/{ca}-ca.crt'),'-purpose',purpose,str(self.target/'client.crt')],env=env,capture_output=True,timeout=5)
   self.assertEqual(r.returncode==0,ok)
  def expiry(path):
   out=run(['openssl','x509','-in',str(path),'-noout','-enddate'],env=env,capture_output=True,check=True,timeout=5).stdout.decode().strip()
   return datetime.strptime(out,'notAfter=%b %d %H:%M:%S %Y GMT').replace(tzinfo=timezone.utc)
  self.assertLessEqual(expiry(self.target/'client.crt'),expiry(self.root/'authorities/source.crt'))
  self.assertEqual(list(self.root.glob('.producer-*')),[])
  with self.assertRaises(ValueError):self.append()
 def test_rejects_unapproved_producer_and_production(self):
  for env,producer in [('development','meta:backend'),('development','manager:worker'),('production','manager:backend')]:
   with patch.dict(os.environ,{'ENV':env}),patch.object(self.renderer.subprocess,'run') as run,self.assertRaises(ValueError):
    self.renderer.prepare_development_producer(self.root,producer)
   run.assert_not_called();self.unchanged()
 def test_rejects_existing_destination_without_removing_it(self):
  self.target.mkdir();marker=self.target/'preserve';marker.write_text('existing')
  with self.assertRaises(ValueError):self.append()
  self.assertEqual(marker.read_text(),'existing');self.assertEqual((self.root/'deployment.env').read_bytes(),self.original)
 def test_rejects_invalid_or_escaped_preparation_before_signing(self):
  for rel in ['authorities/source.key','deployment.env','sources/system-backend']:
   p=self.root/rel;mode=p.stat().st_mode&0o777;p.chmod(0o777)
   with patch.object(self.renderer.subprocess,'run') as run,self.assertRaises(ValueError):self.append()
   run.assert_not_called();p.chmod(mode);self.unchanged()
  p=self.root/'authorities/source.key';saved=p.read_bytes();p.unlink();p.symlink_to(self.base/'authorities/source.key')
  with self.assertRaises(ValueError):self.append()
  p.unlink();p.write_bytes(saved);p.chmod(0o600);self.unchanged()
  ca=self.root/'center/source-ca.crt';ca.write_bytes((self.root/'center/query-ca.crt').read_bytes())
  with self.assertRaises(ValueError):self.append()
  self.unchanged()
 def test_rejects_duplicate_declaration_and_manifest_fields(self):
  values=dict(shlex.split(line)[0].split('=',1) for line in self.original.decode().splitlines())
  declarations=json.loads(values['ADDP_TRACE_DEPLOYMENTS'])
  invalid=[declarations+[declarations[0]],declarations+[{'module_name':'manager','role':'backend','endpoint':'https://127.0.0.1:14318/v1/traces','tls_dir':str(self.target)}],declarations+[declarations[0]|{'module_name':'../escape'}]]
  for rows in invalid:
   f=self.root/'deployment.env';text=self.original.decode().replace(shlex.quote(values['ADDP_TRACE_DEPLOYMENTS']),shlex.quote(json.dumps(rows)))
   f.write_text(text)
   with self.assertRaises(ValueError):self.append()
   self.unchanged(text.encode())
  text=self.original+b'TRACE_OTLP_PORT=14318\n';(self.root/'deployment.env').write_bytes(text)
  with self.assertRaises(ValueError):self.append()
  self.unchanged(text)
 def test_sign_verify_timeout_and_publish_failures_leave_original_inputs(self):
  run=subprocess.run
  for fail_at in range(1,5):
   count=0
   def fail(args,**kwargs):
    nonlocal count
    count+=1
    if count==fail_at:raise subprocess.TimeoutExpired(['openssl'],10)
    return run(args,**kwargs)
   with patch.object(self.renderer.subprocess,'run',side_effect=fail),self.assertRaises(subprocess.TimeoutExpired):self.append()
   self.unchanged()
  with patch.object(self.renderer.os,'replace',side_effect=OSError('publish unavailable')),self.assertRaises(OSError):self.append()
  self.unchanged()
 def test_expired_ca_and_total_deadline_do_not_publish(self):
  expired=subprocess.CompletedProcess(['openssl'],0,b'notAfter=Nov  1 00:00:00 2026 GMT\n')
  with patch.object(self.renderer.subprocess,'run',return_value=expired),self.assertRaises(ValueError):self.append()
  self.unchanged()
  with patch.object(self.renderer.time,'monotonic',side_effect=[0,61]),self.assertRaises(TimeoutError):self.append()
  self.unchanged()
 def test_mismatched_signing_key_is_rejected_and_rolled_back(self):
  (self.root/'authorities/source.key').write_bytes((self.root/'authorities/query.key').read_bytes())
  with self.assertRaises(subprocess.CalledProcessError):self.append()
  self.unchanged()
 def test_cli_errors_are_closed_and_success_does_not_activate(self):
  args=[os.sys.executable,str(ROOT/'scripts/infra/generate-trace-config.py'),'prepare-development-producer','--directory',str(self.root),'--producer','manager:backend']
  env=os.environ|{'ENV':'development','OPENSSL_CONF':'/nonexistent/global-config'}
  r=subprocess.run(args,env=env,capture_output=True,timeout=60)
  self.assertEqual(r.returncode,0,r.stderr.decode());self.assertIn(b'no environment or service was activated',r.stdout)
  r=subprocess.run(args,env=env,capture_output=True,timeout=10)
  self.assertNotEqual(r.returncode,0);self.assertEqual(r.stderr.strip(),b'trace_deployment_invalid')
  self.assertEqual(r.stdout,b'')

if __name__=='__main__':unittest.main()
