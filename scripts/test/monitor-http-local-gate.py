#!/usr/bin/env python3
"""Manual, single-window formal Python HTTP quality acceptance; never CI/Hosted."""
from __future__ import annotations
import ipaddress
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
import signal
import sys
import tempfile
import threading
import time
import hashlib
from datetime import datetime, timezone
import importlib.util

spec=importlib.util.spec_from_file_location('http_local_core',Path(__file__).with_name('monitor-traces-local-gate.py'))
core=importlib.util.module_from_spec(spec);spec.loader.exec_module(core)
require=core.require
LABEL='com.addp.http-quality-local'
BACKENDS=('system-backend','gateway','monitor-backend','agent-backend','copilot-backend')
FRONTENDS=core.FRONTENDS
LIMITS={name:(.025,32) for name in FRONTENDS}|{
    'system-backend':(.5,768),'gateway':(.25,512),'monitor-backend':(.5,768),
    'agent-backend':(1,2048),'copilot-backend':(.75,1024),'postgres':(.5,768),
    'redis':(.1,128),'prometheus':(1,2048),'metrics-control':(.25,128)}
SAMPLER_IMAGE='nginx:1.28.0-alpine@sha256:30f1c0d78e0ad60901648be663a710bdadf19e4c10ac6782c235200619158284'
OWN_INPUTS=('scripts/test/monitor-http-local-gate.py','scripts/test/monitor-http-local-gate_test.py',
    'scripts/test/monitor-http-local-online.py','scripts/test/monitor-http-local-online_test.py',
    'scripts/test/monitor-traces-local-gate.py','scripts/test/monitor-traces-local-online.py',
    'system/backend/cmd/online-test-fixture/http_quality_local.go',
    'system/backend/cmd/online-test-fixture/http_quality_local_test.go',
    'system/backend/cmd/online-test-fixture/execution_trace_local.go',
    'console/frontend/e2e/online/http-quality-local.spec.js',
    'console/frontend/e2e/online/local-identity-support.mjs')

class Lifecycle(core.Lifecycle):
    label=LABEL
    run_prefix='addp-http-local-'
    strict_resources=True
    work_seconds=3300  # reserve five minutes of the original 60-minute window for cleanup
    product_services=BACKENDS+FRONTENDS
    compile_services=('system-backend','gateway','monitor-backend','runtime-log')
    @property
    def sampling_image(self):
        require(hasattr(self,'sampler_image_proof'),'sampler_image_not_admitted')
        return self.sampler_image_proof['image_id']

    @property
    def phase(self):
        return getattr(self,'_phase','admission')

    @phase.setter
    def phase(self,value):
        require(isinstance(value,str) and re.fullmatch('[a-z0-9_]{1,100}',value),'local_phase_invalid')
        changed=value!=getattr(self,'_phase',None)
        self._phase=value
        if changed and hasattr(self,'started'):
            print('Local formal Python HTTP: phase='+value+' elapsed_seconds='+str(round(time.monotonic()-self.started,2)),flush=True)

    def command(self,*args,**kwargs):
        if self.phase=='cleanup' and hasattr(self,'cleanup_deadline'):
            remaining=self.cleanup_deadline-time.monotonic()
            require(remaining>0,'cleanup_deadline_exceeded')
            kwargs['timeout']=min(kwargs.get('timeout',600),remaining)
        return super().command(*args,**kwargs)

    def cleanup(self):
        self.cleanup_deadline=min(self.started+3600,time.monotonic()+120)
        return super().cleanup()


def validate_profile(env,machine,daemon):
    core.validate_profile(env,machine,daemon)
    require(not any(env.get(k) for k in ('ADDP_EXECUTION_TRACE_LOCAL','ADDP_CATALOG_BROWSER_LOCAL')),
            'other_local_profile_forbidden')


def validate_sampler_image(metadata):
    require(isinstance(metadata,dict),'sampler_image_metadata_invalid')
    image_id=metadata.get('id')
    expected='nginx@'+SAMPLER_IMAGE.split('@')[1]
    require(isinstance(image_id,str) and re.fullmatch('sha256:[a-f0-9]{64}',image_id) and
        metadata.get('os')=='linux' and metadata.get('architecture')=='arm64' and
        isinstance(metadata.get('repo_digests'),list) and expected in metadata['repo_digests'],
        'sampler_image_identity_mismatch')
    return {'reference':SAMPLER_IMAGE,'image_id':image_id,'platform':'linux/arm64',
        'repository_digest':expected,'acquisition':'verified_local_cache'}


