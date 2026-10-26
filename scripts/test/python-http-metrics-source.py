"""Gate-owned Python SDK producer. No System, model or business credentials."""
import asyncio
import json
import os
from pathlib import Path
import threading
import time
import urllib.request
from contextlib import asynccontextmanager

from fastapi import FastAPI
from starlette.responses import Response
import uvicorn
from addp_common.client.module_registry import ModuleRegistration
from addp_common.httpmetrics import start_optional
from addp_common.httpmetrics.server import HTTPObservationMiddleware


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


def main():
    index=int(os.environ['HTTP_T2_INDEX'])
    root=Path('/source');config=root/f'python-{index}.deployment.json'
    deadline=time.monotonic()+90
    while not config.exists():
        if time.monotonic()>=deadline:raise RuntimeError('python_http_configuration_missing')
        time.sleep(.1)
    deployment=json.loads(config.read_text())
    os.environ['ADDP_PROCESS_METRICS_DEPLOYMENTS']=json.dumps([deployment])
    os.environ['ADDP_OBSERVABILITY_TRACES_ENABLED']='false'
    registration=ModuleRegistration(module_name=deployment['module_name'],instance_id='python-http-'+str(index),module_url='http://127.0.0.1:18080',route_prefix='/fixture')
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
    thread=threading.Thread(target=server.run,daemon=True);thread.start()
    while not server.started:
        if not thread.is_alive() or time.monotonic()>=deadline:raise RuntimeError('python_http_business_start_failed')
        time.sleep(.05)
    (root/f'python-{index}.record.json').write_text(json.dumps({'id':str(index),'module_name':registration.module_name,'instance_id':registration.instance_id,'role':'backend','started_at':registration.process_started_at,'instance':deployment['listen']}))
    def persist():
        (root/f'python-{index}.http-load.json').write_text(json.dumps(traffic.snapshot()))
        (root/f'python-{index}.requests').write_text(str(sum(traffic.attempts.values())))
    persist()
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
        server.should_exit=True;thread.join(timeout=3);source.close()
        if thread.is_alive():raise RuntimeError('python_http_business_not_stopped')


if __name__=='__main__':main()
