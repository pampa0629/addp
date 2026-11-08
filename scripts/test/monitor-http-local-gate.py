#!/usr/bin/env python3
"""Manual, single-window formal Python HTTP quality acceptance; never CI/Hosted."""
from __future__ import annotations
from contextlib import contextmanager
import ipaddress
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
import signal
import subprocess
import stat
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
CASE_LIMITS={'full':(3600,3300),'compile-checkpoints':(900,780)}
OWN_INPUTS=('scripts/test/monitor-http-local-gate.py','scripts/test/monitor-http-local-gate_test.py',
    'scripts/test/http-quality-local-browser_test.mjs',
    'scripts/test/monitor-http-local-online.py','scripts/test/monitor-http-local-online_test.py',
    'scripts/test/monitor-traces-local-gate.py','scripts/test/monitor-traces-local-online.py',
    'system/backend/cmd/online-test-fixture/http_quality_local.go',
    'system/backend/cmd/online-test-fixture/http_quality_local_test.go',
    'system/backend/cmd/online-test-fixture/execution_trace_local.go',
    'console/frontend/e2e/online/http-quality-local.spec.js',
    'console/frontend/e2e/http-quality-page-support.mjs',
    'console/frontend/e2e/online/local-identity-support.mjs')

class Lifecycle(core.Lifecycle):
    label=LABEL
    run_prefix='addp-http-local-'
    strict_resources=True
    work_seconds=3300  # reserve five minutes of the original 60-minute window for cleanup
    total_seconds=3600
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
        elif self.total_seconds==900:
            remaining=self.started+self.work_seconds-time.monotonic()
            require(remaining>0,'total_deadline_exceeded')
            kwargs['timeout']=min(kwargs.get('timeout',600),remaining)
        return super().command(*args,**kwargs)

    def cleanup(self):
        self.cleanup_deadline=min(self.started+self.total_seconds,time.monotonic()+120)
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
    life.sampler_image_inspect = {
        'operation': 'inspect_sampler_image',
        'method': 'GET',
        'api_family': '/images/{reference}/json',
        'request_signature_sha256': core.docker_request_signature('GET', '/images/'+SAMPLER_IMAGE+'/json', {}),
    }
    metadata=json.loads(life.docker('image','inspect',SAMPLER_IMAGE,'--format',
        '{"id":{{json .Id}},"os":{{json .Os}},"architecture":{{json .Architecture}},"repo_digests":{{json .RepoDigests}}}',
        timeout=5,timing_receipt=life.sampler_image_inspect))
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


@contextmanager
def preparation_phase(life,phase):
    """Restore the outer stage on success only; a failure keeps its exact step."""
    previous=life.phase
    life.phase=phase
    yield
    life.phase=previous


def tls(life,env):
    """Short-lived independent center/source/control roots; private keys never enter reports."""
    with preparation_phase(life,'http_tls_materials'):
        directory=life.work/'certificates';directory.mkdir(mode=0o700)
        config=directory/'openssl.cnf'
        with open(config,'x',opener=lambda path,flags:os.open(path,flags,0o600)) as stream:
            stream.write('[req]\nprompt = no\ndistinguished_name = dn\n[dn]\n')
        openssl_env={**life.env,'OPENSSL_CONF':str(config)}
        def openssl(step,args):
            with preparation_phase(life,'http_tls_'+step.replace('-','_')):
                return life.command(['openssl',*args],env=openssl_env,timeout=10)
        def ca(name):
            key=directory/(name+'.key');crt=directory/(name+'.crt')
            openssl('ca_'+name,['req','-config',str(config),'-x509','-newkey','rsa:2048','-nodes','-days','1',
                '-subj','/CN=local-'+name,'-addext','basicConstraints=critical,CA:TRUE',
                '-addext','keyUsage=critical,keyCertSign,cRLSign','-keyout',str(key),'-out',str(crt)])
            return key,crt
        def cert(name,root,usage,sans):
            key=directory/(name+'.key');csr=directory/(name+'.csr');crt=directory/(name+'.crt')
            ext=directory/(name+'.ext');ext.write_text('extendedKeyUsage='+usage+'\nsubjectAltName='+sans+'\n')
            openssl('csr_'+name,['req','-config',str(config),'-newkey','rsa:2048','-nodes','-subj','/CN='+name,
                '-keyout',str(key),'-out',str(csr)])
            openssl('sign_'+name,['x509','-req','-in',str(csr),'-CA',str(root[1]),'-CAkey',str(root[0]),
                '-set_serial',str(secrets.randbits(120)),'-days','1','-extfile',str(ext),'-out',str(crt)])
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
        with preparation_phase(life,'http_metrics_config_render'):
            life.command([sys.executable,'scripts/infra/generate-metrics-config.py'],env=env)
        with preparation_phase(life,'http_process_metrics_render'):
            life.command([sys.executable,'scripts/prod/generate-process-metrics-platform.py'],env=env)
        # The generated Prometheus config and public certificates must be readable by its product UID.
        deployment.chmod(0o755)