def admit_sampler_image(life):
    life.phase='sampler_image_admission'
    metadata=json.loads(life.docker('image','inspect',SAMPLER_IMAGE,'--format',
        '{"id":{{json .Id}},"os":{{json .Os}},"architecture":{{json .Architecture}},"repo_digests":{{json .RepoDigests}}}',timeout=5))
    life.sampler_image_proof=validate_sampler_image(metadata)


def source_addresses(subnet):
    network=ipaddress.ip_network(subnet,strict=True)
    require(network.version==4 and network.is_private and not (network.is_loopback or network.is_link_local or network.is_reserved or network.network_address.is_unspecified) and network.num_addresses>=64,'owned_ipv4_subnet_too_small')
    return {'agent':str(network.broadcast_address-10),'copilot':str(network.broadcast_address-11)}


def defaults(life):
    env=core.isolated_defaults(life)
    env.update(ENV='test',POSTGRES_DB='addp_test',POSTGRES_USER='http_local',POSTGRES_HOST='127.0.0.1',
        POSTGRES_PORT='5432',ADDP_HTTP_LOCAL='1',ADDP_HTTP_LOCAL_RUN_ID=life.uuid,
        COMPOSE_DISABLE_ENV_FILE='1',ADDP_PUBLIC_ORIGIN='http://nginx',ALLOWED_ORIGINS='http://nginx',
        ADDP_HOST_NODE_NAME='http-quality-local',ADDP_RUNTIME_LOG_ROOT=str(life.work/'unused-logs'),
        ADDP_OBSERVABILITY_LOGS_ENABLED='false',ADDP_OBSERVABILITY_TRACES_ENABLED='false',
        ADDP_OBSERVABILITY_METRICS_ENABLED='true',IMAGE_TAG=life.run_id,REGISTRY='localhost:5001',
        NO_PROXY='127.0.0.1,localhost',no_proxy='127.0.0.1,localhost',
        MONITOR_PROMETHEUS_URL='https://prometheus:9090',
        MONITOR_METRICS_ALLOWED_PORTS='19444,19445',
        MONITOR_METRICS_ALLOWED_CIDRS=','.join(v+'/32' for v in life.addresses.values()),
        ADDP_METRICS_CONTROL_ENABLED='true',PROMETHEUS_SYSTEM_URL='https://metrics-control:9444',
        PROMETHEUS_MONITOR_URL='https://metrics-control:9444',ADDP_METRICS_CONTROL_GATEWAY_URL='http://gateway:8000')
    return env


