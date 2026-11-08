"""Shared Redis/MinIO recovery proof; no real Docker, accounts or services."""
import base64
from contextlib import redirect_stderr
from contextlib import contextmanager
import copy
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import Mock, patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('metrics_source_recovery', ROOT/'scripts/utils/metrics-source.py')
M = importlib.util.module_from_spec(spec); spec.loader.exec_module(M)


class TransportCertificateTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.temporary.cleanup)
        cls.environment = patch.dict(os.environ, {})
        cls.environment.start()
        cls.addClassCleanup(cls.environment.stop)
        cls.root = Path(cls.temporary.name).resolve()
        spec = importlib.util.spec_from_file_location('transport_tls_fixture', ROOT/'scripts/test/metrics-source-fixture.py')
        fixture = importlib.util.module_from_spec(spec); spec.loader.exec_module(fixture)
        cls.tls = fixture.certificates(cls.root)
        cls.original = {name:(cls.tls/name).read_bytes() for name in ('ca.crt','server.crt','server.key')}

    def setUp(self):
        for name, data in self.original.items():
            (self.tls/name).write_bytes(data)

    def openssl(self, *args):
        return subprocess.run(['openssl', *args], check=True, capture_output=True, timeout=5)

    def sign(self, *, authority='keyid:always', purpose='serverAuth', san='IP:127.0.0.1', subject='hash'):
        extension = self.root/'transport.ext'
        extension.write_text('basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\n'
            +'authorityKeyIdentifier='+authority+'\nsubjectKeyIdentifier='+subject
            +'\nextendedKeyUsage='+purpose+'\nsubjectAltName='+san+'\n')
        self.openssl('x509','-req','-in',str(self.root/'server.csr'),'-CA',str(self.tls/'ca.crt'),
            '-CAkey',str(self.root/'ca.key'),'-set_serial','2','-days','1','-extfile',str(extension),'-out',str(self.tls/'server.crt'))

    def preflight(self, *, rejected=False, reason=None):
        before = {name:((self.tls/name).read_bytes(), (self.tls/name).stat()) for name in self.original}
        for kind in ('REDIS','MINIO'):
            prefix = 'ADDP_'+kind+'_METRICS'
            env = {prefix+'_LISTEN':'127.0.0.1:19443', prefix+'_TLS_DIR':str(self.tls)}
            with self.subTest(kind=kind):
                if rejected:
                    with self.assertRaisesRegex(ValueError, '^Metrics source TLS verification failed$') as failure:
                        M.transport(env, ROOT, prefix)
                    self.assertEqual(failure.exception.observation_stage, 'transport-check')
                    if reason:
                        self.assertEqual(getattr(failure.exception, 'source_tls_reason', None), reason)
                else:
                    ip, port, material = M.transport(env, ROOT, prefix)
                    self.assertEqual((ip,port), ('127.0.0.1',19443))
                    self.assertEqual(material, {name:data for name,(data,_) in before.items()})
        for name,(data,stat) in before.items():
            self.assertEqual((self.tls/name).read_bytes(), data)
            current = (self.tls/name).stat()
            self.assertEqual((current.st_ino,current.st_mtime_ns,current.st_mode,current.st_uid),
                (stat.st_ino,stat.st_mtime_ns,stat.st_mode,stat.st_uid))

    def ordinary_chain_passes(self):
        self.openssl('verify','-CAfile',str(self.tls/'ca.crt'),'-purpose','sslserver',
            '-verify_ip','127.0.0.1',str(self.tls/'server.crt'))

    def test_valid_strict_certificate_and_optional_leaf_ski(self):
        self.preflight()
        self.sign(subject='none')
        self.preflight()

    def test_missing_leaf_aki_rejected_despite_ordinary_chain_success(self):
        self.sign(authority='none')
        self.ordinary_chain_passes()
        self.preflight(rejected=True, reason='chain')

    def test_missing_ca_ski_rejected_despite_ordinary_chain_success(self):
        self.openssl('req','-x509','-key',str(self.root/'ca.key'),'-days','1','-subj','/CN=metrics-source-t2-ca',
            '-addext','basicConstraints=critical,CA:TRUE','-addext','keyUsage=critical,keyCertSign,cRLSign',
            '-addext','subjectKeyIdentifier=none','-out',str(self.tls/'ca.crt'))
        self.sign(authority='issuer:always')
        self.ordinary_chain_passes()
        self.preflight(rejected=True, reason='chain')

    def test_wrong_server_purpose_ip_and_key_remain_rejected(self):
        for variation in ('purpose','san','key'):
            with self.subTest(variation=variation):
                self.setUp()
                if variation=='purpose': self.sign(purpose='clientAuth')
                elif variation=='san': self.sign(san='IP:127.0.0.2')
                else: (self.tls/'server.key').write_bytes((self.root/'client.key').read_bytes())
                if variation=='key':
                    for kind in ('REDIS','MINIO'):
                        prefix='ADDP_'+kind+'_METRICS'
                        with self.assertRaisesRegex(ValueError, '^Metrics source certificate key mismatch$'):
                            M.transport({prefix+'_LISTEN':'127.0.0.1:19443',prefix+'_TLS_DIR':str(self.tls)},ROOT,prefix)
                else: self.preflight(rejected=True, reason='purpose' if variation=='purpose' else 'name')

    def test_key_mismatch_and_unavailable_tool_never_expose_raw_details(self):
        prefix='ADDP_REDIS_METRICS';env={prefix+'_LISTEN':'127.0.0.1:19443',prefix+'_TLS_DIR':str(self.tls)}
        (self.tls/'server.key').write_bytes((self.root/'client.key').read_bytes())
        with self.assertRaises(ValueError) as mismatch: M.transport(env,ROOT,prefix)
        self.assertEqual(getattr(mismatch.exception,'source_tls_reason',None),'key')
        self.setUp()
        for failure in (FileNotFoundError('/private/password'),subprocess.TimeoutExpired('secret-command',5,stderr=b'secret-password')):
            with self.subTest(failure=type(failure).__name__),patch.object(M.subprocess,'run',side_effect=failure):
                with self.assertRaises(ValueError) as result:M.transport(env,ROOT,prefix)
                self.assertEqual(getattr(result.exception,'source_tls_reason',None),'unknown')
                self.assertNotIn('private',str(result.exception));self.assertNotIn('secret',str(result.exception))

    def test_standard_check_reports_real_certificates_without_reading_personal_configuration(self):
        for variation,phrase in (('valid','部署输入检查通过'),('aki','证书链'),('purpose','服务器认证'),('san','IP SAN'),('key','私钥不匹配')):
            self.setUp()
            if variation=='aki': self.sign(authority='none')
            elif variation=='purpose': self.sign(purpose='clientAuth')
            elif variation=='san': self.sign(san='IP:127.0.0.2')
            elif variation=='key': (self.tls/'server.key').write_bytes((self.root/'client.key').read_bytes())
            before = {name:(self.tls/name).read_bytes() for name in self.original}
            for kind in ('redis','minio'):
                prefix='ADDP_'+kind.upper()+'_METRICS'
                env={'PATH':os.environ['PATH'],'OPENSSL_CONF':os.environ['OPENSSL_CONF'],prefix+'_ENABLED':'true',
                    prefix+'_LISTEN':'127.0.0.1:19443',prefix+'_TLS_DIR':str(self.tls),prefix+'_STATE_DIR':str(self.root/'state')}
                with self.subTest(variation=variation,kind=kind):
                    result=subprocess.run([sys.executable,str(ROOT/'scripts/infra/metrics-sources.py'),'check','--resolved-env',
                        '--scope','infra','--kind',kind],env=env,capture_output=True,text=True,timeout=10)
                    self.assertEqual(result.returncode,0 if variation=='valid' else 1)
                    self.assertIn(phrase,result.stdout)
                    self.assertNotIn(str(self.tls),result.stdout+result.stderr)
                    self.assertNotIn('BEGIN ',result.stdout+result.stderr)
                    self.assertFalse(result.stderr)
            self.assertEqual(before,{name:(self.tls/name).read_bytes() for name in self.original})


