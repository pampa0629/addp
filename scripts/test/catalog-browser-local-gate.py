#!/usr/bin/env python3
"""Disposable local Catalog browser lifecycle. Never dispatches API T4."""
from __future__ import annotations
import argparse
import base64
import fcntl
import importlib
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))
CORE = importlib.import_module('scripts.test.supermap-sgm-gate')
ISOLATE = importlib.import_module('scripts.utils.disposable-compose')
API = importlib.import_module('scripts.utils.online-api')
DOMAIN = importlib.import_module('scripts.test.catalog-domain-overview-online')
LABEL = 'com.addp.catalog-browser-local'
PREFIX = 'addp-catalog-browser-'
BACKENDS = ('system-backend', 'standard-backend', 'model-backend', 'catalog-backend', 'gateway')
FRONTENDS = ('console', 'catalog-frontend', 'standard-frontend', 'model-frontend', 'nginx')
INFRA = ('postgres', 'redis', 'minio', 'meilisearch')
CHECKS = ('minimum_identity', 'extended_identity', 'domain_drilldown', 'optional_permissions',
          'bound_unit_and_document', 'cross_domain_reference', 'readonly_and_account_switch')
require = CORE.require


def run_id(value):
    parsed = uuid.UUID(value)
    require(parsed.int and str(parsed) == value, 'invalid_run_id')
    return value


def work_path(value):
    return Path(tempfile.gettempdir()).resolve() / (PREFIX + run_id(value))


def safe_environment():
    return {key: value for key, value in os.environ.items() if key in {'PATH','HOME','TMPDIR','DOCKER_HOST','DOCKER_CONTEXT'}}


def validate_profile(environment, endpoint, machine, daemon):
    require(not any(environment.get(k) for k in ('GITHUB_ACTIONS','ADDP_ONLINE_HOSTED','ADDP_ONLINE_OWNER_MANAGED',
            'ADDP_ONLINE_HOST','ADDP_ONLINE_LOCAL_SGM','ADDP_EXECUTION_TRACE_LOCAL')), 'foreign_profile_forbidden')
    require(endpoint.startswith('unix://'), 'remote_docker_forbidden')
    aliases = {'arm64':'arm64','aarch64':'arm64','amd64':'amd64','x86_64':'amd64'}
    require(machine in aliases and aliases.get(daemon) == aliases[machine], 'native_docker_required')


def private_json(path, value):
    with open(path, 'x', opener=lambda p, f: os.open(p, f, 0o600)) as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2)


def check_private(path, directory=False):
    info = path.lstat()
    require(not path.is_symlink() and info.st_uid == os.geteuid() and
            (info.st_mode & 0o777) == (0o700 if directory else 0o600), 'private_path_required')
    require(path.is_dir() if directory else path.is_file(), 'private_path_type_invalid')


class Lifecycle(CORE.Lifecycle):
    def __init__(self, identity=None, minutes=60):
        self.uuid = run_id(identity) if identity else str(uuid.uuid4())
        self.run_id = PREFIX + self.uuid
        self.label = LABEL
        self.work = work_path(self.uuid)
        self.images = []
        self.trace = self.trace_file = None
        self.env = safe_environment()
        self.checkout = self.work/'source'
        self.minutes = minutes
        self.phase = 'preflight'
        self.report_path = self.work.with_suffix('.report.json')
        self.report = {'scope':'local-catalog-browser', 'formal_t4':False, 'run_id':self.uuid, 'result':'failed'}
        self.watcher = None

    def command(self, args, *, environment=None, timeout=180, data=None, cwd=None):
        # Private outputs can contain credentials; never print argv or raw output.
        with open(self.work/'commands.log', 'ab', opener=lambda p,f:os.open(p,f,0o600)) as log:
            process = subprocess.Popen(args, cwd=cwd or (self.checkout if self.checkout.exists() else ROOT),
                env=environment or self.env, stdin=subprocess.PIPE if data is not None else subprocess.DEVNULL,
                stdout=subprocess.PIPE, stderr=log, start_new_session=True)
            try:
                stdout,_ = process.communicate(data,timeout=timeout)
            except BaseException:
                os.killpg(process.pid,signal.SIGTERM)
                try: process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid,signal.SIGKILL);process.wait()
                raise
            log.write(stdout)
            require(process.returncode == 0, 'command_failed_'+Path(args[0]).name)
            return stdout.decode()

    def docker(self,*args,**kwargs):
        return self.command(['docker',*args],**kwargs)

    def compose(self,*args):
        return self.command(['docker','compose','--env-file','/dev/null','-f',str(self.work/'compose.json'),*args],timeout=360)

    def port(self, service, port):
        info=json.loads(self.docker('inspect',self.run_id+'-'+service))[0]
        require(info['Config']['Labels'].get(LABEL)==self.run_id,'foreign_container')
        bindings=info['NetworkSettings']['Ports'].get(f'{port}/tcp')
        require(len(bindings or [])==1 and bindings[0]['HostIp']=='127.0.0.1','loopback_port_required')
        return int(bindings[0]['HostPort'])

    def fixture(self, release=''):
        environment={**self.env,'POSTGRES_HOST':'127.0.0.1','POSTGRES_PORT':str(self.port('postgres',5432))}
        args=['go','run','./cmd/online-test-fixture','--suite','catalog-browser-local','--output',str(self.work/'identities.json')]
        if release:args+=['--catalog-local-release',release]
        self.command(args,environment=environment,cwd=self.checkout/'system/backend',timeout=180)

    def announce(self, phase):
        self.phase=phase
        print('Catalog local: '+phase,flush=True)