def project(life,env,images):
    with preparation_phase(life,'http_infra_compose_projection'):
        infra=json.loads(life.command(['docker','compose','--env-file','/dev/null','--profile','observability-metrics',
        '--profile','observability-metrics-control','-f','docker-compose.infra.yml','config','--format','json'],env=env))
    files=['docker-compose.yml','scripts/prod/metrics-platform.yml','scripts/prod/metrics-query.yml',
           str(life.work/'metrics-deployment/platform-process-metrics.json')]
    with preparation_phase(life,'http_product_compose_projection'):
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


def build(life,*,compile_only=False):
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
    images=core.build_products(life,env,proxies,compile_only=compile_only)
    if compile_only:
        life.phase='compile_checkpoint_summary'
        return env
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


def local_case(arguments):
    require(arguments in ([],['--case=full'],['--case=compile-checkpoints']),'local_case_invalid')
    return arguments[0].split('=',1)[1] if arguments else 'full'


def compile_checkpoint_summary(life):
    samples=[row for row in life.samples if row['phase']=='product_compile']
    require(len(samples)>=12,'compile_checkpoint_samples_insufficient')
    identifiers=set()
    for sample in samples:
        require(sample['sample_duration_seconds']<4,'compile_checkpoint_sample_deadline')
        commands=[command for step in sample['steps'] if step['operation']=='measure_owned_volumes' for command in step['commands']]
        require(len(commands)==1,'compile_checkpoint_command_missing')
        command=commands[0];checkpoint=command.get('volume_measurement_checkpoints',{})
        require(command.get('record_complete') is True and command.get('result')=='passed' and
                command.get('run_id')==life.run_id and command.get('phase')=='product_compile' and
                type(command.get('command_id')) is int and command['command_id']>0 and command['command_id'] not in identifiers,
                'compile_checkpoint_command_identity')
        require(checkpoint.get('fragment_status')=='complete' and checkpoint.get('measurement_entered')=='observed' and
                checkpoint.get('pipeline_returned')=='observed' and type(checkpoint.get('examined_bytes')) is int and
                0<checkpoint['examined_bytes']<=4096,'compile_checkpoint_fragment_incomplete')
        identifiers.add(command['command_id'])
    starts=[row['sample_started_monotonic'] for row in samples]
    require(all(0<b-a<=5 for a,b in zip(starts,starts[1:])),'compile_checkpoint_cadence')
    return {'completed_compile_samples':len(samples),'complete_volume_checkpoints':len(identifiers),
            'controlled_requests':0,'scope':'compile_checkpoint_collection_only'}


VOLUME_EXEC_ACTIONS = ('exec_create', 'exec_start', 'exec_die')
VOLUME_EVENT_OUTPUT_LIMIT = 64 * 1024