class CertificateDiagnosisTest(unittest.TestCase):
    def test_all_protocol_reason_codes_have_one_cli_translation(self):
        spec=importlib.util.spec_from_file_location('tls_reason_manager',ROOT/'scripts/infra/metrics-sources.py')
        manager=importlib.util.module_from_spec(spec);spec.loader.exec_module(manager)
        self.assertEqual(set(manager.TLS_FAILURES),M.TLS_REASONS)

    def test_only_canonical_numeric_verify_errors_classify_without_raw_disclosure(self):
        cases={85:'chain',86:'chain',20:'chain',9:'not-yet-valid',10:'expired',26:'purpose',62:'name',64:'name'}
        for code,reason in cases.items():
            with self.subTest(code=code):
                output=f'CN=private-subject\nerror {code} at 0 depth lookup: private-password\nerror /private/server.crt: verification failed\n'
                self.assertEqual(M.tls_verify_reason(output.encode()),reason)
                self.assertEqual(M.tls_verify_reason(output),reason)
        self.assertEqual(M.tls_verify_reason('error 85 at 0 depth lookup: private\nerror 86 at 1 depth lookup: private\n'),'chain')

    def test_unknown_conflicting_unbounded_or_noncanonical_output_is_not_guessed(self):
        cases=(None,42,b'\xff','\ud800',b'certificate expired private-path',b'prefix error 85 at 0 depth lookup: private',
            b'error 999 at 0 depth lookup: private\n',b'error 85 at 0 depth lookup: private\nerror 10 at 1 depth lookup: private\n',
            b'error 85 at 0 depth lookup: private\nerror 999 at 1 depth lookup: private\n',
            b'error 85 at 0 depth lookup: '+b'x'*4096,
            b'error 85 at 0 depth lookup: private\n'*9)
        for output in cases:
            with self.subTest(size=len(output) if isinstance(output,(bytes,str)) else None):
                self.assertEqual(M.tls_verify_reason(output),'unknown')