def baseline(life):
    ids=life.docker('ps','-aq','--filter','name=^addp-').split()
    if not ids:return {}
    rows=json.loads(life.docker('inspect',*ids))
    return {row['Name']:{'id':row['Id'],'started':row['State']['StartedAt']} for row in rows
            if row['Config']['Labels'].get(LABEL)!=life.run_id}


def defaults(life):
    text=life.command(['bash','-c','set -a; source .env.example; env -0'])
    values=dict(item.split('=',1) for item in text.split('\0') if '=' in item)
    for key in values:
        if key.endswith(('_PASSWORD','_SECRET','_KEY','_PEPPER')):values[key]=secrets.token_hex(32)
    for key in ('ENCRYPTION_KEY','IAM_MFA_ENCRYPTION_KEY','OAUTH_USER_CODE_PEPPER'):
        values[key]=base64.b64encode(secrets.token_bytes(32)).decode()
    values.update(life.env)
    values.update(ENV='test',POSTGRES_DB='addp_test',POSTGRES_USER='catalog_fixture',POSTGRES_HOST='127.0.0.1',
        POSTGRES_PASSWORD=secrets.token_hex(24),COMPOSE_DISABLE_ENV_FILE='1',ADDP_CATALOG_BROWSER_LOCAL='1',
        ADDP_CATALOG_BROWSER_RUN_ID=life.uuid,ADDP_CATALOG_BROWSER_MINUTES=str(life.minutes),
        ADDP_OBSERVABILITY_LOGS_ENABLED='false',ADDP_OBSERVABILITY_METRICS_ENABLED='false',
        ADDP_PUBLIC_ORIGIN=life.origin,ALLOWED_ORIGINS=life.origin,NGINX_PORT=str(life.public_port),
        NGINX_BIND_HOST='127.0.0.1',CATALOG_SOURCE_SYNC_INTERVAL='2s',
        NO_PROXY='127.0.0.1,localhost,.localhost',no_proxy='127.0.0.1,localhost,.localhost')
    return values


def deployment(life, images):
    services={}
    for filename,names in (('docker-compose.infra.yml',INFRA),('docker-compose.yml',BACKENDS+FRONTENDS)):
        document=json.loads(life.command(['docker','compose','--env-file','/dev/null','-f',filename,'config','--format','json']))
        services.update({name:document['services'][name] for name in names})
    config=ISOLATE.isolate_compose({'services':services},services,life.run_id,images,label=LABEL)
    config['services']['postgres']['command']=[*services['postgres']['command'],'-c','cluster_name='+life.run_id]
    config['services']['nginx']['ports'][0]['published']=str(life.public_port)
    for name in INFRA:
        reference=config['services'][name]['image']
        if not life.docker('image','ls','-q',reference).strip():life.docker('pull',reference,timeout=900)
        arch=life.docker('image','inspect',reference,'--format','{{.Architecture}}').strip()
        require(arch==('arm64' if platform.machine() in ('arm64','aarch64') else 'amd64'),'infra_architecture_mismatch')
    for service in config['services'].values():
        service['cpus']='1.0';service['mem_limit']='768m'
    private_json(life.work/'compose.json',config)
    return config