def tls(life,env):
    """Short-lived independent center/source/control roots; private keys never enter reports."""
    directory=life.work/'certificates';directory.mkdir(mode=0o700)
    def ca(name):
        key=directory/(name+'.key');crt=directory/(name+'.crt')
        life.command(['openssl','req','-x509','-newkey','rsa:2048','-nodes','-days','1',
            '-subj','/CN=local-'+name,'-addext','basicConstraints=critical,CA:TRUE',
            '-addext','keyUsage=critical,keyCertSign,cRLSign','-keyout',str(key),'-out',str(crt)],timeout=10)
        return key,crt
    def cert(name,root,usage,sans):
        key=directory/(name+'.key');csr=directory/(name+'.csr');crt=directory/(name+'.crt')
        ext=directory/(name+'.ext');ext.write_text('extendedKeyUsage='+usage+'\nsubjectAltName='+sans+'\n')
        life.command(['openssl','req','-newkey','rsa:2048','-nodes','-subj','/CN='+name,
            '-keyout',str(key),'-out',str(csr)],timeout=10)
        life.command(['openssl','x509','-req','-in',str(csr),'-CA',str(root[1]),'-CAkey',str(root[0]),
            '-set_serial',str(secrets.randbits(120)),'-days','1','-extfile',str(ext),'-out',str(crt)],timeout=10)
        return key,crt
    def copy(source,dest,mode=0o644):
        shutil.copyfile(source,dest);dest.chmod(mode)
    center,source,control=ca('center'),ca('source'),ca('control')
    query=cert('query',center,'clientAuth','DNS:query')
    health=cert('health',center,'clientAuth','DNS:health')
    server=cert('prometheus',center,'serverAuth','DNS:prometheus,DNS:localhost')
    collector=cert('collector',source,'clientAuth','DNS:collector')
    admission=cert('admission',source,'clientAuth','DNS:admission')
    proxy=cert('metrics-control',control,'serverAuth','DNS:metrics-control')
    deployment=life.work/'metrics-deployment';deployment.mkdir(mode=0o700)
    center_dir=life.work/'metrics-center';center_dir.mkdir(mode=0o755)
    control_dir=life.work/'metrics-control';control_dir.mkdir(mode=0o755)
    # UID 65534 reads only these mount trees; the enclosing host directory remains 0700.
    for path,items in ((center_dir,{'ca.crt':center[1],'server.crt':server[1],'server.key':server[0],
            'health.crt':health[1],'health.key':health[0]}),
            (control_dir,{'server.crt':proxy[1],'server.key':proxy[0]}),
            (deployment,{'control-ca.crt':control[1],'source-ca.crt':source[1],
             'collector.crt':collector[1],'collector.key':collector[0]})):
        for name,item in items.items():copy(item,path/name)
    (deployment/'prometheus-client-secret').write_text(env['PROMETHEUS_SERVICE_CLIENT_SECRET'])
    env.update(ADDP_METRICS_DEPLOYMENT_DIR=str(deployment),ADDP_METRICS_TLS_DIR=str(center_dir),
        ADDP_METRICS_CONTROL_DIR=str(control_dir),MONITOR_PROMETHEUS_CA_FILE=str(center[1]),
        MONITOR_PROMETHEUS_CLIENT_CERT_FILE=str(query[1]),MONITOR_PROMETHEUS_CLIENT_KEY_FILE=str(query[0]),
        MONITOR_METRICS_CA_FILE=str(source[1]),MONITOR_METRICS_CLIENT_CERT_FILE=str(admission[1]),
        MONITOR_METRICS_CLIENT_KEY_FILE=str(admission[0]))
    declarations=[]
    for i,(module,address) in enumerate(life.addresses.items()):
        path=life.work/(module+'-source');path.mkdir(mode=0o700)
        pair=cert(module,source,'serverAuth','IP:'+address)
        for name,item in {'ca.crt':source[1],'server.crt':pair[1],'server.key':pair[0]}.items():
            copy(item,path/name,0o600 if name.endswith('.key') else 0o644)
        declarations.append(dict(module_name=module,role='backend',listen=address+':'+str(19444+i),
            endpoint='https://'+address+':'+str(19444+i)+'/metrics',tls_dir=str(path)))
    env['ADDP_PROCESS_METRICS_DEPLOYMENTS']=json.dumps(declarations)
    life.command([sys.executable,'scripts/infra/generate-metrics-config.py'],env=env)
    life.command([sys.executable,'scripts/prod/generate-process-metrics-platform.py'],env=env)
    # The generated Prometheus config and public certificates must be readable by its product UID.
    deployment.chmod(0o755)


def project(life,env,images):
    infra=json.loads(life.command(['docker','compose','--env-file','/dev/null','--profile','observability-metrics',
        '--profile','observability-metrics-control','-f','docker-compose.infra.yml','config','--format','json'],env=env))
    files=['docker-compose.yml','scripts/prod/metrics-platform.yml','scripts/prod/metrics-query.yml',
           str(life.work/'metrics-deployment/platform-process-metrics.json')]
    product=json.loads(life.command(['docker','compose','--env-file','/dev/null',
        *[v for f in files for v in ('-f',f)],'config','--format','json'],env=env))
    require({'postgres','redis','prometheus','metrics-control'}<=infra.get('services',{}).keys(),
        'product_metrics_profile_services_missing')
    require(set(BACKENDS+FRONTENDS)<=product.get('services',{}).keys(),'product_http_services_missing')
    document={name:infra['services'][name] for name in ('postgres','redis','prometheus','metrics-control')}
    document.update({name:product['services'][name] for name in BACKENDS+FRONTENDS})
    document['postgres']['command']+=['-c','cluster_name=addp-http-quality-local-'+life.uuid]
    config=core.isolate(document,life.run_id,images,label=LABEL,limits=LIMITS)
    # Preserve the product volume initialization contract (notably the non-root
    # Prometheus data directory); only names/ownership are isolated here.
    for name,service in config['services'].items():
        original={mount['target']:mount for mount in document[name].get('volumes',[]) if mount['type']=='volume'}
        for mount in service['volumes']:
            if mount['type']=='volume' and mount['target'] in original:
                mount['volume']=dict(original[mount['target']].get('volume',{}))
    config['networks']['data']={'external':True,'name':life.network}
    for module,address in life.addresses.items():
        service=config['services'][module+'-backend']
        service['networks']['data']['ipv4_address']=address
        # Product Python runs as UID 0 and verifies source TLS ownership. A
        # host-user bind cannot satisfy that on Desktop. Copy exactly the same
        # approved inputs to an owned root-only volume, then mount it read-only.
        logical='sdk-'+module+'-tls'
        config['volumes'][logical]={'labels':{LABEL:life.run_id}}
        service['volumes']=[mount for mount in service['volumes'] if mount['target']!='/etc/addp/process-metrics/source']+[
            {'type':'volume','source':logical,'target':'/etc/addp/process-metrics/source','read_only':True,'volume':{'nocopy':True}}]
    life.deployment_config=config
    life.compose_path=life.private_json('compose.json',config)
    life.compose=lambda *args,**kw:life.command(['docker','compose','--env-file','/dev/null','-f',str(life.compose_path),*args],
        env=env,timeout=kw.pop('timeout',300),**kw)
    return config