def volume_exec_event_candidate(raw, run_id, since, until):
    lines = raw.splitlines()
    require(len(raw.encode()) <= VOLUME_EVENT_OUTPUT_LIMIT and len(lines) <= 256, 'event_limit')
    require(not raw or raw.endswith('\n'), 'events_truncated')
    groups = {}
    container_ids = set()
    for line in lines:
        event = json.loads(line)
        actor = event['Actor']
        attributes = actor['Attributes']
        kind, _, command = event['Action'].partition(': ')
        timestamp = event['timeNano']
        require(event['Type'] == 'container' and attributes.get('name') == run_id+'-space' and
                attributes.get(LABEL) == run_id, 'event_owner_mismatch')
        require(kind in VOLUME_EXEC_ACTIONS and type(timestamp) is int and since <= timestamp <= until,
                'event_shape_invalid')
        if kind != 'exec_die' and command != '/bin/sh -ec '+core.VOLUME_SCAN_COMMAND:
            continue
        container_id, exec_id = actor['ID'], attributes['execID']
        require(re.fullmatch('[a-f0-9]{64}', container_id) and re.fullmatch('[a-f0-9]{64}', exec_id),
                'event_identity_invalid')
        container_ids.add(container_id)
        groups.setdefault(exec_id, []).append({'event':kind,'time_utc_ns':timestamp})

    candidates = [(exec_id, events) for exec_id, events in groups.items()
                  if any(event['event'] != 'exec_die' for event in events)]
    require(len(container_ids) <= 1 and len(candidates) <= 1, 'events_ambiguous')
    require(candidates, 'events_missing')
    exec_id, events = candidates[0]
    times = [event['time_utc_ns'] for event in events]
    actions = tuple(event['event'] for event in events)
    require(actions in (VOLUME_EXEC_ACTIONS[:2], VOLUME_EXEC_ACTIONS) and
            all(a < b for a,b in zip(times,times[1:])), 'event_sequence_incomplete')
    return {'status':'unique_candidate','exit_observed':len(events)==3,'events':events,
            'container_id_sha256':hashlib.sha256(next(iter(container_ids)).encode()).hexdigest(),
            'exec_id_sha256':hashlib.sha256(exec_id.encode()).hexdigest(),
            'exec_start_path_sha256':hashlib.sha256(('/exec/'+exec_id+'/start').encode()).hexdigest()}


def volume_exec_event_window(command, image_anchor, cleanup_anchor):
    offsets = []
    for anchor in (image_anchor, cleanup_anchor):
        before, after, wall = (anchor[key] for key in ('monotonic_before_ns','monotonic_after_ns','wall_utc_ns'))
        require(all(type(value) is int for value in (before,after,wall)) and 0 <= after-before <= 50_000_000,
                'host_clock_unavailable')
        offsets.extend((wall-after, wall-before))
    entered, finished = command['entered_monotonic_ns'], command['finished_monotonic_ns']
    require(max(offsets)-min(offsets) <= 50_000_000 and type(entered) is int and type(finished) is int and
            image_anchor['monotonic_after_ns'] <= entered < finished <= cleanup_anchor['monotonic_before_ns'] and
            finished-entered <= 6_000_000_000, 'host_clock_unavailable')
    return entered+min(offsets)-2_000_000_000, min(finished+max(offsets)+2_000_000_000,cleanup_anchor['wall_utc_ns'])


