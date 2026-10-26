"""Gate-owned Python SDK producer. No System, model or business credentials."""
import asyncio
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import threading
import time
import urllib.request
from contextlib import asynccontextmanager

STARTUP_STAGES=('copy','dependencies','imports','configuration','metrics','http_ready','identity','running')
STARTUP_ERRORS=('none','dependency_failed','dependency_conflict','distribution_missing','permission_denied','no_space',
                'module_missing','import_failed','file_missing','startup_failed','cancelled','log_over_budget','diagnostic_write_failed')


def startup_record(stage,status,error='none',exit_code=None,log_bytes=0):
    return {'stage':stage,'status':status,'error':error,'exit_code':exit_code,'log_bytes':log_bytes,'log_over_budget':log_bytes>65536}


def read_startup(path):
    try:
        with path.open('rb') as stream:raw=stream.read(4097)
        if len(raw)>4096:raise ValueError()
        value=json.loads(raw)
        if not isinstance(value,dict) or set(value)!=set(startup_record('copy','starting')):raise ValueError()
        if value['stage'] not in STARTUP_STAGES or value['status'] not in ('starting','passed','failed') or value['error'] not in STARTUP_ERRORS:raise ValueError()
        code=value['exit_code'];size=value['log_bytes']
        if code is not None and (type(code) is not int or not -255<=code<=255):raise ValueError()
        if type(size) is not int or not 0<=size<=256*1024**2 or type(value['log_over_budget']) is not bool or value['log_over_budget']!=(size>65536):raise ValueError()
        return value
    except (OSError,ValueError,UnicodeError):
        return {'stage':'unknown','status':'unavailable'}


def install_diagnostic(path):
    with path.open('rb') as stream:raw=stream.read(65537)
    size=path.stat().st_size
    if len(raw)>65536:return 'log_over_budget',size
    if b'ResolutionImpossible' in raw:return 'dependency_conflict',size
    if b'No matching distribution found' in raw:return 'distribution_missing',size
    if b'Permission denied' in raw:return 'permission_denied',size
    if b'No space left on device' in raw:return 'no_space',size
    return 'dependency_failed',size


def exception_kind(error):
    if isinstance(error,ModuleNotFoundError):return 'module_missing'
    if isinstance(error,ImportError):return 'import_failed'
    if isinstance(error,PermissionError):return 'permission_denied'
    if isinstance(error,FileNotFoundError):return 'file_missing'
    if isinstance(error,(KeyboardInterrupt,SystemExit)):return 'cancelled'
    return 'startup_failed'


def stop_runtime(server,thread,source,primary):
    failed=False
    try:
        server.should_exit=True;thread.join(timeout=3)
        failed=thread.is_alive()
    except Exception:failed=True
    try:source.close()
    except Exception:failed=True
    if failed and not primary:raise RuntimeError('python_http_shutdown_failed')


class HTTPFixtureTraffic:
    """Two finite allocations; a slow preparation cannot spend recovery slots."""
    def __init__(self, index, started):
        self.limit = {1:45, 2:15}[index]
        self.interval = {1:4, 2:12}[index]
        self.started = self.next_request = started
        self.phase = 'failure'
        self.attempts = {'failure':0, 'healthy':0}
        self.last_ms = {'failure':None, 'healthy':None}

    def take(self, now, healthy):
        if healthy:
            self.phase = 'healthy'
        phase = self.phase
        if self.attempts[phase] >= self.limit or now < self.next_request:
            return None
        path = ('/items/fixture-id','/slow','/failure','/missing/fixture-id')[sum(self.attempts.values())%4]
        self.attempts[phase] += 1
        self.last_ms[phase] = int((now-self.started)*1000)
        self.next_request = now + self.interval/(2 if phase == 'healthy' else 1)
        return path

    def snapshot(self):
        return {'phase':self.phase, 'failure_attempts':self.attempts['failure'], 'healthy_attempts':self.attempts['healthy'],
                'failure_limit':self.limit, 'healthy_limit':self.limit,
                'failure_last_ms':self.last_ms['failure'], 'healthy_last_ms':self.last_ms['healthy']}