def build(life):
    life.phase='source_snapshot'
    commit=life.command(['git','rev-parse','HEAD']).strip()
    life.input_hashes={}
    for relative in OWN_INPUTS:
        saved=life.command(['git','show',commit+':'+relative]).encode()
        require((core.ROOT/relative).read_bytes()==saved,'commit_all_http_inputs_before_acceptance')
        life.input_hashes[relative]=hashlib.sha256(saved).hexdigest()
    for relative,marker in {'Makefile':'test-monitor-http-local:',
            'scripts/ci/check-online-ci-registration.py':'def validate_local_http_quality_profile',
            'system/backend/cmd/online-test-fixture/main.go':'return runHTTPQualityLocal(environment, *output, *suite)'}.items():
        saved=life.command(['git','show',commit+':'+relative]).encode()
        require(marker.encode() in saved,'committed_http_integration_missing')
        life.input_hashes[relative]=hashlib.sha256(saved).hexdigest()
    life.command(['git','clone','--no-local','--depth','1','--single-branch','--branch','main',core.ROOT.as_uri(),str(life.checkout)])
    require(life.command(['git','rev-parse','HEAD']).strip()==commit,'snapshot_commit_changed')
    life.source_commit=commit
    admit_sampler_image(life)
    life.phase='owned_network_admission'
    life.network=life.run_id+'_data'
    life.docker('network','create','--internal','--label',LABEL+'='+life.run_id,life.network)
    network=json.loads(life.docker('network','inspect',life.network))[0]
    require(network.get('Internal') is True and network.get('Labels',{}).get(LABEL)==life.run_id,'network_owner_mismatch')
    subnets=[v['Subnet'] for v in network['IPAM']['Config'] if ':' not in v['Subnet']]
    require(len(subnets)==1,'single_owned_ipv4_subnet_required')
    life.addresses=source_addresses(subnets[0])
    env=defaults(life);life.env=env
    life.phase='metrics_deployment_projection';tls(life,env)
    references={name:f'localhost:5001/addp-{name}:{life.run_id}' for name in BACKENDS+FRONTENDS}
    project(life,env,references);core.prepare_sampling_volumes(life)
    proxies=core.prepare_builder(life)
    life.sample();life.thread=threading.Thread(target=life.monitor,daemon=True);life.thread.start()
    life.stop_builder()
    images=core.build_products(life,env,proxies)
    life.images=images
    for name,image in images.items():life.deployment_config['services'][name]['image']=image
    life.compose_path.write_text(json.dumps(life.deployment_config))
    return env


def prepare_source_tls(life):
    for module in ('agent','copilot'):
        life.docker('run','--pull','never','--name',life.run_id+'-tls-'+module,'--label',LABEL+'='+life.run_id,
            '--label',LABEL+'.role=oneshot','--cpus','.1','--memory','64m','--network','none','--read-only',
            '--mount',f'type=bind,source={life.work/(module+"-source")},target=/input,readonly',
            '--mount',f'type=volume,source={life.run_id+"_sdk-"+module+"-tls"},target=/tls',
            '--entrypoint','/bin/sh',life.sampling_image,'-ec',
            'cp /input/ca.crt /input/server.crt /input/server.key /tls/; chmod 700 /tls; chmod 600 /tls/server.key; '
            'test "$(stat -c %u /tls)" = 0; test "$(stat -c %u /tls/server.key)" = 0',timeout=10)