def collect_volume_exec_timeout_events(life, report):
    failure = report.get('resource_guard_failure', {})
    case, stage = report['case'], report.get('stage')
    compile_stage = case == 'compile-checkpoints' and stage == 'product_compile'
    image_stage = case == 'full' and any(
        stage == 'product_image_'+service.replace('-','_') for service in BACKENDS+FRONTENDS)
    if (not (compile_stage or image_stage) or
            (report.get('result'),report.get('error_code'),failure.get('operation'),failure.get('phase')) !=
            ('failed','resource_sample_deadline_exceeded','measure_owned_volumes',stage) or
            report.get('cleanup',{}).get('zero_owned_resources') is not True):
        return
    commands = [command for step in failure.get('steps',[]) if step['operation']=='measure_owned_volumes'
                for command in step['commands'] if command['result']=='failed']
    receipt = {'status':'unknown','reason':'command_unavailable'}
    report['volume_exec_timeout_events'] = receipt
    if len(commands) != 1:
        return
    command = commands[0]
    if (command.get('run_id') != life.run_id or command.get('phase') != stage or
            command.get('record_complete') is not True or type(command.get('command_id')) is not int):
        return
    receipt['command_id'] = command['command_id']
    try:
        image = report['sampler_image_inspect']
        require(image['run_id']==life.run_id and image['phase']=='sampler_image_admission' and
                image['record_complete'] is True and image['result']=='passed', 'host_clock_unavailable')
        since, until = volume_exec_event_window(command,image['host_clock_end'],core.host_clock_anchor())
    except (KeyError, TypeError, core.GateError):
        receipt['reason'] = 'host_clock_unavailable'
        return

    def unix_time(value):
        return f'{value//10**9}.{value%10**9:09d}'

    args = ['docker','events','--since',unix_time(since),'--until',unix_time(until),'--format','{{json .}}',
            '--filter','type=container','--filter','container='+life.run_id+'-space',
            '--filter',f'label={LABEL}={life.run_id}']
    for kind in VOLUME_EXEC_ACTIONS:
        args.extend(('--filter','event='+kind))
    try:
        # The original cleanup is over, so its command deadline does not govern this read.
        raw = core.Lifecycle.command(life,args,timeout=3,guarded=False,output_limit=VOLUME_EVENT_OUTPUT_LIMIT)
    except core.GateError as error:
        receipt['reason'] = 'event_limit' if str(error)=='command_output_limit_exceeded' else 'event_query_failed'
        return
    except (OSError, ValueError, subprocess.SubprocessError, KeyboardInterrupt):
        receipt['reason'] = 'event_query_failed'
        return
    try:
        candidate = volume_exec_event_candidate(raw,life.run_id,since,until)
    except core.GateError as error:
        receipt['reason'] = str(error)
    except (KeyError, TypeError, ValueError, AttributeError):
        receipt['reason'] = 'event_shape_invalid'
    else:
        receipt.pop('reason')
        receipt.update(candidate)


def resume_apiproxy_events(log_directory, signature, since, until):
    """Read only the fixed Desktop VM stream, refusing ambiguous rotations."""
    paths = [log_directory/'init.log'] + [log_directory/f'init.log.{i}' for i in range(9)]

    def inventory():
        files = {}
        for path in paths:
            try:
                info = path.lstat()
            except FileNotFoundError:
                continue
            require(stat.S_ISREG(info.st_mode), 'api_log_unavailable')
            files[path] = (info.st_dev, info.st_ino, info.st_size)
        return files

    files = inventory()
    require(list(files) == paths[:len(files)] and files, 'api_log_coverage_unconfirmed')
    require(sum(size for _, _, size in files.values()) <= 24*1024**2, 'api_log_byte_limit')
    events, first, last = [], None, None
    for path, (device, inode, size) in reversed(files.items()):
        with open(path,'rb',opener=lambda name,flags:os.open(name,flags | os.O_NOFOLLOW | os.O_NONBLOCK)) as stream:
            opened = os.fstat(stream.fileno())
            require(stat.S_ISREG(opened.st_mode) and (opened.st_dev, opened.st_ino) == (device, inode),
                    'api_log_coverage_unconfirmed')
            data = stream.read(size)
        require(len(data) == size and data.endswith(b'\n'), 'api_log_coverage_unconfirmed')
        for line in data.splitlines():
            row = json.loads(line)
            stamp = re.fullmatch(r'(20\d\d-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.(\d{1,9}))?Z', row['time'])
            require(stamp is not None, 'api_log_coverage_unconfirmed')
            ns = int(datetime.fromisoformat(stamp[1]+'+00:00').timestamp())*10**9 + int((stamp[2] or '').ljust(9,'0'))
            first = ns if first is None else min(first, ns)
            last = ns if last is None else max(last, ns)
            if not since <= ns <= until or row.get('component') != 'apiproxy':
                continue
            match = re.match(r'(>>|<<) POST (?:/v\d+\.\d+)?/containers/([a-f0-9]{64})/unpause(?:\s|$)', row['msg'])
            if match and core.compile_resume_request_identity(match[2])['request_signature_sha256'] == signature:
                require(len(events) < 24, 'api_archive_limit')
                events.append({'direction':match[1], 'timestamp_ns':ns, 'request_signature_sha256':signature})
    latest = inventory()
    require(list(latest) == list(files) and
            all(latest[p][:2] == files[p][:2] and latest[p][2] >= files[p][2] for p in files) and
            first is not None and first <= since < until <= last, 'api_log_coverage_unconfirmed')
    return events