def wait_url(url, timeout=120):
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
    deadline=time.monotonic()+timeout
    while time.monotonic()<deadline:
        try:
            with opener.open(url,timeout=3) as response:
                if response.status==200:return
        except OSError:pass
        time.sleep(1)
    raise CORE.GateError('service_readiness_timeout')


def prepare(life):
    life.work.mkdir(mode=0o700)
    endpoint=json.loads(life.docker('context','inspect',*([life.env['DOCKER_CONTEXT']] if life.env.get('DOCKER_CONTEXT') else [])))[0]['Endpoints']['docker']['Host']
    if not life.env.get('DOCKER_CONTEXT'):endpoint=life.env.get('DOCKER_HOST',endpoint)
    daemon=life.docker('info','--format','{{.Architecture}}').strip()
    validate_profile(os.environ,endpoint,platform.machine(),daemon)
    life.env.pop('DOCKER_CONTEXT',None);life.env['DOCKER_HOST']=endpoint
    require(platform.system() in ('Darwin','Linux'),'local_host_required')
    initial=baseline(life)
    commit=life.command(['git','rev-parse','HEAD'],cwd=ROOT).strip()
    for relative in ('scripts/test/catalog-browser-local-gate.py','scripts/test/catalog-browser-local-fixture.py','system/backend/cmd/online-test-fixture/catalog_browser_local.go'):
        committed=life.command(['git','show',commit+':'+relative],cwd=ROOT)
        require((ROOT/relative).read_text()==committed,'commit_catalog_inputs_before_acceptance')
    life.report['commit']=commit
    state={'run_id':life.uuid,'parent_pid':os.getpid(),'deadline':time.time()+10800,'minutes':life.minutes,
           'initial':initial,'docker_host':endpoint,'commit':commit}
    private_json(life.work/'state.json',state)
    # Independent run-scoped watcher, not a persistent service or scheduled task.
    life.watcher=subprocess.Popen([sys.executable,str(Path(__file__).resolve()),'--watch',life.uuid],
        env=life.env,stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True)
    life.announce('source_snapshot')
    life.command(['bash','-c','umask 022; exec "$@"','_','git','clone','--local','--no-hardlinks','--single-branch','--branch','main',str(ROOT),str(life.checkout)])
    require(life.command(['git','rev-parse','HEAD']).strip()==commit,'source_snapshot_changed')
    # Pick one public port before configuring the exact allowed origin. Compose
    # fails if another process takes it; never silently redirects to another app.
    with socket.socket() as reservation:
        reservation.bind(('127.0.0.1',0));life.public_port=reservation.getsockname()[1]
    life.origin=f'http://catalog-{life.uuid}.localhost:{life.public_port}'
    life.env=defaults(life)
    private_json(life.work/'environment.json',life.env)
    docker_binary=shutil.which('docker',path=life.env['PATH'])
    shim=life.work/'tools';shim.mkdir(mode=0o700)
    wrapper=shim/'docker'
    wrapper.write_text('#!/usr/bin/env python3\nimport os,sys\na=sys.argv[1:]\n'
        'if a and a[0]=="run":a=["run","--label",os.environ["CATALOG_BROWSER_LABEL"],"--cpus","2","--memory","3g",*a[1:]]\n'
        'os.execv(os.environ["CATALOG_BROWSER_DOCKER"],[os.environ["CATALOG_BROWSER_DOCKER"],*a])\n')
    wrapper.chmod(0o700)
    life.env.update(PATH=str(shim)+os.pathsep+life.env['PATH'],CATALOG_BROWSER_DOCKER=docker_binary,CATALOG_BROWSER_LABEL=LABEL+'='+life.run_id)
    # Cache only immutable downloaded modules; build products remain disposable.
    module_cache=life.command(['go','env','GOMODCACHE']).strip()
    life.env['ADDP_BUILD_GOMODCACHE']=module_cache
    life.announce('build_product')
    arch='arm64' if daemon in ('arm64','aarch64') else 'amd64'
    # Standard build entry; all output and writable caches are in the snapshot.
    life.command(['make','build','BUILD_ARGS=--arch '+arch+' --services '+','.join(BACKENDS)],timeout=2400)
    references={name:'localhost:5001/addp-'+name+':'+life.run_id for name in BACKENDS+FRONTENDS}
    life.images=list(references.values())
    private_json(life.work/'images.json',life.images)
    life.command(['make','build-images','IMAGE_BUILD_ARGS=--verify --jobs 1 --services '+','.join(BACKENDS+FRONTENDS)+' --tag '+life.run_id],timeout=2400)
    images={name:life.docker('image','inspect',ref,'--format','{{.Id}}').strip() for name,ref in references.items()}
    life.report['images']=images
    life.announce('start_owned_deployment')
    deployment(life,images)
    life.compose('up','-d','--wait',*INFRA)
    # Same initialization SQL as Infra startup, delivered only to the owned DB.
    life.command(['docker','compose','--env-file','/dev/null','-f',str(life.work/'compose.json'),'exec','-T','postgres',
        'psql','-v','ON_ERROR_STOP=1','-U',life.env['POSTGRES_USER'],'-d','addp_test'],
        data=(life.checkout/'scripts/infra/init-postgresql.sql').read_bytes())
    life.compose('up','-d','--wait','system-backend')
    for name in BACKENDS[1:]:life.compose('up','-d','--wait',name)
    life.compose('up','-d','--wait',*FRONTENDS)
    require(life.port('nginx',80)==life.public_port,'public_origin_changed')
    gateway='http://127.0.0.1:'+str(life.port('gateway',8000))
    wait_url(gateway+'/health/ready')
    wait_url('http://127.0.0.1:'+str(life.public_port)+'/health')
    life.announce('prepare_system_identities')
    life.fixture()
    credentials=json.loads((life.work/'identities.json').read_text())
    life.announce('prepare_owner_samples')
    life.command([sys.executable,'scripts/test/catalog-browser-local-fixture.py','--gateway',gateway,
        '--identities',str(life.work/'identities.json'),'--samples',str(life.work/'samples.json')],timeout=180)
    samples=json.loads((life.work/'samples.json').read_text())
    life.fixture('preparer')
    ready={'run_id':life.uuid,'origin':life.origin,'commit':commit,'samples':samples,
           'users':{name:{k:v for k,v in user.items() if k!='password'} for name,user in credentials['users'].items() if name!='preparer'},
           'deadline':min(time.time()+life.minutes*60,__import__('datetime').datetime.fromisoformat(credentials['expires_at'].replace('Z','+00:00')).timestamp())}
    private_json(life.work/'ready.json',ready)
    life.report['ready']=ready
    life.announce('ready '+life.origin+' run_id='+life.uuid)
    return ready