def setup(life,env):
    life.phase='isolated_databases_start'
    life.compose('up','-d','--wait','--no-build','postgres','redis')
    life.compose('exec','-T','postgres','sh','-ec','exec psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"',
        data=(life.checkout/'scripts/infra/init-postgresql.sql').read_text())
    life.phase='owned_source_tls_preparation';prepare_source_tls(life)
    life.phase='formal_product_start'
    for group in [('system-backend',),('gateway','monitor-backend'),('metrics-control','prometheus'),
                  ('agent-backend','copilot-backend'),FRONTENDS]:
        life.compose('up','-d','--wait','--no-build',*group)
    for name in BACKENDS+FRONTENDS:
        row=json.loads(life.docker('inspect',life.run_id+'-'+name))[0]
        require(row['Image']==life.images[name],'product_image_identity_changed')
        if name in ('agent-backend','copilot-backend'):
            address=row['NetworkSettings']['Networks'][life.network]['IPAddress']
            require(address==life.addresses[name.removesuffix('-backend')],'source_ip_identity_changed')
    for name,port,key in [('system-backend',8180,'SYSTEM_URL'),('gateway',8000,'GATEWAY_URL')]:
        binding=json.loads(life.docker('inspect',life.run_id+'-'+name))[0]['NetworkSettings']['Ports'][str(port)+'/tcp']
        require(len(binding)==1 and binding[0]['HostIp']=='127.0.0.1','loopback_publication_required')
        env[key]='http://127.0.0.1:'+binding[0]['HostPort']
    # Full installed distributions, not a hand-selected mini dependency fixture.
    life.phase='full_python_dependencies'
    life.python_proof={}
    for service in ('agent-backend','copilot-backend'):
        life.compose('exec','-T',service,'python','-m','pip','check')
        proof=json.loads(life.compose('exec','-T',service,'python','-c',
            'import sys,importlib.metadata as m; print(__import__("json").dumps({"python":sys.version.split()[0],"distributions":len(list(m.distributions())),"addp_common":m.version("addp-common")}))'))
        require(proof['distributions']>20 and proof['python'].startswith('3.12.' if service=='agent-backend' else '3.11.'),
            'full_product_python_identity_invalid')
        life.python_proof[service]=proof
    core.identity(life,env,suite='python-http-product-local',prefix='ADDP_HTTP_LOCAL_')


def main():
    require(len(sys.argv)==1,'local_entry_has_no_options')
    life=Lifecycle()
    report={'schema':'addp.http-quality-local/v1','run_id':life.run_id,'result':'failed','hosted':False,'capacity':False,
            'formal_runner_t4':False,'assertions':{},'started_at':datetime.now(timezone.utc).isoformat(),
            'limits':{'seconds':3600,'cpu':8,'memory_bytes':12*1024**3,'private_writable_stop_bytes':4*1024**3,'complete_scan_seconds':4}}
    target=Path(tempfile.gettempdir())/(life.run_id+'-report.json')
    try:
        validate_profile(os.environ,platform.machine(),platform.machine())
        endpoint=core.effective_docker_endpoint(os.environ,lambda name:json.loads(life.docker('context','inspect',*([name] if name else [])))[0]['Endpoints']['docker']['Host'])
        core.private_docker_client(life,endpoint)
        validate_profile(os.environ,platform.machine(),life.docker('info','--format','{{.Architecture}}').strip())
        env=build(life);setup(life,env)
        os.environ['NO_PROXY']=os.environ['no_proxy']='127.0.0.1,localhost'
        business=core.module('monitor-http-local-online',life.checkout)
        business.run(life,env,report)
        life.sample();life.check();report['result']='passed'
    except BaseException as error:
        report.update(stage=life.phase,error_code=str(error) if re.fullmatch('[a-z0-9_]+',str(error)) else type(error).__name__)
        if hasattr(life,'last_failure'):report['command_failure']=life.last_failure
        if hasattr(life,'api_failure'):report['api_failure']=life.api_failure
    finally:
        for key,attribute in [('source_commit','source_commit'),('input_sha256','input_hashes'),('images','images'),
                ('python_dependencies','python_proof'),('resource_guard_failure','resource_failure'),('sampler_image','sampler_image_proof'),('go_cache_reclaim','cache_reclaim_receipt')]:
            if hasattr(life,attribute):report[key]=getattr(life,attribute)
        if hasattr(life,'request_budget'):
            report['request_attempt_counts']={'controlled':life.request_budget.controlled,'control':life.request_budget.control}
        report['resource_samples']=life.samples
        errors=life.cleanup()
        elapsed=time.monotonic()-life.started
        report.update(elapsed_seconds=elapsed,owned_list_commands=life.command_evidence.snapshot(),
            cleanup={'zero_owned_resources':not errors,'failures':errors})
        if errors or elapsed>3600:report['result']='failed'
        with open(target,'x',opener=lambda p,f:os.open(p,f,0o600)) as stream:json.dump(report,stream,indent=2)
    print('Local formal Python HTTP: '+report['result']+'; report='+str(target),flush=True)
    return 0 if report['result']=='passed' else 1

if __name__=='__main__':
    signal.signal(signal.SIGTERM,core.interrupted);signal.signal(signal.SIGINT,core.interrupted)
    raise SystemExit(main())