def archived_compile_resume_candidate(report, log_directory=None, cleanup_anchor=None):
    failure = report.get('resource_guard_failure', {})
    if (report.get('case') not in CASE_LIMITS or report.get('result') != 'failed' or
            not report.get('cleanup', {}).get('zero_owned_resources') or
            (report.get('stage'), report.get('error_code'), failure.get('operation')) !=
            ('product_compile', 'resource_sample_deadline_exceeded', 'resume_owned_compile_container')):
        return None
    try:
        def resumes(sample):
            return [c for s in sample['steps'] if s['operation']=='resume_owned_compile_container' for c in s['commands']]

        command, = resumes(failure)
        signature = command.get('request_signature_sha256')
        entered, canceled = command['entered_monotonic_ns'], command['cancel_started_monotonic_ns']
        require(command.get('run_id') == report['run_id'] and command.get('phase') == 'product_compile' and
                command.get('record_complete') is True and command.get('result') == 'failed' and
                type(command.get('command_id')) is int and
                command.get('method') == 'POST' and command.get('api_family') == '/containers/{id}/unpause' and
                isinstance(signature,str) and re.fullmatch('[a-f0-9]{64}',signature) and
                type(entered) is int and type(canceled) is int and entered < canceled <= command['finished_monotonic_ns'],
                'command_not_correlatable')
        image = report['sampler_image_inspect']
        require(image.get('run_id') == report['run_id'] and image.get('record_complete') is True and
                image.get('result') == 'passed', 'host_clock_unavailable')
        anchor = image['host_clock_end']
        if cleanup_anchor is None:
            cleanup_anchor = core.host_clock_anchor()
        if log_directory is None:
            log_directory = Path.home()/'Library/Containers/com.docker.docker/Data/log/vm'
        since, until = volume_exec_event_window(command, anchor, cleanup_anchor)
        coarse = (anchor['monotonic_before_ns']+anchor['monotonic_after_ns'])//2-anchor['wall_utc_ns']
        pairs = core.pair_apiproxy_events(resume_apiproxy_events(log_directory,signature,since,until))
        target = {**command,'decision_monotonic_ns':canceled}
        candidates = [p for p in pairs if entered <= p['started_ns']+coarse < canceled]
        require(len(candidates) == 1 and candidates[0]['started_ns'] < candidates[0]['finished_ns'] and
                core.apiproxy_pair_fits(target,candidates[0],(coarse,coarse)), 'api_request_missing_or_ambiguous')
        pair = candidates[0]
        competitors = [c for sample in report['resource_samples'] for c in resumes(sample)
                       if c.get('request_signature_sha256') == signature]
        require(len(competitors) < 4104 and all(c['run_id'] == report['run_id'] and c['record_complete'] is True and
                c['command_id'] != command['command_id'] and not core.apiproxy_pair_fits(c,pair,(coarse,coarse))
                for c in competitors), 'api_request_reused')
        return {'status':'unique_candidate','command_id':command['command_id'],
                'proxy_log_span_ns':pair['finished_ns']-pair['started_ns']}
    except core.GateError as error:
        return {'status':'indeterminate','reason':str(error)}
    except (KeyError, TypeError, ValueError, AttributeError, OSError):
        return {'status':'indeterminate','reason':'api_archive_unavailable'}