def validate_evidence(evidence, ready):
    require(set(evidence)=={'run_id','commit','browser','checks'},'unexpected_evidence_fields')
    require(evidence.get('run_id')==ready['run_id'] and evidence.get('commit')==ready['commit'] and evidence.get('browser')=='Opera','browser_evidence_identity_mismatch')
    require(set(evidence.get('checks',{}))==set(CHECKS) and all(evidence['checks'][key] is True for key in CHECKS),'browser_assertions_incomplete')
    return evidence


def cleanup(identity, report=None):
    life=Lifecycle(identity)
    if not life.work.exists():return []
    check_private(life.work,True)
    # Serialize parent, watchdog and explicit recovery. A deleted work directory
    # means a previous cleaner has already finished; no global cleanup fallback.
    try:lock=open(life.work/'cleanup.lock','a')
    except FileNotFoundError:return []
    with lock:
        fcntl.flock(lock,fcntl.LOCK_EX)
        if not life.work.exists():return []
        state=json.loads((life.work/'state.json').read_text())
        require(state['run_id']==identity,'foreign_cleanup_manifest')
        life.env['DOCKER_HOST']=state['docker_host'];life.env.pop('DOCKER_CONTEXT',None)
        if (life.work/'environment.json').exists():life.env=json.loads((life.work/'environment.json').read_text())
        if (life.work/'images.json').exists():life.images=json.loads((life.work/'images.json').read_text())
        result=report or {'scope':'local-catalog-browser','formal_t4':False,'run_id':identity,'commit':state['commit'],'result':'failed','reason':'interrupted_or_expired'}
        try:
            if (life.work/'identities.json').exists():
                life.fixture('all')
                result['membership_release']='passed'
            else:result['membership_release']='not_created'
        except Exception:result['membership_release']='failed'
        try:
            current=baseline(life)
            result['daily_containers_unchanged']=all(current.get(k)==v for k,v in state['initial'].items())
        except Exception:result['daily_containers_unchanged']=None
        # Capture only safe error type/category; private command log is destroyed.
        errors=super(Lifecycle,life).cleanup(preserve_on_failure=True)
        if errors:
            # Preserve only non-secret ownership metadata for exact recovery.
            for path in life.work.iterdir():
                if path.name in {'state.json','images.json','cleanup.lock'}:continue
                if path.is_dir() and not path.is_symlink():shutil.rmtree(path)
                else:path.unlink()
        result['cleanup_failures']=errors
        result['zero_residue']=not errors
        if errors or not result['daily_containers_unchanged']:result['result']='failed'
        with open(life.report_path,'w',opener=lambda p,f:os.open(p,f,0o600)) as stream:json.dump(result,stream,ensure_ascii=False,indent=2)
        return errors