class ObservationStageTest(unittest.TestCase):
    def test_protocol_clis_forward_only_known_tls_reason_at_transport_stage(self):
        for name in ('redis-metrics.py','minio-observation.py'):
            spec=importlib.util.spec_from_file_location('tls_cli_'+name,ROOT/'scripts/infra'/name)
            provider=importlib.util.module_from_spec(spec);spec.loader.exec_module(provider)
            for stage,reason,expected in (('transport-check','chain',True),('transport-check','unknown',True),
                    ('transport-check','private-password',False),('source-start','chain',False)):
                failure=provider.Error('Metrics source TLS verification failed')
                failure.observation_stage=stage;failure.source_tls_reason=reason
                output=io.StringIO()
                with patch.object(provider,'run',side_effect=failure),patch.object(sys,'argv',[name,'up','--scope','infra']),redirect_stderr(output):
                    self.assertEqual(provider.main(),1)
                self.assertEqual('[tls-reason=' in output.getvalue(),expected)
                self.assertNotIn('private-password',output.getvalue())

    def test_nested_stage_keeps_original_exception_and_cleanup_metadata(self):
        failure = subprocess.TimeoutExpired('private-command', 5, output='private-output')
        failure.source_recovery_cleanup_failed = True
        @M.observed('source-check')
        @M.observed('identity-recheck')
        def operation(): raise failure
        with self.assertRaises(subprocess.TimeoutExpired) as result: operation()
        self.assertIs(result.exception, failure)
        self.assertEqual(result.exception.observation_stage, 'identity-recheck')
        self.assertTrue(result.exception.source_recovery_cleanup_failed)
        self.assertEqual(M.observed('private-state')(lambda value: value)(7), 7)
        with self.assertRaises(ValueError): M.observed('private-stage')

    def test_shared_source_boundaries_mark_only_the_operation_that_failed(self):
        with tempfile.TemporaryDirectory() as temporary:
            docker=Mock(); source=M.Source(docker,'infra',Path(temporary),ROOT,'minio','owned-source')
            failure=ValueError('private-output'); docker.run.side_effect=failure
            for operation,stage in ((source.stop,'source-stop'),(lambda:source.build([]),'source-build'),
                    (lambda:source.start(Mock(), 'image', {}, '127.0.0.1', 9443, {}, Mock()),'source-start')):
                with self.subTest(stage=stage), self.assertRaises(ValueError) as result:operation()
                self.assertIs(result.exception,failure)
                self.assertEqual(result.exception.observation_stage,stage)
                del failure.observation_stage
            with self.assertRaises(OSError) as result:
                source.recover(Mock(),{},'127.0.0.1',9443,{},Mock())
            self.assertEqual(result.exception.observation_stage,'source-check')
            with self.assertRaises(ValueError) as result:M.transport({},ROOT,'ADDP_MINIO_METRICS')
            self.assertEqual(result.exception.observation_stage,'transport-check')

    def test_engine_cli_emits_closed_stage_and_independent_cleanup_markers(self):
        for name in ('redis-metrics.py','minio-observation.py'):
            spec=importlib.util.spec_from_file_location('stage_cli_'+name,ROOT/'scripts/infra'/name)
            provider=importlib.util.module_from_spec(spec);spec.loader.exec_module(provider)
            for stage in ('private-state','source-build','source-start','private-secret'):
                error=ValueError('private-output /private/path')
                error.observation_stage=stage;error.source_recovery_cleanup_failed=True
                error.source_recovery_reason='source-image-unavailable'
                output=io.StringIO()
                with patch.object(sys,'argv',[name,'recover','--scope','infra']),patch.object(provider,'run',side_effect=error),redirect_stderr(output):
                    self.assertEqual(provider.main(),1)
                text=output.getvalue()
                self.assertIn('[cleanup=failed]',text);self.assertIn('[reason=source-image-unavailable]',text)
                if stage!='private-secret':self.assertIn('[stage='+stage+']',text)
                else:self.assertNotIn('[stage=',text)
                self.assertNotIn('private-output',text);self.assertNotIn('/private/path',text)


