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

ROOT=Path(__file__).resolve().parents[2]
spec=importlib.util.spec_from_file_location('redis_metrics_probe',ROOT/'scripts/infra/redis-metrics.py')
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
PROJECT=os.environ.get('ADDP_REDIS_T2_PROJECT','')
if not PROJECT.startswith('addp-redis-t2-') or os.environ.get('ADDP_REDIS_OBSERVATION_T2')!='1':
    raise SystemExit('Redis lifecycle proof requires the standard isolated gate')
COMPOSE=['docker','compose','--profile','redis-observation','-p',PROJECT,'-f',str(ROOT/'scripts/test/docker-compose.redis-t2.yml')]


def command(args,**kwargs):
    return subprocess.run(args,check=True,capture_output=True,timeout=30,**kwargs)


def certificates(root):
    config=root/'request.cnf';config.write_text('[req]\nprompt=no\ndistinguished_name=dn\n[dn]\nCN=redis-observation-t2\n')
    environment=dict(os.environ,OPENSSL_CONF=str(config))
    def openssl(*args):command(['openssl',*args],env=environment)
    openssl('req','-x509','-newkey','rsa:2048','-nodes','-days','1','-config',str(config),'-subj','/CN=redis-source-t2-ca','-addext','basicConstraints=critical,CA:TRUE','-addext','keyUsage=critical,keyCertSign,cRLSign','-addext','subjectKeyIdentifier=hash','-keyout',str(root/'ca.key'),'-out',str(root/'ca.crt'))
    for name,purpose in [('server','serverAuth'),('client','clientAuth')]:
        openssl('req','-newkey','rsa:2048','-nodes','-config',str(config),'-subj','/CN=redis-source-t2-'+name,'-keyout',str(root/(name+'.key')),'-out',str(root/(name+'.csr')))
        extension=root/(name+'.ext');extension.write_text('basicConstraints=critical,CA:FALSE\nauthorityKeyIdentifier=keyid:always\nsubjectKeyIdentifier=hash\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage='+purpose+'\nsubjectAltName=IP:127.0.0.1\n')
        openssl('x509','-req','-in',str(root/(name+'.csr')),'-CA',str(root/'ca.crt'),'-CAkey',str(root/'ca.key'),'-CAcreateserial','-days','1','-extfile',str(extension),'-out',str(root/(name+'.crt')))
    for name in ('server.key','client.key','ca.key'): (root/name).chmod(0o600)
    tls=root/'source-tls';tls.mkdir(mode=0o700)
    for name in ('ca.crt','server.crt','server.key'):
        (tls/name).write_bytes((root/name).read_bytes());(tls/name).chmod(0o600)
    # Child verification uses the same isolated minimal configuration.
    os.environ['OPENSSL_CONF']=str(config)
    return tls


def context(root,client=True,wrong=False):
    ctx=ssl.create_default_context(cafile=str(root/'ca.crt'))
    if client:
        name='server' if wrong else 'client'
        ctx.load_cert_chain(str(root/(name+'.crt')),str(root/(name+'.key')))
    return ctx


def request(port,ctx,path='/metrics',method='GET'):
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPSHandler(context=ctx))
    try:
        with opener.open(urllib.request.Request(f'https://127.0.0.1:{port}'+path,method=method),timeout=5) as response:
            return response.status,response.read(32769).decode()
    except urllib.error.HTTPError as error:return error.code,error.read(32769).decode()


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
    initial=factory()
    if initial.cli(['SET','addp:observation:preserve','retained'])!='OK':raise AssertionError('disposable marker failed')
    try:
        manager.run('up',env)
        saved=module.decode(module.private.read(state/'owner.json',0o600)[0]);password=saved['password']
        body=ready(port,ctx)
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
        command(COMPOSE+['restart',service])
        current=factory();deadline=time.monotonic()+15
        while True:
            try:
                if current.cli(['PING'])=='PONG':break
            except ValueError:pass
            if time.monotonic()>=deadline:raise AssertionError('owned Redis did not restart')
            time.sleep(.1)
        after=ready(port,ctx)
        if epoch.group(1) in after:raise AssertionError('Redis restart mixed counter generations')
        if current.cli(['GET','addp:observation:preserve'])!='retained':raise AssertionError('Redis restart lost data')
        account=current.account();p=manager.plan(saved['image'],saved)
        if not module.matches(account,p['password_hash'],True):raise AssertionError('observer ACL did not persist across restart')
        manager.run('up',env)
        if module.decode(module.private.read(state/'owner.json',0o600)[0])['password']!=password:raise AssertionError('restart rotated credential')
        ready(port,ctx)
        manager.run('down',env);assert_clean(docker,manager)
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
        manager.run('up',env);ready(port,ctx)
        broken=dict(env,ADDP_REDIS_METRICS_TLS_DIR=str(root/'missing'))
        try:manager.run('up',broken)
        except (ValueError,OSError):pass
        else:raise AssertionError('invalid TLS material accepted')
        assert_clean(docker,manager)
        if not module.matches(factory().account(),p['password_hash'],False):raise AssertionError('failed startup left account enabled')
        if factory().cli(['GET','addp:observation:preserve'])!='retained':raise AssertionError('optional failure changed business data')
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
        print('Redis lifecycle T2 '+scope+': mTLS, restart persistence, fixed runtime generation, stop, corrupt-record isolation and failure cleanup passed',flush=True)
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