def watch(identity):
    work=work_path(identity)
    while work.exists():
        state=json.loads((work/'state.json').read_text())
        deadline=state['deadline']
        if (work/'ready.json').exists():deadline=min(deadline,json.loads((work/'ready.json').read_text())['deadline'])
        try:os.kill(state['parent_pid'],0);alive=True
        except ProcessLookupError:alive=False
        if not alive or time.time()>=deadline:
            if alive:os.kill(state['parent_pid'],signal.SIGTERM)
            time.sleep(3)
            cleanup(identity)
            return
        time.sleep(2)


def main(argv=None):
    parser=argparse.ArgumentParser()
    parser.add_argument('--minutes',type=int,default=60)
    parser.add_argument('--watch');parser.add_argument('--cleanup');parser.add_argument('--finish');parser.add_argument('--evidence')
    args=parser.parse_args(argv)
    if args.watch:watch(run_id(args.watch));return 0
    if args.cleanup:return 1 if cleanup(run_id(args.cleanup)) else 0
    if args.finish:
        work=work_path(args.finish);check_private(work,True)
        evidence=json.loads(Path(args.evidence).read_text())
        validate_evidence(evidence,json.loads((work/'ready.json').read_text()))
        private_json(work/'finished.json',evidence);return 0
    require(1<=args.minutes<=120,'lease_must_be_1_to_120_minutes')
    life=Lifecycle(minutes=args.minutes)
    try:
        ready=prepare(life)
        while time.time()<ready['deadline']:
            if (life.work/'finished.json').exists():
                evidence=validate_evidence(json.loads((life.work/'finished.json').read_text()),ready)
                gateway='http://127.0.0.1:'+str(life.port('gateway',8000))
                life.command([sys.executable,'scripts/test/catalog-browser-local-fixture.py','--gateway',gateway,
                    '--identities',str(life.work/'identities.json'),'--samples',str(life.work/'samples.json'),'--verify'],timeout=30)
                life.report.update(result='passed',browser=evidence,source_unchanged=True)
                break
            time.sleep(1)
        else:raise CORE.GateError('browser_lease_expired')
    except BaseException as error:
        life.report.update(result='failed',phase=life.phase,error_type=type(error).__name__)
        print('Catalog local failed in '+life.phase+' ('+type(error).__name__+'); private output withheld',flush=True)
    finally:
        if (life.work/'state.json').exists():cleanup(life.uuid,life.report)
        elif life.work.exists():shutil.rmtree(life.work)
        if life.watcher is not None:life.watcher.terminate();life.watcher.wait(timeout=5)
    print('Catalog local report: '+str(life.report_path),flush=True)
    result=json.loads(life.report_path.read_text()) if life.report_path.exists() else life.report
    return 0 if result['result']=='passed' else 1


if __name__=='__main__':
    signal.signal(signal.SIGTERM,lambda *_:(_ for _ in ()).throw(KeyboardInterrupt()))
    sys.exit(main())