def produce(index,root,stage):
    from fastapi import FastAPI
    from starlette.responses import Response
    import uvicorn
    from addp_common.client.module_registry import ModuleRegistration
    from addp_common.httpmetrics import start_optional
    from addp_common.httpmetrics.server import HTTPObservationMiddleware
    stage('configuration')
    config=root/f'python-{index}.deployment.json'
    deadline=time.monotonic()+90
    while not config.exists():
        if time.monotonic()>=deadline:raise RuntimeError('python_http_configuration_missing')
        time.sleep(.1)
    deployment=json.loads(config.read_text())
    os.environ['ADDP_PROCESS_METRICS_DEPLOYMENTS']=json.dumps([deployment])
    os.environ['ADDP_OBSERVABILITY_TRACES_ENABLED']='false'
    registration=ModuleRegistration(module_name=deployment['module_name'],instance_id='python-http-'+str(index),module_url='http://127.0.0.1:18080',route_prefix='/fixture')
    stage('metrics')
    source=start_optional(registration)
    if source is None:raise RuntimeError('python_http_source_failed')
    registration.process_metrics=source.declaration
    async def lifespan(_app):yield
    app=FastAPI(lifespan=asynccontextmanager(lifespan));app.add_middleware(HTTPObservationMiddleware,module=registration.module_name)
    traffic=HTTPFixtureTraffic(index,time.monotonic())
    @app.get('/items/{item}')
    async def item(item:str):return Response(status_code=204)
    @app.get('/slow')
    async def slow():await asyncio.sleep(.02);return Response(status_code=204)
    @app.get('/failure')
    async def failure():
        return Response(status_code=204 if traffic.phase=='healthy' else 503)
    server=uvicorn.Server(uvicorn.Config(app,host='127.0.0.1',port=18080,access_log=False,log_config=None,log_level='warning'))
    stage('http_ready')
    thread=threading.Thread(target=server.run,daemon=True);thread.start()
    while not server.started:
        if not thread.is_alive() or time.monotonic()>=deadline:raise RuntimeError('python_http_business_start_failed')
        time.sleep(.05)
    stage('identity')
    (root/f'python-{index}.record.json').write_text(json.dumps({'id':str(index),'module_name':registration.module_name,'instance_id':registration.instance_id,'role':'backend','started_at':registration.process_started_at,'instance':deployment['listen']}))
    def persist():
        (root/f'python-{index}.http-load.json').write_text(json.dumps(traffic.snapshot()))
        (root/f'python-{index}.requests').write_text(str(sum(traffic.attempts.values())))
    persist()
    stage('running','passed')
    try:
        while time.monotonic()<deadline+480 and not (root/f'python-{index}.stop').exists():
            control=root/'process-1.json.http-mode'
            try: healthy=control.read_text()=='healthy'
            except FileNotFoundError: healthy=False
            previous=traffic.phase
            path=traffic.take(time.monotonic(),healthy)
            if path is not None or previous!=traffic.phase:
                persist()
            if path is not None:
                try:
                    with urllib.request.urlopen('http://127.0.0.1:18080'+path,timeout=3) as response:response.read(1024)
                except urllib.error.HTTPError as response:
                    if response.code not in (404,503):raise
                    response.read(1024)
            time.sleep(.05)
        if not (root/f'python-{index}.stop').exists():raise RuntimeError('python_http_fixture_deadline')
    finally:
        stop_runtime(server,thread,source,sys.exc_info()[0] is not None)


def main():
    index=int(os.environ['HTTP_T2_INDEX']);root=Path('/source')
    if index not in (1,2):return 1
    current='copy'
    def stage(name,status='starting',error='none',exit_code=None,log_bytes=0):
        nonlocal current
        current=name
        value=startup_record(name,status,error,exit_code,log_bytes)
        try:(root/f'python-{index}.startup.json').write_text(json.dumps(value))
        except OSError:
            print(json.dumps(startup_record(name,'failed','diagnostic_write_failed')),flush=True)
            raise RuntimeError('python_http_diagnostic_write_failed') from None
    try:
        stage('copy')
        shutil.copytree('/app/common-python','/tmp/common-python',dirs_exist_ok=True)
        stage('dependencies')
        log=Path('/tmp/install.log')
        with log.open('wb') as output:
            result=subprocess.run([sys.executable,'-m','pip','install','--no-cache-dir','--target','/tmp/deps','/tmp/common-python','-r','/source/python-requirements.txt'],stdout=output,stderr=subprocess.STDOUT)
        if result.returncode:
            error,size=install_diagnostic(log)
            stage('dependencies','failed',error,result.returncode,size)
            return 1
        stage('imports')
        produce(index,root,stage)
        return 0
    except BaseException as error:
        try:stage(current,'failed',exception_kind(error))
        except RuntimeError:pass
        return 1


if __name__=='__main__':raise SystemExit(main())