def main():
    case=local_case(sys.argv[1:])
    life=Lifecycle()
    life.total_seconds,life.work_seconds=CASE_LIMITS[case]
    report={'schema':'addp.http-quality-local/v1','run_id':life.run_id,'result':'failed','hosted':False,'capacity':False,
            'formal_runner_t4':False,'assertions':{},'started_at':datetime.now(timezone.utc).isoformat(),
            'case':case,'limits':{'seconds':life.total_seconds,'work_seconds':life.work_seconds,'cpu':8,'memory_bytes':12*1024**3,'private_writable_stop_bytes':4*1024**3,'complete_scan_seconds':4}}
    target=Path(tempfile.gettempdir())/(life.run_id+'-report.json')
    try:
        validate_profile(os.environ,platform.machine(),platform.machine())
        endpoint=core.effective_docker_endpoint(os.environ,lambda name:json.loads(life.docker('context','inspect',*([name] if name else [])))[0]['Endpoints']['docker']['Host'])
        core.private_docker_client(life,endpoint)
        validate_profile(os.environ,platform.machine(),life.docker('info','--format','{{.Architecture}}').strip())
        env=build(life,compile_only=case=='compile-checkpoints')
        if case=='compile-checkpoints':
            report['compile_checkpoint_diagnostic']=compile_checkpoint_summary(life)
            report['not_accepted']=['product_images','APT','password_mfa','collection_api','pages','stop','logout','performance_fix']
        else:
            setup(life,env)
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
                ('python_dependencies','python_proof'),('resource_guard_failure','resource_failure'),
                ('sampler_image','sampler_image_proof'),('sampler_image_inspect','sampler_image_inspect'),
                ('go_cache_reclaim','cache_reclaim_receipt'),('compile_start_handoff','compile_start_handoff')]:
            if hasattr(life,attribute):report[key]=getattr(life,attribute)
        if hasattr(life,'request_budget'):
            report['request_attempt_counts']={'controlled':life.request_budget.controlled,'control':life.request_budget.control}
        report['resource_samples']=life.samples
        errors=life.cleanup()
        elapsed=time.monotonic()-life.started
        report.update(elapsed_seconds=elapsed,owned_list_commands=life.command_evidence.snapshot(),
            cleanup={'zero_owned_resources':not errors,'failures':errors})
        if errors or elapsed>life.total_seconds:report['result']='failed'
        if not errors:
            collect_volume_exec_timeout_events(life,report)
        with open(target,'x',opener=lambda p,f:os.open(p,f,0o600)) as stream:json.dump(report,stream,indent=2)
        try:
            attribution = archived_compile_resume_candidate(report)
            if attribution is not None:
                sidecar = target.with_name(life.run_id+'-resume-attribution.json')
                with open(sidecar,'x',opener=lambda p,f:os.open(p,f,0o600)) as stream:
                    json.dump({'run_id':life.run_id,**attribution},stream,indent=2)
        except (Exception, KeyboardInterrupt):
            print('Local formal Python HTTP: resume attribution unavailable',file=sys.stderr)
    print('Local formal Python HTTP: '+report['result']+'; case='+case+'; report='+str(target),flush=True)
    return 0 if report['result']=='passed' else 1

if __name__=='__main__':
    signal.signal(signal.SIGTERM,core.interrupted);signal.signal(signal.SIGINT,core.interrupted)
    raise SystemExit(main())