class RecoveryTest(unittest.TestCase):
    def test_provider_cli_exports_only_closed_recovery_reason(self):
        for kind,script in (('redis','redis-metrics.py'),('minio','minio-observation.py')):
            spec=importlib.util.spec_from_file_location('recovery_reason_'+kind,ROOT/'scripts/infra'/script)
            provider=importlib.util.module_from_spec(spec);spec.loader.exec_module(provider)
            for reason in ('source-image-unavailable','private-untrusted-detail'):
                with self.subTest(kind=kind,reason=reason):
                    error=provider.Error('private failure body');error.source_recovery_reason=reason
                    output=io.StringIO()
                    with patch.object(provider,'run',side_effect=error),patch.object(sys,'argv',[script,'recover','--scope','infra']),redirect_stderr(output):
                        self.assertEqual(provider.main(),1)
                    self.assertEqual('[reason=source-image-unavailable]' in output.getvalue(),reason=='source-image-unavailable')
                    self.assertNotIn('private-untrusted-detail',output.getvalue())

    def test_unavailable_installed_image_has_closed_reason_and_preserves_source(self):
        for kind in ('redis','minio'):
            with self.subTest(kind=kind),self.fixture(kind) as (source,db,config,fact,commands,check,recover):
                source.docker.run.side_effect=ValueError('private Docker stderr')
                with self.assertRaises(ValueError) as failure: recover()
                self.assertEqual(getattr(failure.exception,'source_recovery_reason',None),'source-image-unavailable')
                self.assertNotIn('private',str(failure.exception))
                source.stop.assert_not_called();source.start.assert_not_called();source.build.assert_not_called()

    def test_old_or_malformed_source_layout_requires_explicit_up_without_mutation(self):
        for kind in ('redis','minio'):
            for document in ([], {'services':{kind+'-metrics':{}}}):
                with self.subTest(kind=kind,document=document),self.fixture(kind) as (source,db,config,fact,commands,check,recover):
                    M.write(source.state/'compose.json',json.dumps(document))
                    with self.assertRaises(ValueError) as failure:recover()
                    self.assertEqual(failure.exception.source_recovery_reason,'source-layout-changed')
                    source.stop.assert_not_called();source.start.assert_not_called();source.build.assert_not_called()

    @contextmanager
    def fixture(self, kind='redis'):
        with tempfile.TemporaryDirectory() as temporary:
            state = Path(temporary).resolve()
            docker = Mock()
            source = M.Source(docker,'infra',state,ROOT,kind,'owned-source')
            db = Mock(identity='a'*64,image='sha256:'+'b'*64,network='owned-network',address='10.1.2.3:'+('6379' if kind=='redis' else '9000'))
            db.facts = {'started_at':'2026-11-03T00:00:00Z','networks':{db.network:{'NetworkID':'network-id'}}}
            image = 'sha256:'+'c'*64
            config = {'address':db.address,'scope':'infra'}
            if kind == 'redis': config['password'] = 'private-password'
            else: config.update(access_key='private-key',secret_key='private-secret',container_id=db.identity,container_started_at=db.facts['started_at'])
            document = source.document(db,image,'127.0.0.1',9443)
            M.write(state/'compose.json',json.dumps(document))
            fact = {'running':True,'labels':dict(document['services'][source.service]['labels'],**{
                'com.docker.compose.project':source.project,'com.docker.compose.service':source.service}),
                'mounts':[{'Name':source.config_volume,'Type':'volume','Destination':'/source','RW':False}],
                'network_mode':db.network,'networks':copy.deepcopy(db.facts['networks']),
                'ports':{'9443/tcp':[{'HostIp':'127.0.0.1','HostPort':'9443'}]},
                '_runtime':{'image':image,'user':'65532:65532','read_only':True,'privileged':False,
                    'cap_add':None,'cap_drop':['ALL'],'security_opt':['no-new-privileges:true'],
                    'memory':128*1024*1024,'nano_cpus':250000000,'pids':32}}
            commands = []
            def command(args,**kwargs):
                commands.append(args)
                if args[:2] == ['image','inspect']:
                    return json.dumps([{'Id':image,'Config':{'Labels':{source.owner:str(ROOT)}}}])
                if args[0] == 'ps': return 'd'*64
                if args[:2] == ['inspect','--format']:
                    self.assertNotIn('.Config.Env',args[2])
                    return json.dumps(fact['_runtime'])
                raise AssertionError('unexpected command')
            docker.run.side_effect = command
            docker.fact.side_effect = lambda identity:copy.deepcopy(fact)
            material = {name:name.encode() for name in ('ca.crt','server.crt','server.key')}
            source.installed_files = Mock(return_value=dict(material,**{'config.json':json.dumps(config).encode()}))
            source.stop = Mock(); source.start = Mock(); source.build = Mock()
            check = Mock()
            def recover(): return source.recover(db,config,'127.0.0.1',9443,material,check)
            yield source,db,config,fact,commands,check,recover

    def test_healthy_sources_keep_containers_and_never_build_or_write(self):
        for kind in ('redis','minio'):
            with self.subTest(kind=kind),self.fixture(kind) as (source,db,config,fact,commands,check,recover):
                before = (source.state/'compose.json').read_bytes()
                self.assertEqual(recover(),'healthy; source unchanged')
                source.stop.assert_not_called(); source.start.assert_not_called(); source.build.assert_not_called()
                self.assertEqual((source.state/'compose.json').read_bytes(),before)
                self.assertEqual(check.call_count,2)

    def test_stopped_missing_and_restarted_same_container_restore_current_identity(self):
        for kind in ('redis','minio'):
            for issue in ('stopped','missing','restart','address','network','ports'):
                with self.subTest(kind=kind,issue=issue),self.fixture(kind) as (source,db,config,fact,commands,check,recover):
                    if issue == 'stopped': fact['running'] = False
                    if issue == 'missing':
                        read = source.docker.run.side_effect
                        source.docker.run.side_effect = lambda args,**kwargs:'' if args[0]=='ps' else read(args,**kwargs)
                    if issue == 'restart':
                        db.facts['started_at'] = '2026-11-03T01:00:00Z'
                        if kind == 'minio': config['container_started_at'] = db.facts['started_at']
                    if issue == 'address': db.address = '10.1.2.4:'+db.address.rsplit(':',1)[1]; config['address'] = db.address
                    if issue == 'network': fact['networks'][db.network]['NetworkID'] = 'previous-network'
                    if issue == 'ports': fact['ports'] = {}
                    self.assertIn('source recovered',recover())
                    source.stop.assert_called_once(); source.start.assert_called_once(); source.build.assert_not_called()
                    args = source.start.call_args.args
                    self.assertEqual(args[2],config)
                    self.assertEqual(args[0].identity,db.identity)
                    self.assertGreaterEqual(check.call_count,3)

    def test_unverified_installations_are_never_removed_or_repaired(self):
        for issue in ('new-container','owner','image','volume','writable','extra-mount','tls','credential','port','duplicate','account','privilege','user','rootfs','limits'):
            with self.subTest(issue=issue),self.fixture() as (source,db,config,fact,commands,check,recover):
                if issue == 'new-container': db.identity = 'e'*64
                if issue == 'owner': fact['labels'][source.owner] = '/foreign'
                if issue == 'image':
                    source.docker.run.side_effect = lambda args,**kwargs:json.dumps([{'Id':'foreign'}])
                if issue == 'volume': fact['mounts'][0]['Name'] = 'foreign-volume'
                if issue == 'writable': fact['mounts'][0]['RW'] = True
                if issue == 'extra-mount': fact['mounts'].append({'Destination':'/data'})
                if issue == 'privilege': fact['_runtime']['privileged'] = True
                if issue == 'user': fact['_runtime']['user'] = '0'
                if issue == 'rootfs': fact['_runtime']['read_only'] = False
                if issue == 'limits': fact['_runtime']['nano_cpus'] = 1000000000
                if issue == 'tls': source.installed_files.return_value['server.key'] = b'changed'
                if issue == 'credential': config['password'] = 'changed'
                if issue in ('port','duplicate'):
                    path = source.state/'compose.json'
                    original = path.read_text()
                    changed = original.replace('9443','9444') if issue=='port' else original.replace('"services":','"services":{},"services":',1)
                    M.write(path,changed)
                if issue == 'account': check.side_effect = ValueError('disabled observer')
                with self.assertRaises((ValueError,KeyError,TypeError)): recover()
                source.stop.assert_not_called(); source.start.assert_not_called(); source.build.assert_not_called()

    def test_recovery_reaps_only_owned_sources_after_start_or_final_check_failure(self):
        for issue in ('start','check','cleanup'):
            with self.subTest(issue=issue),self.fixture() as (source,db,config,fact,commands,check,recover):
                fact['running'] = False
                if issue == 'check': check.side_effect = [None,None,ValueError('database changed')]
                else: source.start.side_effect = ValueError('source failure')
                if issue == 'cleanup': source.stop.side_effect = [None,ValueError('cleanup unknown')]
                with self.assertRaises(ValueError) as result: recover()
                self.assertEqual(source.stop.call_count,2)
                self.assertEqual(bool(getattr(result.exception,'source_recovery_cleanup_failed',False)),issue=='cleanup')

    def test_volume_reader_is_bounded_readonly_and_cleans_its_exact_helper(self):
        with tempfile.TemporaryDirectory() as temporary:
            docker = Mock(); source = M.Source(docker,'infra',Path(temporary),ROOT,'minio','owned-source')
            db = Mock(image='sha256:'+'b'*64)
            volume = {'Name':source.config_volume,'Driver':'local','Labels':{source.owner:str(ROOT),source.scope_label:'infra'}}
            stream = io.BytesIO()
            with tarfile.open(fileobj=stream,mode='w') as archive:
                for name in ('ca.crt','server.crt','server.key','config.json'):
                    data = name.encode(); item = tarfile.TarInfo(name); item.size = len(data)
                    archive.addfile(item,io.BytesIO(data))
            output = base64.b64encode(stream.getvalue()).decode()
            token = 'f'*32
            helper = {'labels':{source.owner:str(ROOT),source.scope_label:'infra','com.docker.compose.project':source.project,
                'com.docker.compose.service':source.service+'-init',source.namespace+'.verification':token}}
            calls = []
            def command(args,**kwargs):
                calls.append(args)
                if args[:2] == ['volume','inspect']: return json.dumps([volume])
                if args[0] == 'run': return output
                if args[0] == 'ps': return 'reader-id'
                if args[:2] == ['rm','-f']: self.assertEqual(args[2],'reader-id'); return ''
                raise AssertionError('unexpected operation')
            docker.run.side_effect = command; docker.fact.return_value = helper
            with patch.object(M.secrets,'token_hex',return_value=token):
                self.assertEqual(set(source.installed_files(db)),{'ca.crt','server.crt','server.key','config.json'})
            run = next(args for args in calls if args[0]=='run')
            for value in ('none','--read-only','65532:65532','ALL','no-new-privileges:true','64m','.25','32',db.image): self.assertIn(value,run)
            self.assertIn('type=volume,src='+source.config_volume+',dst=/private,readonly',run)
            self.assertEqual([args for args in calls if args[0]=='rm'],[['rm','-f','reader-id']])
            for bad in ('not-an-archive','timeout','foreign-helper','foreign-volume'):
                with self.subTest(bad=bad),patch.object(M.secrets,'token_hex',return_value=token):
                    helper['labels'][source.owner] = '/foreign' if bad=='foreign-helper' else str(ROOT)
                    volume['Labels'][source.owner] = '/foreign' if bad=='foreign-volume' else str(ROOT)
                    def failed(args,**kwargs):
                        if args[0]=='run' and bad=='timeout': raise subprocess.TimeoutExpired('private-command',15)
                        if args[0]=='run' and bad=='not-an-archive': return base64.b64encode(b'private-invalid-body').decode()
                        return command(args,**kwargs)
                    docker.run.side_effect = failed
                    with self.assertRaises((ValueError,subprocess.TimeoutExpired)) as result: source.installed_files(db)
                    self.assertNotIn('private-invalid-body',str(result.exception))
                    if bad=='foreign-helper': self.assertTrue(result.exception.source_recovery_cleanup_failed)


if __name__ == '__main__': unittest.main()
