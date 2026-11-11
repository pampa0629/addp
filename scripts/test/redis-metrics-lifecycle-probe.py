#!/usr/bin/env python3
"""Owned Redis persistence, real mTLS source and failure/stop/restart proof."""
import importlib.util
import json
import os
from pathlib import Path
import re
import socket
import ssl
import subprocess
import tempfile
import time
import traceback
import urllib.error
import urllib.request
from unittest import mock

ROOT=Path(__file__).resolve().parents[2]
spec=importlib.util.spec_from_file_location('redis_metrics_probe',ROOT/'scripts/infra/redis-metrics.py')
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
PROJECT=os.environ.get('ADDP_REDIS_T2_PROJECT','')
if not PROJECT.startswith('addp-redis-t2-') or os.environ.get('ADDP_REDIS_OBSERVATION_T2')!='1':
    raise SystemExit('Redis lifecycle proof requires the standard isolated gate')
COMPOSE=['docker','compose','--profile','redis-observation','-p',PROJECT,'-f',str(ROOT/'scripts/test/docker-compose.redis-t2.yml')]


tls_spec=importlib.util.spec_from_file_location('redis_tls_fixture',ROOT/'scripts/test/metrics-source-fixture.py')
tls_fixture=importlib.util.module_from_spec(tls_spec);tls_spec.loader.exec_module(tls_fixture)
command,certificates,context,request=tls_fixture.command,tls_fixture.certificates,tls_fixture.context,tls_fixture.request


def ready(port,ctx):
    deadline=time.monotonic()+15
    diagnosis='no response'
    while True:
        try:
            status,body=request(port,ctx)
            if status==200 and 'addp_redis_memory_allocated_bytes ' in body:return body
            diagnosis='HTTP '+str(status)
        except (OSError,urllib.error.URLError) as error:
            cause=getattr(error,'reason',error)
            diagnosis=type(cause).__name__
            if isinstance(cause,ssl.SSLError):diagnosis+=':'+str(cause.reason)
            if isinstance(cause,ssl.SSLCertVerificationError):diagnosis+=':'+str(cause.verify_code)
        if time.monotonic()>=deadline:raise AssertionError('owned Redis source did not become readable ['+diagnosis+']')
        time.sleep(.1)


def assert_clean(docker,manager):
    for args in (['ps','-aq'],['network','ls','-q']):
        if docker.run([*args,'--filter','label=com.docker.compose.project='+manager.project]).strip():raise AssertionError('owned Redis source residue')
    if docker.run(['volume','ls','-q','--filter','name=^'+manager.config_volume+'$']).strip():raise AssertionError('owned Redis source volume residue')


def probe(scope,docker,root,tls):
    service='redis' if scope=='business' else 'redis-infra'
    with socket.socket() as sock:sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
    state=root/scope
    factory=lambda:module.Database(docker,scope,PROJECT,ROOT/'scripts/test',os.environ['BUSINESS_REDIS_ADMIN_PASSWORD'],service=service)
    manager=module.Lifecycle(docker,scope,state,factory,project=PROJECT+'-metrics-'+scope)
    env={'ADDP_REDIS_METRICS_ENABLED':'true','ADDP_REDIS_METRICS_LISTEN':f'127.0.0.1:{port}','ADDP_REDIS_METRICS_TLS_DIR':str(tls)}
    ctx=context(root)
    def discovered():
        database=factory()
        current={database.identity:('redis',{'container_id':database.identity,'started_at':database.facts['started_at'],'status':'running'})}
        # Only substitute the disposable namespace; the production collector
        # reads actual Docker facts and uses its unchanged validation path.
        with mock.patch.object(module.inventory,'redis_metrics_project',return_value=manager.project):
            return module.inventory.redis_metrics(lambda *args:docker.run(list(args)),scope,current)
    initial=factory()
    if initial.cli(['SET','addp:observation:preserve','retained'])!='OK':raise AssertionError('disposable marker failed')
    try:
        manager.run('up',env)
        saved=module.decode(module.private.read(state/'owner.json',0o600)[0]);password=saved['password']
        peer_scope='business' if scope=='infra' else 'infra'
        peer_state=root/(scope+'-peer-build');peer_state.mkdir()
        peer=module.Lifecycle(docker,peer_scope,peer_state,lambda:None,project=manager.project+'-peer')
        assert peer.image()==saved['image'], 'peer build replaced installed image identity'
        manager.container_source().installed_image(saved['image'])
        body=ready(port,ctx)
        if discovered()!={'state':'available','endpoint':f'https://127.0.0.1:{port}/metrics','container_id':factory().identity,'started_at':factory().facts['started_at']}:raise AssertionError('actual Redis source discovery failed')
        if password in body or os.environ['BUSINESS_REDIS_ADMIN_PASSWORD'] in body or 'run_id' in body:raise AssertionError('secret or raw identity escaped metrics')
        epoch=re.search(r'counter_epoch="([0-9a-f]{64})"',body)
        if not epoch:raise AssertionError('counter runtime identity absent')
        for bad in (context(root,client=False),context(root,wrong=True)):
            try: request(port,bad)
            except (OSError,urllib.error.URLError):pass
            else:raise AssertionError('unauthenticated/wrong-purpose client accepted')
        for path,method in [('/health','GET'),('/metrics?target=redis','GET'),('/metrics','POST')]:
            status,value=request(port,ctx,path,method)
            if status!=404 or 'addp_redis_' in value:raise AssertionError('extra source endpoint opened')
        ids=docker.run(['ps','-q','--filter','label=com.docker.compose.project='+manager.project]).split()
        if len(ids)!=1:raise AssertionError('unexpected long-lived source process')
        detail=module.decode(docker.run(['inspect','--format','{{json .}}',ids[0]]))
        if (detail['Config'].get('Env') or []) and os.environ['BUSINESS_REDIS_ADMIN_PASSWORD'] in json.dumps(detail['Config']['Env']):raise AssertionError('admin secret entered source environment')
        if not detail['HostConfig']['ReadonlyRootfs'] or detail['HostConfig']['Privileged'] or detail['Config']['User']!='65532:65532':raise AssertionError('source privilege drift')
        if any(m['Destination']=='/data' or m.get('Type')=='bind' for m in detail['Mounts']):raise AssertionError('source received Redis data or host directory')
        before=(state/'owner.json').read_bytes(); record=factory().record(); account=factory().account()
        with mock.patch.object(manager,'transition',side_effect=AssertionError('recovery attempted account mutation')), mock.patch.object(manager,'image',side_effect=AssertionError('recovery attempted image build')):
            if manager.run('recover',env)!='healthy; source unchanged':raise AssertionError('healthy source was not reused')
            if docker.run(['ps','-q','--filter','label=com.docker.compose.project='+manager.project]).split()!=ids:raise AssertionError('healthy source was rebuilt')
            docker.run(['stop',ids[0]])
            if 'source recovered' not in manager.run('recover',env):raise AssertionError('stopped source did not recover')
            ready(port,ctx)
            current_source=docker.run(['ps','-q','--filter','label=com.docker.compose.project='+manager.project]).strip()
            docker.run(['rm','-f',current_source])
            if 'source recovered' not in manager.run('recover',env):raise AssertionError('missing source did not recover')
            ready(port,ctx)
        if (state/'owner.json').read_bytes()!=before or factory().record()!=record or factory().account()!=account:raise AssertionError('source recovery changed private state or Redis ACL')
        current_source=docker.run(['ps','-q','--filter','label=com.docker.compose.project='+manager.project]).strip()
        command(COMPOSE+['stop',service])
        try:manager.run('recover',env)
        except ValueError:pass
        else:raise AssertionError('recovery started a stopped Redis database')
        if docker.run(['ps','-q','--filter','label=com.docker.compose.project='+manager.project]).strip()!=current_source:raise AssertionError('rejected recovery changed source')
        command(COMPOSE+['restart',service])
        current=factory();deadline=time.monotonic()+15
        while True:
            try:
                if current.cli(['PING'])=='PONG':break
            except ValueError:pass
            if time.monotonic()>=deadline:raise AssertionError('owned Redis did not restart')
            time.sleep(.1)
        if discovered()!={'state':'unavailable'}:raise AssertionError('old source remained eligible after database restart')
        after=ready(port,ctx)
        if epoch.group(1) in after:raise AssertionError('Redis restart mixed counter generations')
        if current.cli(['GET','addp:observation:preserve'])!='retained':raise AssertionError('Redis restart lost data')
        account=current.account();p=manager.plan(saved['image'],saved)
        if not module.matches(account,p['password_hash'],True):raise AssertionError('observer ACL did not persist across restart')
        if 'source recovered' not in manager.run('recover',env):raise AssertionError('same-container restart did not restore source')
        if module.decode(module.private.read(state/'owner.json',0o600)[0])['password']!=password:raise AssertionError('restart rotated credential')
        ready(port,ctx)
        if discovered().get('state')!='available':raise AssertionError('reapplied source did not restore discovery')
        manager.run('down',env);assert_clean(docker,manager)
        if discovered()!={'state':'not_deployed'}:raise AssertionError('stopped source remained discovered')
        if not module.matches(factory().account(),p['password_hash'],False):raise AssertionError('observer not disabled')
        command(COMPOSE+['restart',service]);current=factory()
        deadline=time.monotonic()+15
        while True:
            try:
                if current.cli(['PING'])=='PONG':break
            except ValueError:pass
            if time.monotonic()>=deadline:raise AssertionError('owned stopped Redis did not restart')
            time.sleep(.1)
        if not module.matches(current.account(),p['password_hash'],False):raise AssertionError('disabled ACL did not persist')
        before=(state/'owner.json').read_bytes(); record=current.record()
        try:manager.run('recover',env)
        except ValueError:pass
        else:raise AssertionError('recovery enabled a disabled observer')
        assert_clean(docker,manager)
        if (state/'owner.json').read_bytes()!=before or factory().record()!=record:raise AssertionError('rejected recovery changed disabled state')
        manager.run('up',env);ready(port,ctx)
        before=(state/'owner.json').read_bytes()
        source_ids=docker.run(['ps','-aq',*module.sources.group.filters(manager.project,{manager.container_source().service})]).split()
        broken=dict(env,ADDP_REDIS_METRICS_TLS_DIR=str(root/'missing'))
        try:manager.run('up',broken)
        except (ValueError,OSError):pass
        else:raise AssertionError('invalid TLS material accepted')
        assert (state/'owner.json').read_bytes()==before, 'preparation failure changed account'
        assert docker.run(['ps','-aq',*module.sources.group.filters(manager.project,{manager.container_source().service})]).split()==source_ids, 'preparation failure replaced source'
        ready(port,ctx)
        if factory().cli(['GET','addp:observation:preserve'])!='retained':raise AssertionError('optional failure changed business data')
        manager.run('down',env);assert_clean(docker,manager)
        # A corrupt optional observer record must never prevent base Redis auth.
        database=factory();disabled=database.record()
        database.replace_record(disabled,'invalid observer record\n')
        command(COMPOSE+['restart',service]);database=factory()
        deadline=time.monotonic()+15
        while True:
            try:
                if database.cli(['PING'])=='PONG':break
            except ValueError:pass
            if time.monotonic()>=deadline:raise AssertionError('corrupt observer blocked base Redis auth')
            time.sleep(.1)
        if database.account() is not None or database.cli(['GET','addp:observation:preserve'])!='retained':raise AssertionError('corrupt observer changed base account or business data')
        try:manager.run('up',env)
        except ValueError:pass
        else:raise AssertionError('corrupt observer was adopted')
        assert_clean(docker,manager)
        database.replace_record('invalid observer record\n',disabled)
        command(COMPOSE+['restart',service])
        deadline=time.monotonic()+15
        while True:
            try:
                if module.matches(factory().account(),p['password_hash'],False):break
            except ValueError:pass
            if time.monotonic()>=deadline:raise AssertionError('restored owned record did not reload')
            time.sleep(.1)
        manager.run('down',env)
        print('Redis lifecycle T2 '+scope+': read-only recovery/reuse, stopped database/observer rejection, mTLS, discovery, restart persistence, runtime generation, stop and zero-residue cleanup passed',flush=True)
    finally:
        try:manager.run('down',env)
        finally:manager.stop_sources();assert_clean(docker,manager)


def main():
    docker=module.deployment.Docker(os.environ)
    with tempfile.TemporaryDirectory(prefix='addp-redis-lifecycle-t2-') as temporary:
        root=Path(temporary).resolve();tls=certificates(root)
        for scope in ('infra','business'):probe(scope,docker,root,tls)
    print('Redis lifecycle T2: zero owned sources and configuration volumes; private temporary files removed')


if __name__=='__main__':
    try:main()
    except (AssertionError,ValueError,OSError,KeyError,subprocess.SubprocessError) as error:
        # Raw command/TLS/Redis data and credentials are never printed.
        frames=traceback.extract_tb(error.__traceback__)
        stage=' > '.join(Path(frame.filename).name+':'+str(frame.lineno)+':'+frame.name for frame in frames[-4:])
        reason=str(error) if isinstance(error,(module.Error,module.private.PrivateStateError,AssertionError)) else type(error).__name__
        raise SystemExit('Redis lifecycle T2 failed ['+stage+']: '+reason)
